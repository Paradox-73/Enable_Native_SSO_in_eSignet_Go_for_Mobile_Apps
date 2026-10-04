#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs the live e2e tests (build tag e2e) in a Go container on the stack's compose network.
# The stack must be up: run it through scripts/with-stack.sh.
#
#   bash scripts/with-stack.sh bash e2e/run.sh [go test flags...]
#   bash scripts/with-stack.sh bash e2e/run.sh -run 'TestE2E_FAPI2Login|TestE2E_PSUTProbe'
#
# - The client keeps the issuer http://localhost:38088 in every URL, iss, htu and aud, and dials
#   esignet:8080 for it (E2E_DIAL_OVERRIDE), so tokens look the same as for the Android apps.
# - Prints the go test -v output, then `e2e: N passed, M failed`. Full log:
#   $PS03_RUN_DIR/logs/e2e-<time>.log
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib.sh"
PS03_SCRIPT_NAME=e2e

case ${1:-} in -h|--help) ps03_usage ;; esac
NETWORK=${PS03_STACK_NETWORK:-ps03-native-sso_default}
docker network inspect "$NETWORK" >/dev/null 2>&1 || ps03_die "network $NETWORK not found: start the stack (scripts/with-stack.sh)"

mkdir -p "$PS03_RUN_DIR/logs"
log=$PS03_RUN_DIR/logs/e2e-$(date +%Y%m%d-%H%M%S).log
rc=0
bash "$PS03_ROOT/scripts/go.sh" --network "$NETWORK" \
  -e E2E_ISSUER=http://localhost:38088 -e E2E_DIAL_OVERRIDE=localhost:38088=esignet:8080 \
  e2e test -tags e2e -count=1 -v "$@" ./... 2>&1 | tee "$log" || rc=$?

passed=$(grep -c -- '^--- PASS: ' "$log" || true)
failed=$(grep -c -- '^--- FAIL: ' "$log" || true)
echo "e2e: $passed passed, $failed failed"
ps03_log "log: $log"
if [ "$rc" -ne 0 ] || [ "$failed" -ne 0 ] || [ "$passed" -eq 0 ]; then exit 1; fi
