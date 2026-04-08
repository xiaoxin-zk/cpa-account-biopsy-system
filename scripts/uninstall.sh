#!/usr/bin/env bash
set -euo pipefail
INSTALL_DIR="${CPA_INSTALL_DIR:-/opt/cpa-account-biopsy-system}"
ENV_FILE="$INSTALL_DIR/.env"
if [ -d "$INSTALL_DIR" ]; then
  cd "$INSTALL_DIR"
  docker compose --env-file "$ENV_FILE" down || true
  cd /
  rm -rf "$INSTALL_DIR"
fi
printf '[cpa-account-biopsy-system] 卸载完成，不影响主项目 CLIProxyAPI\n'
