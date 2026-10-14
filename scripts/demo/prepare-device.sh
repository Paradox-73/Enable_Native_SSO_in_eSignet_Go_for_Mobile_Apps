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
# - --boot also waits for boot, then prepares the emulator as android/scripts/with-emulator.sh does
#   (ps03_emulator_prepare in scripts/lib.sh).
# - Steps: install both APKs (uninstalls first when a different certificate is installed), clear both
#   apps' data, register Hub and Health Card (android/scripts/register-client.sh), open Hub and wait
#   until it is the resumed activity. Prints `device ready: Hub in front, 2 clients registered`.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
PS03_SCRIPT_NAME=prepare-device

MODE=${1:-}
case $MODE in --verify|--boot) ;; *) ps03_usage ;; esac
[ -n "$PS03_SDK" ] && [ -d "$PS03_SDK" ] || ps03_die "set ANDROID_HOME to the Android SDK"
export ANDROID_SERIAL=${ANDROID_SERIAL:-emulator-${PS03_EMULATOR_PORT:-5584}}
HUB=org.badmosip.nativesso.superapp
CARD=org.badmosip.nativesso.childapp

if [ "$MODE" = --boot ]; then
  waited=0
  until [ "$(ps03_adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = 1 ]; do
    [ $waited -ge "${PS03_BOOT_TIMEOUT:-300}" ] && ps03_die "no boot after $waited s"
    sleep 3; waited=$((waited + 3))
  done
  ps03_log "booted in $waited s"
  ps03_emulator_prepare
fi

for app in superapp childapp; do
  ps03_install_apk "$PS03_ROOT/android/$app/build/outputs/apk/debug/$app-debug.apk" "org.badmosip.nativesso.$app"
done
for pkg in "$HUB" "$CARD"; do
  ps03_adb shell pm list packages "$pkg" | tr -d '\r' | grep -qx "package:$pkg" || ps03_die "$pkg is not installed"
  ps03_adb shell pm clear "$pkg" >/dev/null
done
ps03_log "both apps installed, data cleared"

bash "$PS03_ROOT/android/scripts/verify-family-signing.sh" debug >/dev/null || ps03_die "Hub and Health Card are not signed with one certificate"
bash "$PS03_ROOT/android/scripts/register-client.sh" superapp
bash "$PS03_ROOT/android/scripts/register-client.sh" childapp

ps03_adb shell am force-stop "$CARD" >/dev/null 2>&1 || true
ps03_adb shell am start -n "$HUB/.MainActivity" >/dev/null
n=0
until ps03_adb shell dumpsys activity activities 2>/dev/null | tr -d '\r' | grep -E 'topResumedActivity|mResumedActivity' | grep -q "$HUB"; do
  [ $n -ge 15 ] && ps03_die "Hub is not in front after 30 s"
  sleep 2; n=$((n + 1))
done
echo "device ready: Hub in front, 2 clients registered"
