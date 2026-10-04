# Native SSO for mobile apps in eSignet (Go)

MOSIP Decode 2026, problem statement 03, team Badmosip.

This repository adds [OpenID Connect Native SSO for Mobile Apps 1.0](https://openid.net/specs/openid-connect-native-sso-1_0.html)
to eSignet Go and the Thunder ID engine it is built on. A user signs in once, with OTP, in one Android app;
a second app signed with the same certificate then signs in silently, by exchanging the first app's
device secret for its own tokens (OAuth 2.0 Token Exchange, RFC 8693).

> **Status on 2026-10-04: work in progress.** Only the repository scaffold and build tooling exist so far.
> Everything else below is marked *planned* until it has been built and run; results are recorded with dates
> in `docs/test-report.md` as they come in.

## What is here

| Part | Folder | Status |
|---|---|---|
| Thunder ID engine (upstream `thunder-id/thunderid` v1.0.1), Native SSO protocol rules | `thunder/` | planned |
| eSignet Go (upstream `mosip/esignet` v2.0.0), device secret provider and settings | `esignet/` | planned |
| Super App and Child App (Kotlin, Android) | `android/` | started: Gradle project, shared signing, binder contract and the OpenID Connect client library with 64 JVM tests ([android/README.md](android/README.md)); app screens are shells |
| Local stack: eSignet, mock identity system, Postgres (docker compose) | `deploy/` | planned |
| Live integration tests against the local stack | `e2e/` | planned |
| Design, threat model, API docs, test report, demo script | `docs/` | index only ([docs/README.md](docs/README.md)) |
| Build, test, lint and import-check scripts | `scripts/` | done ([scripts/README.md](scripts/README.md)) |

Upstream code is imported unchanged in its own `import:` commit before any team change, so `git diff` against
that commit shows exactly what the team wrote. `bash scripts/verify-import.sh <thunder|esignet>` proves it
byte for byte. Sources, commits and licences: [THIRD_PARTY.md](THIRD_PARTY.md).

## Quick start

Prerequisites: Git, Bash (Git Bash on Windows), Docker (Linux containers), Node.js 20 or later (for the OpenAPI
lint only). Go itself runs in the `golang:1.26-bookworm` container, so no local Go install is needed.

```bash
git clone <this repository> && cd esignet-native-sso
bash scripts/install-hooks.sh          # local git settings: hooks path, long paths on Windows
bash scripts/repo-lint.sh              # words, keys, licence headers, line endings, sizes
bash scripts/go.sh . version           # Go 1.26 in the container
```

Running the stack, the apps and the demo: *planned*; the steps will be in `docs/setup.md` and
`docs/demo-script.md`.

## Developer commands

| Command | What it does |
|---|---|
| `bash scripts/go.sh <module> <go args>` | `go` for one module, in the Linux container |
| `bash scripts/go-test.sh <module> [pkgs] [--changed-cover]` | tests with a one-line summary; coverage of changed lines only |
| `bash scripts/go-lint.sh <module>` | the module's own golangci-lint version and config, on lines changed since the import |
| `bash scripts/openapi-lint.sh <file>` | Redocly lint; fails only if the team's edits added errors |
| `bash scripts/verify-import.sh <name>` | imported folder equals the upstream commit, byte for byte |
| `bash scripts/repo-lint.sh` | repository hygiene checks |

Details, outputs and measured run times: [scripts/README.md](scripts/README.md).

## Licence

Team code: MIT ([LICENSE](LICENSE)). Imported code keeps its own licence: Thunder Apache-2.0
(`thunder/LICENSE`), eSignet MPL-2.0 (`esignet/LICENSE`). See [THIRD_PARTY.md](THIRD_PARTY.md).
