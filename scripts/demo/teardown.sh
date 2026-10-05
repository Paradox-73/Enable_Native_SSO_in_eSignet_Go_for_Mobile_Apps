#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Stops what the demo started and checks that nothing of it is left.
#
#   bash scripts/demo/teardown.sh                 stack and emulator
#   bash scripts/demo/teardown.sh --stack-only    only the stack (containers and volumes)
#   bash scripts/demo/teardown.sh --device-only   only the emulator
#
# - Stack: `deploy/stack.sh down` (deletes the volumes), then checks that no container of the compose
#   project ps03-native-sso runs.
# - Emulator: removes the adb reverse ports and kills emulator-<PS03_EMULATOR_PORT> (default 5584) when
#   it is running; checks it is gone.
# - Does not touch lock directories: the script that took a lock releases it. Never removes anyone
#   else's lock. Prints `teardown ok`, or `teardown FAILED: <what is left>` with exit status 1.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
PS03_SCRIPT_NAME=teardown

STACK=1 DEVICE=1
case ${1:-} in
  "") ;; --stack-only) DEVICE=0 ;; --device-only) STACK=0 ;;
  *) ps03_usage ;;
esac
left=()
if [ $STACK -eq 1 ]; then
  bash "$PS03_ROOT/deploy/stack.sh" down || left+=("stack down failed")
  running=$(docker ps -q --filter label=com.docker.compose.project=ps03-native-sso 2>/dev/null || true)
  [ -z "$running" ] || left+=("containers of ps03-native-sso still run")
fi
if [ $DEVICE -eq 1 ]; then
  SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
  if [ -n "$SDK" ] && [ -x "$SDK/platform-tools/adb" ]; then
    ADB=$SDK/platform-tools/adb
    serial=emulator-${PS03_EMULATOR_PORT:-5584}
    if "$ADB" devices | grep -q "^$serial"; then
      MSYS_NO_PATHCONV=1 "$ADB" -s "$serial" reverse --remove-all >/dev/null 2>&1 || true
      MSYS_NO_PATHCONV=1 "$ADB" -s "$serial" shell am clear-debug-app >/dev/null 2>&1 || true
      "$ADB" -s "$serial" emu kill >/dev/null 2>&1 || true
      i=0
      while [ $i -lt 30 ] && "$ADB" devices | grep -q "^$serial"; do sleep 2; i=$((i + 1)); done
      ! "$ADB" devices | grep -q "^$serial" || left+=("emulator $serial still listed")
    fi
  fi
fi
if [ ${#left[@]} -eq 0 ]; then echo "teardown ok"; else echo "teardown FAILED: ${left[*]}"; exit 1; fi
