# Contributing

How to set up a computer for this repository, build and test it, and take a task from
[docs/STATUS.md](docs/STATUS.md) to a merged pull request. Everything here was run on Windows 11 with Git Bash and
Docker Desktop unless a line says otherwise; Linux and macOS differences are noted where they are known, and
macOS has not been tried by the team.

## Contents

1. [Tools](#tools)
2. [One-time setup](#one-time-setup)
3. [Build and test](#build-and-test)
4. [Local stack and ports](#local-stack-and-ports)
4. [What runs end to end today](#what-runs-end-to-end-today)
5. [Conventions](#conventions)
7. [Pick up a task](#pick-up-a-task)
8. [Troubleshooting](#troubleshooting)

## Tools

| Tool | Version | Needed for | Download |
|---|---|---|---|
| Git, with Git Bash on Windows | any recent | everything; every command here is a Bash command | <https://git-scm.com/downloads> |
| Docker Desktop (Windows, macOS) or Docker Engine with the compose plugin (Linux) | Linux containers; give the Docker VM at least 4 GB of memory (the team laptop uses 8 GB) | Go builds, tests and lint (all Go runs in the `golang:1.26-bookworm` container), the local stack | <https://docs.docker.com/get-started/get-docker/> |
| JDK | 17 or newer (the team uses 21) | Gradle, Android build, `keytool` | <https://adoptium.net/temurin/releases/> |
| Android SDK: platform `android-34`, `build-tools;34.0.0`, `platform-tools`; later `emulator` and `system-images;android-34;google_apis;x86_64` | as listed | Android apps, `apksigner`, `adb`, the emulator | Android Studio <https://developer.android.com/studio> or the command-line tools on the same page |
| Node.js with `npx` | 20 or newer | only `scripts/openapi-lint.sh` (it downloads Redocly CLI 1.34.20 on first run) | <https://nodejs.org/en/download> |
| Python 3 | any 3.x | two acceptance one-liners in `docs/STATUS.md` | <https://www.python.org/downloads/> |

Not needed on your machine: Go (it runs in the container, Go 1.26.8 on 2026-10-04), golangci-lint (v1.64.8 for
Thunder and v2.14.0 for eSignet are built into a Docker volume on first use), mockery (run with `go run`), Gradle
(the wrapper downloads 8.7 with a pinned checksum).

Differences by system:

- **Windows**: run every command in Git Bash, not CMD or PowerShell. Start Gradle with `bash gradlew`. Docker
  Desktop must run Linux containers.
- **Linux**: let your user run Docker without `sudo` (<https://docs.docker.com/engine/install/linux-postinstall/>).
  `./gradlew` works directly (the file is stored executable).
- **macOS**: untested. On Apple silicon the emulator needs the `arm64-v8a` system image instead of `x86_64`.

## One-time setup

```bash
git clone -c core.longpaths=true https://github.com/Paradox-73/Enable_Native_SSO_in_eSignet_Go_for_Mobile_Apps.git esignet-native-sso
cd esignet-native-sso
git config user.name  "Your Name"
git config user.email "an-email-verified-on-your-github-account@example.com"
bash scripts/install-hooks.sh          # prints: install-hooks: core.hooksPath=scripts/hooks
bash scripts/go.sh . version           # prints: go version go1.26.x linux/amd64 (first run pulls the image)
bash scripts/repo-lint.sh              # prints: repo-lint: OK
```

- Fixed on 2026-10-05 (task WP-07): `repo-lint.sh` printed `FAILED (1 problems)` on `main` at `1bd6fd0` for a
  phrase on its word list in `thunder/UPSTREAM.md:114`; that line is reworded and the check prints `repo-lint: OK`.

- `install-hooks.sh` sets two local git settings: `core.hooksPath=scripts/hooks` (a pre-commit hook that keeps
  executable bits on Windows) and `core.longpaths=true` (upstream test files have long paths).
- `core.longpaths=true` on the clone command matters on Windows: without it some upstream files fail to check out.

### Folders outside git

The scripts keep keys, logs, caches and test results out of the repository. Defaults work on a fresh clone; set
the variables only to move the folders.

| Variable | Default on a fresh clone | Holds |
|---|---|---|
| `PS03_RUN_DIR` | `../run` next to the clone if that folder exists, else `run/` inside the clone (ignored by git) | `keys/` (Android signing keystore), `test-results/`, `logs/`, `evidence/`, `locks/`, `upstream-cache/` |
| `PS03_UPSTREAM_DIR` | `../upstream` if it exists, else `$PS03_RUN_DIR/upstream-cache` | upstream clones for import checks; when missing, the needed commit is fetched from GitHub |
| `PS03_LOCK_DIR` | `$PS03_RUN_DIR/locks` | lock folders, so only one Go container of this project runs at a time |
| `PS03_GO_IMAGE` | `golang:1.26-bookworm` | Go container image |
| `PS03_GO_MEMORY`, `PS03_GO_LINT_MEMORY`, `PS03_GO_CPUS` | `2g`, `3g`, `6` | container limits; the CPU count is lowered to what Docker has |
| `PS03_GO_TEST_P` | `4` | packages tested in parallel |
| `PS03_FAMILY_KEYSTORE`, `PS03_FAMILY_KEYSTORE_PASSWORD` | `$PS03_RUN_DIR/keys/ps03-family-test.jks` and a random password in the file next to it | the test certificate that signs both Android apps |

Docker volumes created by the scripts: `ps03-gomod` (Go modules), `ps03-gocache` (build cache), `ps03-gotools`
(golangci-lint binaries).

### Android

```bash
export JAVA_HOME=/path/to/jdk-21                 # Git Bash form of a Windows path: /c/Program Files/...
export ANDROID_HOME=/path/to/Android/Sdk
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "platforms;android-34" "build-tools;34.0.0" "platform-tools"
```

- On Windows the tool is `sdkmanager.bat` in the same folder.
- Android Studio installs the SDK and `sdkmanager` for you; a `local.properties` file with `sdk.dir=...` in
  `android/` also works (ignored by git).
- The first build creates your own signing keystore with `keytool` in `$PS03_RUN_DIR/keys/`. Every person gets a
  different certificate; that is expected.

## Build and test

Run from the repository root. Each script prints one result line on stdout and exits non-zero on failure;
`bash scripts/<name>.sh --help` prints its usage. Full details: [scripts/README.md](scripts/README.md).

| Command | Result line when it passes | Measured 2026-10-04 |
|---|---|---|
| `bash scripts/repo-lint.sh` | `repo-lint: OK` | OK on 2026-10-05 (1 problem on 2026-10-04, fixed by WP-07) |
| `bash scripts/verify-import.sh thunder` | `verify-import: thunder: N files, N same, 0 differ, 0 missing, 0 extra` | 1,680 files |
| `bash scripts/verify-import.sh esignet` | same form | 274 files |
| `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/...` | `go-test: thunder/backend: ... 0 failed` | 2214 tests, 0 failed |
| `bash scripts/go-test.sh esignet/esignet-service ./...` | `go-test: esignet/esignet-service: ... 0 failed` | 29 packages, 1437 tests, 0 failed |
| `bash scripts/go.sh esignet/esignet-service build -o /tmp/esignet ./cmd/esignet` | exit status 0 | exit 0 |
| `bash scripts/go-lint.sh thunder/backend` | `go-lint: thunder/backend: OK (...)` | 0 issues; first cold run 8 min 53 s |
| `bash scripts/go-lint.sh esignet/esignet-service` | `go-lint: esignet/esignet-service: OK (...)` | 0 issues, 41 s |
| `bash scripts/openapi-lint.sh esignet/docs/esignet-openapi.yaml` | `openapi-lint: ...: OK` | 0 errors, 8 warnings (upstream's own) |
| `bash scripts/docs-check.sh docs/design.md docs/threat-model.md` | `docs-check: 0 broken links; tasks traced 11/11; threats with mitigation and test 11/11` | as shown |

- `go-test.sh ... --changed-cover` also prints, per Go file changed since the folder's `import:` commit, the
  coverage of the changed lines only. Task acceptance checks ask for 85 % or more.
- Run one test by name: `bash scripts/go.sh thunder/backend test -count=1 -v -run 'NativeSSO' ./internal/oauth/...`.
- Regenerate Thunder mocks (never edit mock files by hand):
  `bash scripts/go.sh thunder/backend run github.com/vektra/mockery/v3@v3.5.5 --config .mockery.public.yml`, then
  the same with `--config .mockery.private.yml`; `git status --porcelain thunder/` shows what changed.
- Test logs, JSON streams and cover profiles land in `$PS03_RUN_DIR/test-results/`.

Android (from `android/`; on Linux and macOS write `./gradlew`):

```bash
cd android
bash gradlew --no-daemon :oidc-client:test assembleDebug   # BUILD SUCCESSFUL; 64 tests, 0 failures (2026-10-04)
bash scripts/verify-family-signing.sh                      # family-signing: OK <sha-256>
```

- Test report: `android/oidc-client/build/reports/tests/test/index.html`.
- APKs: `android/superapp/build/outputs/apk/debug/superapp-debug.apk` and
  `android/childapp/build/outputs/apk/debug/childapp-debug.apk`.
- More, including the binder contract and the client library: [android/README.md](android/README.md).

## Local stack and ports

The project's own stack (`deploy/`, compose project `ps03-native-sso`): eSignet built from this repository with the
in-repo Thunder, Postgres, the mock identity system and the oidc-ui login pages. Details:
[deploy/README.md](deploy/README.md).

| Port on your machine | Service |
|---|---|
| 38088 | eSignet API; issuer `http://localhost:38088` (the Android apps' default `ps03.issuer`) |
| 38300 | oidc-ui login pages |
| 38082 | mock identity system |
| 38455 | Postgres |
| 38379 | Redis (reserved; the stack uses the in-memory store) |
| 38990-38999 | spare, for test servers |
| 5584 / 5585 | emulator console / adb (serial `emulator-5584`) |

```bash
bash scripts/with-stack.sh bash e2e/run.sh   # up, stack smoke, the live e2e tests, always down -v
bash deploy/stack.sh up | smoke | status | logs | down
```

- `with-stack.sh` holds the `docker-stack` lock while the stack runs; `PS03_KEEP_STACK=1` leaves it running.
- Postgres and keystore passwords are generated on first use into `$PS03_RUN_DIR/stack.env`, outside git.
- Seeded user (upstream [esignet/docker-compose/README.md](esignet/docker-compose/README.md)): UIN `1774231323`,
  PIN `545411`, OTP `111111`.
- 2026-10-05: `with-stack.sh bash e2e/run.sh -run 'TestE2E_FAPI2Login|TestE2E_PSUTProbe'` -> `stack smoke: PASS`,
  `e2e: 2 passed, 0 failed`, 87 s from start to teardown.

## What runs end to end today

Native SSO works on the server side and is tested live; the apps are tested on the JVM and on the emulator. Results of
2026-10-06, in order (full detail: [docs/test-report.md](docs/test-report.md)):

1. `bash scripts/repo-lint.sh` -> `repo-lint: OK`.
2. `bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover` -> 2379 tests, 0 failed.
3. `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover` -> 1592 tests, 0 failed.
4. `bash scripts/with-stack.sh bash e2e/run.sh` -> `e2e: 31 passed, 0 failed` (includes R27, device secret revocation, and R28, consent-only re-consent).
5. `bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh` -> 34 requests, 125 assertions, 0 failed.
6. Android JVM tests (`oidc-client` 67, `secure-store` 13, `superapp` 64, `childapp` 54, 0 failed) and
   `verify-family-signing.sh` -> `family-signing: OK`.
7. `bash scripts/demo/demo.sh --no-emulator` -> `demo ready (server part only; emulator steps NOT RUN)`, `teardown ok`.
8. On the emulator: `bash android/scripts/run-ui-test.sh all` -> `OK (5 tests)`; `run-family-tests.sh` -> every step PASS;
   `bash scripts/demo/demo.sh --check` -> `demo ready`, `teardown ok`.
9. Every job of `.github/workflows/ci.yml` replayed in Linux containers: all passed (row CI-LINUX). On GitHub
   the same workflow passed every job on 2026-10-06 (row CI, run 37464932756).

Short names for these: `bash make.sh help`.

## Conventions

- **Commit messages**: conventional form with a scope, statement task number in the subject when one applies:
  `feat(thunder): accept device-secret actor tokens in token exchange (task 9)`. Prefixes in use: `feat`, `fix`,
  `test`, `docs`, `build`, `ci`, `chore`, `import`; scopes `thunder`, `esignet`, `android`, `e2e`.
- **Sign-off**: every commit is made with `git commit -s` from your own GitHub account (your own `user.name` and an
  email verified on that account), so the history shows who wrote what.
- **Pull requests**: one task, or one clear part of a task, per pull request; small enough to review in one
  sitting. A teammate reviews and approves before merge. The description quotes the acceptance command, its
  result lines and the date.
- **Upstream code**: imported unchanged, in its own `import:` commit, before any change to it. Use
  `bash scripts/import-upstream.sh <name>`, then `bash scripts/verify-import.sh <name>` (must print
  `0 differ, 0 missing`), then commit only that folder. Never edit files inside an `import:` commit. Never run
  eSignet's `./make.sh update-thunder`: it points eSignet back at a downloaded Thunder and drops the team's changes.
- **Licence headers**: new files in `thunder/` use Thunder's header (`// Copyright 2026 The ThunderID Authors` and
  `// SPDX-License-Identifier: Apache-2.0`); new files in `esignet/` use eSignet's MPL-2.0 notice block; every
  other team file carries `SPDX-License-Identifier: MIT`. `repo-lint.sh` checks this.
- **Dependencies**: only OSI-approved licences; each new one is listed with its version and licence in
  `THIRD_PARTY.md`.
- **No secrets in git**: keys, keystores and stack passwords are created at run time in `$PS03_RUN_DIR`.
  `repo-lint.sh` fails on any private-key block or keystore file, except the upstream test fixtures listed in
  `.gitguardian.yaml`. Never log tokens, device secrets, OTPs, UINs or claim values.
- **Docs claim only what was run**: every result carries the date and the command; anything not run is marked
  planned or `NOT RUN`.
- **Code style**: follow the upstream code around you (Thunder `common.ServiceError` with i18n keys, testify
  suites, mocks only from mockery; eSignet `internal/engine/` providers and `MOSIP_ESIGNET_*` settings). Comments
  say why and cite spec sections. Unit tests for Native SSO carry `NativeSSO`, `DeviceSSO` or `DeviceSecret` in
  their names; live tests are named `TestE2E_*`.
- **Line endings**: LF everywhere (`.editorconfig`). `.gitattributes` stores bytes unchanged, so an editor that
  saves CRLF changes the file; `repo-lint.sh` reports it.
- **Executable scripts on Windows**: Git for Windows does not record the executable bit. For a new script run
  `git update-index --add --chmod=+x <file>` before committing; docs always call scripts with `bash` anyway.
- **Commit message check**: `repo-lint.sh` also reads every commit message and rejects a short list of tool and
  vendor names and some trailers. Run it before pushing; fixing a pushed message means rewriting history.

## Pick up a task

1. Open [docs/STATUS.md](docs/STATUS.md), choose a TODO or PARTIAL task whose dependencies are DONE and whose lane
   nobody else holds. Tell the team you are taking that lane.
2. Branch from the latest `main`:

   ```bash
   git switch main && git pull
   git switch -c wp-08-device-secret-provider
   ```

3. Read the task card: goal, owned folders, acceptance check, notes. Edit only the folders the card owns.
4. Work in small commits (`git commit -s`), running the module's tests as you go.
5. Before the pull request: run the acceptance check on the final commit, `bash scripts/repo-lint.sh`, and update
   the task's row and card in `docs/STATUS.md` with the date, result lines and commit ids.
6. Push the branch and open the pull request; ask a teammate to review.
7. If you stop before finishing, push your branch and write in the card what is done, what is left and the branch
   name, so the next person can continue.

## Troubleshooting

Go and Docker:

- **Go test binaries blocked on Windows** (`An Application Control policy has blocked this file`, a different set of
  packages each run): do not run `go test` on Windows directly; use `scripts/go-test.sh`, which runs in Linux.
  eSignet's keymanager tests also fail natively on Windows ("symmetric key not found") and pass in the container.
- **Waiting for lock go-PS03**: another Go container of this project is running. A lock older than 90 minutes is
  removed as stale. If no `golang` container runs (`docker ps`), remove it yourself:
  `rmdir "$PS03_LOCK_DIR/go-PS03"` (by default `$PS03_LOCK_DIR` is `$PS03_RUN_DIR/locks`).
- **Lint times out on a busy machine**: `bash scripts/go-lint.sh thunder/backend -- --timeout=30m`.
- **Container killed for memory**: raise Docker's memory, or run fewer things at once; limits are `PS03_GO_MEMORY`
  and `PS03_GO_LINT_MEMORY`.
- **Broken or stale Go caches**: `docker volume rm ps03-gomod ps03-gocache ps03-gotools` (the next run is slow).
- **eSignet cannot find `data/deployment.yaml`**: it reads it relative to the working directory; start it from
  `esignet/esignet-service/`.
- **Upstream `esignet/esignet-service/Dockerfile` fails**: expected; it cannot see `../../thunder/backend`. The
  project image is built by `deploy/esignet.Dockerfile`, with the repository root as context.
- **`verify-import.sh` needs the network** on a fresh clone: it fetches the pinned upstream commit into
  `$PS03_RUN_DIR/upstream-cache/` (or uses clones in `$PS03_UPSTREAM_DIR`).

Server behaviour:

- **Client registration answers `invalid_redirect_uri`**: the redirect URI needs a host, for example
  `org.badmosip.nativesso.superapp://oauth/callback`, not `io.ps03.superapp:/callback`.
- **A token call answers `invalid_client`**: client authentication runs before anything else; send a valid
  `private_key_jwt` client assertion with audience = issuer.
- **Headless login fails at the captcha step**: send a non-empty `captcha_token`; with no validator configured any
  value passes, an empty one does not.
- **Token exchange rejects the token type**: `subject_token_type` is `urn:ietf:params:oauth:token-type:id_token`
  (underscore), not `id-token`.
- **The id_token in a token exchange is expired**: normal; eSignet id_tokens live 120 s. The Native SSO branch
  (WP-10) accepts it while the device secret and its session are valid.
- **Postman collection fails under newman**: the upstream scripts use WebCrypto and top-level `await`, which newman
  6.2.2 lacks (24 of 26 assertions failed, 2026-10-04). Use the Postman desktop app; the Go e2e suite is the
  automated check.
- **Using Redis for the runtime store**: it must be version 6.2 or newer for PAR.

Android and the emulator (measured 2026-10-04 on AVD `ps03_api34`, Google APIs x86_64, API 34, Chrome 113):

- Create the AVD once:
  `avdmanager create avd -n ps03_api34 -k "system-images;android-34;google_apis;x86_64" -d pixel_6`.
- Start it with `emulator -avd ps03_api34 -port 5584 -gpu host -memory 2048` (add `-no-window -no-audio
  -no-boot-anim` for tests). `-gpu swiftshader_indirect` left Custom Tab pages blank and showed "System UI isn't
  responding". Boot took 78 s with `-gpu host`.
- Right after boot the network may be missing for a short time: wait for it before the first request.
- Reach the laptop from the phone as `localhost`: `adb -s emulator-5584 reverse tcp:38088 tcp:38088` and the same
  for 38300. Do not use `10.0.2.2`: the issuer and DPoP `htu` would differ from what the laptop sees.
- Cleartext HTTP is allowed for `localhost` in debug builds only.
- Chrome's first-run screen breaks a scripted login. Skip it with
  `adb shell 'echo "_ --disable-fre --no-default-browser-check --no-first-run" > /data/local/tmp/chrome-command-line'`
  and `adb shell am set-debug-app --persistent com.android.chrome`; undo with `adb shell am clear-debug-app`.
- Do not use `am start -W` in scripts: it never returned after a Custom Tab launch. Use `am start` and wait for an
  app log line in `logcat`.
- **Family permission not granted / bind refused**: both apps must come from the same build machine (same
  keystore). `INSTALL_FAILED_DUPLICATE_PERMISSION` means a differently signed app declares the family permission;
  uninstall both apps and install your own builds.
- **`verify-family-signing.sh` says no apksigner**: install `build-tools;34.0.0` and set `ANDROID_HOME`.
- Teardown: `adb reverse --remove-all`, `adb shell am clear-debug-app`, `adb -s emulator-5584 emu kill`.

Repository:

- **Some README lines name helper scripts that do not exist in the repository** (a commit helper in
  `scripts/README.md`, an environment script in `android/README.md`): they belong to the first team laptop. Use
  `git commit -s` and the `JAVA_HOME` / `ANDROID_HOME` setup above instead.
- **Executable bit lost after a commit on Windows**: run `git update-index --chmod=+x <file>`, then commit;
  `git ls-files -s <file>` shows `100755` when the bit is stored.
