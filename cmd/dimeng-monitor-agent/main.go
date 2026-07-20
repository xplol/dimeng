package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type config struct {
	Endpoint, ClaimToken, StateDir string
	Once                           bool
}
type metrics struct {
	ObservedAt    time.Time `json:"observed_at"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryTotal   uint64    `json:"memory_total_bytes"`
	MemoryUsed    uint64    `json:"memory_used_bytes"`
	DiskTotal     uint64    `json:"disk_total_bytes"`
	DiskUsed      uint64    `json:"disk_used_bytes"`
	NetworkRx     uint64    `json:"network_rx_bytes"`
	NetworkTx     uint64    `json:"network_tx_bytes"`
	UptimeSeconds uint64    `json:"uptime_seconds"`
}
type enrollment struct {
	AgentID     string `json:"agent_id"`
	AccessToken string `json:"access_token"`
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Endpoint, "endpoint", os.Getenv("DIMENG_ENDPOINT"), "DiMeng API endpoint")
	flag.StringVar(&cfg.ClaimToken, "claim-token", os.Getenv("DIMENG_CLAIM_TOKEN"), "single-use enrollment token")
	flag.StringVar(&cfg.StateDir, "state-dir", "/var/lib/dimeng-monitor-agent", "local state directory")
	flag.BoolVar(&cfg.Once, "once", false, "collect once and print JSON")
	flag.Parse()
	sample, err := collect()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Once {
		_ = json.NewEncoder(os.Stdout).Encode(sample)
		return
	}
	if cfg.Endpoint == "" || cfg.ClaimToken == "" {
		log.Fatal("endpoint and claim-token are required")
	}
	token, err := enroll(cfg, sample)
	if err != nil {
		log.Fatal(err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if sample, err = collect(); err == nil {
			_ = post(cfg.Endpoint+"/api/v1/monitor/agents/heartbeat", token, sample)
		}
		<-ticker.C
	}
}

func enroll(cfg config, sample metrics) (string, error) {
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return "", err
	}
	keyPath := filepath.Join(cfg.StateDir, "agent.key")
	private, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		_, generated, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return "", e
		}
		private = generated
		if e = os.WriteFile(keyPath, private, 0600); e != nil {
			return "", e
		}
	} else if err != nil {
		return "", err
	}
	if len(private) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid local agent key")
	}
	payload := map[string]any{"claim_token": cfg.ClaimToken, "public_key": base64.StdEncoding.EncodeToString(ed25519.PrivateKey(private).Public().(ed25519.PublicKey)), "metrics": sample}
	var response enrollment
	if err := postJSON(cfg.Endpoint+"/api/v1/monitor/agents/enroll", "", payload, &response); err != nil {
		return "", err
	}
	if response.AgentID == "" || response.AccessToken == "" {
		return "", fmt.Errorf("enrollment response missing credentials")
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "session.token"), []byte(response.AccessToken), 0600); err != nil {
		return "", err
	}
	return response.AccessToken, nil
}

func post(url, token string, value any) error { return postJSON(url, token, value, nil) }
func postJSON(url, token string, value, output any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("API returned %s", res.Status)
	}
	if output != nil {
		return json.NewDecoder(res.Body).Decode(output)
	}
	return nil
}

func collect() (metrics, error) {
	m := metrics{ObservedAt: time.Now().UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	mem, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return m, err
	}
	for _, line := range strings.Split(string(mem), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			m.MemoryTotal = n * 1024
		case "MemAvailable:":
			m.MemoryUsed = m.MemoryTotal - n*1024
		}
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		m.DiskTotal = stat.Blocks * uint64(stat.Bsize)
		m.DiskUsed = (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	}
	up, _ := os.ReadFile("/proc/uptime")
	if f := strings.Fields(string(up)); len(f) > 0 {
		v, _ := strconv.ParseFloat(f[0], 64)
		m.UptimeSeconds = uint64(v)
	}
	netdev, _ := os.ReadFile("/proc/net/dev")
	for _, l := range strings.Split(string(netdev), "\n") {
		p := strings.Split(l, ":")
		if len(p) != 2 || strings.TrimSpace(p[0]) == "lo" {
			continue
		}
		f := strings.Fields(p[1])
		if len(f) >= 9 {
			rx, _ := strconv.ParseUint(f[0], 10, 64)
			tx, _ := strconv.ParseUint(f[8], 10, 64)
			m.NetworkRx += rx
			m.NetworkTx += tx
		}
	}
	return m, nil
}
