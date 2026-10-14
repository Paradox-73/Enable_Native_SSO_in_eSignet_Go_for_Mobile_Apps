# Project status

Every work package (WP) of the project, its state and its commits. All of them are DONE. Dated results of every
check are in [test-report.md](test-report.md); this file does not repeat them.

Each task once had a card with its goal, acceptance check, measured results and notes, written while the work was
open. Those cards are history now. Read them with `git show d106024:docs/STATUS.md`.

## Snapshot

| Item | Value |
|---|---|
| Updated | 2026-10-14, after WP-21 |
| `main` | all work is on `main`; [GitHub](https://github.com/Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps) gets it at each push |
| Unfinished work | none in code; the items only a person can do are in [submission-checklist.md](submission-checklist.md) |
| Deadline | 2026-10-26 23:59 IST (MOSIP Decode 2026, problem statement 03, team Badmosip) |

## All tasks

Lanes own folders: `platform` (root files, `scripts/`, `deploy/`, `e2e/`, `docs/`, `.github/`), `thunder`,
`esignet` and `android`. Statement tasks are T1 to T10, with B1 for the bonus.

| Task | Lane | Title | Status | Commits |
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
- **WP-15, Hub's rate limit.** Hub allows 30 calls per caller in 60 s. One Health Card launch costs 4 calls (version,
  bundle, two proofs) because the silent sign-in reuses the connection that read the share. The UI tests wait 61 s after
  the consent-only test so the next test is not refused.
