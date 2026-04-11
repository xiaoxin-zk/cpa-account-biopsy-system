#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
export CPA_TEST_MODE=1

# shellcheck disable=SC1091
source "$ROOT_DIR/scripts/manage.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local got="$1"
  local want="$2"
  local msg="$3"
  [ "$got" = "$want" ] || fail "$msg | got=$got want=$want"
}

assert_match() {
  local got="$1"
  local regex="$2"
  local msg="$3"
  [[ "$got" =~ $regex ]] || fail "$msg | got=$got regex=$regex"
}

test_windows_empty_path_falls_back() {
  local tmp
  local expected
  tmp="$(mktemp -d)"
  CPA_PLATFORM_OVERRIDE=windows
  OS=Windows_NT
  USERPROFILE="$tmp"
  INSTALL_DIR="$(default_install_dir)"
  CPA_AUTH_DIR=""
  resolve_auth_dir >/dev/null
  expected="$(normalize_host_dir "$INSTALL_DIR/$DEFAULT_AUTH_SUBDIR")"
  assert_eq "$CPA_AUTH_DIR" "$expected" "windows empty path should use default auth dir"
  [ -d "$CPA_AUTH_DIR" ] || fail "windows fallback dir should be created"
  rm -rf "$tmp"
}

test_windows_invalid_path_falls_back() {
  local tmp
  local expected
  tmp="$(mktemp -d)"
  CPA_PLATFORM_OVERRIDE=windows
  OS=Windows_NT
  USERPROFILE="$tmp"
  INSTALL_DIR="$(default_install_dir)"
  CPA_AUTH_DIR='Z:\not-real-path'
  resolve_auth_dir >/dev/null
  expected="$(normalize_host_dir "$INSTALL_DIR/$DEFAULT_AUTH_SUBDIR")"
  assert_eq "$CPA_AUTH_DIR" "$expected" "windows invalid path should fall back"
  rm -rf "$tmp"
}

test_windows_drive_bind_source_parses() {
  local src
  CPA_PLATFORM_OVERRIDE=windows
  src="$(extract_bind_source 'C:\data\auths:/data/auths:rw' || true)"
  assert_eq "$src" 'C:\data\auths' "windows drive path should parse as full bind source"
}

test_windows_management_url_rewrites_to_host_gateway() {
  CPA_PLATFORM_OVERRIDE=windows
  CPA_MANAGEMENT_URL='http://127.0.0.1:8317'
  normalize_management_url
  assert_eq "$CPA_MANAGEMENT_URL" 'http://host.docker.internal:8317' "windows management url should target host.docker.internal"
}

test_windows_compose_uses_windows_override() {
  local args
  CPA_PLATFORM_OVERRIDE=windows
  ENV_FILE='/tmp/test.env'
  mapfile -t args < <(compose_args)
  assert_eq "${args[0]}" '--env-file' "compose args should include env-file flag"
  assert_eq "${args[1]}" '/tmp/test.env' "compose args should include env file path"
  assert_eq "${args[2]}" '-f' "compose args should include base compose flag"
  assert_eq "${args[3]}" 'docker-compose.yml' "compose args should include base compose file"
  assert_eq "${args[4]}" '-f' "compose args should include override compose flag"
  assert_eq "${args[5]}" 'docker-compose.windows.yml' "windows should use windows compose override"
}

test_linux_logic_unchanged() {
  local tmp
  local args
  tmp="$(mktemp -d)"
  CPA_PLATFORM_OVERRIDE=linux
  OS=''
  USERPROFILE=''
  INSTALL_DIR='/opt/cpa-account-biopsy-system'
  CPA_AUTH_DIR="$tmp/auths"
  mkdir -p "$CPA_AUTH_DIR"
  resolve_auth_dir >/dev/null
  assert_eq "$CPA_AUTH_DIR" "$tmp/auths" "linux should keep valid discovered path"
  assert_match "$(default_install_dir)" '^/opt/cpa-account-biopsy-system$' "linux default install dir should stay under /opt"
  CPA_MANAGEMENT_URL='http://127.0.0.1:8317'
  normalize_management_url
  assert_eq "$CPA_MANAGEMENT_URL" 'http://127.0.0.1:8317' "linux management url should remain unchanged"
  ENV_FILE='/tmp/test.env'
  mapfile -t args < <(compose_args)
  assert_eq "${args[5]}" 'docker-compose.linux.yml' "linux should use linux compose override"
  rm -rf "$tmp"
}

test_windows_empty_path_falls_back
test_windows_invalid_path_falls_back
test_windows_drive_bind_source_parses
test_windows_management_url_rewrites_to_host_gateway
test_windows_compose_uses_windows_override
test_linux_logic_unchanged

printf 'ok\n'
