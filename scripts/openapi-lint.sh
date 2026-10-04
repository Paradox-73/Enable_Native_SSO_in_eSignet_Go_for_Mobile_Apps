#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Lints an OpenAPI file and fails only if the team's edits added errors.
#
#   bash scripts/openapi-lint.sh <file> [--base FILE]
#
#   bash scripts/openapi-lint.sh esignet/docs/esignet-openapi.yaml
#   bash scripts/openapi-lint.sh thunder/api/oauth2.yaml
#
# - Runs Redocly CLI 1.34.20 ("recommended" rules) through npx on the file in the working tree and
#   on the same file at its folder's "import:" commit (extracted with its folder, so relative $refs
#   resolve). Upstream files carry findings of their own; only an increase counts.
# - A file that did not exist at the import commit has a baseline of 0 errors.
#   --base FILE compares against another file instead.
# - Prints the findings of the working-tree file, then
#   "openapi-lint: <file>: E errors, W warnings (import: E0 errors, W0 warnings): OK|FAILED".
# - Needs Node.js with npx (Node 24 on the team laptops); the first run downloads the CLI.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

REDOCLY="@redocly/cli@1.34.20"
export REDOCLY_TELEMETRY=off REDOCLY_SUPPRESS_UPDATE_NOTICE=true NO_UPDATE_NOTIFIER=1

file="" base=""
while [ $# -gt 0 ]; do
  case $1 in
    --base) [ $# -ge 2 ] || ps03_usage; base=$(cd "$(dirname "$2")" && pwd)/$(basename "$2"); shift 2 ;;
    -h|--help) ps03_usage ;;
    -*) ps03_die "unknown option $1" 2 ;;
    *) file=$1; shift ;;
  esac
done
[ -n "$file" ] || ps03_usage
command -v npx >/dev/null 2>&1 || ps03_die "npx not found: install Node.js"

# Repository-relative path of the file.
abs=$(cd "$(dirname "$file")" 2>/dev/null && pwd)/$(basename "$file")
[ -f "$abs" ] || ps03_die "no such file: $file"
rel=${abs#"$PS03_ROOT"/}
[ "$rel" != "$abs" ] || [ -n "$base" ] || ps03_die "$file is outside the repository; give --base to compare"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/openapi-lint.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# lint <file> <json out>: Redocly exits 1 when it finds errors; the JSON totals are what we read.
lint() {
  (cd "$(dirname "$1")" && npx --yes "$REDOCLY" lint --format=json "$(basename "$1")" > "$2" 2> "$2.log") || true
  [ -s "$2" ] || { cat "$2.log" >&2; ps03_die "Redocly produced no result for $1"; }
}
# totals <json>: "<errors> <warnings>"
totals() {
  node -e 'const t = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).totals;
           console.log(t.errors + " " + t.warnings)' "$1"
}
# findings <json>: one line per problem.
findings() {
  node -e 'for (const p of JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).problems) {
             const at = (p.location && p.location[0]) ? p.location[0].pointer : "";
             console.log(`${p.severity === "error" ? "error" : "warn "} ${p.ruleId} ${at}: ${p.message}`);
           }' "$1"
}

label="import"
if [ -z "$base" ]; then
  top=${rel%%/*}
  since=$(ps03_import_commit "$top")
  if [ -n "$since" ] && git -C "$PS03_ROOT" cat-file -e "$since:$rel" 2>/dev/null; then
    mkdir -p "$tmp/base"
    git -C "$PS03_ROOT" archive "$since" -- "$(dirname "$rel")" | tar -x -f - -C "$tmp/base"
    base=$tmp/base/$rel
    label="import ${since:0:8}"
  else
    label="new file"
  fi
else
  label="base $(basename "$base")"
fi

lint "$abs" "$tmp/now.json"
read -r errors warnings < <(totals "$tmp/now.json")
base_errors=0 base_warnings=0
if [ -n "$base" ]; then
  lint "$base" "$tmp/base.json"
  read -r base_errors base_warnings < <(totals "$tmp/base.json")
fi

findings "$tmp/now.json"
result=OK; [ "$errors" -le "$base_errors" ] || result=FAILED
echo "openapi-lint: $rel: $errors errors, $warnings warnings ($label: $base_errors errors, $base_warnings warnings): $result"
[ $result = OK ]
