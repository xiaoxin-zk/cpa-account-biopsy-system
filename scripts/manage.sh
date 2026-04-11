#!/usr/bin/env bash
set -euo pipefail

APP_NAME="cpa-account-biopsy-system"

is_windows_platform() {
  case "${CPA_PLATFORM_OVERRIDE:-}" in
    windows) return 0 ;;
    linux) return 1 ;;
  esac
  case "${OS:-}:$(uname -s 2>/dev/null || true)" in
    Windows_NT:*|*:MINGW*|*:MSYS*|*:CYGWIN*) return 0 ;;
  esac
  return 1
}

default_install_dir() {
  if is_windows_platform; then
    printf '%s/%s' "${USERPROFILE:-${HOME:-$(pwd)}}" "$APP_NAME"
  else
    printf '/opt/%s' "$APP_NAME"
  fi
}

INSTALL_DIR="${CPA_INSTALL_DIR:-$(default_install_dir)}"
REPO_URL="${CPA_REPO_URL:-https://github.com/xiaoxin-zk/cpa-account-biopsy-system.git}"
REPO_REF="${CPA_REPO_REF:-main}"
ENV_FILE="$INSTALL_DIR/.env"
TEST_MODE="${CPA_TEST_MODE:-0}"
DEFAULT_AUTH_SUBDIR="data/auths"
CPA_AUTH_CONTAINER_DIR="${CPA_AUTH_CONTAINER_DIR:-/data/auths}"
CPA_WEB_PORT="${CPA_WEB_PORT:-18317}"

log() { printf '[%s] %s\n' "$APP_NAME" "$*"; }
die() { log "$*"; exit 1; }

has_cmd() { command -v "$1" >/dev/null 2>&1; }

compose_args() {
  local args=(--env-file "$ENV_FILE" -f docker-compose.yml)
  if is_windows_platform; then
    args+=(-f docker-compose.windows.yml)
  else
    args+=(-f docker-compose.linux.yml)
  fi
  printf '%s\n' "${args[@]}"
}

run_compose() {
  mapfile -t _compose_args < <(compose_args)
  run_cmd docker compose "${_compose_args[@]}" "$@"
}

run_cmd() {
  if [ "$TEST_MODE" = "1" ]; then
    log "[TEST MODE] $*"
    return 0
  fi
  "$@"
}

repo_dirty_status() {
  git -C "$INSTALL_DIR" status --porcelain --untracked-files=no 2>/dev/null || true
}

backup_dirty_patch() {
  local backup_dir="$INSTALL_DIR/.cpa-local-backups"
  local timestamp backup_file
  timestamp="$(date +%Y%m%d-%H%M%S 2>/dev/null || date +%s)"
  backup_file="$backup_dir/dirty-worktree-${timestamp}.patch"
  mkdir -p "$backup_dir"
  git -C "$INSTALL_DIR" diff --binary > "$backup_file"
  printf '%s' "$backup_file"
}

abort_dirty_update() {
  local dirty backup_file
  dirty="$(repo_dirty_status)"
  [ -n "$dirty" ] || return 0
  backup_file="$(backup_dirty_patch)"
  log "检测到安装目录存在未提交的本地改动，已停止自动更新，避免覆盖服务器上的手工修改。"
  while IFS= read -r line; do
    [ -n "$line" ] && log "本地改动: $line"
  done <<< "$dirty"
  log "已导出当前本地改动补丁备份: $backup_file"
  log "如果你确认这些改动已经包含在 GitHub 最新代码中，可先执行："
  log "sudo git -c safe.directory=$INSTALL_DIR -C $INSTALL_DIR fetch origin $REPO_REF"
  log "sudo git -c safe.directory=$INSTALL_DIR -C $INSTALL_DIR reset --hard origin/$REPO_REF"
  log "然后重新运行统一更新命令。"
  log "如果你不确认这些改动是否已入库，请先查看补丁备份文件，再决定是否覆盖。"
  exit 1
}

http_get_code() {
  local url="$1"
  shift || true
  if [ "$TEST_MODE" = "1" ]; then
    printf '200'
    return 0
  fi
  curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$@" "$url"
}

format_access_addrs() {
  local listen_addr="$1"
  local port="${listen_addr#:}"
  if [ -z "$port" ] || [ "$port" = "$listen_addr" ]; then
    port="18317"
  fi
  if is_windows_platform && [ -n "${CPA_WEB_PORT:-}" ]; then
    port="$CPA_WEB_PORT"
  fi
  local localhost="http://127.0.0.1:${port}"
  local lan_ip=""
  local public_ip=""

  if has_cmd ip; then
    lan_ip="$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([^ ]*\).*/\1/p' | head -n 1)"
  fi
  if [ -z "$lan_ip" ] && has_cmd hostname; then
    lan_ip="$(hostname -I 2>/dev/null | awk '{print $1}' | head -n 1)"
  fi
  if [ -z "$lan_ip" ] && has_cmd ifconfig; then
    lan_ip="$(ifconfig 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p' | awk '$1 != "127.0.0.1" {print; exit}')"
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

trim_wrapped_quotes() {
  local value="$1"
  value="${value#\"}"
  value="${value%\"}"
  value="${value#\'}"
  value="${value%\'}"
  printf '%s' "$value"
}

default_auth_dir() {
  printf '%s/%s' "$INSTALL_DIR" "$DEFAULT_AUTH_SUBDIR"
}

normalize_host_dir() {
  local raw normalized
  raw="$(trim_wrapped_quotes "${1:-}")"
  raw="${raw%/}"
  raw="${raw%\\}"
  [ -n "$raw" ] || return 1
  if is_windows_platform; then
    if has_cmd cygpath; then
      normalized="$(cygpath -am "$raw" 2>/dev/null || true)"
    else
      normalized="${raw//\\//}"
      case "$normalized" in
        [A-Za-z]:/*|./*|../*|/*) ;;
        *) return 1 ;;
      esac
    fi
  else
    if [ -d "$raw" ]; then
      normalized="$(cd "$raw" 2>/dev/null && pwd || true)"
    else
      normalized="$raw"
    fi
  fi
  [ -n "$normalized" ] || return 1
  printf '%s' "$normalized"
}

ensure_auth_dir_exists() {
  local dir="$1"
  [ -n "$dir" ] || return 1
  if [ -d "$dir" ]; then
    return 0
  fi
  mkdir -p "$dir"
}

resolve_auth_dir() {
  local fallback normalized custom_value
  fallback="$(default_auth_dir)"

  if [ -n "${CPA_AUTH_DIR:-}" ]; then
    normalized="$(normalize_host_dir "$CPA_AUTH_DIR" 2>/dev/null || true)"
    if [ -n "$normalized" ] && [ -d "$normalized" ]; then
      CPA_AUTH_DIR="$normalized"
      return 0
    fi
    log "检测到账号目录配置无效，已忽略并准备回退: ${CPA_AUTH_DIR}"
    CPA_AUTH_DIR=""
  fi

  if is_windows_platform; then
    log "Windows 下建议直接使用默认目录，不需要手动输入绝对路径。"
    log "如未指定，将自动使用程序目录下的数据目录。"
    if [ "$TEST_MODE" = "1" ]; then
      custom_value=""
    else
      read -r -p "如需自定义账号目录（高级场景，可直接回车使用默认目录）: " custom_value
    fi
    custom_value="$(trim_wrapped_quotes "$custom_value")"
    if [ -n "$custom_value" ]; then
      normalized="$(normalize_host_dir "$custom_value" 2>/dev/null || true)"
      if [ -n "$normalized" ] && [ -d "$normalized" ]; then
        CPA_AUTH_DIR="$normalized"
        log "已使用自定义账号目录: $CPA_AUTH_DIR"
        return 0
      fi
      log "输入的目录无效，已自动回退到默认目录。"
    fi
    ensure_auth_dir_exists "$fallback"
    CPA_AUTH_DIR="$(normalize_host_dir "$fallback" 2>/dev/null || printf '%s' "$fallback")"
    log "Windows 下最终使用账号目录: $CPA_AUTH_DIR"
    return 0
  fi

  prompt_if_empty CPA_AUTH_DIR "未自动找到 CLIProxyAPI 的账号目录，请输入 auths 目录路径"
  normalized="$(normalize_host_dir "$CPA_AUTH_DIR" 2>/dev/null || true)"
  [ -n "$normalized" ] && CPA_AUTH_DIR="$normalized"
}

extract_bind_source() {
  local line="$1"
  if [[ "$line" =~ ^([A-Za-z]:[\\/][^:]*):(/[^:]*)(:.*)?$ ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  if [[ "$line" =~ ^([^:]+):(/[^:]*)(:.*)?$ ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
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
  local inspect envs binds discovered_auth_dir="" discovered_config_path="" discovered_management_key="" detected_port=""
  log "已发现主项目容器: $name"
  inspect="$(docker inspect "$name" --format '{{range .Mounts}}{{println .Destination "=" .Source}}{{end}}' 2>/dev/null || true)"
  envs="$(docker inspect "$name" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null || true)"
  binds="$(docker inspect "$name" --format '{{range .HostConfig.Binds}}{{println .}}{{end}}' 2>/dev/null || true)"
  discovered_auth_dir="$(printf '%s' "$inspect" | awk -F' = ' '$1=="/root/.cli-proxy-api"{print $2}' | head -n 1)"
  if [ -z "$discovered_auth_dir" ]; then
    while IFS= read -r line; do
      case "$line" in
        *auths*|*.cli-proxy-api*)
          discovered_auth_dir="$(extract_bind_source "$line" || true)"
          [ -n "$discovered_auth_dir" ] && break
          ;;
      esac
    done <<< "$binds"
  fi
  discovered_config_path="$(printf '%s' "$inspect" | awk -F' = ' '$2 ~ /config.yaml$/{print $2}' | head -n 1)"
  if [ -z "$discovered_config_path" ]; then
    while IFS= read -r line; do
      case "$line" in
        *config.yaml*)
          discovered_config_path="$(extract_bind_source "$line" || true)"
          [ -n "$discovered_config_path" ] && break
          ;;
      esac
    done <<< "$binds"
  fi
  discovered_management_key="$(printf '%s' "$envs" | awk -F= '$1=="MANAGEMENT_PASSWORD"{print substr($0,index($0,"=")+1)}' | head -n 1)"
  if printf '%s' "$envs" | grep -q '^PORT='; then
    detected_port="$(printf '%s' "$envs" | awk -F= '$1=="PORT"{print $2}' | head -n 1)"
  fi

  [ -n "$discovered_auth_dir" ] && { CPA_AUTH_DIR="$discovered_auth_dir"; log "从容器挂载识别到账号目录: $CPA_AUTH_DIR"; }
  [ -n "$discovered_config_path" ] && { CPA_CONFIG_PATH="$discovered_config_path"; log "从容器挂载识别到配置文件: $CPA_CONFIG_PATH"; }
  [ -n "$discovered_management_key" ] && CPA_MANAGEMENT_KEY="$discovered_management_key"
  if [ -n "$detected_port" ]; then
    CPA_MANAGEMENT_URL="http://127.0.0.1:${detected_port}"
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
    src="$(extract_bind_source "$line" || true)"
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
  if is_windows_platform; then
    ensure_auth_dir_exists "$CPA_AUTH_DIR"
  fi
  [ -d "$CPA_AUTH_DIR" ] || die "auths 目录不存在: $CPA_AUTH_DIR"
}

normalize_management_url() {
  if ! is_windows_platform; then
    return 0
  fi
  case "${CPA_MANAGEMENT_URL:-}" in
    http://127.0.0.1:*|https://127.0.0.1:*|http://localhost:*|https://localhost:*)
      CPA_MANAGEMENT_URL="$(printf '%s' "$CPA_MANAGEMENT_URL" | sed -E 's#(https?://)(127\.0\.0\.1|localhost)#\1host.docker.internal#')"
      log "Windows Docker 下已将管理地址调整为容器可访问地址: $CPA_MANAGEMENT_URL"
      ;;
  esac
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
    mgmt_code="$(http_get_code "$CPA_MANAGEMENT_URL/v0/management/auth-files" -H "Authorization: Bearer ${CPA_MANAGEMENT_KEY}")" || mgmt_code="000"
    [ "$mgmt_code" = "200" ] || die "management API 自检失败: ${CPA_MANAGEMENT_URL}/v0/management/auth-files 返回 $mgmt_code，请检查管理地址或管理密码"
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
    abort_dirty_update
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

  resolve_auth_dir
  prompt_if_empty CPA_MANAGEMENT_URL "未自动获取到 CLIProxyAPI 管理地址，请输入管理地址（例如 http://127.0.0.1:8317）"
  prompt_if_empty CPA_MANAGEMENT_KEY "未自动获取到 CLIProxyAPI 管理密码，请输入你的 CLIProxyAPI 管理密码"
  normalize_management_url

  upsert_env CPA_AUTH_DIR "${CPA_AUTH_DIR:-}"
  upsert_env CPA_AUTH_CONTAINER_DIR "${CPA_AUTH_CONTAINER_DIR:-/data/auths}"
  upsert_env CPA_WEB_PORT "${CPA_WEB_PORT:-18317}"
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
  run_compose up -d --build
  run_post_install_checks
  local listen_addr="${CPA_LISTEN_ADDR:-:18317}"
  log "完成"
  log "$(format_access_addrs "$listen_addr")"
  log "首次访问将进入 Web 初始化页面设置仪表台密码"
}

restart_service() {
  [ -d "$INSTALL_DIR" ] || die "尚未安装，无法重启"
  cd "$INSTALL_DIR"
  run_compose restart
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
    mapfile -t _compose_args < <(compose_args); (cd "$INSTALL_DIR" && docker compose "${_compose_args[@]}" ps) || true
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
  run_compose down
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

main() {
  case "${1:-}" in
    --install) install_or_update ;;
    --update) install_or_update ;;
    --restart) restart_service ;;
    --status) show_status ;;
    --uninstall) uninstall_service ;;
    *) run_menu ;;
  esac
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
