# DiMeng Monitor Agent

DiMeng Monitor Agent is an open-source Linux server monitor. It only makes outbound HTTPS requests to the DiMeng API. It opens no listening port and has no remote shell, terminal, file manager, port forwarding, or arbitrary command execution.

## Repositories

- Primary source: https://github.com/xplol/dimeng
- China mirror: https://gitee.com/xiang_peng/dimeng

GitHub is the primary source of releases and issues. Gitee mirrors the same `main` branch for domestic access.

## Status

This is the initial public implementation. It collects Linux memory, root filesystem, network byte totals, uptime, OS and architecture, and enrolls with a one-time claim token. CPU sampling and API history aggregation are being completed alongside the DiMeng API.

## Local verification

```bash
go build ./cmd/dimeng-monitor-agent
./dimeng-monitor-agent --once
```

## Enrollment

The client creates a single-use claim token. Do not place the token in a URL, chat log, or Git repository.

```bash
sudo ./dimeng-monitor-agent \
  --endpoint https://api.ping1.me \
  --claim-token '<single-use-token>'
```

## Security boundary

- Outbound HTTPS only, normally TCP 443.
- No listener and no inbound firewall rule required.
- No collection of file contents, process arguments, environment variables, SSH keys, databases, or packet payloads.
- Enrollment token is single-use; the agent generates an Ed25519 key under its state directory.
- The public monitor Agent is separate from DiMeng's private platform probe Agent.
