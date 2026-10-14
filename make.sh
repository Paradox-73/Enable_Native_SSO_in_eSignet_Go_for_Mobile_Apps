#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Short names for the commands of this repository. Each target runs one script from scripts/ (or the
# Android Gradle wrapper) and passes extra arguments on.
#
#   bash make.sh help
#   bash make.sh <target> [arguments]
#
# Targets:
#   lint          repository checks (scripts/repo-lint.sh)
#   test-go       unit tests of Thunder and eSignet with changed-line coverage
#   test-android  Android JVM tests of all four modules
#   stack-up      start the local stack and check it; stack-down stops it and deletes its volumes
#   e2e           stack up, the live e2e tests, stack down
#   newman        stack up, the Postman folder "Native SSO" under newman, stack down
#   demo          full demo with an emulator (needs the emulator lock to be free)
#   demo-check    server part of the demo, verified and torn down
#   evidence      run the checks and rewrite docs/test-report.md (scripts/evidence.sh --run)
#   report        rewrite docs/test-report.md from docs/test-evidence.tsv only
#   docs-check    link check of design.md and threat-model.md (and any files you add as arguments)
#   fresh-clone   clean clone, lint, Go unit tests, Android JVM tests
# - Run from any folder; needs bash (Git Bash on Windows). Exit status: the target's.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$here"

target=${1:-help}
[ $# -gt 0 ] && shift

case $target in
  lint)         exec bash scripts/repo-lint.sh "$@" ;;
  test-go)      bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover "$@"
                exec bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover "$@" ;;
  test-android) cd android
                exec bash gradlew --no-daemon :oidc-client:test :secure-store:testDebugUnitTest \
                  :superapp:testDebugUnitTest :childapp:testDebugUnitTest "$@" ;;
  stack-up)     bash deploy/stack.sh up && exec bash deploy/stack.sh smoke ;;
  stack-down)   exec bash deploy/stack.sh down ;;
  e2e)          exec bash scripts/with-stack.sh bash e2e/run.sh "$@" ;;
  newman)       exec bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh "$@" ;;
  demo)         exec bash scripts/demo/demo.sh "$@" ;;
  demo-check)   exec bash scripts/demo/demo.sh --no-emulator "$@" ;;
  evidence)     exec bash scripts/evidence.sh --run "$@" ;;
  report)       exec bash scripts/evidence.sh "$@" ;;
  docs-check)   exec bash scripts/docs-check.sh docs/design.md docs/threat-model.md "$@" ;;
  fresh-clone)  exec bash scripts/fresh-clone-check.sh "$@" ;;
  help|-h|--help) awk 'NR >= 5 { if ($0 !~ /^#/) exit; sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}" ;;
  *) echo "make.sh: unknown target '$target'; run: bash make.sh help" >&2; exit 2 ;;
esac
