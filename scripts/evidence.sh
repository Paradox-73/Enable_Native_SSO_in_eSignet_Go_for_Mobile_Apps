#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Collects test evidence and writes docs/test-report.md.
#
#   bash scripts/evidence.sh                 rewrite docs/test-report.md from the record docs/test-evidence.tsv
#   bash scripts/evidence.sh --run [--with-lint]
#                                            run the checks below, update the record with what they printed,
#                                            then rewrite the report
#   bash scripts/evidence.sh --check         exit 0 when every row is PASS or a `NOT RUN` row whose note names a
#                                            known limit (limit:Lnn, listed under "Known limits"); any FAIL row,
#                                            unknown result or unknown limit fails
#
# - The record (docs/test-evidence.tsv, tab separated, one row per result) is the only source of the report.
#   Each row keeps its own date: a row that a run did not touch keeps the date of the run that set it.
# - `--run` runs, one after the other: repo-lint; Go unit tests of Thunder (`./internal/oauth/... ./pkg/...`) and
#   eSignet (`./...`) with the coverage of the changed lines; the e2e suite (scripts/with-stack.sh); the Native SSO
#   Postman folder with newman when newman is on PATH; Android JVM tests of the four modules; the signing check of
#   both APKs; the server part of the demo (scripts/demo/demo.sh --no-emulator). `--with-lint` adds golangci-lint
#   for Thunder, eSignet and e2e (several minutes). It never runs emulator tests: those rows stay as recorded.
# - Rule rows (R01-R28, A01-A13) are set from the test names that passed in the run: Go unit tests from the
#   `go test -json` streams, e2e tests from the e2e logs, Android classes from the JUnit XML files.
# - Run it from a shell that holds the docker-stack lock when other jobs share the machine, with
#   PS03_HELD_LOCKS=:docker-stack (scripts/with-stack.sh then does not wait for itself). Needs JAVA_HOME (17+) and
#   ANDROID_HOME for the Android steps, Docker, and python3 or python.
# - Run details go to $PS03_RUN_DIR/evidence/<date>/ (one log per step). Prints `evidence: <n> results at <commit>`.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
PS03_SCRIPT_NAME=evidence

MODE=report WITH_LINT=0
for a in "$@"; do
  case $a in
    --run) MODE=run ;; --check) MODE=check ;; --with-lint) WITH_LINT=1 ;;
    -h|--help) ps03_usage ;; *) ps03_die "unknown option $a" ;;
  esac
done
RECORD=$PS03_ROOT/docs/test-evidence.tsv
REPORT=$PS03_ROOT/docs/test-report.md
TODAY=$(date +%Y-%m-%d)
OUT=$PS03_RUN_DIR/evidence/$TODAY
PY=python3
"$PY" -c 'import json' >/dev/null 2>&1 || PY=python
COMMIT=$(git -C "$PS03_ROOT" rev-parse --short=7 HEAD 2>/dev/null || echo unknown)

# ---------------------------------------------------------------------------------------------
# The Python part is scripts/evidence.py: record, test-name checks, report.
# ---------------------------------------------------------------------------------------------
export EV_RECORD=$RECORD EV_REPORT=$REPORT EV_TODAY=$TODAY EV_COMMIT=$COMMIT
ev() { "$PY" "$PS03_ROOT/scripts/evidence.py" "$@"; }

[ -f "$RECORD" ] || ps03_die "missing $RECORD"
case $MODE in
  check)  ev check; exit $? ;;
  report) ev report; exit $? ;;
esac

# ---------------------------------------------------------------------------------------------
# --run
# ---------------------------------------------------------------------------------------------
mkdir -p "$OUT"
export EV_JUNIT_OUT=$OUT/junit-classes.json
GO_JSON=() E2E_LOGS=()
FAILS=0

# step ID LOGNAME COMMAND...: runs a command, keeps its output in $OUT/<LOGNAME>.log, returns its status.
step() {
  local id=$1 name=$2 rc=0 t0; shift 2
  t0=$(date +%s)
  ps03_log "$id: $*"
  "$@" >"$OUT/$name.log" 2>&1 || rc=$?
  STEP_SECONDS=$(( $(date +%s) - t0 ))
  [ $rc -eq 0 ] || { FAILS=$((FAILS + 1)); ps03_warn "$id exited $rc; see $OUT/$name.log"; }
  return 0
}
STEP_RC() { :; }
last() { grep -E "$1" "$OUT/$2.log" | tail -1 || true; }

# environment
ev env "Date of the last run" "$TODAY"
ev env "Commit of the last run" "$COMMIT$(git -C "$PS03_ROOT" status --porcelain 2>/dev/null | grep -q . && echo ' plus uncommitted changes')"
ev env "Operating system" "$(uname -s) $(uname -r) ($(cmd.exe /c ver 2>/dev/null | tr -d '\r' | grep -o 'Version [0-9.]*' || true))"
ev env "Docker" "$(docker version --format 'server {{.Server.Version}}, client {{.Client.Version}}' 2>/dev/null || echo unknown)"
ev env "Go (container $PS03_GO_IMAGE)" "$(bash "$PS03_ROOT/scripts/go.sh" . version 2>/dev/null | tr -d '\r' | head -1)"
ev env "JDK" "$(java -version 2>&1 | head -1 | tr -d '\r')"
ev env "Stack images" "$(grep -E '^\s+image:' "$PS03_ROOT/deploy/compose.yaml" | sed 's/.*image: *//' | sort -u | tr '\n' ' ')"
command -v newman >/dev/null 2>&1 && ev env "newman" "$(newman --version 2>/dev/null | tr -d '\r')"

# repo-lint
step REPO-LINT repo-lint bash "$PS03_ROOT/scripts/repo-lint.sh"
if grep -q '^repo-lint: OK' "$OUT/repo-lint.log"; then ev set REPO-LINT PASS "repo-lint: OK, $STEP_SECONDS s"; else ev set REPO-LINT FAIL "see $OUT/repo-lint.log"; fi

# Go unit tests with coverage of changed lines
go_unit() {
  local id=$1 cov=$2 module=$3; shift 3
  step "$id" "$id" bash "$PS03_ROOT/scripts/go-test.sh" "$module" "$@" --changed-cover
  local line cl json
  line=$(last '^go-test: [a-z/-]+: [0-9]+ packages' "$id")
  cl=$(last '^changed-cover: total' "$id")
  json=$(ls -t "$PS03_RUN_DIR"/test-results/"$(echo "$module" | tr / -)"-*.json 2>/dev/null | head -1 || true)
  [ -n "$json" ] && GO_JSON+=("$(ps03_host_path "$json")")
  if printf '%s' "$line" | grep -q ' 0 failed$'; then
    ev set "$id" PASS "${line#go-test: }; $STEP_SECONDS s"
  else ev set "$id" FAIL "${line:-no result line}; see $OUT/$id.log"; fi
  if [ -n "$cl" ]; then ev set "$cov" PASS "${cl#changed-cover: }"; else ev set "$cov" FAIL "no changed-cover line"; fi
}
go_unit UNIT-THUNDER COVER-THUNDER thunder/backend ./internal/oauth/... ./pkg/...
go_unit UNIT-ESIGNET COVER-ESIGNET esignet/esignet-service ./...

# lint
if [ $WITH_LINT -eq 1 ]; then
  lint() {
    local id=$1; shift
    step "$id" "$id" bash "$PS03_ROOT/scripts/go-lint.sh" "$@"
    if grep -q '^go-lint: .*OK' "$OUT/$id.log"; then ev set "$id" PASS "$(last '^go-lint: .*OK' "$id" | sed 's/^go-lint: //'), $STEP_SECONDS s"
    else ev set "$id" FAIL "see $OUT/$id.log"; fi
  }
  lint LINT-THUNDER thunder/backend -- --timeout=30m
  lint LINT-ESIGNET esignet/esignet-service
  lint LINT-E2E e2e -- --build-tags e2e
fi

# e2e suite
step E2E e2e bash "$PS03_ROOT/scripts/with-stack.sh" bash "$PS03_ROOT/e2e/run.sh"
e2e_line=$(last '^e2e: [0-9]+ passed' e2e)
while IFS= read -r f; do E2E_LOGS+=("$(ps03_host_path "$f")"); done < <(grep -o '[^ ]*e2e-[0-9-]*-phase[12].log' "$OUT/e2e.log" | sort -u)
if printf '%s' "$e2e_line" | grep -q ' 0 failed$'; then ev set E2E PASS "$e2e_line; $STEP_SECONDS s"
else ev set E2E FAIL "${e2e_line:-no result line}; see $OUT/e2e.log"; fi

# Postman folder with newman
if command -v newman >/dev/null 2>&1; then
  step NEWMAN newman bash "$PS03_ROOT/scripts/with-stack.sh" bash "$PS03_ROOT/esignet/postman-collection/native-sso/run.sh"
  req=$(grep -E '^\s*│\s*requests' "$OUT/newman.log" | tail -1 | sed 's/│/ /g' | tr -s ' ')
  asr=$(grep -E '^\s*│\s*assertions' "$OUT/newman.log" | tail -1 | sed 's/│/ /g' | tr -s ' ')
  if printf '%s' "$asr" | grep -Eq 'assertions [0-9]+ 0 *$'; then ev set NEWMAN PASS "newman 6: $req; $asr; $STEP_SECONDS s (columns: executed, failed)"
  else ev set NEWMAN FAIL "$req; $asr; see $OUT/newman.log"; fi
else
  ps03_warn "newman is not on PATH: NEWMAN row keeps its recorded result"
fi

# Android JVM tests and signing
step JVM jvm bash -c "cd '$PS03_ROOT/android' && bash gradlew --no-daemon --no-build-cache :oidc-client:cleanTest :oidc-client:test :secure-store:cleanTestDebugUnitTest :secure-store:testDebugUnitTest :superapp:cleanTestDebugUnitTest :superapp:testDebugUnitTest :childapp:cleanTestDebugUnitTest :childapp:testDebugUnitTest :superapp:assembleDebug :childapp:assembleDebug"
A=$PS03_ROOT/android
ev junit "JVM-OIDC=$A/oidc-client/build/test-results" "JVM-STORE=$A/secure-store/build/test-results" \
  "JVM-HUB=$A/superapp/build/test-results" "JVM-CARD=$A/childapp/build/test-results"
if grep -q 'BUILD SUCCESSFUL' "$OUT/jvm.log"; then ps03_log "gradle: BUILD SUCCESSFUL in $STEP_SECONDS s"; else ev set JVM-OIDC FAIL "gradle build failed; see $OUT/jvm.log"; fi
step APK-SIGN sign bash "$A/scripts/verify-family-signing.sh" debug
sign=$(last '^family-signing: ' sign || true)
if printf '%s' "$sign" | grep -q '^family-signing: OK'; then ev set APK-SIGN PASS "$sign"; ev set A01 PASS "$sign"; else ev set APK-SIGN FAIL "${sign:-no result line}"; fi

# rule rows from the test names that passed
GO_ARG=$(IFS=,; echo "${GO_JSON[*]:-}")
E2E_ARG=$(IFS=,; echo "${E2E_LOGS[*]:-}")
ev rules --go "$GO_ARG" --e2e "$E2E_ARG" --classes "$EV_JUNIT_OUT"

# server part of the demo
step DEMO-SERVER demo bash "$PS03_ROOT/scripts/demo/demo.sh" --no-emulator
if grep -q '^demo ready (server part only' "$OUT/demo.log" && grep -q '^teardown ok' "$OUT/demo.log"; then
  ev set DEMO-SERVER PASS "demo ready (server part only; emulator steps NOT RUN), teardown ok, $STEP_SECONDS s"
else ev set DEMO-SERVER FAIL "see $OUT/demo.log"; fi

ev report
ps03_log "logs in $OUT; failed steps: $FAILS"
[ $FAILS -eq 0 ] || exit 1
