# Submission checklist

Every item of the MOSIP Decode 2026 submission guidelines and of problem statement 03, where it is, and its state on
2026-10-05. States: **READY** (exists and checked), **PARTIAL** (exists, something open), **TODO** (not made), **PERSON**
(only a team member can do it). Deadline: 2026-10-26 23:59 IST.

## Mandatory items of the submission guidelines

| Item | Where | State | Open |
|---|---|---|---|
| Source code, complete, hosted | this repository, GitHub `Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps` | PARTIAL | local commits since `main` `1bd6fd0` are not pushed; push, then check the repository is readable by MOSIP |
| Working prototype or demo | stack and both apps: [setup.md](setup.md); `bash scripts/demo/demo.sh` | PARTIAL | server part runs (65 s, 2026-10-05); the emulator part has never run (limit L06) |
| Pitch deck | outline: [ppt-outline.md](ppt-outline.md) | TODO | build the slide file from the outline after the emulator run; export PPT and PDF |
| Video demo, 2 to 5 minutes | storyboard (4 min 30 s): [demo-script.md](demo-script.md) | TODO | record after the emulator run; upload to the cloud storage |
| Usage and installation instructions | [../README.md](../README.md), [setup.md](setup.md) | READY | follow them once on a clean clone: `bash scripts/fresh-clone-check.sh` |
| API documentation | [api.md](api.md), [../esignet/docs/esignet-openapi.yaml](../esignet/docs/esignet-openapi.yaml), [../thunder/api/oauth2.yaml](../thunder/api/oauth2.yaml) | READY | OpenAPI lint 0 errors |
| Git repository with history | same repository; history never rewritten | PARTIAL | push |
| College or university credentials | not in the repository (personal documents) | PERSON | every member uploads a valid ID to the cloud storage |

## Other items of the guidelines

| Item | Where | State |
|---|---|---|
| Design mockups, wireframes, user flow | [ux.md](ux.md), [diagrams/user-flow.png](diagrams/user-flow.png) | PARTIAL: text wireframes and a flow diagram; no device screenshots yet |
| Architecture diagram | [diagrams/components.png](diagrams/components.png) and four sequence diagrams ([diagrams/README.md](diagrams/README.md)) | READY |
| Test data | [test-data.md](test-data.md) | READY |
| Test cases and results | [test-report.md](test-report.md), `scripts/evidence.sh` | PARTIAL: 14 results are NOT RUN (emulator tests, CI, manual runs), each with its reason |

## Deliverables of problem statement 03

| Deliverable | Where | State |
|---|---|---|
| Native apps showing the full flow | [../android/README.md](../android/README.md) | PARTIAL: JVM-tested; not seen on an emulator |
| Updated Postman collection | [../esignet/postman-collection/README.md](../esignet/postman-collection/README.md#native-sso) | READY: newman 31 requests, 116 assertions, 0 failed (2026-10-05) |
| Updated API documentation | [api.md](api.md) | READY |
| Design document based on MOSIP/eSignet design and RFC behaviour | [design.md](design.md), [threat-model.md](threat-model.md) | READY; section 17 lists where the statement and the specifications differ |
| PPT summarising approach and implementation | [ppt-outline.md](ppt-outline.md) | TODO (see Pitch deck) |

## Steps to submit

1. Run the emulator checks once the emulator lock is free: `bash scripts/demo/demo.sh --check`, then
   `bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all`.
2. `bash scripts/evidence.sh --run` and commit the new `docs/test-report.md`; copy the chosen screenshots into
   `docs/screens/` and link them in [ux.md](ux.md).
3. Record the video and build the slide deck; export PPT, PDF and video.
4. `bash scripts/repo-lint.sh`, `bash scripts/docs-check.sh docs/design.md docs/threat-model.md`,
   `bash scripts/fresh-clone-check.sh` (each must pass).
5. Push `main`; open the repository in a private browser window to check access.
6. Put deck, video, ID proofs and any file too large for git into the cloud storage; set the sharing permission so MOSIP can open it.
7. Email decode@mosip.io with the team name (Badmosip), the problem statement (03, Enable Native SSO in eSignet Go for
   Mobile Apps) and the repository and storage URLs. Check that every link opens.

## Rules the team keeps

- No result is written that was not run; unrun checks are marked NOT RUN with a reason.
- No secrets in git (`bash scripts/repo-lint.sh` checks); test keys are made at run time.
- Imported code stays byte for byte in its `import:` commits ([../THIRD_PARTY.md](../THIRD_PARTY.md)).
