# 滴萌服务器探针 / DiMeng Monitor Agent

滴萌服务器探针是开源的 Linux 服务器状态采集器。它只主动向滴萌 API 发起 HTTPS 请求，不新增监听端口，也不包含远程 Shell、终端、文件管理、端口转发或任意命令执行能力。

## 公开仓库与联系

- GitHub 主仓库：https://github.com/xplol/dimeng
- Gitee 国内镜像：https://gitee.com/xiang_peng/dimeng
- 博主 QQ：5759323
- 微信 / 电话：18981837812

GitHub 是主发布源，Gitee 镜像相同的稳定版本，方便中国大陆服务器下载。

## 当前状态

当前仓库包含 Agent、安装脚本、`fwq` 管理命令和 Linux `amd64/arm64` 构建产物。Agent 的首次注册、会话凭据复用、心跳、安装、重启和卸载流程已在 Ubuntu 26.04 测试机完成验证。

滴萌监控注册与心跳 API 已通过 `https://xlx.wipecell.top` 接通。真实测试已验证：安装后显示出口 IP、一次性绑定码和主机指纹；登录用户可以认领服务器；其他用户不能读取该服务器、详情或指标。`v0.2.0` 新增终端绑定二维码，`v0.3.0` 增加结构化 doctor、能力上报和独立签名升级器，`v0.3.1` 修复首次安装目录初始化和失败清理，`v0.3.2` 确保升级监听单元立即启动，`v0.3.3` 增加 GitHub/Gitee 实际下载重定向域名白名单，最终发布状态以 GitHub Release 为准。

## 支持范围

- Linux `amd64`、`arm64`
- 使用 systemd 的 Ubuntu、Debian、CentOS/RHEL、Rocky/Alma、Alibaba Cloud Linux
- 自动补齐 `ca-certificates`、`curl`、`coreutils`、`passwd/shadow-utils` 等缺失依赖
- 暂不支持 Windows、macOS、Alpine/OpenRC、容器内安装和非 systemd 系统

## 安装前授权

安装脚本会先显示公开仓库、联系方式和安装与数据授权协议。输入 `1` 表示同意并继续，输入 `2` 表示不同意并退出；其他输入也会停止安装。自动化部署可在已经阅读协议后显式加入 `--accept-agreement`。

首次注册成功后，安装器会显示终端二维码、服务器出口 IP、一次性绑定码、主机指纹和过期时间。用户登录滴萌客户端后扫描二维码即可自动核验；无法扫码时仍可手工输入 IP 与绑定码。绑定码 15 分钟失效且只能使用一次，不要把二维码或绑定码写进 Git、聊天记录、公开 URL 或工单截图。

中国大陆服务器推荐从 Gitee 下载脚本：

```sh
curl -fL https://gitee.com/xiang_peng/dimeng/raw/main/scripts/install.sh -o dimeng-install.sh
sudo sh dimeng-install.sh \
  --endpoint 'https://xlx.wipecell.top'
```

海外服务器可将脚本地址替换为 `https://raw.githubusercontent.com/xplol/dimeng/main/scripts/install.sh`。安装器在中国大陆优先下载 Gitee 发布物，失败后自动回退 GitHub Release 和 GitHub Raw。

安装器会识别架构、下载对应二进制和 `.sha256`、完成校验、自检后再安装。

发布 `v0.3.3` 后启用签名升级器时，需要同时设置 `DIMENG_VERSION=v0.3.3` 和 `DIMENG_ENABLE_SIGNED_UPGRADE=1`。发布辅助脚本 `scripts/build-release.sh` 会生成 Linux `amd64/arm64` Agent、升级器、SHA-256、manifest 和 Ed25519 签名；签名私钥不进入仓库。

## fwq 管理命令

安装完成后直接输入：

```sh
fwq                 # 打开交互菜单
fwq status          # 查看状态
sudo fwq doctor     # 检查配置、凭据、版本与本地安全状态
sudo fwq doctor --json
fwq logs            # 查看最近日志
sudo fwq restart    # 重启
sudo fwq uninstall  # 卸载 Agent 和本地身份，保留 fwq 便于重装
sudo fwq purge      # 完整清除 Agent、身份、fwq 和专用系统用户
```

`fwq uninstall` 和 `fwq purge` 都不会删除系统依赖，防止影响服务器上其他软件。

## 数据与安全边界

- 采集：CPU、内存、根文件系统磁盘、网卡累计收发量、运行时长、操作系统和架构。
- 不采集：站点文件内容、数据库内容、SSH 密钥、环境变量、进程参数、网络数据包内容。
- 只出站 HTTPS，通常使用 TCP 443；不要求新增入站防火墙规则。
- 以无登录权限的 `dimeng-agent` 专用用户运行。
- systemd 默认限制 `MemoryMax=64M`、`CPUQuota=20%`，并启用文件系统、设备、内核和权限沙箱。
- Agent 会话凭据权限为 `0600`；绑定码只保存在本地绑定回执和服务端哈希中，后续心跳使用本地会话凭据。
- `fwq uninstall` 会删除本地身份，重新安装时必须生成新的绑定码。
- `v0.3.3` 支持自动接收升级建议，但只有固定公钥验签、SHA-256、版本、架构和下载域名白名单全部通过后才会替换二进制；可在 `/etc/dimeng-monitor-agent/agent.env` 设置 `DIMENG_AUTO_UPGRADE=false` 关闭。当前稳定安装入口仍是 `v0.2.0`，升级器必须随 `v0.3.3` 签名发布物一起显式启用。
- 普通 Agent 仍以 `dimeng-agent` 用户运行；root 升级器是独立 oneshot 服务，只处理固定 JSON，不接受 Shell 或任意命令。
- 升级前保留上一版；候选版本必须连续通过 systemd active 和版本检查，失败会自动恢复旧二进制。
- 公开监控 Agent 与滴萌平台私有测速 Agent 完全分离。
- 终端二维码由 `github.com/skip2/go-qrcode` 在服务器本地生成，不会把绑定信息发送给第三方二维码服务；第三方许可见 `THIRD_PARTY_NOTICES.md`。

## 后续上线工作

- 建立正式数据库迁移流程，替代当前应用启动时的幂等建表。
- 在隐私政策中明确指标保存期限、用途、导出方式和用户删除流程。
- 建立版本升级、回滚和安全漏洞通知机制，并在真实 `amd64/arm64` 服务器上完成发布验收。

## 本地开发验证

```sh
go test ./...
go vet ./...
go build ./cmd/dimeng-monitor-agent
./dimeng-monitor-agent --once
sh -n scripts/install.sh
```

安装器和 systemd 变更必须在隔离 Linux 测试机验证安装、首次注册、重启、无监听端口、卸载与原有业务不受影响，不能只根据 macOS 编译结果发布。
