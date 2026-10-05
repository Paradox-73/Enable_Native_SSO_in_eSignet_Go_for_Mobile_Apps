# UX: flows, screens and accessibility

The user flows of the two apps, text wireframes of every screen, and what was done for accessibility. Written
2026-10-05 from the apps' string files (`android/*/src/main/res/values/strings.xml`), the screen code and the unit tests.

## Screenshots

- **There are none yet.** The UI tests that save screenshots (`SuperAppSignInTest`, `ChildAppSilentSignInTest`,
  `ChildAppReconsentTest`) need the Android emulator, and no emulator run exists (limit L06 in
  [test-report.md](test-report.md#known-limits)). The tests write them to `$PS03_RUN_DIR/evidence/<date>/screens/`; after
  the first run the team copies the chosen ones into `docs/screens/` and links them here.
- The wireframes below are drawn from the code, not from a device.

## Flows

![User flow](diagrams/user-flow.png)

| Flow | Steps the user sees | Diagram |
|---|---|---|
| First sign-in in Hub | Hub home (signed out) -> "Sign in with eSignet" -> browser: UIN, OTP, consent -> back in Hub, signed in | [seq-super-app-login](diagrams/seq-super-app-login.png) |
| Health Card, silent | open Health Card -> "Signing you in with Hub..." -> home with name and email; no browser | [seq-child-app-sso](diagrams/seq-child-app-sso.png) |
| Health Card, Hub not signed in | "Sign in to Hub first" with "Open Hub"; after Hub signs in, Health Card signs in on resume | same |
| Re-consent | "Add my phone number" -> explainer (OTP asked again) -> "Continue to eSignet" -> browser login -> phone number shown | [seq-reconsent](diagrams/seq-reconsent.png) |
| Sign out of all apps | Hub "Sign out of all apps" -> Hub signed out; Health Card clears itself on its next resume | [seq-child-app-sso](diagrams/seq-child-app-sso.png) |

## Wireframes

### Hub (Super App)

```
Signed out                              Signed in
+---------------------------------+     +---------------------------------+
| Hub                             |     | Hub                             |
| Super App. You sign in here     |     | Signed in as                    |
| once with eSignet; your other   |     |   <name>                        |
| apps from the same publisher    |     | Sign-in shared with my other    |
| can then sign you in without    |     | apps: Yes                       |
| asking again.                   |     | Apps using this sign-in         |
| eSignet server                  |     |   Health Card, last 10:42       |
|   http://localhost:38088        |     |                                 |
| [ ] Use this sign-in in my      |     | [ Sign out of all apps ]        |
|     other apps                  |     +---------------------------------+
| [ Sign in with eSignet ]        |
+---------------------------------+
Working: "Signing you in..." / "Finish signing in in the browser."  [ Cancel ]
Errors: network, server refused (code), failed security check, expired, wrong sign-in, unexpected
```

### Health Card (Child App)

```
Hub first                  Consent                         Home
+--------------------+     +----------------------------+  +----------------------------+
| Health Card        |     | Health Card needs your     |  | Health Card                |
| Sign in to Hub     |     | permission                 |  | Signed in as <name>        |
| first              |     | Health Card asks to see:   |  | Email  <email>             |
| <reason>           |     | your phone number. Your    |  | Phone number               |
| [ Open Hub ]       |     | Hub sign-in did not        |  |   Not shared with Health   |
| [ Check again ]    |     | include this, so eSignet   |  |   Card   [Add my phone     |
+--------------------+     | must ask you first.        |  |   number]                  |
                           | eSignet keeps no browser   |  | How you signed in          |
                           | session, so you will enter |  |   With Hub's sign-in. No   |
                           | your ID and a new OTP.     |  |   password or OTP needed.  |
                           | [ Continue to eSignet ]    |  | [ Sign out of Health Card ]|
                           | [ Not now ]                |  +----------------------------+
                           +----------------------------+
Signed out of Health Card: "You signed out of Health Card. Hub is still signed in." [ Continue with Hub ]
```

- Each Hub failure has its own text on the Health Card screen: not installed, signed with another certificate,
  no answer in time, busy, version mismatch, Hub signed out ("Your Hub sign-in has ended. Open Hub and sign in again").
- Every error screen says what happened in plain words and offers one action ("Try again", "Open Hub").

## Design decisions

- The re-consent screen tells the user before the browser opens that a new OTP will be asked: eSignet Go keeps no browser
  session (limit L02), so a surprise is avoided.
- The switch "Use this sign-in in my other apps" is off by default in Hub (the user opts in) and decides whether the `device_sso` scope is
  requested at all; with it off Hub behaves as an ordinary sign-in app and Health Card shows the Hub-first state.
- Hub lists the apps that used its sign-in with the time of last use, never a token ("Apps using this sign-in").
- Sign-out is one action in Hub; Health Card follows without a network call by noticing, on every resume, that Hub's
  sign-in is gone.

## Accessibility

Done in WP-15 on the screens of both apps. The JVM checks differ by app: Health Card has screen tests at 200 % font on a
small phone, Hub has the contrast test and the same semantics in its code, but no screen test of its own.

| Aspect | What was done | Check |
|---|---|---|
| Labels and reading order | headings are marked; each labelled value is one TalkBack item; the reading order follows the visual order | Health Card: `CardScreensTest`; Hub: `heading()` in `superapp/.../MainActivity.kt`, no test |
| Touch targets | 48 dp touch targets on Health Card controls | `CardScreensTest` |
| Colour contrast | every text and background pair is at least 4.5 to 1, in light and dark | `HubThemeTest`, `CardThemeTest` |
| Large text | every Health Card screen keeps its actions reachable at 200 % font scale | `CardScreensTest` |
| Status changes | status and error text is a polite live region, so it is spoken when it changes | `CardScreensTest` ("is spoken"); Hub `error_text` in code |
| No colour-only meaning | states carry words ("Yes", "No", "Not shared with Health Card") | strings |

Not done: a TalkBack walk-through on a device or emulator and an accessibility scanner run (limit L08). Until then the
claim is limited to the JVM checks above.
