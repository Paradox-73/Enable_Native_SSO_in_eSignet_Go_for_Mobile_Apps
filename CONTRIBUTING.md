# Contributing

How to change this repository: the extra setup a contributor needs, the developer commands, the conventions, and the
way a change reaches `main`. Installing the tools, ports, running the stack and the apps, and troubleshooting are in
[docs/setup.md](docs/setup.md); every script and setting is in [scripts/README.md](scripts/README.md).

## One-time setup

Install the tools and check the clone as [docs/setup.md](docs/setup.md#install-and-check) says, then set your own
identity for this clone:

```bash
git config user.name  "Your Name"
git config user.email "an-email-verified-on-your-github-account@example.com"
bash scripts/install-hooks.sh          # prints: install-hooks: core.hooksPath=scripts/hooks
```

`install-hooks.sh` sets two local git settings: `core.hooksPath=scripts/hooks` (a pre-commit hook that keeps executable
bits on Windows) and `core.longpaths=true` (upstream test files have long paths).

The scripts keep keys, logs, caches and test results out of the repository, under `$PS03_RUN_DIR`. The defaults work on
a fresh clone; the variables that move them are listed in [scripts/README.md](scripts/README.md#settings).

## Developer commands

The test and lint commands, with the line each prints when it passes, are in
[docs/setup.md](docs/setup.md#tests-and-checks). Commands only a contributor needs:

- Coverage of your change: `go-test.sh ... --changed-cover` prints, per Go file changed since the folder's `import:`
  commit, the coverage of the changed lines only. Keep it at 85 % or more.
- One test by name: `bash scripts/go.sh thunder/backend test -count=1 -v -run 'NativeSSO' ./internal/oauth/...`.
- Regenerate Thunder mocks (never edit mock files by hand):
  `bash scripts/go.sh thunder/backend run github.com/vektra/mockery/v3@v3.5.5 --config .mockery.public.yml`, then
  the same with `--config .mockery.private.yml`; `git status --porcelain thunder/` shows what changed.
- Test logs, JSON streams and cover profiles land in `$PS03_RUN_DIR/test-results/`.
- Android test reports: `android/<module>/build/reports/tests/`. More on the apps: [android/README.md](android/README.md).
- After a change that affects a test result, rerun `bash scripts/evidence.sh --run` (or record an emulator result in
  `docs/test-evidence.tsv` and run `bash scripts/evidence.sh`). Never edit `docs/test-report.md` by hand.

## Conventions

- **Commit messages**: conventional form with a scope, statement task number in the subject when one applies:
  `feat(thunder): accept device-secret actor tokens in token exchange (task 9)`. Prefixes in use: `feat`, `fix`,
  `test`, `docs`, `build`, `ci`, `chore`, `import`; scopes `thunder`, `esignet`, `android`, `e2e`.
- **Sign-off**: every commit is made with `git commit -s` from your own GitHub account (your own `user.name` and an
  email verified on that account), so the history shows who wrote what.
- **Pull requests**: one change per pull request, small enough to review in one sitting. A teammate reviews and
  approves before merge. The description quotes the commands you ran, their result lines and the date.
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
  planned or `NOT RUN`. A number that the test report holds is linked, not copied.
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

## Make a change

Every work package in [docs/STATUS.md](docs/STATUS.md) is DONE; new work follows the same steps.

1. Branch from the latest `main`: `git switch main && git pull && git switch -c <short-name>`.
2. Edit only the folders your change needs. Folder ownership (lanes) is listed in [docs/STATUS.md](docs/STATUS.md#all-tasks).
3. Work in small commits (`git commit -s`), running the module's tests as you go.
4. Before the pull request: run the tests of every module you touched, `bash scripts/repo-lint.sh`, and
   `bash make.sh docs-check`; update the docs your change makes wrong.
5. Push the branch and open the pull request; ask a teammate to review.
