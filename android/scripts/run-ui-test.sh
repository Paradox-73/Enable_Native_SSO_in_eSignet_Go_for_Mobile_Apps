#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Builds, installs and runs one instrumented UI test class on the connected emulator.
#
#   bash android/scripts/run-ui-test.sh <superapp|childapp> <TestClass>
#   bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh superapp SuperAppSignInTest
#
# - Builds `:<app>:assembleDebug :<app>:assembleDebugAndroidTest` (skip with PS03_SKIP_BUILD=1; needs
#   JAVA_HOME 17+), installs both APKs (uninstalls first when the signature differs), registers the
#   app with eSignet (register-client.sh), then runs the class with `am instrument -w`.
# - Instrumentation arguments from the environment: PS03_TEST_UIN, PS03_TEST_OTP, PS03_TEST_NAME.
# - Prints the instrumentation output (it ends with `OK (1 test)` when the test passes). Logcat and
#   the test's last screenshot and window dump go to $PS03_RUN_DIR/evidence/ui-<app>-<time>/.
# - Uses ANDROID_SERIAL when set. Exit status 0 only when the output has `OK (`.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
PS03_SCRIPT_NAME=run-ui-test

case ${1:-} in -h|--help|"") ps03_usage ;; esac
[ $# -eq 2 ] || ps03_usage
APP=$1
CLASS=$2
case $APP in superapp|childapp) ;; *) ps03_die "unknown app $APP (superapp or childapp)" 2 ;; esac
PACKAGE=org.badmosip.nativesso.$APP
ANDROID_DIR=$PS03_ROOT/android
SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
ADB=${SDK:+$SDK/platform-tools/}adb
adb_() { MSYS_NO_PATHCONV=1 "$ADB" ${ANDROID_SERIAL:+-s "$ANDROID_SERIAL"} "$@"; }

out=$PS03_RUN_DIR/evidence/ui-$APP-$(date +%Y%m%d-%H%M%S)
mkdir -p "$out"

if [ "${PS03_SKIP_BUILD:-0}" != 1 ]; then
  ps03_log "building $APP and its test APK"
  (cd "$ANDROID_DIR" && bash gradlew --no-daemon -q ":$APP:assembleDebug" ":$APP:assembleDebugAndroidTest") >&2
fi
apk=$ANDROID_DIR/$APP/build/outputs/apk/debug/$APP-debug.apk
test_apk=$ANDROID_DIR/$APP/build/outputs/apk/androidTest/debug/$APP-debug-androidTest.apk
[ -f "$apk" ] && [ -f "$test_apk" ] || ps03_die "APKs missing; build first"

install_apk() {
  local file=$1 pkg=$2 result
  result=$(adb_ install -r -t "$(ps03_host_path "$file")" 2>&1 || true)
  if printf '%s' "$result" | grep -q 'INSTALL_FAILED_UPDATE_INCOMPATIBLE\|INSTALL_FAILED_DUPLICATE_PERMISSION'; then
    ps03_warn "$pkg is installed with another certificate; uninstalling it first"
    adb_ uninstall "$pkg" >/dev/null 2>&1 || true
    result=$(adb_ install -r -t "$(ps03_host_path "$file")" 2>&1 || true)
  fi
  printf '%s' "$result" | grep -q Success || ps03_die "install of $file failed: $result"
}
install_apk "$apk" "$PACKAGE"
install_apk "$test_apk" "$PACKAGE.test"
ps03_log "installed $PACKAGE and $PACKAGE.test"

bash "$PS03_ROOT/android/scripts/register-client.sh" "$APP"

args=(-e class "$PACKAGE.$CLASS")
[ -n "${PS03_TEST_UIN:-}" ] && args+=(-e uin "$PS03_TEST_UIN")
[ -n "${PS03_TEST_OTP:-}" ] && args+=(-e otp "$PS03_TEST_OTP")
# adb shell joins its arguments into one device command line: quote values with spaces.
[ -n "${PS03_TEST_NAME:-}" ] && args+=(-e expectedName "'$PS03_TEST_NAME'")

adb_ logcat -c || true
adb_ shell rm -rf "/sdcard/Android/data/$PACKAGE/files/ui-test" >/dev/null 2>&1 || true
ps03_log "running $PACKAGE.$CLASS"
result=$(adb_ shell am instrument -w "${args[@]}" "$PACKAGE.test/androidx.test.runner.AndroidJUnitRunner" 2>&1 | tr -d '\r' || true)
printf '%s\n' "$result"

adb_ logcat -d -v time >"$out/logcat.txt" 2>/dev/null || true
adb_ pull "/sdcard/Android/data/$PACKAGE/files/ui-test" "$(ps03_host_path "$out")" >/dev/null 2>&1 || true
grep -E 'PS03Hub|PS03HubTest|AndroidRuntime' "$out/logcat.txt" >"$out/app-log.txt" 2>/dev/null || true
printf '%s\n' "$result" >"$out/instrumentation.txt"
ps03_log "evidence: $out"

printf '%s\n' "$result" | grep -q '^OK (' || exit 1
