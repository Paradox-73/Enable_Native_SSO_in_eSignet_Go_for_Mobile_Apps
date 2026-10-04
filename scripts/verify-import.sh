#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Proves that an imported folder is byte-for-byte the allow-listed part of its upstream commit.
#
#   bash scripts/verify-import.sh <name> [--worktree] [--repo DIR]
#   bash scripts/verify-import.sh <name> --list-excluded
#   bash scripts/verify-import.sh --self-test
#
#   bash scripts/verify-import.sh thunder
#
# - <name> selects scripts/imports/<name>.manifest (folder, upstream repository, commit, paths).
# - Compares the folder's files in its "import: upstream ..." commit with the allow-listed files
#   of the upstream commit: git blob id (exact bytes) and file mode of every file.
#   Later team commits are not compared: the import commit is what judges diff against.
# - Before that commit exists (or with --worktree) it checks the working tree instead and also
#   reports files that the repository's ignore rules would keep out of the commit.
# - Prints "differ", "missing" and "extra" lines, then
#   "verify-import: <name>: N files, N same, 0 differ, 0 missing, 0 extra"; exit status 1 on any
#   difference (extra files in the working tree are reported but allowed: UPSTREAM.md and later work).
# - --list-excluded prints the upstream files left out of the import: every file next to an
#   allow-listed path, and each other top-level entry with its file count.
# - --self-test runs the checks against throw-away repositories and prints
#   "verify-import: self-test OK".
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# ---------------------------------------------------------------------------------------------
verify() { # verify <repo> <manifest> <worktree 0|1>
  local repo=$1 manifest=$2 worktree=$3
  local dir commit up import_commit label
  dir=$(ps03_manifest_value "$manifest" dir)
  commit=$(ps03_manifest_value "$manifest" commit)
  label="$(ps03_manifest_value "$manifest" repo)@${commit:0:8}"
  local -a paths; mapfile -t paths < <(ps03_manifest_values "$manifest" path)
  up=$(ps03_upstream_clone "$manifest")

  local -A exp_blob=() exp_mode=() act_blob=() act_mode=()
  local mode sha path
  while IFS= read -r -d '' line; do
    IFS=$' 	' read -r mode _ sha <<<"${line%%$'\t'*}"; path=${line#*$'\t'}
    exp_blob[$path]=$sha; exp_mode[$path]=$mode
  done < <(git -C "$up" ls-tree -r -z "$commit" -- "${paths[@]}")
  [ ${#exp_blob[@]} -gt 0 ] || ps03_die "the allow-list matches no file of $label"

  import_commit=$(ps03_import_commit "$dir" "$repo")
  if [ "$worktree" -eq 0 ] && [ -n "$import_commit" ]; then
    ps03_log "checking import commit ${import_commit:0:12} against $label"
    while IFS= read -r -d '' line; do
      IFS=$' 	' read -r mode _ sha <<<"${line%%$'\t'*}"; path=${line#*$'\t'}
      path=${path#"$dir"/}
      act_blob[$path]=$sha; act_mode[$path]=$mode
    done < <(git -C "$repo" ls-tree -r -z "$import_commit" -- "$dir")
  else
    worktree=1
    ps03_log "checking the working tree of $dir/ against $label"
    [ -d "$repo/$dir" ] || ps03_die "$dir/ does not exist"
    local -a files=()
    mapfile -t files < <(cd "$repo/$dir" && find . -type f | sed 's#^\./##' | LC_ALL=C sort)
    if [ ${#files[@]} -gt 0 ]; then
      local -a sums; mapfile -t sums < <(printf "$dir/%s\n" "${files[@]}" | git -C "$repo" hash-object --no-filters --stdin-paths)
      local i
      for i in "${!files[@]}"; do act_blob[${files[$i]}]=${sums[$i]}; act_mode[${files[$i]}]=100644; done
      # The index knows the executable bit even where the file system does not (Windows).
      while IFS= read -r -d '' line; do
        IFS=$' 	' read -r mode sha _ <<<"${line%%$'\t'*}"; path=${line#*$'\t'}; path=${path#"$dir"/}
        [ -n "${act_blob[$path]+x}" ] && act_mode[$path]=$mode
      done < <(git -C "$repo" ls-files -s -z -- "$dir")
    fi
  fi

  local same=0 differ=0 missing=0 extra=0 hint
  local -A ignored=()
  if [ "$worktree" -eq 1 ]; then
    while IFS= read -r path; do ignored[${path#"$dir"/}]=1; done < <(
      for path in "${!exp_blob[@]}"; do printf '%s\n' "$dir/$path"; done \
        | git -C "$repo" check-ignore --no-index --stdin 2>/dev/null || true)
  fi
  for path in $(printf '%s\n' "${!exp_blob[@]}" | LC_ALL=C sort); do
    if [ -z "${act_blob[$path]+x}" ]; then
      echo "missing $dir/$path"; missing=$((missing + 1))
    elif [ -n "${ignored[$path]+x}" ]; then
      echo "missing $dir/$path (an ignore rule keeps it out of commits)"; missing=$((missing + 1))
    elif [ "${act_blob[$path]}" != "${exp_blob[$path]}" ]; then
      echo "differ  $dir/$path (content)"; differ=$((differ + 1))
    elif [ "${act_mode[$path]}" != "${exp_mode[$path]}" ]; then
      hint=""; [ "$worktree" -eq 1 ] && hint="; before committing run: git update-index --chmod=+x $dir/$path"
      echo "differ  $dir/$path (mode ${act_mode[$path]}, upstream ${exp_mode[$path]}$hint)"
      differ=$((differ + 1))
    else
      same=$((same + 1))
    fi
  done
  for path in $(printf '%s\n' "${!act_blob[@]}" | LC_ALL=C sort); do
    [ -n "${exp_blob[$path]+x}" ] && continue
    echo "extra   $dir/$path"; extra=$((extra + 1))
  done

  echo "verify-import: $(basename "${manifest%.manifest}"): ${#exp_blob[@]} files, $same same, $differ differ, $missing missing, $extra extra"
  [ $differ -eq 0 ] && [ $missing -eq 0 ] && { [ "$worktree" -eq 1 ] || [ $extra -eq 0 ]; }
}

# ---------------------------------------------------------------------------------------------
list_excluded() { # list_excluded <manifest>
  local manifest=$1 commit up p parent n=0
  commit=$(ps03_manifest_value "$manifest" commit)
  local -a paths; mapfile -t paths < <(ps03_manifest_values "$manifest" path)
  up=$(ps03_upstream_clone "$manifest")
  local -A included=() parents=()
  for p in "${paths[@]}"; do
    included[$p]=1
    parent=$(dirname "$p"); [ "$parent" != "." ] && parents[$parent]=1
  done
  is_included() { local q=$1; while :; do [ -n "${included[$q]+x}" ] && return 0; [ "$q" = "${q%/*}" ] && return 1; q=${q%/*}; done; }

  # Files next to allow-listed paths: the exact list of what was left out where the import is partial.
  for parent in $(printf '%s\n' "${!parents[@]}" | LC_ALL=C sort); do
    while IFS= read -r p; do
      is_included "$p" && continue
      echo "excluded $p"; n=$((n + 1))
    done < <(git -C "$up" ls-tree -r --name-only "$commit" -- "$parent")
  done
  # Other top-level entries: one line each with the number of files, to keep the list readable.
  local count
  while IFS= read -r -d '' line; do
    p=${line#*$'\t'}
    is_included "$p" && continue
    [ -n "${parents[$p]+x}" ] && continue
    case ${line%% *} in
      040000) count=$(git -C "$up" ls-tree -r --name-only "$commit" -- "$p" | wc -l | tr -d ' ')
              echo "excluded $p/ ($count files)"; n=$((n + count)) ;;
      *) echo "excluded $p"; n=$((n + 1)) ;;
    esac
  done < <(git -C "$up" ls-tree -z "$commit")
  echo "verify-import: $(basename "${manifest%.manifest}"): $n upstream files left out"
}

# ---------------------------------------------------------------------------------------------
self_test() {
  local tmp script=$PS03_ROOT/scripts/verify-import.sh fail=0
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/verify-import-test.XXXXXX")
  trap 'rm -rf "$tmp"' RETURN
  export GIT_AUTHOR_NAME=self-test GIT_AUTHOR_EMAIL=self-test@example.invalid
  export GIT_COMMITTER_NAME=self-test GIT_COMMITTER_EMAIL=self-test@example.invalid
  g() { git -c core.autocrlf=false -c init.defaultBranch=main "$@"; }

  # Upstream: allow-listed files (one executable, one name with a space), files left out next to
  # them and a top-level folder left out.
  local up=$tmp/upstream/lib-clone
  mkdir -p "$up/src/sub" "$up/docs" "$up/other"
  g init -q "$up"
  printf 'licence\n' > "$up/LICENSE"
  printf 'package src\n' > "$up/src/a.go"
  printf '#!/bin/sh\necho run\n' > "$up/src/run.sh"
  printf 'line one\r\nline two\r\n' > "$up/src/sub/crlf.txt"  # bytes must survive unconverted
  printf 'notes\n' > "$up/src/NOTES.md"
  printf 'doc\n' > "$up/docs/with space.md"
  printf 'x\n' > "$up/other/x.txt"
  printf 'y\n' > "$up/other/y.txt"
  g -C "$up" add -A && g -C "$up" update-index --chmod=+x src/run.sh && g -C "$up" commit -qm upstream
  local sha; sha=$(g -C "$up" rev-parse HEAD)

  new_team_repo() { # new_team_repo <path>: empty team repository with the manifest
    mkdir -p "$1/scripts/imports"
    g init -q "$1"
    printf '* -text\n' > "$1/.gitattributes"
    cat > "$1/scripts/imports/lib.manifest" <<MANIFEST
dir      lib
repo     example/upstream
commit   $sha
licence  MIT
clone    lib-clone
path     LICENSE
path     src/a.go
path     src/run.sh
path     src/sub
path     docs
MANIFEST
    g -C "$1" add -A && g -C "$1" commit -qm "chore: start"
  }
  import_into() { PS03_UPSTREAM_DIR=$tmp/upstream bash "$PS03_ROOT/scripts/import-upstream.sh" lib --repo "$1" >/dev/null 2>&1; }
  run_verify() { PS03_UPSTREAM_DIR=$tmp/upstream bash "$script" lib --repo "$@" 2>/dev/null; }
  expect() { # expect <case> <expected exit> <expected output fragment> <verify args...>
    local name=$1 want_rc=$2 want_out=$3 out rc=0; shift 3
    out=$(run_verify "$@") || rc=$?
    if [ $rc -ne "$want_rc" ] || ! grep -qF -- "$want_out" <<<"$out"; then
      echo "self-test case '$name': exit $rc (want $want_rc), output:" >&2; echo "$out" >&2
      fail=1
    fi
  }
  # Same steps as the shared commit helper: stage the folder, then a path-limited commit, which
  # keeps executable bits only through scripts/hooks/pre-commit.
  commit_import() {
    g -C "$1" add -A -- lib &&       g -C "$1" -c core.hooksPath="$PS03_ROOT/scripts/hooks" commit -qm "import: upstream example/upstream@${sha:0:8} (MIT)" -- lib
  }

  local ok=$tmp/ok
  new_team_repo "$ok"; import_into "$ok"
  expect "working tree before the import commit" 0 "5 files, 5 same, 0 differ, 0 missing, 0 extra" "$ok"
  commit_import "$ok"
  expect "import commit" 0 "5 same, 0 differ, 0 missing, 0 extra" "$ok"
  printf 'package src // team change\n' > "$ok/lib/src/a.go"; printf 'x\n' > "$ok/lib/UPSTREAM.md"
  g -C "$ok" add -A && g -C "$ok" commit -qm "feat: team change after the import"
  expect "later team commits are not compared" 0 "0 differ, 0 missing" "$ok"
  expect "working tree after team changes" 1 "1 differ, 0 missing, 1 extra" "$ok" --worktree

  local changed=$tmp/changed
  new_team_repo "$changed"; import_into "$changed"
  printf 'line one\nline two\n' > "$changed/lib/src/sub/crlf.txt"
  commit_import "$changed"
  expect "changed bytes in the import commit" 1 "differ  lib/src/sub/crlf.txt (content)" "$changed"

  local gone=$tmp/missing
  new_team_repo "$gone"; import_into "$gone"
  rm "$gone/lib/docs/with space.md"; printf 'team file\n' > "$gone/lib/team.txt"
  commit_import "$gone"
  expect "file missing from the import commit" 1 "missing lib/docs/with space.md" "$gone"
  expect "extra file in the import commit" 1 "extra   lib/team.txt" "$gone"

  local mode=$tmp/mode
  new_team_repo "$mode"; import_into "$mode"
  chmod -x "$mode/lib/src/run.sh"; g -C "$mode" update-index --chmod=-x lib/src/run.sh
  commit_import "$mode"
  expect "executable bit lost" 1 "differ  lib/src/run.sh (mode 100644, upstream 100755" "$mode"

  local ign=$tmp/ignored
  new_team_repo "$ign"; import_into "$ign"
  printf '*.go\n' > "$ign/.gitignore"
  expect "ignore rule would drop an imported file" 1 "missing lib/src/a.go (an ignore rule keeps it out of commits)" "$ign"

  local listed; listed=$(PS03_UPSTREAM_DIR=$tmp/upstream bash "$script" lib --repo "$ok" --list-excluded 2>/dev/null) || fail=1
  for want in "excluded src/NOTES.md" "excluded other/ (2 files)" "3 upstream files left out"; do
    grep -qF -- "$want" <<<"$listed" || { echo "self-test case 'list-excluded': no '$want' in:" >&2; echo "$listed" >&2; fail=1; }
  done
  if grep -qE 'excluded (src/a.go|LICENSE|docs)' <<<"$listed"; then
    echo "self-test case 'list-excluded': an allow-listed path is listed as excluded" >&2; fail=1
  fi

  if [ $fail -eq 0 ]; then echo "verify-import: self-test OK"; else echo "verify-import: self-test FAILED"; return 1; fi
}

# ---------------------------------------------------------------------------------------------
IFS=$'\n'
repo=$PS03_ROOT name="" worktree=0 action=verify
while [ $# -gt 0 ]; do
  case $1 in
    --self-test) action=self-test; shift ;;
    --list-excluded) action=list; shift ;;
    --worktree) worktree=1; shift ;;
    --repo) [ $# -ge 2 ] || ps03_usage; repo=$(cd "$2" && pwd); shift 2 ;;
    -h|--help) ps03_usage ;;
    -*) ps03_die "unknown option $1" 2 ;;
    *) name=${1%/}; shift ;;
  esac
done

case $action in
  self-test) self_test ;;
  *)
    [ -n "$name" ] || ps03_usage
    manifest=$repo/scripts/imports/$name.manifest
    if [ ! -f "$manifest" ]; then
      known=$(for m in "$repo"/scripts/imports/*.manifest; do [ -f "$m" ] && basename "$m" .manifest; done | tr '\n' ' ')
      ps03_die "no manifest $manifest (known: ${known:-none})"
    fi
    if [ $action = list ]; then list_excluded "$manifest"; else verify "$repo" "$manifest" "$worktree"; fi
    ;;
esac
