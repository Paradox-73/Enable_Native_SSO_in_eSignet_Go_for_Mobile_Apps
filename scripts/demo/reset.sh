#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Returns a running demo to its starting state between two rehearsals or takes, without a rebuild.
#
#   bash scripts/demo/reset.sh            stack from clean volumes, then both apps cleared and re-registered
#   bash scripts/demo/reset.sh --device   only the apps (clear data, register again, Hub in front)
#
# - Needs the emulator running (started by `bash scripts/demo/demo.sh`) and ANDROID_HOME. Uses the
#   `docker-stack` lock for the stack restart; set PS03_HELD_LOCKS=:docker-stack if you hold it already.
# - Clearing an app's data creates new device keys, so both clients get new client ids; the old ones
#   stay in the database until the stack is recreated, which the default mode does.
# - Prints `reset ok`.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
PS03_SCRIPT_NAME=reset
export PS03_LOCK_STALE_MIN=100000000

case ${1:-} in "") DEVICE_ONLY=0 ;; --device) DEVICE_ONLY=1 ;; *) ps03_usage ;; esac
if [ $DEVICE_ONLY -eq 0 ]; then
  fresh_stack() {
    bash "$PS03_ROOT/deploy/stack.sh" down
    bash "$PS03_ROOT/deploy/stack.sh" up
    bash "$PS03_ROOT/deploy/stack.sh" smoke
  }
  ps03_with_lock docker-stack fresh_stack
fi
bash "$PS03_ROOT/scripts/demo/prepare-device.sh" --verify
echo "reset ok"
