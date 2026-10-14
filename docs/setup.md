# Setup and run

How to install the tools, start the stack, run the tests and run both apps, and what to do when something fails.
Everything here was run on Windows 11 (Home, 10.0.26200) with Git Bash and Docker Desktop unless a line says
otherwise. Dated results of every check: [test-report.md](test-report.md).

## Systems

| System | State |
|---|---|
| Windows 11, Git Bash, Docker Desktop (Linux containers) | the path that was run: every command in this repository's results was run here |
| Linux (Ubuntu) | every job of `.github/workflows/ci.yml` was replayed in Linux containers on 2026-10-05 (ubuntu 24.04 for repo-lint, the Docker daemon for the Go jobs and e2e, eclipse-temurin 17 with Android SDK 34 for the Android job): all passed; the last hosted GitHub Actions run (ubuntu-24.04 runners, 2 CPUs) passed every job (row CI and limit L05 of [test-report.md](test-report.md)) |
| macOS | on 2026-10-13 (macOS 15, Apple silicon, Docker Desktop with 4 GB, GNU bash 5.3 from Homebrew) `repo-lint`, `docs-check`, the Go unit tests, `go-lint` and the 31 live e2e tests of that day passed; the Android builds and the emulator were not run. The default `/bin/bash` 3.2 is too old: install bash 4.4 or newer (`brew install bash`). On Apple silicon use the `arm64-v8a` emulator image |

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Git with Git Bash | any recent | every command here is a Bash command; on Windows do not use CMD or PowerShell |
| Bash | 4.4 or newer (Git Bash and Linux ship 5.x; macOS needs `brew install bash`) | every script; `scripts/lib.sh` stops with exit status 2 on an older bash |
| Docker Desktop (or Docker Engine with the compose plugin) | Linux containers, 4 GB memory or more for the Docker VM (the team laptop uses 8 GB) | Go builds and tests, lint, the local stack |
| JDK | 17 or newer (team: 21) | Gradle, the Android build, `keytool` |
| Android SDK | platform `android-34`, `build-tools;34.0.0`, `platform-tools`; for the emulator also `emulator` and `system-images;android-34;google_apis;x86_64` | apps, `apksigner`, `adb`, the emulator |
| Node.js with `npx` | 20 or newer | `scripts/openapi-lint.sh` only (it downloads Redocly CLI 1.34.20 on first run) |
| newman | 6 (`npm install -g newman@6`) | the Postman folder run only |
| Python | any 3.x (`python` or `python3`) | `scripts/evidence.sh`, `android/scripts/register-client.sh` |

Not needed: Go (runs in the container `golang:1.26-bookworm`), golangci-lint (v1.64.8 for Thunder and v2.14.0 for
eSignet, built into a Docker volume on first use), mockery (run with `go run`), Gradle (the wrapper downloads 8.7 with a
pinned checksum).

On Linux, let your user run Docker without `sudo` (<https://docs.docker.com/engine/install/linux-postinstall/>).

## Install and check

```bash
git clone -c core.longpaths=true https://github.com/Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps.git esignet-native-sso
cd esignet-native-sso
bash scripts/install-hooks.sh        # install-hooks: core.hooksPath=scripts/hooks (also sets core.longpaths)
bash scripts/repo-lint.sh            # repo-lint: OK
bash scripts/go.sh . version         # go version go1.26.x linux/amd64 (first run pulls the image)
export JAVA_HOME=/path/to/jdk-21     # Git Bash form of a Windows path: /c/Program Files/...
export ANDROID_HOME=/path/to/Android/Sdk
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "platforms;android-34" "build-tools;34.0.0" "platform-tools"
```

- `core.longpaths=true` on the clone line matters on Windows: some upstream test files have very long paths.
- On Windows the SDK tools are `sdkmanager.bat` and `avdmanager.bat`. Android Studio installs the SDK for you; a
  `local.properties` file with `sdk.dir=...` in `android/` also works (ignored by git).
- Keys, logs and results go to `$PS03_RUN_DIR` (the folder `run/` next to the clone when it exists, else `run/` inside
  it, which git ignores). The first Android build creates a test signing key there; each machine gets its own.
  Every setting: [../scripts/README.md](../scripts/README.md#settings).

## Ports

| Port | Service |
|---|---|
| 38088 | eSignet API; the issuer is `http://localhost:38088` |
| 38300 | login pages (oidc-ui) |
| 38082 | mock identity system |
| 38455 | Postgres |
| 5584 / 5585 | emulator console and adb (`emulator-5584`) |

The emulator reaches the stack through `adb reverse` on 38088 and 38300, so the issuer is `http://localhost:38088` on the
phone as on the laptop. Do not use `10.0.2.2`: it would change `iss` and every DPoP `htu`. Images and services of the
stack: [../deploy/README.md](../deploy/README.md).

## Run the server side

```bash
bash scripts/with-stack.sh bash e2e/run.sh
```

- It builds the eSignet image from this repository, starts the stack (about 30 to 40 s once the image is cached, 78 s the
  first time), checks it (`stack smoke: PASS`), runs the live tests, and removes the stack with its volumes.
- Expected last line: `e2e: N passed, 0 failed`. The suite waits out a real 120 s id_token lifetime, in parallel with the
  other tests.
- Leave the stack up for manual work: `PS03_KEEP_STACK=1 bash scripts/with-stack.sh true`, then
  `bash deploy/stack.sh down` when done. Other commands: `bash deploy/stack.sh status | logs [service]`.
- The Postman folder: `bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh` (needs newman);
  how to run it in the Postman app: [../esignet/postman-collection/README.md](../esignet/postman-collection/README.md#native-sso).
- By hand with curl: the discovery document at `http://localhost:38088/.well-known/openid-configuration` shows
  `native_sso_supported: true` and `device_sso` in `scopes_supported`.
- Seeded user: UIN `1774231323`, OTP `111111`. All test data: [test-data.md](test-data.md).

## Run the apps

```bash
( cd android && bash gradlew --no-daemon assembleDebug )          # both APKs, signed with one test certificate
bash android/scripts/verify-family-signing.sh debug               # family-signing: OK <sha-256>
bash scripts/demo/demo.sh                                         # stack, visible emulator, both apps installed and registered, Hub open
```

- APKs: `android/superapp/build/outputs/apk/debug/superapp-debug.apk` (Hub) and
  `android/childapp/build/outputs/apk/debug/childapp-debug.apk` (Health Card).
- Create the emulator once (the scripts do not):
  `"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "emulator" "system-images;android-34;google_apis;x86_64"`, then
  `"$ANDROID_HOME/cmdline-tools/latest/bin/avdmanager" create avd -n ps03_api34 -k "system-images;android-34;google_apis;x86_64" -d pixel_6`.
- Each app registers itself as an eSignet client with the public half of a key made in the Android Keystore:
  `bash android/scripts/register-client.sh superapp` (then `childapp`). `demo.sh` does this.
- The demo, click by click: [demo-script.md](demo-script.md). The UI tests:
  `bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all`.
- Emulator runs use one image (AVD `ps03_api34`, API 34, Chrome 113): limit L06 in
  [test-report.md](test-report.md#known-limits).

## Tests and checks

| Command | Passes when it prints | Measured |
|---|---|---|
| `bash scripts/repo-lint.sh` | `repo-lint: OK` | seconds |
| `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover` | `... 0 failed` | 35 s on 2026-10-05 |
| `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover` | `... 0 failed` | 37 s on 2026-10-05 |
| `bash scripts/go-lint.sh thunder/backend` and `esignet/esignet-service` | `go-lint: ...: OK` | first cold run 8 min 53 s for Thunder |
| `( cd android && bash gradlew --no-daemon :oidc-client:test :secure-store:testDebugUnitTest :superapp:testDebugUnitTest :childapp:testDebugUnitTest )` | `BUILD SUCCESSFUL` | 27 to 59 s on 2026-10-05 |
| `bash scripts/openapi-lint.sh esignet/docs/esignet-openapi.yaml` | `openapi-lint: ...: OK` | seconds |
| `bash scripts/docs-check.sh docs/design.md docs/threat-model.md` | `0 broken links; tasks traced 11/11` | seconds |
| `bash scripts/evidence.sh --run` | `evidence: <n> results at <commit>` | several minutes; rewrites [test-report.md](test-report.md) |
| `bash scripts/fresh-clone-check.sh` | `fresh-clone-check: PASS` | clean clone, lint, Go unit tests, Android JVM tests |

`bash make.sh help` lists short names for these. Every script and its options: [../scripts/README.md](../scripts/README.md).

## Troubleshooting

Setup, Go and Docker:

| Symptom | Cause and fix |
|---|---|
| `Waiting for lock go-PS03`, `docker-stack` or `emulator` | another job on the machine holds the lock. A waiting script removes any lock older than 90 minutes (`PS03_LOCK_STALE_MIN`) as stale; `demo.sh` instead refuses to start while the emulator lock exists. Check that nobody uses it (`docker ps`, the emulator window), then remove that one directory under `$PS03_LOCK_DIR` yourself |
| `An Application Control policy has blocked this file` on Windows | do not run `go test` natively; `scripts/go-test.sh` runs it in Linux. eSignet's keymanager tests also fail natively on Windows ("symmetric key not found") |
| Lint times out on a busy machine | `bash scripts/go-lint.sh thunder/backend -- --timeout=30m` |
| Container killed for memory | raise Docker's memory or run fewer jobs at once; limits are `PS03_GO_MEMORY` and `PS03_GO_LINT_MEMORY` |
| Broken or stale Go caches | `docker volume rm ps03-gomod ps03-gocache ps03-gotools` (the next run is slow) |
| `verify-import.sh` needs the network | on a fresh clone it fetches the pinned upstream commit into `$PS03_RUN_DIR/upstream-cache/` |
| Stack does not become healthy | Docker memory under 4 GB, or a port in the table above is taken; `bash deploy/stack.sh logs esignet` |
| First `up` is slow | the image build (78 s on 2026-10-05); later runs reuse the build cache |
| Upstream `esignet/esignet-service/Dockerfile` fails | expected: it cannot see `../../thunder/backend`. The image is built by `deploy/esignet.Dockerfile` with the repository root as context |
| eSignet cannot find `data/deployment.yaml` | it reads it relative to the working directory; start it from `esignet/esignet-service/` |
| Executable bit lost after a commit on Windows | `git update-index --chmod=+x <file>`, then commit; `git ls-files -s <file>` shows `100755` |

Server behaviour:

| Symptom | Cause and fix |
|---|---|
| Client registration answers `invalid_redirect_uri` | the redirect URI needs a host: `org.badmosip.nativesso.superapp://oauth/callback`, not `io.ps03.superapp:/callback` |
| A token call answers `invalid_client` | client authentication runs first; send a `private_key_jwt` assertion whose audience is the issuer. The upstream Postman folders use the endpoint URL and get this error |
| Headless login fails at the captcha step | send a non-empty `captcha_token`; with no validator any value passes, an empty one does not. In the browser, tick the box |
| Token exchange rejects the token type | `subject_token_type` is `urn:ietf:params:oauth:token-type:id_token` (underscore), not `id-token` |
| The id_token in a token exchange is expired | normal: eSignet id_tokens live 120 s, and the Native SSO exchange accepts an expired one while its device secret and session live (decision D8) |
| Redis runtime store fails at PAR | Redis must be 6.2 or newer |

Android and the emulator (measured 2026-10-04 on AVD `ps03_api34`, Google APIs x86_64, API 34, Chrome 113):

| Symptom | Cause and fix |
|---|---|
| Custom Tab pages blank, "System UI isn't responding" | start the emulator with `-gpu host`, not `-gpu swiftshader_indirect`: `emulator -avd ps03_api34 -port 5584 -gpu host -memory 2048` |
| First request after boot fails | the network comes up a little after boot; wait for it |
| Chrome first-run screen breaks a scripted login | `with-emulator.sh` and `demo.sh` turn it off through a command-line file; if the AVD was wiped run `bash scripts/demo/reset.sh --device` |
| `am start -W` never returns | it hangs after a Custom Tab launch; use `am start` and wait for an app log line in `logcat` |
| Bind refused or family permission not granted | both apps must come from the same build machine (same keystore). `INSTALL_FAILED_DUPLICATE_PERMISSION` means a differently signed app declares the family permission: uninstall both apps and install your own builds |
| `INSTALL_FAILED_UPDATE_INCOMPATIBLE` | an app on the emulator has another signing certificate; the scripts uninstall and reinstall it |
| `verify-family-signing.sh` finds no `apksigner` | install `build-tools;34.0.0` and set `ANDROID_HOME` |

Cleanup by hand: `adb reverse --remove-all`, `adb shell am clear-debug-app`, `adb -s emulator-5584 emu kill`.
