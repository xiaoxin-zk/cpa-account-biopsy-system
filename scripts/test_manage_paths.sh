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

test_linux_logic_unchanged() {
  local tmp
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
  rm -rf "$tmp"
}

test_windows_empty_path_falls_back
test_windows_invalid_path_falls_back
test_windows_drive_bind_source_parses
test_linux_logic_unchanged

printf 'ok\n'
