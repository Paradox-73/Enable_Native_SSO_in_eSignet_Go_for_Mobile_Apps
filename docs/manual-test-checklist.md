# Manual test checklist before recording

Steps a teammate follows on an emulator or a real Android phone, once, before the video is recorded. Each step has the
command and the screen to expect. The inputs are fixed: UIN `1774231323`, OTP `111111`, issuer `http://localhost:38088`.
All commands run in Git Bash (or any bash 4.4 or newer) from the repository root. Setup details: [setup.md](setup.md).

Tick each box. A step that does not match: note the screen (screenshot) and see [demo-script.md](demo-script.md#when-something-fails).

## A. Machine

- [ ] 1. Docker is running with at least 4 GB; `docker info` prints no error.
- [ ] 2. `echo $JAVA_HOME $ANDROID_HOME` shows a JDK 17 or newer and an Android SDK with platform 34. On the team laptop:
      `source /e/Projects/MOSIP/toolchain/env.sh && use_java21`.
- [ ] 3. `bash scripts/repo-lint.sh` prints `repo-lint: OK`.
- [ ] 4. Ports 38088, 38300, 38082 and 38455 are free: `docker ps` lists no `ps03-` container.

## B. Build and install

- [ ] 5. `( cd android && bash gradlew --no-daemon assembleDebug )` ends with `BUILD SUCCESSFUL`.
- [ ] 6. `bash android/scripts/verify-family-signing.sh debug` prints `family-signing: OK <sha-256>`: both apps carry one certificate.
- [ ] 7. **Emulator**: `bash scripts/demo/demo.sh` builds, starts the stack and a visible emulator, installs both apps, registers
      both with eSignet and opens Hub. It prints `demo ready`. Skip to step 11.
- [ ] 8. **Real phone** (USB debugging on, `adb devices` lists it): start the stack with `PS03_KEEP_STACK=1 bash scripts/with-stack.sh true`,
      then forward the ports so the phone sees the issuer as `http://localhost:38088`:
      `adb reverse tcp:38088 tcp:38088 && adb reverse tcp:38300 tcp:38300`.
- [ ] 9. Real phone: `adb install -r android/superapp/build/outputs/apk/debug/superapp-debug.apk` and
      `adb install -r android/childapp/build/outputs/apk/debug/childapp-debug.apk`. If Android answers
      `INSTALL_FAILED_UPDATE_INCOMPATIBLE`, uninstall the older app first.
- [ ] 10. Real phone: `bash android/scripts/register-client.sh superapp` then `bash android/scripts/register-client.sh childapp`; each
      prints `register-client: <client_id> ACTIVE`. (Physical phone: not run by the team so far; limit L09. Chrome must be the default browser.)

## C. Flows (expected screens)

- [ ] 11. **Hub, signed out.** Hub shows "Sign in once with eSignet..." and the switch "Share this sign-in with my other apps". Turn it on.
- [ ] 12. **Hub login.** Tap "Sign in with eSignet". Chrome opens eSignet's page "Hub is requesting authentication". Enter `1774231323`,
      tick the captcha box if shown, "Get OTP", enter `111111`, "Verify", then "Allow" on the consent page. Hub returns with
      "Signed in as" the seeded user's name, "Sign-in shared with my other apps: Yes", "Apps using this sign-in: None yet".
- [ ] 13. **Silent sign-in.** Open Health Card. It shows "Signing you in with Hub..." for a moment, then its home with the name, the
      email and "With Hub's sign-in. No password or OTP was needed." No browser opens. Back in Hub, "Apps using this sign-in" lists Health Card.
- [ ] 14. **Consent only, Not now.** In Health Card tap "Allow card sharing". The consent screen says no ID or OTP is needed. Tap
      "Continue to eSignet". Chrome shows eSignet's page "Allow more access?" with no login field. Tap "Not now". Health Card shows
      "You did not give the extra permission. Nothing changed." and "Card sharing with clinics: Not allowed".
- [ ] 15. **Consent only, Allow.** Repeat step 14 and tap "Allow". Health Card shows "Card sharing with clinics: Allowed" and still
      "With Hub's sign-in".
- [ ] 16. **Consent with a login.** Tap "Add my phone number". The consent screen says eSignet will ask for the ID and a new OTP. Tap
      "Continue to eSignet", enter the UIN and OTP again. Health Card shows the phone number and "With your own eSignet sign-in, after
      you gave the extra permission."
- [ ] 17. **Sign out of all apps.** In Hub tap "Sign out of all apps": "You are signed out of Hub and of every app that used this
      sign-in." Open Health Card: "Sign in to Hub first" with "Open Hub".
- [ ] 18. **Expired id_token** (optional, takes over an hour). Sign in to Hub, wait 61 minutes, clear Health Card's data (Settings, Apps, Health Card, Storage, Clear data), open
      Health Card. Expected: Health Card signs in silently. eSignet accepts Hub's expired id_token while the device secret lives (8 hours; no age cap by default).

## D. Server checks (headless, in the terminal)

- [ ] 19. `bash scripts/with-stack.sh bash e2e/run.sh` ends with `e2e: 32 passed, 0 failed`.
- [ ] 20. `bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh` (needs newman 6) reports 0 failed.
- [ ] 21. `bash make.sh docs-check` ends with `0 broken links; tasks traced 11/11; threats with mitigation and test 12/12`.

## E. Cleanup

- [ ] 22. `bash scripts/demo/teardown.sh` (stack and emulator); then `docker ps` lists no `ps03-` container.
      On a real phone also: `adb reverse --remove-all`.
