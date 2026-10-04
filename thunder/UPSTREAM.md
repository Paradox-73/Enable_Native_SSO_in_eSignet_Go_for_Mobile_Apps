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

### Planned

| Work package | Change |
|---|---|
| WP-07 | Code grant: `device_sso` allowed-scope check, `sid`, device secret issuance through the provider, `ds_hash` in the `id_token` |
| WP-10 | Token exchange with a device secret as `actor_token`: provider validation, `id_token` checks (`ds_hash`, `sid`, DPoP `cnf.jkt`), scope rules |
| WP-12 | Scope-subset rules, re-consent signal, discovery metadata |

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
