#!/bin/sh
set -e

CONFIG=/CLIProxyAPI/config.yaml
TEMPLATE=/CLIProxyAPI/config.template.yaml

# Ensure runtime directories exist
mkdir -p /root/.cli-proxy-api /CLIProxyAPI/logs

# Docker creates a directory when the host file doesn't exist; replace it with the template
if [ -d "$CONFIG" ]; then
  rmdir "$CONFIG" 2>/dev/null || true
fi

# Seed config.yaml from template if missing or empty
if [ ! -s "$CONFIG" ]; then
  echo "[entrypoint] config.yaml is empty — seeding from template"
  cp "$TEMPLATE" "$CONFIG"
  echo "[entrypoint] Edit config.yaml or configure via the management panel."
fi

exec ./CLIProxyAPI "$@"
