# Test data

Every identity, client, key and setting the tests and the demo use, and where each comes from. All of it is test data
for the local stack; none of it is a real person or a real credential. Written 2026-10-05.

## Test identity

The mock identity system in the stack is seeded by upstream's [init.sql](../esignet/docker-compose/init.sql) with one
usable test person (the same one every upstream eSignet example uses).

| Item | Value | Used by |
|---|---|---|
| UIN (individual id) | `1774231323` | e2e suite, Postman folder, UI tests, demo |
| OTP | `111111` (the mock identity system answers every OTP request with it) | same |
| PIN | `545411` (password login; not used by the Native SSO flows) | upstream Postman folder only |
| Name, email, phone | name `Siddharth K Mansour`, an email and a phone number are in the seed row | claims in the id_token and userinfo |
| Captcha token | any non-empty string (the stack has no captcha validator) | e2e suite, Postman folder |

## Clients

No client is stored in the repository. Each run registers its clients through `POST /client-mgmt/client` with keys it
makes itself, so the data below describes what is registered, not fixed ids.

| Client | Made by | `relyingPartyId` | Grants | `allowed_authorization_scopes` | Purpose |
|---|---|---|---|---|---|
| Hub | the app on first start; `android/scripts/register-client.sh superapp` | `ps03-family` (variable `PS03_RELYING_PARTY`) | `authorization_code`, token exchange | `device_sso` | first app; receives the device secret |
| Health Card | the app; `register-client.sh childapp` | `ps03-family` | same | `device_sso` | second app; token exchange and re-consent login |
| e2e Hub-like and Child-like | `e2e/nativesso_fixture_test.go` (`registerNative`: allowed `device_sso` and the token-exchange grant) | `ps03-vendor-<random>` | `authorization_code`, token exchange | `device_sso` | rules R01-R26 |
| e2e variants | same, with one property changed per test | another relying party (`ps03-other-vendor`), or the vendor's | the default, or without the token-exchange grant | the default, or without `device_sso` | R02 and R13 (client without `device_sso` or without the grant), R07 (no DPoP binding), R19 (another relying party) |
| Postman: Hub, Health Card, plain, other | folder "Native SSO", request 01 | `ps03-vendor`, `ps03-other-vendor` | as above | as above | newman run |

- Settings of every demo and test client: `private_key_jwt`, pushed authorization requests required, PKCE required,
  DPoP-bound access tokens. Redirect URIs of the apps are in each app's `client-registration.json` (debug builds write
  it to the app's private files; `register-client.sh` reads it with `adb run-as`).
- `client_id` is derived from the public key, so registering the same key twice answers "already registered".

## Keys

| Key | Where it is made | Stored where |
|---|---|---|
| App client key (ES256, for `private_key_jwt`) | Android Keystore on first start | inside the Keystore; only the public JWK is sent to eSignet |
| Hub's DPoP key (ES256) | Android Keystore; deleted by "Sign out of all apps" | inside the Keystore |
| Health Card's client key | Android Keystore | inside the Keystore |
| e2e and Postman keys (ES256) | in memory at the start of each run | not stored |
| eSignet signing and device-secret keys | eSignet creates its PKCS#12 keystore on first start | the Docker volume `keystore`, deleted by `deploy/stack.sh down` |
| Android test signing certificate | the first Gradle build, with `keytool` | `$PS03_RUN_DIR/keys/ps03-family-test.jks` and a random password file next to it, outside git |
| Stack passwords (Postgres, keystore) | first `deploy/stack.sh up` | `$PS03_RUN_DIR/stack.env`, outside git |

`bash scripts/repo-lint.sh` fails on any private key or keystore file in git, except the upstream test fixtures named in
`.gitguardian.yaml`.

## Settings the tests depend on

| Setting | Value | Why |
|---|---|---|
| Issuer | `http://localhost:38088` | `iss` of every token and base of every DPoP `htu` |
| id_token lifetime | 120 s | `TestE2E_Exchange_ExpiredIDToken` waits it out and expects the exchange to still work |
| Device secret lifetime | 28800 s (8 h) by default; 20 s in e2e phase 2 | `TestE2E_Exchange_ExpiredDeviceSecret`; set by `PS03_DEVICE_SECRET_VALIDITY_SECONDS` |
| Scope of the Hub login | `openid profile email device_sso` | the exchange may narrow it (`openid email`) |
| Scope that triggers re-consent | `phone` (claim `phone_number`) | `TestE2E_Exchange_ScopeSuperset`, `ChildAppReconsentTest` |
| `exchange_only` | `true` | any other token exchange is refused (R26) |
| `scope_exceeded_error` | `interaction_required` | R15 |

## Inputs of the Android tests

| Test | Input |
|---|---|
| JVM tests | fake eSignet on MockWebServer; test keys made in the test; no network |
| `SuperAppSignInTest` and the other UI tests | instrumentation arguments `uin`, `otp`, `expectedName` (defaults: `1774231323`, `111111`); from the shell `PS03_TEST_UIN`, `PS03_TEST_OTP`, `PS03_TEST_NAME` |
| Family tests | three APK sets built by `android/scripts/build-apks.sh`: family, intruder (another certificate, no permission) and takeover (another certificate, permission declared) |
