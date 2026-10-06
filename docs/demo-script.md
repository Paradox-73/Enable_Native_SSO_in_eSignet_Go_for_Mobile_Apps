# Demo script and video storyboard

How to run the Native SSO demo (Hub and Health Card on an Android emulator against the local eSignet stack) the same
way every time, what to say and show for each of the 4 minutes 30 seconds of the video, and what to do when
something fails. Written 2026-10-05.

## Status of this runbook

- **Run on 2026-10-05 and again on 2026-10-06 with the emulator**: `bash scripts/demo/demo.sh --check` printed `demo ready` and
  `teardown ok` (row DEMO-FULL of [test-report.md](test-report.md), with its duration), and `docker ps` listed no
  container afterwards. The server part alone (`--no-emulator`) is row DEMO-SERVER.
- Every screen named below, including the "Not now" fallback, was seen on the emulator by the UI tests of
  2026-10-06 (`OK (5 tests)`); screenshots are in [screens/](screens/) and
  [ux.md](ux.md#screenshots). Screen texts in this file are the apps' strings.
- Not measured yet: the live runbook by a person at presenting speed. Rehearse once and fill the last column of
  [Timings](#timings).

## Prerequisites

- Windows 11 with Git Bash and Docker Desktop (Linux containers, at least 4 GB), JDK 17 or newer, Android SDK with
  platform 34, build-tools 34.0.0, `emulator` and the system image `system-images;android-34;google_apis;x86_64`,
  and the AVD `ps03_api34`. Details: [../CONTRIBUTING.md](../CONTRIBUTING.md).
- `JAVA_HOME` and `ANDROID_HOME` set in the shell.
- Free ports 38088, 38300, 38082, 38455 and the emulator ports 5584/5585.

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
5. **Health Card after a long wait** (rehearse once, more than 2 minutes after the Hub login): sign in to Hub
   again, wait, open Health Card. It still signs in without a browser: the expired id_token is accepted while the
   device secret lives (the device secret lives 8 hours by default).

## Video storyboard: 4 min 30 s

Narration is written to be read at a calm pace. Times are planned, not measured.

| Time | Screen | Say | Show |
|---|---|---|---|
| 0:00-0:20 | Title card with the two app names | The problem: three apps from one publisher make the user sign in three times. Native SSO signs in once. | Slide: problem statement 03, team Badmosip |
| 0:20-0:50 | Diagram `docs/diagrams/user-flow.png` | One sign-in in Hub; Hub holds a device secret; Health Card trades it for its own tokens at the token endpoint. | The user-flow diagram |
| 0:50-1:30 | Hub: tap Sign in, Chrome login | Hub asks for the extra scope `device_sso`. eSignet answers with a device secret next to the tokens. The tokens are bound to Hub's key with DPoP. | Hub home, Chrome login with UIN and OTP, "Signed in as" |
| 1:30-2:20 | Health Card opens | Health Card binds to Hub's service, which only answers apps signed with the same certificate. It sends the id_token and device secret to eSignet with its own client key. No browser, no OTP. | Health Card home, "With Hub's sign-in" line; Hub's "Apps using this sign-in" |
| 2:20-3:10 | Re-consent | Health Card asks for a permission Hub's login did not include. eSignet answers `interaction_required` with a consent page: one tap, no login. For the phone number, which needs new user data, the user logs in once more. | Consent-only page, "Allowed"; consent screen, Chrome login, phone number on screen |
| 3:10-3:40 | Sign out of all apps | Hub deletes its key and asks eSignet to end the device secret's session. No family app can sign in with it any more. | Hub sign out, Health Card "Sign in to Hub first" |
| 3:40-4:10 | Terminal | The same rules are tested live: 31 of 31 end-to-end tests, 34 Postman requests with 125 assertions. | `e2e: 31 passed, 0 failed`; newman summary |
| 4:10-4:30 | Closing slide | Thunder and eSignet changes, API docs, Postman collection, design doc, both apps. | Repository link, README |

## Timings

| Step | Measured on 2026-10-05 | Emulator run (to fill in at the first rehearsal) |
|---|---|---|
| Preflight, APK build (up to date), signing check | 15 s | |
| Stack from clean volumes | 37 s | |
| Headless flow (4 tests) and teardown | 11 s | |
| Whole `--no-emulator` run | 65 s | |
| Emulator boot | not run | |
| Install, clear, register both apps | not run | |
| Whole `demo.sh --check` | not run (acceptance: under 10 minutes) | |

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
