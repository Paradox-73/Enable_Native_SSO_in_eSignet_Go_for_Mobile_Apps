# Manual test results

Copy this file, fill it in and send it back. One person fills in one copy. Steps are in
[manual-test-checklist.md](manual-test-checklist.md). Use the step id (`MT01`, `MT02`, ...) to find a step.

- Do the steps in order. Leave a row blank when you did not run the step. Write `skipped` and the reason in notes when you
  chose not to.
- Pass or fail: write `pass` or `fail`. Write what you saw in notes when it differs from the expected result.
- Time taken: minutes and seconds of your own stopwatch, or the time the command printed.
- Screenshot or log filename: save screenshots and terminal output in a folder next to this file and write the file names.
- For MT01 to MT04 repeat the row once per operating system, and write the OS in the OS/device column.
- Send it back in one of two ways. Commit it as `docs/manual-test-results/<date>-<name>.md`, for example
  `docs/manual-test-results/2026-10-15-kanav.md`. Or send the file to the team.

## Header

| Field | Value |
|---|---|
| Tester | |
| Date | |
| Commit tested (`git rev-parse --short HEAD`) | |
| Operating system and version | |
| Device or emulator (model, Android version, AVD name) | |
| Docker memory setting | |
| JDK version and Android SDK path | |

## A. Fresh clone and tools

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT01 | Clone and check the repository | | | | | | |
| MT02 | Check the bash version | | | | | | |
| MT03 | Check Docker and the free ports | | | | | | |
| MT04 | Check the JDK and the Android SDK | | | | | | |

## B. Emulator and build

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT05 | Create the emulator on an Intel or AMD computer (macOS Intel, Linux, Windows) | | | | | | |
| MT06 | Create the emulator on Apple silicon (M1 or newer) | | | | | | |
| MT07 | Build both apps | | | | | | |
| MT08 | Check that both apps share one signing certificate | | | | | | |
| MT09 | Dry run of the demo script | | | | | | |
| MT10 | Start the demo on the emulator | | | | | | |

## C. App walkthrough

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT11 | Hub, signed out | | | | | | |
| MT12 | Hub login | | | | | | |
| MT13 | Silent sign-in | | | | | | |
| MT14 | Consent only, Not now | | | | | | |
| MT15 | Consent only, Allow | | | | | | |
| MT16 | Consent with a login | | | | | | |
| MT17 | Sign out of all apps | | | | | | |
| MT18 | Expired id_token | | | | | | |

## D. Server checks

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT19 | Live end-to-end tests | | | | | | |
| MT20 | Postman folder under newman | | | | | | |
| MT21 | Documentation check | | | | | | |

## E. Real phone

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT22 | Prepare the phone | | | | | | |
| MT23 | Start the stack and forward the ports | | | | | | |
| MT24 | Install both apps | | | | | | |
| MT25 | Register both apps with eSignet | | | | | | |
| MT26 | Hub sign-in on the phone | | | | | | |
| MT27 | Health Card silent sign-in on the phone | | | | | | |

## F. Hardware-backed key check

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT28 | Record the phone | | | | | | |
| MT29 | Read the key log line | | | | | | |

## G. TalkBack pass

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT30 | Turn TalkBack on | | | | | | |
| MT31 | Hub pass | | | | | | |
| MT32 | Health Card pass | | | | | | |
| MT33 | 200 % font size | | | | | | |
| MT34 | Restore the phone | | | | | | |

## H. Captcha and internet

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT35 | Check internet before any demo | | | | | | |
| MT36 | Offline behaviour | | | | | | |

## I. Video dry run

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT37 | Set up the recording | | | | | | |
| MT38 | Scenes 1 and 2 (planned 0:00 to 0:45) | | | | | | |
| MT39 | Scenes 3 and 4 (planned 0:45 to 1:55) | | | | | | |
| MT40 | Scenes 5 to 7 (planned 1:55 to 3:55) | | | | | | |
| MT41 | Scenes 8 to 10 (planned 3:55 to 4:45) | | | | | | |
| MT42 | Watch the take back | | | | | | |

## J. Cleanup

| id | step | tester | OS/device | pass/fail | time taken | notes | screenshot or log filename |
|---|---|---|---|---|---|---|---|
| MT43 | Tear down | | | | | | |
