# Threat model: Native SSO for mobile apps in eSignet Go

- Status: **final for submission, 2026-10-05.** Written 2026-10-04 for the mentor call; the mitigations were
  re-checked against the built code on 2026-10-05. It covers the design in [design.md](design.md). Test ids (R01,
  A03, ...) are defined in [design.md section 20](design.md#20-rules-and-tests); dated results per test are in
  [test-report.md](test-report.md). Emulator tests ran on 2026-10-05 (one emulator image, limit L06).
- Method: assets and trust boundaries first, then STRIDE (spoofing, tampering, repudiation, information
  disclosure, denial of service, elevation of privilege) per component, then the threats T1-T12 with mitigations,
  what is left, and the tests that cover them.
- Sources: Native SSO draft 07 section 6, RFC 9449 section 11, RFC 8725, FAPI 2.0 Security Profile section 6, the
  Android guidance on custom permissions, and an emulator test run on 2026-10-04 (section [4](#4-threats-t1-t12)).

## 1. Assets

| Asset | Where it lives | Why it matters |
|---|---|---|
| Device secret | Super App storage (encrypted); in transit to the Child App and the token endpoint | With an id_token and a proof from the Super App's key it gives any family app tokens for the user |
| Super App DPoP private key | Android Keystore of the Super App, non-exportable | Every Super App and Child App token, and the device secret, is bound to it (`cnf.jkt`) |
| Each app's client key (`private_key_jwt`) | Android Keystore of that app, non-exportable | Proves which client is calling the token endpoint |
| id_tokens and access tokens | Both apps' storage (encrypted) | Identify the user; access tokens read the user's claims at userinfo |
| Login session (`sid`) and its session record | eSignet runtime store (memory or Redis) | Decides whether silent sign-in still works |
| Released user claims | Thunder attribute cache, eSignet consent records | Personal data |
| eSignet signing and encryption keys | eSignet keymanager (database keys wrapped by the HSM or PKCS#12 master key) | Forge or read device secrets and id_tokens |

## 2. Trust boundaries

1. **Between apps on the phone.** Each Android app runs as its own Linux user. The only way across is the Super
   App's bound service `FamilySsoService`.
2. **Between the phone and eSignet.** HTTP requests from the apps and from the Custom Tab. In production this is
   TLS; the local demo uses plain HTTP to `localhost` over `adb reverse`, which never leaves the laptop.
3. **Between the Custom Tab and the apps.** The authorization response comes back through a redirect to the
   app's scheme.
4. **Inside eSignet.** Thunder treats the device secret as opaque and trusts the provider's answer; the provider
   trusts the keymanager and the runtime store.

Assumptions:
- Apps signed with the vendor's certificate are trusted by design: Native SSO section 1 is about apps "signed by
  the same vendor". A compromised vendor signing key defeats every on-device control.
- The phone's OS and Keystore are not compromised. A rooted phone is treated as residual risk, not mitigated.
- eSignet's own keys and database are protected as they are today (architecture section 2.4).

## 3. STRIDE per component

| Component | Spoofing | Tampering | Repudiation | Information disclosure | Denial of service | Elevation of privilege |
|---|---|---|---|---|---|---|
| Super App storage | - | T7 (swap secret or id_token) | - | T1 | - | - |
| `FamilySsoService` (bound service) | T2 (fake family app) | - | caller package logged per call | T1 (bundle handed to an intruder) | rate limit per caller | T6 (signing oracle) |
| Child App | T2 (another client posing as it) | - | - | T1 (its own tokens) | - | T5 (asks for more scope) |
| Custom Tab login | T11 | T11 (code injection) | - | T11 | - | - |
| Network, phone to eSignet | T8 | T4 (replay) | - | T1 (without TLS) | - | - |
| Thunder token endpoint | T2, T8 | T7 (forged or mixed tokens) | eSignet access log with correlation id; audit events are future work | error texts name no user data | client auth and cheap request checks run before any decryption | T5, T9 |
| eSignet DeviceSecretProvider and keymanager | T7 (token of another type) | T7 | - | T1 (claims inside the secret, prevented by JWE) | RSA decryption only after client auth | - |
| Runtime store | - | session records written only by the provider | - | user id kept server side, not in the token | TTL on every record | T9 (stale record) |
| The person holding the phone | T10 | - | - | T10 | - | T10 |

## 4. Threats T1-T12

| Threat | How it happens | Mitigations | Residual risk | Tests |
|---|---|---|---|---|
| T1 Device secret theft | Malware, a backup, a log line or a rooted phone copies the device secret and the id_token from the Super App | Device secret is a JWE: readable only by eSignet. Bound to the Super App's non-exportable DPoP key by `cnf.jkt`, so it is useless without a fresh proof from that key (Native SSO 6.2, RFC 9449 6.1). Stored encrypted with a Keystore AES-GCM key in `noBackupFilesDir`, backup off. Never logged by the apps or the server. Lifetime capped by `exp` and the session record. "Sign out of all apps" deletes the DPoP key | Code running inside the Super App, or on a rooted phone, can still ask Keystore to sign while the app is installed | R09, R14, A03, A11 |
| T2 App impersonation | An app from another developer binds to `FamilySsoService`, or a client of another vendor sends the Super App's tokens to the token endpoint | Signature-level permission declared by both apps; a signing-certificate check on every call (`hasSigningCertificate`), because on 2026-10-04 the emulator let a foreign app redeclare the permission after the owning app was uninstalled; package allow-list. At the server the Child App authenticates with its own `private_key_jwt`, must allow `device_sso` and must share the Super App's relying party (D7) | A stolen vendor signing certificate defeats the on-device checks | A05, A06, R13, R19 |
| T3 Key extraction | An attacker copies the Super App's DPoP key or an app's client key | Keys are created in Android Keystore as non-exportable, in StrongBox when present; key material never enters the app process. The Child App never receives key material, only finished proofs | The emulator's Keystore is software-backed (demo limit); a rooted phone can use, not copy, the key | R14, A04 |
| T4 Replay | A captured DPoP proof, authorization code, `request_uri` or client assertion is sent again | DPoP `jti` store and `iat` window apply to every grant, the exchange included (RFC 9449 11.1, Thunder `dpop/verifier.go:107`); proofs bound to `htm` and `htu`; single-use codes and `request_uri` with short lifetimes (FAPI 2.0); client assertion `jti` store. The device secret is meant to be used many times, so each use needs a new proof | Pre-generated proofs (RFC 9449 11.2) until server DPoP nonces exist | R22 |
| T5 Scope escalation | The Child App asks for scopes the user never granted to the Super App, or for `device_sso` to mint its own device secret | Subset rule with a hard failure instead of Thunder's silent filter; `device_sso` in an exchange gives `invalid_scope`; both clients must allow `device_sso`; wider scope goes through eSignet's consent screen for the Child App (bonus) | A same-vendor app can always get what the user already consented to for the Super App: that is the purpose of Native SSO | R02, R13, R15, R16, R17 |
| T6 Signing oracle abuse | A family app, or a bug in one, asks the Super App to sign DPoP proofs for other servers or endpoints | The service builds every proof itself (fresh `jti`, current `iat`); it signs only `htm`/`htu` pairs on an allow-list (token endpoint, userinfo); it never signs caller-supplied bytes; per-caller rate limit; the Super App lists "Apps using this sign-in" | Apps with the vendor certificate are trusted by design (Native SSO section 1) | A05, A07 |
| T7 Token substitution or confusion | An id_token or access token is sent as the device secret; user A's device secret is sent with user B's id_token; an id_token is forged or edited; an expired id_token is reused | Inner `typ` `ds+jwt` and a dedicated signing key (RFC 8725 3.11, 3.12); both JWE layers validated (RFC 8725 3.3); `ds_hash` binds one id_token to one device secret; `sid` must match in both and in the session record; id_token signature checked against eSignet's own key; expired id_token accepted only under the conditions of section [5](#5-why-accepting-an-expired-id_token-is-safe) | - | R09, R11, R12, R20, R21 |
| T8 Use from another device | The device secret and id_token are copied to another phone or a server | `cnf.jkt` binding to the Super App's non-exportable key (Native SSO 6.2); the Child App's new tokens are bound to the same key | An attacker with code on the original phone can relay proofs (see T1) | R14, R22 |
| T9 Stale session | The user signs out of the Super App or the session ends, but a family app still exchanges | Sign-out deletes the DPoP key, so no valid proof can be made; Hub then revokes the device secret at the RFC 7009 revocation endpoint, which deletes its session record, so a later exchange gives `invalid_grant` (Native SSO 4.3 step 4); only the client the device secret was issued to may revoke it; the session record lives no longer than the device secret | Sign-out without network leaves the record until its TTL (the DPoP key is gone anyway); access tokens already issued to the Child App stay valid until their own `exp` | R12, R27, A11 |
| T10 Wrong person | Someone else picks up the unlocked phone and opens the Child App | Out of scope of the specification (Native SSO 6.4 says it does not ensure the same human); the device secret's lifetime is a setting; the Super App can end all family sign-ins at once; an option to require device unlock for the DPoP key is noted as future work | Accepted for the demo; relies on the phone's screen lock | A11 |
| T11 Interception of the Super App's login response | Another app registers the same redirect scheme, or an attacker injects a stolen code, or the login moves to another browser | PAR with client authentication; PKCE S256; authorization code bound to the DPoP key (RFC 9449 10.1); `state`, `nonce` and `iss` checked (RFC 9207); system browser via Custom Tab, never a WebView (RFC 8252, FAPI 2.0 6.5); a unique scheme per app, App Links as a production option | A custom scheme can be claimed by another app; the stolen code is still useless without the verifier and the DPoP key | A02, R07 |
| T12 Consent without the user | Health Card itself, which receives the `consent_uri`, posts "Allow" with its own HTTP client or web view so the user never sees the page; another app or a web page gets the `consent_uri` and answers, frames the page to trick a tap, forges the `native_sso_consent=granted` redirect, or reuses an old approval | The page shows the choice and records an answer only in the browser of the Super App's login: `/oauth2/authorize` sets an `HttpOnly`, `SameSite=Lax` cookie limited to the consent page for a `device_sso` login, its hash is kept under the login's `auth_id`, and the ticket carries that `auth_id`; an app's own client and web views do not have the browser's cookies, so the answer is 403 and the ticket stays for the user. The ticket is issued only after every binding check of the exchange, is 256 random bits stored as a hash, lives at most 300 s and is used up by the first answer; a form needs `Origin` or `Sec-Fetch-Site: same-origin`; the page refuses framing (`X-Frame-Options: DENY`, `frame-ancestors 'none'`), runs no script, sends no referrer to other sites and refuses a form from another origin; the approval counts only for that app, user and login session and only inside a token exchange that passes every binding check again; the redirect carries no authority; Health Card opens only the issuer's consent page | Malware that controls the system browser (accessibility service, rooted phone) could tap "Allow", as it could for any web consent; clearing the browser's cookies makes the user sign in again for the extra scope; the approval grants nothing without the device secret and the Super App's key | R28, A13 |

## 5. Why accepting an expired id_token is safe

The Child App almost always presents an expired id_token, and the server accepts it. This section explains why that
is the correct and safe choice.

Why it is expired:
- eSignet signs every engine JWT, id_tokens included, with a 120 s lifetime
  (`esignet-service/data/deployment.yaml:119`, `internal/config/app.go:345`), and sets no separate id_token
  lifetime. Opening the Child App more than two minutes after the Super App login is the normal case.
- Requiring an unexpired id_token would break silent sign-in two minutes after every login, which defeats the
  statement's user journey (steps 10-11).

Why the specifications allow it:
- Native SSO 6.3: the id_token "may have expired at the time of use"; it is used "in the same manner as
  id_token_hint" (Native SSO 4.1), not as a login assertion for the server.
- OpenID Connect RP-Initiated Logout 1.0 section 2 makes the same choice for `id_token_hint`: the provider should
  accept it after `exp` while the session is current or recent.

Why it is safe here: an expired id_token is accepted only when **all** of these hold.

| Check | What an attacker would need to get past it | Rule |
|---|---|---|
| Device secret present, decrypts with eSignet's key, inner `typ` and signature valid, `exp` not passed | A device secret eSignet issued and that has not expired (default lifetime 8 hours) | R09 |
| Session record for its `sid` exists and matches `auth_id` and `client_id` | A login session that has not ended | R12 |
| id_token signed by eSignet's own key, `iss` is eSignet, `aud` is the device secret's client | A genuine id_token; editing one breaks the signature | R20 |
| `ds_hash` in the id_token equals the hash of this device secret; `sid` equal in both | The id_token issued together with that exact device secret | R11, R12 |
| DPoP proof signed by the key whose thumbprint is the device secret's `cnf.jkt` | Use of the Super App's non-exportable Keystore key, on that phone, at that moment | R14 |
| Child App authenticated with its own key, allows `device_sso`, same relying party as the Super App | Being a registered family client of the same vendor | R13, R19 |
| Optional `id_token_max_age_seconds` cap | An id_token younger than the cap, when an operator sets one | R21 |

- The id_token's `exp` protects a relying party that receives an id_token as proof of a fresh login. Here the
  server does not rely on the id_token for that: it relies on the device secret, the session record and the DPoP
  key. The id_token only names the user and the session, and that statement stays true after `exp`.
- How long silent sign-in works is therefore decided by the device secret's `exp`, the session record's TTL and
  the Super App still holding its key, all of which the operator or the user controls.
- An attacker who has only an old id_token (from a log, a screenshot, a backup) gets nothing: the exchange needs
  the matching device secret and a proof from the Super App's key.

## 6. Out of scope and accepted risks

- A compromised vendor signing certificate or a rooted phone: the on-device controls assume neither.
- Same-human assurance (T10): Native SSO does not provide it; the screen lock does.
- Revocation of tokens already issued: revoking the device secret ends its session, so no new exchange succeeds,
  but access tokens issued before stay valid until their `exp` (eSignet keeps the token deny list of Thunder turned
  off, `oauth.token_revocation.enabled: false`).
- Child App tokens bound to the Super App's DPoP key (design decision D3): the Child App's access token carries
  the Super App's `cnf.jkt`, so the Child App needs the Super App for every userinfo or API call (it asks for a proof
  over the bound service each time). Gain: no second device key and one place to end every family token ("Sign out
  of all apps" deletes that key). Cost: the Child App cannot call APIs while the Super App is uninstalled or
  refuses, and a compromised Super App can act for every family app; giving the Child App its own DPoP key in the
  exchange would remove that coupling at the price of a second key to manage.
- Denial of service against eSignet in general: unchanged by this feature; the exchange adds one RSA decryption,
  which runs only after client authentication and the cheap request checks.
- The local demo runs over HTTP on `localhost`; every deployment beyond the laptop must use TLS (FAPI 2.0).
