# Manual test checklist

What only a person can check: a fresh clone on each operating system, the emulator and a real phone, the Android hardware
key store, a TalkBack pass, the captcha on the login page, and a video dry run. Every step has a stable id (`MT01`, `MT02`,
...) so the results can name it. Write your results in [manual-test-results-template.md](manual-test-results-template.md).

- Inputs are fixed. UIN `1774231323`, OTP `111111`, issuer `http://localhost:38088`. Test data: [test-data.md](test-data.md).
- All commands run in a bash shell from the repository root. On Windows that is Git Bash. Do not use CMD or PowerShell.
- A step that does not match: take a screenshot, save the terminal output, and see
  [demo-script.md](demo-script.md#when-something-fails).
- Setup details and the full tool list: [setup.md](setup.md). Known limits: [test-report.md](test-report.md#known-limits).
- Steps MT11 to MT18 are the app walkthrough. The other sections prepare for it or add checks around it.

| Section | Steps | Needs |
|---|---|---|
| A. Fresh clone and tools | MT01 to MT04 | each of macOS, Linux and Windows |
| B. Emulator and build | MT05 to MT10 | one machine with the Android SDK |
| C. App walkthrough | MT11 to MT18 | the emulator from MT10 |
| D. Server checks | MT19 to MT21 | Docker only |
| E. Real phone | MT22 to MT27 | an Android phone and a USB cable |
| F. Hardware-backed key check | MT28 to MT29 | the phone from section E |
| G. TalkBack pass | MT30 to MT34 | the phone from section E |
| H. Captcha and internet | MT35 to MT36 | the emulator or the phone |
| I. Video dry run | MT37 to MT42 | the emulator from MT10 |
| J. Cleanup | MT43 | any |

## A. Fresh clone and tools

Do MT01 to MT04 once on each of macOS, Linux and Windows. Use a new folder each time. Write the OS in the results.

**MT01. Clone and check the repository.** The repository is public; no GitHub login is needed.

```bash
git clone -c core.longpaths=true https://github.com/Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps.git esignet-native-sso
cd esignet-native-sso
bash scripts/install-hooks.sh
bash scripts/repo-lint.sh
bash scripts/go.sh . version
```

- `core.longpaths=true` matters on Windows. Some upstream test files have very long paths.
- Expect `repo-lint: OK`. Expect `go version go1.26.x linux/<arch>` from the last command. The first run pulls a Docker image
  and takes longer. Record the time of each command.
- On Windows also run `git config core.longpaths`. Expect `true`.

**MT02. Check the bash version.** Run `bash --version | head -1`. Expect version 4.4 or newer.

- Linux and Git Bash on Windows ship bash 5.x.
- macOS: `/bin/bash` is version 3.2 and is too old. First run `/bin/bash scripts/repo-lint.sh` and record what it prints.
  Expect it to stop with exit status 2. Then run `brew install bash`, open a new terminal, and make sure `bash --version`
  shows 4.4 or newer and `which bash` is the Homebrew one (for example `/opt/homebrew/bin/bash` on Apple silicon). If it is
  not, run `export PATH="/opt/homebrew/bin:$PATH"` (`/usr/local/bin` on an Intel Mac). Run `bash scripts/repo-lint.sh` again.
  Expect `repo-lint: OK`.

**MT03. Check Docker and the free ports.**

1. Set the Docker memory limit to 4 GB or more (Docker Desktop, Settings, Resources). On Linux use Docker Engine with the
   compose plugin.
2. Run `docker info | grep -i "total memory"`. Expect a value that matches your setting.
3. Run `docker ps`. Expect no container whose name starts with `ps03-`.
4. Ports 38088, 38300, 38082 and 38455 must be free. A program that uses one of them stops the stack in MT10 and MT19.

**MT04. Check the JDK and the Android SDK.**

```bash
export JAVA_HOME=/path/to/jdk-17-or-newer       # Git Bash form of a Windows path: /c/Program Files/...
export ANDROID_HOME=/path/to/Android/Sdk
"$JAVA_HOME/bin/java" -version
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "platforms;android-34" "build-tools;34.0.0" "platform-tools"
ls "$ANDROID_HOME/platforms" "$ANDROID_HOME/build-tools"
```

- On Windows the tool is `sdkmanager.bat` in the same folder.
- Expect Java 17 or newer, `android-34` under `platforms` and `34.0.0` under `build-tools`.
- If `cmdline-tools/latest` does not exist, install "Android SDK Command-line Tools" from Android Studio (SDK Manager, SDK Tools).

## B. Emulator and build

Do MT05 or MT06 once, on the machine you will record on. Use one AVD name on every machine: `ps03_api34` (Android 34, Google
APIs). The scripts read that name.

**MT05. Create the emulator on an Intel or AMD computer (macOS Intel, Linux, Windows).**

```bash
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "emulator" "system-images;android-34;google_apis;x86_64"
"$ANDROID_HOME/cmdline-tools/latest/bin/avdmanager" create avd -n ps03_api34 -k "system-images;android-34;google_apis;x86_64"
"$ANDROID_HOME/emulator/emulator" -list-avds
```

- On Windows use `avdmanager.bat` and `emulator.exe` if the plain names are not found.
- Expect the last command to print `ps03_api34`.

**MT06. Create the emulator on Apple silicon (M1 or newer).** Same AVD name, the `arm64-v8a` image.

```bash
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "emulator" "system-images;android-34;google_apis;arm64-v8a"
"$ANDROID_HOME/cmdline-tools/latest/bin/avdmanager" create avd -n ps03_api34 -k "system-images;android-34;google_apis;arm64-v8a"
"$ANDROID_HOME/emulator/emulator" -list-avds
```

- Expect `ps03_api34` in the list. The team has not run the emulator on Apple silicon, so record anything that fails to boot.

**MT07. Build both apps.**

```bash
( cd android && bash gradlew --no-daemon assembleDebug )
```

- Expect `BUILD SUCCESSFUL`. Record the time. The first build downloads Gradle 8.7 and creates a test signing key in
  `$PS03_RUN_DIR/keys/`. Every machine gets its own key. That is expected.
- The APKs are `android/superapp/build/outputs/apk/debug/superapp-debug.apk` (Hub) and
  `android/childapp/build/outputs/apk/debug/childapp-debug.apk` (Health Card).

**MT08. Check that both apps share one signing certificate.**

```bash
bash android/scripts/verify-family-signing.sh debug
```

- Expect `family-signing: OK <sha-256>`. Both apps carry one certificate.

**MT09. Dry run of the demo script.**

```bash
bash scripts/demo/demo.sh --dry-run
```

- Expect the list of steps with `would run:` lines, then `demo dry-run: OK`. Nothing is built or started.

**MT10. Start the demo on the emulator.**

```bash
bash scripts/demo/demo.sh
```

- It builds (add `--skip-build` to reuse the APKs of MT07), starts the stack, starts a visible emulator, installs both apps,
  registers both with eSignet and opens Hub.
- Expect these lines on the way: `family-signing: OK`, `stack smoke: PASS`, `device ready: Hub in front, 2 clients registered`,
  then `demo ready`. Leave the window open. The stack and the emulator stay up until you press Enter.
- If it stops with "the emulator lock ... exists", read the message. The script never removes the lock itself.

## C. App walkthrough

Start state: Hub in front, signed out, Health Card installed and signed out. Run these on the emulator. On the phone, MT26
and MT27 repeat the first two.

**MT11. Hub, signed out.** Hub shows "Sign in once with eSignet..." and the switch "Share this sign-in with my other apps".
Turn the switch on.

**MT12. Hub login.** Tap "Sign in with eSignet". Chrome opens eSignet's page "Hub is requesting authentication". Enter
`1774231323`, tick the captcha box if shown, tap "Get OTP", enter `111111`, tap "Verify". On the consent page turn on the
optional attributes switch (the email; it starts off), then tap "Allow". Expect Hub to return with "Signed in as" and the seeded user's name, "Sign-in shared with my other apps: Yes" and
"Apps using this sign-in: None yet".

**MT13. Silent sign-in.** Open Health Card. Expect "Signing you in with Hub..." for a moment, then its home screen with the
name, the email (`siddhartha.km@gmail.com`; optional in Hub's login, so it shows "Not shared with Health Card" if the
switch stayed off in MT12) and "With Hub's sign-in. No password or OTP was needed." No browser opens. Back in Hub, "Apps using this
sign-in" lists Health Card.

**MT14. Consent only, Not now.** In Health Card tap "Allow card sharing". The consent screen says no ID or OTP is needed.
Tap "Continue to eSignet". Chrome shows eSignet's page "Allow more access?" with no login field. Tap "Not now". Expect
Health Card to show "You did not give the extra permission. Nothing changed." and "Card sharing with clinics: Not allowed".

**MT15. Consent only, Allow.** Repeat MT14 and tap "Allow". Expect "Card sharing with clinics: Allowed" and still "With Hub's
sign-in".

**MT16. Consent with a login.** Tap "Add my phone number". The consent screen says eSignet will ask for the ID and a new OTP.
Tap "Continue to eSignet" and enter the UIN and the OTP again. Expect Health Card to show the phone number and "With your own
eSignet sign-in, after you gave the extra permission."

**MT17. Sign out of all apps.** In Hub tap "Sign out of all apps". Expect "You are signed out of Hub and of every app that used
this sign-in." Open Health Card. Expect "Sign in to Hub first" with "Open Hub".

**MT18. Expired id_token.** Optional, takes over an hour. The id_token age cap is off by default, so eSignet accepts an expired
id_token while Hub's device secret (8 hours) and login session live.

1. Sign in to Hub with sharing on (MT11 and MT12). Keep the stack and the device running.
2. Wait 61 minutes.
3. Clear Health Card's data (Settings, Apps, Health Card, Storage, Clear data). The app then makes a new key, so register it
   again: `bash android/scripts/register-client.sh childapp`. Expect `register-client: <client_id> ACTIVE`.
4. Open Health Card. Expect it to sign in silently, as in MT13, with no browser.

## D. Server checks

Headless checks in the terminal. They need Docker only.

**MT19. Live end-to-end tests.** Run `bash scripts/with-stack.sh bash e2e/run.sh`. Expect the last line
`e2e: 32 passed, 0 failed`. The run takes several minutes. It waits out a real 120 s id_token lifetime.

**MT20. Postman folder under newman.** Needs newman 6 (`npm install -g newman@6`). Run
`bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh`. Expect 0 failed.

**MT21. Documentation check.** Run `bash make.sh docs-check`. Expect the line
`docs-check: 0 broken links; tasks traced 11/11; threats with mitigation and test 12/12`.

## E. Real phone

Closes limit L09 of the test report. Use an Android 9 or newer phone (`minSdk` is 28).

**MT22. Prepare the phone.** Turn on Developer options and USB debugging. Connect the cable and accept the prompt on the
phone. Put `adb` on your path with `export PATH="$ANDROID_HOME/platform-tools:$PATH"`. Run `adb devices`. Expect the phone
listed as `device`, not `unauthorized`. Chrome must be the default browser. Open Chrome once and finish its first-run
screens. Close the emulator, or run `export ANDROID_SERIAL=<serial from adb devices>`, so every command goes to the phone.

**MT23. Start the stack and forward the ports.** The phone must see the issuer as `http://localhost:38088`. Do not use
`10.0.2.2` or the laptop's address.

```bash
PS03_KEEP_STACK=1 bash scripts/with-stack.sh true
adb reverse tcp:38088 tcp:38088
adb reverse tcp:38300 tcp:38300
adb reverse --list
```

- Expect the stack to start, then the list to show both ports. `adb reverse` works only while the cable is connected.

**MT24. Install both apps.**

```bash
adb install -r android/superapp/build/outputs/apk/debug/superapp-debug.apk
adb install -r android/childapp/build/outputs/apk/debug/childapp-debug.apk
```

- Expect `Success` twice. If Android answers `INSTALL_FAILED_UPDATE_INCOMPATIBLE`, run `adb uninstall
  org.badmosip.nativesso.superapp` (and `org.badmosip.nativesso.childapp`) and install again.

**MT25. Register both apps with eSignet.**

```bash
bash android/scripts/register-client.sh superapp
bash android/scripts/register-client.sh childapp
```

- Expect `register-client: <client_id> ACTIVE` for each. The script starts the app once and reads its public key. The private
  key stays in the phone's Keystore.

**MT26. Hub sign-in on the phone.** Do MT11 and MT12 on the phone. Record the time from the tap on "Sign in with eSignet" to Hub
showing "Signed in as".

**MT27. Health Card silent sign-in on the phone.** Do MT13 on the phone. Expect no browser and the line "With Hub's sign-in.
No password or OTP was needed." Optionally do MT14 to MT17 as well and note the result per step.

## F. Hardware-backed key check

Hub makes two signing keys in the Android Keystore: the DPoP key and the client key. The code asks for StrongBox first and
falls back when the phone has none (`android/superapp/src/main/java/org/badmosip/nativesso/superapp/keys/AndroidKeystoreKeys.kt`).
It logs which one it got, in debug builds only, once, when it creates the key. Health Card's keys follow the same rule
(`CardKeys.kt`) but log no line.

**MT28. Record the phone.** Run these and copy the output into your results.

```bash
adb shell getprop ro.product.model
adb shell getprop ro.build.version.release
adb shell pm list features | grep -i strongbox
```

- A line that contains `strongbox` means the phone has a StrongBox chip. No line means it has none, so the keys sit in the
  TEE (trusted execution environment) at best.

**MT29. Read the key log line.** The line appears only when the key is created, so start from a fresh install.

```bash
adb uninstall org.badmosip.nativesso.superapp
adb install -r android/superapp/build/outputs/apk/debug/superapp-debug.apk
adb logcat -c
bash android/scripts/register-client.sh superapp
```

Then tap "Sign in with eSignet" in Hub and sign in as in MT12 (this also creates the DPoP key). Then run:

```bash
adb logcat -d -s PS03Hub:I
```

- Expect a line `client key: ...` and a line `DPoP key: ...`. The value is one of three words.
  - `STRONGBOX`: the key is in the StrongBox chip. This should match a `strongbox` line in MT28.
  - `TEE_OR_SOFTWARE`: StrongBox was not available. On a real phone this normally means the TEE. The app cannot tell the TEE
    from software. On the emulator it is software (limit L01).
  - `EXISTING`: the key was made earlier, so nothing is reported. Uninstall, install again and repeat.
- No other command in this repository reports the security level. The result of this step is the device model, the Android
  version, the MT28 feature line and the word from the log.
- Uninstalling Hub deleted its keys, so Hub has a new client_id. The `register-client.sh` call above registered it. Health Card
  is not affected.

## G. TalkBack pass

Closes limit L08 of the test report. Use the phone. If you have no phone, use the emulator when its Settings, Accessibility
list TalkBack. Record the device in the results. TalkBack basics: swipe right for the next item, swipe left for the
previous, double-tap to activate.

**MT30. Turn TalkBack on.** Settings, Accessibility, TalkBack, On. Do the short tutorial if it starts. Keep the stack and
the port forwarding from section E running, with Hub signed out.

**MT31. Hub pass.** Open Hub and swipe through every item, then repeat on each screen below. For each item write down: what
TalkBack says, and whether it says what the item is (heading, button, switch with its on or off state). Record every item
that is not announced, has no label, is announced twice or is skipped. Record the reading order if it differs from the
visual order.

1. Hub signed out: the title, the text "Sign in once with eSignet...", the switch "Share this sign-in with my other apps",
   the button "Sign in with eSignet".
2. The eSignet sign-in in Chrome (eSignet's own page, so note problems but they are not Hub's).
3. Hub signed in: "Signed in as", "Sign-in shared with my other apps: Yes", "Apps using this sign-in" and the button
   "Sign out of all apps".
4. After sign-out: "You are signed out of Hub and of every app that used this sign-in."
- Expect each label and its value to be read as one item, headings to be announced as headings, and each control to be
  announced with its role.

**MT32. Health Card pass.** Sign in to Hub again first. Swipe through every item on each screen.

1. "Signing you in with Hub..." (expect it to be spoken without a touch).
2. Home screen: the name, the email, "With Hub's sign-in. No password or OTP was needed.", the buttons "Allow card sharing"
   and "Add my phone number", and the status "Card sharing with clinics: ...".
3. The consent screen after "Allow card sharing" and the consent screen after "Add my phone number".
4. The result lines after "Not now" ("You did not give the extra permission. Nothing changed.") and after "Allow".
5. "Sign in to Hub first" with the button "Open Hub" (sign out of Hub first).
- Expect status and error text to be spoken when it appears, with no touch.

**MT33. 200 % font size.** Set the font to its largest size (Settings, Display, Font size, or run
`adb shell settings put system font_scale 2.0`). Open every screen of MT31 and MT32 again. TalkBack can stay on or off. Record any text
that is cut off, any button that cannot be reached by scrolling, and any overlap. Expect every action to stay reachable.

**MT34. Restore the phone.** Turn TalkBack off. Set the font back (`adb shell settings put system font_scale 1.0`). Expect
normal text size.

## H. Captcha and internet

The eSignet login page loads an hCaptcha widget from `https://js.hcaptcha.com`. The stack has no captcha validator, so any
token passes, but the widget script needs internet in the browser that shows the page. An offline demo stalls on this page.

**MT35. Check internet before any demo.** On the laptop run `curl -sI https://js.hcaptcha.com | head -1`. Expect an HTTP status
line. Check that the emulator or the phone also has internet: open any web page in its Chrome. Then do MT12 and expect the
captcha box to appear and to accept a tap.

**MT36. Offline behaviour.** Optional. Turn the device's network off (airplane mode on a phone, Wi-Fi off on the emulator). `adb reverse` still works. Start the sign-in as in MT12. Record what the eSignet page shows and where
the flow stops. Turn the network on again. The team expects the page to load and the sign-in to stall at the captcha.

## I. Video dry run

Follow [video-script.md](video-script.md). The finished video must run 2 to 5 minutes. The script plans 4 minutes 45
seconds. Use a stopwatch and write the real time of each scene in the results.

**MT37. Set up the recording.** Run `bash scripts/demo/reset.sh` for a clean start, or `bash scripts/demo/demo.sh` as in MT10.
Hub in front, signed out. Record the emulator window and the terminal in one capture at 1080p. Hide notifications.

**MT38. Scenes 1 and 2 (planned 0:00 to 0:45).** The problem slide, then the component diagram. Record the real time. Check the
narration fits and no picture is unreadable.

**MT39. Scenes 3 and 4 (planned 0:45 to 1:55).** Hub sign-in with sharing on, then Health Card signing in with no browser. This
is MT11 to MT13 at presenting speed. Record the real time.

**MT40. Scenes 5 to 7 (planned 1:55 to 3:55).** The server rejections in the terminal, the consent flow with "Not now" and
"Allow", then sign-out. This is MT14, MT15 and MT17 at presenting speed. Record the real time.

**MT41. Scenes 8 to 10 (planned 3:55 to 4:45).** The test report, the documents and the closing slide. Record the real time.
If the whole take runs over 5 minutes, use the cut list at the end of the video script and record what you cut.

**MT42. Watch the take back.** Check that the total is between 2 and 5 minutes, the voice is clear, every UI text is legible,
and no token, key or password shows. Check that the narration says plainly what the script lists under "Say plainly". If MT22
to MT34 passed, tell the team that scene 10 still says a physical phone and a TalkBack pass were not run, and must change.

## J. Cleanup

**MT43. Tear down.** Run `bash scripts/demo/teardown.sh`. Then run `docker ps`. Expect no container whose name starts with
`ps03-`. On a phone also run `adb reverse --remove-all`. Check that TalkBack is off and the font scale is back to normal
(MT34).
