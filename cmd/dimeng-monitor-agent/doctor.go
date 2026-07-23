package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorReport struct {
	Status       string        `json:"status"`
	Version      string        `json:"version"`
	Capabilities []string      `json:"capabilities"`
	Checks       []doctorCheck `json:"checks"`
}

func runDoctor(cfg config, output io.Writer) error {
	report := buildDoctorReport(cfg)
	if cfg.DoctorJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	fmt.Fprintf(output, "滴萌 Agent doctor\n版本：%s\n能力：%s\n\n", report.Version, strings.Join(report.Capabilities, ", "))
	for _, check := range report.Checks {
		fmt.Fprintf(output, "%-12s %-8s %s\n", check.Name, check.Status, check.Message)
	}
	if report.Status != "ok" {
		return fmt.Errorf("doctor found failed checks")
	}
	return nil
}

func buildDoctorReport(cfg config) doctorReport {
	checks := []doctorCheck{
		checkDoctorEndpoint(cfg.Endpoint),
		checkDoctorStateDir(cfg.StateDir),
		checkDoctorFile(filepath.Join(cfg.StateDir, "agent.key"), "设备密钥"),
		checkDoctorSession(filepath.Join(cfg.StateDir, "session.token")),
	}
	status := "ok"
	for _, check := range checks {
		if check.Status == "fail" {
			status = "fail"
			break
		}
	}
	return doctorReport{Status: status, Version: agentVersion, Capabilities: append([]string(nil), agentCapabilities...), Checks: checks}
}

func checkDoctorEndpoint(raw string) doctorCheck {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return doctorCheck{Name: "API 地址", Status: "fail", Message: "API 地址为空或格式无效"}
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost")) {
		return doctorCheck{Name: "API 地址", Status: "fail", Message: "正式环境必须使用 HTTPS"}
	}
	return doctorCheck{Name: "API 地址", Status: "ok", Message: parsed.Scheme + "://" + parsed.Host}
}

func checkDoctorStateDir(path string) doctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return doctorCheck{Name: "状态目录", Status: "fail", Message: "状态目录不存在或不可读取"}
	}
	if !info.IsDir() {
		return doctorCheck{Name: "状态目录", Status: "fail", Message: "状态路径不是目录"}
	}
	if info.Mode().Perm()&0077 != 0 {
		return doctorCheck{Name: "状态目录", Status: "fail", Message: "状态目录权限过宽，应为 0700"}
	}
	return doctorCheck{Name: "状态目录", Status: "ok", Message: path}
}

func checkDoctorFile(path, label string) doctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		return doctorCheck{Name: label, Status: "warn", Message: "尚未生成，首次注册后会创建"}
	}
	if info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return doctorCheck{Name: label, Status: "fail", Message: "文件不存在、是目录或权限过宽"}
	}
	return doctorCheck{Name: label, Status: "ok", Message: "存在且权限受限"}
}

func checkDoctorSession(path string) doctorCheck {
	check := checkDoctorFile(path, "会话凭据")
	if check.Status != "ok" {
		return check
	}
	value, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(value)) == "" {
		return doctorCheck{Name: "会话凭据", Status: "fail", Message: "凭据为空或不可读取"}
	}
	return doctorCheck{Name: "会话凭据", Status: "ok", Message: "已注册"}
}
