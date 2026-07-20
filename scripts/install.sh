#!/usr/bin/env sh
set -eu

BIN="${1:-./dimeng-monitor-agent}"
ENDPOINT="${DIMENG_ENDPOINT:-}"
CLAIM_TOKEN="${DIMENG_CLAIM_TOKEN:-}"

if [ "$(id -u)" -ne 0 ]; then echo "Run with sudo." >&2; exit 1; fi
if [ ! -x "$BIN" ] || [ -z "$ENDPOINT" ] || [ -z "$CLAIM_TOKEN" ]; then echo "Set DIMENG_ENDPOINT and DIMENG_CLAIM_TOKEN, then pass the built binary path." >&2; exit 1; fi
id -u dimeng-agent >/dev/null 2>&1 || useradd --system --home /var/lib/dimeng-monitor-agent --shell /usr/sbin/nologin dimeng-agent
install -d -o dimeng-agent -g dimeng-agent -m 0700 /var/lib/dimeng-monitor-agent
install -d -m 0750 /etc/dimeng-monitor-agent
install -m 0755 "$BIN" /usr/local/bin/dimeng-monitor-agent
printf 'DIMENG_ENDPOINT=%s\nDIMENG_CLAIM_TOKEN=%s\n' "$ENDPOINT" "$CLAIM_TOKEN" > /etc/dimeng-monitor-agent/agent.env
chmod 0600 /etc/dimeng-monitor-agent/agent.env
install -m 0644 packaging/systemd/dimeng-monitor-agent.service /etc/systemd/system/dimeng-monitor-agent.service
systemctl daemon-reload
systemctl enable --now dimeng-monitor-agent
