# Submission checklist

Every item of the MOSIP Decode 2026 submission guidelines and of problem statement 03, where it is, and its state on
2026-10-22. States: **READY** (exists and checked), **PARTIAL** (exists, something open), **PERSON**
(only a team member can do it). Deadline: 2026-10-26 23:59 IST. Dated results: [test-report.md](test-report.md).

## Mandatory items of the submission guidelines

| Item | Where | State | Open |
|---|---|---|---|
| Source code, complete, hosted | GitHub `Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps` (public) | READY | GitHub Actions passed every job on 2026-10-24 at `9bba2d7` (run 40134113684) |
| Working prototype or demo | stack and both apps: [setup.md](setup.md); `bash scripts/demo/demo.sh` | READY | `bash scripts/demo/demo.sh --check` (row DEMO-FULL). Emulator and one real phone (OnePlus CPH2569, Android 15; [phone-run.md](evidence/2026-10-24/phone-run.md)) |
| Pitch deck | outline: [ppt-outline.md](ppt-outline.md) | READY | the deck is ready and is submitted with the other deliverables (it is not kept in this repository) |
| Video demo, 2 to 5 minutes | script with scenes and timings: [video-script.md](video-script.md); runbook: [demo-script.md](demo-script.md) | READY (link pending) | recorded; upload to the cloud storage and add the link here |
| Usage and installation instructions | [../README.md](../README.md), [setup.md](setup.md) | READY | a person runs a fresh clone on macOS, Linux and Windows (checklist steps MT01 to MT10) and sends the results template back |
| API documentation | [api.md](api.md), [../esignet/docs/esignet-openapi.yaml](../esignet/docs/esignet-openapi.yaml), [../thunder/api/oauth2.yaml](../thunder/api/oauth2.yaml) | READY | OpenAPI lint 0 errors |
| Git repository with history | same repository; history never rewritten; members in the README's Team table | PARTIAL | most commits come from one account; each member commits their own remaining work (deck notes, manual test results) from their own account (PERSON) |
| College or university credentials | not in the repository (personal documents) | PERSON | every member uploads a valid ID to the cloud storage |

## Other items of the guidelines

| Item | Where | State |
|---|---|---|
| Design mockups, wireframes, user flow | [ux.md](ux.md), [diagrams/user-flow.png](diagrams/user-flow.png) | READY: emulator screenshots of every screen ([screens/](screens/)), the screens in words and the flow diagram |
| Architecture diagram | [diagrams/components.png](diagrams/components.png) and the sequence diagrams ([diagrams/README.md](diagrams/README.md)) | READY |
| Test data | [test-data.md](test-data.md) | READY |
| Test cases and results | [test-report.md](test-report.md) with trimmed logs in [evidence/](evidence/), `scripts/evidence.sh` | READY: every automated check PASS; NOT RUN only for the Postman desktop run, with its reason; TalkBack and physical phone passed on 2026-10-24 |

## Deliverables of problem statement 03

| Deliverable | Where | State |
|---|---|---|
| Native apps showing the full flow | [../android/README.md](../android/README.md) | READY: JVM-tested (rows JVM-*) and run on the emulator (UI-HUB, UI-CARD, UI-FAMILY, DEMO-FULL) |
| Updated Postman collection | [../esignet/postman-collection/README.md](../esignet/postman-collection/README.md#native-sso) | READY: run under newman (row NEWMAN), including consent-only re-consent (Card 05 to Card 09) |
| Updated API documentation | [api.md](api.md) | READY |
| Design document based on MOSIP/eSignet design and RFC behaviour | [design.md](design.md), [threat-model.md](threat-model.md) | READY; section 17 lists where the statement, its sequence diagrams and the specifications differ |
| PPT summarising approach and implementation | [ppt-outline.md](ppt-outline.md) | READY (see Pitch deck) |

## Steps to submit

1. On the day: `bash scripts/demo/demo.sh --check` (expect `demo ready`, then `teardown ok`), then
   `bash scripts/evidence.sh --check`.
2. The video is recorded ([video-script.md](video-script.md)); link: https://youtu.be/KlTitpsxiIo. The deck is ready ([ppt-outline.md](ppt-outline.md) is its outline); export PPT, PDF and video. Keep the deck's team slide in step with the README's Team table.
3. `bash scripts/repo-lint.sh`, `bash make.sh docs-check`, `bash scripts/fresh-clone-check.sh` (each must pass).
4. Push `main`; open the Actions tab and record the run of `ci` (URL and result) in `docs/test-evidence.tsv`, row CI,
   then `bash scripts/evidence.sh` to rewrite the report.
5. Collect the filled [manual-test-results-template.md](manual-test-results-template.md) copies; the TalkBack pass and
   phone run of 2026-10-24 are in rows M02 and M03 of `docs/test-evidence.tsv` ([phone-run.md](evidence/2026-10-24/phone-run.md)).
6. Put deck, video, ID proofs and any file too large for git into the cloud storage; set the sharing permission so
   MOSIP can open it.
7. Email decode@mosip.io with the team name (Badmosip), the problem statement (03, Enable Native SSO in eSignet Go for
   Mobile Apps) and the repository and storage URLs. Open every link from a logged-out browser first.

## Rules the team keeps

- No result is written that was not run; unrun checks are marked NOT RUN with a reason.
- No secrets in git (`bash scripts/repo-lint.sh` checks); test keys are made at run time.
- Imported code stays byte for byte in its `import:` commits ([../THIRD_PARTY.md](../THIRD_PARTY.md)).
