# shellcheck shell=bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Shared helpers for the scripts in this folder. Source it, do not run it:
#
#   . "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
#
# It sets:
#   PS03_ROOT          repository root (absolute, POSIX form)
#   PS03_RUN_DIR       runtime folder: keys, logs, test results, evidence. Default: <repo>/run (ignored by git).
#   PS03_UPSTREAM_DIR  folder with upstream clones used for import checks.
#                      Default: <repo>/../upstream when it exists, else $PS03_RUN_DIR/upstream-cache.
#   PS03_LOCK_DIR      folder of mkdir-style locks shared with other build jobs on the machine.
#                      Default: empty, which turns locking off; set it to share a lock folder.
#   PS03_GO_IMAGE      container image for Go work (default golang:1.26-bookworm).
#   PS03_SDK           Android SDK folder: ANDROID_HOME, else ANDROID_SDK_ROOT (may be empty).
#
# The scripts need bash 4.4 or newer (associative arrays, mapfile, ${var,,}, empty arrays under set -u).
# macOS ships bash 3.2 as /bin/bash, so stop here with the fix instead of failing later on a syntax error.

if [ -z "${BASH_VERSINFO:-}" ] || [ "${BASH_VERSINFO[0]}" -lt 4 ] ||
  { [ "${BASH_VERSINFO[0]}" -eq 4 ] && [ "${BASH_VERSINFO[1]}" -lt 4 ]; }; then
  printf '%s: error: needs bash 4.4 or newer, this is bash %s (%s).\n' \
    "$(basename "$0" .sh)" "${BASH_VERSION:-unknown}" "${BASH:-sh}" >&2
  printf '  macOS: brew install bash, open a new terminal and check that "bash --version" shows 5.x.\n' >&2
  exit 2
fi

PS03_SCRIPT_NAME=${PS03_SCRIPT_NAME:-$(basename "${0%.sh}")}
PS03_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

PS03_RUN_DIR=${PS03_RUN_DIR:-$PS03_ROOT/run}
if [ -z "${PS03_UPSTREAM_DIR:-}" ]; then
  if [ -d "$PS03_ROOT/../upstream" ]; then PS03_UPSTREAM_DIR=$(cd "$PS03_ROOT/../upstream" && pwd)
  else PS03_UPSTREAM_DIR=$PS03_RUN_DIR/upstream-cache; fi
fi
PS03_LOCK_DIR=${PS03_LOCK_DIR:-}
PS03_GO_IMAGE=${PS03_GO_IMAGE:-golang:1.26-bookworm}
export PS03_ROOT PS03_RUN_DIR PS03_UPSTREAM_DIR PS03_LOCK_DIR PS03_GO_IMAGE

# Logging goes to stderr so that stdout carries only the result lines other tools parse.
ps03_log()  { printf '%s: %s\n' "$PS03_SCRIPT_NAME" "$*" >&2; }
ps03_warn() { printf '%s: warning: %s\n' "$PS03_SCRIPT_NAME" "$*" >&2; }
ps03_die()  { printf '%s: error: %s\n' "$PS03_SCRIPT_NAME" "$*" >&2; exit "${2:-1}"; }

# ps03_usage: prints the calling script's header comment (from line 5, after the licence lines,
# to the first line that is not a comment) to stderr and exits 2.
ps03_usage() { awk 'NR >= 5 { if ($0 !~ /^#/) exit; sub(/^# ?/, ""); print }' "$0" >&2; exit 2; }

# ps03_host_path PATH: the form Docker Desktop on Windows accepts for bind mounts (E:/x/y);
# unchanged on Linux and macOS.
ps03_host_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s\n' "$1"; fi
}

# Android: the SDK folder (empty when unset) and adb from it, else from PATH, on $ANDROID_SERIAL when set.
PS03_SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
ps03_adb() { MSYS_NO_PATHCONV=1 "${PS03_SDK:+$PS03_SDK/platform-tools/}adb" ${ANDROID_SERIAL:+-s "$ANDROID_SERIAL"} "$@"; }

# ps03_emulator_prepare: on a booted emulator, wake the screen, turn animations off, forward the stack
# ports 38088 and 38300 (adb reverse) and turn Chrome's first-run screens off.
ps03_emulator_prepare() {
  local s
  ps03_adb shell input keyevent 82 >/dev/null 2>&1 || true
  for s in window_animation_scale transition_animation_scale animator_duration_scale; do
    ps03_adb shell settings put global "$s" 0 >/dev/null 2>&1 || true
  done
  ps03_adb reverse tcp:38088 tcp:38088 >/dev/null
  ps03_adb reverse tcp:38300 tcp:38300 >/dev/null
  ps03_adb shell 'echo "_ --disable-fre --no-default-browser-check --no-first-run" > /data/local/tmp/chrome-command-line'
  ps03_adb shell am set-debug-app --persistent com.android.chrome >/dev/null
}

# ps03_install_apk FILE PACKAGE: installs FILE; when PACKAGE is installed with another certificate,
# uninstalls it and installs again. Stops the script when the install fails.
ps03_install_apk() {
  local file=$1 pkg=$2 result
  [ -f "$file" ] || ps03_die "missing $file: build the apps first"
  result=$(ps03_adb install -r -t "$(ps03_host_path "$file")" 2>&1 || true)
  if printf '%s' "$result" | grep -q 'INSTALL_FAILED_UPDATE_INCOMPATIBLE\|INSTALL_FAILED_DUPLICATE_PERMISSION'; then
    ps03_warn "$pkg is installed with another certificate; uninstalling it first"
    ps03_adb uninstall "$pkg" >/dev/null 2>&1 || true
    result=$(ps03_adb install -r -t "$(ps03_host_path "$file")" 2>&1 || true)
  fi
  printf '%s' "$result" | grep -q Success || ps03_die "install of $file failed: $result"
}

# ---------------------------------------------------------------------------------------------
# Locks, off unless PS03_LOCK_DIR is set. A lock is a directory: mkdir is atomic on every file system.
# The names held by this process tree are kept in PS03_HELD_LOCKS, so a script started by another
# script that already holds a lock does not wait for itself.
# ---------------------------------------------------------------------------------------------
PS03_LOCK_TIMEOUT_MIN=${PS03_LOCK_TIMEOUT_MIN:-60}
PS03_LOCK_STALE_MIN=${PS03_LOCK_STALE_MIN:-90}

ps03_lock_held() { case ":${PS03_HELD_LOCKS:-}:" in *":$1:"*) return 0 ;; *) return 1 ;; esac; }

ps03_lock_acquire() {
  local name=$1 dir="$PS03_LOCK_DIR/$1" waited=0
  [ -n "$PS03_LOCK_DIR" ] && mkdir -p "$PS03_LOCK_DIR"
  until [ -z "$PS03_LOCK_DIR" ] || mkdir "$dir" 2>/dev/null; do
    # A lock older than the stale limit belongs to a job that died without releasing it.
    if [ -n "$(find "$dir" -maxdepth 0 -mmin +"$PS03_LOCK_STALE_MIN" 2>/dev/null)" ]; then
      ps03_warn "removing stale lock $name (older than $PS03_LOCK_STALE_MIN min)"
      rmdir "$dir" 2>/dev/null || true
      continue
    fi
    [ $waited -eq 0 ] && ps03_log "waiting for lock $name"
    sleep 5; waited=$((waited + 5))
    [ $waited -ge $((PS03_LOCK_TIMEOUT_MIN * 60)) ] && ps03_die "timeout after $PS03_LOCK_TIMEOUT_MIN min waiting for lock $name" 99
  done
  PS03_HELD_LOCKS="${PS03_HELD_LOCKS:-}:$name"
  export PS03_HELD_LOCKS
}

ps03_lock_release() {
  [ -z "$PS03_LOCK_DIR" ] || rmdir "$PS03_LOCK_DIR/$1" 2>/dev/null || true
  PS03_HELD_LOCKS=$(printf '%s' "${PS03_HELD_LOCKS:-}" | sed "s/:$1\$//; s/:$1:/:/")
  export PS03_HELD_LOCKS
}

# ps03_with_lock NAME CMD...: runs CMD while holding lock NAME, releases it on any exit or signal.
ps03_with_lock() {
  local name=$1 rc=0; shift
  if ps03_lock_held "$name"; then "$@"; return; fi
  ps03_lock_acquire "$name"
  # shellcheck disable=SC2064 # expand the name now: the trap runs after this function returns
  trap "ps03_lock_release '$name'" EXIT
  # shellcheck disable=SC2064 # expand the name now, as above
  trap "ps03_lock_release '$name'; exit 130" INT TERM
  "$@" || rc=$?
  ps03_lock_release "$name"
  trap - EXIT INT TERM
  return $rc
}

# ---------------------------------------------------------------------------------------------
# Go container. Go always runs in Linux here: Windows Application Control blocks freshly built
# test binaries at random (measured 2026-10-04), and CI runs Linux anyway.
# ---------------------------------------------------------------------------------------------
PS03_GO_MEMORY=${PS03_GO_MEMORY:-2g}
PS03_GO_CPUS=${PS03_GO_CPUS:-6}

# ps03_go_container [--network NAME] [--memory SIZE] [-e NAME=VALUE]... <dir> <command...>
#   Runs <command> in the Go image with the repository at /repo and the working directory
#   /repo/<dir>. Module, build and tool caches live in Docker volumes (bind-mounting a Windows
#   module cache was far too slow, measured 2026-10-04). $PS03_RUN_DIR/test-results is /results.
#   Does not take the go-PS03 lock: callers hold it (see ps03_with_lock).
ps03_go_container() {
  local network="" memory=$PS03_GO_MEMORY dir
  local -a envs=()
  while [ $# -gt 0 ]; do
    case $1 in
      --network) network=$2; shift 2 ;;
      --memory) memory=$2; shift 2 ;;
      -e) envs+=(-e "$2"); shift 2 ;;
      *) break ;;
    esac
  done
  [ $# -ge 2 ] || ps03_die "ps03_go_container: need <dir> <command...>"
  dir=$1; shift
  [ -d "$PS03_ROOT/$dir" ] || ps03_die "no such directory in the repository: $dir"
  mkdir -p "$PS03_RUN_DIR/test-results"

  local name="ps03-go-$$-${RANDOM}" cpus=$PS03_GO_CPUS ncpu
  # Docker refuses --cpus above the CPUs it has (a hosted CI runner has 2), so cap the setting.
  ncpu=$(docker info --format '{{.NCPU}}' 2>/dev/null || true)
  if [[ $ncpu =~ ^[0-9]+$ ]] && [[ $cpus =~ ^[0-9]+$ ]] && [ "$cpus" -gt "$ncpu" ]; then cpus=$ncpu; fi
  local -a args=(run --rm --init --name "$name" --memory "$memory" --cpus "$cpus"
    -v "$(ps03_host_path "$PS03_ROOT"):/repo"
    -v "$(ps03_host_path "$PS03_RUN_DIR/test-results"):/results"
    -v ps03-gomod:/go/pkg/mod -v ps03-gocache:/root/.cache/go-build -v ps03-gotools:/gotools
    -e GOTOOLCHAIN=local -e "CGO_ENABLED=${CGO_ENABLED:-0}"
    -w "/repo/${dir#./}")
  [ -n "${GOPROXY:-}" ] && args+=(-e "GOPROXY=$GOPROXY")
  [ -n "${GOFLAGS:-}" ] && args+=(-e "GOFLAGS=$GOFLAGS")
  [ -n "$network" ] && args+=(--network "$network")
  [ ${#envs[@]} -gt 0 ] && args+=("${envs[@]}")

  # The bind-mounted repository belongs to another user than the container's root, so git (used
  # by go build for version stamps, golangci-lint --new-from-rev) refuses it without this entry.
  local rc=0
  # shellcheck disable=SC2064 # expand the container name now: it is local to this function
  trap "docker rm -f '$name' >/dev/null 2>&1; exit 130" INT TERM
  MSYS_NO_PATHCONV=1 docker "${args[@]}" "$PS03_GO_IMAGE" \
    sh -c 'git config --global --add safe.directory /repo && exec "$@"' sh "$@" || rc=$?
  trap - INT TERM
  return $rc
}

# ---------------------------------------------------------------------------------------------
# Upstream imports. Each scripts/imports/<name>.manifest describes one unchanged import:
#   dir <folder in this repo>, repo <owner/name>, url, commit <full sha>, licence, clone <folder
#   under PS03_UPSTREAM_DIR>, header <text new team files in the folder must contain>,
#   path <upstream path> (one line per allow-listed file or directory).
# ---------------------------------------------------------------------------------------------

# ps03_manifest_value FILE KEY: first value of KEY.
ps03_manifest_value() { sed -n "s/^$2[[:space:]]\{1,\}//p" "$1" | head -n 1 | sed 's/[[:space:]]*$//'; }
# ps03_manifest_values FILE KEY: every value of KEY, one per line.
ps03_manifest_values() { sed -n "s/^$2[[:space:]]\{1,\}//p" "$1" | sed 's/[[:space:]]*$//'; }

# ps03_manifest_for DIR [ROOT]: the manifest whose dir is DIR (empty when none).
ps03_manifest_for() {
  local m root=${2:-$PS03_ROOT}
  for m in "$root"/scripts/imports/*.manifest; do
    [ -f "$m" ] || continue
    [ "$(ps03_manifest_value "$m" dir)" = "${1%/}" ] && { printf '%s\n' "$m"; return 0; }
  done
  return 0
}

# ps03_import_commit DIR [ROOT]: the oldest commit whose subject starts with "import: upstream"
# and that touches DIR. Empty when the folder has not been imported yet.
ps03_import_commit() {
  git -C "${2:-$PS03_ROOT}" log --reverse --format=%H --extended-regexp \
    --grep='^import: upstream ' -- "${1%/}" 2>/dev/null | head -n 1
}

# ps03_module_import_commit MODULE_DIR: import commit of the import folder that contains a module
# directory (thunder/backend -> import commit of thunder). Empty when not inside an import.
ps03_module_import_commit() {
  local top=${1#./}; top=${top%%/*}
  [ -n "$(ps03_manifest_for "$top")" ] && ps03_import_commit "$top"
  return 0
}

# ps03_upstream_clone MANIFEST: path of a git repository that contains the manifest's commit.
# Looks in $PS03_UPSTREAM_DIR/<clone>, $PS03_UPSTREAM_DIR/<dir> and $PS03_RUN_DIR/upstream-cache/<dir>;
# when none has it, fetches only that commit from the manifest URL into the cache folder.
ps03_upstream_clone() {
  local m=$1 dir clone commit url c cache
  dir=$(ps03_manifest_value "$m" dir); clone=$(ps03_manifest_value "$m" clone)
  commit=$(ps03_manifest_value "$m" commit); url=$(ps03_manifest_value "$m" url)
  cache=$PS03_RUN_DIR/upstream-cache/$dir
  for c in ${clone:+"$PS03_UPSTREAM_DIR/$clone"} "$PS03_UPSTREAM_DIR/$dir" "$cache"; do
    if [ -d "$c" ] && git -C "$c" cat-file -e "$commit^{commit}" 2>/dev/null; then
      printf '%s\n' "$c"; return 0
    fi
  done
  [ -n "$url" ] || ps03_die "commit $commit not found locally and $m has no url"
  ps03_log "fetching $url $commit into $cache"
  mkdir -p "$cache"
  git -C "$cache" init -q
  git -C "$cache" fetch -q --depth 1 "$url" "$commit" >&2 || ps03_die "could not fetch $commit from $url"
  printf '%s\n' "$cache"
}
