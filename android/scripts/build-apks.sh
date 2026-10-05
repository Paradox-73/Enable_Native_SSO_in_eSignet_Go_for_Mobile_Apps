#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Builds the APK sets the family single sign-on tests install (android/scripts/run-family-tests.sh):
#
#   bash android/scripts/build-apks.sh
#
# - family:   Hub, Health Card and Health Card's test APK, all signed with the family certificate.
# - intruder: Health Card and its test APK built with -Pps03.intruder=true (Android debug
#             certificate, family permission not declared).
# - takeover: the same with -Pps03.intruder=takeover (Android debug certificate, permission declared).
# - Each set is copied to $PS03_RUN_DIR/apks/<set>/ (hub.apk, child.apk, child-test.apk), because the
#   three builds write to the same Gradle output folder. Prints Gradle's BUILD SUCCESSFUL per set.
# - Needs JAVA_HOME 17+ and ANDROID_HOME. Exit status: the first failing build's.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
PS03_SCRIPT_NAME=build-apks

case ${1:-} in -h|--help) ps03_usage ;; esac
ANDROID_DIR=$PS03_ROOT/android
OUT=$PS03_RUN_DIR/apks
outputs=$ANDROID_DIR/childapp/build/outputs/apk

build_set() {
  local set=$1 property=$2
  shift 2
  ps03_log "building the $set set ($property)"
  (cd "$ANDROID_DIR" && bash gradlew --no-daemon "-Pps03.intruder=$property" "$@")
  rm -rf "${OUT:?}/$set"
  mkdir -p "$OUT/$set"
  cp "$outputs/debug/childapp-debug.apk" "$OUT/$set/child.apk"
  cp "$outputs/androidTest/debug/childapp-debug-androidTest.apk" "$OUT/$set/child-test.apk"
}

build_set family false :superapp:assembleDebug :childapp:assembleDebug :childapp:assembleDebugAndroidTest
cp "$ANDROID_DIR/superapp/build/outputs/apk/debug/superapp-debug.apk" "$OUT/family/hub.apk"
build_set intruder true :childapp:assembleDebug :childapp:assembleDebugAndroidTest
build_set takeover takeover :childapp:assembleDebug :childapp:assembleDebugAndroidTest
ps03_log "APK sets in $OUT: family, intruder, takeover"
