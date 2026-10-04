#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs the Go tests of one module in the Linux container and prints a one-line summary.
#
#   bash scripts/go-test.sh <module dir> [packages...] [--changed-cover] [--min PERCENT] [-- go test flags...]
#
#   bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/...
#   bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover
#   bash scripts/go-test.sh thunder/backend ./internal/oauth/... -- -run TestTokenExchange -race
#
# - Runs "go test -count=1 -coverprofile -json" (packages default to ./...). The JSON stream,
#   cover profile and stderr are kept in $PS03_RUN_DIR/test-results/<module>-<time>.*.
# - Prints one "FAIL <package> <test>" line per failure, then
#   "go-test: <module>: N packages (...), M tests: P passed, S skipped, F failed".
# - --changed-cover also prints, per Go file changed since the module's "import:" commit (or every
#   file, for a module the team wrote), the coverage of the statements on changed lines only:
#   cover-profile blocks intersected with "git diff -U0", so upstream's own untested code in a
#   touched file does not count. Test files and generated files are skipped.
#   --min PERCENT makes a total below PERCENT fail.
# - Exit status: 0 when every test passed (and the minimum, if given, was met), else 1.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ $# -ge 1 ] || ps03_usage
case $1 in -h|--help) ps03_usage ;; esac
dir=${1%/}; shift
[ -f "$PS03_ROOT/$dir/go.mod" ] || ps03_die "$dir is not a Go module (no go.mod)"

packages=() go_flags=() changed=0 min=""
while [ $# -gt 0 ]; do
  case $1 in
    --changed-cover) changed=1; shift ;;
    --min) [ $# -ge 2 ] || ps03_usage; min=$2; shift 2 ;;
    --) shift; go_flags=("$@"); break ;;
    -h|--help) ps03_usage ;;
    -*) ps03_die "unknown option $1 (go test flags go after --)" 2 ;;
    *) packages+=("$1"); shift ;;
  esac
done
[ ${#packages[@]} -gt 0 ] || packages=(./...)

slug=$(printf '%s' "$dir" | sed 's#^\./##; s#/#-#g'); [ "$slug" = "." ] && slug=root
base="$slug-$(date +%Y%m%d-%H%M%S)"
results=$PS03_RUN_DIR/test-results
mkdir -p "$results"

changed_args=()
if [ $changed -eq 1 ]; then
  # The diff is taken on the host (plain git, no container needed) against the working tree, so
  # uncommitted work counts as changed too.
  since=$(ps03_module_import_commit "$dir")
  if [ -z "$since" ]; then
    # Not inside an import: every line of the module is the team's. Diff against the empty tree.
    since=$(git -C "$PS03_ROOT" hash-object -t tree /dev/null)
    ps03_log "$dir is not inside an imported folder: measuring every line"
  else
    ps03_log "changed lines are measured against import commit ${since:0:12}"
  fi
  rel=(); [ "$dir" != "." ] && rel=(--relative="$dir/")
  git -C "$PS03_ROOT" diff -U0 --no-color --no-ext-diff --no-renames --src-prefix=a/ --dst-prefix=b/ \
    ${rel[@]+"${rel[@]}"} "$since" -- "$dir" > "$results/$base.diff"
  git -C "$PS03_ROOT" ls-files --others --exclude-standard -- "$dir" \
    | { if [ "$dir" = "." ]; then cat; else sed "s#^$dir/##"; fi; } > "$results/$base.untracked"
  changed_args=(-module "/repo/$dir" -profile "/results/$base.cover" -diff "/results/$base.diff"
    -untracked "/results/$base.untracked")
  [ -n "$min" ] && changed_args+=(-min "$min")
fi

# One container run: go test, then the two summary tools from scripts/gotools (standard library
# only, so they build offline, from the shared build cache after the first run).
inner='
set -u
out=/results/$BASE
go test -count=1 -p "$TEST_P" -coverprofile="$out.cover" -json "$@" > "$out.json" 2> "$out.stderr"
if [ -s "$out.stderr" ]; then tail -n 40 "$out.stderr" >&2; fi
(cd /repo/scripts/gotools && go build -o /tmp/gotools/ ./cmd/...) || exit 2
/tmp/gotools/testsummary -name "$NAME" "$out.json"; rc=$?
if [ -n "$CHANGED_ARGS" ] && [ -s "$out.cover" ]; then
  # shellcheck disable=SC2086 # word splitting is intended: paths here contain no spaces
  /tmp/gotools/changedcover $CHANGED_ARGS || { [ $rc -eq 0 ] && rc=1; }
fi
exit $rc
'
ps03_with_lock go-PS03 ps03_go_container \
  -e "BASE=$base" -e "NAME=$dir" -e "TEST_P=${PS03_GO_TEST_P:-4}" \
  -e "CHANGED_ARGS=${changed_args[*]+${changed_args[*]}}" \
  "$dir" sh -c "$inner" sh ${go_flags[@]+"${go_flags[@]}"} "${packages[@]}"
