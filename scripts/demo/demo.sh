#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# One command that prepares the whole Native SSO demo and, with --check, proves it and tears it down.
#
#   bash scripts/demo/demo.sh [--check] [--dry-run] [--no-emulator] [--skip-build]
#
# Steps, in this order (heavy steps: APKs first, then the stack, then the emulator):
#   1. build   Hub and Health Card debug APKs, check both carry one signing certificate
#   2. stack   eSignet (with Thunder), Postgres, mock identity system, login pages, from clean volumes
#   3. server  headless run of the Native SSO flow against the stack (login, device secret, exchange,
#              re-consent signal): the e2e tests named in SERVER_TESTS below
#   4. device  emulator, install both apps, register both clients with their device keys, open Hub
# Options:
#   --check        verify each step, print `demo ready`, then tear everything down and print `teardown ok`
#   --dry-run      print the steps and the commands, run nothing
#   --no-emulator  stop after step 3 (prints `demo ready (server part only; emulator steps NOT RUN)`)
#   --skip-build   reuse the APKs already built (step 1 only checks they exist)
# Without --check the stack and a visible emulator stay up until you press Enter or Ctrl-C; the
# runbook is docs/demo-script.md. Environment: JAVA_HOME (17+), ANDROID_HOME, and the variables of
# scripts/lib.sh. PS03_AVD and PS03_EMULATOR_PORT (default ps03_api34, 5584) pick the emulator.
# - Takes the `docker-stack` lock and the `emulator` lock. It never removes a lock: when the emulator
#   lock directory exists it stops and tells you which directory to remove after checking nobody uses it.
# - A caller that already holds docker-stack (for example `with_lock docker-stack`) sets
#   PS03_HELD_LOCKS=:docker-stack, exactly as for scripts/with-stack.sh.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
PS03_SCRIPT_NAME=demo
# A lock is never treated as stale here: a leftover lock is reported, not removed.
export PS03_LOCK_STALE_MIN=100000000

CHECK=0 DRY=0 EMU=1 BUILD=1
for a in "$@"; do
  case $a in
    --check) CHECK=1 ;; --dry-run) DRY=1 ;; --no-emulator) EMU=0 ;; --skip-build) BUILD=0 ;;
    -h|--help) ps03_usage ;; *) ps03_die "unknown option $a" ;;
  esac
done

DEMO_DIR=$PS03_ROOT/scripts/demo
SERVER_TESTS='^(TestE2E_SuperAppLogin_IssuesDeviceSecret|TestE2E_Exchange_ChildTokens|TestE2E_Exchange_NarrowerScope|TestE2E_Exchange_ScopeSuperset)$'
SERIAL=emulator-${PS03_EMULATOR_PORT:-5584}
SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
t_start=$(date +%s)
step() { ps03_log "step $1: $2 ($(( $(date +%s) - t_start )) s since start)"; }
run() { if [ $DRY -eq 1 ]; then echo "  would run: $*" >&2; else "$@"; fi; }

if [ $DRY -eq 1 ]; then
  ps03_log "dry run: nothing is built, started or changed"
  PS03_DRY_PLAN=1
fi

preflight() {
  [ $DRY -eq 1 ] && return 0
  command -v docker >/dev/null || ps03_die "docker not found"
  docker info >/dev/null 2>&1 || ps03_die "Docker is not running"
  [ -n "${JAVA_HOME:-}" ] || ps03_die "set JAVA_HOME to a JDK 17 or newer"
  if [ $BUILD -eq 1 ] || [ $EMU -eq 1 ]; then
    [ -n "$SDK" ] && [ -d "$SDK" ] || ps03_die "set ANDROID_HOME to the Android SDK"
  fi
  if [ $EMU -eq 1 ] && ! ps03_lock_held emulator && [ -d "$PS03_LOCK_DIR/emulator" ]; then
    ps03_die "the emulator lock $PS03_LOCK_DIR/emulator exists (created $(date -r "$PS03_LOCK_DIR/emulator" '+%F %T')). Another job may be using the emulator. If none is, remove that one directory yourself (rmdir) and run this again; this script never removes it. Use --no-emulator to run the server part meanwhile."
  fi
}

build_apks() {
  step 1 "build Hub and Health Card, check one signer"
  if [ $BUILD -eq 1 ]; then
    run bash -c "cd '$PS03_ROOT/android' && bash gradlew --no-daemon -q :superapp:assembleDebug :childapp:assembleDebug"
  fi
  if [ $DRY -eq 0 ]; then
    for app in superapp childapp; do
      [ -f "$PS03_ROOT/android/$app/build/outputs/apk/debug/$app-debug.apk" ] || ps03_die "missing $app APK; run without --skip-build"
    done
  fi
  run bash "$PS03_ROOT/android/scripts/verify-family-signing.sh" debug
}

server_part() {
  step 2 "stack from clean volumes"
  run bash "$PS03_ROOT/deploy/stack.sh" down
  run bash "$PS03_ROOT/deploy/stack.sh" up
  run bash "$PS03_ROOT/deploy/stack.sh" smoke
  step 3 "headless Native SSO flow against the stack"
  run bash "$PS03_ROOT/e2e/run.sh" -run "$SERVER_TESTS"
}

device_part() {
  step 4 "emulator, apps, clients, Hub in front"
  if [ $DRY -eq 1 ]; then
    echo "  would run: bash scripts/demo/prepare-device.sh   (inside a headless emulator with --check, a visible one otherwise)" >&2
    return 0
  fi
  if [ $CHECK -eq 1 ]; then
    PS03_HELD_LOCKS="${PS03_HELD_LOCKS:-}" bash "$PS03_ROOT/android/scripts/with-emulator.sh" bash "$DEMO_DIR/prepare-device.sh" --verify
  else
    live_emulator
  fi
}

# Visible emulator for the live demo: holds the emulator lock until the presenter ends the demo.
live_emulator() {
  local emu_pid="" rc=0 log
  ps03_lock_held emulator || ps03_lock_acquire emulator
  # shellcheck disable=SC2317 # called through the trap
  stop_live() { bash "$DEMO_DIR/teardown.sh" --device-only || true; ps03_lock_held emulator && ps03_lock_release emulator || true; }
  trap 'stop_live; exit 130' INT TERM
  log=$PS03_RUN_DIR/logs/demo-emulator-$(date +%Y%m%d-%H%M%S).log
  mkdir -p "$PS03_RUN_DIR/logs"
  "$SDK/emulator/emulator" -avd "${PS03_AVD:-ps03_api34}" -port "${PS03_EMULATOR_PORT:-5584}" -no-audio -gpu host \
    -memory 2048 -no-snapshot-save >"$log" 2>&1 &
  emu_pid=$!
  ANDROID_SERIAL=$SERIAL bash "$DEMO_DIR/prepare-device.sh" --boot || rc=$?
  if [ $rc -eq 0 ]; then
    echo "demo ready"
    ps03_log "stack and emulator stay up. Follow docs/demo-script.md. Press Enter or Ctrl-C to tear down."
    read -r _ || true
  fi
  kill "$emu_pid" 2>/dev/null || true
  stop_live
  trap - INT TERM
  return $rc
}

all_steps() {
  local rc=0
  build_apks || rc=$?
  if [ $rc -eq 0 ]; then server_part || rc=$?; fi
  if [ $rc -eq 0 ] && [ $EMU -eq 1 ]; then device_part || rc=$?; fi
  if [ $rc -eq 0 ] && [ $DRY -eq 0 ] && { [ $CHECK -eq 1 ] || [ $EMU -eq 0 ]; }; then
    if [ $EMU -eq 1 ]; then echo "demo ready"; else echo "demo ready (server part only; emulator steps NOT RUN)"; fi
  fi
  if [ "${PS03_KEEP_STACK:-0}" = 1 ]; then ps03_log "PS03_KEEP_STACK=1: leaving the stack running"
  else run bash "$DEMO_DIR/teardown.sh" --stack-only || rc=$?; fi
  return $rc
}

preflight
if [ $DRY -eq 1 ]; then all_steps; echo "demo dry-run: OK"; exit 0; fi
rc=0
ps03_with_lock docker-stack all_steps || rc=$?
[ $rc -eq 0 ] || ps03_die "demo failed (exit $rc)"
ps03_log "finished in $(( $(date +%s) - t_start )) s"
