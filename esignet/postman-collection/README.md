# Postman — eSignet embedder flows

Manual API checks for a running [esignet-service](../esignet-service/README.md) instance.

All OAuth clients are **confidential** and must use `clientAuthMethods: ["private_key_jwt"]` at registration. Public clients (`none`) are not supported.

Variable names follow the standard eSignet Postman convention (snake_case: `baseUrl`, `client_id`, `client_private_key`, `redirect_uri`, `code`, `access_token`, `dpop_*`, …).

## Files

| File | Purpose |
|------|---------|
| `Go-eSignet (local).postman_environment.json` | Environment variables (`baseUrl`, audiences, credentials, runtime tokens/keys) |
| `Go-eSignet.postman_collection.json` | The collection: client management + mock identity creation + a single FAPI2.0 (PAR + DPoP) flow covering OTP, password, and Sunbird KBI authentication + Sunbird registry CRUD + the **Native SSO** folder (see [Native SSO](#native-sso)) |
| `Go-eSignet Native SSO (local stack).postman_environment.json` | Environment for the Native SSO folder: issuer `http://localhost:38088`, the seeded user, empty run-time variables |
| `native-sso/` | Source of the Native SSO folder: `lib.js` (key, client assertion and DPoP helpers), `build.py` (writes the folder into the collection), `run.sh` (newman) |

## Quick start

1. Start the server with `MOSIP_ESIGNET_HOST` matching `baseUrl` in the environment (default `http://127.0.0.1:8080`). See [esignet-service/README.md](../esignet-service/README.md) for build and run steps.
2. In Postman, import **both** files (environment first, then collection).
3. Select the **Go-eSignet (local)** environment.
4. Run **Client Management → Create client** once, with `additionalConfig.require_pushed_authorization_requests` and `dpop_bound_access_tokens` set to `true` (see below), and `authContextRefs` covering the authentication method you want to exercise.
5. Run the **FAPI2.0 flow** folder top to bottom, running only the `Flow execute` step(s) matching the ACR the server actually offered (see [FAPI2.0 flow](#fapi20-flow) below) — and **Sunbird Registry → Create Policy (Identity)** first if you're testing the KBI branch.

No external script or manually-generated key is needed: **Create client**'s pre-request script generates a fresh RSA key pair and `client_id` entirely inside Postman (`crypto.subtle`) and stores them in `client_private_key` / `client_public_key` / `client_id`. Every later request in the flow folder signs with that same private key.

## Folders

### Client Management

`POST/GET/PUT /client-mgmt/client`. Run **Create client** first — its pre-request script generates the RSA key and `client_id` used by the flow folder. In the **Create client** body:

- `additionalConfig`: `require_pushed_authorization_requests: true` and `dpop_bound_access_tokens: true` — required for the FAPI2.0 flow folder.
- `authContextRefs` defaults to `["mosip:idp:acr:generated-code", "mosip:idp:acr:password", "mosip:idp:acr:biometrics"]` (OTP + password branches). To exercise the Sunbird KBI branch, add `"mosip:idp:acr:knowledge"`.

If scope enforcement is enabled server-side (see `esignet-service`'s `security_config`), set `client_mgmt_token` to a valid Bearer JWT first; otherwise leave it empty.

### User Mgmt

`POST /v1/mock-identity-system/identity` against a running [esignet-mock-services](https://github.com/mosip/esignet-mock-services) instance — only relevant when `MOSIP_ESIGNET_AUTHN_PROVIDER=mock` (the `.env.example` default). Run **User Mgmt → Mock → Create User** before the FAPI2.0 flow's OTP or password branch to seed an identity the mock provider can authenticate.

Its pre-request script fills in `individual_id` (random UIN) and `password` (`Mosip@123`) only if they're still empty, so it won't clobber values you've already set — the created identity always matches what the flow later submits. The mock provider issues a fixed test OTP itself, so `otp` doesn't need to be set here. Requires `mock_identity_system_url` reachable (defaults to `http://localhost:8082`, matching `MOSIP_ESIGNET_MOCK_DOMAIN_URL`).

### FAPI2.0 flow

Pushed Authorization Requests + DPoP-bound tokens wrap all three authentication methods (OTP, password, Sunbird KBI) in one folder:

Initiate PAR → Initiate Authorization → Flow meta → Flow execute — start → Flow execute — select acr → **one matching branch** → Flow execute — consent → Auth callback → Exchange Code for Token → Exchange Token for Userinfo

**select acr** advances past the ACR-selection screen automatically (it re-submits whatever action the server just offered). What comes next depends on the ACR the client actually selected — run only the matching branch and skip the others:

| ACR | Branch |
|-----|--------|
| `mosip:idp:acr:generated-code` (OTP) | Flow execute — enter uin → Flow execute — enter OTP |
| `mosip:idp:acr:password` | Flow execute — enter uin → Flow execute — enter password |
| `mosip:idp:acr:knowledge` (Sunbird KBI) | Flow execute — sunbird KBI (single step; needs an identity seeded by **Sunbird Registry → Create Policy (Identity)** first) |

The PAR request generates a fresh DPoP key pair (`dpop_*` variables) and reuses it for the token and userinfo DPoP proofs — DPoP applies to every branch, including Sunbird KBI. `Flow meta` is keyed by `client_id` (not a separate application id). `Flow execute — enter uin` submits a hardcoded placeholder captcha token (`test-captcha-token`) alongside `individual_id`; this only works if the server's captcha check accepts/skips it in your environment. Requires the server's Redis to be **6.2+** (PAR uses `GETDEL`).

**Server requirements:** client registered with `require_pushed_authorization_requests: true` and `dpop_bound_access_tokens: true` (see Client Management above). Set `individual_id` and `otp` (OTP branch) or `individual_id` and `password` (password branch) in the environment before running.

For the OTP/password branches, the server's `MOSIP_ESIGNET_AUTHN_PROVIDER` must match how the identity was seeded: `mock` (the `.env.example` default) if you seeded it with **User Mgmt → Mock → Create User**, or `mosip` with MOSIP variables configured (see `esignet-service/.env.example`) if you're testing against a real MOSIP identity system — the `mosip` provider does not use the mock identity service, so an identity seeded via User Mgmt won't authenticate against it.

### Sunbird Registry

CRUD against the Sunbird registry (Insurance schema) used by the FAPI2.0 flow's Sunbird KBI branch: `Create Policy (Identity)` → `Get Policy` → `Search Policy` → `Delete Policy`.

Run **Create Policy (Identity)** first — its pre-request script generates a random identity (name/DOB/policy number) and stores it as `sunbird_individual_id` / `sunbird_full_name` / `sunbird_dob`, dedicated variables so they never clobber the MOSIP `individual_id` used by the OTP/password branches. **Delete Policy** is opt-in: it's skipped unless `sunbird_cleanup` is set to `true`, so a top-to-bottom "Run collection" doesn't delete the seeded record before the Sunbird KBI branch consumes it — run it manually, after KBI. Requires `sunbird_url` reachable.

## Audiences (editable from the environment)

The `aud` of each `private_key_jwt` client assertion **and** the `htu` of each DPoP proof are read from environment variables — change them here to test, instead of editing pre-request scripts. Each falls back to a `{{baseUrl}}`-derived default if left empty.

| Variable | Default | Drives |
|----------|---------|--------|
| `audience` | `{{baseUrl}}/oauth2/token` | token `client_assertion` `aud` **and** the token-request DPoP `htu` |
| `par_audience` | `{{baseUrl}}/oauth2/par` | PAR `par_client_assertion` `aud` **and** the PAR DPoP `htu` |
| `userinfo_audience` | `{{baseUrl}}/oauth2/userinfo` | the userinfo DPoP `htu` |

> A DPoP `htu` must equal the URL the request is actually sent to. If you change `baseUrl`, update these audiences to match (or clear them to use the `{{baseUrl}}`-derived fallback).

## The two client assertions (collection variables)

The `private_key_jwt` signed for the token request and the one signed for PAR are **separate** values, so they can never be confused:

| Collection variable | Signed by | Sent as `client_assertion` at |
|---------------------|-----------|-------------------------------|
| `client_assertion` | **Exchange Code for Token**'s own pre-request script (PS256) | `/oauth2/token` |
| `par_client_assertion` | **Initiate PAR**'s own pre-request script (PS256) | `/oauth2/par` |

Both are stored as **collection variables** (not environment) and regenerated on every run — they are transient signing artifacts. Do not confuse either with `assertion`, which is the **flow assertion** returned by `/flow/execute` on `COMPLETE` and submitted to `/oauth2/auth/callback`.

## Environment variables

| Variable | Used by | Notes |
|----------|---------|-------|
| `baseUrl` | All | Base URL; must match `MOSIP_ESIGNET_HOST` |
| `audience`, `par_audience`, `userinfo_audience` | FAPI2.0 flow | Client-assertion `aud` + DPoP `htu` — see [Audiences](#audiences-editable-from-the-environment) |
| `scope`, `redirect_uri` | FAPI2.0 flow | OAuth parameters |
| `relying_party_id` | Client Management | Relying party id for client-mgmt create |
| `client_mgmt_token` | Client Management | Bearer JWT (only needed if scope enforcement is enabled; leave empty otherwise) |
| `client_id`, `client_private_key`, `client_public_key` | All | Generated automatically by **Create client**; every request signs with these |
| `individual_id` | FAPI2.0 flow (`Flow execute — enter uin`), User Mgmt | MOSIP UIN; OTP and password branches only. Defaulted (random) by **User Mgmt → Mock → Create User** if empty |
| `otp` | FAPI2.0 flow (`Flow execute — enter OTP`) | OTP value; OTP branch only |
| `password` | FAPI2.0 flow (`Flow execute — enter password`), User Mgmt | No default in the environment file — set it manually, or let **User Mgmt → Mock → Create User** default it to `Mosip@123` |
| `mock_identity_system_url` | User Mgmt | esignet-mock-services base URL; only relevant when `MOSIP_ESIGNET_AUTHN_PROVIDER=mock` |
| `sunbird_url` | Sunbird Registry | Sunbird registry base URL |
| `sunbird_individual_id`, `sunbird_full_name`, `sunbird_dob` | Sunbird Registry, FAPI2.0 flow (Sunbird KBI branch) | Generated identity; set by **Create Policy (Identity)**, consumed by the KBI branch |
| `sunbird_insurance_id` | Sunbird Registry | `osid` of the created registry record; set by **Create Policy (Identity)** |
| `sunbird_cleanup` | Sunbird Registry | Set to `true` to enable **Delete Policy**; left unset so it is skipped by default |
| `code_verifier`, `code_challenge` | FAPI2.0 flow | PKCE, generated per-run |
| `request_uri` | FAPI2.0 flow | Set from the PAR response |
| `dpop_jkt`, `dpop_private_key`, `dpop_public_key` | FAPI2.0 flow | DPoP key, generated by Initiate PAR |
| `execution_id`, `auth_id`, `challenge_token`, `action_ref`, `assertion` | Flow execute steps | Set automatically as the flow progresses |
| `code`, `access_token`, `id_token` | Auth callback / Exchange Code for Token | Set automatically as the flow progresses; `id_token` is not pre-declared in the environment file |

`client_assertion` and `par_client_assertion` are **collection variables**, not environment variables — see [the section above](#the-two-client-assertions-collection-variables).

## `private_key_jwt` at PAR and the token endpoint

Both **Initiate PAR** and **Exchange Code for Token** sign their own `private_key_jwt` in their pre-request script: PS256, `iss`/`sub` = `client_id`, `aud` = `par_audience`/`audience` respectively (falls back to the matching `{{baseUrl}}`-derived endpoint), plus a DPoP proof reusing the key generated by Initiate PAR.

- `client_assertion_type`: `urn:ietf:params:oauth:client-assertion-type:jwt-bearer`
- All signing uses the Postman runtime (`crypto.subtle`) — no external script is needed during a run.

## Troubleshooting

| Symptom | Likely cause |
|---------|----------------|
| **Create client** fails with a `crypto.subtle` error | Update Postman (needs `crypto.subtle.generateKey` support), or set `client_private_key` + `client_public_key` + `client_id` manually |
| Token returns `invalid_client` | Run **Create client** again, or check that `client_id`/`client_private_key` weren't overwritten by a later Create client run |
| Token/PAR returns `invalid_client` after editing an audience | The `audience` / `par_audience` must equal the server's expected token/PAR endpoint |
| DPoP proof rejected (`invalid_dpop_proof`) | A DPoP `htu` (from `audience` / `par_audience` / `userinfo_audience`) does not match the URL the request was sent to — clear it to use the `{{baseUrl}}`-derived fallback |
| Client Management returns 401 | `security_config.issuer_url`/`jwks_url` are set in `esignet-service/data/deployment.yaml` (enforcement on) — supply a valid `client_mgmt_token`, or clear those settings to disable enforcement |
| Discovery / health fails | Server not running, or `baseUrl` does not match `MOSIP_ESIGNET_HOST` |
| `Flow execute — enter uin`/`enter OTP` fails | `MOSIP_ESIGNET_AUTHN_PROVIDER` doesn't match how the identity was seeded (`mock` for **User Mgmt**-seeded identities, `mosip` for real MOSIP identities), or `individual_id`/`otp` not set |
| OTP/password branch fails with an unknown-identity error under `MOSIP_ESIGNET_AUTHN_PROVIDER=mock` | Run **User Mgmt → Mock → Create User** first to seed the identity, and check `mock_identity_system_url` is reachable |
| `Flow execute — enter password` blocks with a "Set otp…" error | Its pre-request guard checks `otp`, not `password` — set any non-empty `otp` value to satisfy it even though the password branch doesn't use it |
| FAPI2.0 flow fails at PAR or token | Server's Redis is older than 6.2, or the client wasn't created with `require_pushed_authorization_requests`/`dpop_bound_access_tokens` |
| `Flow execute — sunbird KBI` fails with a "Set sunbird_*" error | Run **Sunbird Registry → Create Policy (Identity)** first to seed `sunbird_individual_id`/`sunbird_full_name`/`sunbird_dob`, and make sure `authContextRefs` on the client includes `"mosip:idp:acr:knowledge"` |
| A `Flow execute` step returns an unexpected-input error | You ran a branch that doesn't match the ACR the server actually offered after **select acr** — check the response's `data.inputs` and run the matching branch only (see [FAPI2.0 flow](#fapi20-flow)) |
| `Flow execute — enter uin` captcha rejected | The request sends a hardcoded placeholder (`test-captcha-token`); this only passes if the server's captcha validation is disabled/mocked for your environment |

## Native SSO

The **Native SSO** folder runs OpenID Connect Native SSO for Mobile Apps 1.0 end to end against the local stack
(`deploy/` at the repository root): a first app ("Hub") signs in with `device_sso` and receives a `device_secret`; a
second app ("Health Card") of the same relying party signs in without a browser by token exchange. It has 43
requests: setup (discovery check, four client registrations), the Hub sign-in, the Health Card exchange and userinfo,
the same with the optional `dpop_jkt` profile extension (`Card 03`, `Card 04`; the demo apps do not send it),
consent-only re-consent on eSignet's consent page (`Card 05` to `Card 09`, bonus B1), 13 negative requests with tests, five revocation requests (only the Hub may revoke its device secret; the
exchange fails afterwards, and userinfo refuses both Health Card tokens with 401), and a cleanup request.

### Run it in newman

```bash
bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh
```

`with-stack.sh` starts the stack, runs the command and stops the stack. `run.sh` calls
`newman run Go-eSignet.postman_collection.json -e "Go-eSignet Native SSO (local stack).postman_environment.json" --folder "Native SSO"`.
It needs newman 6. Expected result: 43 requests, 152 assertions, 0 failed (2026-10-14).

### Run it in the Postman app

1. Start the stack: `bash deploy/stack.sh up` (or `PS03_KEEP_STACK=1 bash scripts/with-stack.sh true`). Stop it with `bash deploy/stack.sh down`.
2. Import the collection and the environment `Go-eSignet Native SSO (local stack)`, select it.
3. Run the **Native SSO** folder top to bottom (Runner, or each request in order).

### How the steps work

| Part | What the request scripts do |
|---|---|
| Keys | `01 Generate keys and register the Hub` makes four ES256 (P-256) client key pairs and the Hub's DPoP key in its pre-request script. The keys live in environment variables of the run (initial values stay empty); `99 Cleanup` unsets them. Nothing secret is stored in the collection or the environment file. |
| Client assertion | Every token, PAR request is authenticated with a `private_key_jwt` (ES256) whose `aud` is the **issuer**, `http://localhost:38088`. The upstream folders use the endpoint URL; this server answers `invalid_client` to that. |
| DPoP | Each request sets its `DPoP` header: `htm` and `htu` of that request, `ath` for userinfo. The Hub's DPoP key signs the Hub requests **and the Health Card's token exchange**, because the device secret is bound to the Hub's key (in the Android apps the Hub signs that proof for the Health Card). |
| Login | `Hub 04` to `Hub 07` post the screens of the login page to `/flow/execute` (identifier and captcha token, OTP, consent). No browser is opened: the collection takes `authId` and `executionId` from the `Location` header of `Hub 02 Authorize` (the request does not follow the redirect), and the code from the `/oauth2/auth/callback` answer. The headless login helper `e2e/client/fapi.go` (`runFlow`) posts the same JSON in the same order, so the two can be compared. Seeded user: UIN `1774231323`, OTP `111111`. |
| Consent-only re-consent | `Card 05` asks for the permission `health_card_share`, which needs no new user data: the answer is `interaction_required` with a one-time `consent_uri`. `Card 06` opens that page and `Card 07` posts "Allow" with `Origin` set to the issuer, as the page's form does; `Card 08` shows that a ticket answers once; `Card 09` repeats the exchange and gets the permission, bound to the Hub's key. This works from Postman because Postman keeps the `HttpOnly` cookie that `/oauth2/authorize` set at `Hub 02`, so it plays the user's own browser. The Health Card app never has that cookie: from its own HTTP client the page answers 403 (e2e `TestE2E_Exchange_ConsentOnlyReconsent`, subtest "the app cannot answer for the user"). |
| Re-consent | `Neg 01` shows the `interaction_required` answer (scope `openid email phone`, the Hub was granted `openid profile email device_sso`). The follow-up (the Health Card's own PAR with `prompt=consent`, a new login, its own token request) uses the same requests as the Hub sign-in with the Health Card's client and key; the Go e2e suite and the Android test `ChildAppReconsentTest` run it. |

### Why it runs in newman

newman has no `crypto.subtle` and no top-level `await`, so the upstream scripts fail there (24 of 26 assertions failed
on 2026-10-04). The Native SSO scripts use only `CryptoJS` and `BigInt`: `native-sso/lib.js` implements ES256
(ECDSA over P-256 with an RFC 6979 nonce), JWK thumbprints, PKCE, client assertions and DPoP proofs, and is stored in
the collection variable `ns_lib`, which each script loads with `new Function(...)`. The other folders still need the
Postman desktop app. To change the folder, edit `native-sso/lib.js` or `native-sso/build.py` and run
`python esignet/postman-collection/native-sso/build.py`; do not edit the folder in the app and export it over the file.

### Environment variables of the Native SSO folder

| Variable | Set by | Meaning |
|---|---|---|
| `ns_issuer` | you | Issuer and base URL, `http://localhost:38088` |
| `ns_individual_id`, `ns_otp`, `ns_captcha_token` | you | Seeded user, its OTP, any non-empty captcha token (the stack has no captcha validator) |
| `client_mgmt_token` | you | Bearer JWT for `/client-mgmt/*`; empty on the local stack |
| `ns_hub_*`, `ns_card_*`, `ns_plain_*`, `ns_other_*` (`_id`, `_key`, `_pub`) | script | Client ids and key pairs of the Hub, the Health Card, a client without `device_sso`, and an app of another relying party |
| `ns_hub_dpop_key` | script | The Hub's DPoP key |
| `ns_card_dpop_key`, `ns_card_own_access_token` | script | The Health Card's own DPoP key and the token bound to it (`dpop_jkt` extension, `Card 03`) |
| `ns_consent_uri`, `ns_consent_ticket` | script | The `consent_uri` of `Card 05` and its ticket, answered at `Card 07` |
| `ns_request_uri`, `ns_auth_id`, `ns_execution_id`, `ns_challenge_token`, `ns_action_ref`, `ns_assertion`, `ns_code`, `ns_code_verifier`, `ns_state`, `ns_nonce` | script | State of the authorization and login steps |
| `ns_device_secret`, `ns_hub_id_token`, `ns_hub_access_token`, `ns_card_id_token`, `ns_card_access_token`, `ns_hub_sub`, `ns_hub_sid` | script | Results of the token requests |

### Troubleshooting

| Symptom | Cause |
|---|---|
| `invalid_client` at PAR or token | The client assertion `aud` is not the issuer, or `ns_issuer` differs from the server's issuer |
| `ReferenceError: BigInt` or a script error naming `new Function` | Postman or newman too old; use newman 6 or Postman 10 and newer |
| `Variable ns_hub_key is empty` | Run `01 Generate keys and register the Hub` first (it makes every key) |
| `Neg 08` returns something other than `invalid_request` | `oauth.native_sso.exchange_only` is `false` on the server |
| Card userinfo has a `name` claim | The server image predates the commit that limits userinfo to the granted scope; rebuild the stack |
