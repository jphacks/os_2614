#!/usr/bin/env bash
# Android エミュレータ (AVD) の起動・停止・状態確認。
#   pnpm emulator start [--headless] [--cold]
#   pnpm emulator stop|status|list|create
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

usage() {
  cat <<'EOF'
usage: emulator.sh <command> [options]

commands:
  start [--headless] [--cold]  AVD を起動して boot 完了まで待つ
  stop                         起動中のエミュレータを終了
  status                       接続中の端末と boot 状態を表示
  list                         AVD 一覧
  create [--force]             AVD が無ければ作成する (system image は事前に導入が必要)
  boot-wait [timeout]          boot 完了を待つ
EOF
}

require_emulator() {
  [ -x "$EMULATOR" ] || die "emulator が見つかりません: $EMULATOR"
}

cmd_start() {
  local headless=0 cold=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --headless) headless=1 ;;
      --cold) cold=1 ;;
      *) die "unknown option: $1" ;;
    esac
    shift
  done

  require_emulator
  require_adb

  local running
  running="$("$ADB" devices | awk '$1 ~ /^emulator-/ && $2 == "device" { print $1 }' | head -1)"
  if [ -n "$running" ]; then
    info "既に起動中のエミュレータがあります: $running"
    cmd_boot_wait "${BOOT_TIMEOUT:-240}"
    return 0
  fi

  "$EMULATOR" -list-avds | grep -qx "$AVD_NAME" \
    || die "AVD '$AVD_NAME' がありません。'pnpm emulator create' を実行してください"

  local -a args=(-avd "$AVD_NAME" -no-audio -netdelay none -netspeed full)
  if [ "$headless" -eq 1 ]; then
    args+=(-no-window -no-boot-anim -gpu swiftshader_indirect)
  else
    args+=(-gpu auto)
  fi
  if [ "$cold" -eq 1 ]; then
    args+=(-no-snapshot-load)
  fi

  info "emulator を起動します (AVD=$AVD_NAME, headless=$headless)"
  nohup "$EMULATOR" "${args[@]}" >"$ARTIFACT_DIR/emulator.log" 2>&1 &
  disown || true

  cmd_boot_wait "${BOOT_TIMEOUT:-240}"
}

cmd_boot_wait() {
  require_adb
  local timeout="${1:-240}" elapsed=0 serial=""
  while [ "$elapsed" -lt "$timeout" ]; do
    serial="$("$ADB" devices | awk '$1 ~ /^emulator-/ && $2 == "device" { print $1 }' | head -1)"
    if [ -n "$serial" ]; then
      local booted
      booted="$("$ADB" -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')"
      if [ "$booted" = "1" ]; then
        info "boot 完了: $serial (${elapsed}s)"
        export ANDROID_SERIAL="$serial"
        return 0
      fi
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  tail -20 "$ARTIFACT_DIR/emulator.log" >&2 || true
  die "エミュレータの boot がタイムアウトしました (${timeout}s)"
}

cmd_stop() {
  require_adb
  local serials
  serials="$("$ADB" devices | awk '$1 ~ /^emulator-/ { print $1 }')"
  if [ -z "$serials" ]; then
    info "起動中のエミュレータはありません"
    return 0
  fi
  while IFS= read -r serial; do
    [ -n "$serial" ] || continue
    info "停止: $serial"
    "$ADB" -s "$serial" emu kill >/dev/null 2>&1 || true
  done <<<"$serials"
}

cmd_status() {
  require_adb
  "$ADB" devices -l
  local serial
  for serial in $("$ADB" devices | awk '$1 ~ /^emulator-/ && $2 == "device" { print $1 }'); do
    printf '%s boot_completed=%s\n' "$serial" \
      "$("$ADB" -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')"
  done
}

cmd_list() {
  require_emulator
  "$EMULATOR" -list-avds
}

cmd_create() {
  local force=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --force) force=1 ;;
      *) die "unknown option: $1" ;;
    esac
    shift
  done

  [ -x "$AVDMANAGER" ] || die "avdmanager が見つかりません: $AVDMANAGER"
  if "$EMULATOR" -list-avds | grep -qx "$AVD_NAME" && [ "$force" -eq 0 ]; then
    info "AVD '$AVD_NAME' は既に存在します"
    return 0
  fi
  [ -d "$SDK/system-images/android-36/google_apis/arm64-v8a" ] \
    || die "system image がありません。'sdkmanager \"$SYSTEM_IMAGE\"' を実行してください"

  echo no | "$AVDMANAGER" create avd -n "$AVD_NAME" -k "$SYSTEM_IMAGE" -d "$DEVICE_PROFILE" --force >/dev/null
  local config="$HOME/.android/avd/$AVD_NAME.avd/config.ini"
  if [ -f "$config" ]; then
    sed -i '' \
      -e 's/^hw.keyboard=.*/hw.keyboard=yes/' \
      -e 's/^hw.ramSize=.*/hw.ramSize=4096/' \
      -e 's/^disk.dataPartition.size=.*/disk.dataPartition.size=8G/' \
      "$config"
  fi
  info "AVD '$AVD_NAME' を作成しました"
}

main() {
  [ $# -gt 0 ] || { usage; exit 1; }
  local cmd="$1"
  shift
  case "$cmd" in
    start) cmd_start "$@" ;;
    stop) cmd_stop "$@" ;;
    status) cmd_status "$@" ;;
    list) cmd_list "$@" ;;
    create) cmd_create "$@" ;;
    boot-wait) cmd_boot_wait "$@" ;;
    -h|--help|help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
