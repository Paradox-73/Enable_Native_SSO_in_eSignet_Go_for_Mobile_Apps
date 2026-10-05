# Offering the Thunder changes upstream

Notes for opening pull requests against [thunder-id/thunderid](https://github.com/thunder-id/thunderid). **Nothing has been
sent.** This file says what to send, in which pieces, and what a reviewer will ask. Written 2026-10-05.

## Why upstream wants it

- Thunder issue #3402 "App-native SSO" asks for the same capability; the team's work is an implementation of
  OpenID Connect Native SSO for Mobile Apps 1.0 (draft 07) on Thunder v1.0.1.
- With no `DeviceSecretProvider` passed to the engine, Thunder behaves exactly as before, so the change is opt-in
  for every embedder (`TestNoDeviceSSO_ResponseIsUnchanged`; the upstream tests pass: 2363 tests, 0 failed on 2026-10-05).

## What to send, in order

Each piece builds and passes its tests on its own. The import commit is `c4c5443`; the team's changes are
`git diff c4c5443 -- thunder/` ([../../thunder/UPSTREAM.md](../../thunder/UPSTREAM.md) lists them file by file).

| # | Piece | Commit in this repository | Contents |
|---|---|---|---|
| 1 | Provider contract | `4e7e6f8`, `68c5aed` | `DeviceSecretProvider`, request, result and claims types, errors `NSSO-1001`/`NSSO-1002`, `WithDeviceSecretProvider`, `oauth.native_sso` settings, mock, `device_secret` in the token response model |
| 2 | `device_sso` checks | `2a43b22` | scope kept with the OpenID scopes; PAR, authorize and the code grant reject it for a client that does not list it in `allowed_authorization_scopes` |
| 3 | Device secret at the code grant | `3dec587` | `sid` in the id_token, call to `GenerateDeviceSecret`, `ds_hash`, both-or-neither rule, `device_secret` in the response |
| 4 | Token exchange | `13d17ac` | the `urn:openid:params:token-type:device-secret` actor token, validation order, id_token checks, session check, DPoP key match |
| 5 | Scope rules and discovery | `6bc7644` | subset and narrowing, `device_sso` refused in the exchange, `interaction_required`, tokens for the second app, `native_sso_supported` and `device_sso` in discovery |

## Before opening the pull requests

- Rebase onto Thunder `main`: the work is on v1.0.1. Thunder `main` may already have a session claim named `sid`; the
  team used the same constant name (`ClaimSessionID`) to keep the rebase small ([../design.md](../design.md) section 8).
- Open an issue first, quoting #3402 and linking the design ([../design.md](../design.md)); ask the maintainers whether
  they want the five pieces or one.
- Check the contributor rules of the upstream repository (sign-off, commit style, mockery version v3.5.5, licence header
  `SPDX-License-Identifier: Apache-2.0`) and re-run `make mockery`.
- Re-run on the rebased branch: `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover`
  (changed lines 341 of 352 covered, 96.9 %, on 2026-10-05) and the lint with Thunder's own golangci-lint v1.64.8
  (0 issues on 2026-10-05).

## What a reviewer will ask

| Question | Answer |
|---|---|
| Does it change behaviour for existing clients? | no: every new path needs the `device_sso` scope or the device-secret actor token type, and a provider |
| Where is the device secret format? | not in Thunder: it is opaque to Thunder by the specification (3.2); eSignet's provider defines it |
| Why `invalid_request` for a bad device secret, but `invalid_grant` for an ended session? | RFC 8693 2.2.2 for the first, Native SSO 4.3 step 4 for the second ([../design.md](../design.md) section 13) |
| Why is the subject token type written with an underscore? | Thunder rejects the hyphen form in the specification example; `urn:ietf:params:oauth:token-type:id_token` is accepted |
| Tests? | unit suites `TestAuthorizationCodeNativeSSOSuite`, `TestTokenExchangeNativeSSOSuite`, discovery and engine tests; live tests are in this repository's `e2e/` and use the eSignet embedding |

## Not part of the offer

- eSignet's provider and settings (see [esignet.md](esignet.md)).
- The Android apps and the Postman folder.
