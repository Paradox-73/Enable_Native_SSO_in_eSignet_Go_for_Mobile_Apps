# Setup and run

How to install the tools, start the stack, run the tests and run both apps. Everything here was run on Windows 11
(Home, 10.0.26200) with Git Bash and Docker Desktop on 2026-10-05, unless a line says otherwise. Written 2026-10-05.

## Systems

| System | State |
|---|---|
| Windows 11, Git Bash, Docker Desktop (Linux containers) | the path that was run: every command in this repository's results was run here |
| Linux (Ubuntu) | every job of `.github/workflows/ci.yml` was replayed in Linux containers on 2026-10-05 (ubuntu 24.04 for repo-lint, the Docker daemon for the Go jobs and e2e, eclipse-temurin 17 with Android SDK 34 for the Android job): all passed; the hosted GitHub Actions run of 2026-10-06 (ubuntu-24.04 runners, 2 CPUs) passed every job |
| macOS | on 2026-10-13 (macOS 15, Apple silicon, Docker Desktop with 4 GB, GNU bash 5.3 from Homebrew) `repo-lint`, `docs-check`, the Go unit tests, `go-lint` and the 31 live e2e tests of that day passed; the Android builds and the emulator were not run. The default `/bin/bash` 3.2 is too old: install bash 4.4 or newer (`brew install bash`). On Apple silicon use the `arm64-v8a` emulator image |

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Git with Git Bash | any recent | every command here is a Bash command; on Windows do not use CMD or PowerShell |
| Bash | 4.4 or newer (Git Bash and Linux ship 5.x; macOS needs `brew install bash`) | every script; `scripts/lib.sh` stops with exit status 2 on an older bash |
| Docker Desktop (or Docker Engine with the compose plugin) | Linux containers, 4 GB memory or more for the Docker VM | Go builds and tests, lint, the local stack |
| JDK | 17 or newer (team: 21) | Gradle, the Android build, `keytool` |
| Android SDK | platform `android-34`, `build-tools;34.0.0`, `platform-tools`; for the emulator also `emulator` and `system-images;android-34;google_apis;x86_64` | apps, `apksigner`, `adb`, the emulator |
| Node.js | 20 or newer | `scripts/openapi-lint.sh` only |
| newman | 6 (`npm install -g newman@6`) | the Postman folder run only |
| Python | any 3.x (`python` or `python3`) | `scripts/evidence.sh`, `android/scripts/register-client.sh`, `scripts/docs-check.sh` |

Not needed: Go (runs in the container `golang:1.26-bookworm`), golangci-lint (built into a Docker volume on first use),
Gradle (the wrapper downloads 8.7).

## Install and check

```bash
git clone -c core.longpaths=true https://github.com/Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps.git esignet-native-sso
cd esignet-native-sso
bash scripts/install-hooks.sh        # core.hooksPath and core.longpaths
bash scripts/repo-lint.sh            # repo-lint: OK
bash scripts/go.sh . version         # go version go1.26.x linux/amd64 (first run pulls the image)
export JAVA_HOME=/path/to/jdk-21     # Git Bash form of a Windows path: /c/Program Files/...
export ANDROID_HOME=/path/to/Android/Sdk
```

- `core.longpaths=true` on the clone line matters on Windows: some upstream test files have very long paths.
- Keys, logs and results go to `$PS03_RUN_DIR` (the folder `run/` next to the clone when it exists, else `run/` inside
  it, which git ignores). The first Android build creates a test signing key there; each machine gets its own.

## Ports

| Port | Service |
|---|---|
| 38088 | eSignet API; the issuer is `http://localhost:38088` |
| 38300 | login pages (oidc-ui) |
| 38082 | mock identity system |
| 38455 | Postgres |
| 5584 / 5585 | emulator console and adb (`emulator-5584`) |

The emulator reaches the stack through `adb reverse` on 38088 and 38300, so the issuer is `http://localhost:38088` on the
phone as on the laptop. Do not use `10.0.2.2`: it would change `iss` and every DPoP `htu`.

## Run the server side

```bash
bash scripts/with-stack.sh bash e2e/run.sh
```

- It builds the eSignet image from this repository, starts the stack (about 30 to 40 s once the image is cached, 78 s the
  first time), checks it (`stack smoke: PASS`), runs the live tests, and removes the stack with its volumes.
- Expected last lines: `e2e: 32 passed, 0 failed`. The suite waits out a real 120 s id_token lifetime, in parallel with the other tests (see the timing in the e2e log).
- Leave the stack up for manual work: `PS03_KEEP_STACK=1 bash scripts/with-stack.sh true`, then
  `bash deploy/stack.sh down` when done. Other commands: `bash deploy/stack.sh status | logs [service]`.
- The Postman folder: `bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh` (needs newman);
  how to run it in the Postman app: [../esignet/postman-collection/README.md](../esignet/postman-collection/README.md#native-sso).
- By hand with curl: the discovery document at `http://localhost:38088/.well-known/openid-configuration` shows
  `native_sso_supported: true` and `device_sso` in `scopes_supported`.

## Run the apps

```bash
( cd android && bash gradlew --no-daemon assembleDebug )          # both APKs, signed with one test certificate
bash android/scripts/verify-family-signing.sh debug               # family-signing: OK <sha-256>
bash scripts/demo/demo.sh                                         # stack, visible emulator, both apps installed and registered, Hub open
```

- APKs: `android/superapp/build/outputs/apk/debug/superapp-debug.apk` (Hub) and
  `android/childapp/build/outputs/apk/debug/childapp-debug.apk` (Health Card).
- Create the emulator once (not done by the scripts; the command is from the Android tools documentation, the team laptop made its AVD earlier):
  `"$ANDROID_HOME/cmdline-tools/latest/bin/avdmanager" create avd -n ps03_api34 -k "system-images;android-34;google_apis;x86_64"`.
- Each app registers itself as an eSignet client with the public half of a key made in the Android Keystore:
  `bash android/scripts/register-client.sh superapp` (then `childapp`). `demo.sh` does this.
- The demo, click by click: [demo-script.md](demo-script.md). The UI tests:
  `bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all`.
- **The emulator part has not been run** (see [test-report.md](test-report.md#known-limits), limit L06). The commands above
  are written and checked up to the point where the emulator starts.

## Tests and checks

| Command | Passes when it prints | Measured |
|---|---|---|
| `bash scripts/repo-lint.sh` | `repo-lint: OK` | seconds |
| `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover` | `... 0 failed` | 35 s on 2026-10-05 |
| `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover` | `... 0 failed` | 37 s on 2026-10-05 |
| `bash scripts/go-lint.sh thunder/backend` and `esignet/esignet-service` | `go-lint: ...: OK` | first cold run 8 min 53 s for Thunder |
| `( cd android && bash gradlew --no-daemon :oidc-client:test :secure-store:testDebugUnitTest :superapp:testDebugUnitTest :childapp:testDebugUnitTest )` | `BUILD SUCCESSFUL` | 27 to 59 s on 2026-10-05 |
| `bash scripts/docs-check.sh docs/design.md docs/threat-model.md` | `0 broken links; tasks traced 11/11` | seconds |
| `bash scripts/evidence.sh --run` | `evidence: <n> results at <commit>` | several minutes; rewrites [test-report.md](test-report.md) |
| `bash scripts/fresh-clone-check.sh` | `fresh-clone-check: PASS` | clean clone, lint, Go unit tests, Android JVM tests |

`bash make.sh help` lists short names for these.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `Waiting for lock docker-stack` or `emulator` | another job on the machine holds the lock. The scripts of this project never remove an emulator lock; check that nobody uses the emulator, then remove that one directory (`/e/Projects/MOSIP/toolchain/locks/emulator` on the team laptop, else `$PS03_RUN_DIR/locks/emulator`) yourself |
| `Go test binaries blocked` on Windows | do not run `go test` natively; `scripts/go-test.sh` runs it in Linux |
| stack does not become healthy | Docker memory under 4 GB, or a port in the table above is taken; `bash deploy/stack.sh logs esignet` |
| First `up` is slow | the image build (78 s on 2026-10-05); later runs reuse the build cache |
| `INSTALL_FAILED_UPDATE_INCOMPATIBLE` | an app on the emulator has another signing certificate; the scripts uninstall and reinstall it |
| Chrome first-run screen on the emulator | `with-emulator.sh` and `demo.sh` turn it off through a command-line file; if the AVD was wiped run `bash scripts/demo/reset.sh --device` |
| eSignet page asks for a captcha | tick the box; the stack has no captcha validator, any token passes in the API |
| Postman desktop shows `invalid_client` for the upstream folders | they use the endpoint URL as assertion audience; the Native SSO folder uses the issuer |

More: [../CONTRIBUTING.md](../CONTRIBUTING.md) (conventions, task workflow), [../deploy/README.md](../deploy/README.md) (stack),
[../android/README.md](../android/README.md) (apps), [../scripts/README.md](../scripts/README.md) (scripts).
