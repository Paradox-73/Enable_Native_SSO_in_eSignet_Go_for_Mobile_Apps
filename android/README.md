# Android apps and the OpenID Connect client

Two Kotlin apps from one publisher, signed with one certificate, and the client library they share:

- **Hub** (Super App, `org.badmosip.nativesso.superapp`): the user signs in here once with eSignet
  (OTP and consent in the browser). It keeps the tokens and the Native SSO device secret.
- **Health Card** (Child App, `org.badmosip.nativesso.childapp`): signs the same user in without a
  password or OTP, by sending Hub's id_token and device secret to eSignet in a token exchange.

> **State on 2026-10-04.** Built and tested: the Gradle project, shared signing, the binder contract
> and the `oidc-client` library (64 JVM tests, all passing). The two apps are shells: a home screen
> that shows the server URL and the signing certificate (Health Card also shows whether Hub is
> installed with the same certificate). The browser login, the Android Keystore keys, encrypted
> storage and the single sign-on service are the next work packages; nothing below claims them.

## Modules

| Module | Kind | What it holds |
|---|---|---|
| `oidc-client` | Kotlin/JVM library, no Android code | discovery, PKCE, DPoP proofs, `private_key_jwt`, PAR, authorization-code and Native SSO token-exchange requests, userinfo, `use_dpop_nonce` retry, RFC 7638 thumbprints, id_token checks including `ds_hash` and `sid`, typed errors |
| `sso-contract` | Android library | `IFamilySso.aidl` and the `SsoBundle` parcelable, the permission, action and package names, error codes, a signing-certificate helper |
| `superapp` | Android app | Hub shell, declares the family permission |
| `childapp` | Android app | Health Card shell, declares the same permission and a `<queries>` entry for Hub |

Toolchain: AGP 8.6.0, Gradle 8.7 (wrapper, checksum pinned), Kotlin 2.0.20, Jetpack Compose (BOM
2024.09.02) with Material 3, Java 17 bytecode, compileSdk and targetSdk 34, minSdk 28 (Android 9:
`PackageManager.hasSigningCertificate` and StrongBox exist from this level). Gradle runs on JDK 17 or
newer; the team uses JDK 21. Versions live in [`gradle/libs.versions.toml`](gradle/libs.versions.toml).

## Build and test

```bash
# Git Bash on the team laptops; elsewhere set JAVA_HOME (17+) and ANDROID_HOME yourself.
source /e/Projects/MOSIP/toolchain/env.sh && use_java21
cd android
bash gradlew --no-daemon :oidc-client:test assembleDebug   # BUILD SUCCESSFUL
bash scripts/verify-family-signing.sh                      # family-signing: OK <sha-256>
```

- `gradlew` and `scripts/verify-family-signing.sh` are stored executable, so on Linux and macOS
  `./gradlew` and `./scripts/verify-family-signing.sh` work without the `bash` prefix.
- Test reports: `oidc-client/build/reports/tests/test/index.html`; JUnit XML in
  `oidc-client/build/test-results/test/`.
- APKs: `superapp/build/outputs/apk/debug/superapp-debug.apk` and
  `childapp/build/outputs/apk/debug/childapp-debug.apk`.
- The issuer URL compiled into the apps is the Gradle property `ps03.issuer` (default
  `http://localhost:38088`, see `gradle.properties`); override with `-Pps03.issuer=...`.

Measured on 2026-10-04 (Windows 11 laptop shared with other builds, `--no-daemon`):

| Run | Result | Wall time |
| --- | --- | --- |
| Acceptance command, first run after source changes (98 of 109 tasks executed, test results up to date) | `BUILD SUCCESSFUL` | 6 min 57 s |
| Acceptance command again, `:oidc-client:cleanTest` first (test results come from the Gradle build cache) | `BUILD SUCCESSFUL` | 1 min 7 s |
| `--no-build-cache :oidc-client:cleanTest :oidc-client:test` (all tests really run) | 64 tests, 0 failures, 0 errors | 51 s |
| `verify-family-signing.sh` | both APKs `CN=PS03 App Family (test only)`, SHA-256 `a787b1f3...1e200de0` | under 5 s |

## One signing certificate for the family

Hub's single sign-on service will be guarded by the permission
`org.badmosip.nativesso.permission.FAMILY_SSO` with `protectionLevel="signature"`: Android grants it
only to apps signed with the same certificate as the app that declared it. So both apps must carry
one certificate, in debug builds too.

- The root [`build.gradle.kts`](build.gradle.kts) gives every application module a `family` signing
  config for debug and release.
- On the first build it creates a PKCS12 test keystore with `keytool` (EC P-256, alias `family`,
  `CN=PS03 App Family (test only)`, 10 years) at `$PS03_RUN_DIR/keys/ps03-family-test.jks`, and a
  random password in `ps03-family-test.jks.password` next to it. `PS03_RUN_DIR` defaults to
  `<repo>/../run` when that folder exists, else `<repo>/run`, as in `scripts/lib.sh`.
- Neither file is ever committed (`.gitignore` covers `*.jks` and `run/`; `scripts/repo-lint.sh`
  fails on any keystore in the tree). They are test material: never sign a store release with them.
- Overrides: Gradle property `ps03.familyKeystore` or environment variable `PS03_FAMILY_KEYSTORE` for
  the path; `ps03.familyKeystorePassword` or `PS03_FAMILY_KEYSTORE_PASSWORD` for the password.
- Every team member who builds gets their own certificate. Install both apps from the same build
  machine, otherwise the permission is not granted (that is the protection working).
- Both apps declare the permission, not only Hub, so whichever is installed first defines it with
  the signature level (see "custom permission takeover" in
  [`FamilySsoContract.kt`](sso-contract/src/main/java/org/badmosip/nativesso/contract/FamilySsoContract.kt)).

## The binder contract (`sso-contract`)

```
interface IFamilySso {
    int getVersion();
    @nullable SsoBundle getSsoBundle();                    // null when nobody is signed in to Hub
    String signDpopProof(String htm, String htu, @nullable String ath, @nullable String nonce);
}
parcelable SsoBundle { String issuer; String idToken; String deviceSecret; String scope; long expiresAtEpochSec; }
```

- Hub never hands out its access token or any private key. It shares the id_token and device secret,
  and signs DPoP proofs itself, because the token exchange must be proven with the key the device
  secret is bound to (statement task 9 iii.4, `cnf.jkt`).
- Errors travel as `ServiceSpecificException` codes: `ERROR_NOT_SIGNED_IN`, `ERROR_CALLER_NOT_ALLOWED`,
  `ERROR_HTU_NOT_ALLOWED`, `ERROR_RATE_LIMITED`.

## The client library (`oidc-client`)

Package `org.badmosip.nativesso.oidc`. Blocking calls (run them off the main thread); every failure
is an `OidcException` subtype; `toString()` of every token-holding type hides the tokens.

| Type | Job | Reference |
|---|---|---|
| `OidcClient.discover` | reads discovery and checks the document names exactly the expected issuer | OpenID Connect Discovery 4.3 |
| `LoginSession` | `state`, `nonce` and a PKCE pair for one browser login | RFC 6749 10.12, RFC 7636 |
| `OidcClient.pushAuthorizationRequest`, `authorizationUrl` | PAR with a DPoP proof, then the URL with only `client_id` and `request_uri` | RFC 9126, RFC 9449 10 |
| `OidcClient.parseAuthorizationResponse` | checks `state` and `iss`, maps `error` to a typed error | RFC 6749 4.1.2, RFC 9207 |
| `OidcClient.exchangeAuthorizationCode` | code + `code_verifier`; the answer holds `device_secret` when `device_sso` was granted | RFC 6749 4.1.3, Native SSO 3.4 |
| `OidcClient.exchangeDeviceSecret` | the Native SSO token exchange | Native SSO 4.1, RFC 8693 2.1 |
| `OidcClient.userInfo` | `Authorization: DPoP <token>` with a proof carrying `ath`; JSON or signed JWT answers | RFC 9449 7.1, OpenID Connect Core 5.3 |
| `DpopProofFactory` / `DpopProofSource` | builds proofs over a `JwsSigner`; the source interface lets Health Card forward proof requests to Hub | RFC 9449 4.2 |
| `ClientAssertionFactory` | `private_key_jwt` assertion, new `jti` per request | RFC 7523 3, FAPI 2.0 5.3.2.1 |
| `JwsSigner`, `EcSignatures`, `SoftwareEcSigner` | signing interface for Keystore-held keys; DER to `R || S` conversion; in-memory signer for tests | RFC 7518 3.4 |
| `JwkThumbprint` | RFC 7638 SHA-256 thumbprint, the `cnf.jkt` value | RFC 7638, RFC 9449 6.1 |
| `IdTokenValidator`, `IssuerSignatureVerifier`, `HttpJwksSource` | id_token checks (below); one JWKS refresh on an unknown key | OpenID Connect Core 3.1.3.7 |
| `OAuthErrorException` and siblings | typed errors (below) | RFC 6749 5.2 |

Decisions worth knowing:

- **Token type with an underscore.** `subject_token_type` is
  `urn:ietf:params:oauth:token-type:id_token`, as RFC 8693 section 3 registers it, Native SSO 4.1
  requires it and Thunder compares it. The request example in Native SSO 4.2 writes `id-token` with a
  hyphen; a server following RFC 8693 rejects that.
- **No `device_sso` in a token exchange.** `DeviceSecretExchange` refuses it, and refuses a scope
  without `openid`, before anything is sent (statement task 10b, Native SSO 4.1). The server checks
  it too; the client check keeps a coding mistake from ever reaching the network.
- **Client assertion audience = issuer.** FAPI 2.0 5.3.2.1 asks for it and Thunder accepts nothing
  else; `AssertionAudience.ENDPOINT` exists for servers that want the endpoint URL.
- **`token_type` must be `DPoP`.** A `Bearer` answer to a DPoP request is refused (RFC 9449 5).
- **`use_dpop_nonce`.** On that error with a `DPoP-Nonce` header (400 from the token endpoint, or 401
  with `WWW-Authenticate: DPoP error="use_dpop_nonce"` from userinfo) the request is sent once more
  with the nonce in a fresh proof and a fresh client assertion; a second nonce error is returned to
  the caller. The latest nonce per server is reused on the next request.
- **`ds_hash` rule.** Native SSO 3.4.1 leaves the binding between `ds_hash` and the device secret to
  the server. This project's eSignet uses the `at_hash` construction with SHA-256: base64url of the
  left 16 bytes of SHA-256 over the ASCII device secret (`TokenHashes.dsHash`).
  `IdTokenValidator` takes another rule as a parameter if a server differs.

id_token checks, each with its own `IdTokenFailure` reason and a test:

| Check | Reason on failure |
|---|---|
| a signed JWT with readable claims of the right types; `exp`, `iat`, `sub` present | `MALFORMED` |
| `alg` on the allowed list (PS256/384/512, RS256/384/512, ES256/384/512; never `none` or HMAC) | `ALGORITHM` |
| a key in the issuer's JWKS with the token's `kid` (one forced refresh before giving up) | `UNKNOWN_KEY` |
| signature verifies | `SIGNATURE` |
| `iss` is the issuer | `ISSUER` |
| `aud` contains the app's client_id; with several audiences, `azp` is the app | `AUDIENCE` |
| not expired, beyond 60 s clock skew | `EXPIRED` |
| `iat` not more than 60 s in the future | `ISSUED_IN_FUTURE` |
| `nonce` equals the login's nonce (when the app expects one) | `NONCE` |
| `at_hash` matches the access token (when both are present) | `AT_HASH` |
| with a device secret: `ds_hash` present and matching | `DS_HASH` |
| with a device secret: `sid` present | `SID_MISSING` |

Typed errors:

| Exception | When | What the app does |
|---|---|---|
| `OAuthErrorException` (`error`: `OAuthError`, `code`, `description`, `httpStatus`) | the server sent an OAuth error | `requiresInteraction` is true for `interaction_required`, `consent_required`, `login_required`: Health Card then runs its own browser login with `prompt=consent` (bonus re-consent). `invalid_grant` from an exchange means the Hub session ended. |
| `HttpStatusException` | an HTTP error without an OAuth error body | show a retry message |
| `NetworkException` | no connection, timeout, reset | show an offline message |
| `InvalidResponseException` | a broken answer: missing fields, wrong `token_type`, wrong issuer in discovery, `state` mismatch | stop; do not keep any token |
| `IdTokenValidationException` (`reason`) | an id_token check failed | stop; do not keep any token |

What the 64 tests cover (`oidc-client/src/test`): the RFC 7638, RFC 7636 and RFC 9449 example
vectors; DER/JWS signature conversion including short values; every proof and assertion claim; every
id_token check above with a passing and a failing case; PAR, code exchange, token exchange and
userinfo requests field by field against MockWebServer; nonce retry once and only once; error mapping
from JSON bodies and from `WWW-Authenticate`; discovery issuer mismatch, missing fields, HTTP 500 and
a dropped connection.

## Licences of the app dependencies

All OSI-approved; listed with versions in [`../THIRD_PARTY.md`](../THIRD_PARTY.md).
