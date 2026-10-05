# Third-party code and tools

Everything in this repository that the team did not write, with its source, version and licence.
All licences are OSI-approved.

## Imported upstream source

Each import is one commit, `import: upstream <repo>@<commit> (<licence>)`, holding upstream files byte for byte
(taken with `git archive`, line-ending conversion off). Team changes come in later commits, so judges can diff
against the import. The allow-list of imported paths is machine-readable in `scripts/imports/<name>.manifest`;
`bash scripts/import-upstream.sh <name>` copies exactly those paths and `bash scripts/verify-import.sh <name>`
checks every file's bytes and mode against the upstream commit.

| Folder | Upstream | Version | Commit | Licence | Imported paths | Status |
|---|---|---|---|---|---|---|
| `thunder/` | [thunder-id/thunderid](https://github.com/thunder-id/thunderid) | v1.0.1 | `12f517c4384004e360e26eb7eb1ab0501b678bb2` | Apache-2.0 | `LICENSE`, `api/oauth2.yaml`, `backend/` (1,678 of its 1,679 files) | imported (`c4c5443`, WP-02); team changes on top: [thunder/UPSTREAM.md](thunder/UPSTREAM.md) |
| `esignet/` | [mosip/esignet](https://github.com/mosip/esignet) | v2.0.0 | `db528423dac7898ad546cb7b2f5f867a949676a7` | MPL-2.0 | `LICENSE`, `README.md`, `THIRD-PARTY-NOTICES.txt`, `docs/`, `postman-collection/`, `docker-compose/`, `esignet-service/` (241 of its 242 files) | imported (`4eac1cc`, WP-03) |

- Left out of `backend/` and `esignet-service/`: one contributor-tool instruction file at the top of each,
  not needed to build, test or document the code. Everything else upstream (web frontends, Helm charts, UI test
  suites, other services) is left out because this work does not change it.
  `bash scripts/verify-import.sh <name> --list-excluded` prints every file left out
  (dry run on 2026-10-04: Thunder 4,257 files, eSignet 703 files).
- Why these versions: eSignet v2.0.0 pins exactly this Thunder commit in `esignet-service/go.mod`, and both
  have released container images and documentation that match.
- New Go files inside an imported folder carry that upstream's licence header (Thunder: Apache-2.0 SPDX line;
  eSignet: the MPL-2.0 notice block), so the change can be offered upstream.

### Key material inside the imports

Thunder's import contains four test fixtures with private keys that protect nothing: three ML-DSA test keys
(`backend/internal/system/kmprovider/defaultkm/pki/testdata/mldsa{44,65,87}.key`) and one unit test with an
embedded PEM key (`backend/internal/system/utils/configutil_test.go`). They are listed in `.gitguardian.yaml`;
`scripts/repo-lint.sh` fails on any other private key. The eSignet import has none (checked 2026-10-04).

## Tools used by the scripts (not part of the delivered code)

| Tool | Version | Licence | Used by |
|---|---|---|---|
| Go toolchain, `golang` container image | 1.26 (`golang:1.26-bookworm`; 1.26.8 on 2026-10-04) | BSD-3-Clause | `scripts/go.sh`, `go-test.sh`, `go-lint.sh` |
| golangci-lint | v1.64.8 (Thunder's pin) and v2.14.0 (eSignet uses the latest v2) | GPL-3.0 | `scripts/go-lint.sh` |
| Redocly CLI | 1.34.20 | MIT | `scripts/openapi-lint.sh` |
| newman (Postman CLI) | 6.2.2 | Apache-2.0 | `esignet/postman-collection/native-sso/run.sh`, `scripts/evidence.sh` |
| Mermaid CLI output (PNG diagrams) | see `docs/diagrams/README.md` | MIT | diagram images in `docs/diagrams/` |

## Android app dependencies

Declared in `android/gradle/libs.versions.toml`; downloaded by Gradle, not stored in this repository
(except the Gradle wrapper files listed first).

| Component | Version | Licence | Used by |
|---|---|---|---|
| Gradle wrapper (`android/gradlew`, `gradlew.bat`, `gradle/wrapper/gradle-wrapper.jar`) | 8.7 | Apache-2.0 | building the Android project |
| Android Gradle Plugin | 8.6.0 | Apache-2.0 | build |
| Kotlin (compiler, Gradle plugins, standard library) | 2.0.20 | Apache-2.0 | all modules |
| AndroidX Core KTX | 1.13.1 | Apache-2.0 | `superapp`, `childapp` |
| AndroidX Activity Compose | 1.9.2 | Apache-2.0 | `superapp`, `childapp` |
| Jetpack Compose BOM, Material 3 | 2024.09.02 | Apache-2.0 | `superapp`, `childapp` |
| OkHttp | 4.12.0 | Apache-2.0 | `oidc-client` (HTTP) |
| Nimbus JOSE + JWT | 10.10 | Apache-2.0 | `oidc-client` (JWS/JWK parsing, id_token signature checks) |
| AndroidX Browser (Custom Tabs) | 1.8.0 | Apache-2.0 | `superapp` (browser login) |
| JUnit | 4.13.2 | EPL-1.0 | tests only |
| AndroidX Test runner, AndroidX Test JUnit extension | 1.6.2, 1.2.1 | Apache-2.0 | instrumented tests only |
| AndroidX UI Automator | 2.3.0 | Apache-2.0 | instrumented tests only (drives Chrome in `SuperAppSignInTest`) |
| OkHttp MockWebServer | 4.12.0 | Apache-2.0 | tests only (`oidc-client`, `superapp`) |
| Robolectric | 4.13 | MIT | JVM tests only (`superapp`, `childapp`: the family binder service and client) |
| AndroidX Test Core | 1.6.1 | Apache-2.0 | JVM tests only, with Robolectric |
| Compose UI test (`ui-test-junit4`, `ui-test-manifest`), version from the Compose BOM 2024.09.02 | 2024.09.02 | Apache-2.0 | JVM screen tests and debug builds (`childapp`, `superapp`) |

## Local stack images

Pulled by `deploy/compose.yaml`; not stored in this repository. The eSignet image is built from this repository.

| Image | Version | Licence | Used for |
|---|---|---|---|
| `mosipid/oidc-ui` | 2.0.0 | MPL-2.0 | login pages |
| `mosipid/mock-identity-system` | 0.14.0 | MPL-2.0 | seeded test identity |
| `postgres` | `bookworm` tag | PostgreSQL licence | eSignet and mock identity databases |

## Go test code

`e2e/` has no dependency outside the Go standard library (`e2e/go.mod` lists none), so the live tests add nothing to
this list.
