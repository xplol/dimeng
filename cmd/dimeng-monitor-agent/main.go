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
	Endpoint, ClaimToken, ClaimTokenFile, StateDir string
	Once, ShowEnrollment                           bool
}
type metrics struct {
	ObservedAt    time.Time `json:"observed_at"`
	OS            string    `json:"os"`
	OSName        string    `json:"os_name"`
	Hostname      string    `json:"hostname"`
	Arch          string    `json:"arch"`
	AgentVersion  string    `json:"agent_version"`
	CPUCores      int       `json:"cpu_cores"`
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
	AgentID          string `json:"agent_id"`
	AccessToken      string `json:"access_token,omitempty"`
	BindingCode      string `json:"binding_code"`
	ObservedIP       string `json:"observed_ip"`
	Fingerprint      string `json:"fingerprint"`
	BindingExpiresAt string `json:"binding_expires_at"`
}

const agentVersion = "v0.1.1"

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Endpoint, "endpoint", os.Getenv("DIMENG_ENDPOINT"), "DiMeng API endpoint")
	flag.StringVar(&cfg.ClaimToken, "claim-token", os.Getenv("DIMENG_CLAIM_TOKEN"), "single-use enrollment token")
	flag.StringVar(&cfg.ClaimTokenFile, "claim-token-file", os.Getenv("DIMENG_CLAIM_TOKEN_FILE"), "file containing the single-use enrollment token")
	flag.StringVar(&cfg.StateDir, "state-dir", "/var/lib/dimeng-monitor-agent", "local state directory")
	flag.BoolVar(&cfg.Once, "once", false, "collect once and print JSON")
	flag.BoolVar(&cfg.ShowEnrollment, "show-enrollment", false, "show the latest binding receipt")
	flag.Parse()
	if cfg.ShowEnrollment {
		if err := showEnrollmentReceipt(cfg.StateDir, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	sample, err := collect()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Once {
		_ = json.NewEncoder(os.Stdout).Encode(sample)
		return
	}
	if cfg.Endpoint == "" {
		log.Fatal("endpoint is required")
	}
	token, err := loadSessionToken(cfg.StateDir)
	if os.IsNotExist(err) {
		cfg.ClaimToken, err = resolveClaimToken(cfg.ClaimToken, cfg.ClaimTokenFile)
		if err != nil {
			log.Fatal(err)
		}
		var result enrollment
		result, err = enroll(cfg, sample)
		token = result.AccessToken
		if err == nil && cfg.ClaimTokenFile != "" {
			if removeErr := os.Remove(cfg.ClaimTokenFile); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Printf("warning: failed to remove used claim token: %v", removeErr)
			}
		}
	}
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

func loadSessionToken(stateDir string) (string, error) {
	value, err := os.ReadFile(filepath.Join(stateDir, "session.token"))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(value))
	if token == "" {
		return "", fmt.Errorf("local session token is empty")
	}
	return token, nil
}

func resolveClaimToken(inline, filePath string) (string, error) {
	if token := strings.TrimSpace(inline); token != "" {
		return token, nil
	}
	if filePath == "" {
		return "", nil
	}
	value, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read claim token: %w", err)
	}
	token := strings.TrimSpace(string(value))
	if token == "" {
		return "", fmt.Errorf("claim token file is empty")
	}
	return token, nil
}

func enroll(cfg config, sample metrics) (enrollment, error) {
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return enrollment{}, err
	}
	keyPath := filepath.Join(cfg.StateDir, "agent.key")
	private, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		_, generated, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return enrollment{}, e
		}
		private = generated
		if e = os.WriteFile(keyPath, private, 0600); e != nil {
			return enrollment{}, e
		}
	} else if err != nil {
		return enrollment{}, err
	}
	if len(private) != ed25519.PrivateKeySize {
		return enrollment{}, fmt.Errorf("invalid local agent key")
	}
	payload := map[string]any{"claim_token": cfg.ClaimToken, "public_key": base64.StdEncoding.EncodeToString(ed25519.PrivateKey(private).Public().(ed25519.PublicKey)), "metrics": sample}
	var response enrollment
	if err := postJSON(cfg.Endpoint+"/api/v1/monitor/agents/enroll", "", payload, &response); err != nil {
		return enrollment{}, err
	}
	if response.AgentID == "" || response.AccessToken == "" {
		return enrollment{}, fmt.Errorf("enrollment response missing credentials")
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "session.token"), []byte(response.AccessToken), 0600); err != nil {
		return enrollment{}, err
	}
	if response.BindingCode != "" {
		if err := saveEnrollmentReceipt(cfg.StateDir, response); err != nil {
			return enrollment{}, err
		}
	}
	return response, nil
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
	hostname, _ := os.Hostname()
	m := metrics{ObservedAt: time.Now().UTC(), OS: runtime.GOOS, OSName: readOSName(), Hostname: hostname, Arch: runtime.GOARCH, AgentVersion: agentVersion, CPUCores: runtime.NumCPU()}
	cpu, err := sampleCPUPercent(200 * time.Millisecond)
	if err != nil {
		return m, err
	}
	m.CPUPercent = cpu
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

func readOSName() string {
	value, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(value), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return runtime.GOOS
}

func saveEnrollmentReceipt(stateDir string, result enrollment) error {
	receipt := enrollment{
		AgentID: result.AgentID, BindingCode: result.BindingCode, ObservedIP: result.ObservedIP,
		Fingerprint: result.Fingerprint, BindingExpiresAt: result.BindingExpiresAt,
	}
	value, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "enrollment.json"), value, 0600)
}

func showEnrollmentReceipt(stateDir string, output *os.File) error {
	value, err := os.ReadFile(filepath.Join(stateDir, "enrollment.json"))
	if err != nil {
		return fmt.Errorf("read enrollment receipt: %w", err)
	}
	var receipt enrollment
	if err := json.Unmarshal(value, &receipt); err != nil {
		return fmt.Errorf("decode enrollment receipt: %w", err)
	}
	_, err = fmt.Fprintf(output, "滴萌探针等待绑定\n公网 IP：%s\n绑定码：%s\n主机指纹：%s\n有效期至：%s\nAgent ID：%s\n", receipt.ObservedIP, receipt.BindingCode, receipt.Fingerprint, receipt.BindingExpiresAt, receipt.AgentID)
	return err
}

type cpuTimes struct {
	total uint64
	idle  uint64
}

func sampleCPUPercent(interval time.Duration) (float64, error) {
	first, err := readCPUTimes()
	if err != nil {
		return 0, err
	}
	time.Sleep(interval)
	second, err := readCPUTimes()
	if err != nil {
		return 0, err
	}
	totalDelta := second.total - first.total
	if totalDelta == 0 {
		return 0, nil
	}
	idleDelta := second.idle - first.idle
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta), nil
}

func readCPUTimes() (cpuTimes, error) {
	value, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	return parseCPUTimes(string(value))
}

func parseCPUTimes(value string) (cpuTimes, error) {
	line := strings.SplitN(value, "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, fmt.Errorf("invalid /proc/stat cpu line")
	}
	var times cpuTimes
	for index, field := range fields[1:] {
		part, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuTimes{}, fmt.Errorf("invalid /proc/stat value: %w", err)
		}
		times.total += part
		if index == 3 || index == 4 {
			times.idle += part
		}
	}
	return times, nil
}
