#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs the go command for one module of this repository inside the golang:1.26-bookworm container.
#
#   bash scripts/go.sh [--network NAME] [-e NAME=VALUE]... <module dir> <go arguments...>
#
#   bash scripts/go.sh . version
#   bash scripts/go.sh esignet/esignet-service build -o /tmp/esignet ./cmd/esignet
#   bash scripts/go.sh --network ps03-native-sso_default e2e test -tags e2e ./...
#
# - The whole repository is mounted at /repo, so relative replace directives between modules
#   (esignet-service -> ../../thunder/backend) resolve exactly as on a developer machine.
# - With PS03_LOCK_DIR set, holds the go-PS03 lock while the container runs: at most one PS03 Go container (2 GB cap)
#   at a time on the shared Docker VM.
# - Exit status is the go command's.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

opts=()
while [ $# -gt 0 ]; do
  case $1 in
    --network|-e) [ $# -ge 2 ] || ps03_usage; opts+=("$1" "$2"); shift 2 ;;
    -h|--help) ps03_usage ;;
    *) break ;;
  esac
done
[ $# -ge 2 ] || ps03_usage
dir=$1; shift

ps03_with_lock go-PS03 ps03_go_container ${opts[@]+"${opts[@]}"} "$dir" go "$@"
