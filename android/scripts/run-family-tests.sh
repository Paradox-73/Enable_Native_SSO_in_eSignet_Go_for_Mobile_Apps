#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Installs the APK sets from build-apks.sh and runs the family single sign-on steps on the connected
# emulator (no eSignet server needed: Hub's session is seeded by its debug-only receiver).
#
#   bash android/scripts/build-apks.sh
#   bash android/scripts/with-emulator.sh bash android/scripts/run-family-tests.sh
#
# Steps, each printing one line:
#   family-bind PASS                    family Health Card gets the bundle and a token-endpoint proof
#   htu-allow-list PASS                 a proof for a foreign URL is refused (ERROR_HTU_NOT_ALLOWED)
#   signed-out PASS (ERROR_NOT_SIGNED_IN)  Hub signed out: no bundle, no proof
#   intruder PASS (SecurityException)   debug-signed Health Card without the permission: bind refused
#   takeover PASS (rejected)            debug-signed Health Card that defined the permission while Hub
#                                       was absent holds it, yet Hub's per-call check refuses it
#     or takeover PASS (install refused: ...)  when Android will not install Hub next to that app
# - Uninstalls both apps first and leaves the family set installed at the end.
# - Uses ANDROID_SERIAL when set. Exit status 0 only when every step passes.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
PS03_SCRIPT_NAME=run-family-tests

case ${1:-} in -h|--help) ps03_usage ;; esac
SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
ADB=${SDK:+$SDK/platform-tools/}adb
adb_() { MSYS_NO_PATHCONV=1 "$ADB" ${ANDROID_SERIAL:+-s "$ANDROID_SERIAL"} "$@"; }
APKS=$PS03_RUN_DIR/apks
HUB=org.badmosip.nativesso.superapp
CHILD=org.badmosip.nativesso.childapp
for f in family/hub.apk family/child.apk family/child-test.apk intruder/child.apk intruder/child-test.apk \
  takeover/child.apk takeover/child-test.apk; do
  [ -f "$APKS/$f" ] || ps03_die "missing $APKS/$f; run android/scripts/build-apks.sh first"
done

out=$PS03_RUN_DIR/evidence/family-$(date +%Y%m%d-%H%M%S)
mkdir -p "$out"
failed=0

install() { adb_ install -r -t "$(ps03_host_path "$APKS/$1")" 2>&1 | tr -d '\r'; }
must_install() { local r; r=$(install "$1"); printf '%s' "$r" | grep -q Success || ps03_die "install of $1 failed: $r"; }
uninstall_all() { adb_ uninstall "$CHILD.test" >/dev/null 2>&1 || true; adb_ uninstall "$CHILD" >/dev/null 2>&1 || true; adb_ uninstall "$HUB" >/dev/null 2>&1 || true; }
seed() {
  local r
  r=$(adb_ shell am broadcast -f 32 -n "$HUB/.sso.DebugSessionSeeder" -e op "$1" 2>&1 | tr -d '\r')
  printf '%s' "$r" | grep -q 'result=1' || ps03_die "Hub seeder ($1) failed: $r"
}
# run_step <label> <test method> <pass text>
run_step() {
  local label=$1 method=$2 pass=$3 result
  result=$(adb_ shell am instrument -w -e class "$CHILD.FamilySsoBindTest#$method" \
    "$CHILD.test/androidx.test.runner.AndroidJUnitRunner" 2>&1 | tr -d '\r' || true)
  printf '%s\n' "$result" >"$out/$label.txt"
  if printf '%s\n' "$result" | grep -q '^OK ('; then
    echo "$label PASS$pass"
  else
    echo "$label FAILED (see $out/$label.txt)"
    failed=1
  fi
}

ps03_log "family set"
uninstall_all
must_install family/hub.apk
must_install family/child.apk
must_install family/child-test.apk
seed seed
run_step family-bind familyBind ""
run_step htu-allow-list htuAllowList ""
seed clear
run_step signed-out signedOut " (ERROR_NOT_SIGNED_IN)"

ps03_log "intruder set"
adb_ uninstall "$CHILD.test" >/dev/null 2>&1 || true
adb_ uninstall "$CHILD" >/dev/null 2>&1 || true
must_install intruder/child.apk
must_install intruder/child-test.apk
seed seed
run_step intruder intruderRefused " (SecurityException)"

ps03_log "takeover set: Hub absent while the other app defines the permission"
uninstall_all
must_install takeover/child.apk
must_install takeover/child-test.apk
# adb exits non-zero when Android refuses the install; that answer is checked below.
hub_result=$(install family/hub.apk || true)
if printf '%s' "$hub_result" | grep -q Success; then
  seed seed
  run_step takeover takeoverRefused " (rejected)"
else
  reason=$(printf '%s' "$hub_result" | grep -o 'INSTALL_FAILED_[A-Z_]*' | head -n 1)
  printf '%s\n' "$hub_result" >"$out/takeover.txt"
  if [ -n "$reason" ]; then echo "takeover PASS (install refused: $reason)"; else echo "takeover FAILED ($hub_result)"; failed=1; fi
fi

ps03_log "restoring the family set"
uninstall_all
must_install family/hub.apk
must_install family/child.apk
adb_ logcat -d -v time >"$out/logcat.txt" 2>/dev/null || true
grep -E 'PS03Hub|AndroidRuntime' "$out/logcat.txt" >"$out/app-log.txt" 2>/dev/null || true
ps03_log "evidence: $out"
exit $failed
