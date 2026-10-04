#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs a module's own golangci-lint version and configuration, judging only the team's lines.
#
#   bash scripts/go-lint.sh <module dir> [-- golangci-lint run flags...]
#   bash scripts/go-lint.sh --versions
#
#   bash scripts/go-lint.sh thunder/backend
#   bash scripts/go-lint.sh esignet/esignet-service
#
# - Version: Thunder pins golangci-lint v1.64.8 (its Makefile, GOLANGCI_LINT_VERSION). eSignet's
#   make.sh installs "latest" of v2; v2.14.0 (released 2026-09-24) was the latest v2 on 2026-10-04
#   and is pinned here so results do not change under us. The module's .golangci.yml decides:
#   "version: 2" selects v2, any other config v1, no config v2 with its defaults.
# - Inside an imported folder, "--new-from-rev=<import commit>" limits findings to lines changed
#   since the unchanged upstream import (committed or not); upstream's own findings are not ours.
# - Runs in the Go container under the go-PS03 lock; binaries are built once into the ps03-gotools
#   volume. Memory cap 3 GB (PS03_GO_LINT_MEMORY): type-checking all of Thunder needs more than 2.
# - Exit status is golangci-lint's (1 when it reports issues).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

V1_VERSION=v1.64.8
V1_PACKAGE=github.com/golangci/golangci-lint/cmd/golangci-lint
V2_VERSION=v2.14.0
V2_PACKAGE=github.com/golangci/golangci-lint/v2/cmd/golangci-lint

# Installs a pinned release into the tools volume once, then prints its path.
install_fn='
lint_bin() {
  dir=/gotools/golangci-lint-$1
  if [ ! -x "$dir/golangci-lint" ]; then
    echo "go-lint: building golangci-lint $1 (first run only)" >&2
    GOBIN=$dir go install "$2@$1" >&2 || exit 2
  fi
  echo "$dir/golangci-lint"
}
'

[ $# -ge 1 ] || ps03_usage
case $1 in
  -h|--help) ps03_usage ;;
  --versions)
    inner="$install_fn"'
"$(lint_bin "$V1_VERSION" "$V1_PACKAGE")" --version
"$(lint_bin "$V2_VERSION" "$V2_PACKAGE")" --version
'
    ps03_with_lock go-PS03 ps03_go_container --memory "${PS03_GO_LINT_MEMORY:-3g}" \
      -e "V1_VERSION=$V1_VERSION" -e "V1_PACKAGE=$V1_PACKAGE" -e "V2_VERSION=$V2_VERSION" -e "V2_PACKAGE=$V2_PACKAGE" \
      . sh -c "$inner"
    exit
    ;;
esac

dir=${1%/}; shift
[ "${1:-}" = "--" ] && shift
[ -f "$PS03_ROOT/$dir/go.mod" ] || ps03_die "$dir is not a Go module (no go.mod)"

version=$V2_VERSION package=$V2_PACKAGE config=none
for c in .golangci.yml .golangci.yaml; do
  [ -f "$PS03_ROOT/$dir/$c" ] || continue
  config=$c
  if ! grep -qE '^version:[[:space:]]*"?2' "$PS03_ROOT/$dir/$c"; then version=$V1_VERSION package=$V1_PACKAGE; fi
  break
done

since=$(ps03_module_import_commit "$dir")
if [ -n "$since" ]; then
  scope="lines changed since import ${since:0:12}"
else
  scope="every line: not inside an imported folder"
fi
ps03_log "$dir: golangci-lint $version, config $config, $scope"

inner="$install_fn"'
bin=$(lint_bin "$VERSION" "$PACKAGE") || exit 2
export GOLANGCI_LINT_CACHE=/gotools/golangci-lint-cache
if [ -n "$SINCE" ]; then set -- --new-from-rev="$SINCE" "$@"; fi
"$bin" run --timeout=15m "$@" ./...
'
rc=0
ps03_with_lock go-PS03 ps03_go_container --memory "${PS03_GO_LINT_MEMORY:-3g}" \
  -e "VERSION=$version" -e "PACKAGE=$package" -e "SINCE=$since" \
  "$dir" sh -c "$inner" sh "$@" || rc=$?
if [ $rc -eq 0 ]; then echo "go-lint: $dir: OK ($scope)"; else echo "go-lint: $dir: FAILED, exit $rc ($scope)"; fi
exit $rc
