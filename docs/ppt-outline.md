# Slide deck outline

Outline of the pitch deck (the PPT deliverable). The submission guidelines ask for the problem, the solution, the
technology stack, the target audience and the potential impact; the evaluation weights are functionality 40 %,
quality of deliverable 40 %, user experience and design 10 %, presentation 10 %. Slide times add up to 8 minutes
and are planned, not measured. The slide file itself is not in the repository: the team builds it from this outline
and keeps the numbers in step with [test-report.md](test-report.md). Written 2026-10-05.

Every number on a slide must come from a dated line in the test report. Anything not run is shown as "not run yet", not
left out.

| # | Slide | Time | Content | Source |
|---|---|---|---|---|
| 1 | Title | 0:15 | Native SSO for mobile apps in eSignet Go; team Badmosip; problem statement 03 | - |
| 2 | The problem | 0:45 | A citizen with three apps from one agency signs in with an OTP three times; each OTP costs an SMS, a delay and a phishing chance. The apps cannot share a browser session, and eSignet Go had no way to share a sign-in between native apps | [design.md](design.md) section 1 |
| 3 | The idea | 0:45 | Sign in once; the first app (Hub) receives a device secret; the second app (Health Card) trades it at the token endpoint for its own tokens, no browser, no OTP. Standard: OpenID Connect Native SSO for Mobile Apps 1.0 with OAuth 2.0 Token Exchange (RFC 8693) and DPoP (RFC 9449) | [diagrams/user-flow.png](diagrams/user-flow.png) |
| 4 | Architecture | 1:00 | Components: Hub, Health Card, eSignet Go, Thunder ID engine inside it, Device Secret Provider, mock identity system. Who owns what: Thunder owns the protocol, eSignet owns policy and keys | [diagrams/components.png](diagrams/components.png) |
| 5 | Flow | 1:00 | Login with `device_sso` -> device secret -> app-to-app hand-over -> token exchange -> second app signed in. Re-consent when more scope is needed: one tap on eSignet's page with no login for a permission, a fresh login only for new user data | [seq-super-app-login](diagrams/seq-super-app-login.png), [seq-child-app-sso](diagrams/seq-child-app-sso.png), [seq-consent-only](diagrams/seq-consent-only.png), [seq-reconsent](diagrams/seq-reconsent.png) |
| 6 | What we built | 1:00 | Thunder: scope checks, `sid`, `ds_hash`, device secret at the code grant, exchange with an ordered validation chain (client, request shape, audience, scope, DPoP, device secret, id_token, session, key), scope rules, consent-only re-consent page, discovery, device secret revocation at sign-out. eSignet: the provider (signed then encrypted JWT; its `claims` field lists only the names of the released claims, the values stay on the server, so a leaked device secret reveals nothing about the user), settings, client registration, userinfo filter. Android: Hub, Health Card, shared libraries. All tasks T1-T10 and bonus B1 | [README.md](../README.md) task table, [changes.md](changes.md) |
| 7 | Security | 0:45 | Device secret bound to Hub's DPoP key; Hub signs proofs but never hands over its key; the hand-over is a bound service, not an intent, because a service lets Hub check the caller's signing certificate on every call and keep answering (proofs for each API call); family permission is signature-level and checked on every call; exchange checks `ds_hash`, `sid`, client rights, same relying party, replayed proofs; consent page with one-time tickets that grant nothing without the device secret and Hub's key; 12 threats with mitigation and test | [threat-model.md](threat-model.md), [exchange-validation.png](diagrams/exchange-validation.png) |
| 8 | Demo | 1:00 | The video (4 min 30 s) or a live run; fallback is the server demo and the screens of the UI tests | [demo-script.md](demo-script.md) |
| 9 | Evidence | 0:45 | 31 of 31 live tests; 34 Postman requests, 125 assertions, 0 failed; 2434 Thunder and 1593 eSignet unit tests, 0 failed; changed-line coverage 98.8 % and 92.3 %; 207 Android JVM tests; 6 emulator UI tests and the family tests passed (2026-10-06); GitHub Actions green on every job (run of 2026-10-06 before the consent-only change). **Not run yet**: TalkBack, a physical phone | [test-report.md](test-report.md) |
| 10 | User experience | 0:30 | Three screens that matter: Hub signed in, Health Card silent sign-in, eSignet's consent page with no login, and the consent screen with its honest OTP message for new user data (files in `docs/screens/`); both apps share one layout and palette with their own icons; accessibility checks (contrast 4.5:1, 200 % font, 48 dp targets) | [ux.md](ux.md) |
| 11 | Impact and audience | 0:30 | Government and public-sector app suites that use MOSIP eSignet; fewer OTPs; a reusable provider interface any eSignet deployment can implement; changes written to be offered upstream to Thunder and eSignet | [upstream-prs/](upstream-prs/thunder.md) |
| 12 | Limits and next steps | 0:30 | Re-consent with no login works for permissions; new user data (the phone number) still needs a fresh login with OTP, because eSignet's identity system releases data only after an authentication; the consent page shows the scope value, not a friendly text; sign-out ends the device secret on the server, but access tokens already issued live until they expire; consent approvals are audit log lines, not eSignet consent records; not run: TalkBack, a physical phone; future work: revoking issued tokens too, device attestation, iOS | [design.md](design.md) section 21, test report limits |

## Speaker notes

- Slide 2: say one concrete number only if the team can source it; do not quote OTP cost figures from memory.
- Slide 6: show the three folders with their counts from `git diff --stat c4c5443 -- thunder/` and
  `git diff --stat 4eac1cc -- esignet/` run on the day, so the numbers are current.
- Slide 9: read the "not run" line aloud; judges see it in the report anyway.
- Slide 8: record the video after a fresh `bash scripts/demo/demo.sh --check` on the day; the storyboard is in the demo script.

## Checklist before the deck is final

- [ ] Every figure matches the latest `docs/test-report.md`.
- [ ] Screens on slide 10 are the emulator screenshots in `docs/screens/` (taken 2026-10-06 by the UI tests).
- [ ] Team names and roles added; no logo or name of an organisation that has not agreed.
- [ ] Deck exported as PDF as well as PPT, stored with the other deliverables ([submission-checklist.md](submission-checklist.md)).
