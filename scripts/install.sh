#!/usr/bin/env sh
set -eu

AGENT_VERSION="${DIMENG_VERSION:-v0.1.0}"
AGREEMENT_VERSION="2026-07-22-v1"
SERVICE_NAME="dimeng-monitor-agent"
AGENT_USER="dimeng-agent"
BIN_PATH="/usr/local/bin/dimeng-monitor-agent"
CONFIG_DIR="/etc/dimeng-monitor-agent"
ENV_PATH="${CONFIG_DIR}/agent.env"
STATE_DIR="/var/lib/dimeng-monitor-agent"
CLAIM_TOKEN_PATH="${STATE_DIR}/claim.token"
SESSION_TOKEN_PATH="${STATE_DIR}/session.token"
UNIT_PATH="/etc/systemd/system/${SERVICE_NAME}.service"
MANAGER_DIR="/usr/local/lib/dimeng-monitor-agent"
MANAGER_SCRIPT="${MANAGER_DIR}/install.sh"
MANAGER_PATH="/usr/local/bin/fwq"
INSTALLER_URL="${DIMENG_INSTALLER_URL:-https://raw.githubusercontent.com/xplol/dimeng/main/scripts/install.sh}"
GITHUB_RELEASE_BASE="https://github.com/xplol/dimeng/releases/download/${AGENT_VERSION}"
GITHUB_RAW_BASE="https://raw.githubusercontent.com/xplol/dimeng/${AGENT_VERSION}/dist"
GITEE_RAW_BASE="https://gitee.com/xiang_peng/dimeng/raw/${AGENT_VERSION}/dist"

TEMP_ROOT=""

info() { printf '[滴萌] %s\n' "$*"; }
warn() { printf '[滴萌] 警告：%s\n' "$*" >&2; }
die() { printf '[滴萌] 错误：%s\n' "$*" >&2; exit 1; }

cleanup() {
  if [ -n "$TEMP_ROOT" ] && [ -d "$TEMP_ROOT" ]; then
    rm -rf "$TEMP_ROOT"
  fi
}
trap cleanup EXIT HUP INT TERM

require_root() {
  [ "$(id -u)" -eq 0 ] || die "请使用 sudo 或 root 运行。"
}

has_tty() {
  ( : </dev/tty ) 2>/dev/null
}

print_project_info() {
  cat <<'EOF'

滴萌服务器探针
公开仓库：
  GitHub：https://github.com/xplol/dimeng
  Gitee：https://gitee.com/xiang_peng/dimeng

作者 / 博主联系方式：
  QQ：5759323
  微信 / 电话：18981837812
EOF
}

print_agreement() {
  print_project_info
  cat <<EOF

安装与数据授权协议（${AGREEMENT_VERSION}）

1. 我确认自己是本服务器所有者，或已获得服务器所有者的明确授权。
2. 我授权滴萌探针采集并上报 CPU、内存、磁盘、网络流量、运行时长、系统及架构等运行指标。
3. 探针不读取站点文件内容、数据库内容、SSH 密钥、环境变量、进程参数或网络数据包内容。
4. 探针只主动发起 HTTPS 出站连接，不新增公网监听端口，不提供远程 Shell、文件管理或任意命令执行能力。
5. 我理解安装和运行仍会占用少量 CPU、内存、磁盘和网络资源，并同意自行确认服务器业务兼容性。
6. 首次注册后，终端会显示 15 分钟有效的一次性绑定码；用户需在滴萌客户端登录后完成绑定。
7. 我可以随时运行 fwq uninstall 卸载探针；系统依赖不会自动删除，以免影响其他软件。
8. 项目源码按仓库中的开源许可证公开；本协议只用于服务器安装授权和指标数据处理告知。

继续安装表示你已阅读并同意以上内容。
EOF
}

accept_agreement() {
  print_agreement
  if [ "${ACCEPT_AGREEMENT:-0}" = "1" ]; then
    info "已通过命令参数确认授权协议。"
    return
  fi
  has_tty || die "当前环境无法交互确认，请阅读协议后添加 --accept-agreement。"
  printf '\n请输入“同意”继续安装，其他输入将退出：' >/dev/tty
  answer=""
  IFS= read -r answer </dev/tty || true
  [ "$answer" = "同意" ] || die "你没有同意授权协议，安装已取消。"
}

install_usage() {
  cat <<'EOF'
用法：
  sudo sh install.sh --endpoint <HTTPS地址>

参数：
  --endpoint URL          滴萌 API 地址；更新时可读取现有配置
  --claim-token TOKEN     兼容旧版预签发流程，普通用户无需提供
  --binary PATH           使用本地二进制，供受控测试使用
  --checksum SHA256       校验本地二进制
  --download-base URL     覆盖发布文件下载目录
  --accept-agreement      非交互方式确认已阅读并同意授权协议
  --no-start              安装并启用服务，但暂不启动
  -h, --help              显示帮助

安装后运行 fwq 管理探针。
EOF
}

manager_usage() {
  cat <<'EOF'
fwq - 滴萌服务器探针管理命令

  fwq                 打开交互菜单
  fwq install [...]   安装或更新探针
  fwq status          查看服务状态
  fwq logs            查看最近日志
  fwq restart         重启服务
  fwq uninstall       卸载探针，保留 fwq
  fwq purge           完整清除探针和 fwq
  fwq info            查看仓库与联系方式
  fwq help            显示帮助
EOF
}

download_file() {
  url="$1"
  destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --connect-timeout 15 --max-time 180 --retry 2 --retry-delay 2 -o "$destination" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -T 30 -t 3 -O "$destination" "$url"
  else
    return 127
  fi
}

install_dependencies() {
  missing=""
  for command_name in curl sha256sum install useradd systemctl; do
    command -v "$command_name" >/dev/null 2>&1 || missing="${missing} ${command_name}"
  done
  if [ -z "$missing" ]; then
    info "系统依赖已满足，无需安装额外软件包。"
    return
  fi

  info "缺少依赖:${missing}，正在使用系统包管理器安装。"
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends ca-certificates curl coreutils passwd
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y ca-certificates curl coreutils shadow-utils
  elif command -v yum >/dev/null 2>&1; then
    yum install -y ca-certificates curl coreutils shadow-utils
  else
    die "未识别到受支持的包管理器，请先安装 curl、coreutils、shadow-utils/passwd 和 systemd。"
  fi

  for command_name in curl sha256sum install useradd systemctl; do
    command -v "$command_name" >/dev/null 2>&1 || die "依赖安装后仍找不到 ${command_name}。"
  done
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) die "暂不支持当前架构：$(uname -m)。仅支持 Linux amd64 和 arm64。" ;;
  esac
}

verify_checksum() {
  file_path="$1"
  expected="$2"
  actual="$(sha256sum "$file_path" | awk '{print $1}')"
  [ "$actual" = "$expected" ] || die "SHA-256 校验失败，已停止安装。"
}

obtain_binary() {
  arch="$1"
  destination="$2"
  if [ -n "$LOCAL_BINARY" ]; then
    [ -f "$LOCAL_BINARY" ] || die "找不到本地二进制：${LOCAL_BINARY}"
    cp "$LOCAL_BINARY" "$destination"
    if [ -n "$LOCAL_CHECKSUM" ]; then
      verify_checksum "$destination" "$LOCAL_CHECKSUM"
    else
      warn "本地测试二进制未提供 SHA-256；公开安装必须使用带校验的发布资产。"
    fi
    return
  fi

  asset="dimeng-monitor-agent-linux-${arch}"
  checksum_file="${TEMP_ROOT}/${asset}.sha256"
  bases="${DOWNLOAD_BASE:-} ${GITHUB_RELEASE_BASE} ${GITHUB_RAW_BASE} ${GITEE_RAW_BASE}"
  for base in $bases; do
    [ -n "$base" ] || continue
    info "尝试从 ${base} 下载 ${asset}。"
    if download_file "${base}/${asset}" "$destination" && download_file "${base}/${asset}.sha256" "$checksum_file"; then
      expected="$(awk 'NF {print $1; exit}' "$checksum_file")"
      [ -n "$expected" ] || continue
      verify_checksum "$destination" "$expected"
      return
    fi
  done
  die "无法下载并校验 ${asset}。请检查网络或发布资产。"
}

validate_endpoint() {
  case "$ENDPOINT" in
    https://*) ;;
    http://127.0.0.1*|http://localhost*)
      [ "${DIMENG_ALLOW_INSECURE_LOCAL:-0}" = "1" ] || die "正式安装只允许 HTTPS API 地址。"
      ;;
    *) die "API 地址必须使用 HTTPS。" ;;
  esac
  case "$ENDPOINT" in *[![:graph:]]*) die "API 地址包含非法空白字符。" ;; esac
}

read_existing_endpoint() {
  [ -f "$ENV_PATH" ] || return 0
  sed -n 's/^DIMENG_ENDPOINT=//p' "$ENV_PATH" | head -n 1
}

prepare_manager_script() {
  manager_temp="${TEMP_ROOT}/install.sh"
  source_name="$(basename "$0")"
  case "$source_name" in
    sh|dash|bash|zsh|-*)
      info "通过管道执行，正在下载管理脚本副本。"
      download_file "$INSTALLER_URL" "$manager_temp" || die "无法下载 fwq 管理脚本。"
      ;;
    *)
      [ -r "$0" ] || die "无法读取安装脚本自身，请先下载脚本文件再执行。"
      cp "$0" "$manager_temp"
      ;;
  esac
  install -d -m 0755 "$MANAGER_DIR"
  install -m 0755 "$manager_temp" "$MANAGER_SCRIPT"
  ln -sfn "$MANAGER_SCRIPT" "$MANAGER_PATH"
}

write_systemd_unit() {
  unit_temp="${TEMP_ROOT}/${SERVICE_NAME}.service"
  cat >"$unit_temp" <<EOF
[Unit]
Description=DiMeng Monitor Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${AGENT_USER}
Group=${AGENT_USER}
EnvironmentFile=${ENV_PATH}
ExecStart=${BIN_PATH} --endpoint \${DIMENG_ENDPOINT} --claim-token-file ${CLAIM_TOKEN_PATH}
Restart=always
RestartSec=10
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectHome=yes
ProtectSystem=strict
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=${STATE_DIR}
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
MemoryMax=64M
CPUQuota=20%

[Install]
WantedBy=multi-user.target
EOF
  install -m 0644 "$unit_temp" "$UNIT_PATH"
}

install_agent() {
  require_root
  [ "$(uname -s)" = "Linux" ] || die "当前安装器只支持 Linux。"
  [ -d /run/systemd/system ] || die "当前系统未使用 systemd，暂不支持自动安装。"

  accept_agreement
  install_dependencies

  if [ -z "$ENDPOINT" ]; then
    ENDPOINT="$(read_existing_endpoint)"
  fi
  [ -n "$ENDPOINT" ] || die "首次安装必须提供 --endpoint。"
  validate_endpoint
  TEMP_ROOT="$(mktemp -d)"
  arch="$(detect_arch)"
  staged_binary="${TEMP_ROOT}/dimeng-monitor-agent"
  obtain_binary "$arch" "$staged_binary"
  chmod 0755 "$staged_binary"
  "$staged_binary" --once >/dev/null || die "二进制自检失败。"

  if ! id -u "$AGENT_USER" >/dev/null 2>&1; then
    nologin_shell="$(command -v nologin || true)"
    [ -n "$nologin_shell" ] || nologin_shell="/usr/sbin/nologin"
    useradd --system --home "$STATE_DIR" --shell "$nologin_shell" "$AGENT_USER"
  fi

  install -d -m 0750 "$CONFIG_DIR"
  install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 "$STATE_DIR"
  install -m 0755 "$staged_binary" "$BIN_PATH"
  printf 'DIMENG_ENDPOINT=%s\n' "$ENDPOINT" >"$ENV_PATH"
  chmod 0600 "$ENV_PATH"
  if [ ! -s "$SESSION_TOKEN_PATH" ] && [ -n "$CLAIM_TOKEN" ]; then
    printf '%s\n' "$CLAIM_TOKEN" >"$CLAIM_TOKEN_PATH"
    chown "$AGENT_USER:$AGENT_USER" "$CLAIM_TOKEN_PATH"
    chmod 0600 "$CLAIM_TOKEN_PATH"
  else
    rm -f "$CLAIM_TOKEN_PATH"
  fi
  printf 'agreement=%s\naccepted_at=%s\n' "$AGREEMENT_VERSION" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >"${CONFIG_DIR}/agreement.accepted"
  chmod 0644 "${CONFIG_DIR}/agreement.accepted"

  write_systemd_unit
  prepare_manager_script
  systemctl daemon-reload
  if [ "$NO_START" = "1" ]; then
    systemctl enable "$SERVICE_NAME"
    info "探针已安装并设为开机启动，本次按参数要求未启动。"
  else
    systemctl enable --now "$SERVICE_NAME"
    wait_for_enrollment
    info "探针已安装并启动。"
  fi
  info "管理命令：fwq"
  info "状态检查：fwq status"
  info "卸载命令：fwq uninstall"
}

install_main() {
  ENDPOINT="${DIMENG_ENDPOINT:-}"
  CLAIM_TOKEN="${DIMENG_CLAIM_TOKEN:-}"
  LOCAL_BINARY=""
  LOCAL_CHECKSUM=""
  DOWNLOAD_BASE="${DIMENG_DOWNLOAD_BASE:-}"
  ACCEPT_AGREEMENT="${DIMENG_ACCEPT_AGREEMENT:-0}"
  NO_START=0

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --endpoint) [ "$#" -ge 2 ] || die "--endpoint 缺少参数"; ENDPOINT="$2"; shift 2 ;;
      --claim-token) [ "$#" -ge 2 ] || die "--claim-token 缺少参数"; CLAIM_TOKEN="$2"; shift 2 ;;
      --binary) [ "$#" -ge 2 ] || die "--binary 缺少参数"; LOCAL_BINARY="$2"; shift 2 ;;
      --checksum) [ "$#" -ge 2 ] || die "--checksum 缺少参数"; LOCAL_CHECKSUM="$2"; shift 2 ;;
      --download-base) [ "$#" -ge 2 ] || die "--download-base 缺少参数"; DOWNLOAD_BASE="$2"; shift 2 ;;
      --accept-agreement) ACCEPT_AGREEMENT=1; shift ;;
      --no-start) NO_START=1; shift ;;
      -h|--help) install_usage; exit 0 ;;
      *) die "未知参数：$1" ;;
    esac
  done
  install_agent
}

confirm_removal() {
  prompt="$1"
  force="$2"
  [ "$force" = "1" ] && return
  has_tty || die "非交互卸载请添加 --yes。"
  printf '%s 输入“确认”继续：' "$prompt" >/dev/tty
  answer=""
  IFS= read -r answer </dev/tty || true
  [ "$answer" = "确认" ] || die "操作已取消。"
}

uninstall_agent() {
  require_root
  full_purge="$1"
  force="$2"
  if [ "$full_purge" = "1" ]; then
    confirm_removal "将完整删除滴萌探针、身份凭据和 fwq。" "$force"
  else
    confirm_removal "将删除滴萌探针、服务和本地身份凭据，保留 fwq。" "$force"
  fi

  systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
  rm -f "$UNIT_PATH" "$BIN_PATH"
  rm -rf "$CONFIG_DIR" "$STATE_DIR"
  systemctl daemon-reload
  systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
  info "滴萌探针已卸载；没有删除任何系统依赖。"

  if [ "$full_purge" = "1" ]; then
    rm -f "$MANAGER_PATH"
    rm -rf "$MANAGER_DIR"
    if id -u "$AGENT_USER" >/dev/null 2>&1; then
      userdel "$AGENT_USER" >/dev/null 2>&1 || true
    fi
    info "fwq 管理命令和专用系统用户已清除。"
  else
    info "可运行 fwq install 重新安装。"
  fi
}

manager_status() {
  print_project_info
  if [ ! -x "$BIN_PATH" ]; then
    printf '\n状态：未安装\n'
    return
  fi
  printf '\n二进制：%s\n' "$BIN_PATH"
  printf '服务状态：%s\n' "$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || true)"
  printf '开机启动：%s\n' "$(systemctl is-enabled "$SERVICE_NAME" 2>/dev/null || true)"
  printf '本地身份：%s\n' "$([ -s "$SESSION_TOKEN_PATH" ] && printf '已注册' || printf '等待首次注册')"
}

interactive_install() {
  has_tty || die "交互安装需要终端；非交互环境请使用 fwq install --endpoint ...。"
  existing_endpoint="$(read_existing_endpoint)"
  if [ -n "$existing_endpoint" ]; then
    printf 'API 地址 [%s]：' "$existing_endpoint" >/dev/tty
  else
    printf 'API 地址（例如 https://api.example.com）：' >/dev/tty
  fi
  endpoint_input=""
  IFS= read -r endpoint_input </dev/tty || true
  [ -n "$endpoint_input" ] || endpoint_input="$existing_endpoint"

  install_main --endpoint "$endpoint_input"
}

wait_for_enrollment() {
  attempts=0
  while [ "$attempts" -lt 20 ]; do
    if [ -s "$SESSION_TOKEN_PATH" ]; then
      info "首次注册成功。"
      printf '\n'
      "$BIN_PATH" --state-dir "$STATE_DIR" --show-enrollment || true
      printf '\n请在滴萌客户端的“添加服务器”页面输入上面的公网 IP 和绑定码。\n'
      return 0
    fi
    if ! systemctl is-active --quiet "$SERVICE_NAME" && [ "$attempts" -ge 2 ]; then
      break
    fi
    attempts=$((attempts + 1))
    sleep 1
  done

  warn "首次注册没有成功，正在停止服务，避免持续重试。"
  systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
  journalctl -u "$SERVICE_NAME" -n 12 --no-pager >&2 || true
  die "请确认 API 注册接口和服务器出站网络后，运行 fwq install 重试。"
}

manager_menu() {
  has_tty || { manager_usage; return; }
  while :; do
    cat >/dev/tty <<'EOF'

滴萌服务器探针管理
  1. 安装 / 更新
  2. 查看状态
  3. 查看日志
  4. 重启服务
  5. 卸载探针（保留 fwq）
  6. 完整清除
  7. 项目与联系方式
  0. 退出
EOF
    printf '请选择：' >/dev/tty
    choice=""
    IFS= read -r choice </dev/tty || return
    case "$choice" in
      1) interactive_install ;;
      2) manager_status ;;
      3) journalctl -u "$SERVICE_NAME" -n 100 --no-pager ;;
      4) require_root; systemctl restart "$SERVICE_NAME"; manager_status ;;
      5) uninstall_agent 0 0 ;;
      6) uninstall_agent 1 0; return ;;
      7) print_project_info ;;
      0) return ;;
      *) warn "无效选择。" ;;
    esac
  done
}

manager_main() {
  command_name="${1:-menu}"
  [ "$#" -eq 0 ] || shift
  case "$command_name" in
    menu) manager_menu ;;
    install|update) install_main "$@" ;;
    status) manager_status ;;
    logs) journalctl -u "$SERVICE_NAME" -n 100 --no-pager "$@" ;;
    restart) require_root; systemctl restart "$SERVICE_NAME"; manager_status ;;
    uninstall)
      force=0
      [ "${1:-}" = "--yes" ] && force=1
      uninstall_agent 0 "$force"
      ;;
    purge)
      force=0
      [ "${1:-}" = "--yes" ] && force=1
      uninstall_agent 1 "$force"
      ;;
    info) print_project_info ;;
    help|-h|--help) manager_usage ;;
    *) manager_usage; die "未知 fwq 命令：${command_name}" ;;
  esac
}

case "$(basename "$0")" in
  fwq) manager_main "$@" ;;
  *) install_main "$@" ;;
esac
