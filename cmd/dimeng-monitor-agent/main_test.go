package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSessionToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.token"), []byte("session-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := loadSessionToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if token != "session-value" {
		t.Fatalf("unexpected token %q", token)
	}
}

func TestResolveClaimToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claim.token")
	if err := os.WriteFile(path, []byte("file-value\n"), 0600); err != nil {
		t.Fatal(err)
	}

	token, err := resolveClaimToken("inline-value", path)
	if err != nil || token != "inline-value" {
		t.Fatalf("inline token was not preferred: token=%q err=%v", token, err)
	}
	token, err = resolveClaimToken("", path)
	if err != nil || token != "file-value" {
		t.Fatalf("file token was not loaded: token=%q err=%v", token, err)
	}
	token, err = resolveClaimToken("", filepath.Join(dir, "missing.token"))
	if err != nil || token != "" {
		t.Fatalf("missing optional token should be empty: token=%q err=%v", token, err)
	}
}

func TestSaveEnrollmentReceiptOmitsAccessToken(t *testing.T) {
	dir := t.TempDir()
	result := enrollment{AgentID: "agent-1", AccessToken: "secret-session", BindingCode: "DM-ABCD-2345-WXYZ", ObservedIP: "203.0.113.10", Fingerprint: "A1B2C3D4", BindingExpiresAt: "2026-07-22T13:30:00Z"}
	if err := saveEnrollmentReceipt(dir, result); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(filepath.Join(dir, "enrollment.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(value), result.AccessToken) {
		t.Fatal("receipt must not contain the access token")
	}
}

func TestBuildBindingURI(t *testing.T) {
	receipt := enrollment{ObservedIP: "203.0.113.10", BindingCode: "DM-ABCD-2345-WXYZ", Fingerprint: "A1B2C3D4"}
	value, err := buildBindingURI(receipt)
	if err != nil {
		t.Fatal(err)
	}
	expected := "dimeng://bind?code=DM-ABCD-2345-WXYZ&fingerprint=A1B2C3D4&ip=203.0.113.10"
	if value != expected {
		t.Fatalf("unexpected binding URI %q", value)
	}
}

func TestShowEnrollmentReceiptIncludesQRCodeAndFallback(t *testing.T) {
	dir := t.TempDir()
	receipt := enrollment{AgentID: "agent-1", BindingCode: "DM-ABCD-2345-WXYZ", ObservedIP: "203.0.113.10", Fingerprint: "A1B2C3D4", BindingExpiresAt: "2026-07-22T13:30:00Z"}
	if err := saveEnrollmentReceipt(dir, receipt); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := showEnrollmentReceipt(dir, &output); err != nil {
		t.Fatal(err)
	}
	value := output.String()
	for _, expected := range []string{"扫描二维码", "DM-ABCD-2345-WXYZ", "A1B2C3D4", "手工输入"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("receipt output missing %q", expected)
		}
	}
}

func TestParseCPUTimes(t *testing.T) {
	times, err := parseCPUTimes("cpu  100 20 30 400 50 6 7 8 0 0\ncpu0 1 2 3 4\n")
	if err != nil {
		t.Fatal(err)
	}
	if times.total != 621 {
		t.Fatalf("unexpected total %d", times.total)
	}
	if times.idle != 450 {
		t.Fatalf("unexpected idle %d", times.idle)
	}
}
