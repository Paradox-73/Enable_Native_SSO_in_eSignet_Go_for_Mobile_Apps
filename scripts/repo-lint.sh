#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Repository hygiene checks that need no build: keys, licence headers, line endings, sizes.
#
#   bash scripts/repo-lint.sh [--repo DIR]
#   bash scripts/repo-lint.sh --self-test
#
# Team-written content is every file outside the imported folders (scripts/imports/*.manifest),
# plus, inside an imported folder, files added after its "import:" commit and the lines the team
# added to upstream files. Upstream bytes are never judged: they stay as published.
#
# - keys: no private key block (PEM "BEGIN ... PRIVATE KEY") and no keystore file (.jks,
#   .keystore, .p12, .pfx, .bks) anywhere, imported folders included, except the test fixtures
#   listed under ignored_paths in .gitguardian.yaml.
# - headers: team source files carry "SPDX-License-Identifier: MIT" in their first 10 lines; new
#   Go files inside an imported folder carry that upstream's header (manifest "header" line).
# - line endings: team text files use LF (.gitattributes stores bytes unchanged, so a CRLF from
#   a Windows editor would be committed as is). Windows batch files are exempt.
# - size: no file over 5 MiB.
# Files ignored by git are not checked. Prints one line per problem, then "repo-lint: OK" or
# "repo-lint: FAILED (N problems)"; exit status 1 on any problem.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GO_GENERATED='^// Code generated .* DO NOT EDIT\.$'
KEY_BLOCK='-----BEGIN [A-Z0-9 ]*PRIVATE KEY'
KEYSTORE_EXT='jks|keystore|p12|pfx|bks'
HEADER_EXT='go|sh|bash|kt|kts|java|py|js|mjs|cjs|ts|aidl|gradle|yaml|yml|toml'
MAX_BYTES=$((5 * 1024 * 1024))

# ---------------------------------------------------------------------------------------------
lint_repo() {
  local repo=$1 problems=0
  cd "$repo"
  problem() { echo "repo-lint: $*"; problems=$((problems + 1)); }

  # Every file git would commit: tracked or untracked, not ignored, still present.
  local -a all=()
  while IFS= read -r -d '' f; do [ -f "$f" ] && all+=("$f"); done < <(git ls-files -z -co --exclude-standard)

  # Split into team files (full checks), team-added lines in upstream files, and untouched imports.
  local -a team=() import_dirs=() pending=()
  local -A import_of=() header_of=() modified=()
  local m d c f status path
  for m in scripts/imports/*.manifest; do
    [ -f "$m" ] || continue
    d=$(ps03_manifest_value "$m" dir)
    import_dirs+=("$d")
    header_of[$d]=$(ps03_manifest_value "$m" header)
    c=$(ps03_import_commit "$d" "$repo")
    if [ -z "$c" ]; then
      [ -d "$d" ] && pending+=("$d")
      continue
    fi
    import_of[$d]=$c
    while IFS=$'\t' read -r status path; do
      case $status in
        A) team+=("$path") ;;
        M|T) modified[$path]=$d ;;
      esac
    done < <(git diff --name-status --no-renames "$c" -- "$d")
    while IFS= read -r -d '' path; do team+=("$path"); done < <(git ls-files -z -o --exclude-standard -- "$d")
  done
  for d in "${pending[@]+"${pending[@]}"}"; do
    echo "repo-lint: note: $d/ has no import commit yet; its files count as upstream (run scripts/verify-import.sh)"
  done
  local in_import
  for f in "${all[@]+"${all[@]}"}"; do
    in_import=0
    for d in "${import_dirs[@]+"${import_dirs[@]}"}"; do case $f in "$d"/*) in_import=1 ;; esac; done
    [ $in_import -eq 0 ] && team+=("$f")
  done
  local -a team_present=()
  for f in "${team[@]+"${team[@]}"}"; do [ -f "$f" ] && team_present+=("$f"); done
  team=("${team_present[@]+"${team_present[@]}"}")

  # --- private keys and keystores ---------------------------------------------------------------
  local -a allowed_keys=()
  if [ -f .gitguardian.yaml ]; then
    mapfile -t allowed_keys < <(awk '/^[[:space:]]*ignored_paths:/ { on = 1; next }
                                     on && /^[[:space:]]*-[[:space:]]/ { sub(/^[[:space:]]*-[[:space:]]*/, ""); gsub(/["\047]/, ""); print; next }
                                     on && /^[^[:space:]#]/ { on = 0 }' .gitguardian.yaml)
  fi
  # shellcheck disable=SC2053 # the right-hand side is a glob pattern from .gitguardian.yaml
  is_allowed_key() { local p; for p in "${allowed_keys[@]+"${allowed_keys[@]}"}"; do [[ $1 == $p ]] && return 0; done; return 1; }
  local -a key_files=()
  if [ ${#all[@]} -gt 0 ]; then
    mapfile -t key_files < <(printf '%s\0' "${all[@]}" | xargs -0 grep -l -I -E -s -- "$KEY_BLOCK" || true)
  fi
  for f in "${all[@]+"${all[@]}"}"; do
    [[ ${f,,} =~ \.($KEYSTORE_EXT)$ ]] && key_files+=("$f")
  done
  for f in "${key_files[@]+"${key_files[@]}"}"; do
    is_allowed_key "$f" || problem "keys: $f holds a private key and is not a fixture listed in .gitguardian.yaml"
  done

  # --- licence headers --------------------------------------------------------------------------
  local base ext want top
  for f in "${team[@]+"${team[@]}"}"; do
    base=${f##*/}; ext=${base##*.}; [ "$ext" = "$base" ] && ext=""
    case $f in */gradle/wrapper/*|gradle/wrapper/*) continue ;; esac
    case $base in gradlew|gradlew.bat) continue ;; esac
    top=${f%%/*}; want="SPDX-License-Identifier: MIT"
    if [ -n "${import_of[$top]+x}" ]; then
      # Inside an import only Go sources need a header, in the upstream's own form.
      [ "$ext" = go ] || continue
      want=${header_of[$top]}; [ -n "$want" ] || continue
    else
      if ! [[ $ext =~ ^($HEADER_EXT)$ ]] && [ "$base" != Dockerfile ] && [ "$base" != Makefile ] \
          && [[ $base != *.Dockerfile ]] && [ "$(head -c 2 "$f")" != "#!" ]; then
        continue
      fi
    fi
    head -n 10 "$f" | grep -qE -- "$GO_GENERATED" && continue
    head -n 10 "$f" | grep -qF -- "$want" || problem "headers: $f: no \"$want\" in the first 10 lines"
  done

  # --- line endings ----------------------------------------------------------------------------
  for f in "${team[@]+"${team[@]}"}"; do
    case ${f,,} in *.bat|*.cmd) continue ;; esac
    # -U: Git for Windows' grep would otherwise drop the carriage returns it is asked to find.
    grep -q -U -I $'\r' "$f" 2>/dev/null && problem "line endings: $f has CRLF line endings"
  done
  for path in "${!modified[@]}"; do
    added_lines "$path" | grep -q -U $'\r$' && problem "line endings: $path: added lines end with CRLF"
  done

  # --- size ------------------------------------------------------------------------------------
  if [ ${#all[@]} -gt 0 ]; then
    while IFS= read -r f; do
      problem "size: ${f#./} is larger than 5 MiB"
    done < <(printf '%s\0' "${all[@]}" | xargs -0 sh -c 'find "$@" -maxdepth 0 -type f -size +'"$MAX_BYTES"'c' sh 2>/dev/null)
  fi

  echo "repo-lint: checked ${#team[@]} team files, ${#modified[@]} changed upstream files, ${#all[@]} files for keys and size"
  if [ $problems -eq 0 ]; then echo "repo-lint: OK"; else echo "repo-lint: FAILED ($problems problems)"; return 1; fi
}

# added_lines PATH: "path:line:text" for each line the team added to an upstream file since its
# folder's import commit (working tree included).
added_lines() {
  local path=$1 top=${1%%/*}
  git diff -U0 --no-color --no-ext-diff "${import_of[$top]}" -- "$path" | awk -v p="$path" '
    /^@@/ { s = $0; sub(/^@@ -[0-9,]+ \+/, "", s); sub(/[ ,].*/, "", s); n = s + 0; next }
    /^\+\+\+ / { next }
    /^\+/ { print p ":" n ":" substr($0, 2); n++ }'
}

# ---------------------------------------------------------------------------------------------
self_test() {
  local tmp fail=0 script=$PS03_ROOT/scripts/repo-lint.sh
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/repo-lint-test.XXXXXX")
  trap 'rm -rf "$tmp"' RETURN
  export GIT_AUTHOR_NAME=self-test GIT_AUTHOR_EMAIL=self-test@example.invalid
  export GIT_COMMITTER_NAME=self-test GIT_COMMITTER_EMAIL=self-test@example.invalid
  g() { git -c core.autocrlf=false -c init.defaultBranch=main "$@"; }
  local key; key="-----BEGIN EC PRIVATE""KEY-----"; key=${key/PRIVATEKEY/PRIVATE KEY}

  # Base repository: a team script, an import folder with upstream Go code
  # and a key fixture listed in .gitguardian.yaml, imported in its own commit.
  local base=$tmp/base
  mkdir -p "$base/scripts/imports" "$base/vendor/pkg" "$base/vendor/testdata"
  g init -q "$base"
  printf '* -text\n' > "$base/.gitattributes"
  printf 'dir vendor\nheader SPDX-License-Identifier: Apache-2.0\npath pkg\n' > "$base/scripts/imports/vendor.manifest"
  printf '#!/bin/sh\n# SPDX-License-Identifier: MIT\necho "sets the User-Agent header"\n' > "$base/scripts/run.sh"
  printf '# SPDX-License-Identifier: MIT\nversion: 2\nsecret:\n  ignored_paths:\n    - vendor/testdata/test.key\n' > "$base/.gitguardian.yaml"
  g -C "$base" add -A && g -C "$base" commit -qm "chore: start"
  printf 'package pkg\n\nfunc A() {}\n' > "$base/vendor/pkg/a.go"
  printf '%s\nAAAA\n' "$key" > "$base/vendor/testdata/test.key"
  g -C "$base" add -A && g -C "$base" commit -qm "import: upstream example/vendor@0123456 (Apache-2.0)"

  # expect <case> <OK|FAILED> <output fragment or ""> <setup command...>: copies the base
  # repository, runs the setup inside the copy, then repo-lint.
  expect() {
    local name=$1 want=$2 fragment=$3 dir out; shift 3
    dir=$tmp/case-$RANDOM; cp -r "$base" "$dir"
    (cd "$dir" && "$@") >/dev/null 2>&1 || { echo "self-test case '$name': setup failed" >&2; fail=1; return; }
    out=$(bash "$script" --repo "$dir" 2>&1) || true
    if ! grep -qF "repo-lint: $want" <<<"$out" || { [ -n "$fragment" ] && ! grep -qF -- "$fragment" <<<"$out"; }; then
      echo "self-test case '$name': want $want ${fragment:+with \"$fragment\"}, got:" >&2; echo "$out" >&2; fail=1
    fi
  }
  nothing() { :; }
  upstream_lf_edit() { printf '// a team line\n' >> vendor/pkg/a.go; }
  new_go_without_header() { printf 'package pkg\n' > vendor/pkg/b.go; }
  new_go_with_header() { printf '// Copyright 2026 The Authors\n// SPDX-License-Identifier: Apache-2.0\n\npackage pkg\n' > vendor/pkg/b.go; }
  # The marker line as mockery writes it: the file needs no licence header.
  new_generated_go() { printf '// Code generated by mockery; DO NOT EDIT.\n\npackage pkg\n' > vendor/pkg/mock.go; }
  team_script_without_header() { printf '#!/bin/sh\necho hi\n' > tool.sh; }
  unlisted_key() { printf '%s\nAAAA\n' "$key" > scripts/dev.pem; }
  keystore_file() { printf 'binary' > release.p12; }
  crlf_file() { printf '# SPDX-License-Identifier: MIT\r\nx: 1\r\n' > crlf.yaml; }
  big_file() { head -c $((6 * 1024 * 1024)) /dev/zero > big.bin; }

  expect "clean repository" OK "" nothing
  expect "LF line added to an upstream file" OK "" upstream_lf_edit
  expect "new Go file in an import without its header" FAILED "headers: vendor/pkg/b.go" new_go_without_header
  expect "new Go file in an import with its header" OK "" new_go_with_header
  expect "generated Go file needs no header" OK "" new_generated_go
  expect "team script without MIT header" FAILED "headers: tool.sh" team_script_without_header
  expect "private key outside the fixture list" FAILED "keys: scripts/dev.pem" unlisted_key
  expect "keystore file" FAILED "keys: release.p12" keystore_file
  expect "CRLF line endings" FAILED "line endings: crlf.yaml" crlf_file
  expect "file over 5 MiB" FAILED "size: big.bin" big_file

  if [ $fail -eq 0 ]; then echo "repo-lint: self-test OK"; else echo "repo-lint: self-test FAILED"; return 1; fi
}

# ---------------------------------------------------------------------------------------------
repo=$PS03_ROOT action=lint
while [ $# -gt 0 ]; do
  case $1 in
    --self-test) action=self-test; shift ;;
    --repo) [ $# -ge 2 ] || ps03_usage; repo=$(cd "$2" && pwd); shift 2 ;;
    -h|--help) ps03_usage ;;
    *) ps03_die "unknown argument $1" 2 ;;
  esac
done
declare -A import_of=()
if [ $action = self-test ]; then self_test; else lint_repo "$repo"; fi
