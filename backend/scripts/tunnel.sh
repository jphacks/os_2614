#!/usr/bin/env bash
# https://chain.i32.jp を localhost:8080 に繋ぐ Cloudflare Tunnel を起動する。
# token はリポジトリに置かず、実行のたびに cf CLI で取得する。
set -euo pipefail

TUNNEL_ID="${CHAIN_TUNNEL_ID:-3a8a99ec-0e8e-40b6-ad72-fe337c27acab}"

token="$(cf tunnels token get "$TUNNEL_ID" | tr -d '"[:space:]')"
exec cloudflared tunnel --no-autoupdate run --token "$token"
