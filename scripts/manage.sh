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

http_get_code() {
  local url="$1"
  shift || true
  if [ "$TEST_MODE" = "1" ]; then
    printf '200'
    return 0
  fi
  curl -fsS -o /dev/null -w '%{http_code}' --max-time 5 "$@" "$url"
}

format_access_addrs() {
  local listen_addr="$1"
  local port="${listen_addr#:}"
  if [ -z "$port" ] || [ "$port" = "$listen_addr" ]; then
    port="18317"
  fi
  local localhost="http://127.0.0.1:${port}"
  local lan_ip=""
  local public_ip=""

  if has_cmd ip; then
    lan_ip="$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1;i<=NF;i++) if ($i=="src") print $(i+1)}' | head -n 1)"
  fi
  if [ -z "$lan_ip" ] && has_cmd hostname; then
    lan_ip="$(hostname -I 2>/dev/null | awk '{print $1}' | head -n 1)"
  fi
  if [ -z "$lan_ip" ] && has_cmd ifconfig; then
    lan_ip="$(ifconfig 2>/dev/null | awk '/inet / && $2 != "127.0.0.1" {print $2; exit}')"
  fi

  if [ "$TEST_MODE" = "1" ]; then
    public_ip="203.0.113.10"
  elif has_cmd curl; then
    public_ip="$(curl -fsSL --max-time 3 https://api.ipify.org 2>/dev/null || true)"
  fi

  printf '本机访问地址: %s' "$localhost"
  if [ -n "$lan_ip" ] && [ "$lan_ip" != "127.0.0.1" ]; then
    printf '\n[%s] 局域网访问地址: http://%s:%s' "$APP_NAME" "$lan_ip" "$port"
  fi
  if [ -n "$public_ip" ]; then
    printf '\n[%s] 公网访问地址: http://%s:%s' "$APP_NAME" "$public_ip" "$port"
  fi
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
  docker ps --format '{{.Names}} {{.Image}} {{.Ports}}' 2>/dev/null | grep -Ei 'cli-proxy-api|cliproxyapi' | awk '{print $1}' | head -n 1 || true
}

detect_from_container() {
  local name="$1"
  [ -n "$name" ] || return 0
  local inspect envs binds
  log "已发现主项目容器: $name"
  inspect="$(docker inspect "$name" --format '{{range .Mounts}}{{println .Destination "=" .Source}}{{end}}' 2>/dev/null || true)"
  envs="$(docker inspect "$name" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null || true)"
  binds="$(docker inspect "$name" --format '{{range .HostConfig.Binds}}{{println .}}{{end}}' 2>/dev/null || true)"
  if [ -z "${CPA_AUTH_DIR:-}" ]; then
    CPA_AUTH_DIR="$(printf '%s' "$inspect" | awk -F' = ' '$1=="/root/.cli-proxy-api"{print $2}' | head -n 1)"
  fi
  if [ -z "${CPA_AUTH_DIR:-}" ]; then
    CPA_AUTH_DIR="$(printf '%s' "$binds" | awk -F: '/\.cli-proxy-api/ {print $1}' | head -n 1)"
  fi
  if [ -z "${CPA_CONFIG_PATH:-}" ]; then
    CPA_CONFIG_PATH="$(printf '%s' "$inspect" | awk -F' = ' '$2 ~ /config.yaml$/{print $2}' | head -n 1)"
  fi
  if [ -z "${CPA_CONFIG_PATH:-}" ]; then
    CPA_CONFIG_PATH="$(printf '%s' "$binds" | awk -F: '/config.yaml/ {print $1}' | head -n 1)"
  fi
  if [ -z "${CPA_MANAGEMENT_KEY:-}" ]; then
    CPA_MANAGEMENT_KEY="$(printf '%s' "$envs" | awk -F= '$1=="MANAGEMENT_PASSWORD"{print substr($0,index($0,"=")+1)}' | head -n 1)"
  fi
  if [ -z "${CPA_MANAGEMENT_URL:-}" ]; then
    if printf '%s' "$envs" | grep -q '^PORT='; then
      local detected_port
      detected_port="$(printf '%s' "$envs" | awk -F= '$1=="PORT"{print $2}' | head -n 1)"
      [ -n "$detected_port" ] && CPA_MANAGEMENT_URL="http://127.0.0.1:${detected_port}"
    fi
  fi

  detect_from_mount_directories "$inspect"
  detect_from_bind_directories "$binds"
}

scan_candidate_root() {
  local root="$1"
  [ -n "$root" ] || return 0
  [ -d "$root" ] || return 0
  log "正在从挂载目录尝试识别配置: $root"

  local config_candidates=(
    "$root/config.yaml"
    "$root/config/config.yaml"
    "$root/data/config.yaml"
    "$root/CLIProxyAPI/config.yaml"
  )
  local auth_candidates=(
    "$root/auths"
    "$root/.cli-proxy-api"
    "$root/data/auths"
    "$root/CLIProxyAPI/auths"
  )

  if [ -z "${CPA_CONFIG_PATH:-}" ]; then
    for candidate in "${config_candidates[@]}"; do
      if [ -f "$candidate" ]; then
        CPA_CONFIG_PATH="$candidate"
        log "自动识别到配置文件: $CPA_CONFIG_PATH"
        break
      fi
    done
  fi

  if [ -z "${CPA_AUTH_DIR:-}" ]; then
    for candidate in "${auth_candidates[@]}"; do
      if [ -d "$candidate" ]; then
        CPA_AUTH_DIR="$candidate"
        log "自动识别到账号目录: $CPA_AUTH_DIR"
        break
      fi
    done
  fi

  if [ -z "${CPA_CONFIG_PATH:-}" ] || [ -z "${CPA_AUTH_DIR:-}" ]; then
    local best_root=""
    for candidate_root in "$root" "$root/config" "$root/data" "$root/CLIProxyAPI"; do
      if [ -d "$candidate_root" ] && [ -f "$candidate_root/config.yaml" ] && { [ -d "$candidate_root/auths" ] || [ -d "$candidate_root/.cli-proxy-api" ]; }; then
        best_root="$candidate_root"
        break
      fi
    done
    if [ -n "$best_root" ]; then
      [ -z "${CPA_CONFIG_PATH:-}" ] && [ -f "$best_root/config.yaml" ] && CPA_CONFIG_PATH="$best_root/config.yaml"
      if [ -z "${CPA_AUTH_DIR:-}" ]; then
        [ -d "$best_root/auths" ] && CPA_AUTH_DIR="$best_root/auths"
        [ -z "${CPA_AUTH_DIR:-}" ] && [ -d "$best_root/.cli-proxy-api" ] && CPA_AUTH_DIR="$best_root/.cli-proxy-api"
      fi
      [ -n "${CPA_CONFIG_PATH:-}" ] && log "目录结构匹配到配置文件: $CPA_CONFIG_PATH"
      [ -n "${CPA_AUTH_DIR:-}" ] && log "目录结构匹配到账号目录: $CPA_AUTH_DIR"
    fi
  fi
}

detect_from_mount_directories() {
  local inspect="$1"
  [ -n "$inspect" ] || return 0
  while IFS= read -r line; do
    local src
    src="$(printf '%s' "$line" | awk -F' = ' '{print $2}')"
    [ -n "$src" ] || continue
    if [ -d "$src" ]; then
      scan_candidate_root "$src"
    fi
  done <<< "$inspect"
}

detect_from_bind_directories() {
  local binds="$1"
  [ -n "$binds" ] || return 0
  while IFS= read -r line; do
    local src
    src="$(printf '%s' "$line" | awk -F: '{print $1}')"
    [ -n "$src" ] || continue
    if [ -d "$src" ]; then
      scan_candidate_root "$src"
    fi
  done <<< "$binds"
}

detect_from_compose_files() {
  local candidates="/opt /root /home"
  local compose_file
  for base in $candidates; do
    while IFS= read -r -d '' compose_file; do
      if [ -z "${CPA_AUTH_DIR:-}" ]; then
        CPA_AUTH_DIR="$(grep -E '/root/.cli-proxy-api|/\.cli-proxy-api|auths' "$compose_file" 2>/dev/null | sed -E 's/^[[:space:]-]*([^:]+):.*/\1/' | head -n 1 || true)"
      fi
      if [ -z "${CPA_CONFIG_PATH:-}" ]; then
        CPA_CONFIG_PATH="$(grep -E 'config.yaml' "$compose_file" 2>/dev/null | sed -E 's/^[[:space:]-]*([^:]+):.*/\1/' | head -n 1 || true)"
      fi
    done < <(find "$base" -maxdepth 4 \( -name docker-compose.yml -o -name compose.yml \) -print0 2>/dev/null)
  done
}

detect_common_paths() {
  for p in /root/.cli-proxy-api /opt/cliproxy/auths /opt/CLIProxyAPI/auths /opt/CLIProxyAPI/.cli-proxy-api /data/cli-proxy-api/auths /www/cli-proxy-api/auths; do
    if [ -z "${CPA_AUTH_DIR:-}" ] && [ -d "$p" ]; then CPA_AUTH_DIR="$p"; fi
  done
  for p in /CLIProxyAPI/config.yaml /opt/cliproxy/config.yaml /opt/CLIProxyAPI/config.yaml /root/CLIProxyAPI/config.yaml /data/cli-proxy-api/config.yaml /www/cli-proxy-api/config.yaml; do
    if [ -z "${CPA_CONFIG_PATH:-}" ] && [ -f "$p" ]; then CPA_CONFIG_PATH="$p"; fi
  done
  if [ -z "${CPA_MANAGEMENT_URL:-}" ]; then
    CPA_MANAGEMENT_URL="http://127.0.0.1:8317"
  fi
}

validate_host_inputs() {
  [ -n "${CPA_AUTH_DIR:-}" ] || die "auths 目录为空，无法安装"
  [ -d "$CPA_AUTH_DIR" ] || die "auths 目录不存在: $CPA_AUTH_DIR"
}

count_auth_files() {
  local dir="$1"
  find "$dir" -maxdepth 1 -type f -name '*.json' ! -name '.account-health-*' 2>/dev/null | wc -l | tr -d ' '
}

run_post_install_checks() {
  validate_host_inputs
  local auth_count listen_addr port sidecar_url sidecar_code mgmt_code bootstrap_json bootstrap_auth_count bootstrap_error
  auth_count="$(count_auth_files "$CPA_AUTH_DIR")"
  log "自检: auths 目录 $CPA_AUTH_DIR，检测到 $auth_count 个账号文件"
  if [ -n "${CPA_CONFIG_PATH:-}" ]; then
    log "自检: config.yaml 路径 $CPA_CONFIG_PATH"
  else
    log "自检: 当前未使用宿主机 config.yaml 挂载"
  fi

  if [ -n "${CPA_MANAGEMENT_KEY:-}" ] && has_cmd curl; then
    mgmt_code="$(http_get_code "$CPA_MANAGEMENT_URL/v0/management/auth-files/health" -H "Authorization: Bearer ${CPA_MANAGEMENT_KEY}")" || mgmt_code="000"
    [ "$mgmt_code" = "200" ] || die "management API 自检失败: ${CPA_MANAGEMENT_URL}/v0/management/auth-files/health 返回 $mgmt_code，请检查管理地址或管理密码"
    log "自检: management API 可用 -> $CPA_MANAGEMENT_URL"
  fi

  listen_addr="${CPA_LISTEN_ADDR:-:18317}"
  port="${listen_addr#:}"
  if [ -z "$port" ] || [ "$port" = "$listen_addr" ]; then
    port="18317"
  fi
  sidecar_url="http://127.0.0.1:${port}/healthz"
  if has_cmd curl; then
    sidecar_code="$(http_get_code "$sidecar_url")" || sidecar_code="000"
    [ "$sidecar_code" = "200" ] || die "sidecar 健康检查失败: $sidecar_url 返回 $sidecar_code"
    log "自检: sidecar 健康检查通过 -> $sidecar_url"

    bootstrap_json="$(curl -fsSL --max-time 5 "http://127.0.0.1:${port}/api/bootstrap-state" 2>/dev/null || true)"
    bootstrap_auth_count="$(printf '%s' "$bootstrap_json" | sed -n 's/.*"auth_count":\([0-9]\+\).*/\1/p' | head -n 1)"
    bootstrap_error="$(printf '%s' "$bootstrap_json" | sed -n 's/.*"last_error":"\([^"]*\)".*/\1/p' | head -n 1)"
    if [ -n "$bootstrap_error" ]; then
      log "自检: sidecar 已启动，但后端状态异常 -> $bootstrap_error"
    elif [ -n "$bootstrap_auth_count" ] && [ "$bootstrap_auth_count" -gt 0 ] 2>/dev/null; then
      log "自检: sidecar 已连接主项目，当前已读取 $bootstrap_auth_count 个账号"
    else
      log "自检: sidecar 已启动，但当前未读取到账号信息"
    fi
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
  if [ -z "${CPA_AUTH_DIR:-}" ] || [ -z "${CPA_CONFIG_PATH:-}" ] || [ -z "${CPA_MANAGEMENT_KEY:-}" ]; then
    detect_from_compose_files
  fi
  if [ -z "${CPA_AUTH_DIR:-}" ] || [ -z "${CPA_CONFIG_PATH:-}" ]; then
    detect_common_paths
  fi

  if [ -n "${CPA_AUTH_DIR:-}" ] && [ ! -d "$CPA_AUTH_DIR" ]; then
    log "已检测到旧的账号目录配置不可用：$CPA_AUTH_DIR"
    CPA_AUTH_DIR=""
  fi
  if [ -n "${CPA_CONFIG_PATH:-}" ] && [ ! -f "$CPA_CONFIG_PATH" ]; then
    log "已检测到旧的配置文件路径不可用：$CPA_CONFIG_PATH"
    CPA_CONFIG_PATH=""
  fi

  prompt_if_empty CPA_AUTH_DIR "未自动找到 CLIProxyAPI 的账号目录，请输入 auths 目录路径"
  prompt_if_empty CPA_MANAGEMENT_URL "未自动获取到 CLIProxyAPI 管理地址，请输入管理地址（例如 http://127.0.0.1:8317）"
  prompt_if_empty CPA_MANAGEMENT_KEY "未自动获取到 CLIProxyAPI 管理密码，请输入你的 CLIProxyAPI 管理密码"

  upsert_env CPA_AUTH_DIR "${CPA_AUTH_DIR:-}"
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
  validate_host_inputs
  cd "$INSTALL_DIR"
  run_cmd docker compose --env-file "$ENV_FILE" up -d --build
  run_post_install_checks
  local listen_addr="${CPA_LISTEN_ADDR:-:18317}"
  log "完成"
  log "$(format_access_addrs "$listen_addr")"
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
  validate_host_inputs
  log "当前状态: 已安装"
  log "安装目录: $INSTALL_DIR"
  log "$(format_access_addrs "${CPA_LISTEN_ADDR:-:18317}")"
  if has_cmd docker; then
    (cd "$INSTALL_DIR" && docker compose --env-file "$ENV_FILE" ps) || true
  else
    log "docker 命令不可用，无法显示容器状态"
  fi
  run_post_install_checks
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
