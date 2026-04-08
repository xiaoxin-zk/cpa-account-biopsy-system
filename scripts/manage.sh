#!/usr/bin/env bash
set -euo pipefail

APP_NAME="cpa-account-biopsy-system"
INSTALL_DIR="${CPA_INSTALL_DIR:-/opt/${APP_NAME}}"
REPO_URL="${CPA_REPO_URL:-https://github.com/xiaoxin-zk/cpa-account-biopsy-system.git}"
REPO_REF="${CPA_REPO_REF:-main}"
ENV_FILE="$INSTALL_DIR/.env"
TEST_MODE="${CPA_TEST_MODE:-0}"

log() { printf '[%s] %s\n' "$APP_NAME" "$*"; }
die() { log "$*"; exit 1; }

has_cmd() { command -v "$1" >/dev/null 2>&1; }

run_cmd() {
  if [ "$TEST_MODE" = "1" ]; then
    log "[TEST MODE] $*"
    return 0
  fi
  "$@"
}

ensure_env_file() {
  mkdir -p "$INSTALL_DIR"
  if [ ! -f "$ENV_FILE" ] && [ -f "$INSTALL_DIR/.env.example" ]; then
    cp "$INSTALL_DIR/.env.example" "$ENV_FILE"
  fi
}

load_existing_env() {
  if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
  fi
}

upsert_env() {
  local key="$1"
  local value="$2"
  [ -n "$value" ] || return 0
  ensure_env_file
  if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
    sed -i "s#^${key}=.*#${key}=${value}#" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

prompt_if_empty() {
  local var_name="$1"
  local prompt_text="$2"
  local current_value="${!var_name:-}"
  if [ -n "$current_value" ]; then
    return 0
  fi
  if [ "$TEST_MODE" = "1" ]; then
    case "$var_name" in
      CPA_AUTH_DIR) current_value="/tmp/cpa-test/auths" ;;
      CPA_CONFIG_PATH) current_value="/tmp/cpa-test/config.yaml" ;;
      CPA_MANAGEMENT_URL) current_value="http://127.0.0.1:8317" ;;
      CPA_MANAGEMENT_KEY) current_value="test-management-password" ;;
      *) current_value="test-value" ;;
    esac
    log "[TEST MODE] auto-filled $var_name=$current_value"
    eval "$var_name=\"$current_value\""
    return 0
  fi
  read -r -p "$prompt_text: " current_value
  eval "$var_name=\"$current_value\""
}

detect_container_name() {
  if ! has_cmd docker; then
    return 0
  fi
  docker ps --format '{{.Names}}' 2>/dev/null | grep -E 'cliproxyapi|cli-proxy-api' | head -n 1 || true
}

detect_from_container() {
  local name="$1"
  [ -n "$name" ] || return 0
  local inspect envs
  inspect="$(docker inspect "$name" --format '{{range .Mounts}}{{println .Destination "=" .Source}}{{end}}' 2>/dev/null || true)"
  envs="$(docker inspect "$name" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null || true)"
  if [ -z "${CPA_AUTH_DIR:-}" ]; then
    CPA_AUTH_DIR="$(printf '%s' "$inspect" | awk -F' = ' '$1=="/root/.cli-proxy-api"{print $2}' | head -n 1)"
  fi
  if [ -z "${CPA_CONFIG_PATH:-}" ]; then
    CPA_CONFIG_PATH="$(printf '%s' "$inspect" | awk -F' = ' '$2 ~ /config.yaml$/{print $2}' | head -n 1)"
  fi
  if [ -z "${CPA_MANAGEMENT_KEY:-}" ]; then
    CPA_MANAGEMENT_KEY="$(printf '%s' "$envs" | awk -F= '$1=="MANAGEMENT_PASSWORD"{print substr($0,index($0,"=")+1)}' | head -n 1)"
  fi
}

detect_common_paths() {
  for p in /root/.cli-proxy-api /opt/cliproxy/auths /opt/CLIProxyAPI/auths; do
    if [ -z "${CPA_AUTH_DIR:-}" ] && [ -d "$p" ]; then CPA_AUTH_DIR="$p"; fi
  done
  for p in /CLIProxyAPI/config.yaml /opt/cliproxy/config.yaml /opt/CLIProxyAPI/config.yaml; do
    if [ -z "${CPA_CONFIG_PATH:-}" ] && [ -f "$p" ]; then CPA_CONFIG_PATH="$p"; fi
  done
  if [ -z "${CPA_MANAGEMENT_URL:-}" ]; then
    CPA_MANAGEMENT_URL="http://127.0.0.1:8317"
  fi
}

bootstrap_repo() {
  if [ -d "$INSTALL_DIR/.git" ]; then
    run_cmd git -C "$INSTALL_DIR" fetch --tags origin
    run_cmd git -C "$INSTALL_DIR" checkout "$REPO_REF"
    run_cmd git -C "$INSTALL_DIR" pull --ff-only origin "$REPO_REF"
  else
    rm -rf "$INSTALL_DIR"
    run_cmd git clone --branch "$REPO_REF" "$REPO_URL" "$INSTALL_DIR"
  fi
  ensure_env_file
}

prepare_config() {
  load_existing_env
  local container_name
  container_name="$(detect_container_name)"
  detect_from_container "$container_name"
  detect_common_paths

  prompt_if_empty CPA_AUTH_DIR "未自动找到 CLIProxyAPI 的账号目录，请输入 auths 目录路径"
  prompt_if_empty CPA_CONFIG_PATH "未自动找到 CLIProxyAPI 的配置文件，请输入 config.yaml 路径"
  prompt_if_empty CPA_MANAGEMENT_URL "未自动获取到 CLIProxyAPI 管理地址，请输入管理地址（例如 http://127.0.0.1:8317）"
  prompt_if_empty CPA_MANAGEMENT_KEY "未自动获取到 CLIProxyAPI 管理密码，请输入你的 CLIProxyAPI 管理密码"

  upsert_env CPA_AUTH_DIR "${CPA_AUTH_DIR:-}"
  upsert_env CPA_CONFIG_PATH "${CPA_CONFIG_PATH:-}"
  upsert_env CPA_MANAGEMENT_URL "${CPA_MANAGEMENT_URL:-}"
  upsert_env CPA_MANAGEMENT_KEY "${CPA_MANAGEMENT_KEY:-}"
  upsert_env CPA_LISTEN_ADDR "${CPA_LISTEN_ADDR:-:18317}"
  upsert_env CPA_SNAPSHOT_INTERVAL "${CPA_SNAPSHOT_INTERVAL:-5m}"
  upsert_env CPA_PROBE_INTERVAL "${CPA_PROBE_INTERVAL:-1h}"
}

install_or_update() {
  log "开始安装/更新 CPA Account Biopsy System"
  bootstrap_repo
  prepare_config
  cd "$INSTALL_DIR"
  run_cmd docker compose --env-file "$ENV_FILE" up -d --build
  local listen_addr="${CPA_LISTEN_ADDR:-:18317}"
  log "完成"
  log "访问地址: http://SERVER_IP${listen_addr#:}"
  log "首次访问将进入 Web 初始化页面设置仪表台密码"
}

restart_service() {
  [ -d "$INSTALL_DIR" ] || die "尚未安装，无法重启"
  cd "$INSTALL_DIR"
  run_cmd docker compose --env-file "$ENV_FILE" restart
  log "已重启 sidecar 服务"
}

show_status() {
  if [ ! -d "$INSTALL_DIR" ]; then
    log "当前状态: 未安装"
    return 0
  fi
  load_existing_env
  log "当前状态: 已安装"
  log "安装目录: $INSTALL_DIR"
  log "访问地址: http://SERVER_IP${CPA_LISTEN_ADDR#:}"
  if has_cmd docker; then
    (cd "$INSTALL_DIR" && docker compose --env-file "$ENV_FILE" ps) || true
  else
    log "docker 命令不可用，无法显示容器状态"
  fi
}

uninstall_service() {
  [ -d "$INSTALL_DIR" ] || { log "未安装，无需卸载"; return 0; }
  if [ "$TEST_MODE" != "1" ]; then
    read -r -p "确认卸载 CPA Account Biopsy System? [y/N]: " answer
    case "$answer" in
      y|Y|yes|YES) ;;
      *) log "已取消卸载"; return 0 ;;
    esac
  fi
  cd "$INSTALL_DIR"
  run_cmd docker compose --env-file "$ENV_FILE" down
  cd /
  rm -rf "$INSTALL_DIR"
  log "卸载完成，不影响主项目 CLIProxyAPI"
}

print_menu() {
  cat <<'EOF'

CPA Account Biopsy System
1) 安装
2) 更新
3) 卸载
4) 重启服务
5) 查看状态
6) 退出

EOF
}

run_menu() {
  while true; do
    print_menu
    if [ "$TEST_MODE" = "1" ] && [ -n "${CPA_TEST_CHOICE:-}" ]; then
      choice="$CPA_TEST_CHOICE"
      log "[TEST MODE] selected $choice"
    else
      read -r -p "请选择操作 [1-6]: " choice
    fi
    case "$choice" in
      1) install_or_update ;;
      2) install_or_update ;;
      3) uninstall_service ;;
      4) restart_service ;;
      5) show_status ;;
      6) log "已退出"; break ;;
      *) log "无效选项，请重新输入" ;;
    esac
    if [ "$TEST_MODE" = "1" ]; then
      break
    fi
  done
}

case "${1:-}" in
  --install) install_or_update ;;
  --update) install_or_update ;;
  --restart) restart_service ;;
  --status) show_status ;;
  --uninstall) uninstall_service ;;
  *) run_menu ;;
esac
