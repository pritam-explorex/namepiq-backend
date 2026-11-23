#!/usr/bin/env bash
# Expose local API (:8080) as https://api-stage.namepiq.com via Cloudflare Tunnel.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TOKEN_FILE="$ROOT/.cloudflared-stage.token"
if [[ ! -f "$TOKEN_FILE" ]]; then
  echo "missing $TOKEN_FILE — recreate tunnel token first" >&2
  exit 1
fi
exec cloudflared tunnel run --token "$(cat "$TOKEN_FILE")"
