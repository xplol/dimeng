package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestSignatureAndAssetChecks(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-public.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(public)), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"version":"v0.3.0","assets":[{"os":"linux","arch":"amd64","url":"https://downloads.example.com/a","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
	signature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, manifest)))
	if err := verifyManifestSignature(manifest, signature, keyPath); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findAsset(parsed, "linux", "amd64"); !ok {
		t.Fatal("expected linux/amd64 asset")
	}
	if _, ok := findAsset(parsed, "linux", "arm64"); ok {
		t.Fatal("unexpected linux/arm64 asset")
	}
}

func TestRejectsUnknownRequestFieldsAndHashMismatch(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(requestPath, []byte(`{"version":"v0.3.0","manifest_url":"https://example.com/m","signature_url":"https://example.com/s","command":"sh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readUpgradeRequest(requestPath); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown request field was accepted: %v", err)
	}
	if err := verifySHA256([]byte("binary"), strings.Repeat("0", 64)); err == nil {
		t.Fatal("SHA-256 mismatch was accepted")
	}
}

func TestVersionComparisonAndAllowedHosts(t *testing.T) {
	if compareVersion("v0.10.0", "v0.9.9") <= 0 {
		t.Fatal("semantic comparison did not handle double digits")
	}
	if !validVersion("v1.2.3") || validVersion("v1.2") || validVersion("v1.2.3-beta") {
		t.Fatal("unexpected version validation")
	}
	hosts := parseAllowedHosts("downloads.example.com, github.com")
	if !hosts["downloads.example.com"] || !hosts["github.com"] {
		t.Fatal("allowed hosts were not normalized")
	}
	if isTrustedReleaseURL(nil, hosts) {
		t.Fatal("nil URL must not be trusted")
	}
}
