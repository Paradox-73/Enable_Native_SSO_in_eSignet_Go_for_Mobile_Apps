#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Starts the test emulator headless, prepares it, runs one command, and stops it again.
#
#   bash android/scripts/with-emulator.sh <command...>
#   bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh superapp SuperAppSignInTest
#
# - With PS03_LOCK_DIR set, holds the `emulator` lock there for the whole run. A caller
#   that already holds it (for example through the toolchain's `with_lock emulator`) sets
#   PS03_HELD_LOCKS=:emulator so this script does not wait for itself.
# - AVD `$PS03_AVD` (default ps03_api34) on console port `$PS03_EMULATOR_PORT` (default 5584, serial
#   emulator-5584), `-no-window -no-audio -no-boot-anim -gpu host`, RAM in MB from
#   `$PS03_EMULATOR_MEMORY` (default 2048); waits for boot and network.
# - `adb reverse` for the stack ports 38088 (eSignet) and 38300 (login pages), so the phone reaches
#   the issuer as http://localhost:38088, exactly as the laptop does.
# - Chrome: first-run screens off (command-line file plus `am set-debug-app`).
# - The command runs with ANDROID_SERIAL set. Afterwards: reverse ports removed, Chrome debug app
#   cleared, emulator killed (unless it was already running before this script, or PS03_KEEP_EMULATOR=1).
# - Needs ANDROID_HOME (or ANDROID_SDK_ROOT). Exit status: the command's, or the first failing step's.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
PS03_SCRIPT_NAME=with-emulator

[ $# -ge 1 ] || ps03_usage
case $1 in -h|--help) ps03_usage ;; esac

[ -n "$PS03_SDK" ] && [ -d "$PS03_SDK" ] || ps03_die "set ANDROID_HOME to the Android SDK"
AVD=${PS03_AVD:-ps03_api34}
PORT=${PS03_EMULATOR_PORT:-5584}
SERIAL=emulator-$PORT
BOOT_TIMEOUT=${PS03_BOOT_TIMEOUT:-300}
export ANDROID_SERIAL=$SERIAL

run_with_emulator() {
  local started=0 rc=0 emu_pid=""
  mkdir -p "$PS03_RUN_DIR/logs"
  # shellcheck disable=SC2317 # called through the trap
  teardown() {
    ps03_adb reverse --remove-all >/dev/null 2>&1 || true
    ps03_adb shell am clear-debug-app >/dev/null 2>&1 || true
    if [ "$started" = 1 ] && [ "${PS03_KEEP_EMULATOR:-0}" != 1 ]; then
      ps03_log "stopping $SERIAL"
      ps03_adb emu kill >/dev/null 2>&1 || true
      local i=0
      while [ $i -lt 30 ] && ps03_adb devices | grep -q "^$SERIAL"; do sleep 2; i=$((i + 1)); done
      [ -n "$emu_pid" ] && kill "$emu_pid" 2>/dev/null || true
    fi
  }
  trap 'teardown; ps03_lock_release emulator; exit 130' INT TERM

  ps03_adb start-server >/dev/null 2>&1 || true
  if ps03_adb devices | grep -q "^$SERIAL[[:space:]]*device"; then
    ps03_log "$SERIAL already running; using it and leaving it running"
  else
    local log="$PS03_RUN_DIR/logs/emulator-$(date +%Y%m%d-%H%M%S).log"
    ps03_log "starting AVD $AVD on port $PORT (log: $log)"
    "$PS03_SDK/emulator/emulator" -avd "$AVD" -port "$PORT" -no-window -no-audio -no-boot-anim -gpu host -memory "${PS03_EMULATOR_MEMORY:-2048}" \
      -no-snapshot-save >"$log" 2>&1 &
    emu_pid=$!
    started=1
    local waited=0
    until [ "$(ps03_adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = 1 ]; do
      kill -0 "$emu_pid" 2>/dev/null || { teardown; ps03_die "emulator exited during boot; see $log"; }
      [ $waited -ge "$BOOT_TIMEOUT" ] && { teardown; ps03_die "no boot after $BOOT_TIMEOUT s"; }
      sleep 3; waited=$((waited + 3))
    done
    ps03_log "booted in ${waited} s"
  fi

  # Right after boot the network can be missing for a while (docs/setup.md, troubleshooting).
  local n=0
  until ps03_adb shell 'ping -c 1 -W 2 8.8.8.8 >/dev/null 2>&1 || ip route | grep -q default' >/dev/null 2>&1; do
    [ $n -ge 30 ] && { ps03_warn "network still not up after 60 s; continuing"; break; }
    sleep 2; n=$((n + 1))
  done
  ps03_emulator_prepare
  ps03_log "$SERIAL ready: adb reverse 38088 and 38300, Chrome first-run off"

  "$@" || rc=$?
  teardown
  trap - INT TERM
  return $rc
}

ps03_with_lock emulator run_with_emulator "$@"
