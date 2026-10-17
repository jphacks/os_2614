#!/usr/bin/env bash
# ライブリロード付きで Android (実機/エミュレータ) にアプリを入れて起動する。
# adb reverse を使うので、端末と Mac が同じ Wi-Fi でなくても USB/ワイヤレス adb で動く。
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

PORT="${CHAIN_DEV_PORT:-3000}"
DEV_PID=""

cleanup() {
  if [ -n "$DEV_PID" ]; then
    kill "$DEV_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

resolve_serial

if ! curl -fsS "http://127.0.0.1:${PORT}/" >/dev/null 2>&1; then
  info "Next.js dev server を起動します (0.0.0.0:${PORT})"
  ( cd "$ROOT_DIR" && exec pnpm exec next dev -H 0.0.0.0 -p "$PORT" ) \
    >"$ARTIFACT_DIR/next-dev.log" 2>&1 &
  DEV_PID=$!

  for _ in $(seq 1 120); do
    if curl -fsS "http://127.0.0.1:${PORT}/" >/dev/null 2>&1; then
      break
    fi
    if ! kill -0 "$DEV_PID" >/dev/null 2>&1; then
      tail -30 "$ARTIFACT_DIR/next-dev.log" >&2 || true
      die "Next.js dev server の起動に失敗しました"
    fi
    sleep 1
  done
  curl -fsS "http://127.0.0.1:${PORT}/" >/dev/null 2>&1 \
    || die "Next.js dev server が ${PORT} で応答しません"
else
  info "既存の Next.js dev server (port ${PORT}) を使います"
fi

if [ ! -d "$ROOT_DIR/out" ]; then
  info "out/ が無いので static export を作成します"
  ( cd "$ROOT_DIR" && pnpm build )
fi

info "live reload で起動: device=$ANDROID_SERIAL url=http://localhost:${PORT}"
info "Ctrl+C で終了します"
( cd "$ROOT_DIR" && exec pnpm exec cap run android \
    --live-reload \
    --host localhost \
    --port "$PORT" \
    --forwardPorts "${PORT}:${PORT}" \
    --target "$ANDROID_SERIAL" )
