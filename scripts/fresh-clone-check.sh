#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Proves that a clean clone of the committed repository builds and passes its checks.
#
#   bash scripts/fresh-clone-check.sh [--keep] [--no-android]
#
# - Clones the current repository (committed state only: uncommitted changes are not in it) into a new
#   folder under $PS03_RUN_DIR, with core.longpaths on, then runs inside the clone:
#     1. scripts/repo-lint.sh
#     2. Go unit tests in the container: Thunder `./internal/oauth/... ./pkg/...` and eSignet `./...`
#     3. Android JVM tests of the four modules (skipped with --no-android; needs JAVA_HOME and ANDROID_HOME)
# - The clone gets its own run folder ($PS03_RUN_DIR/fresh-clone-*/run), so nothing of the working copy leaks in:
#   the Android test certificate is made new, no upstream cache is reused.
# - Deletes the clone at the end unless --keep. Takes the same locks as the scripts it calls.
# - Prints `fresh-clone-check: PASS (<commit>)` or `fresh-clone-check: FAILED at step <n>: <name>`; exit status 1 on failure.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
PS03_SCRIPT_NAME=fresh-clone-check

KEEP=0 ANDROID=1
for a in "$@"; do
  case $a in --keep) KEEP=1 ;; --no-android) ANDROID=0 ;; -h|--help) ps03_usage ;; *) ps03_die "unknown option $a" ;; esac
done

commit=$(git -C "$PS03_ROOT" rev-parse --short=7 HEAD)
base=$PS03_RUN_DIR/fresh-clone-$(date +%Y%m%d-%H%M%S)
clone=$base/repo
mkdir -p "$base"
# shellcheck disable=SC2317 # called through the trap
cleanup() { [ "$KEEP" = 1 ] && ps03_log "kept $base" || rm -rf "$base"; }
trap cleanup EXIT

ps03_log "cloning $commit into $clone"
git clone -q -c core.longpaths=true --no-hardlinks "$PS03_ROOT" "$clone"
git -C "$clone" checkout -q "$commit"
export PS03_RUN_DIR=$base/run
mkdir -p "$PS03_RUN_DIR"

step=0
run_step() {
  local name=$1; shift
  step=$((step + 1))
  ps03_log "step $step: $name"
  if ! "$@"; then
    echo "fresh-clone-check: FAILED at step $step: $name"
    exit 1
  fi
}

run_step "repo-lint" bash "$clone/scripts/repo-lint.sh"
run_step "Thunder unit tests" bash "$clone/scripts/go-test.sh" thunder/backend ./internal/oauth/... ./pkg/...
run_step "eSignet unit tests" bash "$clone/scripts/go-test.sh" esignet/esignet-service ./...
if [ $ANDROID -eq 1 ]; then
  run_step "Android JVM tests" bash -c "cd '$clone/android' && bash gradlew --no-daemon :oidc-client:test :secure-store:testDebugUnitTest :superapp:testDebugUnitTest :childapp:testDebugUnitTest"
else
  ps03_log "Android step skipped (--no-android)"
fi
echo "fresh-clone-check: PASS ($commit)"
