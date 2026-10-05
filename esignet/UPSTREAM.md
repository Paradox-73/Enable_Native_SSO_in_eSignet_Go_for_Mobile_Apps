# eSignet Go: upstream import and team changes

This folder holds eSignet Go, MOSIP's login server (`esignet-service/`, a Go service that embeds the Thunder ID
engine for every OAuth 2.0 and OpenID Connect endpoint), plus the team's changes for OpenID Connect Native SSO
for Mobile Apps 1.0 ("Native SSO"). This file says where the code came from, how to check that the import is
unchanged, how the service is built against the Thunder copy in this repository, and what the team changed.

## Source

| Item | Value |
|---|---|
| Upstream repository | [mosip/esignet](https://github.com/mosip/esignet) |
| Version | v2.0.0 (tag on the commit below) |
| Commit | `db528423dac7898ad546cb7b2f5f867a949676a7` (2026-09-25) |
| Licence | MPL-2.0 ([LICENSE](LICENSE), unchanged); bundled dependencies in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt) |
| Import commit in this repository | `4eac1cc` (`import: upstream mosip/esignet@db528423 (MPL-2.0)`) |
| Imported paths | `LICENSE`, `README.md`, `THIRD-PARTY-NOTICES.txt`, `docs/`, `postman-collection/`, `docker-compose/`, `esignet-service/` without one contributor-tool instruction file (274 files; 241 of the 242 files in `esignet-service/`) |
| Allow-list | [`../scripts/imports/esignet.manifest`](../scripts/imports/esignet.manifest) |

- The files were taken with `git archive` of the upstream commit, line-ending conversion off, so every file is
  byte for byte the upstream blob, and `docker-entrypoint.sh` keeps its executable bit.
- Left out (703 files): the web login UI (`oidc-ui/`), Helm charts and deployment scripts, database setup and
  upgrade scripts for cluster deployments, API and UI test suites, performance tests, partner onboarding, editor
  and CI settings. This work changes none of them. `bash scripts/verify-import.sh esignet --list-excluded`
  prints every left-out file.

## Checking the import

Run from the repository root:

```bash
bash scripts/verify-import.sh esignet      # compares the import commit with the upstream commit
git diff 4eac1cc -- esignet/               # every team change since the import
```

`verify-import.sh` compares the blob id and file mode of every allow-listed file and ends with
`0 differ, 0 missing` when the import commit is identical to upstream.

## Building against the in-repo Thunder

Upstream eSignet fetches Thunder as a Go module pinned to the same commit this repository imports
(`thunder-id/thunderid@12f517c4`, v1.0.1):

```text
replace github.com/thunder-id/thunderid => github.com/thunder-id/thunderid/backend v0.0.0-20260825062603-12f517c43840
```

The team's Native SSO rules live in Thunder (`../thunder/`), so eSignet must compile against that copy. The
`build:` commit after the import changes exactly one line of `esignet-service/go.mod`:

```text
replace github.com/thunder-id/thunderid => ../../thunder/backend
```

and `go mod tidy` then removes the two `go.sum` lines of the fetched Thunder module, which are no longer used.
No other dependency changes: the in-repo Thunder starts from the same commit, so its own `go.mod` requirements
are the ones upstream eSignet already resolved.

Consequences of the relative path:

- `esignet/` and `thunder/` must sit next to each other, as they do in this repository. `scripts/go.sh` mounts
  the whole repository into the Go container for this reason.
- Do not run upstream's `./make.sh update-thunder`: it rewrites the `replace` line back to a fetched Thunder
  commit and would silently drop the team's Thunder changes.
- Upstream's `esignet-service/Dockerfile` uses `esignet-service/` as its build context and runs
  `go mod download` before copying the sources, so it cannot see `../../thunder/backend`. The local stack builds
  the image with a Dockerfile whose context is the repository root (work package WP-06); the upstream file stays
  unchanged.

Build and test commands (Go runs in a container; see [`../scripts/README.md`](../scripts/README.md)):

```bash
bash scripts/go.sh esignet/esignet-service build -o /tmp/esignet ./cmd/esignet
bash scripts/go-test.sh esignet/esignet-service ./...
bash scripts/go-lint.sh esignet/esignet-service
```

## Team changes

New Go files inside `esignet/` carry eSignet's MPL-2.0 notice block, and the changes follow eSignet's package
layout (`internal/engine/` providers, `internal/config/` settings with `MOSIP_ESIGNET_*` environment overrides,
`data/deployment.yaml` defaults), error handling and tests, so they can be offered upstream.

| Work package | Change | Status |
|---|---|---|
| WP-03 | Unchanged import (`4eac1cc`); `replace` line pointing at `../../thunder/backend` (`3a0e127`) | done |
| WP-08 | Device Secret Provider (signed, then encrypted device secret with eSignet's keymanager; session records in the runtime store), Native SSO settings, client registration for the token-exchange grant and the `device_sso` scope | done (unit tests; not yet run against the live stack) |
| WP-14 | Native SSO in `docs/esignet-openapi.yaml` and a "Native SSO" folder in the Postman collection | planned |
| R27 | `EndDeviceSecretSession` in the Device Secret Provider (deletes the session record; a missing record is not an error); `/oauth2/revoke` in `docs/esignet-openapi.yaml`; three revocation requests in the Postman folder | done (unit tests; live test `TestE2E_Revoke_DeviceSecret`) |

## Results

Measured on 2026-10-04 in the `golang:1.26-bookworm` container (Go 1.26.8), after the `build:` commit:

- `bash scripts/verify-import.sh esignet`: 274 files, 274 same, 0 differ, 0 missing, 0 extra.
- `bash scripts/go.sh esignet/esignet-service build -o /tmp/esignet ./cmd/esignet`: exit 0.
- `bash scripts/go-test.sh esignet/esignet-service ./...`: 29 packages, 1437 tests: 1437 passed, 0 skipped,
  0 failed (upstream's own tests, unchanged, now compiled against `../thunder/backend` with the Native SSO
  provider contract of WP-02).

Measured on 2026-10-05 after the WP-08 changes (same container):

- `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover`: 29 packages, 1537 tests: 1537 passed,
  0 skipped, 0 failed; changed lines 166/181 statements covered (91.7 %), `internal/engine/device_secret_provider.go`
  128/132 (97.0 %).
- `bash scripts/go-lint.sh esignet/esignet-service`: 0 issues.
- At that time no login against the running service existed; since WP-06 the live e2e suite (`e2e/`) runs
  against the stack, which creates the `DEVICE_SECRET` key in Postgres at start-up (see `docs/test-report.md`).

Measured on 2026-10-05 after the R27 change (device secret revocation):

- `bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover`: 29 packages, 1588 tests: 1588 passed,
  0 skipped, 0 failed; changed lines 318/339 (93.8 %), `internal/engine/device_secret_provider.go` 135/139 (97.1 %).
- `bash scripts/go-lint.sh esignet/esignet-service`: 0 issues.
