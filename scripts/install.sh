#!/usr/bin/env bash
set -euo pipefail

APP_NAME="cpa-account-biopsy-system"
INSTALL_DIR="${CPA_INSTALL_DIR:-/opt/${APP_NAME}}"
REPO_URL="${CPA_REPO_URL:-https://github.com/xiaoxin-zk/cpa-account-biopsy-system.git}"
REPO_REF="${CPA_REPO_REF:-main}"
ENV_FILE="$INSTALL_DIR/.env"

log() { printf '[%s] %s\n' "$APP_NAME" "$*"; }
die() { log "$*"; exit 1; }

upsert_env() {
  local key="$1"
  local value="$2"
  [ -n "$value" ] || return 0
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
  read -r -p "$prompt_text: " current_value
  eval "$var_name=\"$current_value\""
}

detect_container_name() {
  docker ps --format '{{.Names}}' 2>/dev/null | grep -E 'cliproxyapi|cli-proxy-api' | head -n 1 || true
}

detect_from_container() {
  local name="$1"
  [ -n "$name" ] || return 0
  local inspect
  inspect="$(docker inspect "$name" --format '{{range .Mounts}}{{println .Destination "=" .Source}}{{end}}' 2>/dev/null || true)"
  if [ -z "${CPA_AUTH_DIR:-}" ]; then
    CPA_AUTH_DIR="$(printf '%s' "$inspect" | awk -F' = ' '$1=="/root/.cli-proxy-api"{print $2}' | head -n 1)"
  fi
  if [ -z "${CPA_CONFIG_PATH:-}" ]; then
    CPA_CONFIG_PATH="$(printf '%s' "$inspect" | awk -F' = ' '$1 ~ /config.yaml$/{print $2}' | head -n 1)"
  fi
  if [ -z "${CPA_MANAGEMENT_KEY:-}" ]; then
    CPA_MANAGEMENT_KEY="$(docker inspect "$name" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null | awk -F= '$1=="MANAGEMENT_PASSWORD"{print substr($0,index($0,"=")+1)}' | head -n 1)"
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

load_existing_env() {
  if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$ENV_FILE"
    set +a
  fi
}

install_or_update() {
  mkdir -p "$INSTALL_DIR"
  if [ -d "$INSTALL_DIR/.git" ]; then
    log "更新项目: $INSTALL_DIR"
    git -C "$INSTALL_DIR" fetch --tags origin
    git -C "$INSTALL_DIR" checkout "$REPO_REF"
    git -C "$INSTALL_DIR" pull --ff-only origin "$REPO_REF"
  else
    rm -rf "$INSTALL_DIR"
    log "拉取项目: $REPO_URL"
    git clone --branch "$REPO_REF" "$REPO_URL" "$INSTALL_DIR"
  fi

  if [ ! -f "$ENV_FILE" ]; then
    cp "$INSTALL_DIR/.env.example" "$ENV_FILE"
  fi

  load_existing_env
  local container_name
  container_name="$(detect_container_name)"
  detect_from_container "$container_name"
  detect_common_paths

  prompt_if_empty CPA_AUTH_DIR "未找到 CLIProxyAPI 的 auths 目录，请输入路径"
  prompt_if_empty CPA_CONFIG_PATH "未找到 CLIProxyAPI 的 config.yaml，请输入路径"
  prompt_if_empty CPA_MANAGEMENT_URL "未找到主项目 management 地址，请输入（例如 http://127.0.0.1:8317）"
  prompt_if_empty CPA_MANAGEMENT_KEY "未自动发现 management 密钥，请输入 MANAGEMENT_PASSWORD 明文"

  upsert_env CPA_AUTH_DIR "${CPA_AUTH_DIR:-}"
  upsert_env CPA_CONFIG_PATH "${CPA_CONFIG_PATH:-}"
  upsert_env CPA_MANAGEMENT_URL "${CPA_MANAGEMENT_URL:-}"
  upsert_env CPA_MANAGEMENT_KEY "${CPA_MANAGEMENT_KEY:-}"
  upsert_env CPA_LISTEN_ADDR "${CPA_LISTEN_ADDR:-:18317}"
  upsert_env CPA_SNAPSHOT_INTERVAL "${CPA_SNAPSHOT_INTERVAL:-5m}"
  upsert_env CPA_PROBE_INTERVAL "${CPA_PROBE_INTERVAL:-1h}"

  cd "$INSTALL_DIR"
  docker compose --env-file "$ENV_FILE" up -d --build

  local listen_addr="${CPA_LISTEN_ADDR:-:18317}"
  log "安装/更新完成"
  log "访问地址: http://SERVER_IP${listen_addr#:}"
  log "首次访问将进入 Web 初始化页面设置仪表台密码"
  log "健康检查: curl http://127.0.0.1${listen_addr#:}/healthz"
}

uninstall() {
  if [ -d "$INSTALL_DIR" ]; then
    cd "$INSTALL_DIR"
    docker compose --env-file "$ENV_FILE" down || true
    cd /
    rm -rf "$INSTALL_DIR"
  fi
  log "卸载完成，不影响主项目 CLIProxyAPI"
}

case "${1:-}" in
  --uninstall)
    uninstall
    ;;
  *)
    install_or_update
    ;;
esac
