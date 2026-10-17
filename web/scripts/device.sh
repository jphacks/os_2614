#!/usr/bin/env bash
# 実機・エミュレータ共通の操作 CLI。AI agent からもそのまま叩ける。
#   pnpm device [-s SERIAL] <command> [args]
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

usage() {
  cat <<'EOF'
usage: device.sh [-s SERIAL] <command> [args]

commands:
  devices                     接続中の端末一覧
  pair <ip:port> [code]       ワイヤレスデバッグをペアリング
  connect <ip:port>           ワイヤレスデバッグに接続
  disconnect [ip:port]        接続を解除
  info                        端末とアプリの情報
  install                     次を実行: next build -> cap sync -> assembleDebug -> install
  launch                      アプリを起動
  stop                        アプリを強制停止
  restart                     アプリを再起動
  clear                       アプリのデータを削除
  screenshot [path]           スクリーンショット (既定: artifacts/screenshot-<ts>.png)
  ui [path]                   UI ツリーを保存し、テキスト要素を一覧表示
  tap <x> <y>                 座標タップ
  tap-text <text>             表示テキスト/content-desc を探して中心をタップ
  wait-text <text> [sec]      テキストが現れるまで待つ (既定 30 秒)
  type <ascii>                ASCII 文字列を入力 (日本語は CDP を使う)
  swipe <x1> <y1> <x2> <y2> [ms]
  back | home                 戻る / ホーム
  cdp [port]                  WebView の DevTools を localhost:<port> に転送 (既定 9222)
  logs [lines]                logcat (既定 200 行)
EOF
}

cmd_devices() {
  require_adb
  "$ADB" devices -l
}

cmd_pair() {
  require_adb
  [ $# -ge 1 ] || die "usage: pair <ip:port> [code]"
  if [ $# -ge 2 ]; then
    "$ADB" pair "$1" "$2"
  else
    "$ADB" pair "$1"
  fi
}

cmd_connect() {
  require_adb
  [ $# -ge 1 ] || die "usage: connect <ip:port>"
  "$ADB" connect "$1"
}

cmd_disconnect() {
  require_adb
  if [ $# -ge 1 ]; then
    "$ADB" disconnect "$1"
  else
    "$ADB" disconnect
  fi
}

cmd_info() {
  resolve_serial
  adb_target shell getprop ro.product.model
  adb_target shell getprop ro.build.version.release
  adb_target shell getprop ro.build.version.sdk
  adb_target shell wm size
  adb_target shell wm density
  adb_target shell dumpsys package "$APP_ID" | awk '/versionName=/{print; exit}'
}

cmd_install() {
  local sync=1
  while [ $# -gt 0 ]; do
    case "$1" in
      --no-build|--apk-only) sync=0 ;;
      *) die "unknown option: $1" ;;
    esac
    shift
  done

  resolve_serial
  if [ "$sync" -eq 1 ]; then
    info "web を build して Capacitor に sync します"
    ( cd "$ROOT_DIR" && pnpm android:sync )
    ( cd "$ROOT_DIR/android" && ./gradlew assembleDebug --console=plain )
  fi

  local apk="$ROOT_DIR/android/app/build/outputs/apk/debug/app-debug.apk"
  [ -f "$apk" ] || die "APK がありません: $apk"
  adb_target install -r "$apk"
  info "installed: $apk"
}

cmd_launch() {
  resolve_serial
  adb_target shell am start -W -n "$ACTIVITY"
}

cmd_stop() {
  resolve_serial
  adb_target shell am force-stop "$APP_ID"
}

cmd_restart() {
  resolve_serial
  adb_target shell am force-stop "$APP_ID" || true
  sleep 1
  adb_target shell am start -W -n "$ACTIVITY"
}

cmd_clear() {
  resolve_serial
  adb_target shell pm clear "$APP_ID"
}

cmd_screenshot() {
  resolve_serial
  local path="${1:-$ARTIFACT_DIR/screenshot-$(date +%Y%m%d-%H%M%S).png}"
  mkdir -p "$(dirname "$path")"
  adb_target exec-out screencap -p >"$path"
  info "$path"
}

cmd_ui() {
  resolve_serial
  local path="${1:-$ARTIFACT_DIR/ui-$(date +%Y%m%d-%H%M%S).xml}"
  mkdir -p "$(dirname "$path")"
  local xml
  xml="$(ui_xml)"
  [ -n "$xml" ] || die "UI ツリーを取得できませんでした"
  printf '%s' "$xml" >"$path"
  info "saved: $path"
  ui_list "$path"
}

cmd_tap() {
  [ $# -ge 2 ] || die "usage: tap <x> <y>"
  resolve_serial
  adb_target shell input tap "$1" "$2"
}

cmd_tap_text() {
  [ $# -ge 1 ] || die "usage: tap-text <text>"
  resolve_serial
  local xml_file="$ARTIFACT_DIR/.ui-lookup.xml" found
  ui_xml >"$xml_file"
  found="$(ui_find_center "$xml_file" "$1")" || die "テキストが見つかりません: $1"
  local x y label
  read -r x y label <<<"$found"
  info "tap ($x, $y): $label"
  adb_target shell input tap "$x" "$y"
}

cmd_wait_text() {
  [ $# -ge 1 ] || die "usage: wait-text <text> [seconds]"
  local needle="$1" timeout="${2:-30}" elapsed=0 xml_file="$ARTIFACT_DIR/.ui-wait.xml"
  resolve_serial
  while [ "$elapsed" -lt "$timeout" ]; do
    ui_xml >"$xml_file"
    if grep -Fq -- "$needle" "$xml_file"; then
      info "found after ${elapsed}s: $needle"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  die "タイムアウト (${timeout}s): $needle"
}

cmd_type() {
  [ $# -ge 1 ] || die "usage: type <ascii>"
  resolve_serial
  local escaped="${1// /%s}"
  adb_target shell input text "$escaped"
}

cmd_swipe() {
  [ $# -ge 4 ] || die "usage: swipe <x1> <y1> <x2> <y2> [ms]"
  resolve_serial
  adb_target shell input swipe "$1" "$2" "$3" "$4" "${5:-300}"
}

cmd_back() {
  resolve_serial
  adb_target shell input keyevent 4
}

cmd_home() {
  resolve_serial
  adb_target shell input keyevent 3
}

cmd_cdp() {
  resolve_serial
  local port="${1:-9222}"
  local pid

  adb_target shell am start -W -n "$ACTIVITY" >/dev/null
  sleep 2

  pid="$(adb_target shell pidof "$APP_ID" 2>/dev/null | tr -d '\r' | awk '{print $1}')"

  local socket=""
  if [ -n "$pid" ]; then
    socket="$(adb_target shell cat /proc/net/unix 2>/dev/null \
      | grep -o "webview_devtools_remote_${pid}" | head -1 | tr -d '\r')"
  fi
  if [ -z "$socket" ]; then
    socket="$(adb_target shell cat /proc/net/unix 2>/dev/null \
      | grep -o 'webview_devtools_remote_[0-9]*' | head -1 | tr -d '\r')"
  fi
  [ -n "$socket" ] || die "WebView の DevTools socket が見つかりません (debug APK か確認してください)"

  adb_target forward "tcp:${port}" "localabstract:${socket}" >/dev/null
  info "cdp: http://127.0.0.1:${port} (${socket})"

  local body=""
  for _ in $(seq 1 15); do
    body="$(curl -fsS "http://127.0.0.1:${port}/json/list" 2>/dev/null || true)"
    if [ -n "$body" ] && [ "$(printf '%s' "$body" | tr -d '[:space:]')" != "[]" ]; then
      printf '%s\n' "$body"
      return 0
    fi
    sleep 1
  done

  printf '%s\n' "$body"
  die "WebView のページがまだありません。アプリの起動完了後に再実行してください"
}

cmd_logs() {
  resolve_serial
  local lines="${1:-200}" pid
  pid="$(adb_target shell pidof "$APP_ID" 2>/dev/null | tr -d '\r' | awk '{print $1}')"
  if [ -n "$pid" ]; then
    adb_target logcat -d --pid="$pid" -t "$lines"
  else
    adb_target logcat -d -t "$lines"
  fi
}

main() {
  local serial_opt=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -s|--serial)
        [ $# -ge 2 ] || die "usage: -s <serial>"
        serial_opt="$2"
        shift 2
        ;;
      *) break ;;
    esac
  done
  [ -n "$serial_opt" ] && export ANDROID_SERIAL="$serial_opt"

  [ $# -gt 0 ] || { usage; exit 1; }
  local cmd="$1"
  shift
  case "$cmd" in
    devices) cmd_devices "$@" ;;
    pair) cmd_pair "$@" ;;
    connect) cmd_connect "$@" ;;
    disconnect) cmd_disconnect "$@" ;;
    info) cmd_info "$@" ;;
    install) cmd_install "$@" ;;
    launch) cmd_launch "$@" ;;
    stop) cmd_stop "$@" ;;
    restart) cmd_restart "$@" ;;
    clear) cmd_clear "$@" ;;
    screenshot) cmd_screenshot "$@" ;;
    ui) cmd_ui "$@" ;;
    tap) cmd_tap "$@" ;;
    tap-text) cmd_tap_text "$@" ;;
    wait-text) cmd_wait_text "$@" ;;
    type) cmd_type "$@" ;;
    swipe) cmd_swipe "$@" ;;
    back) cmd_back "$@" ;;
    home) cmd_home "$@" ;;
    cdp) cmd_cdp "$@" ;;
    logs) cmd_logs "$@" ;;
    -h|--help|help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
