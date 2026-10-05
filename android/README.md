# Android apps and the OpenID Connect client

Two Kotlin apps from one publisher, signed with one certificate, and the client library they share:

- **Hub** (Super App, `org.badmosip.nativesso.superapp`): the user signs in here once with eSignet
  (OTP and consent in the browser). It keeps the tokens and the Native SSO device secret.
- **Health Card** (Child App, `org.badmosip.nativesso.childapp`): signs the same user in without a
  password or OTP, by sending Hub's id_token and device secret to eSignet in a token exchange.

> **State on 2026-10-05.** Built and tested: the Gradle project, shared signing, the binder contract,
> the `oidc-client` library (64 JVM tests), the `secure-store` module (13 JVM tests), Hub (sign-in
> with eSignet, the family single sign-on service, "Sign out of all apps" that deletes the DPoP key:
> 54 JVM and Robolectric tests) and Health Card (silent sign-in by token exchange, re-consent,
> sign-out, accessibility: 50 Robolectric and JVM tests). NOT RUN yet on the emulator:
> `bash android/scripts/run-ui-test.sh all` (`SuperAppSignInTest`, `ChildAppSilentSignInTest`,
> `ChildAppReconsentTest`, see [Health Card](#health-card-silent-sign-in-re-consent-sign-out)) and
> the family steps (see [Family single sign-on](#family-single-sign-on-hub-service-and-health-card)).

## Modules

| Module | Kind | What it holds |
|---|---|---|
| `oidc-client` | Kotlin/JVM library, no Android code | discovery, PKCE, DPoP proofs, `private_key_jwt`, PAR, authorization-code and Native SSO token-exchange requests, userinfo, `use_dpop_nonce` retry, RFC 7638 thumbprints, id_token checks including `ds_hash` and `sid`, typed errors |
| `sso-contract` | Android library | `IFamilySso.aidl` and the `SsoBundle` parcelable, the permission, action and package names, error codes, a signing-certificate helper |
| `secure-store` | Android library | `EncryptedFileStore`: one AES-256-GCM file per blob in `noBackupFilesDir`, key in Android Keystore (`KeystoreSecureStore`) |
| `superapp` | Android app | Hub: sign-in with eSignet (Custom Tab, PAR, PKCE, DPoP, `private_key_jwt`), Keystore keys, encrypted token storage; the family single sign-on service (`sso/`); declares the family permission |
| `childapp` | Android app | Health Card: asks Hub for its shared sign-in over the binder (`HubConnection`), signs in by Native SSO token exchange with proofs from Hub, re-consent through its own PAR login with `prompt=consent`, its own Keystore keys and encrypted store; declares the same permission and `<queries>` entries for Hub and Custom Tabs |

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
bash gradlew --no-daemon :secure-store:testDebugUnitTest :superapp:testDebugUnitTest :childapp:testDebugUnitTest
bash gradlew --no-daemon assembleDebug assembleDebugAndroidTest   # BUILD SUCCESSFUL (APKs and test APKs)
bash scripts/verify-family-signing.sh                      # family-signing: OK <sha-256>
```

- `gradlew` and the scripts in `scripts/` are stored executable, so on Linux and macOS
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

Hub's single sign-on service is guarded by the permission
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

## Hub sign-in with eSignet

What happens when the user taps "Sign in with eSignet" (`superapp/.../auth/HubAuthenticator.kt`, no
Android code, so every step is covered by JVM tests):

1. Discovery of `ps03.issuer`; the document must name exactly that issuer.
2. PAR with `scope=openid profile` (plus `device_sso` when "Use this sign-in in my other apps" is on),
   `acr_values=mosip:idp:acr:generated-code`, a `claims` request for `name` in userinfo, PKCE S256,
   `state`, `nonce`, a DPoP proof and a `private_key_jwt` assertion (audience = issuer).
3. The pending login (`state`, `nonce`, `code_verifier`, the switch) is written to the encrypted
   store, then the authorization URL opens in a Custom Tab.
4. eSignet's login pages (OTP, consent) redirect to `org.badmosip.nativesso.superapp://oauth/callback`.
   `RedirectActivity` (exported, shows nothing) passes the URI to `MainActivity` (`singleTask`, so the
   Custom Tab closes). Because the pending login is on disk, this also works when Android killed Hub
   while the browser was open.
5. A redirect whose `state` differs is refused and leaves the login pending (another app cannot
   cancel it); a matching one uses the pending login up, so a replay finds nothing. A login older
   than 10 minutes is refused.
6. Code exchange with the DPoP proof; then every id_token check of `IdTokenValidator`, with the
   login's `nonce`, the access token (`at_hash`) and, when a device secret came back, `ds_hash` and
   `sid`. A device secret the login did not ask for is refused. Any failure stores nothing.
7. Userinfo (signed JWT, verified with the issuer's keys; ignored when its `sub` differs) gives the
   name for the home screen. When userinfo fails the login still stands, without a name.
8. The `TokenBundle` (access token, id_token, device secret if any, `sub`, `sid`, DPoP `jkt`, name)
   is written to the encrypted store as one blob. `toString()` of every record hides the tokens.

Keys (`superapp/.../keys/`):

| Key | Alias | Use |
|---|---|---|
| DPoP key, EC P-256 | `hub-dpop-es256` | every DPoP proof; the tokens and the device secret are bound to it (`cnf.jkt`). "Sign out of all apps" deletes it (`ReplaceableSigner`); the next sign-in makes a new one, so every device secret and Health Card token bound to the old key stops working |
| client key, EC P-256 | `hub-client-auth-es256` | `private_key_jwt`; `kid` = its RFC 7638 thumbprint |
| store key, AES-256-GCM | `secure-store-aes` | encrypts the store (module `secure-store`) |

- All three are Android Keystore keys: created on first use, never exportable, in StrongBox when the
  device has one (else TEE; software on an emulator). `EcKeyJwsSigner` turns Keystore's DER ECDSA
  signatures into the 64-byte `R || S` form JWS needs.
- Debug builds derive the client_id from the client key: `ps03-hub-` + 12 hex characters of SHA-256
  over its thumbprint. Each install has its own key and so its own registration; eSignet's client
  management never changes a registered key. Debug builds also write the public half to
  `files/client-registration.json` for `scripts/register-client.sh`.

Storage (`secure-store`): one file per blob in `noBackupFilesDir/secure-store/`, layout magic `PS3S`,
version byte, IV length, IV, AES-GCM ciphertext and tag; the associated data is magic, version and
blob name, so a file copied under another name fails. Writes go to a temporary file that is moved
over the old one. A blob that cannot be decrypted (key gone, file changed) counts as signed out and is
deleted. Hub also sets `allowBackup=false` and data-extraction rules that exclude everything. Jetpack
`security-crypto` is not used (deprecated since 1.1.0-beta01).

Cleartext HTTP: release builds allow none (`src/main/res/xml/network_security_config.xml`); debug
builds allow it for `localhost` and `127.0.0.1` only (`src/debug/res/...`), for the local stack.

### On the emulator

```bash
source /e/Projects/MOSIP/toolchain/env.sh && use_java21
bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all
# ... OK (3 tests)
```

| Script | What it does |
|---|---|
| `scripts/with-emulator.sh <cmd...>` | takes the `emulator` lock; boots AVD `ps03_api34` headless on port 5584 (`-gpu host`), waits for boot and network, `adb reverse` 38088 and 38300, turns Chrome's first-run screens off, runs the command, then cleans up and kills the emulator it started |
| `scripts/register-client.sh [superapp\|childapp]` | reads `files/client-registration.json` with `adb shell run-as` (starts the app once if needed) and registers the client through `POST /client-mgmt/client` (private_key_jwt, PAR, PKCE, DPoP, relying party `ps03-family`, grants `authorization_code` and token exchange, `allowed_authorization_scopes` `device_sso`). A client_id registered before 2026-10-05 lacks the last two: reinstall the app (new key, new client_id) or start the stack from clean volumes |
| `scripts/run-ui-test.sh <app> <Class>` | builds the app and test APKs, installs them, registers the client, runs the class with `am instrument`, saves logcat, the last screenshot and window dump to `$PS03_RUN_DIR/evidence/ui-<app>-<time>/` and the screenshots a test takes on purpose to `$PS03_RUN_DIR/evidence/<date>/screens/` |
| `scripts/run-ui-test.sh all` | `SuperAppSignInTest`, then `ChildAppSilentSignInTest` and `ChildAppReconsentTest` (they use Hub's sign-in from the first); prints `OK (3 tests)` or `FAILED: <classes>` |

`SuperAppSignInTest` (UI Automator): signs out, starts Hub, turns "Use this sign-in in my other apps" on, taps "Sign in with
eSignet", then in Chrome types UIN `1774231323`, taps the captcha box when there is one, "Get OTP",
types OTP `111111`, "Verify" and "Allow", and expects Hub's home screen with the name
`Siddharth K Mansour` (arguments `uin`, `otp`, `expectedName`; environment `PS03_TEST_UIN`,
`PS03_TEST_OTP`, `PS03_TEST_NAME` in `run-ui-test.sh`). Then it checks the stored session has an
id_token and a device secret whose `ds_hash` is in the id_token next to `sid`, the file is under
`noBackupFilesDir`, and neither the id_token, the access token, the device secret nor the JSON field
names can be read in the file.

What the JVM tests cover:

- `secure-store` (13): round trip, ciphertext only on disk, new IV per write, atomic replace, a
  changed ciphertext or IV byte, a file copied under another name, wrong magic or version, truncation,
  another key, delete, empty blob, name rules.
- `superapp` (33 of its 54; the other 21 are listed under [Family single sign-on](#family-single-sign-on-hub-service-and-health-card)): the whole sign-in against a fake eSignet on MockWebServer (PAR and token fields,
  PKCE pairing, client assertion audience and `kid`, DPoP proofs at PAR, token and userinfo with the
  right `htu`); `device_sso` on with the device secret kept after the `ds_hash` check; wrong `ds_hash`,
  wrong `nonce`, an unasked device secret, error redirects: nothing stored; `state` mismatch keeps the
  login pending; replay, expiry, another redirect URI; finishing in a new process from the same store;
  userinfo failing or about another `sub`; sign-out (which deletes the DPoP key, and a new key at the
  next use: `ReplaceableSignerTest`); text contrast of both color schemes (`HubThemeTest`); tokens unreadable in the encrypted file; records,
  name rules, redirect matching, client_id rule; ES256 signatures from `EcKeyJwsSigner` verified by
  Nimbus and by `java.security`.

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
- Refusals carry one of the codes `ERROR_NOT_SIGNED_IN`, `ERROR_CALLER_NOT_ALLOWED`,
  `ERROR_HTU_NOT_ALLOWED`, `ERROR_RATE_LIMITED` in the exception message:
  `FamilySsoContract.exceptionFor(code, message)` makes a `SecurityException` for a caller that is
  not allowed and an `IllegalStateException` otherwise; `FamilySsoContract.errorCodeOf(e)` reads the
  code back. `android.os.ServiceSpecificException` was the first plan, but it is not in the public
  SDK (absent from android-34's `android.jar`), and a binder call can only rethrow public exception
  types. A bad `ath` or `nonce` arrives as `IllegalArgumentException` without a code.

## Family single sign-on: Hub service and Health Card

Hub (`superapp/.../sso/`):

- `FamilySsoService`: the bound service. Exported, `android:permission` = the family permission,
  intent action `org.badmosip.nativesso.action.FAMILY_SSO`; any other action gets no binder. It
  passes `Binder.getCallingUid()` to `FamilySsoGate` on every call.
- `FamilySsoGate` (no Android types, so all rules run in JVM tests), checked in this order on every
  call:
  1. caller: a package of the calling uid is on the allow-list (Health Card only) and is signed with
     Hub's own current certificate (`PackageManager` `GET_SIGNING_CERTIFICATES`, SHA-256). The
     permission alone is not trusted: an app of another signer can hold it after the takeover
     sequence recorded in `docs/STATUS.md` (WP-11);
  2. rate: 30 calls per caller uid in any 60 s (one silent sign-in takes about five);
  3. session: only a sign-in with a device secret, that is, one made with "Use this sign-in in my
     other apps" on. Otherwise `getSsoBundle()` is null and `signDpopProof` refuses with
     `ERROR_NOT_SIGNED_IN`;
  4. proofs: `POST` to the token endpoint, `GET` or `POST` to the userinfo endpoint, exactly as
     discovered at Hub's login (stored with the session); no query, fragment or user info; host
     compared without case. Hub builds `jti`, `iat`, `htm`, `htu` itself; the caller gives only `ath`
     (43 base64url characters) and `nonce` (RFC 9449 `NQCHAR`, at most 512).
- The bundle is the id_token, the device secret, the issuer and the granted scope.
  `expiresAtEpochSec` is 0: the device secret is encrypted for the server, so Hub cannot read its
  expiry.
- "Apps using this sign-in" on Hub's home screen: package and last time of each `getSsoBundle()`,
  kept in the encrypted store (blob `family-access`), cleared at sign-out. Logs name the package and
  the method, never a token.
- Debug builds only: `DebugSessionSeeder` (`src/debug/`), a receiver guarded by
  `android.permission.DUMP` (held by `adb shell`, by no ordinary app), stores a made-up shared
  session (`op=seed`) or signs out (`op=clear`), so the family steps need no server.

Health Card (`childapp/.../HubConnection.kt`):

- `HubConnection.use { service -> ... }` binds with an explicit intent to Hub's service class,
  waits at most 5 s, runs the calls, unbinds. Call it off the main thread.
- `status()` gives `Available(bundle)` or `Unavailable(reason)`: `NOT_SIGNED_IN`, `HUB_MISSING`,
  `BIND_REFUSED` (Android refused the bind: no permission), `CALLER_REFUSED` (Hub's per-call
  check), `TIMEOUT`, `RATE_LIMITED`, `VERSION_MISMATCH`, `FAILED`. The screen says what the user
  can do for each.
- `HubDpopProofSource(service)` is the `DpopProofSource` the token exchange and userinfo use (see
  [Health Card](#health-card-silent-sign-in-re-consent-sign-out)).

Test builds and scripts:

| Command | What it does |
|---|---|
| `bash android/scripts/build-apks.sh` | three Gradle builds, copied to `$PS03_RUN_DIR/apks/`: `family` (Hub, Health Card, its test APK), `intruder` (`-Pps03.intruder=true`: Health Card's package signed with the Android debug certificate, permission not declared), `takeover` (`-Pps03.intruder=takeover`: the same, permission declared) |
| `bash android/scripts/with-emulator.sh bash android/scripts/run-family-tests.sh` | installs the sets and runs `FamilySsoBindTest` step by step; prints `family-bind PASS`, `htu-allow-list PASS`, `signed-out PASS (ERROR_NOT_SIGNED_IN)`, `intruder PASS (SecurityException)`, then `takeover PASS (rejected)` or, when Android refuses to install Hub next to the app that defined the permission, `takeover PASS (install refused: ...)`; leaves the family set installed |

What the JVM and Robolectric tests cover:

- `superapp` `FamilySsoGateTest` (14): bundle contents; another certificate refused on all three
  calls; a family-signed package outside the allow-list; an unknown uid; Hub without a readable
  certificate admits nobody; no session and a session without device secret; proofs verified with
  Hub's key (`htm`, `htu`, `ath`, `nonce`, `iat`, new `jti` each time); 13 refused method and URL
  forms; no endpoints stored; malformed `ath` and `nonce`; rate limit and its window; refused
  callers do not use up the rate; the access list (no tokens in it or in logs, unreadable blob
  dropped, one row per package).
- `superapp` `FamilySsoServiceTest` (7, Robolectric, Android 14): the merged manifest (exported,
  permission, signature protection), the stub with `Binder.getCallingUid()` and certificates read
  through `PackageManager`: family caller served; a caller with Health Card's package name and a
  different signature refused on every call; an unknown uid; not signed in; a foreign `htu`.
- `childapp` `HubConnectionTest` (10, Robolectric): the explicit intent, bundle available and
  connection released, not signed in, each coded refusal, Android refusing the bind, Hub missing,
  the timeout, another contract version, proofs forwarded and an `htu` refusal surfaced, an
  uncoded exception passed through.

## Health Card: silent sign-in, re-consent, sign-out

Silent sign-in (`childapp/.../auth/CardAuthenticator.kt`, no Android code; `CardController.kt` holds
the screen logic, also without Android types):

1. Health Card opens and asks Hub (`HubConnection`) for its shared sign-in. None: the screen "Sign in
   to Hub first" with the reason, "Open Hub" and "Check again".
2. Token exchange through `oidc-client` (Native SSO 1.0 section 4.1): `subject_token` = Hub's id_token,
   `actor_token` = the device secret, `audience` = the issuer, `scope=openid profile` (never
   `device_sso`), Health Card's own `private_key_jwt`, and a DPoP proof signed by Hub's key through
   Hub's service (the device secret is bound to that key; `use_dpop_nonce` is answered once with a new
   proof from Hub).
3. id_token checks: signature, `iss`, `aud` = Health Card's client_id, `at_hash`, and `sid` equal to
   the `sid` of Hub's id_token. No device secret may come back. Any failure stores nothing.
4. Userinfo with the new access token and a Hub-signed proof carrying `ath`: name, email (and phone
   when granted) for the home screen.
5. `CardSession` (tokens, scope, `sid`, Hub's `sid`, which key the token is bound to) in Health Card's
   own encrypted store. No browser, no password, no OTP.

Re-consent (bonus B1): "Add my phone number" asks the exchange for `openid profile phone`. Hub's
login never had `phone`, so eSignet answers HTTP 400 `interaction_required` with "user consent is
needed for: phone". Health Card shows "Health Card needs your permission" (which permission, and that
eSignet will ask for the ID and a new OTP because it keeps no browser session):

- "Continue to eSignet": Health Card's own PAR login (`prompt=consent`, its own DPoP key, PKCE,
  `state`, `nonce`) in a Custom Tab; the redirect `org.badmosip.nativesso.childapp://oauth/callback`
  comes back through `RedirectActivity`; code exchange, id_token checks with `nonce`, userinfo; the
  session is now Health Card's own (`binding=OWN`).
- "Not now", "Cancel" in the browser step, or "deny" on eSignet's page (`error=access_denied`): the
  earlier sign-in stays and the screen says nothing changed.

Sign-out:

- Hub "Sign out of all apps" deletes Hub's tokens, device secret, access list and its DPoP key; Hub's
  service then answers `ERROR_NOT_SIGNED_IN`.
- Health Card asks Hub on every resume. Hub signed out or uninstalled: Health Card deletes its own
  tokens and shows "Sign in to Hub first". Hub signed in again (another `sid`): Health Card signs in
  again silently. Hub busy or slow: Health Card keeps its sign-in.
- "Sign out of Health Card" deletes only Health Card's tokens; Health Card stays signed out (no
  automatic sign-in) until the user taps "Continue with Hub".

Errors and what the screen says: `invalid_grant` (Hub's login ended: open Hub), other OAuth errors
with their code, HTTP errors without an OAuth body, no network, a failed security check (with the
check's name), an unusable answer, Hub refusing or timing out (with the reason), consent declined, a
login that took too long, a redirect that is not the login in progress.

Health Card's keys (`childapp/.../keys/CardKeys.kt`, Android Keystore, StrongBox when present):
`card-client-auth-es256` (`private_key_jwt`, client_id `ps03-card-` + 12 hex characters of SHA-256
over its thumbprint, registered with `register-client.sh childapp`) and `card-dpop-es256` (only for
its own consent login). Debug builds allow cleartext HTTP to `localhost` only, as Hub does.

Accessibility, both apps: headings on titles; a label and its value are one TalkBack item; status
lines and errors are polite live regions; progress indicators carry a description; Hub's sharing
switch row is one toggle with the switch role; all buttons are full width and at least 48 dp high;
every screen scrolls, so 200 % font size cuts nothing off; reading order is title, status, actions,
then details; text colors reach at least 4.5:1 on their backgrounds in light and dark mode
(`CardTheme.kt`, `HubTheme.kt`).

What the Robolectric and JVM tests cover (`childapp`, 50 with `HubConnectionTest`):

- `CardAuthenticatorTest` (15): the exchange form field by field, Health Card's client assertion,
  both proofs verified with Hub's key (`htu`, `ath`), the stored session; a nonce challenge; ten
  OAuth errors (`invalid_request`, `invalid_grant`, `invalid_target`, `unauthorized_client`,
  `invalid_scope`, `invalid_dpop_proof`, `invalid_client`, `interaction_required`, `server_error`,
  `temporarily_unavailable`) each storing nothing; HTTP 502 and a dead server; wrong `aud`, wrong or
  missing `sid`; a Bearer token; Hub sharing nothing, refusing a proof, refusing the caller, not
  installed; Hub signed in to another server; `interaction_required` to the consent login (`prompt`,
  scope, Health Card's own key) and its result; refusal on eSignet's page; `state` mismatch; sign-out
  and its mark; records.
- `CardControllerTest` (13): silent sign-in on start; sharing off and Hub missing; seven server
  answers, a security check and no network to their screens; Hub busy keeps the sign-in; re-consent
  accepted (consent screen, browser URL, resume ignored during the login, own session with phone);
  re-consent declined on the screen, on eSignet's page, by cancelling, and with no earlier sign-in;
  Hub signing out clears both kinds of session; Hub's new login signs in again; user sign-out sticks
  across restarts.
- `CardScreensTest` (10, Compose on Robolectric, 200 % font on a 360 x 640 dp screen): every screen's
  texts, headings, merged label-value items, live regions, the progress description, reading order,
  and that every button is reachable, at least 48 dp high and runs its action, in light and dark.
- `CardThemeTest` (2): the contrast formula and every text and background pair of both schemes.

UI Automator tests (need the stack and the emulator; `run-ui-test.sh all` runs them after Hub's):

- `ChildAppSilentSignInTest`: Hub shares its sign-in; Health Card opens on its home screen with the
  name while a watcher checks every 200 ms that no browser comes to the front; the session is bound
  to Hub's key with Hub's `sid`, no `device_sso`, encrypted on disk. Screenshot
  `health-card-silent-sign-in.png`.
- `ChildAppReconsentTest`: "Add my phone number", the consent screen (screenshot
  `health-card-consent-screen.png`), "Continue to eSignet", UIN, a new OTP and "Allow" in Chrome,
  then Health Card's own session with `phone` in its scope (screenshot `health-card-after-consent.png`).

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
