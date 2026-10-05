#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Puts the emulator into the starting state of the demo: both apps installed with a clean state, both
# registered as eSignet clients with their own device keys, Hub in front.
#
#   bash scripts/demo/prepare-device.sh --verify   emulator already booted (run inside android/scripts/with-emulator.sh)
#   bash scripts/demo/prepare-device.sh --boot     emulator process already started: wait for boot, then prepare
#
# - Needs the stack up (scripts/demo/demo.sh does it), ANDROID_HOME, and the APKs built by
#   `bash gradlew assembleDebug` in android/. Uses ANDROID_SERIAL (default emulator-5584).
# - --boot also does what android/scripts/with-emulator.sh does for a headless run: waits for boot,
#   turns animations off, forwards ports 38088 and 38300 (adb reverse), skips Chrome first-run screens.
# - Steps: install both APKs (uninstalls first when a different certificate is installed), clear both
#   apps' data, register Hub and Health Card (android/scripts/register-client.sh), open Hub and wait
#   until it is the resumed activity. Prints `device ready: Hub in front, 2 clients registered`.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
PS03_SCRIPT_NAME=prepare-device

MODE=${1:-}
case $MODE in --verify|--boot) ;; *) ps03_usage ;; esac
SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
[ -n "$SDK" ] && [ -d "$SDK" ] || ps03_die "set ANDROID_HOME to the Android SDK"
ADB=$SDK/platform-tools/adb
export ANDROID_SERIAL=${ANDROID_SERIAL:-emulator-${PS03_EMULATOR_PORT:-5584}}
adb_() { MSYS_NO_PATHCONV=1 "$ADB" -s "$ANDROID_SERIAL" "$@"; }
HUB=org.badmosip.nativesso.superapp
CARD=org.badmosip.nativesso.childapp

if [ "$MODE" = --boot ]; then
  waited=0
  until [ "$(adb_ shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = 1 ]; do
    [ $waited -ge "${PS03_BOOT_TIMEOUT:-300}" ] && ps03_die "no boot after $waited s"
    sleep 3; waited=$((waited + 3))
  done
  ps03_log "booted in $waited s"
  adb_ shell input keyevent 82 >/dev/null 2>&1 || true
  for s in window_animation_scale transition_animation_scale animator_duration_scale; do
    adb_ shell settings put global "$s" 0 >/dev/null 2>&1 || true
  done
  adb_ reverse tcp:38088 tcp:38088 >/dev/null
  adb_ reverse tcp:38300 tcp:38300 >/dev/null
  adb_ shell 'echo "_ --disable-fre --no-default-browser-check --no-first-run" > /data/local/tmp/chrome-command-line'
  adb_ shell am set-debug-app --persistent com.android.chrome >/dev/null
fi

install_apk() {
  local app=$1 pkg=$2 apk result
  apk=$PS03_ROOT/android/$app/build/outputs/apk/debug/$app-debug.apk
  [ -f "$apk" ] || ps03_die "missing $apk: build the apps first"
  result=$(adb_ install -r -t "$(ps03_host_path "$apk")" 2>&1 || true)
  if printf '%s' "$result" | grep -q 'INSTALL_FAILED_UPDATE_INCOMPATIBLE\|INSTALL_FAILED_DUPLICATE_PERMISSION'; then
    ps03_warn "$pkg is installed with another certificate; uninstalling it first"
    adb_ uninstall "$pkg" >/dev/null 2>&1 || true
    result=$(adb_ install -r -t "$(ps03_host_path "$apk")" 2>&1 || true)
  fi
  printf '%s' "$result" | grep -q Success || ps03_die "install of $apk failed: $result"
}

install_apk superapp "$HUB"
install_apk childapp "$CARD"
for pkg in "$HUB" "$CARD"; do
  adb_ shell pm list packages "$pkg" | tr -d '\r' | grep -qx "package:$pkg" || ps03_die "$pkg is not installed"
  adb_ shell pm clear "$pkg" >/dev/null
done
ps03_log "both apps installed, data cleared"

bash "$PS03_ROOT/android/scripts/verify-family-signing.sh" debug >/dev/null || ps03_die "Hub and Health Card are not signed with one certificate"
bash "$PS03_ROOT/android/scripts/register-client.sh" superapp
bash "$PS03_ROOT/android/scripts/register-client.sh" childapp

adb_ shell am force-stop "$CARD" >/dev/null 2>&1 || true
adb_ shell am start -n "$HUB/.MainActivity" >/dev/null
n=0
until adb_ shell dumpsys activity activities 2>/dev/null | tr -d '\r' | grep -E 'topResumedActivity|mResumedActivity' | grep -q "$HUB"; do
  [ $n -ge 15 ] && ps03_die "Hub is not in front after 30 s"
  sleep 2; n=$((n + 1))
done
echo "device ready: Hub in front, 2 clients registered"
