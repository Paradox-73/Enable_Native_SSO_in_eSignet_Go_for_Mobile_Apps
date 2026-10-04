# Project status and remaining work

What is finished, what is half done, and what is left, task by task, so that anyone on the team can pick up a
task on their own computer. How to set up a machine, build, test and commit: [../CONTRIBUTING.md](../CONTRIBUTING.md).

## Snapshot

| Item | Value |
|---|---|
| Written | 2026-10-04, when work stopped at 20:45 IST |
| `main` | `1bd6fd0` (2026-10-04 17:14 IST), on [GitHub](https://github.com/Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps) |
| Unfinished work | branch `wip/2026-10-04-stop`, one commit `4be5ec8` on top of `main` (half of WP-07, see its card) |
| Deadline | 2026-10-26 23:59 IST (MOSIP Decode 2026, problem statement 03, team Badmosip) |
| Target dates | feature-complete (WP-06 to WP-16 DONE) Fri 2026-10-16; code freeze Thu 2026-10-22, only doc and test-report fixes after it |

Update this file in the same pull request that changes a task's state (see [Definition of done](#definition-of-done)).

## Words used in this file

- **Task** (WP-nn): one work package with a goal, the folders it owns and an acceptance check.
- **Lane**: a group of tasks that owns a set of folders. Four lanes: `platform`, `thunder`, `esignet`, `android`.
- **Statement task** (T1 to T10, B1 for the bonus): the numbered tasks of problem statement 03. Rule ids (R01 to
  R26 for the server, A01 to A12 for the apps) and the test that proves each rule are listed in
  [design.md section 20](design.md#20-rules-and-planned-tests).
- **Thunder**: the Thunder ID engine (`thunder/`), a Go library eSignet runs in its own process for every OAuth
  and OpenID Connect endpoint. It owns the protocol: scopes, token endpoint, token exchange, id_token claims.
- **eSignet**: MOSIP's login server in Go (`esignet/esignet-service/`). It owns policy and keys: client settings,
  the device secret implementation, configuration.
- **Hub** (Super App) and **Health Card** (Child App): the two Android apps in `android/`, signed with one
  certificate. The user signs in once in Hub; Health Card then signs in without a browser.
- **`device_sso`**: the scope (word in the login request's `scope` list) that asks for Native SSO.
- **Device secret**: the value Hub receives at login; with Hub's id_token it lets Health Card get its own tokens.
  Here it is a signed token inside an encrypted token (JWS inside JWE).
- **`ds_hash`**: a hash in the id_token that ties it to one device secret. **`sid`**: the login-session id in
  the id_token.
- **DPoP** (RFC 9449): the app signs a small proof for every request with a private key; tokens carry that key's
  thumbprint as `cnf.jkt`. **PAR** (RFC 9126): login parameters are sent to the server before the browser opens.
- **Token exchange** (RFC 8693): a token-endpoint call that trades existing tokens for new ones.
- **Provider**: Thunder's plug-in pattern: Thunder defines a Go interface, eSignet passes an implementation in with
  a `With...Provider` option.
- **Relying party**: the vendor organisation registered in eSignet (`relyingPartyId`); Thunder calls it the
  client's organisation unit (OU).

## All tasks

| Task | Lane | Title | Depends on | Status | Commits |
|---|---|---|---|---|---|
| WP-01 | platform | Repository scaffold and build tooling | - | DONE | `fab22df`, `c45ac32` |
| WP-02 | thunder | Thunder: unchanged import of v1.0.1 and the Native SSO provider contract | WP-01 | DONE | `c4c5443`, `4e7e6f8`, `68c5aed` |
| WP-03 | esignet | eSignet: unchanged import of v2.0.0 and build against the in-repo Thunder | WP-02 | DONE | `4eac1cc`, `3a0e127`, `1bd6fd0` |
| WP-04 | android | Android project, shared signing and the OpenID Connect client library | WP-01 | DONE | `691de72`, `a3727cd` |
| WP-05 | platform | Design document and threat model | WP-01 | DONE | `e052436` |
| WP-06 | platform | Local stack and headless FAPI 2.0 login | WP-03 | TODO | |
| WP-07 | thunder | Thunder: `device_sso` checks, `sid`, device secret issuance and `ds_hash` at the code grant (T1, T2, T5, T6) | WP-02 | PARTIAL | branch `wip/2026-10-04-stop` (`4be5ec8`) |
| WP-08 | esignet | eSignet: Device Secret Provider, Native SSO settings and client registration (T4) | WP-03 | TODO | |
| WP-09 | android | Hub: Sign in with eSignet on the emulator, tokens stored encrypted | WP-04, WP-06 | TODO | |
| WP-10 | thunder | Thunder: token exchange with a device-secret actor token, validation (T9) | WP-07 | TODO | |
| WP-11 | android | App-to-app: Hub family SSO service and Health Card connection (T7) | WP-09 | TODO | |
| WP-12 | thunder | Thunder: scope rules, Child App tokens, re-consent signal and discovery (T10, B1) | WP-10 | TODO | |
| WP-13 | platform | Integration tests: live Native SSO suite for every rule, and CI workflow | WP-06, WP-08, WP-12 | TODO | |
| WP-14 | esignet | eSignet API documentation and Postman collection for Native SSO | WP-08, WP-12 | TODO | |
| WP-15 | android | Health Card: silent sign-in by token exchange, re-consent, sign-out, accessibility (T7, T8, B1) | WP-08, WP-11, WP-12 | TODO | |
| WP-16 | platform | End-to-end demo script (one command, repeatable) and video storyboard | WP-13, WP-15 | TODO | |
| WP-17 | platform | Test report with dated evidence | WP-13, WP-14, WP-16 | TODO | |
| WP-18 | platform | Documentation set and submission pack | WP-05, WP-16, WP-17 | TODO | |

Results recorded for the DONE tasks (all on 2026-10-04, commands in [../CONTRIBUTING.md](../CONTRIBUTING.md)):

- WP-01: `repo-lint: OK`; Go 1.26.8 in the container; `verify-import: self-test OK`; both golangci-lint versions
  (v1.64.8, v2.14.0) installed. Measured run times are in [../scripts/README.md](../scripts/README.md).
- WP-02: `verify-import.sh thunder` 0 differ, 0 missing; Thunder `./internal/oauth/... ./pkg/...` 2214 tests,
  0 failed; changed lines 22/22 covered; mockery (both configs) leaves no change; lint 0 issues.
- WP-03: `verify-import.sh esignet` 274 files, 0 differ, 0 missing, 0 extra; `go build ./cmd/esignet` exit 0;
  eSignet `./...` 29 packages, 1437 tests, 0 failed.
- WP-04: `BUILD SUCCESSFUL`; 64 JVM tests, 0 failures; both debug APKs signed by one certificate
  (`verify-family-signing.sh`). Details: [../android/README.md](../android/README.md).
- WP-05: `docs-check: 0 broken links; tasks traced 11/11; threats with mitigation and test 11/11`.

## Critical path

- Longest chain: WP-07 -> WP-10 -> WP-12 -> WP-13 and WP-15 -> WP-16 -> WP-17 -> WP-18.
- WP-07, WP-10 and WP-12 are all in `thunder/`, so they run one after another; start there first.
- Free to start now, in parallel: WP-07 (finish), WP-06, WP-08. WP-09 can start as soon as WP-06 is DONE.

## Remaining tasks

Every acceptance command runs from the repository root in Git Bash (Windows) or a normal shell (Linux, macOS).
A script marked *(new)* does not exist yet: the task writes it. Android commands need `JAVA_HOME` (JDK 17 or newer)
and `ANDROID_HOME` set (see [../CONTRIBUTING.md](../CONTRIBUTING.md#android)); on Windows write `bash gradlew`
instead of `./gradlew`. Upstream file references with a line number (`file.go:123`) point at the unchanged import
commit of that folder.

### WP-06 Local stack and headless FAPI 2.0 login

- **Goal**
  - `deploy/compose.yaml`, compose project `ps03-native-sso`: Postgres on 38455, mock identity system 0.14.0 on
    38082, eSignet built from this repository on 38088 with issuer `http://localhost:38088`, oidc-ui 2.0.0 (login
    pages) on 38300. Reuse `esignet/docker-compose/init.sql` and `nginx.conf` (read them; do not edit them).
  - `deploy/esignet.Dockerfile` with the repository root as build context, so the in-repo Thunder is visible; Go
    module and build cache mounts. `deploy/stack.sh` with `up`, `down`, `status`, `logs`, `smoke`; database and
    keystore passwords generated at run time into `$PS03_RUN_DIR/stack.env`.
  - `scripts/with-stack.sh`: starts the stack, runs the given command, always runs `docker compose down -v` at the
    end; takes a `docker-stack` lock (same lock helpers as `scripts/lib.sh`).
  - `e2e/`: a Go module with `client/` (discovery, PKCE, DPoP ES256, `private_key_jwt`, PAR, authorize, the
    `/flow/execute` OTP steps and consent as in the upstream Postman "FAPI2.0 flow", callback, token, userinfo) and
    `fapi_login_test.go` (build tag `e2e`) that registers its own client with keys made at run time; `e2e/run.sh`.
  - `TestE2E_PSUTProbe`: logs the seeded user in through two clients of one relying party and prints
    `psut-scope: relying-party` (same `sub`) or `psut-scope: client`. Record the answer in this file (WP-06 notes):
    WP-12 and WP-15 depend on it.
- **Owns**: `deploy/`, `e2e/`, `scripts/with-stack.sh`.
- **Acceptance**
  - `bash scripts/with-stack.sh bash e2e/run.sh -run 'TestE2E_FAPI2Login|TestE2E_PSUTProbe'` prints
    `stack smoke: PASS`, `--- PASS: TestE2E_FAPI2Login`, one `psut-scope:` line and `e2e: 2 passed, 0 failed`.
  - Afterwards `docker ps -q --filter label=com.docker.compose.project=ps03-native-sso` prints nothing.
- **Notes**
  - Upstream `esignet/esignet-service/Dockerfile` cannot build this code: its context is `esignet-service/`, so it
    cannot see `../../thunder/backend` (the `replace` line in `go.mod`). Keep it unchanged; write the new one.
  - `MOSIP_ESIGNET_HOST` must be `http://localhost:38088`: it is the issuer and the base of every DPoP `htu`. The
    apps reach the same URL on the emulator through `adb reverse`, so `iss`, `htu` and `aud` are identical on the
    laptop and the phone. Do not use `10.0.2.2`.
  - The e2e client runs in a Go container on the compose network
    (`bash scripts/go.sh --network ps03-native-sso_default e2e test -tags e2e ./...`) and must dial `esignet:8080`
    whenever a URL says `localhost:38088`, so the issuer stays the same.
  - Captcha: with no validator URL eSignet skips the server-side captcha check but still rejects an empty
    `captcha_token` (`esignet-service/internal/engine/captcha_provider.go:59-70`): send a fixed non-empty value.
    Also check whether the hCaptcha box in the login page works without internet and write the result in
    `deploy/README.md`.
  - Seeded test user (upstream `esignet/docker-compose/README.md`): UIN `1774231323`, PIN `545411`, OTP `111111`.
  - Client redirect URIs need a host: `io.ps03.superapp:/callback` was rejected with `invalid_redirect_uri`
    (`internal/clientmgmt/validate.go:318-343`); use the form `org.badmosip.nativesso.superapp://oauth/callback`.
  - Client authentication runs before the grant-type check: a token call without a client assertion answers
    `invalid_client` and never reaches a grant handler.
  - Fallback if the image build is too slow or fails: run `go run ./cmd/esignet` in a golang container with the
    repository mounted (a compose profile `dev`). eSignet reads `data/deployment.yaml` relative to its working
    directory (`internal/config/app.go:236`), so start it from `esignet/esignet-service/`.
  - Memory measured 2026-10-04 with upstream 2.0.0 images: mock identity 535 MiB, Postgres 98 MiB, eSignet 43 MiB,
    oidc-ui 4.5 MiB. Health answered after 15 s, mock identity after 31 s.

### WP-07 Thunder: `device_sso` checks, `sid`, device secret issuance and `ds_hash` at the code grant (PARTIAL)

- **Goal**
  - `device_sso` reaches the authorization-code record as an OpenID scope and releases no claims (T1).
  - PAR and authorize reject `device_sso` with `invalid_scope` when the client's allowed scopes lack it or no
    provider is configured (T2).
  - The provider travels from the engine context through `oauth.Initialize` and `granthandlers.Initialize` into the
    authorization-code handler.
  - Code grant with `device_sso`: client still allowed (T2), DPoP proof present, mint `sid`, carry `auth_id` on the
    code record, call `GenerateDeviceSecret` (T5), extend the cached user attributes to the device-secret lifetime,
    id_token with `sid` and `ds_hash`, `device_secret` in the response (T6); if any step fails, no tokens at all.
  - Logins without `device_sso` behave exactly as upstream (existing tests stay green).
- **Depends on**: WP-02 (DONE).
- **Owns**: `thunder/`.
- **Acceptance**
  - `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover` ends with `0 failed`
    and the changed lines at 85 % coverage or more.
  - `bash scripts/go.sh thunder/backend test -count=1 -v -run 'DeviceSSO|DeviceSecret|NativeSSO' ./internal/oauth/...`
    shows at least 12 passing cases, including client-not-allowed, missing-DPoP and provider-failure cases.
  - `bash scripts/go-lint.sh thunder/backend` exits 0.
- **Already on `wip/2026-10-04-stop`** (all under `thunder/backend/internal/oauth/`; not on `main`):
  - `oauth2/nativesso/nativesso.go` (new package): `RequestsDeviceSSO`; `ValidateDeviceSSOScope` (returns
    `invalid_scope` when Native SSO is off, the client's `Scopes` lack `device_sso`, or `openid` is missing);
    `NewSessionID` (128 random bits, base64url); `DeviceSecretHash` (`ds_hash`: base64url of the left 16 bytes of
    SHA-256 over the device secret); `ReleasedClaimNames` (names, never values, of the claims released at login).
  - `oauth2/utils/oauthutils.go`: `SeparateOIDCAndNonOIDCScopes` keeps `device_sso` with the OpenID scopes.
  - `oauth2/authz/validator.go`, `oauth2/authz/service.go`: authorize applies `ValidateDeviceSSOScope`;
    `newAuthorizationValidator` now takes a `nativeSSOEnabled` flag.
  - `oauth2/par/service.go`: PAR applies `ValidateDeviceSSOScope` before storing the request.
  - `config/config.go`: new field `NativeSSOEnabled`. **Nothing sets it yet**, so Native SSO stays off at run time
    even when eSignet passes a provider.
  - Tests: `oauth2/authz/validator_native_sso_test.go` (4 cases), `oauth2/par/service_native_sso_test.go`
    (5 cases), `oauth2/utils/oauthutils_native_sso_test.go` (2 cases); `oauth2/authz/validator_test.go` adjusted
    to the new constructor.
  - Measured 2026-10-04 on these files:
    `bash scripts/go-test.sh thunder/backend ./internal/oauth/oauth2/authz/ ./internal/oauth/oauth2/par/ ./internal/oauth/oauth2/utils/ ./internal/oauth/oauth2/nativesso/ --changed-cover`
    printed 4 packages, 470 tests, 0 failed; `nativesso.go` 0/34 statements covered (the package has no tests of
    its own yet). `go-lint.sh` was not run on these files.
- **Still to do**
  0. Reword `thunder/UPSTREAM.md` line 114 ("regenerated with no change" matches the word list of
     `scripts/repo-lint.sh`, which prints `FAILED (1 problems)` on `main` today), for example to "regenerated; the
     committed mocks did not change".
  1. Set `NativeSSOEnabled` in `oauth.Initialize` from the provider it receives (true only when the provider is
     not nil), and pass the provider down to the authorization-code handler in `oauth2/granthandlers/`.
  2. Unit tests for `nativesso.go` itself (`NewSessionID` length and uniqueness, `DeviceSecretHash` against a
     fixed vector, `ReleasedClaimNames` with plain and opaque attributes, every `ValidateDeviceSSOScope` branch).
  3. Carry `auth_id` onto the code record: `AuthorizationCode` (`oauth2/authz/model.go:23-47`) has no such field;
     the value is `AuthZPostRequest.AuthID` (`authz/model.go:51`).
  4. In the authorization-code handler, when the granted scopes contain `device_sso`: check the client again
     (its settings can change between login and token request); require a DPoP proof (`invalid_request` for a
     client without `dpop_bound_access_tokens`; DPoP-bound clients are already stopped with `invalid_dpop_proof`
     by `oauth2/token/service.go:267-279`); `NewSessionID`; build `providers.DeviceSecretRequest` (`Validate()`
     lists the required fields); call `GenerateDeviceSecret`; extend the attribute cache with
     `ExtendAttributeCacheTTL` (`internal/attributecache/service.go:163`); add `sid` and `ds_hash` to the
     id_token; return `device_secret`. A provider error gives `server_error` and no tokens.
  5. Tests for the code grant: allowed client, client not allowed, missing DPoP proof, provider failure, no
     `device_sso` (unchanged response).
- **Notes**
  - Resume from the branch without its `wip:` commit message (see
    [../CONTRIBUTING.md](../CONTRIBUTING.md#pick-up-a-task)).
  - "Client allows `device_sso`" means `OAuthClient.Scopes` contains it. eSignet fills that list from the
    `scope_claims` keys plus the client's `allowed_authorization_scopes`
    (`esignet-service/internal/engine/actor_provider.go:99,276-284`), so the check reads exactly that setting.
  - `device_sso` is its own constant (`providers.ScopeDeviceSSO`, `constants.ScopeDeviceSSO`), never an entry in
    `StandardOIDCScopes`: standalone Thunder advertises those and gives them to every new client.
  - Claim names to reuse: `constants.ClaimSessionID` (`sid`), `constants.ClaimDeviceSecretHash` (`ds_hash`).
  - Fallback if extending the attribute cache is blocked: keep the released claim values encrypted in eSignet's
    session record instead (agree it with the `esignet` lane first).
  - Contract reference: [design.md section 8](design.md#8-device-secret-provider-contract); error codes:
    [design.md section 13](design.md#13-error-table).

### WP-08 eSignet: Device Secret Provider, Native SSO settings and client registration (T4)

- **Goal**
  - `esignet/esignet-service/internal/engine/device_secret_provider.go`, constructor
    `NewDeviceSecretProvider(cfg, signatureSvc, cryptoSvc, runtimeStore, clock)`:
    - Generate: claims `iss`, `client_id`, `auth_id`, `sid`, `scope`, `acr`, `claims` (names only), `cnf.jkt`,
      `iat`, `exp`, `jti` -> header `{"alg":"ES256","typ":"ds+jwt","kid":...}` signed with
      `signature.Service.SignRaw` (key `OIDC_SERVICE` / `EC_SECP256R1_SIGN`) -> encrypted with
      `cryptomanager.JWTEncrypt` (RSA-OAEP-256 + A256GCM, `cty` JWT) to key `OIDC_SERVICE` / `DEVICE_SECRET` ->
      session record `{sid -> auth_id, client, user, attribute-cache id, expiry}` with a TTL in the runtime store
      (namespace `providers.NamespaceNativeSSOSession`).
    - Validate: `JWTDecrypt` -> inner `typ` must be `ds+jwt` and `alg` ES256 -> `VerifyRaw` -> `iss`, `iat`, `exp`
      -> session record exists and matches `sid`, `auth_id`, `client_id` -> claims. Return
      `providers.ErrorInvalidDeviceSecret` for a bad value, `providers.ErrorDeviceSecretSessionEnded` when the
      session record is gone.
  - `provisionKeyHierarchy` (`cmd/esignet/main.go:346`) also calls
    `keymanager.Service.GetCertificate(ctx, OIDC_SERVICE, DEVICE_SECRET)`, so a key problem fails at start-up.
  - Pass the provider with `thunderidengine.WithDeviceSecretProvider` in `cmd/esignet/main.go`.
  - Settings ([design.md section 14](design.md#14-configuration)): `oauth.native_sso.*` (Thunder's type, no
    mapping code needed; `exchange_only: true` in eSignet) and eSignet's own `native_sso.*` with
    `MOSIP_ESIGNET_NATIVE_SSO_*` environment overrides in `internal/config/app.go`; `data/deployment.yaml` gains
    `device_sso` in `oauth.allowed_scopes` and the token-exchange grant in `oauth.allowed_grant_types`; document
    them in `esignet/docs/configuration.md`.
  - Client registration: client management accepts `urn:ietf:params:oauth:grant-type:token-exchange`
    (`internal/clientmgmt/validate.go:423-426`); the actor provider maps each client's registered grant types
    instead of the hard-coded `authorization_code` (`internal/engine/actor_provider.go:85`).
- **Depends on**: WP-03 (DONE). Uses the contract from WP-02 (DONE); it does not need WP-07.
- **Owns**: `esignet/`.
- **Acceptance**
  - `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover` ends with `0 failed` and reports at
    least 85 % for `device_secret_provider.go` and for the changed lines overall.
  - `bash scripts/go.sh esignet/esignet-service test -count=1 -v -run 'DeviceSecret|GrantType' ./internal/...`
    shows at least 14 passing cases: round trip, every claim present, `typ` other than `ds+jwt` rejected, an
    id_token presented as a device secret rejected, tampered ciphertext, wrong key, expired, unknown or deleted
    session, wrong issuer, valid and invalid grant types, grant-type mapping.
  - `bash scripts/go-lint.sh esignet/esignet-service` exits 0.
- **Notes**
  - Never add `device_sso` to eSignet's `scope_claims`: `getAllowedScopes` copies every `scope_claims` key into
    every client's allowed scopes, which would let every client pass the T2 and T9 iii.3 checks. A client gets
    `device_sso` only through `allowed_authorization_scopes`.
  - Use `SignRaw`/`VerifyRaw`, not `JWSSign`: `JWSSign` builds a header with only `alg`, `kid` and certificate
    fields, so it cannot carry `typ: ds+jwt` (that header stops an id_token being accepted as a device secret).
  - The encryption key is created lazily by `GetCertificate` (`internal/keymanager/service.go:731`), not by
    `GenerateMasterKey`. `JWTDecrypt` finds keys by the JWE `kid`, so rotated keys keep working until revoked.
  - id_tokens are signed with the RSA_2048 key (PS256); device secrets use the EC key, so the two never share a key.
  - No user identifiers or attribute values inside the device secret: they stay in the session record.
  - New Go files inside `esignet/` start with eSignet's MPL-2.0 notice block (copy it from an existing file).
  - Format reference: [design.md section 9](design.md#9-device-secret-format).

### WP-09 Hub: Sign in with eSignet on the emulator, tokens stored encrypted

- **Goal**
  - Android Keystore keys for DPoP and client authentication (EC P-256, not exportable, StrongBox when present)
    and a Keystore `JwsSigner` (DER to raw `r||s`).
  - New module `android/secure-store`: AES-GCM Keystore key, ciphertext in `noBackupFilesDir`,
    `allowBackup=false`, data-extraction rules.
  - Compose screens: welcome with "Sign in with eSignet" and the switch "Use this sign-in in my other apps" (decides
    whether `device_sso` is requested); home with profile and sign out. Custom Tab plus a `RedirectActivity`
    (custom scheme with a host, `state` check, survives process death); token request; id_token checks including
    `ds_hash`; cleartext HTTP allowed for `localhost` in debug builds only.
  - `android/scripts/with-emulator.sh` *(new)*, `android/scripts/register-client.sh` *(new: reads the app's public
    key with `run-as` and registers the client through eSignet client management)*,
    `android/scripts/run-ui-test.sh` *(new)*; UI Automator test `SuperAppSignInTest` drives Chrome (UIN
    `1774231323`, OTP `111111`, Allow).
- **Depends on**: WP-04 (DONE), WP-06.
- **Owns**: `android/`.
- **Acceptance**
  - `cd android && ./gradlew --no-daemon assembleDebug assembleDebugAndroidTest` prints `BUILD SUCCESSFUL`.
  - `bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh superapp SuperAppSignInTest`
    prints `OK (1 test)`; the test asserts the home screen shows the user's name and that an encrypted token
    bundle with an id_token exists.
- **Notes**
  - Until WP-07 is DONE the server does not know `device_sso`: run the UI test with the switch off. The code that
    stores a returned `device_secret` is covered here by JVM tests; on the device it is asserted in WP-15.
  - Do not use Jetpack `security-crypto` (its APIs are deprecated since 1.1.0-beta01); write the small store.
  - Emulator facts measured 2026-10-04 (AVD `ps03_api34`, Google APIs x86_64, API 34, Chrome 113): see the
    emulator part of [../CONTRIBUTING.md](../CONTRIBUTING.md#troubleshooting).
  - A plain HTTP 302 from the login page to the app's custom scheme reached `RedirectActivity` without a tap
    (2026-10-04, stand-in page). A redirect made by script after a button tap was not tested: retest with the real
    eSignet page; fallback is a "Return to app" page with an intent link.
  - If UI Automator cannot reach the eSignet page inside Chrome: drive it through Chrome's DevTools socket
    (`adb forward tcp:38990 localabstract:chrome_devtools_remote`) or `uiautomator dump` plus `adb shell input`.
  - Package names stay `org.badmosip.nativesso.superapp` and `org.badmosip.nativesso.childapp`; the issuer comes
    from the Gradle property `ps03.issuer` (default `http://localhost:38088`).

### WP-10 Thunder: token exchange with a device-secret actor token, validation (T9)

- **Goal**
  - Accept `actor_token_type=urn:openid:params:token-type:device-secret` with
    `subject_token_type=urn:ietf:params:oauth:token-type:id_token` (T9 i); reject the older `urn:x-oath:` URN and
    other mismatches with a clear `invalid_request` message.
  - Pass the provider into the token-exchange handler (same path as WP-07).
  - Branch inside `HandleGrant` the way the ID-JAG branch does (`oauth2/granthandlers/token_exchange.go:145-150`)
    into a new `token_exchange_native_sso.go`, before the generic path validates the actor token. Checks in order:
    `audience` equals the issuer (`invalid_target`); requesting client allows `device_sso` and the token-exchange
    grant (`unauthorized_client`, T9 iii.3); `ValidateDeviceSecret` (T9 ii); id_token signed by this server, `iss`,
    `aud` equals the device secret's `client_id`; same relying party and the Hub client still allows `device_sso`;
    `ds_hash` (T9 iii.1); `sid` (T9 iii.2); `dpop.VerifyProofBinding` against `cnf.jkt` (T9 iii.4); with
    `exchange_only` set, every other token exchange is rejected with `invalid_request`.
  - Expired id_token: accept a past `exp` while the device secret and its session are valid (signature and `iss`
    still checked), capped by `id_token_max_age_seconds` when that is set.
  - On success: a DPoP-bound access token with the device-secret scope (scope rules follow in WP-12).
  - The generic exchange and ID-JAG paths keep upstream behaviour.
- **Depends on**: WP-07.
- **Owns**: `thunder/`.
- **Acceptance**
  - `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover` ends with `0 failed`
    and the changed lines at 85 % or more.
  - `bash scripts/go.sh thunder/backend test -count=1 -v -run NativeSSO ./internal/oauth/oauth2/granthandlers/`
    shows at least 15 passing cases covering T9 i, T9 ii, T9 iii.1-4, audience, same vendor, expired id_token inside
    and outside the cap, and the older URN.
  - `bash scripts/go-lint.sh thunder/backend` exits 0.
- **Notes**
  - The expired-id_token rule is required, not optional: eSignet id_tokens live 120 s
    (`esignet-service/data/deployment.yaml:119`) and Thunder's subject-token validators reject expired tokens
    (`tokenservice/validator.go:529`). Without the new check, silent sign-in stops working 2 minutes after the Hub
    login. Reason: [design.md section 11.1](design.md#111-why-an-expired-id_token-is-accepted).
  - Error codes per case: [design.md section 13](design.md#13-error-table) (`invalid_request` for a bad device
    secret or id_token, `invalid_grant` for an ended session or a DPoP key mismatch, `invalid_target`,
    `unauthorized_client`).
  - `urn:openid:params:token-type:device-secret` is deliberately not in the generic list of supported token types;
    only this branch accepts it, and only as `actor_token_type`.
  - Validation order: [design.md section 11](design.md#11-validation-order-at-the-token-exchange).

### WP-11 App-to-app: Hub family SSO service and Health Card connection (T7)

- **Goal**
  - Hub `FamilySsoService` implementing `IFamilySso` (`android/sso-contract`): service guarded by
    `org.badmosip.nativesso.permission.FAMILY_SSO` (`protectionLevel="signature"`, declared in both apps); on every
    call, check the caller (`Binder.getCallingUid` -> `hasSigningCertificate` with Hub's certificate SHA-256, plus a
    package allow-list); proofs built inside Hub (fresh `jti`, `iat`, optional `ath` and `nonce`); only `htm`
    POST/GET and `htu` equal to the token or userinfo endpoint from discovery; a rate limit; an "Apps using this
    sign-in" list (package and time; no tokens in logs).
  - Health Card: `<queries>` entry (exists), binder client with a timeout, sign-in screen that shows whether a
    bundle is available.
  - Instrumented tests: family bind works; an intruder build (`-Pps03.intruder=true`, debug certificate) gets
    `SecurityException`; the permission-takeover sequence is refused by the per-call check; a foreign `htu` is
    refused. Tests seed Hub's store with a test bundle through a debug-only hook (no server needed); without a
    bundle, signing is refused with `ERROR_NOT_SIGNED_IN`.
  - `android/scripts/build-apks.sh` *(new)* builds the family and intruder APKs;
    `android/scripts/run-family-tests.sh` *(new)* only installs and runs.
- **Depends on**: WP-09.
- **Owns**: `android/`.
- **Acceptance**
  - `bash android/scripts/build-apks.sh` prints `BUILD SUCCESSFUL` twice (family, intruder).
  - `bash android/scripts/with-emulator.sh bash android/scripts/run-family-tests.sh` prints `family-bind PASS`,
    `intruder PASS (SecurityException)`, `takeover PASS (rejected)`, `htu-allow-list PASS`.
- **Notes**
  - Takeover sequence seen on the emulator on 2026-10-04: after the app that owned the permission definition was
    uninstalled, a differently signed APK that also declared the permission installed and was granted it. That is
    why the per-call certificate check is required, not optional.
  - While Hub is installed, a differently signed APK that declares the same permission fails to install with
    `INSTALL_FAILED_DUPLICATE_PERMISSION` (expected).
  - Contract and error codes: [../android/README.md](../android/README.md#the-binder-contract-sso-contract).

### WP-12 Thunder: scope rules, Child App tokens, re-consent signal and discovery (T10, B1)

- **Goal**
  - T10a: the requested scope must be a subset of the device secret's scope and include `openid`; no scope means
    the original scope minus `device_sso`; a wider scope answers `interaction_required` (or `invalid_scope` when
    `scope_exceeded_error` says so); a narrower scope is allowed.
  - T10b: `device_sso` in the exchange answers `invalid_scope`.
  - Child App tokens: access token bound to the proof key and carrying the session's attribute-cache id (`aci`);
    user attributes from that cache filtered by the Child App's claims and the device secret's claims; id_token
    with `aud` = Child App, same `sub`, `sid`, `acr`, `auth_time`, no `ds_hash`; `issued_token_type`
    `urn:ietf:params:oauth:token-type:access_token`, `token_type: DPoP`.
  - Discovery: `native_sso_supported: true`, `device_sso` in `scopes_supported` and the token-exchange grant in
    `grant_types_supported`, only when a provider is configured; update `thunder/api/oauth2.yaml`.
- **Depends on**: WP-10.
- **Owns**: `thunder/`.
- **Acceptance**
  - `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover` ends with `0 failed`
    and the changed lines at 85 % or more.
  - `bash scripts/go.sh thunder/backend test -count=1 -v -run 'NativeSSO|Discovery' ./internal/oauth/...` shows
    passing cases for superset (both settings), `device_sso` in the exchange, narrower scope, Child App id_token and
    discovery.
  - `bash scripts/openapi-lint.sh thunder/api/oauth2.yaml` prints `... : OK`; `bash scripts/go-lint.sh thunder/backend`
    exits 0.
- **Notes**
  - Only the device-secret branch fails hard on a wider scope; the generic exchange keeps upstream's silent
    filtering (`token_exchange.go:460-498`).
  - The default stays `interaction_required`: Native SSO 4.4 requires it when consent is needed, the exchange still
    fails as T10a asks, and the same answer starts the re-consent bonus.
  - Read the WP-06 PSUT probe result first: if `sub` differs per client, the Child App's `sub` rule must be
    revisited ([design.md section 16](design.md#16-decisions-d1-d11), D7).

### WP-13 Integration tests: live Native SSO suite for every rule, and CI workflow

- **Goal**
  - e2e tests for R01 to R26 against the running stack, each asserting its HTTP status and `error`
    ([design.md section 20](design.md#20-rules-and-planned-tests) has the test names).
  - The session-ended case runs in a second phase after an eSignet restart (the runtime store is in memory),
    driven by `e2e/run.sh`.
  - Clients the suite registers: Hub-like and Child-like (same relying party `ps03-vendor`, DPoP-bound), one of
    another relying party (`ps03-other-vendor`), and one with `device_sso` but without `dpop_bound_access_tokens`
    (the only way to reach R07's `invalid_request`).
  - `TestE2E_Exchange_ExpiredIDToken` waits past the Hub id_token's real 120 s lifetime (in parallel with the rest)
    and expects 200.
  - JSON results into `$PS03_RUN_DIR/evidence/`.
  - `.github/workflows/ci.yml`: jobs `repo-lint`; `go-lint` (both modules); `go-unit` (Thunder
    `./internal/oauth/... ./pkg/...`, eSignet `./...`); `android-unit` (oidc-client JVM tests, `assembleDebug`,
    Android lint); `e2e` (build the image, compose up, `go test -tags e2e`, compose down). Emulator tests stay local.
- **Depends on**: WP-06, WP-08, WP-12.
- **Owns**: `e2e/`, `.github/`.
- **Acceptance**: `bash scripts/with-stack.sh bash e2e/run.sh` prints `e2e: N passed, 0 failed` with N at least 25.
- **Notes**
  - CI has never run: there was no workflow file on 2026-10-04. Report CI as `NOT RUN` until the first hosted run.
  - Thunder rejects a missing DPoP proof from a DPoP-bound client with `invalid_dpop_proof` before any grant
    handler runs (`oauth2/token/service.go:267-279`); assert that, not `invalid_request`, for such clients.

### WP-14 eSignet API documentation and Postman collection for Native SSO

- **Goal**
  - `esignet/docs/esignet-openapi.yaml`: `device_sso` on PAR and authorize; `device_secret` in the token response;
    the token-exchange device-secret profile with parameters and error codes; discovery fields; client management
    with the token-exchange grant and an `allowed_authorization_scopes` example.
  - `esignet/postman-collection/Go-eSignet.postman_collection.json`: a "Native SSO" folder in the upstream script
    style (create Hub and Health Card clients, PAR with `device_sso`, authorize, flow steps, consent, callback,
    token with device secret, token exchange, Child App userinfo, at least 6 negative requests with tests);
    environment variables; a README section.
- **Depends on**: WP-08, WP-12.
- **Owns**: `esignet/`.
- **Acceptance**
  - `bash scripts/openapi-lint.sh esignet/docs/esignet-openapi.yaml` prints `... : OK`.
  - `python -c "import json;c=json.load(open('esignet/postman-collection/Go-eSignet.postman_collection.json',encoding='utf-8'));print(len([i for i in c['item'] if i['name']=='Native SSO'][0]['item']))"`
    prints 18 or more.
- **Notes**
  - The upstream collection runs only in the Postman desktop app: its scripts use WebCrypto (`crypto.subtle`) and
    top-level `await`, which newman 6.2.2 lacks (24 of 26 assertions failed on 2026-10-04). Say so in the README
    section and point to the Go e2e suite for automated runs.
  - Edit the upstream files in place in their own `docs:` or `feat(esignet):` commits; never in an `import:` commit.

### WP-15 Health Card: silent sign-in by token exchange, re-consent, sign-out, accessibility (T7, T8, B1)

- **Goal**
  - Hub with the switch on now receives a real device secret: `SuperAppSignInTest` also asserts that the encrypted
    bundle holds it and that `ds_hash` matched.
  - Health Card sign-in: bundle and proof from Hub -> token exchange through `oidc-client` (`audience` = issuer,
    subset scope, never `device_sso`, its own `private_key_jwt`) -> id_token checks (`aud`, `sid`) -> encrypted
    storage -> home with name and email (userinfo with a Hub-signed proof carrying `ath`).
  - "Sign in to Hub first" state with an "Open Hub" button.
  - `interaction_required` -> explainer screen -> Health Card's own PAR login with `prompt=consent` -> tokens.
  - Hub "Sign out of all apps" clears tokens and device secret and deletes Hub's DPoP key (a new one is made at the
    next sign-in), so every device secret and sibling token bound to the old key stops working; Hub's service then
    answers `ERROR_NOT_SIGNED_IN`. Health Card checks Hub's bundle on every resume and, when Hub is signed out,
    clears its own tokens and shows the Hub-first state.
  - Accessibility pass on every screen: TalkBack labels and order, 48 dp touch targets, contrast 4.5:1, 200 % font.
  - UI Automator tests `ChildAppSilentSignInTest` (asserts no browser opened) and `ChildAppReconsentTest`; each
    saves screenshots to `$PS03_RUN_DIR/evidence/<date>/screens/` (the platform lane copies chosen ones into
    `docs/`, so this lane never writes outside `android/`).
- **Depends on**: WP-08, WP-11, WP-12.
- **Owns**: `android/`.
- **Acceptance**: `bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all`
  prints `OK` with at least 3 tests: `SuperAppSignInTest` (with device secret), `ChildAppSilentSignInTest`,
  `ChildAppReconsentTest`.
- **Notes**
  - `subject_token_type` is `urn:ietf:params:oauth:token-type:id_token` with an underscore; the hyphen form in the
    Native SSO 4.2 example is rejected by Thunder. `oidc-client` already does this.
  - Re-consent asks for the OTP again: eSignet Go keeps no browser session. Say so in the explainer screen.
  - Pick the extra scope for the re-consent demo (for example `phone`) after checking eSignet's scope-to-claims
    mapping in `esignet/esignet-service/data/deployment.yaml`.

### WP-16 End-to-end demo script (one command, repeatable) and video storyboard

- **Goal**
  - `scripts/demo/demo.sh` *(new)*: build APKs -> stack from clean volumes -> emulator -> install both apps ->
    register both clients with their device keys -> open Hub. `--check` verifies and tears down.
    `scripts/demo/reset.sh` and `scripts/demo/teardown.sh` *(new)*.
  - `docs/demo-script.md`: live runbook (exact inputs, expected screens, fallbacks) and a 4 min 30 s storyboard;
    measured timings with the date.
- **Depends on**: WP-13, WP-15.
- **Owns**: `scripts/demo/`, `docs/demo-script.md`.
- **Acceptance**: `bash scripts/demo/demo.sh --check` prints `demo ready` (stack PASS, both apps installed with one
  signer, both clients registered, Hub in front) and then `teardown ok` within 10 minutes; afterwards no `ps03-`
  container runs and the emulator is stopped.
- **Notes**
  - Repeatable inputs: seeded UIN `1774231323`, OTP `111111`, fixed client ids, scripted registration, fresh volumes,
    emulator animations off, Chrome first-run screen skipped.
  - The Health Card step works whether it runs 10 s or 2 hours after the Hub login (expired id_token accepted while
    the device secret lives); rehearse it once more than 2 minutes after the Hub login.
  - Order of heavy steps: build APKs first, then start the stack, then the emulator.

### WP-17 Test report with dated evidence

- **Goal**
  - `scripts/evidence.sh` *(new)*: runs and collects Go unit tests (Thunder `./internal/oauth/... ./pkg/...`,
    eSignet `./...`) with coverage, the e2e suite, Android JVM and UI tests and the demo check; records date,
    commit, commands, image versions and durations in `$PS03_RUN_DIR/evidence/<date>/`.
  - Writes `docs/test-report.md`: summary, environment, how to reproduce, the rule matrix (rule, task, unit test,
    e2e test, Android test, result, date), coverage of changed Go lines and the lint result, manual runs (Postman,
    accessibility, phone, with name and date), known limits.
- **Depends on**: WP-13, WP-14, WP-16.
- **Owns**: `scripts/evidence.sh`, `docs/test-report.md`.
- **Acceptance**
  - `bash scripts/evidence.sh` prints `evidence: <n> results at <commit>` and rewrites `docs/test-report.md`.
  - `grep -c '| PASS |' docs/test-report.md` prints 35 or more.
  - `bash scripts/evidence.sh --check` exits 0: every row is PASS or a `NOT RUN` row whose reason is listed under
    "Known limits"; any FAIL row fails the check.
- **Notes**: known limits to list: emulator Keystore is software-backed, OTP again on re-consent, in-memory runtime
  store, server-side session record ends only by its TTL (no revoke call), CI `NOT RUN` until it has run.

### WP-18 Documentation set and submission pack

- **Goal**
  - Final `README.md`; `docs/setup.md` (prerequisites, ports, troubleshooting; the Windows 11 + Git Bash + Docker
    Desktop path that was actually run; Linux only as far as CI ran it; macOS marked untested); `docs/test-data.md`;
    `docs/api.md` (index of API docs, error table, Postman how-to); `docs/changes.md` (statement task -> commits ->
    files -> tests); `docs/ux.md` (flows, wireframes, screenshots from `$PS03_RUN_DIR/evidence/<date>/screens/`,
    accessibility notes); `docs/ppt-outline.md`; `docs/upstream-prs/thunder.md` and `docs/upstream-prs/esignet.md`;
    `docs/submission-checklist.md`; a root `make.sh` wrapper; `scripts/fresh-clone-check.sh`.
  - Re-sync names in `docs/design.md` with the final code; refresh app and test dependencies in `THIRD_PARTY.md`
    from `android/gradle/libs.versions.toml` and `e2e/go.mod`.
- **Depends on**: WP-05 (DONE), WP-16, WP-17.
- **Owns**: `README.md`, `THIRD_PARTY.md`, `make.sh`, `docs/`, `scripts/fresh-clone-check.sh`.
- **Acceptance**
  - `bash scripts/docs-check.sh docs/design.md docs/threat-model.md` prints `0 broken links; tasks traced 11/11`.
  - `bash scripts/fresh-clone-check.sh` prints `fresh-clone-check: PASS` (clean clone into a temporary folder,
    repo-lint, Go unit tests in the container, Android JVM tests).
- **Notes**: known drifts to fix, found on 2026-10-04:
  - `README.md` "What is here" table still marks `thunder/` as planned and says only the scaffold exists.
  - `THIRD_PARTY.md` still marks the Thunder import as planned.
  - `docs/design.md` names `model.go` for the provider types; they live in
    `thunder/backend/pkg/thunderidengine/providers/device_secret.go`.
  - `docs/README.md` does not list this file or `CONTRIBUTING.md` yet.
  - `scripts/README.md` (import workflow) and `android/README.md` (build commands) name helper scripts that exist
    only on the first team laptop; replace them with the commands in `CONTRIBUTING.md`.

## Decisions that may change

Open questions for the mentor call on Wed 2026-10-07, 16:00 IST. Each answer is behind one constant or setting:

| Question | Current choice | Where it is decided |
|---|---|---|
| Is `ds_hash` plus `sid` in the id_token the intended reading of "device secret embedded into the id_token" (T4a ix)? | `ds_hash` and `sid` in the id_token, device secret as its own response field | Thunder token builder (WP-07); D2 |
| Child App tokens bound to Hub's DPoP key (T8), or to the Child App's own key? | Hub's key | WP-10, WP-15; D3 |
| `interaction_required` or `invalid_scope` for a wider scope; OTP again on re-consent acceptable? | `interaction_required` | `oauth.native_sso.scope_exceeded_error`; D6 |
| "Same vendor" = same eSignet relying party, both clients allow `device_sso`? | yes | WP-10; D7 |
| Meaning of `auth_id` and `claims` in the device secret | authorization request id; names of released claims | WP-07, WP-08 |
| Client key held in Android Keystore instead of a backend? | Keystore key, not exportable | WP-09; design.md section 18 |
| Expired id_token accepted while the device secret lives; default device-secret lifetime? | accepted; 28800 s (8 h) | `id_token_max_age_seconds`, `device_secret_validity_seconds`; D8 |
| Server-side sign-out cascade needed? | no: local sign-out deletes Hub's DPoP key; session record ends by TTL | optional stretch, after WP-16 |

Record each answer and the date in the notes of the affected task card.

## How to split the remaining work between people

Rule: a folder has exactly one owner at a time. Each lane owns fixed folders, and only the person holding a lane
edits them. A lane moves to another person only after all its open pull requests are merged.

| Lane | Folders it owns | Remaining tasks, in order |
|---|---|---|
| `thunder` | `thunder/` | WP-07 (finish), WP-10, WP-12 |
| `esignet` | `esignet/` | WP-08, WP-14 |
| `android` | `android/` | WP-09, WP-11, WP-15 |
| `platform` | `README.md`, `THIRD_PARTY.md`, root dot-files, `make.sh`, `scripts/`, `deploy/`, `e2e/`, `docs/`, `.github/` | WP-06, WP-13, WP-16, WP-17, WP-18 |

- **Two people**
  - Person A, server: `thunder` and `esignet`. Order: WP-07, WP-08, WP-10, WP-12, WP-14.
  - Person B, stack and apps: `platform` and `android`. Order: WP-06, WP-09, WP-11, then WP-13 and WP-15 once
    WP-08 and WP-12 are merged, then WP-16, WP-17, WP-18.
- **Three people**
  - A: `thunder` (WP-07, WP-10, WP-12).
  - B: `platform` and `esignet`: WP-06 first (WP-09 waits for it), then WP-08, WP-13, WP-14, WP-16, WP-17, WP-18.
  - C: `android` (WP-09, WP-11, WP-15).
- **Four people**: one lane each.
- Exceptions to the folder rule, kept small on purpose:
  - Every task pull request edits its own row and card in this file (`docs/STATUS.md`). Touch nothing else here;
    on a merge conflict keep both sides.
  - A change to the Thunder provider contract (`thunder/backend/pkg/thunderidengine/providers/`) breaks the
    `esignet` lane's build: agree it with that lane's owner before opening the pull request.
  - A new dependency is recorded in `THIRD_PARTY.md` by the `platform` owner: ask in your pull request.

## Definition of done

A task is DONE only when all of these hold, in one pull request (or a series merged together):

- [ ] All tests of the touched modules pass (`bash scripts/go-test.sh ...` ends with `0 failed`; Android
      `BUILD SUCCESSFUL`), and `bash scripts/repo-lint.sh` prints `repo-lint: OK`.
- [ ] The task's acceptance check above was run on the final commit, and the pull request quotes the command,
      its result lines and the date.
- [ ] Docs touched by the change are updated (the folder's README or `UPSTREAM.md`, `docs/` pages the task owns),
      and say only what was run; anything not run is marked planned or `NOT RUN`.
- [ ] This file is updated in the same pull request: the task's status in [All tasks](#all-tasks), its commit ids,
      and a dated note in its card (results, decisions, anything the next task must know).
- [ ] A teammate reviewed and approved the pull request; every commit is signed off (`git commit -s`).
