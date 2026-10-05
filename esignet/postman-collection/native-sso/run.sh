#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs the "Native SSO" folder with newman against the local stack.
#
#   bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh [newman flags]
#
# Needs newman 6 on PATH (toolchain/env-newman.sh on the team laptops, or `npm install -g newman`).
# Prints newman's report; exit status is newman's.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec newman run "$here/Go-eSignet.postman_collection.json" \
  -e "$here/Go-eSignet Native SSO (local stack).postman_environment.json" \
  --folder "Native SSO" --reporter-cli-no-banner "$@"
