#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs the live e2e tests (build tag e2e) in a Go container on the stack's compose network.
# The stack must be up: run it through scripts/with-stack.sh.
#
#   bash scripts/with-stack.sh bash e2e/run.sh                 the whole suite, both phases
#   bash scripts/with-stack.sh bash e2e/run.sh -run 'TestE2E_FAPI2Login|TestE2E_PSUTProbe'
#
# - Without arguments: phase 1 (every test except the phase-2 ones), then eSignet is recreated with
#   a device secret lifetime of E2E_DEVICE_SECRET_VALIDITY_SECONDS (default 20 s), which also empties
#   its in-memory runtime store (with PS03_RUNTIME_STORE=redis, Redis is recreated empty as well), then
#   phase 2 (session ended, expired device secret). The Hub session
#   is handed over in a state file under $PS03_RUN_DIR/test-results, deleted at the end.
# - With arguments: one phase-1 run with those go test flags.
# - The client keeps the issuer http://localhost:38088 in every URL, iss, htu and aud, and dials
#   esignet:8080 for it (E2E_DIAL_OVERRIDE), so tokens look the same as for the Android apps.
# - Prints the go test -v output, then `e2e: N passed, M failed`. Full logs:
#   $PS03_RUN_DIR/logs/e2e-<time>-phase<N>.log; one JSON line per checked answer:
#   $PS03_RUN_DIR/evidence/e2e-<time>/evidence.jsonl
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib.sh"
PS03_SCRIPT_NAME=e2e

case ${1:-} in -h|--help) ps03_usage ;; esac
NETWORK=${PS03_STACK_NETWORK:-ps03-native-sso_default}
docker network inspect "$NETWORK" >/dev/null 2>&1 || ps03_die "network $NETWORK not found: start the stack (scripts/with-stack.sh)"

PHASE2_TESTS='^(TestE2E_Exchange_SessionEnded|TestE2E_Exchange_ExpiredDeviceSecret)$'
VALIDITY=${E2E_DEVICE_SECRET_VALIDITY_SECONDS:-20}
stamp=$(date +%Y%m%d-%H%M%S)
mkdir -p "$PS03_RUN_DIR/logs" "$PS03_RUN_DIR/evidence" "$PS03_RUN_DIR/test-results"
state=e2e-state-$stamp.json            # under $PS03_RUN_DIR/test-results, /results in the container
evidence=e2e-evidence-$stamp
logs=()

run_phase() {
  local phase=$1 rc=0 log; shift
  log=$PS03_RUN_DIR/logs/e2e-$stamp-phase$phase.log
  logs+=("$log")
  bash "$PS03_ROOT/scripts/go.sh" --network "$NETWORK" \
    -e E2E_ISSUER=http://localhost:38088 -e E2E_DIAL_OVERRIDE=localhost:38088=esignet:8080 \
    -e E2E_PHASE="$phase" -e E2E_STATE_FILE="/results/$state" -e E2E_EVIDENCE_DIR="/results/$evidence" \
    -e E2E_DEVICE_SECRET_VALIDITY_SECONDS="$VALIDITY" \
    e2e test -tags e2e -count=1 -timeout 20m -v "$@" ./... 2>&1 | tee "$log" || rc=$?
  return $rc
}

rc=0
if [ $# -gt 0 ]; then
  run_phase 1 "$@" || rc=$?
else
  run_phase 1 -skip "$PHASE2_TESTS" || rc=$?
  ps03_log "phase 2: recreating eSignet with a device secret lifetime of $VALIDITY s"
  # With the Redis store the sessions would survive an eSignet restart, so Redis is recreated too (it keeps
  # no data on disk); both stores then start empty, which phase 2 needs.
  restart=(esignet)
  [ "${PS03_RUNTIME_STORE:-inmemory}" = redis ] && restart=(redis esignet)
  PS03_DEVICE_SECRET_VALIDITY_SECONDS=$VALIDITY bash "$PS03_ROOT/deploy/stack.sh" restart "${restart[@]}" || rc=1
  [ $rc -eq 0 ] || ps03_warn "phase 1 or the restart failed; running phase 2 anyway"
  run_phase 2 -run "$PHASE2_TESTS" || rc=$?
fi
rm -f "$PS03_RUN_DIR/test-results/$state"
if [ -d "$PS03_RUN_DIR/test-results/$evidence" ]; then
  # On Linux the folder belongs to the container's root user, so it cannot be moved; copy it then.
  mv "$PS03_RUN_DIR/test-results/$evidence" "$PS03_RUN_DIR/evidence/e2e-$stamp" 2>/dev/null ||
    cp -R "$PS03_RUN_DIR/test-results/$evidence" "$PS03_RUN_DIR/evidence/e2e-$stamp"
  ps03_log "evidence: $PS03_RUN_DIR/evidence/e2e-$stamp/evidence.jsonl"
fi

passed=$(cat "${logs[@]}" | grep -c -- '^--- PASS: ' || true)
failed=$(cat "${logs[@]}" | grep -c -- '^--- FAIL: ' || true)
echo "e2e: $passed passed, $failed failed"
ps03_log "logs: ${logs[*]}"
if [ "$rc" -ne 0 ] || [ "$failed" -ne 0 ] || [ "$passed" -eq 0 ]; then exit 1; fi
