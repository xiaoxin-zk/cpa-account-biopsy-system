#!/usr/bin/env bash
set -euo pipefail

APP_NAME="cpa-account-biopsy-system"
INSTALL_DIR="${CPA_INSTALL_DIR:-/opt/${APP_NAME}}"
REPO_URL="${CPA_REPO_URL:-https://github.com/xiaoxin-zk/cpa-account-biopsy-system.git}"
REPO_REF="${CPA_REPO_REF:-main}"

log() { printf '[%s] %s\n' "$APP_NAME" "$*"; }
die() { log "$*"; exit 1; }

ensure_value() {
  local name="$1"
  local value="$2"
  [ -n "$value" ] || die "缺少必要配置: $name"
}

mkdir -p "$INSTALL_DIR"

if [ -d "$INSTALL_DIR/.git" ]; then
  log "更新仓库: $INSTALL_DIR"
  git -C "$INSTALL_DIR" fetch --tags origin
  git -C "$INSTALL_DIR" checkout "$REPO_REF"
  git -C "$INSTALL_DIR" pull --ff-only origin "$REPO_REF"
else
  rm -rf "$INSTALL_DIR"
  log "拉取仓库: $REPO_URL"
  git clone --branch "$REPO_REF" "$REPO_URL" "$INSTALL_DIR"
fi

ENV_FILE="$INSTALL_DIR/.env"
if [ ! -f "$ENV_FILE" ]; then
  cp "$INSTALL_DIR/.env.example" "$ENV_FILE"
fi

if [ -n "${CPA_MANAGEMENT_URL:-}" ]; then grep -q '^CPA_MANAGEMENT_URL=' "$ENV_FILE" && sed -i "s#^CPA_MANAGEMENT_URL=.*#CPA_MANAGEMENT_URL=${CPA_MANAGEMENT_URL}#" "$ENV_FILE" || printf '\nCPA_MANAGEMENT_URL=%s\n' "$CPA_MANAGEMENT_URL" >> "$ENV_FILE"; fi
if [ -n "${CPA_MANAGEMENT_KEY:-}" ]; then grep -q '^CPA_MANAGEMENT_KEY=' "$ENV_FILE" && sed -i "s#^CPA_MANAGEMENT_KEY=.*#CPA_MANAGEMENT_KEY=${CPA_MANAGEMENT_KEY}#" "$ENV_FILE" || printf '\nCPA_MANAGEMENT_KEY=%s\n' "$CPA_MANAGEMENT_KEY" >> "$ENV_FILE"; fi
if [ -n "${CPA_WEB_TOKEN:-}" ]; then grep -q '^CPA_WEB_TOKEN=' "$ENV_FILE" && sed -i "s#^CPA_WEB_TOKEN=.*#CPA_WEB_TOKEN=${CPA_WEB_TOKEN}#" "$ENV_FILE" || printf '\nCPA_WEB_TOKEN=%s\n' "$CPA_WEB_TOKEN" >> "$ENV_FILE"; fi
if [ -n "${CPA_AUTH_DIR:-}" ]; then grep -q '^CPA_AUTH_DIR=' "$ENV_FILE" && sed -i "s#^CPA_AUTH_DIR=.*#CPA_AUTH_DIR=${CPA_AUTH_DIR}#" "$ENV_FILE" || printf '\nCPA_AUTH_DIR=%s\n' "$CPA_AUTH_DIR" >> "$ENV_FILE"; fi
if [ -n "${CPA_CONFIG_PATH:-}" ]; then grep -q '^CPA_CONFIG_PATH=' "$ENV_FILE" && sed -i "s#^CPA_CONFIG_PATH=.*#CPA_CONFIG_PATH=${CPA_CONFIG_PATH}#" "$ENV_FILE" || printf '\nCPA_CONFIG_PATH=%s\n' "$CPA_CONFIG_PATH" >> "$ENV_FILE"; fi
if [ -n "${CPA_LISTEN_ADDR:-}" ]; then grep -q '^CPA_LISTEN_ADDR=' "$ENV_FILE" && sed -i "s#^CPA_LISTEN_ADDR=.*#CPA_LISTEN_ADDR=${CPA_LISTEN_ADDR}#" "$ENV_FILE" || printf '\nCPA_LISTEN_ADDR=%s\n' "$CPA_LISTEN_ADDR" >> "$ENV_FILE"; fi
if [ -n "${CPA_SNAPSHOT_INTERVAL:-}" ]; then grep -q '^CPA_SNAPSHOT_INTERVAL=' "$ENV_FILE" && sed -i "s#^CPA_SNAPSHOT_INTERVAL=.*#CPA_SNAPSHOT_INTERVAL=${CPA_SNAPSHOT_INTERVAL}#" "$ENV_FILE" || printf '\nCPA_SNAPSHOT_INTERVAL=%s\n' "$CPA_SNAPSHOT_INTERVAL" >> "$ENV_FILE"; fi
if [ -n "${CPA_PROBE_INTERVAL:-}" ]; then grep -q '^CPA_PROBE_INTERVAL=' "$ENV_FILE" && sed -i "s#^CPA_PROBE_INTERVAL=.*#CPA_PROBE_INTERVAL=${CPA_PROBE_INTERVAL}#" "$ENV_FILE" || printf '\nCPA_PROBE_INTERVAL=%s\n' "$CPA_PROBE_INTERVAL" >> "$ENV_FILE"; fi

source "$ENV_FILE"
ensure_value "CPA_MANAGEMENT_URL" "${CPA_MANAGEMENT_URL:-}"
ensure_value "CPA_MANAGEMENT_KEY" "${CPA_MANAGEMENT_KEY:-}"
ensure_value "CPA_AUTH_DIR" "${CPA_AUTH_DIR:-}"
ensure_value "CPA_CONFIG_PATH" "${CPA_CONFIG_PATH:-}"

cd "$INSTALL_DIR"
docker compose --env-file "$ENV_FILE" up -d --build

PORT_DISPLAY="${CPA_LISTEN_ADDR:-:18317}"
log "部署完成"
log "访问地址: http://SERVER_IP${PORT_DISPLAY#:}"
log "健康检查: curl http://127.0.0.1${PORT_DISPLAY#:}/healthz"
