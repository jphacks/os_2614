#!/usr/bin/env bash
# 共通設定。各スクリプトから source して使う。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
ADB="$SDK/platform-tools/adb"
EMULATOR="$SDK/emulator/emulator"
AVDMANAGER="$SDK/cmdline-tools/latest/bin/avdmanager"
SDKMANAGER="$SDK/cmdline-tools/latest/bin/sdkmanager"

AVD_NAME="${CHAIN_AVD:-chain}"
SYSTEM_IMAGE="${CHAIN_SYSTEM_IMAGE:-system-images;android-36;google_apis;arm64-v8a}"
DEVICE_PROFILE="${CHAIN_DEVICE_PROFILE:-pixel_7}"
APP_ID="${CHAIN_APP_ID:-jp.i32.chain}"
ACTIVITY="${APP_ID}/.MainActivity"
ARTIFACT_DIR="${CHAIN_ARTIFACTS:-$ROOT_DIR/artifacts}"

mkdir -p "$ARTIFACT_DIR"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

info() {
  printf '%s\n' "$*"
}

require_adb() {
  [ -x "$ADB" ] || die "adb が見つかりません: $ADB (ANDROID_HOME を設定してください)"
}

# ANDROID_SERIAL が未設定なら、接続中の端末が1台のときだけ自動選択する。
resolve_serial() {
  require_adb
  if [ -n "${ANDROID_SERIAL:-}" ]; then
    return 0
  fi

  local devices count
  devices="$("$ADB" devices | awk 'NR > 1 && $2 == "device" { print $1 }')"
  count="$(printf '%s\n' "$devices" | sed '/^$/d' | wc -l | tr -d ' ')"

  if [ "$count" -eq 0 ]; then
    die "接続中の端末がありません。'pnpm emulator start' でエミュレータを起動するか、USB デバッグを有効にして端末を接続してください"
  fi
  if [ "$count" -gt 1 ]; then
    printf '接続中の端末:\n%s\n' "$devices" >&2
    die "端末が複数あります。ANDROID_SERIAL=<serial> または 'pnpm device -s <serial> <command>' で指定してください"
  fi

  export ANDROID_SERIAL="$devices"
  info "device: $ANDROID_SERIAL"
}

adb_target() {
  "$ADB" -s "${ANDROID_SERIAL:?resolve_serial を先に呼んでください}" "$@"
}

# macOS の LAN IP (en0/en1/default route の順で探す)
lan_ip() {
  local ip=""
  ip="$(ipconfig getifaddr en0 2>/dev/null || true)"
  [ -n "$ip" ] || ip="$(ipconfig getifaddr en1 2>/dev/null || true)"
  if [ -z "$ip" ]; then
    local iface
    iface="$(route -n get default 2>/dev/null | awk '/interface:/{print $2}')"
    [ -n "$iface" ] && ip="$(ipconfig getifaddr "$iface" 2>/dev/null || true)"
  fi
  printf '%s' "$ip"
}

# uiautomator の XML を取得する (WebView 内の DOM テキストも取れる)
ui_xml() {
  adb_target shell uiautomator dump /sdcard/chain-ui.xml >/dev/null 2>&1 || true
  adb_target exec-out cat /sdcard/chain-ui.xml 2>/dev/null || true
}

# XML 内の text/content-desc/bounds を1行1要素で表示する
ui_list() {
  local xml_file="$1"
  python3 - "$xml_file" <<'PY'
import sys
import xml.etree.ElementTree as ET

tree = ET.parse(sys.argv[1])
for node in tree.iter("node"):
    text = node.get("text", "")
    desc = node.get("content-desc", "")
    bounds = node.get("bounds", "")
    cls = node.get("class", "")
    if text or desc:
        print(f"{bounds}\t{cls}\ttext={text!r}\tcontent-desc={desc!r}")
PY
}

# テキストに一致する最も小さいノードの中心座標を返す: "x y label"
ui_find_center() {
  local xml_file="$1"
  local needle="$2"
  python3 - "$xml_file" "$needle" <<'PY'
import re
import sys
import xml.etree.ElementTree as ET

path, needle = sys.argv[1], sys.argv[2]
tree = ET.parse(path)
best = None
for node in tree.iter("node"):
    haystack = f'{node.get("text", "")} {node.get("content-desc", "")}'
    if needle not in haystack:
        continue
    match = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", node.get("bounds", ""))
    if not match:
        continue
    x1, y1, x2, y2 = map(int, match.groups())
    area = (x2 - x1) * (y2 - y1)
    if best is None or area < best[0]:
        best = (area, (x1 + x2) // 2, (y1 + y2) // 2, node.get("text") or node.get("content-desc"))

if best is None:
    sys.exit(1)
print(f"{best[1]} {best[2]} {best[3]}")
PY
}
