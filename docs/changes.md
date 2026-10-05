# Changes by statement task

For each task of problem statement 03: the commits that implement it, the files they touch, and the tests that check it.
Upstream code was imported unchanged first (`c4c5443` Thunder v1.0.1, `4eac1cc` eSignet v2.0.0), so
`git diff c4c5443 -- thunder/` and `git diff 4eac1cc -- esignet/` show exactly what the team changed.
Written 2026-10-05; commit ids are the ones in `git log` on that date. Rule ids (R01-R26, A01-A12):
[design.md section 20](design.md#20-rules-and-tests); results: [test-report.md](test-report.md).

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
| Discovery (T1 support) | `6bc7644` | `thunder/.../discovery/service.go` | R24: `TestE2E_Discovery_NativeSSO` |
| Sign-out on the server (beyond the statement) | `554ff29`, `edbb9c5`, `bfae0fe`, `8c5eea9` | `thunder/.../revocation/` (device secret branch of `POST /oauth2/revoke`), `providers/interface.go` (`EndDeviceSecretSession`), `esignet/.../engine/device_secret_provider.go`, `android/oidc-client/.../OidcClient.kt` (`revokeToken`), `android/superapp/.../auth/HubAuthenticator.kt` | R27, A11: `TestE2E_Revoke_DeviceSecret`, `TestDeviceSecretRevocationTestSuite`, `HubAuthenticatorTest`; `ChildAppSignOutTest` (emulator) |
| App names, icons and wording (UX) | `db54d66` | both apps' `MainActivity.kt`, `strings.xml`, `res/drawable`, `res/mipmap-anydpi` | `HubScreensTest`, `CardScreensTest` |

## Supporting work

| Work | Commits | Files |
|---|---|---|
| Local stack and headless FAPI 2.0 login | `bf5a1b2` | `deploy/`, `e2e/client/`, `scripts/with-stack.sh` |
| Live suite and CI workflow | `4c6c7bf` | `e2e/nativesso_test.go`, `e2e/nativesso_fixture_test.go`, `e2e/run.sh`, `.github/workflows/ci.yml` |
| API documents and Postman folder | `ba0e764`, `6aff73e` | `esignet/docs/esignet-openapi.yaml`, `thunder/api/oauth2.yaml`, `esignet/postman-collection/` |
| Health Card, sign-out, accessibility | `fe17bf6` | `android/childapp/`, `android/superapp/`, `android/scripts/` |
| Demo, evidence, documents | `55a70c9`, this pack | `scripts/demo/`, `scripts/evidence.sh`, `docs/` |
| Imports and build switch | `c4c5443`, `4eac1cc`, `3a0e127` | `thunder/`, `esignet/`, `esignet/esignet-service/go.mod` (builds against the in-repo Thunder) |

## What is not changed

- Thunder keeps every upstream endpoint and behaviour for requests without `device_sso`
  (`TestNoDeviceSSO_ResponseIsUnchanged`; the upstream tests still pass: 2379 Thunder and 1588 eSignet tests, 0 failed on 2026-10-05).
- No change to eSignet's other services or to the web frontends.
- Changes meant for the upstream projects are described in [upstream-prs/thunder.md](upstream-prs/thunder.md) and
  [upstream-prs/esignet.md](upstream-prs/esignet.md); none has been sent.
