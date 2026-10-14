# Demo script

How to run the Native SSO demo (Hub and Health Card on an Android emulator against the local eSignet stack) the same
way every time, and what to do when something fails. The video's shot list is [video-script.md](video-script.md).
Written 2026-10-05, updated 2026-10-14.

## Status of this runbook

- **Run on 2026-10-05, 2026-10-06 and 2026-10-14 with the emulator**: `bash scripts/demo/demo.sh --check` printed `demo ready` and
  `teardown ok` (row DEMO-FULL of [test-report.md](test-report.md), with its duration), and `docker ps` listed no
  container afterwards. The server part alone (`--no-emulator`) is row DEMO-SERVER.
- Every screen named below, including the "Not now" fallback, was seen on the emulator by the UI tests of
  2026-10-14 (`OK (6 tests)`); screenshots are in [screens/](screens/) and
  [ux.md](ux.md#screenshots). Screen texts in this file are the apps' strings.
- Not measured yet: the live runbook by a person at presenting speed. Rehearse once and fill the last row of
  [Timings](#timings).

## Prerequisites

The tools of [setup.md](setup.md#prerequisites), including the emulator image and the AVD `ps03_api34`, with
`JAVA_HOME` and `ANDROID_HOME` set and the ports of [setup.md](setup.md#ports) free.

## One command

```bash
bash scripts/demo/demo.sh --check     # prove everything, then tear down: prints "demo ready" then "teardown ok"
bash scripts/demo/demo.sh             # live demo: leaves the stack and a visible emulator running
bash scripts/demo/demo.sh --dry-run   # print the steps, change nothing
bash scripts/demo/demo.sh --no-emulator --check   # server part only
```

| Step | What it does | Proof line printed |
|---|---|---|
| 1 build | builds Hub and Health Card debug APKs, checks both carry one certificate | `family-signing: OK <sha-256>` |
| 2 stack | `deploy/stack.sh down`, `up`, `smoke`: eSignet with Thunder, Postgres, mock identity system, login pages, empty volumes | `stack smoke: PASS` |
| 3 server | runs 4 live e2e tests: Hub login with `device_sso`, token exchange for the Health Card, narrower scope, scope superset (`interaction_required`) | `e2e: 4 passed, 0 failed` |
| 4 device | starts the emulator, installs both apps, clears their data, registers both as clients with their own device keys, opens Hub | `device ready: Hub in front, 2 clients registered` |
| end | with `--check`: tears down | `demo ready`, then `teardown ok` |

- Other scripts: `bash scripts/demo/reset.sh` (clean stack and clean apps between takes, no rebuild; `--device`
  only resets the apps) and `bash scripts/demo/teardown.sh` (stack and emulator; afterwards no `ps03-` container
  runs and the emulator is gone).
- Order of heavy steps: APK build, then stack, then emulator (the build and the emulator compete for memory).
- The scripts never remove a lock directory. If `demo.sh` reports that the emulator lock exists, check that no
  other job uses the emulator, remove that directory yourself, and run again.

## Fixed inputs

| Item | Value |
|---|---|
| Issuer | `http://localhost:38088` (the emulator reaches it through `adb reverse`) |
| Seeded user, UIN | `1774231323` |
| OTP | `111111` |
| Client ids | derived from each app's device key: new after each reset, printed by `register-client.sh` |
| Relying party | `ps03-family` (both apps), so both get the same `sub` |
| Extra permission for re-consent | `phone` |
| Animations | off (set by the scripts); Chrome first-run screens skipped |

## Live runbook

Start state: Hub in front, signed out. Health Card installed, signed out.

1. **Hub, sign in.**
   - Show the Hub start screen: "Sign in once with eSignet. Your other apps from the same publisher ...". Turn the
     switch "Share this sign-in with my other apps" on (it is off at first start).
   - Tap "Sign in with eSignet". Chrome opens eSignet's page. Enter UIN `1774231323`, request the OTP, enter
     `111111`, tick consent when asked. Chrome returns to Hub.
   - Expect: "Signed in as" with the seeded user's name, "Sign-in shared with my other apps: Yes",
     "Apps using this sign-in: None yet".
2. **Health Card, no login.**
   - Open Health Card (launcher). Expect "Signing you in with Hub..." for a moment, then the home screen with name
     and email and the line "With Hub's sign-in. No password or OTP was needed." No browser opens.
   - Back in Hub, "Apps using this sign-in" now lists Health Card with a time.
3. **Re-consent.**
   - First without a login: in Health Card tap "Allow card sharing". The consent screen says no ID or OTP is
     needed. Tap "Continue to eSignet": eSignet's page "Allow more access?" names Health Card, Hub and the
     permission `health_card_share`. Tap "Allow". Back in Health Card: "Card sharing with clinics: Allowed", still
     "With Hub's sign-in".
   - Then with a login, for new user data: in Health Card tap "Add my phone number". The consent screen says "Health Card needs your permission" and that
     eSignet will ask for the ID and a new OTP (eSignet Go keeps no browser session; say this on camera).
   - Tap "Continue to eSignet", enter UIN and OTP again. Expect the phone number on the home screen and "With your
     own eSignet sign-in, after you gave the extra permission."
   - Fallback to show the decline path: "Not now" returns with "Nothing changed".
4. **Sign out of all apps.**
   - In Hub tap "Sign out of all apps". Hub shows "You are signed out of Hub and of every app that used this
     sign-in."; its DPoP key is deleted and eSignet ends the device secret's session (rule R27).
   - Open Health Card: it clears its tokens and shows "Sign in to Hub first" with "Open Hub".
5. **Health Card long after Hub's login.** Hub's id_token expires after 120 s, and Health Card still signs in silently: eSignet
   accepts an expired id_token while the device secret and its login session live (8 hours, decision D8). No id_token age cap
   is set by default, so the demo flow above works at any pace within those 8 hours.

## Timings

| Step | Measured |
|---|---|
| Preflight, APK build (up to date), signing check | 15 s on 2026-10-05 |
| Stack from clean volumes | 37 s on 2026-10-05 |
| Headless flow (4 tests) and teardown | 11 s on 2026-10-05 |
| Whole `--no-emulator` run | 65 s on 2026-10-05; current value in row DEMO-SERVER of [test-report.md](test-report.md) |
| Whole `demo.sh --check` (acceptance: under 10 minutes) | row DEMO-FULL of [test-report.md](test-report.md) |
| Live runbook by a person at presenting speed | not measured; rehearse once and fill this row |

## When something fails

| Symptom | Cause | Do |
|---|---|---|
| `the emulator lock ... exists` | an earlier job left the lock directory | check nobody uses the emulator, `rmdir` that directory yourself, run again |
| `stack smoke` fails or the stack does not start | ports in use, Docker out of memory | `bash scripts/demo/teardown.sh`, free the ports, raise the Docker memory to 4 GB or more |
| Chrome shows a first-run screen | the command-line file was not written | `bash scripts/demo/reset.sh --device` |
| eSignet page shows a captcha | the login page may ask for hCaptcha | tick the box; the UI test does this too |
| Health Card shows "Hub did not answer in time" | Hub was killed in the background | open Hub once, then "Check again" |
| Health Card shows "Hub refused this app" | the two APKs were signed with different certificates | `bash android/scripts/verify-family-signing.sh`; rebuild both |
| Emulator too slow on camera | software rendering | record the screen from a phone or show the UI-test screenshots in `docs/ux.md`; the server part stands on its own |
| "Pixel Launcher isn't responding" over the app | the emulator is short of CPU | tap "Wait", close other heavy jobs; `adb shell am broadcast -a android.intent.action.CLOSE_SYSTEM_DIALOGS` |
| Whole device part fails on the day | anything | run `bash scripts/demo/demo.sh --no-emulator --check` and the Postman folder "Native SSO" (`docs/api.md`), narrate over the diagrams |
