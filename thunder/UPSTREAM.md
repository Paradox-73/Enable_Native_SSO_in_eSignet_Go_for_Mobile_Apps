# Thunder ID engine: upstream import and team changes

This folder holds the Thunder ID identity engine that eSignet Go v2.0.0 embeds, plus the team's changes for
OpenID Connect Native SSO for Mobile Apps 1.0 ("Native SSO"). This file says where the code came from, how to
check that the import is unchanged, and what the team changed on top of it.

## Source

| Item | Value |
|---|---|
| Upstream repository | [thunder-id/thunderid](https://github.com/thunder-id/thunderid) |
| Version | v1.0.1 |
| Commit | `12f517c4384004e360e26eb7eb1ab0501b678bb2` (the commit eSignet v2.0.0 pins in `esignet-service/go.mod`) |
| Licence | Apache-2.0 ([LICENSE](LICENSE), unchanged) |
| Import commit in this repository | `c4c5443` (`import: upstream thunder-id/thunderid@12f517c4 (Apache-2.0)`) |
| Imported paths | `LICENSE`, `api/oauth2.yaml`, `backend/` without one contributor-tool instruction file (1,680 files) |
| Allow-list | [`../scripts/imports/thunder.manifest`](../scripts/imports/thunder.manifest) |

- The files were taken with `git archive` of the upstream commit, line-ending conversion off, so every file is
  byte for byte the upstream blob.
- Everything else upstream (web frontends, Helm charts, other tools) is left out because this work does not
  change it. `bash scripts/verify-import.sh thunder --list-excluded` prints every left-out file.

## Checking the import

Run from the repository root:

```bash
bash scripts/verify-import.sh thunder      # compares the import commit with the upstream commit
git diff c4c5443 -- thunder/               # every team change since the import
```

`verify-import.sh` compares the blob id and file mode of every allow-listed file and ends with
`0 differ, 0 missing` when the import commit is identical to upstream.

## Team changes

New Go files carry Thunder's own header (`SPDX-License-Identifier: Apache-2.0`), and the changes follow
Thunder's package layout, error style (`common.ServiceError` with i18n keys), testify suites and mockery mocks,
so they can be offered upstream.

### Native SSO provider contract (work package WP-02)

This step adds only names, types and settings; no grant handler uses them yet. With no provider passed, the
engine behaves exactly as upstream.

| Change | File (under `backend/` unless noted) |
|---|---|
| `DeviceSecretProvider` interface: `GenerateDeviceSecret` and `ValidateDeviceSecret` | `pkg/thunderidengine/providers/interface.go` |
| Request and result types `DeviceSecretRequest` (with `Validate`), `DeviceSecret`, `DeviceSecretClaims`; errors `ErrorInvalidDeviceSecret` (`NSSO-1001`) and `ErrorDeviceSecretSessionEnded` (`NSSO-1002`) | `pkg/thunderidengine/providers/device_secret.go` |
| Shared names: `ScopeDeviceSSO` (`device_sso`), `TokenTypeDeviceSecret` (`urn:openid:params:token-type:device-secret`), runtime-store namespace `NamespaceNativeSSOSession` (`nativesso:session`) | `pkg/thunderidengine/providers/constants.go` |
| Postgres partition for the new namespace (the schema comment requires one per namespace) | `dbscripts/runtime_transient/postgres.sql` |
| OAuth names: `ScopeDeviceSSO`, `TokenTypeIdentifierDeviceSecret`, `ClaimSessionID` (`sid`), `ClaimDeviceSecretHash` (`ds_hash`), `ErrorInteractionRequired` | `internal/oauth/oauth2/constants/constants.go` |
| `OAuthConfig.NativeSSO` settings (yaml `native_sso`; eSignet reads them as `oauth.native_sso`) with validation | `pkg/thunderidengine/config/config.go`, `validate.go` |
| `WithDeviceSecretProvider` engine option; a nil provider leaves Native SSO off; settings checked at engine start | `pkg/thunderidengine/engine.go` |
| `device_secret` in the token response | `internal/oauth/oauth2/model/token.go`, `internal/oauth/oauth2/token/service.go`, `api/oauth2.yaml` |
| Default messages for the two new error keys | `internal/system/i18n/core/defaults.go` |
| Mock `DeviceSecretProviderMock` (mockery entry and generated file) | `.mockery.public.yml`, `tests/mocks/devicesecretprovidermock/` |
| Ignore the SQLite files Thunder's own tests write (upstream ignores them in its root `.gitignore`, which is not imported) | `thunder/.gitignore` |

Design decisions in this step:

- `device_sso` is a separate constant, not a `StandardOIDCScopes` entry. The standalone server advertises the
  standard scopes and gives them to every new client; `device_sso` must be granted only to clients whose allowed
  scopes include it, and it releases no user claims.
- `urn:openid:params:token-type:device-secret` is not added to the generic list of supported token types, so the
  existing token exchange keeps rejecting it. Only the Native SSO branch of the token exchange (WP-10) accepts it,
  and only as `actor_token_type`.
- The engine treats a device secret as opaque. Its format (a signed, then encrypted JWT in eSignet), its keys and
  how login sessions are tracked belong to the provider, which eSignet implements (WP-08).
- `NativeSSOConfig` fields:
  - `id_token_max_age_seconds`: cap on the age of the `id_token` presented in a token exchange; `0` means no
    cap beyond the device secret and its session. Negative values are rejected.
  - `scope_exceeded_error`: error code when a token exchange asks for more scopes than were granted with the
    device secret: `interaction_required` (default) or `invalid_scope`. Other values are rejected.
  - `exchange_only`: when true, the token-exchange grant accepts only the Native SSO profile.

### Native SSO at PAR, authorize and the authorization-code grant (work package WP-07)

With no provider passed, `device_sso` is refused with `invalid_scope` and every other login behaves as upstream.

| Change | File (under `backend/internal/oauth/` unless noted) |
|---|---|
| New package `nativesso`: `RequestsDeviceSSO`, `ValidateDeviceSSOScope` (`invalid_scope` when Native SSO is off, the client's `Scopes` lack `device_sso`, or `openid` is missing), `NewSessionID` (128 random bits, base64url), `DeviceSecretHash` (`ds_hash`), `ReleasedClaimNames` | `oauth2/nativesso/nativesso.go` |
| `device_sso` stays with the OpenID scopes, so it reaches the authorization code | `oauth2/utils/oauthutils.go` |
| PAR and authorize apply `ValidateDeviceSSOScope` | `oauth2/par/service.go`, `oauth2/authz/validator.go`, `oauth2/authz/service.go` |
| `Config.NativeSSOEnabled`, set by `oauth.Initialize` from the provider (true only when it is not nil) | `config/config.go`, `init.go` |
| The provider is passed from the engine through `oauth.Initialize`, `granthandlers.Initialize` and `newGrantHandlerProvider` to the authorization-code handler; the standalone server passes nil | `init.go`, `oauth2/granthandlers/init.go`, `provider.go`, `backend/pkg/thunderidengine/engine.go`, `backend/cmd/server/servicemanager.go` |
| A code granted `device_sso` carries its authorization request id (`AuthorizationCode.AuthID`) | `oauth2/authz/model.go`, `oauth2/authz/service.go` |
| Code grant with `device_sso`: client still allowed and provider present (else `invalid_scope`); DPoP proof present (else `invalid_request`); new `sid`; `GenerateDeviceSecret`; attribute cache kept for the longest of device secret, access token and refresh token; `id_token` with `sid` and `ds_hash`; `device_secret` in the response. Any failure after the checks is `server_error` with no tokens | `oauth2/granthandlers/authorization_code.go`, `authorization_code_native_sso.go` |
| `IDTokenBuildContext.SessionID` and `DeviceSecretHash`, written after the user claims so no attribute can replace them | `oauth2/tokenservice/model.go`, `builder.go` |

Design decisions in this step:

- The device secret is requested after the access token is built and before the `id_token`, so a request refused
  by a check leaves no login session in the provider. If the `id_token` cannot be built after that, the session
  record stays until its TTL ends; Thunder has no call to delete it.
- The attribute cache TTL is set to the longest of the three lifetimes plus Thunder's usual 60 s buffer, never
  shorter: `ExtendAttributeCacheTTL` replaces the TTL, and a refresh token (24 h by default) can outlive the device
  secret (8 h by default in eSignet).
- `claims` in the device secret holds names only. With eSignet's opaque attribute token (`_jwt`) Thunder cannot
  see which values exist, so it lists every name the granted scopes and the `claims` parameter release.

### Native SSO token exchange with a device secret (work package WP-10)

A token exchange whose `actor_token_type` is `urn:openid:params:token-type:device-secret` takes its own branch
before the generic exchange looks at the actor token, the way an ID-JAG request does. With no provider passed the
type is refused with `invalid_request`, as before.

| Change | File (under `backend/internal/oauth/` unless noted) |
|---|---|
| `ValidateGrant`: the draft-06 URN `urn:x-oath:...:device-secret` is refused naming the new URN; Native SSO requests need `subject_token_type` `...:id_token`, an `actor_token`, no `requested_token_type` other than an access token, and exactly one `audience` equal to the issuer (`invalid_target` otherwise); with `oauth.native_sso.exchange_only` every other token exchange is `invalid_request` | `oauth2/granthandlers/token_exchange.go`, `token_exchange_native_sso.go` |
| `HandleGrant` checks, in the order of design section 11: requesting client allows `device_sso` (`unauthorized_client`); `ValidateDeviceSecret` (session ended: `invalid_grant`; other client errors: `invalid_request`; server errors: `server_error`); returned claims re-checked (issuer, expiry, client, subject, `sid`, `cnf.jkt`); `id_token`; `aud` = client of the device secret, same `sub`; optional age cap; `ds_hash`; `sid`; same organization unit and the first client still allows `device_sso`; `dpop.VerifyProofBinding` against the device secret's `cnf.jkt` (`invalid_grant`) | `oauth2/granthandlers/token_exchange_native_sso.go` |
| On success: an access token for the requesting client, same `sub`, the device secret's scope without `device_sso`, bound to the request's DPoP key | same |
| `TokenValidatorInterface.ValidateNativeSSOIDToken`: typ `JWT`, no refresh-token shape, issuer is this server, signature, `sub`, `iat`, `nbf`, deny list; `exp` not enforced | `oauth2/tokenservice/validator.go`, `validator_native_sso.go`; mock regenerated |
| The token-exchange handler receives the actor provider and the device secret provider | `oauth2/granthandlers/provider.go` |

Design decisions in this step:

- An expired `id_token` is accepted (Native SSO 6.3, design section 11.1); `oauth.native_sso.id_token_max_age_seconds`
  caps its age since `iat` when set.
- The first client's organization unit and `device_sso` are read at exchange time, not from the device secret,
  so removing `device_sso` from the first client stops sharing at once.
- `ds_hash` and `sid` are compared in constant time; error descriptions never carry tokens or user data.

### Native SSO scope rules, second-app tokens and discovery (work package WP-12)

| Change | File (under `backend/internal/oauth/` unless noted) |
|---|---|
| `ValidateGrant` (Native SSO branch): a requested `scope` with `device_sso` or without `openid` is `invalid_scope` | `oauth2/granthandlers/token_exchange_native_sso.go` |
| After the DPoP binding check: no scope gives the device secret's scope without `device_sso`; an equal or narrower scope is issued as requested; any value outside it answers `oauth.native_sso.scope_exceeded_error` (default `interaction_required`, else `invalid_scope`) with the missing values in `error_description` (the re-consent signal) | same |
| Second-app tokens: access token with the login's attribute-cache id (`aci`) and the request's DPoP key; `id_token` with `aud` = requesting client, same `sub`, `sid`, `acr`, `auth_time`, no `ds_hash`, no `nonce`; user attributes limited to the claims released to the first app and released by the resolved scope under the requesting client's scope mapping; an opaque `_jwt` attribute token is passed on; an expired attribute cache is `invalid_grant` | same; the handler now also receives the attribute cache service (`provider.go`, `token_exchange.go`) |
| OpenID Provider metadata: `native_sso_supported: true` and `device_sso` in `scopes_supported` only when a provider is configured and the token-exchange grant is allowed; otherwise `device_sso` is removed from `scopes_supported` even if configured | `oauth2/discovery/service.go`, `model.go` |
| Token endpoint description, `actor_token_type` and the 400 answer document the Native SSO exchange | `thunder/api/oauth2.yaml` |

Design decisions in this step:

- The scope check runs after every binding check (design section 11, step 13), so `interaction_required` is given
  only to an app that holds a genuine device secret and the first app's DPoP key.
- `grant_types_supported` is not changed: the token-exchange grant appears there exactly when it is allowed, and
  `native_sso_supported` is false when it is not.


### Device secret revocation (rule R27)

The app a device secret was issued to revokes it at the RFC 7009 endpoint; the login session ends, so every later
exchange with it is `invalid_grant`.

| Change | File (under `backend/` unless noted) |
|---|---|
| `DeviceSecretProvider.EndDeviceSecretSession` (mock regenerated in mockery's format) | `pkg/thunderidengine/providers/interface.go`, `tests/mocks/devicesecretprovidermock/` |
| `TokenTypeHintDeviceSecret` (`device_secret`) | `internal/oauth/oauth2/constants/constants.go` |
| Device secret branch of `POST /oauth2/revoke`: chosen by `token_type_hint=device_secret`, or no hint and a compact JWE; invalid or ended values are a no-op `200`; another client's device secret `invalid_grant`; then `EndDeviceSecretSession` | `internal/oauth/oauth2/revocation/device_secret.go`, `handler.go`, `init.go` |
| The endpoint is registered when token revocation is on **or** the engine has a provider; with only the provider, other tokens get `unsupported_token_type` (RFC 7009 2.2.1) | `internal/oauth/init.go` |
| `revocation_endpoint` in discovery in the same two cases | `internal/oauth/oauth2/discovery/service.go` |
| API description | `api/oauth2.yaml` |

Tests: `revocation/device_secret_test.go` (routing, ownership, no-op cases, server errors, `unsupported_token_type`,
route registration without the deny list), `discovery/discovery_native_sso_test.go`
(`TestDiscovery_NativeSSOAdvertisesRevocationEndpoint`).

Results measured on 2026-10-05 for R27:

- `go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover`: 32 packages, 2379 tests, 0 failed (1
  skipped by upstream); 376 of 396 statements on changed lines covered (94.9 %); `revocation/device_secret.go`
  20/20, `revocation/handler.go` 11/11, `discovery/service.go` 15/15. `internal/oauth/init.go` (0/18) has no
  unit test upstream either; the live suite reaches it.
- `go-lint.sh thunder/backend -- --timeout=30m`: 0 issues.


## Tests and checks

Run from the repository root (Go runs in a container; see [`../scripts/README.md`](../scripts/README.md)):

```bash
bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/...
bash scripts/go-lint.sh thunder/backend
# Mocks are generated, never edited; after changing an interface, regenerate both sets:
bash scripts/go.sh thunder/backend run github.com/vektra/mockery/v3@v3.5.5 --config .mockery.public.yml
bash scripts/go.sh thunder/backend run github.com/vektra/mockery/v3@v3.5.5 --config .mockery.private.yml
```

Unit tests added for the contract:

| Test file (under `backend/`) | Checks |
|---|---|
| `pkg/thunderidengine/providers/device_secret_test.go` | required request fields, `device_sso` in the scopes, JSON field names, error types and i18n keys |
| `pkg/thunderidengine/config/native_sso_test.go` | settings validation and the `interaction_required` default |
| `pkg/thunderidengine/engine_native_sso_test.go` | the option sets the provider; omitted or nil leaves Native SSO off; bad settings stop the engine |
| `internal/oauth/oauth2/constants/constants_test.go` | protocol names; `device_sso` is not a standard scope; the device-secret type is not a generic token type |
| `internal/oauth/oauth2/model/token_test.go` | `device_secret` is in the JSON only when set |
| `internal/oauth/oauth2/token/service_test.go` | the token service copies the device secret into the response |

Unit tests added for WP-07 (all names contain `DeviceSSO`, `DeviceSecret` or `NativeSSO`):

| Test file (under `backend/internal/oauth/oauth2/`) | Checks |
|---|---|
| `nativesso/nativesso_test.go` | every `ValidateDeviceSSOScope` branch; `NewSessionID` length and uniqueness; `DeviceSecretHash` against fixed SHA-256 vectors; `ReleasedClaimNames` with plain and opaque attributes |
| `authz/validator_native_sso_test.go`, `par/service_native_sso_test.go` | allowed client passes; client not allowed, no provider, no `openid`: `invalid_scope` |
| `authz/service_native_sso_test.go` | a `device_sso` code carries `AuthID`; other codes do not |
| `utils/oauthutils_native_sso_test.go` | `device_sso` stays an OpenID scope and releases no claims |
| `granthandlers/authorization_code_native_sso_test.go` | allowed client gets `device_secret`, `sid`, `ds_hash`, full provider request; client not allowed and no provider: `invalid_scope`; no DPoP proof: `invalid_request`; provider error, empty or expired secret, incomplete request, cache failure: `server_error` and no tokens; no `device_sso`: response unchanged; cache TTL |
| `tokenservice/builder_native_sso_test.go` | `sid` and `ds_hash` in the `id_token`, not replaceable by user attributes, absent without a device secret |

Results measured on 2026-10-05 for WP-07:

- `go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover`: 32 packages, 2270 tests, 0 failed (1
  skipped by upstream); 135 of 145 statements on changed lines covered (93.1 %). Not covered: `internal/oauth/init.go`
  (9) and `granthandlers/init.go` (1), whose packages have no unit tests; the engine test `TestNew_HappyPath` runs
  them.
- `go.sh thunder/backend test -count=1 -v -run 'DeviceSSO|DeviceSecret|NativeSSO' ./internal/oauth/...`: 55 `PASS`
  lines (suites and cases), 0 `FAIL`.
- `go-lint.sh thunder/backend -- --timeout=30m`: 0 issues.
- No interface changed, so the mocks were not regenerated.

Unit tests added for WP-10:

| Test file (under `backend/internal/oauth/oauth2/`) | Checks |
|---|---|
| `granthandlers/token_exchange_native_sso_test.go` | request shape (old URN, token types, audience, requested type, no provider, exchange-only); success with an expired `id_token`; client without `device_sso`; every provider outcome; re-checked claims; forged `id_token`; `aud`, `sub`, `ds_hash`, `sid` mismatches; age cap inside and outside; other vendor; first-client lookup errors; DPoP key mismatch and missing proof; build failure |
| `tokenservice/validator_native_sso_test.go` | expired `id_token` accepted; wrong typ, refresh-token shape, other issuer, bad signature, missing `sub` or `iat`, future `nbf`, revoked: rejected |

Results measured on 2026-10-05 for WP-10:

- `go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover`: 32 packages, 2341 tests, 0 failed (1
  skipped by upstream); 276 of 287 statements on changed lines covered (96.2 %); `token_exchange_native_sso.go`
  99/100, `validator_native_sso.go` 35/35.
- `go.sh thunder/backend test -count=1 -v -run NativeSSO ./internal/oauth/oauth2/granthandlers/`: 73 `PASS` lines
  (55 of them in the token-exchange suite), 0 `FAIL`.
- `go-lint.sh thunder/backend -- --timeout=30m`: 0 issues.

Unit tests added for WP-12:

| Test file (under `backend/internal/oauth/oauth2/`) | Checks |
|---|---|
| `granthandlers/token_exchange_native_sso_scope_test.go` | `device_sso` in the exchange and no `openid`: `invalid_scope`; wider scope: `interaction_required` naming the missing values, `invalid_scope` by setting, and only after the binding checks; narrower and default scope; second-app access token and `id_token` fields; claim filtering by both apps; opaque attributes; attribute-cache errors; `id_token` build failure |
| `discovery/discovery_native_sso_test.go` | flag and `device_sso` with a provider; listed once; unrestricted grants; nothing without a provider or without the token-exchange grant |

Results measured on 2026-10-05 for WP-12:

- `go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover`: 32 packages, 2363 tests, 0 failed (1
  skipped by upstream); 341 of 352 statements on changed lines covered (96.9 %); `token_exchange_native_sso.go`
  151/152, `discovery/service.go` 13/13.
- `go.sh thunder/backend test -count=1 -v -run 'NativeSSO|Discovery' ./internal/oauth/...`: 149 `PASS` lines, 0
  `FAIL`.
- `openapi-lint.sh thunder/api/oauth2.yaml`: 0 errors, 13 warnings (the same 13 as the import), `OK`.
- `go-lint.sh thunder/backend -- --timeout=30m`: 0 issues.

Results measured on 2026-10-04 for the contract step:

- `verify-import.sh thunder`: 1680 files, 0 differ, 0 missing, 0 extra.
- `go-test.sh thunder/backend ./internal/oauth/... ./pkg/...`: 31 packages, 2214 tests, 0 failed (1 skipped by
  upstream); 22 of 22 statements on changed lines covered.
- Both mockery configurations regenerated; the committed mocks did not change.
- `internal/system/i18n/core/defaults.go` matches the output of upstream's `tools/i18n-extractor` at the same
  commit (the generator of that file, run from the upstream clone).
- `go-lint.sh thunder/backend` (golangci-lint v1.64.8, upstream's config, changed lines): 0 issues. On a busy
  machine the loading step needed more than the default 15 minutes: `bash scripts/go-lint.sh thunder/backend --
  --timeout=30m`.
