#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Points this clone's git hooks at scripts/hooks (local git config, nothing is committed).
#
#   bash scripts/install-hooks.sh
#
# - scripts/hooks/pre-commit keeps executable bits in path-limited commits on Windows.
# - Also sets core.longpaths (upstream test fixtures have long paths on Windows).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

git -C "$PS03_ROOT" config core.hooksPath scripts/hooks
git -C "$PS03_ROOT" config core.longpaths true
echo "install-hooks: core.hooksPath=$(git -C "$PS03_ROOT" config core.hooksPath)"
