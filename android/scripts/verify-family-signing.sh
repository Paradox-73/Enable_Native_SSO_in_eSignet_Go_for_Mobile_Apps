#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Checks that the Super App and Child App APKs are signed with one and the same certificate, the
# condition for the signature-level permission that guards the family single sign-on service.
#
#   bash android/scripts/verify-family-signing.sh [debug|release]
#
# Needs ANDROID_HOME or ANDROID_SDK_ROOT (build-tools 34.0.0) and java on PATH; build the APKs first
# (bash gradlew assembleDebug). Prints each APK's certificate SHA-256 digest, then
# "family-signing: OK <digest>" or "family-signing: FAILED ..." (exit status 1).
set -euo pipefail

variant=${1:-debug}
here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
apksigner_jar=${ANDROID_HOME:-${ANDROID_SDK_ROOT:?set ANDROID_HOME to the Android SDK}}/build-tools/34.0.0/lib/apksigner.jar
[ -f "$apksigner_jar" ] || { echo "family-signing: FAILED (no $apksigner_jar)"; exit 1; }

digests=()
for app in superapp childapp; do
  apk=$here/$app/build/outputs/apk/$variant/$app-$variant.apk
  [ -f "$apk" ] || { echo "family-signing: FAILED (missing $apk; build it first)"; exit 1; }
  out=$(java -jar "$apksigner_jar" verify --print-certs "$apk")
  signers=$(grep -c 'certificate SHA-256 digest' <<<"$out" || true)
  [ "$signers" = 1 ] || { echo "family-signing: FAILED ($app has $signers signers, expected 1)"; exit 1; }
  digest=$(sed -n 's/.*certificate SHA-256 digest: //p' <<<"$out")
  echo "$app: $(sed -n 's/.*certificate DN: //p' <<<"$out"), SHA-256 $digest"
  digests+=("$digest")
done

if [ "${digests[0]}" = "${digests[1]}" ]; then
  echo "family-signing: OK ${digests[0]}"
else
  echo "family-signing: FAILED (certificates differ)"
  exit 1
fi
