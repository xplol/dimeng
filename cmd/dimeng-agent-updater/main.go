package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxBinaryBytes int64 = 64 * 1024 * 1024

type upgradeRequest struct {
	Version      string `json:"version"`
	ManifestURL  string `json:"manifest_url"`
	SignatureURL string `json:"signature_url"`
}

type releaseManifest struct {
	Version string         `json:"version"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type updaterConfig struct {
	RequestFile  string
	PublicKey    string
	InstallPath  string
	BackupPath   string
	Service      string
	AllowedHosts map[string]bool
	HTTPClient   *http.Client
	RunCommand   func(string, ...string) error
}

func main() {
	var requestFile, publicKey, installPath, backupPath, service, hosts string
	flag.StringVar(&requestFile, "request-file", "/var/lib/dimeng-monitor-agent/upgrade/request.json", "upgrade request JSON")
	flag.StringVar(&publicKey, "public-key-file", "/etc/dimeng-monitor-agent/release-public.key", "base64 Ed25519 public key")
	flag.StringVar(&installPath, "install-path", "/usr/local/bin/dimeng-monitor-agent", "agent binary path")
	flag.StringVar(&backupPath, "backup-path", "/usr/local/lib/dimeng-monitor-agent/dimeng-monitor-agent.previous", "previous agent binary path")
	flag.StringVar(&service, "service", "dimeng-monitor-agent.service", "systemd service to restart")
	flag.StringVar(&hosts, "allowed-hosts", "", "comma-separated trusted release hosts")
	flag.Parse()

	cfg := updaterConfig{RequestFile: requestFile, PublicKey: publicKey, InstallPath: installPath, BackupPath: backupPath, Service: service, AllowedHosts: parseAllowedHosts(hosts), HTTPClient: &http.Client{Timeout: 45 * time.Second}, RunCommand: runCommand}
	if err := applyUpgrade(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "[滴萌升级器] 错误：", err)
		os.Exit(1)
	}
}

func applyUpgrade(ctx context.Context, cfg updaterConfig) error {
	request, err := readUpgradeRequest(cfg.RequestFile)
	if err != nil {
		return err
	}
	if !validVersion(request.Version) {
		return errors.New("invalid requested version")
	}
	manifestRaw, err := downloadTrusted(ctx, cfg, request.ManifestURL, 512*1024)
	if err != nil {
		return fmt.Errorf("download manifest: %w", err)
	}
	signatureRaw, err := downloadTrusted(ctx, cfg, request.SignatureURL, 16*1024)
	if err != nil {
		return fmt.Errorf("download manifest signature: %w", err)
	}
	if err := verifyManifestSignature(manifestRaw, signatureRaw, cfg.PublicKey); err != nil {
		return fmt.Errorf("verify manifest signature: %w", err)
	}
	manifest, err := parseManifest(manifestRaw)
	if err != nil {
		return err
	}
	if manifest.Version != request.Version {
		return errors.New("manifest version does not match request")
	}
	if err := rejectDowngrade(cfg.InstallPath, manifest.Version); err != nil {
		return err
	}
	asset, ok := findAsset(manifest, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("release has no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	binary, err := downloadTrusted(ctx, cfg, asset.URL, maxBinaryBytes)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	if err := verifySHA256(binary, asset.SHA256); err != nil {
		return err
	}
	if err := replaceAndRestart(cfg, binary, manifest.Version); err != nil {
		return err
	}
	return os.Remove(cfg.RequestFile)
}

func readUpgradeRequest(path string) (upgradeRequest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return upgradeRequest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request upgradeRequest
	if err := decoder.Decode(&request); err != nil {
		return upgradeRequest{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return upgradeRequest{}, errors.New("upgrade request has trailing JSON")
	}
	return request, nil
}

func parseManifest(raw []byte) (releaseManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if !validVersion(manifest.Version) || len(manifest.Assets) == 0 {
		return releaseManifest{}, errors.New("invalid manifest")
	}
	return manifest, nil
}

func verifyManifestSignature(message, signature []byte, keyFile string) error {
	encoded, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid Ed25519 public key")
	}
	decodedSignature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || len(decodedSignature) != ed25519.SignatureSize {
		return errors.New("invalid manifest signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), message, decodedSignature) {
		return errors.New("signature mismatch")
	}
	return nil
}

func downloadTrusted(ctx context.Context, cfg updaterConfig, rawURL string, maxBytes int64) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isTrustedReleaseURL(parsed, cfg.AllowedHosts) {
		return nil, errors.New("release URL is not an allowed HTTPS host")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	client := *cfg.HTTPClient
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if !isTrustedReleaseURL(next.URL, cfg.AllowedHosts) {
			return errors.New("redirected to an untrusted release URL")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > maxBytes {
		return nil, errors.New("download exceeds size limit")
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > maxBytes {
		return nil, errors.New("download exceeds size limit")
	}
	return value, nil
}

func isTrustedReleaseURL(parsed *url.URL, allowedHosts map[string]bool) bool {
	return parsed != nil && parsed.Scheme == "https" && parsed.Hostname() != "" && allowedHosts[strings.ToLower(parsed.Hostname())]
}

func replaceAndRestart(cfg updaterConfig, binary []byte, version string) error {
	directory := filepath.Dir(cfg.InstallPath)
	staged, err := os.CreateTemp(directory, ".dimeng-monitor-agent-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(binary); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Chmod(0755); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := verifyBinaryVersion(stagedPath, version); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.BackupPath), 0755); err != nil {
		return err
	}
	if err := copyFileAtomic(cfg.InstallPath, cfg.BackupPath, 0755); err != nil {
		return err
	}
	if err := os.Rename(stagedPath, cfg.InstallPath); err != nil {
		return err
	}
	if err := cfg.RunCommand("systemctl", "restart", cfg.Service); err == nil && waitForStableService(cfg, version, 20*time.Second) {
		return nil
	}
	if restoreErr := copyFileAtomic(cfg.BackupPath, cfg.InstallPath, 0755); restoreErr != nil {
		return fmt.Errorf("new version failed and rollback failed: %w", restoreErr)
	}
	if restartErr := cfg.RunCommand("systemctl", "restart", cfg.Service); restartErr != nil {
		return fmt.Errorf("new version failed; restored previous binary but restart failed: %w", restartErr)
	}
	return errors.New("new version failed health check and was rolled back")
}

func copyFileAtomic(sourcePath, destinationPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destinationPath), ".dimeng-copy-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destinationPath)
}

func waitForStableService(cfg updaterConfig, version string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	consecutiveHealthy := 0
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if cfg.RunCommand("systemctl", "is-active", "--quiet", cfg.Service) == nil && verifyBinaryVersion(cfg.InstallPath, version) == nil {
			consecutiveHealthy++
			if consecutiveHealthy >= 5 {
				return true
			}
		} else {
			consecutiveHealthy = 0
		}
	}
	return false
}

func verifyBinaryVersion(path, expected string) error {
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return fmt.Errorf("candidate version check failed: %w", err)
	}
	if strings.TrimSpace(string(output)) != expected {
		return errors.New("candidate binary version does not match manifest")
	}
	return nil
}

func rejectDowngrade(path, target string) error {
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return nil
	}
	current := strings.TrimSpace(string(output))
	if validVersion(current) && compareVersion(target, current) < 0 {
		return fmt.Errorf("refusing downgrade from %s to %s", current, target)
	}
	return nil
}

func validVersion(value string) bool {
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func compareVersion(left, right string) int {
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	for index := range leftParts {
		if len(leftParts[index]) != len(rightParts[index]) {
			if len(leftParts[index]) < len(rightParts[index]) {
				return -1
			}
			return 1
		}
		if leftParts[index] < rightParts[index] {
			return -1
		}
		if leftParts[index] > rightParts[index] {
			return 1
		}
	}
	return 0
}

func findAsset(manifest releaseManifest, goos, arch string) (releaseAsset, bool) {
	for _, asset := range manifest.Assets {
		if asset.OS == goos && asset.Arch == arch && len(asset.SHA256) == sha256.Size*2 {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func verifySHA256(value []byte, expected string) error {
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("invalid asset SHA-256")
	}
	digest := sha256.Sum256(value)
	if !bytes.Equal(digest[:], decoded) {
		return errors.New("asset SHA-256 mismatch")
	}
	return nil
}

func parseAllowedHosts(value string) map[string]bool {
	result := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		if host := strings.ToLower(strings.TrimSpace(item)); host != "" {
			result[host] = true
		}
	}
	return result
}

func runCommand(name string, arguments ...string) error {
	return exec.Command(name, arguments...).Run()
}
