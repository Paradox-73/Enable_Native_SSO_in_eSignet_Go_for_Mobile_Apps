#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Starts the local stack, checks it, runs one command, and always stops the stack again.
#
#   bash scripts/with-stack.sh <command...>
#   bash scripts/with-stack.sh bash e2e/run.sh -run 'TestE2E_FAPI2Login|TestE2E_PSUTProbe'
#
# - Holds the docker-stack lock for the whole run (shared with other projects on this machine).
# - Runs `deploy/stack.sh up` and `deploy/stack.sh smoke` (prints `stack smoke: PASS`), then the
#   command, then `docker compose down -v` even when a step fails or the run is interrupted.
# - Set PS03_KEEP_STACK=1 to leave the stack running afterwards (the lock is still released).
# - Exit status: the command's, or the first failing step's.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ $# -ge 1 ] || ps03_usage
case $1 in -h|--help) ps03_usage ;; esac

run_with_stack() {
  local rc=0
  # shellcheck disable=SC2317 # called through the trap
  stop_stack() {
    if [ "${PS03_KEEP_STACK:-0}" = 1 ]; then ps03_log "PS03_KEEP_STACK=1: leaving the stack running"
    else bash "$PS03_ROOT/deploy/stack.sh" down || ps03_warn "stack down failed"; fi
  }
  trap 'stop_stack; ps03_lock_release docker-stack; exit 130' INT TERM
  bash "$PS03_ROOT/deploy/stack.sh" up || rc=$?
  [ $rc -eq 0 ] && { bash "$PS03_ROOT/deploy/stack.sh" smoke || rc=$?; }
  [ $rc -eq 0 ] && { "$@" || rc=$?; }
  stop_stack
  trap - INT TERM
  return $rc
}

ps03_with_lock docker-stack run_with_stack "$@"
