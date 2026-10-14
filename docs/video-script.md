# Video script: Native SSO for mobile apps in eSignet Go

The script for the demo video the submission guidelines ask for ("typically 2-5 minutes ... the problem being addressed,
the functioning of the solution, the main characteristics"). It runs 4 minutes 45 seconds at a calm reading pace. Times
are planned, not measured: time one take and cut to fit before recording. Runbook for the live steps, fixed inputs and
fallbacks: [demo-script.md](demo-script.md). Checks to do before recording: [manual-test-checklist.md](manual-test-checklist.md).

How the rubric is covered (weights from the evaluation criteria): **Functionality 40 %** is shots 3 to 7, **Quality
of deliverable 40 %** is shots 2, 8 and 9, **User experience 10 %** is shots 3 to 6 (the apps' own screens), **Presentation
10 %** is shots 1, 2 and 10. Statement tasks are named as T1 to T10 and B1 (bonus), P1 and P2 (prerequisites).

## Before recording

- One clean take of the setup: `bash scripts/demo/demo.sh` (stack up, visible emulator, both apps installed and registered,
  Hub in front). Use `bash scripts/demo/reset.sh` between takes. Inputs: UIN `1774231323`, OTP `111111`.
- Screen-record the emulator window and the terminal in one capture, 1080p. Hide notifications.
- Record the terminal shot (shot 7) separately, or run it against the demo stack after shot 6; it takes under a minute.
- Do not claim anything that this file marks "say plainly".

## Shots

| # | Time | On screen | Narration | Proves |
|---|---|---|---|---|
| 1 | 0:00-0:20 | Slide: problem statement 03, team Badmosip, then `docs/diagrams/user-flow.png` | A citizen with several apps from one agency types an OTP into each of them, because mobile apps cannot share a browser session. Native SSO lets the second app reuse the first app's login, safely. We built it into eSignet Go and the Thunder engine, plus two real Android apps. | Presentation: problem, value. Statement: "Why we want to solve it" |
| 2 | 0:20-0:45 | `docs/diagrams/components.png`, then a quick pan over the folders `thunder/`, `esignet/`, `android/` | Thunder owns the protocol rules. eSignet owns the policy and the keys, through a Device Secret Provider interface that Thunder defines and eSignet implements. Hub is the first app, Health Card the second. | Quality: reusability and maintainability (provider interface, imports kept byte for byte). T3 |
| 3 | 0:45-1:20 | Emulator: Hub start screen, switch "Share this sign-in with my other apps" on, tap "Sign in with eSignet", Chrome shows eSignet's login page, type UIN, "Get OTP", OTP, consent "Allow", Hub shows "Signed in as" and "Sign-in shared with my other apps: Yes" | Hub asks for the extra scope `device_sso` with a pushed request, PKCE and a DPoP key that never leaves the phone's Keystore. eSignet checks that this client may use `device_sso`, then returns the tokens and a device secret: a signed, then encrypted token. Hub stores all of it encrypted. | P1, P2, T1, T2, T4, T5, T6 (statement user journey steps 1-9) |
| 4 | 1:20-1:55 | Emulator: open Health Card. It shows "Signing you in with Hub...", then its home screen with the name and "With Hub's sign-in. No password or OTP was needed." Switch to Hub: "Apps using this sign-in" lists Health Card | Health Card binds to Hub's service. Android lets only an app signed with the same certificate bind, and Hub checks the caller's certificate on every call. Hub hands over its id_token and device secret and signs a DPoP proof; Health Card sends them to the token endpoint as a token exchange, without `device_sso`. No browser, no OTP, no second consent. | T7, T8, T9 (journey steps 10-11). Both apps signed with one certificate (P1) |
| 5 | 1:55-2:35 | Terminal, pre-run: `bash e2e/run.sh -run 'TestE2E_Exchange_(DPoPKeyMismatch\|ScopeSuperset\|DeviceSSOInScope\|DSHashMismatch)$'` against the demo stack, then `docs/test-report.md` rows R11 to R16 and R21 | The server rejects what it must. A wrong DPoP key: the key in the device secret has to match the proof, so a stolen device secret alone is useless. A wider scope than Hub's: refused. `device_sso` in the exchange: refused, it is valid only at the code grant. A mismatched `ds_hash`: refused. An ended login session is refused too (row R12 of the test report); an expired id_token is still accepted while the device secret lives, as Native SSO 6.3 allows (row R21). | T9 iii.1-4, T10a, T10b; rules R11-R16, R21 |
| 6 | 2:35-3:25 | Emulator: Health Card "Allow card sharing", its consent screen, "Continue to eSignet", eSignet's own page with "Allow" and "Not now". First tap "Not now" and show "Nothing changed". Repeat and tap "Allow": "Card sharing with clinics: Allowed" | Health Card now asks for a permission Hub's login never had. eSignet answers `interaction_required` with a link to its own consent page, which needs no login. The page works only in the browser that did the Hub login, through a cookie the app cannot read, so the app cannot approve for the user. "Not now" changes nothing. "Allow" lets the same exchange succeed. New user data, such as the phone number, still needs a fresh login, and the app says so before the browser opens. | B1; rules R15, R28; consent presence cookie (threat T12) |
| 7 | 3:25-3:55 | Emulator: Hub "Sign out of all apps"; then open Health Card: "Sign in to Hub first". Then the terminal line of `TestE2E_Revoke_DeviceSecret` in `docs/test-report.md` (row R27) | Sign-out deletes Hub's DPoP key and revokes the device secret. eSignet ends the login session, and every access token it issued to Health Card carries that session id, so userinfo and introspection now refuse them. Health Card does not even try: it sees Hub is gone. | R27, A11; sign-out revocation |
| 8 | 3:55-4:20 | Screen: `docs/test-report.md` top table; terminal: `bash make.sh docs-check` output | 2468 Thunder and 1607 eSignet unit tests, 32 live end-to-end tests against the real stack, 43 Postman requests, the Android tests on a real emulator, linters with no findings, and a scan of the Go libraries with no reachable known vulnerability. Every rule has an id, a unit test and a live test. | Quality: tests, documentation, code quality. Deliverables: Postman collection, API docs |
| 9 | 4:20-4:35 | Screen: `docs/design.md` contents, `docs/api.md`, `docs/threat-model.md`, `esignet/postman-collection/` | The design document follows the RFCs, the API document lists the new endpoints and errors, and the threat model lists twelve threats, each with a mitigation and a test. | Quality: documentation. Deliverables: design, API docs |
| 10 | 4:35-4:45 | Closing slide: repository link, README, limits line | Limits, plainly: Health Card can sign in silently for 8 hours after Hub's login, the device secret's lifetime; after that Hub still says "signed in" but the exchange is refused and the user signs in to Hub again. The server keeps its sessions in memory. Not run: a physical phone and a TalkBack pass. | Presentation: honest scope. Known limits L03, L08, L09, L10 |

## Security story, for the narration of shots 3 to 7 and for Q and A

Say these in your own words; each line is a fact the code and a test already prove.

1. **Device secret.** A signed (ES256) then encrypted (JWE) token that holds `cnf.jkt`, `auth_id`, expiry, `client_id`,
   scope, `acr`, the names of the released claims and `iss`. The apps cannot read it. Test: `TestE2E_DeviceSecret_IsNestedJWE`.
2. **DPoP binding.** The device secret names the thumbprint of Hub's key. The exchange must carry a DPoP proof signed by that
   key, so the id_token and device secret together are not enough. Test: `TestE2E_Exchange_DPoPKeyMismatch`.
3. **Scope subset rule.** Health Card gets only a subset of Hub's scope; a wider request is `interaction_required`, not a
   silent grant. Tests: `_NarrowerScope`, `_ScopeSuperset`.
4. **`device_sso` rejected on exchange.** The scope is valid only at the code grant. Test: `TestE2E_Exchange_DeviceSSOInScope`.
5. **Consent presence cookie.** The consent-only page records an answer only with an `HttpOnly` cookie set during the user's
   own Hub login. Test: the subtest "the app cannot answer for the user" (403).
6. **Sign-out revocation.** Access tokens of the second app carry the login session id (`sid`); ending the session makes
   userinfo and introspection refuse them. Test: `TestE2E_Revoke_DeviceSecret`.
7. **Expired id_token, bound checks.** An expired id_token is accepted only together with its own device secret (`ds_hash`,
   `sid`), a live login session and a proof from Hub's key (Native SSO 6.3, decision D8). An operator can add an age cap
   (`id_token_max_age_seconds`, off by default). Tests: `TestE2E_Exchange_ExpiredIDToken`, unit `..._ExpiredIDTokenOutsideCap`.

## Say plainly (do not skip)

- After 8 hours from Hub's login (the device secret's lifetime) the exchange is refused. Hub cannot see this, so it still
  says "signed in"; the user signs out of Hub and in again.
- Tokens that a resource server checks only by signature, without asking eSignet, stay valid until they expire.
- The demo's runtime store is in memory: an eSignet restart ends every session. Several replicas need eSignet's Redis
  store, which the stack runs with `PS03_RUNTIME_STORE=redis` (the live suite passed against it).

## Cut list if the take runs over 5 minutes

Drop shot 9 first, then the second half of shot 5, then the "Not now" half of shot 6.
