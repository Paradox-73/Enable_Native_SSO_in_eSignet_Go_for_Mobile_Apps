#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Runs the local stack (deploy/compose.yaml, compose project ps03-native-sso).
#
#   bash deploy/stack.sh up       build eSignet from this repository, start everything, wait until healthy
#   bash deploy/stack.sh smoke    check health, discovery, JWKS, login pages; prints `stack smoke: PASS`
#   bash deploy/stack.sh status   docker compose ps
#   bash deploy/stack.sh logs [SERVICE...]   docker compose logs (no follow)
#   bash deploy/stack.sh restart SERVICE...  recreate services with the current environment, wait healthy
#   bash deploy/stack.sh down     stop everything and delete the volumes (database, keystore)
#
# - PS03_RUNTIME_STORE=redis at `up` adds the compose profile "redis": eSignet keeps its runtime store in a
#   Redis container instead of in memory. `up` saves the choice in stack.env; restart, logs, status and down
#   read it from there and ignore the variable.
# - Passwords for Postgres and the eSignet keystore are generated on first use into
#   $PS03_RUN_DIR/stack.env (outside git) and passed to compose with --env-file.
# - Does not take the docker-stack lock; scripts/with-stack.sh does (up, command, down).
# - Ports: 38455 Postgres, 38082 mock identity, 38088 eSignet (issuer http://localhost:38088),
#   38300 oidc-ui, all published on 127.0.0.1 only.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib.sh"
PS03_SCRIPT_NAME=stack

PROJECT=ps03-native-sso
COMPOSE_FILE=$PS03_ROOT/deploy/compose.yaml
ENV_FILE=$PS03_RUN_DIR/stack.env
ISSUER=http://localhost:38088
UP_TIMEOUT_S=${PS03_STACK_UP_TIMEOUT_S:-300}

random_secret() { head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

ensure_env() {
  if [ ! -s "$ENV_FILE" ]; then
    mkdir -p "$(dirname "$ENV_FILE")"
    umask 077
    {
      echo "# Written by deploy/stack.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ). Local test stack only."
      echo "PS03_DB_PASSWORD=$(random_secret)"
      echo "PS03_KEYSTORE_PASSWORD=$(random_secret)"
    } > "$ENV_FILE"
    ps03_log "created $ENV_FILE"
  fi
  # Files written before the Redis profile existed lack its password.
  grep -q '^PS03_REDIS_PASSWORD=' "$ENV_FILE" || echo "PS03_REDIS_PASSWORD=$(random_secret)" >> "$ENV_FILE"
}

# runtime_store: the store `up` saved in stack.env (inmemory when the file has none). Only `up` reads the
# shell's PS03_RUNTIME_STORE, so every later command of a stack, e2e/run.sh included, uses the same store.
runtime_store() {
  local store
  store=$(sed -n 's/^PS03_RUNTIME_STORE=//p' "$ENV_FILE" 2>/dev/null | tail -1)
  check_store "${store:-inmemory}" && echo "${store:-inmemory}"
}

check_store() {
  case $1 in
    inmemory|redis) ;;
    *) ps03_die "PS03_RUNTIME_STORE must be inmemory or redis, got $1" ;;
  esac
}

compose() {
  local store; store=$(runtime_store) || exit 1
  local -a profile=()
  [ "$store" = redis ] && profile=(--profile redis)
  PS03_RUNTIME_STORE=$store docker compose -p "$PROJECT" -f "$(ps03_host_path "$COMPOSE_FILE")" \
    --env-file "$(ps03_host_path "$ENV_FILE")" "${profile[@]}" "$@"
}

# wait_http NAME URL PATTERN: polls URL until its body matches PATTERN or UP_TIMEOUT_S passes.
wait_http() {
  local name=$1 url=$2 pattern=$3 t0 body
  t0=$(date +%s)
  while :; do
    body=$(curl -s --max-time 5 "$url" 2>/dev/null || true)
    if printf '%s' "$body" | grep -q "$pattern"; then
      ps03_log "$name ready after $(( $(date +%s) - t0 )) s"
      return 0
    fi
    [ $(( $(date +%s) - t0 )) -ge "$UP_TIMEOUT_S" ] && { ps03_warn "$name not ready after $UP_TIMEOUT_S s"; return 1; }
    sleep 3
  done
}

cmd_up() {
  ensure_env
  local store=${PS03_RUNTIME_STORE:-inmemory}
  check_store "$store"
  # grep and mv, not sed -i: BSD sed on macOS needs a different -i syntax.
  (umask 077; { grep -v '^PS03_RUNTIME_STORE=' "$ENV_FILE" || true; echo "PS03_RUNTIME_STORE=$store"; } > "$ENV_FILE.tmp")
  mv "$ENV_FILE.tmp" "$ENV_FILE"
  local t0; t0=$(date +%s)
  ps03_log "building eSignet image and starting $PROJECT"
  compose up -d --build --wait --wait-timeout "$UP_TIMEOUT_S" >&2 \
    || { compose ps >&2 || true; compose logs --tail 60 esignet >&2 || true; ps03_die "compose up failed"; }
  wait_http "mock identity" http://localhost:38082/v1/mock-identity-system/actuator/health '"status":"UP"' \
    || ps03_die "mock identity system did not become healthy"
  echo "stack up: $PROJECT ready in $(( $(date +%s) - t0 )) s (issuer $ISSUER)"
}

# restart SERVICE...: recreates the named services from the built image, with the current
# environment (for example PS03_DEVICE_SECRET_VALIDITY_SECONDS), and waits until they are healthy.
cmd_restart() {
  ensure_env
  [ $# -ge 1 ] || ps03_die "restart: name the services (for example esignet)"
  local t0; t0=$(date +%s)
  compose up -d --no-build --no-deps --force-recreate --wait --wait-timeout "$UP_TIMEOUT_S" "$@" >&2     || { compose logs --tail 60 "$@" >&2 || true; ps03_die "restart of $* failed"; }
  echo "stack restart: $* ready in $(( $(date +%s) - t0 )) s"
}

cmd_down() {
  if [ -s "$ENV_FILE" ]; then compose --profile '*' down -v --remove-orphans >&2
  else PS03_DB_PASSWORD=x PS03_KEYSTORE_PASSWORD=x docker compose -p "$PROJECT" \
    -f "$(ps03_host_path "$COMPOSE_FILE")" --profile '*' down -v --remove-orphans >&2; fi
  echo "stack down: $PROJECT"
}

cmd_smoke() {
  local fails=0 body code
  check() { if [ "$2" = ok ]; then ps03_log "ok   $1"; else ps03_log "FAIL $1: $3"; fails=$((fails + 1)); fi; }

  body=$(curl -s --max-time 5 "$ISSUER/health" || true)
  if printf '%s' "$body" | grep -qi 'up\|ok'; then check "eSignet /health" ok; else check "eSignet /health" no "$body"; fi

  body=$(curl -s --max-time 5 "$ISSUER/.well-known/openid-configuration" || true)
  if printf '%s' "$body" | grep -q "\"issuer\":\"$ISSUER\""; then check "discovery issuer $ISSUER" ok
  else check "discovery issuer" no "$(printf '%s' "$body" | head -c 200)"; fi
  if printf '%s' "$body" | grep -q '"pushed_authorization_request_endpoint":"'"$ISSUER"'/oauth2/par"'; then
    check "discovery PAR endpoint" ok; else check "discovery PAR endpoint" no "missing"; fi
  if printf '%s' "$body" | grep -q '"dpop_signing_alg_values_supported":\[[^]]*"ES256"'; then
    check "discovery DPoP ES256" ok; else check "discovery DPoP ES256" no "missing"; fi

  body=$(curl -s --max-time 5 "$ISSUER/oauth2/jwks" || true)
  if printf '%s' "$body" | grep -q '"kid"'; then check "JWKS has a key" ok; else check "JWKS" no "$(printf '%s' "$body" | head -c 200)"; fi

  body=$(curl -s --max-time 5 http://localhost:38082/v1/mock-identity-system/actuator/health || true)
  if printf '%s' "$body" | grep -q '"status":"UP"'; then check "mock identity health" ok; else check "mock identity health" no "$(printf '%s' "$body" | head -c 200)"; fi

  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://localhost:38300/ || true)
  if [ "$code" = 200 ]; then check "oidc-ui login pages" ok; else check "oidc-ui login pages" no "HTTP $code"; fi
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://localhost:38300/.well-known/openid-configuration || true)
  if [ "$code" = 200 ]; then check "oidc-ui proxy to eSignet" ok; else check "oidc-ui proxy to eSignet" no "HTTP $code"; fi

  if [ $fails -eq 0 ]; then echo "stack smoke: PASS"; else echo "stack smoke: FAIL ($fails checks)"; return 1; fi
}

[ $# -ge 1 ] || ps03_usage
command=$1; shift
case $command in
  up) cmd_up ;;
  down) cmd_down ;;
  restart) cmd_restart "$@" ;;
  smoke) cmd_smoke ;;
  status) ensure_env; compose ps ;;
  logs) ensure_env; compose logs --no-color "$@" ;;
  -h|--help|help) ps03_usage ;;
  *) ps03_usage ;;
esac
