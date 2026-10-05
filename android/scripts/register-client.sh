#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Registers an installed debug app as an eSignet client, with the public half of its Keystore client key.
#
#   bash android/scripts/register-client.sh [superapp|childapp]     (default superapp)
#
# - The app writes files/client-registration.json on start (debug builds only): client_id, redirect
#   URI, ACR and the public JWK of its private_key_jwt key. This script reads it with
#   `adb shell run-as`, so the private key never leaves Android Keystore and nothing secret is copied.
# - Starts the app once if the file is not there yet and waits up to 60 s for it.
# - POST $PS03_ISSUER/client-mgmt/client (default issuer http://localhost:38088): FAPI 2.0 client,
#   private_key_jwt, PAR and PKCE required, DPoP-bound tokens, relying party `$PS03_RELYING_PARTY`
#   (default ps03-family). Both apps get the authorization-code and token-exchange grants and may ask
#   for `device_sso` (additionalConfig.allowed_authorization_scopes): Hub to receive a device secret,
#   Health Card because eSignet lets only a client allowed `device_sso` use one (Native SSO, rule R13). A client_id that already exists counts as registered (the client_id is
#   derived from the key, so the same id means the same key).
# - Prints `register-client: <client_id> ACTIVE` (or `already registered`). Uses ANDROID_SERIAL when set.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
PS03_SCRIPT_NAME=register-client

case ${1:-} in -h|--help) ps03_usage ;; esac
APP=${1:-superapp}
case $APP in superapp|childapp) ;; *) ps03_die "unknown app $APP (superapp or childapp)" 2 ;; esac
PACKAGE=org.badmosip.nativesso.$APP
ISSUER=${PS03_ISSUER:-http://localhost:38088}
RP=${PS03_RELYING_PARTY:-ps03-family}
SDK=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
ADB=${SDK:+$SDK/platform-tools/}adb
adb_() { MSYS_NO_PATHCONV=1 "$ADB" ${ANDROID_SERIAL:+-s "$ANDROID_SERIAL"} "$@"; }
PY=python3
"$PY" -c 'import json' >/dev/null 2>&1 || PY=python

read_info() { adb_ exec-out run-as "$PACKAGE" cat files/client-registration.json 2>/dev/null || true; }

info=$(read_info)
if ! printf '%s' "$info" | grep -q '"client_id"'; then
  ps03_log "starting $PACKAGE once so it creates its keys"
  adb_ shell am start -n "$PACKAGE/.MainActivity" >/dev/null
  for _ in $(seq 1 30); do
    sleep 2
    info=$(read_info)
    printf '%s' "$info" | grep -q '"client_id"' && break
  done
fi
printf '%s' "$info" | grep -q '"client_id"' || ps03_die "no files/client-registration.json in $PACKAGE (a debug build is needed)"

body=$(printf '%s' "$info" | RP=$RP "$PY" -c '
import json, os, sys, datetime
info = json.load(sys.stdin)
jwk = {k: v for k, v in info["public_jwk"].items() if k in ("kty", "crv", "x", "y", "kid", "use", "alg")}
now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")
print(json.dumps({"requestTime": now, "request": {
    "clientId": info["client_id"],
    "clientName": info["client_id"],
    "clientNameLangMap": {"eng": info["client_id"]},
    "relyingPartyId": os.environ["RP"],
    "logoUri": "https://example.com/logo.png",
    "redirectUris": [info["redirect_uri"]],
    "userClaims": ["name", "email", "phone_number"],
    "authContextRefs": [info.get("acr") or "mosip:idp:acr:generated-code"],
    "publicKey": jwk,
    "grantTypes": ["authorization_code", "urn:ietf:params:oauth:grant-type:token-exchange"],
    "clientAuthMethods": ["private_key_jwt"],
    "additionalConfig": {
        "require_pushed_authorization_requests": True,
        "dpop_bound_access_tokens": True,
        "require_pkce": True,
        "allowed_authorization_scopes": ["device_sso"],
    },
}}))')
client_id=$(printf '%s' "$info" | "$PY" -c 'import json,sys; print(json.load(sys.stdin)["client_id"])')

answer=$(curl -sS -X POST "$ISSUER/client-mgmt/client" -H 'Content-Type: application/json' --data-binary "$body")
if printf '%s' "$answer" | grep -q '"status" *: *"ACTIVE"'; then
  echo "register-client: $client_id ACTIVE"
elif printf '%s' "$answer" | grep -qi 'duplicate\|already exist'; then
  echo "register-client: $client_id already registered"
else
  ps03_die "registration of $client_id failed: $answer"
fi
