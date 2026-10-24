# Real-phone run, 2026-10-24

Steps MT22 to MT33 of [manual-test-checklist.md](../../manual-test-checklist.md), run between 23:15 and 23:40 IST. Rows M02 and M03
of [test-evidence.tsv](../../test-evidence.tsv) record the result.

## Device

| Item | Value |
|---|---|
| Phone | OnePlus CPH2569 |
| Android | 15 (SDK 35), arm64-v8a |
| SoC and memory | SM7325, 11.0 GiB RAM |
| StrongBox | none: `pm list features` has no strongbox line; keystore2 logged `HARDWARE_TYPE_UNAVAILABLE` for the StrongBox attempt and Hub logged `DPoP key: TEE_OR_SOFTWARE`, so the keys fell back to the TEE as designed |

## Setup (MT22 to MT25)

- Stack started with `scripts/with-stack.sh`; the smoke check passed.
- `adb reverse` set for ports 38088 and 38300.
- Both debug APKs installed (`Success`).
- `register-client.sh` printed `ps03-hub-c806bedbfdce ACTIVE` and `ps03-card-2a7899aa23a7 ACTIVE`.

## Results

| Step | What | Result |
|---|---|---|
| MT26 | Hub sign-in on the phone | PASS. Hub showed "Signed in as Siddharth K Mansour" and "Sign-in shared with my other apps: Yes, until 25/10/2026, 07:24" (8 hours after the sign-in). Screenshot: [hub-home-phone.png](../../screens/hub-home-phone.png) |
| MT27 | Health Card silent sign-in | PASS. No browser opened; Health Card signed in with Hub's sign-in |
| MT14 to MT17 | Consent "Not now", consent "Allow", add phone number with own login, sign out of all apps | PASS: run by a team member on the phone; every step showed the expected screen |
| MT30 to MT33 | TalkBack pass and large font | PASS: run by a team member on the phone; no issues found |

## Observations

1. The tester first signed in with the sharing switch off, out of the checklist order. Hub showed "Sign-in shared with my
   other apps: No", and Health Card showed "Sign in to Hub first", as designed: sharing is opt-in, so without it Health Card
   gets no sign-in. After signing in again with the switch on, as MT11 says, silent sign-in worked.
2. Typing a wrong ID three times led eSignet's page to "Something went wrong (401)" (mock identity: `invalid_individual_id`).
   This is eSignet's behaviour, not a defect of the apps.
3. The USB port forwarding (`adb reverse`) dropped once mid-test and was added again. When eSignet is unreachable from the
   phone, run `adb reverse --list` and add the missing port.
4. OnePlus freezes background apps (logcat `OplusHansManager freeze ... superapp`). Silent sign-in still worked, because
   binding Hub's service wakes Hub.
