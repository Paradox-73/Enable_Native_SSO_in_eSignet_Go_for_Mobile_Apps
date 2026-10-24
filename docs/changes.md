# Changes and status

For each task of problem statement 03: the commits that implement it, the files they touch, and the tests that check it;
then every work package (WP) with its commits, the decisions the mentor may change, and facts found while building.
All work packages are DONE; the items only a person can do are in [submission-checklist.md](submission-checklist.md).
Upstream code was imported unchanged first (`c4c5443` Thunder v1.0.1, `4eac1cc` eSignet v2.0.0), so
`git diff c4c5443 -- thunder/` and `git diff 4eac1cc -- esignet/` show exactly what the team changed.
Rule ids (R01-R28, A01-A13): [design.md section 20](design.md#20-rules-and-tests); results: [test-report.md](test-report.md).
The task cards written while the work was open (goal, acceptance check, notes) are kept in git history:
`git show d106024:docs/STATUS.md`.

## By task

| Task | Commits | Files (main ones) | Tests |
|---|---|---|---|
| P1 two apps, one certificate | `691de72`, `a3727cd` | `android/build.gradle.kts`, `android/scripts/verify-family-signing.sh` | A01: `verify-family-signing.sh` |
| P2 "Sign in with eSignet" | `72b2528`, `b1ec211` | `android/superapp/.../auth/HubAuthenticator.kt`, `android/oidc-client/` (PAR, PKCE, `private_key_jwt`, DPoP, id_token checks), `android/secure-store/` | `HubAuthenticatorTest`, `OidcClientTest`; `HubScreensTest`; `SuperAppSignInTest` (emulator) |
| T1 `device_sso` scope | `4e7e6f8`, `2a43b22` | `thunder/backend/pkg/thunderidengine/providers/constants.go`, `internal/oauth/oauth2/utils/oauthutils.go`, `esignet/esignet-service/data/deployment.yaml`, `esignet/esignet-service/internal/config/native_sso.go` | R01, R24: `TestE2E_Discovery_NativeSSO`, `TestE2E_SuperAppLogin_IssuesDeviceSecret` |
| T2 allowed-scope check | `2a43b22`, `3dec587` | `thunder/.../par/service.go`, `authz/service.go`, `authz/validator.go`, `granthandlers/authorization_code_native_sso.go` | R02: `TestE2E_DeviceSSO_ClientNotAllowed`, `TestDeviceSSO_ClientNotAllowedIsInvalidScope` |
| T3 provider interface | `4e7e6f8`, `68c5aed` | `thunder/.../providers/interface.go`, `device_secret.go`, `engine.go` (`WithDeviceSecretProvider`), `config/config.go`, `config/validate.go` | R03: `TestDeviceSSO_WithoutProviderIsInvalidScope`, `TestDiscovery_NativeSSONotAdvertisedWithoutProvider` |
| T4 generate and validate | `9314b2b`, `64e5c9e`, `f8a58e0`, `6aff73e` | `esignet/esignet-service/internal/engine/device_secret_provider.go`, `internal/engine/actor_provider.go`, `internal/clientmgmt/validate.go`, `cmd/esignet/main.go`, `docs/configuration.md` | R04, R09: `TestE2E_DeviceSecret_IsNestedJWE`, `TestDeviceSecretProviderTestSuite` |
| T5 call Generate at the token endpoint | `3dec587` | `thunder/.../granthandlers/authorization_code_native_sso.go`, `nativesso/nativesso.go` (`ds_hash`), `tokenservice/builder.go` | R05, R08: `TestE2E_IDToken_HasSIDAndDSHash`, `TestDeviceSSO_ProviderFailureIssuesNoTokens` |
| T6 return and keep the device secret | `3dec587`, `72b2528` | `thunder/.../model/token.go`, `android/secure-store/.../EncryptedFileStore.kt`, `android/superapp/.../auth/StoredRecords.kt` | R06, A03: `TestE2E_TokenResponse_Fields`, `EncryptedFileStoreTest`, `StoredRecordsTest` |
| T7 app-to-app | `b5d52d6` | `android/sso-contract/` (`IFamilySso.aidl`, `FamilySsoContract.kt`), `android/superapp/.../sso/FamilySsoService.kt`, `FamilySsoGate.kt`, `android/childapp/.../HubConnection.kt` | A04-A07: `FamilySsoGateTest`, `FamilySsoServiceTest`, `HubConnectionTest`; `FamilySsoBindTest` and `run-family-tests.sh` (emulator) |
| T8 exchange without `device_sso` | `fe17bf6` | `android/childapp/.../auth/CardAuthenticator.kt`, `android/oidc-client/.../OidcClient.kt` | A08: `CardAuthenticatorTest`, `OidcClientTest` |
| T9 token exchange and validation | `13d17ac` | `thunder/.../granthandlers/token_exchange_native_sso.go`, `token_exchange.go`, `tokenservice/validator_native_sso.go`, `constants/constants.go` | R09-R14, R18-R22, R26: the `TestE2E_Exchange_*` tests, `TestTokenExchangeNativeSSOSuite` |
| T10 scope rules | `6bc7644` | `token_exchange_native_sso.go` (scope subset, `device_sso` refused), `esignet/esignet-service/internal/httpmiddleware/userinfo_scope.go` (`91cf6fe`) | R15-R17: `TestE2E_Exchange_NarrowerScope`, `_ScopeSuperset`, `_DeviceSSOInScope` |
| B1 re-consent | `6bc7644`, `fe17bf6` | `token_exchange_native_sso.go` (`interaction_required`), `android/childapp/.../CardController.kt`, `CardServices.kt` | R15, A10: `TestE2E_Exchange_ScopeSuperset`, `CardControllerTest`; `ChildAppReconsentTest` (emulator) |
| B1 consent-only re-consent (no login) | `28f42ee`, `0a03abc`, `7464f37`, `ef69358` | `thunder/.../nativesso/consent_page.go`, `consent_store.go`, `consent_audit.go`, `token_exchange_native_sso.go` (`consent_uri`), `token/handler.go`, `esignet/.../config/native_sso.go`, `data/deployment.yaml`, `android/childapp/.../CardController.kt`, `auth/CardAuthenticator.kt`, `android/oidc-client/.../OidcException.kt` | R28, A13: `TestE2E_Exchange_ConsentOnlyReconsent`, `TestTokenExchangeNativeSSOSuite/TestNativeSSO_Consent_*`, `TestConsentPage_*`, `CardControllerTest`; `ChildAppConsentOnlyTest` (emulator) |
| Start-up code tests | `e450de8` | `thunder/backend/internal/oauth/init_test.go` | `TestInitialize_*`: `init.go` 24/24 changed statements covered |
| Discovery (T1 support) | `6bc7644` | `thunder/.../discovery/service.go` | R24: `TestE2E_Discovery_NativeSSO` |
| Sign-out on the server (beyond the statement) | `554ff29`, `edbb9c5`, `bfae0fe`, `8c5eea9` | `thunder/.../revocation/` (device secret branch of `POST /oauth2/revoke`), `providers/interface.go` (`EndDeviceSecretSession`), `esignet/.../engine/device_secret_provider.go`, `android/oidc-client/.../OidcClient.kt` (`revokeToken`), `android/superapp/.../auth/HubAuthenticator.kt` | R27, A11: `TestE2E_Revoke_DeviceSecret`, `TestDeviceSecretRevocationTestSuite`, `HubAuthenticatorTest`; `ChildAppSignOutTest` (emulator) |
| App names, icons and wording (UX) | `db54d66` | both apps' `MainActivity.kt`, `strings.xml`, `res/drawable`, `res/mipmap-anydpi` | `HubScreensTest`, `CardScreensTest` |

## Supporting work

| Work | Work package | Files |
|---|---|---|
| Local stack and headless FAPI 2.0 login | WP-06 | `deploy/`, `e2e/client/`, `scripts/with-stack.sh` |
| Live suite and CI workflow | WP-13 | `e2e/nativesso_test.go`, `e2e/nativesso_fixture_test.go`, `e2e/run.sh`, `.github/workflows/ci.yml` |
| API documents and Postman folder | WP-14, WP-08 | `esignet/docs/esignet-openapi.yaml`, `thunder/api/oauth2.yaml`, `esignet/postman-collection/` |
| Health Card, sign-out, accessibility | WP-15 | `android/childapp/`, `android/superapp/`, `android/scripts/` |
| Demo, evidence, documents | WP-16 to WP-18 | `scripts/demo/`, `scripts/evidence.sh`, `docs/` |
| Imports and build switch | WP-02, WP-03 | `thunder/`, `esignet/`, `esignet/esignet-service/go.mod` (builds against the in-repo Thunder) |

## Work packages

Lanes own folders: `platform` (root files, `scripts/`, `deploy/`, `e2e/`, `docs/`, `.github/`), `thunder`,
`esignet` and `android`. Statement tasks are T1 to T10, with B1 for the bonus.

| Package | Lane | Title | Status | Commits |
|---|---|---|---|---|
| WP-01 | platform | Repository scaffold and build tooling | DONE | `fab22df`, `c45ac32` |
| WP-02 | thunder | Thunder: unchanged import of v1.0.1 and the Native SSO provider contract | DONE | `c4c5443`, `4e7e6f8`, `68c5aed` |
| WP-03 | esignet | eSignet: unchanged import of v2.0.0 and build against the in-repo Thunder | DONE | `4eac1cc`, `3a0e127`, `1bd6fd0` |
| WP-04 | android | Android project, shared signing and the OpenID Connect client library | DONE | `691de72`, `a3727cd` |
| WP-05 | platform | Design document and threat model | DONE | `e052436` |
| WP-06 | platform | Local stack and headless FAPI 2.0 login | DONE | `bf5a1b2` |
| WP-07 | thunder | `device_sso` checks, `sid`, device secret issuance and `ds_hash` at the code grant (T1, T2, T5, T6) | DONE | `1a8a6e7`, `2a43b22`, `3dec587` |
| WP-08 | esignet | Device Secret Provider, Native SSO settings and client registration (T4) | DONE | `9314b2b`, `64e5c9e`, `f8a58e0`, `6aff73e` |
| WP-09 | android | Hub: sign in with eSignet on the emulator, tokens stored encrypted | DONE | `72b2528`, `b1ec211`, `9f6c100` |
| WP-10 | thunder | Token exchange with a device-secret actor token, validation (T9) | DONE | `13d17ac` |
| WP-11 | android | App-to-app: Hub family SSO service and Health Card connection (T7) | DONE | `b5d52d6`, `da537ac` |
| WP-12 | thunder | Scope rules, Child App tokens, re-consent signal and discovery (T10, B1) | DONE | `6bc7644` |
| WP-13 | platform | Live Native SSO suite for every rule, and the CI workflow | DONE | `4c6c7bf` |
| WP-14 | esignet | eSignet API documentation and Postman collection for Native SSO | DONE | `ba0e764` |
| WP-15 | android | Health Card: silent sign-in, re-consent, sign-out, accessibility (T7, T8, B1) | DONE | `fe17bf6`, `da537ac` |
| WP-16 | platform | One-command demo and video storyboard | DONE | `55a70c9` |
| WP-17 | platform | Test report with dated evidence | DONE | `scripts/evidence.sh` and its record |
| WP-18 | platform | Documentation set and submission pack | DONE | the `docs:` commits of 2026-10-05 |
| WP-19 | thunder, android | Consent-only re-consent (B1 in full) and tests of the OAuth start-up code | DONE | `28f42ee`, `e450de8`, `7464f37`, `0a03abc`, `ef69358` |
| WP-20 | all | Review fixes of 2026-10-13/14 and a full re-run | DONE | `1c453a9` to `1f4aa6b`, `989200d`, `fd48556`, `2ec2c0d` |
| WP-21 | all | Audit gaps of 2026-10-14: id_token age cap off by default, consent reuse record, Redis runtime store, committed logs | DONE | `07faa4e` to `d106024` |
| WP-22 | android, platform | Health Card shows the email after the silent sign-in; ports on 127.0.0.1; accessibility scan of every app screen in the UI tests | DONE | `28cf520`, `44b643b`, `94ae777`, `7bfa2ef` |

## Decisions the mentor may change

Each answer is behind one constant or setting. The decision ids (D2 to D8) are in
[design.md section 16](design.md#16-decisions-d1-d11).

| Question | Current choice | Where it is decided |
|---|---|---|
| Is `ds_hash` plus `sid` in the id_token the intended reading of "device secret embedded into the id_token" (T4a ix)? | `ds_hash` and `sid` in the id_token, device secret as its own response field | Thunder token builder (WP-07); D2 |
| Child App tokens bound to Hub's DPoP key (T8), or to the Child App's own key? | Hub's key, as the statement's diagram 2 shows; the optional parameter `dpop_jkt` (own key) exists on the server and the apps do not use it | WP-10, WP-15, WP-20; D3 |
| `interaction_required` or `invalid_scope` for a wider scope; OTP again on re-consent for new user data acceptable? | `interaction_required`, plus `consent_uri` when consent alone is enough | `oauth.native_sso.scope_exceeded_error`; D6 |
| "Same vendor" = same eSignet relying party, both clients allow `device_sso`? | yes | WP-10; D7 |
| Meaning of `auth_id` and `claims` in the device secret | authorization request id; names of released claims | WP-07, WP-08 |
| Client key held in Android Keystore instead of a backend? | Keystore key, not exportable | WP-09; design.md section 18 |
| Expired id_token accepted while the device secret lives; default device-secret lifetime? | accepted for the device secret's lifetime (28800 s); no id_token age cap by default (a 3600 s default cap broke silent sign-in an hour after Hub's login while Hub said "signed in") | `id_token_max_age_seconds` (default 0), `device_secret_validity_seconds`; D8 |
| Server-side sign-out cascade needed? | yes: Hub's sign-out deletes its DPoP key and revokes the device secret (RFC 7009), which ends the session record (rule R27); Health Card's access tokens carry the session id, so userinfo and introspection refuse them afterwards | Thunder `revocation/`, eSignet `EndDeviceSecretSession`, `HubAuthenticator.signOut` |

## Findings worth keeping

Facts found while building that no other document records.

- **WP-11, custom permission takeover.** Seen on the emulator on 2026-10-04: after the app that defined the family
  permission was uninstalled, a differently signed APK that also declared the permission installed and was granted it.
  So the permission alone proves nothing, and Hub checks the caller's signing certificate on every call
  (`FamilySsoGate`). While Hub is installed, such an APK fails to install with `INSTALL_FAILED_DUPLICATE_PERMISSION`;
  `run-family-tests.sh` accepts that refusal as the other passing outcome of its takeover step.
- **WP-06, Postgres volume.** `postgres:bookworm` (Postgres 18) exits when a volume is mounted at
  `/var/lib/postgresql/data`, so the stack's Postgres has no named data volume.
- **WP-09, userinfo signature.** The mock identity system signs userinfo with RS256 under a key whose JWKS entry says
  PS256, and FAPI 2.0 does not allow RS256. eSignet's userinfo middleware verifies that token and signs it again with
  eSignet's own key, with the `alg` and `kid` of that key's JWKS entry.
- **WP-08, signing key rotation.** eSignet's `VerifyRaw` checks a device secret against the current signing certificate
  only, so rotating the `EC_SECP256R1_SIGN` key ends every device secret signed with the old key (silent sign-in stops
  until each user signs in to Hub again). The encryption key keeps working after rotation, because it is found by `kid`.
- **WP-09, unknown userinfo key.** When userinfo arrives with a `kid` eSignet does not know, it fetches the identity
  system's keys once more, past the mock provider's 5-minute cache. One early e2e run failed every userinfo call before
  this.
- **WP-14, userinfo `iss`.** The userinfo JWT's `iss` is `http://localhost:8088/v1/esignet` (a setting of the mock
  identity system), not the issuer `http://localhost:38088`.
- **WP-15, Hub's rate limit.** Hub allows 30 calls per caller in 60 s. One Health Card launch costs 4 calls (version,
  bundle, two proofs) because the silent sign-in reuses the connection that read the share. The UI tests wait 61 s after
  the consent-only test so the next test is not refused.

## What is not changed

- Thunder keeps every upstream endpoint and behaviour for requests without `device_sso`
  (`TestNoDeviceSSO_ResponseIsUnchanged`; the upstream tests still pass, rows UNIT-THUNDER and UNIT-ESIGNET of [test-report.md](test-report.md)).
- No change to eSignet's other services or to the web frontends.
- Changes meant for the upstream projects are described in [upstream-prs/thunder.md](upstream-prs/thunder.md) and
  [upstream-prs/esignet.md](upstream-prs/esignet.md); none has been sent.
