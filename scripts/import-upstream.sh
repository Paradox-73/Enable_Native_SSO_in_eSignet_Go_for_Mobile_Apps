#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Copies the allow-listed files of one upstream commit, byte for byte, into this repository.
#
#   bash scripts/import-upstream.sh <name> [--repo DIR]
#
#   bash scripts/import-upstream.sh thunder
#
# - <name> selects scripts/imports/<name>.manifest (folder, upstream repository, commit, paths).
# - Files come from "git archive <commit>" of the upstream clone with line-ending conversion off
#   (Git for Windows sets core.autocrlf=true system-wide, which archive applies), never
#   from a checked-out working copy, which Git for Windows may have converted to CRLF.
#   The clone is $PS03_UPSTREAM_DIR/<clone>; when it is absent or lacks the commit, the commit is
#   fetched from the manifest URL into $PS03_RUN_DIR/upstream-cache/<name>.
# - Upstream files with the executable bit are added to the index with +x (Windows does not
#   record the bit), so the import commit keeps upstream's file modes.
# - Refuses to write into a folder that already has files.
# - Next steps: bash scripts/verify-import.sh <name>, then commit only <folder> with the message
#   "import: upstream <repo>@<short commit> (<licence>)".
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

repo=$PS03_ROOT name=""
while [ $# -gt 0 ]; do
  case $1 in
    --repo) [ $# -ge 2 ] || ps03_usage; repo=$(cd "$2" && pwd); shift 2 ;;
    -h|--help) ps03_usage ;;
    -*) ps03_die "unknown option $1" 2 ;;
    *) name=$1; shift ;;
  esac
done
[ -n "$name" ] || ps03_usage

manifest=$repo/scripts/imports/${name%/}.manifest
[ -f "$manifest" ] || ps03_die "no manifest $manifest"
dir=$(ps03_manifest_value "$manifest" dir)
commit=$(ps03_manifest_value "$manifest" commit)
mapfile -t paths < <(ps03_manifest_values "$manifest" path)
[ -n "$dir" ] && [ -n "$commit" ] && [ ${#paths[@]} -gt 0 ] || ps03_die "$manifest needs dir, commit and path lines"

if [ -d "$repo/$dir" ] && [ -n "$(ls -A "$repo/$dir")" ]; then
  ps03_die "$dir already has files; remove them first or verify them with scripts/verify-import.sh"
fi

up=$(ps03_upstream_clone "$manifest")
mkdir -p "$repo/$dir"
git -C "$up" -c core.autocrlf=false -c core.eol=lf archive --format=tar "$commit" -- "${paths[@]}" | tar -x -f - -C "$repo/$dir"

# Executable files: record +x in the index (core.filemode is false on Windows).
count=0
while IFS= read -r -d '' entry; do
  path=${entry#*$'\t'}
  git -C "$repo" update-index --add --chmod=+x -- "$dir/$path"
  count=$((count + 1))
done < <(git -C "$up" ls-tree -r -z "$commit" -- "${paths[@]}" | grep -z '^100755 ')

files=$(git -C "$up" ls-tree -r --name-only "$commit" -- "${paths[@]}" | wc -l | tr -d ' ')
ps03_log "copied $files files of $(ps03_manifest_value "$manifest" repo)@${commit:0:8} into $dir/ ($count staged as executable)"
echo "import-upstream: $dir: $files files from $(ps03_manifest_value "$manifest" repo)@${commit:0:8}"
