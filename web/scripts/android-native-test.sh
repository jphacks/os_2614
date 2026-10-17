#!/usr/bin/env bash
# native テスト一式 (JVM 単体 + instrumented + debug build + 再起動復帰) を回す。
#   pnpm test:android:native
# 端末が無ければ agent-smoke.sh と同じくヘッドレスエミュレータを起動する。
# 再起動テストは emulator-* のときだけ実行する (実機は絶対に再起動しない)。
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

STARTED_EMULATOR=0
JVM_TESTS=0
INSTRUMENTED_TESTS=0
INSTRUMENTED_FILES=0
BOOT_CHECK="SKIPPED"

cleanup() {
  local rc=$?
  if [ "$STARTED_EMULATOR" -eq 1 ]; then
    info "このスクリプトが起動したエミュレータを停止します"
    bash "$SCRIPT_DIR/emulator.sh" stop >/dev/null 2>&1 || true
  fi
  exit "$rc"
}
trap cleanup EXIT

xml_attr() {
  local file="$1" name="$2" value
  value="$(grep -o "${name}=\"[0-9]*\"" "$file" | head -1 | tr -dc '0-9')"
  printf '%s' "${value:-0}"
}

file_mtime() {
  stat -f %m "$1" 2>/dev/null || stat -c %Y "$1"
}

print_failure_summary() {
  local dir="$1" file tests failures errors
  info "--- test result XML summary ($dir) ---" >&2
  while IFS= read -r -d '' file; do
    tests="$(xml_attr "$file" tests)"
    failures="$(xml_attr "$file" failures)"
    errors="$(xml_attr "$file" errors)"
    info "$(basename "$file"): tests=$tests failures=$failures errors=$errors" >&2
    grep -E '<(failure|error)' "$file" | head -5 >&2 || true
  done < <(find "$dir" -maxdepth 1 -type f -name 'TEST-*.xml' -print0 2>/dev/null)
}

require_adb

if ! "$ADB" devices | awk 'NR > 1 && $2 == "device" { found = 1 } END { exit !found }'; then
  info "接続中の端末が無いため、ヘッドレスエミュレータを起動します"
  bash "$SCRIPT_DIR/emulator.sh" start --headless
  STARTED_EMULATOR=1
fi

resolve_serial

UNIT_RESULTS="$ROOT_DIR/android/app/build/test-results/testDebugUnitTest"
CONNECTED_RESULTS="$ROOT_DIR/android/app/build/outputs/androidTest-results/connected/debug"

info "==> JVM unit tests"
rm -rf "$UNIT_RESULTS"
if ! ( cd "$ROOT_DIR/android" && ./gradlew --offline :app:testDebugUnitTest ); then
  print_failure_summary "$UNIT_RESULTS"
  die "JVM unit tests failed"
fi
JVM_TESTS="$(find "$UNIT_RESULTS" -maxdepth 1 -type f -name 'TEST-*.xml' -print0 2>/dev/null \
  | while IFS= read -r -d '' f; do xml_attr "$f" tests; printf '\n'; done \
  | awk '{ total += $1 } END { print total + 0 }')"
[ "$JVM_TESTS" -gt 0 ] || die "JVM テスト結果がありません: $UNIT_RESULTS"
while IFS= read -r -d '' f; do
  failures="$(xml_attr "$f" failures)"
  errors="$(xml_attr "$f" errors)"
  [ "$failures" -eq 0 ] && [ "$errors" -eq 0 ] \
    || die "JVM テストが失敗しています: $(basename "$f") failures=$failures errors=$errors"
done < <(find "$UNIT_RESULTS" -maxdepth 1 -type f -name 'TEST-*.xml' -print0 2>/dev/null)
info "JVM tests: $JVM_TESTS (failures=0 errors=0)"

info "==> debug APK build"
( cd "$ROOT_DIR/android" && ./gradlew --offline :app:assembleDebug )

info "==> instrumented tests (connectedDebugAndroidTest)"
rm -rf "$CONNECTED_RESULTS"
CONNECTED_START="$(date +%s)"
if ! ( cd "$ROOT_DIR/android" && ./gradlew :app:connectedDebugAndroidTest ); then
  while IFS= read -r -d '' log; do
    info "--- $(basename "$log") (tail) ---" >&2
    tail -60 "$log" >&2 || true
  done < <(find "$CONNECTED_RESULTS" -maxdepth 1 -type f -name 'utp*.log' -print0 2>/dev/null)
  print_failure_summary "$CONNECTED_RESULTS"
  die "instrumented tests failed"
fi

while IFS= read -r -d '' f; do
  INSTRUMENTED_FILES=$((INSTRUMENTED_FILES + 1))
  tests="$(xml_attr "$f" tests)"
  failures="$(xml_attr "$f" failures)"
  errors="$(xml_attr "$f" errors)"
  mtime="$(file_mtime "$f")"
  [ "$tests" -gt 0 ] || die "instrumented テスト件数が 0 です: $(basename "$f")"
  [ "$failures" -eq 0 ] && [ "$errors" -eq 0 ] \
    || die "instrumented テストが失敗しています: $(basename "$f") failures=$failures errors=$errors"
  [ "$mtime" -ge "$CONNECTED_START" ] \
    || die "古い instrumented 結果を使っています: $(basename "$f") (mtime=$mtime start=$CONNECTED_START)"
  info "instrumented: $(basename "$f") tests=$tests failures=0 errors=0"
  INSTRUMENTED_TESTS=$((INSTRUMENTED_TESTS + tests))
done < <(find "$CONNECTED_RESULTS" -maxdepth 1 -type f -name 'TEST-*.xml' -print0 2>/dev/null)
[ "$INSTRUMENTED_FILES" -gt 0 ] || die "instrumented テスト結果がありません: $CONNECTED_RESULTS"
info "instrumented tests: $INSTRUMENTED_TESTS ($INSTRUMENTED_FILES device result file(s))"

if [[ "$ANDROID_SERIAL" == emulator-* ]]; then
  info "==> boot restore check ($ANDROID_SERIAL)"
  apk="$ROOT_DIR/android/app/build/outputs/apk/debug/app-debug.apk"
  [ -f "$apk" ] || die "debug APK がありません: $apk"
  adb_target install -r "$apk" >/dev/null
  adb_target shell pm list packages "$APP_ID" | grep -q "package:${APP_ID}" \
    || die "アプリがインストールされていません: $APP_ID"

  # 新規インストール直後は stopped 状態で BOOT_COMPLETED が届かないため一度起動する。
  adb_target shell am start -W -n "$ACTIVITY" >/dev/null
  adb_target shell am kill "$APP_ID" >/dev/null 2>&1 || true

  boot_xml='<map><boolean name="started_by_user" value="true" /></map>'
  boot_b64="$(printf '%s' "$boot_xml" | base64 | tr -d '\n')"
  adb_target shell "run-as ${APP_ID} sh -c 'mkdir -p shared_prefs; printf %s ${boot_b64} | base64 -d > shared_prefs/chain_boot.xml'"
  adb_target shell "run-as ${APP_ID} cat shared_prefs/chain_boot.xml" | grep -Fq 'started_by_user' \
    || die "再起動フラグを書き込めませんでした (run-as $APP_ID)"

  for perm in BLUETOOTH_SCAN BLUETOOTH_ADVERTISE BLUETOOTH_CONNECT; do
    adb_target shell pm grant "$APP_ID" "android.permission.$perm" >/dev/null 2>&1 || true
  done

  # stopped 解除と権限付与は PackageManager が非同期に永続化する。再起動前に待って
  # ディスクへ書き込みを確定させないと、reboot 後に stopped=true へ戻ってしまう。
  sleep 10
  adb_target shell sync
  adb_target shell dumpsys package "$APP_ID" | grep -q 'stopped=false' \
    || die "アプリが stopped のままで BOOT_COMPLETED を受け取れません: $APP_ID"
  if adb_target shell dumpsys package "$APP_ID" \
    | grep -Eq 'android.permission.BLUETOOTH_(SCAN|ADVERTISE|CONNECT): granted=false'; then
    die "BLE 実行時権限を付与できませんでした: $APP_ID"
  fi

  info "emulator を再起動します (adb reboot)"
  adb_target reboot
  adb_target wait-for-device
  booted=0
  for _ in $(seq 1 120); do
    if [ "$(adb_target shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; then
      booted=1
      break
    fi
    sleep 2
  done
  [ "$booted" -eq 1 ] || die "再起動後の boot 完了を待てませんでした ($ANDROID_SERIAL)"

  service_found=0
  for _ in $(seq 1 60); do
    if adb_target shell dumpsys activity services "$APP_ID" 2>/dev/null | grep -q 'ProximityService'; then
      service_found=1
      break
    fi
    sleep 2
  done
  [ "$service_found" -eq 1 ] || die "再起動後に ProximityService が復帰していません"
  BOOT_CHECK="PASSED (ProximityService running after reboot)"
else
  info "==> boot restore check SKIPPED (non-emulator device: $ANDROID_SERIAL; 実機は再起動しません)"
  BOOT_CHECK="SKIPPED (non-emulator: $ANDROID_SERIAL)"
fi

info ""
info "NATIVE TESTS OK"
info "device:              $ANDROID_SERIAL"
info "jvm tests:           $JVM_TESTS (failures=0 errors=0)"
info "instrumented tests:  $INSTRUMENTED_TESTS (failures=0 errors=0, $INSTRUMENTED_FILES result file(s))"
info "boot restore:        $BOOT_CHECK"
