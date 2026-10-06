# UX: flows, screens and accessibility

The user flows of the two apps, screenshots of every screen from the emulator, and what was done for accessibility.
Written 2026-10-05, updated 2026-10-06 (consent-only re-consent), from the apps' string files (`android/*/src/main/res/values/strings.xml`), the screen code, the
unit tests and the emulator runs of 2026-10-05 and 2026-10-06.

## Screenshots

Taken by the emulator UI tests (`bash android/scripts/run-ui-test.sh all`, AVD `ps03_api34`, API 34) against the
local stack, with the mock identity system's test user (UIN `1774231323`, see [test-data.md](test-data.md)). The tests
save them to `$PS03_RUN_DIR/evidence/<date>/screens/`; the files below are copied from that folder unchanged.

| Hub: signed out, sharing switched on | Hub: signed in | Hub: after "Sign out of all apps" |
|---|---|---|
| ![Hub welcome](screens/hub-welcome-sharing-on.png) | ![Hub home](screens/hub-home.png) | ![Hub signed out](screens/hub-signed-out.png) |

| Health Card: signed in by Hub, no browser | Health Card: re-consent screen | Health Card: after the extra permission |
|---|---|---|
| ![Health Card silent sign-in](screens/health-card-silent-sign-in.png) | ![Consent screen](screens/health-card-consent-screen.png) | ![After consent](screens/health-card-after-consent.png) |

| Health Card: signed out of Health Card only | Health Card: Hub signed out, or sharing off | Health Card: Hub not installed |
|---|---|---|
| ![Health Card signed out](screens/health-card-signed-out.png) | ![Sign in to Hub first](screens/health-card-needs-hub.png) | ![Hub not installed](screens/health-card-hub-missing.png) |

| Health Card: "Not now" on the consent screen |
|---|
| ![Consent declined](screens/health-card-consent-declined.png) |

| Health Card: permission that needs no login | eSignet's consent page (no ID, no OTP) | Health Card: card sharing allowed |
|---|---|---|
| ![Consent-only screen](screens/health-card-consent-only-screen.png) | ![eSignet consent page](screens/esignet-consent-only-page.png) | ![After consent-only](screens/health-card-after-consent-only.png) |

- Test that takes each screenshot: `SuperAppSignInTest` (the two Hub screens before sign-out),
  `ChildAppSilentSignInTest`, `ChildAppConsentOnlyTest` (the three consent-only screens), `ChildAppReconsentTest`
  (consent screen, "Not now", after the extra permission), `ChildAppSignOutTest` (the three sign-out screens) and
  `ChildAppHubMissingTest` (Hub not installed), all from the UI test run of 2026-10-06 that ended at 19:34 IST at
  `ef69358` (`run-ui-test.sh all`: `OK (6 tests)`).
- "Hub signed out" and "sharing switched off in Hub" are one screen for Health Card: in both cases Hub's service
  has no sign-in to share (`FamilySsoGate.bundle` returns nothing), so Health Card shows the text "Hub has no
  sign-in it shares. Open Hub, turn on "Share this sign-in with my other apps" and sign in."
- When Hub is not installed, Health Card says "Not installed. Install Hub and sign in there first." and hides the
  "Open Hub" button. After "Not now" on the consent screen, Health Card returns to its home screen, still signed in
  with Hub, with "You did not give the extra permission. Nothing changed."
- Both apps have their own launcher icon in the same style: Hub blue with a "one sign-in, many apps" mark, Health
  Card teal with a card and a health cross. The same mark heads every screen, next to the app name.

## Flows

![User flow](diagrams/user-flow.png)

| Flow | Steps the user sees | Diagram |
|---|---|---|
| First sign-in in Hub | Hub (signed out) -> switch "Share this sign-in with my other apps" -> "Sign in with eSignet" -> browser: UIN, OTP, consent -> back in Hub, signed in | [seq-super-app-login](diagrams/seq-super-app-login.png) |
| Health Card, silent | open Health Card -> "Signing you in with Hub..." -> home with name and email; no browser | [seq-child-app-sso](diagrams/seq-child-app-sso.png) |
| Health Card, Hub not signed in | "Sign in to Hub first" with "Open Hub"; after Hub signs in, Health Card signs in on resume | same |
| Re-consent, no login (permission) | "Allow card sharing" -> explainer ("no ID or OTP is needed") -> "Continue to eSignet" -> eSignet's page "Allow more access?" -> "Allow" -> back in Health Card, still signed in with Hub, "Card sharing with clinics: Allowed" | [seq-consent-only](diagrams/seq-consent-only.png) |
| Re-consent, no login, declined | same, then "Not now" on eSignet's page -> home again with "You did not give the extra permission. Nothing changed." | same |
| Re-consent with login (new user data) | "Add my phone number" -> explainer (OTP asked again) -> "Continue to eSignet" -> browser login -> phone number shown | [seq-reconsent](diagrams/seq-reconsent.png) |
| Re-consent declined | "Add my phone number" -> explainer -> "Not now" -> home again, still signed in with Hub, with "You did not give the extra permission. Nothing changed." | [seq-reconsent](diagrams/seq-reconsent.png) |
| Sign out of Health Card | "Sign out of Health Card" -> "You signed out of Health Card. Hub is still signed in." -> "Continue with Hub" | - |
| Sign out of all apps | Hub "Sign out of all apps" -> "You are signed out of Hub and of every app that used this sign-in."; Hub also asks eSignet to end the device secret's session; Health Card clears itself on its next resume | [seq-child-app-sso](diagrams/seq-child-app-sso.png) |

## Screens in words

### Hub

```
Signed out                              Signed in
+---------------------------------+     +---------------------------------+
| [mark] Hub                      |     | [mark] Hub                      |
| Sign in once with eSignet. Your |     | Signed in as                    |
| other apps from the same        |     |   <name>                        |
| publisher, such as Health Card, |     | Sign-in shared with my other    |
| can then sign you in without    |     | apps: Yes                       |
| asking again.                   |     | Apps using this sign-in         |
| [ ] Share this sign-in with my  |     |   Health Card, last used 10:42  |
|     other apps                  |     | [ Sign out of all apps ]        |
| [ Sign in with eSignet ]        |     | Show technical details          |
| Show technical details          |     +---------------------------------+
+---------------------------------+
Working: "Signing you in..." / "Finish signing in on eSignet's page in the browser."  [ Cancel ]
After sign-out: "You are signed out of Hub and of every app that used this sign-in."
Errors: network, server refused (with its error code), failed security check, expired, wrong sign-in, unexpected
```

### Health Card

```
Hub first                  Consent                         Home
+--------------------+     +----------------------------+  +----------------------------+
| [mark] Health Card |     | [mark] Health Card         |  | [mark] Health Card         |
| Sign in to Hub     |     | Health Card needs your     |  | Signed in as <name>        |
| first              |     | permission                 |  | Email  <email>             |
| <reason>           |     | Health Card asks to see:   |  | Phone number               |
| Your health card.  |     | your phone number. Your    |  |   Not shared with Health   |
| Signs you in with  |     | Hub sign-in did not        |  |   Card   [Add my phone     |
| the sign-in you    |     | include this, so eSignet   |  |   number]                  |
| already have in    |     | must ask you first.        |  | How you signed in          |
| Hub ...            |     | eSignet keeps no browser   |  |   With Hub's sign-in. No   |
| [ Open Hub ]       |     | session, so you will enter |  |   password or OTP needed.  |
| [ Check again ]    |     | your ID and a new OTP.     |  | [ Sign out of Health Card ]|
+--------------------+     | [ Continue to eSignet ]    |  +----------------------------+
                           | [ Not now ]                |
                           +----------------------------+
Signed out of Health Card: "You signed out of Health Card. Hub is still signed in." [ Continue with Hub ]
```

- Each Hub failure has its own text on the Health Card screen: not installed, signed with another certificate,
  no answer in time, busy, version mismatch, Hub signed out ("Your Hub sign-in has ended. Open Hub and sign in again").
- Every error screen says what happened in plain words and offers one action ("Try again", "Open Hub").
- No screen shows a package name, a server address, a certificate or the words "Super App" or "Child App". Those
  details sit behind "Show technical details" at the bottom of every screen, folded by default: Hub shows the
  eSignet server and its client ID, Health Card the eSignet server, its signing certificate and whether Hub carries
  the same one. `HubScreensTest` and `CardScreensTest` check that nothing of this shows until the user opens it.
- Release builds write nothing to the Android log; debug builds log the steps (never a token) for the UI tests.

## Design decisions

- Both apps share one layout and one colour palette: the app mark and name at the top, then what is happening,
  then what the user can do, then the folded technical details. Only the mark's colour tells them apart.
- The re-consent screen tells the user before the browser opens whether a new OTP will be asked. A permission that
  needs no new user data is approved on eSignet's own page with one tap and no login; only new user data (the phone
  number) needs the ID and a new OTP, because eSignet's identity system releases user data only after an
  authentication (limit L02).
- eSignet's consent page is the server's own page, not Health Card's: the app cannot claim the user said yes. It
  names both apps, uses the same 48 dp buttons and a readable font size, and needs no script.
- Gap: the consent page shows the permission by its scope value (`health_card_share`); a friendly text from the
  resource server's description is not wired yet.
- The switch "Share this sign-in with my other apps" is off by default in Hub (the user opts in) and decides whether the `device_sso` scope is
  requested at all; with it off Hub behaves as an ordinary sign-in app and Health Card shows the Hub-first state.
- Hub lists the apps that used its sign-in by the name the launcher shows, with the time of last use, never a token
  or a package name ("Apps using this sign-in"). An app Android does not let Hub see is listed as "An app from the
  same publisher".
- Sign-out is one action in Hub. It ends Hub's sign-in on the phone at once, then asks eSignet to end the device
  secret's session (rule R27); Health Card follows without a network call by noticing, on every resume, that Hub's
  sign-in is gone.

## Accessibility

Done in WP-15 on the screens of both apps, and checked by the same kind of JVM screen test in each app
(`HubScreensTest`, `CardScreensTest`), at 200 % font size on a small phone (360 x 640 dp).

| Aspect | What was done | Check |
|---|---|---|
| Labels and reading order | headings are marked; each labelled value is one TalkBack item; the reading order follows the visual order | `HubScreensTest`, `CardScreensTest` |
| Touch targets | 48 dp touch targets on every control of both apps | `HubScreensTest`, `CardScreensTest` |
| Colour contrast | every text and background pair is at least 4.5 to 1, in light and dark | `HubThemeTest`, `CardThemeTest` |
| Large text | every screen of both apps keeps its actions reachable at 200 % font scale | `HubScreensTest`, `CardScreensTest` |
| Status changes | status and error text is a polite live region, so it is spoken when it changes | `HubScreensTest`, `CardScreensTest` ("is spoken") |
| No colour-only meaning | states carry words ("Yes", "No", "Not shared with Health Card") | strings |
| Decorative images | the app mark next to the name has no description, so TalkBack reads the name once | code (`Header()` in both `MainActivity.kt`) |

Not done: a TalkBack walk-through on a device or emulator and an accessibility scanner run (limit L08). Until then the
claim is limited to the JVM checks above.
