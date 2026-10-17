#!/usr/bin/env bash
# AI agent 用の最短スモークテスト。
# 端末が無ければヘッドレスのエミュレータを起動し、アプリを入れて起動し、
# 画面テキスト・スクリーンショット・WebView CDP を取得する。
#   pnpm android:smoke [--build]
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

BUILD=0
while [ $# -gt 0 ]; do
  case "$1" in
    --build) BUILD=1 ;;
    *) die "unknown option: $1" ;;
  esac
  shift
done

require_adb

if ! "$ADB" devices | awk 'NR > 1 && $2 == "device" { found = 1 } END { exit !found }'; then
  info "接続中の端末が無いため、ヘッドレスエミュレータを起動します"
  bash "$SCRIPT_DIR/emulator.sh" start --headless
fi

resolve_serial

if [ "$BUILD" -eq 1 ] || ! adb_target shell pm list packages "$APP_ID" | grep -q "package:${APP_ID}"; then
  bash "$SCRIPT_DIR/device.sh" install
fi

info "アニメーションを無効化してテストを安定させます"
adb_target shell settings put global window_animation_scale 0 >/dev/null 2>&1 || true
adb_target shell settings put global transition_animation_scale 0 >/dev/null 2>&1 || true
adb_target shell settings put global animator_duration_scale 0 >/dev/null 2>&1 || true

bash "$SCRIPT_DIR/device.sh" launch >/dev/null
bash "$SCRIPT_DIR/device.sh" wait-text "ちかく" 60

shot="$ARTIFACT_DIR/smoke-${ANDROID_SERIAL}.png"
ui="$ARTIFACT_DIR/smoke-${ANDROID_SERIAL}.xml"
bash "$SCRIPT_DIR/device.sh" screenshot "$shot" >/dev/null
bash "$SCRIPT_DIR/device.sh" ui "$ui" >/dev/null

info ""
info "SMOKE OK"
info "device:     $ANDROID_SERIAL"
info "screenshot: $shot"
info "ui:         $ui"
info "cdp:        pnpm device cdp 9222  ->  http://127.0.0.1:9222"
