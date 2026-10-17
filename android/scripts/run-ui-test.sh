#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Builds, installs and runs instrumented UI tests on the connected emulator.
#
#   bash android/scripts/run-ui-test.sh <superapp|childapp> <TestClass>
#   bash android/scripts/run-ui-test.sh all
#   bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all
#
# - `all` runs, in this order: superapp SuperAppSignInTest (Hub signs in with sharing on, so it holds
#   a device secret), childapp ChildAppSilentSignInTest, childapp ChildAppConsentOnlyTest, childapp
#   ChildAppReconsentTest, childapp ChildAppSignOutTest (it signs Hub out), childapp
#   ChildAppHubMissingTest (last: it uninstalls Hub; the script installs Hub again afterwards). The
#   Health Card tests need the Hub sign-in of the first one. Prints `OK (6 tests)` when all pass.
# - Builds `:<app>:assembleDebug :<app>:assembleDebugAndroidTest` (skip with PS03_SKIP_BUILD=1; needs
#   JAVA_HOME 17+), installs both APKs (uninstalls first when the signature differs), registers the
#   app with eSignet (register-client.sh), then runs the class with `am instrument -w`.
# - Instrumentation arguments from the environment: PS03_TEST_UIN, PS03_TEST_OTP, PS03_TEST_NAME,
#   PS03_TEST_EMAIL.
# - Prints the instrumentation output (it ends with `OK (1 test)` when the test passes). Logcat and
#   the test's last screenshot and window dump go to $PS03_RUN_DIR/evidence/ui-<app>-<time>/; the
#   screenshots a test saves on purpose also go to $PS03_RUN_DIR/evidence/<date>/screens/.
# - Uses ANDROID_SERIAL when set. Exit status 0 only when every run printed `OK (`.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
PS03_SCRIPT_NAME=run-ui-test

case ${1:-} in -h|--help|"") ps03_usage ;; esac
ANDROID_DIR=$PS03_ROOT/android
screens=$PS03_RUN_DIR/evidence/$(date +%Y-%m-%d)/screens

build() {
  [ "${PS03_SKIP_BUILD:-0}" = 1 ] && return 0
  local tasks=() app
  for app in "$@"; do tasks+=(":$app:assembleDebug" ":$app:assembleDebugAndroidTest"); done
  ps03_log "building $* and the test APKs"
  (cd "$ANDROID_DIR" && bash gradlew --no-daemon -q "${tasks[@]}") >&2
}

# run_one <app> <TestClass>: install, register, run; returns 0 only when the output has `OK (`.
run_one() {
  local app=$1 class=$2 package out apk test_apk result
  package=org.badmosip.nativesso.$app
  out=$PS03_RUN_DIR/evidence/ui-$app-$(date +%Y%m%d-%H%M%S)
  mkdir -p "$out"
  apk=$ANDROID_DIR/$app/build/outputs/apk/debug/$app-debug.apk
  test_apk=$ANDROID_DIR/$app/build/outputs/apk/androidTest/debug/$app-debug-androidTest.apk
  [ -f "$apk" ] && [ -f "$test_apk" ] || ps03_die "APKs missing; build first"
  ps03_install_apk "$apk" "$package"
  ps03_install_apk "$test_apk" "$package.test"
  ps03_log "installed $package and $package.test"

  bash "$PS03_ROOT/android/scripts/register-client.sh" "$app"

  local args=(-e class "$package.$class")
  [ -n "${PS03_TEST_UIN:-}" ] && args+=(-e uin "$PS03_TEST_UIN")
  [ -n "${PS03_TEST_OTP:-}" ] && args+=(-e otp "$PS03_TEST_OTP")
  # adb shell joins its arguments into one device command line: quote values with spaces.
  [ -n "${PS03_TEST_NAME:-}" ] && args+=(-e expectedName "'$PS03_TEST_NAME'")
  [ -n "${PS03_TEST_EMAIL:-}" ] && args+=(-e expectedEmail "$PS03_TEST_EMAIL")

  ps03_adb logcat -c || true
  # A busy emulator may show a system "isn't responding" dialog over the app; close it before the test.
  ps03_adb shell am broadcast -a android.intent.action.CLOSE_SYSTEM_DIALOGS >/dev/null 2>&1 || true
  ps03_adb shell rm -rf "/sdcard/Android/data/$package/files/ui-test" >/dev/null 2>&1 || true
  ps03_log "running $package.$class"
  result=$(ps03_adb shell am instrument -w "${args[@]}" "$package.test/androidx.test.runner.AndroidJUnitRunner" 2>&1 | tr -d '\r' || true)
  printf '%s\n' "$result"

  ps03_adb logcat -d -v time >"$out/logcat.txt" 2>/dev/null || true
  ps03_adb pull "/sdcard/Android/data/$package/files/ui-test" "$(ps03_host_path "$out")" >/dev/null 2>&1 || true
  if [ -d "$out/ui-test/screens" ]; then
    mkdir -p "$screens"
    cp "$out/ui-test/screens/"*.png "$screens/" 2>/dev/null || true
  fi
  grep -E 'PS03Hub|PS03HubTest|PS03Card|PS03CardTest|AndroidRuntime' "$out/logcat.txt" >"$out/app-log.txt" 2>/dev/null || true
  printf '%s\n' "$result" >"$out/instrumentation.txt"
  ps03_log "evidence: $out"

  printf '%s\n' "$result" | grep -q '^OK ('
}

if [ "$1" = all ]; then
  [ $# -eq 1 ] || ps03_usage
  build superapp childapp
  passed=0
  failed=()
  pairs=(superapp:SuperAppSignInTest childapp:ChildAppSilentSignInTest childapp:ChildAppConsentOnlyTest childapp:ChildAppReconsentTest childapp:ChildAppSignOutTest childapp:ChildAppHubMissingTest)
  for pair in "${pairs[@]}"; do
    if run_one "${pair%%:*}" "${pair#*:}"; then
      passed=$((passed + 1))
    else
      failed+=("${pair#*:}")
    fi
  done
  # ChildAppHubMissingTest uninstalled Hub: put it back, signed out, for the next run.
  ps03_install_apk "$ANDROID_DIR/superapp/build/outputs/apk/debug/superapp-debug.apk" org.badmosip.nativesso.superapp
  ps03_log "screenshots: $screens"
  if [ ${#failed[@]} -eq 0 ]; then
    echo "OK ($passed tests)"
  else
    echo "FAILED: ${failed[*]} ($passed of ${#pairs[@]} passed)"
    exit 1
  fi
  exit 0
fi

[ $# -eq 2 ] || ps03_usage
case $1 in superapp|childapp) ;; *) ps03_die "unknown app $1 (superapp or childapp)" 2 ;; esac
build "$1"
run_one "$1" "$2"
