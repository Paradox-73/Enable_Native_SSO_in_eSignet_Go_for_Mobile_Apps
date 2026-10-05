# Offering the eSignet changes upstream

Notes for opening pull requests against [mosip/esignet](https://github.com/mosip/esignet). **Nothing has been sent.**
Written 2026-10-05. The eSignet changes depend on the Thunder changes ([thunder.md](thunder.md)): they use the provider
interface and the engine option that piece 1 adds, so send them after it is merged or in the same release.

## What to send

The import commit is `4eac1cc`; the team's changes are `git diff 4eac1cc -- esignet/`.

| # | Piece | Commit | Contents |
|---|---|---|---|
| 1 | Token-exchange grant at client registration | `9314b2b` | `internal/clientmgmt/validate.go` accepts `urn:ietf:params:oauth:grant-type:token-exchange` (and refuses unknown grant types); `internal/engine/actor_provider.go` passes the client's grants to Thunder |
| 2 | Settings | `64e5c9e` | `internal/config/native_sso.go`, `app.go`, `data/deployment.yaml`: `oauth.native_sso.*`, `native_sso.*` and the `MOSIP_ESIGNET_NATIVE_SSO_*` variables |
| 3 | Device Secret Provider | `f8a58e0` | `internal/engine/device_secret_provider.go` (signed inner JWT, RSA-OAEP-256 JWE, session record in the runtime store), key provisioning and wiring in `cmd/esignet/main.go` |
| 4 | Userinfo limited to the granted scope | `91cf6fe` | `internal/httpmiddleware/userinfo_scope.go`: an access token from a token exchange sees only the claims of its scope; userinfo of every grant is signed with eSignet's key and the alg its JWKS entry declares (PS256), after the identity system's RS256 token is verified |
| 5 | Documentation | `6aff73e`, `ba0e764` | `docs/configuration.md`, `docs/esignet-openapi.yaml`, the "Native SSO" folder of the Postman collection |

## Before opening the pull requests

- Rebase onto eSignet `main`; the work is on v2.0.0, which pins Thunder commit `12f517c4`. The team builds eSignet against
  the in-repo Thunder (`3a0e127`); upstream would keep its own pin and move it after the Thunder pull request.
- Check the contributor rules (sign-off, MPL-2.0 header on new files, `golangci-lint` v2, `make.sh`).
- Re-run: `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover` (1588 tests, 0 failed; changed lines
  318 of 339, 93.8 %, on 2026-10-05), `bash scripts/go-lint.sh esignet/esignet-service` (0 issues on 2026-10-05), and the
  OpenAPI lint (0 errors, same 8 warnings as the import).
- Do not run eSignet's `./make.sh update-thunder` on this repository: it points eSignet back at a downloaded Thunder.

## What a reviewer will ask

| Question | Answer |
|---|---|
| Does the change affect deployments that do not use Native SSO? | no: `device_sso` and the token-exchange grant are opt-in per client; `exchange_only` only takes effect when a client uses token exchange |
| Where is the session record kept? | in eSignet's runtime store, under the Thunder namespace `nativesso:session`; the runtime store is in memory in this release, so a restart ends the sessions (limit L03 in [../test-report.md](../test-report.md#known-limits)) |
| Why must the signing key differ from the engine's token key? | so a device secret can never be accepted as an id_token or the other way round; start-up fails otherwise (`TestNewDeviceSecretProvider_RejectsBadSettings`, case "signing key is token key") |
| Why a userinfo filter in eSignet? | Thunder passes the identity system's userinfo token through unchanged, so a narrower Child App scope would otherwise still see every claim of the first login |
| `device_sso` in `scope_claims`? | never: `getAllowedScopes` copies every `scope_claims` key into every client's allowed scopes, which would defeat the per-client check ([../design.md](../design.md) section 7) |

## Not part of the offer

- The Android apps, the local stack (`deploy/`) and the live test suite (`e2e/`); they can be offered as a separate
  example repository if MOSIP wants it.
