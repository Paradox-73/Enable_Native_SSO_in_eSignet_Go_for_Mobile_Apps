# Scripts

Build, test, lint and import checks for this repository. Every script runs with `bash` from any folder, prints
its result on stdout (progress goes to stderr) and exits non-zero on failure, so CI can call them as they are.
`bash scripts/<name>.sh --help` prints the usage from the script's header.

## Why Go runs in a container

Go always runs in the `golang:1.26-bookworm` Linux container, with the whole repository mounted at `/repo`:

- Windows Application Control blocked 3 to 33 freshly built test binaries per `go test` run on the team laptops
  (measured 2026-10-04, a different set each time); in the container every package ran.
- `esignet/esignet-service/go.mod` will point at `../../thunder/backend` with a relative `replace` line, which
  resolves only when both folders are mounted together.
- Module, build and tool caches live in the Docker volumes `ps03-gomod`, `ps03-gocache` and `ps03-gotools`
  (bind-mounting a Windows module cache was far too slow).
- The container runs as root, so git inside it is told the mounted repository is safe
  (`safe.directory /repo`); `go build` and `golangci-lint --new-from-rev` both run git.
- A Go container takes the `go-PS03` lock, so at most one runs at a time (2 GB memory cap, 3 GB for lint) on the
  shared Docker VM. A script started by a script that already holds the lock does not wait for itself.

## Commands

| Script | Use | Result line |
|---|---|---|
| `go.sh [--network NAME] [-e K=V] <module> <go args>` | any `go` command for one module | the go command's output |
| `go-test.sh <module> [pkgs] [--changed-cover] [--min N] [-- go test flags]` | unit tests | `go-test: <module>: N packages (...), M tests: P passed, S skipped, F failed` |
| `go-lint.sh <module>` / `go-lint.sh --versions` | golangci-lint with the module's own version and config | `go-lint: <module>: OK (<scope>)` |
| `openapi-lint.sh <file> [--base FILE]` | Redocly lint, compared with the import commit | `openapi-lint: <file>: E errors, W warnings (import: ...): OK` |
| `import-upstream.sh <name>` | copy an upstream commit's allow-listed files | `import-upstream: <folder>: N files from <repo>@<commit>` |
| `verify-import.sh <name> [--worktree] [--list-excluded]` / `--self-test` | prove an import is unchanged | `verify-import: <name>: N files, N same, 0 differ, 0 missing, 0 extra` |
| `repo-lint.sh` / `--self-test` | words, keys, licence headers, line endings, sizes | `repo-lint: OK` |
| `install-hooks.sh` | point git at `scripts/hooks` and enable long paths (local config) | `install-hooks: core.hooksPath=scripts/hooks` |
| `docs-check.sh <file.md>...` / `--self-test` | links, statement-task traceability, threat coverage, test ids, diagram copies | `docs-check: 0 broken links; tasks traced 11/11; threats with mitigation and test N/N` |

`<module>` is a folder with a `go.mod`, relative to the repository root, for example `thunder/backend`,
`esignet/esignet-service`, `e2e` or `.`; `<name>` is an import from `scripts/imports/` (`thunder`, `esignet`).

### go-test.sh

- Runs `go test -count=1 -p 4 -coverprofile -json`; packages default to `./...`. The JSON stream, cover profile
  and stderr are kept in `$PS03_RUN_DIR/test-results/<module>-<time>.*`.
- `--changed-cover` prints, for each Go file changed since the module's `import:` commit (uncommitted work
  included), the coverage of the statements on changed lines only: cover-profile blocks that overlap a line in
  `git diff -U0`. Upstream's own untested code in a touched file does not count. Test files and generated files
  are skipped. For a module the team wrote from scratch every line counts. `--min 80` fails below 80 %.
- Both summaries come from the small standard-library Go tools in `gotools/` (`cmd/testsummary`,
  `cmd/changedcover`), which have their own tests: `bash scripts/go-test.sh scripts/gotools`.

### go-lint.sh

- Thunder pins golangci-lint v1.64.8 (its `Makefile`); eSignet's `make.sh` installs the latest v2, pinned here
  to v2.14.0, the latest v2 on 2026-10-04 (released 2026-09-24). The module's `.golangci.yml` decides: a
  `version: "2"` line selects v2, any other config v1, no config v2 with its defaults.
- Inside an imported folder only lines changed since the import commit are judged (`--new-from-rev`).

### Imports: `scripts/imports/*.manifest`, `import-upstream.sh`, `verify-import.sh`

- A manifest names the target folder, upstream repository, URL, full commit, licence, local clone folder, the
  licence header for new Go files, and the allow-listed `path`s.
- `import-upstream.sh` takes the files from `git archive` of the upstream clone with line-ending conversion
  off (Git for Windows sets `core.autocrlf=true` system-wide, and `git archive` applies it), and stages the
  files that are executable upstream with `+x`.
- The clone is looked up in `$PS03_UPSTREAM_DIR` (default `../upstream`); without one, only the needed commit is
  fetched from the manifest URL into `$PS03_RUN_DIR/upstream-cache/`.
- `verify-import.sh` compares the import commit (or, before it exists, the working tree) with the upstream
  commit: blob id and mode of every file, files the ignore rules would drop, extra files.
- Import workflow:

  ```bash
  bash scripts/import-upstream.sh thunder
  bash scripts/verify-import.sh thunder        # working tree: 0 differ, 0 missing
  git add -A -- thunder && git commit -s -m 'import: upstream thunder-id/thunderid@12f517c4 (Apache-2.0)' -- thunder
  bash scripts/verify-import.sh thunder        # the import commit itself
  ```

### hooks/pre-commit

A path-limited commit (`git commit -- <paths>`) is built from HEAD plus the working tree. On Windows the working tree has no executable bit, so upstream's executable scripts would be
committed as plain files. The hook copies the executable bit from the real index into that commit. Install it
once per clone with `bash scripts/install-hooks.sh`. Limit: a commit whose only change is a file mode is not
possible this way, because git decides there is nothing to commit before the hook runs.

### repo-lint.sh

Checks only what git would commit (ignored files are skipped):

- words: a fixed list of tool and vendor names (stored rot13-encoded in the script) in team-written files,
  lines the team added to upstream files, file names and every commit message; `User-Agent` and Go's
  generated-code marker are allowed. Upstream text is not judged.
- keys: no `BEGIN ... PRIVATE KEY` block and no keystore file anywhere, except the fixtures in
  `.gitguardian.yaml`.
- headers: `SPDX-License-Identifier: MIT` in the first 10 lines of team source files (Go, shell, Kotlin, Java,
  Python, JavaScript, TypeScript, AIDL, Gradle, YAML, TOML, Dockerfiles, Makefiles, any file starting with
  `#!`); new Go files inside an import carry that upstream's header instead. Gradle wrapper files are exempt.
- line endings: LF in team text files (`.bat` and `.cmd` exempt) and in lines added to upstream files.
- size: no file over 5 MiB.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `PS03_RUN_DIR` | `../run` if it exists, else `./run` (ignored by git) | test results, logs, keys, upstream cache |
| `PS03_UPSTREAM_DIR` | `../upstream` if it exists, else `$PS03_RUN_DIR/upstream-cache` | upstream clones |
| `PS03_LOCK_DIR` | `/e/Projects/MOSIP/toolchain/locks` if it exists, else `$PS03_RUN_DIR/locks` | lock folders; one older than 90 minutes is removed as stale |
| `PS03_GO_IMAGE` | `golang:1.26-bookworm` | Go container image |
| `PS03_GO_MEMORY`, `PS03_GO_LINT_MEMORY`, `PS03_GO_CPUS` | `2g`, `3g`, `6` | container limits; the CPU count is lowered to what Docker has |
| `PS03_GO_TEST_P` | `4` | packages tested in parallel |
| `PS03_HELD_LOCKS` | empty | colon-separated lock names this process tree already holds (for example `:docker-stack:emulator`); a script skips taking a lock named here |
| `PS03_LOCK_STALE_MIN`, `PS03_LOCK_TIMEOUT_MIN` | `90`, `60` | a waiting script removes a lock older than the first; it gives up (exit 99) after the second |
| `CGO_ENABLED` | `0` | passed into the container (eSignet's local builds are cgo-free) |
| `PS03_FAMILY_KEYSTORE`, `PS03_FAMILY_KEYSTORE_PASSWORD` | `$PS03_RUN_DIR/keys/ps03-family-test.jks` and a random password in the file next to it | the test certificate that signs both Android apps |

`$PS03_RUN_DIR` holds `keys/`, `test-results/`, `logs/`, `evidence/`, `locks/` (when `PS03_LOCK_DIR` points there),
`upstream-cache/` and the stack's `stack.env`. Docker volumes created by the scripts: `ps03-gomod` (Go modules),
`ps03-gocache` (build cache), `ps03-gotools` (golangci-lint binaries).

## Measured on 2026-10-04 (Windows 11 laptop, Docker Desktop, 12 logical CPUs)

| Run | Result | Time |
|---|---|---|
| `bash scripts/go.sh . version` | `go version go1.26.8 linux/amd64` | 1 s |
| `bash scripts/go-lint.sh --versions`, first run (builds both linters) | `golangci-lint has version v1.64.8 ...` and `... 2.14.0 ...` | 2 min 17 s |
| `bash scripts/go-test.sh scripts/gotools --changed-cover` | 4 packages, 20 tests, 0 failed; 93.0 % of statements covered | about 5 s |
| `bash scripts/go-lint.sh scripts/gotools` | 0 issues | 4 s |
| `bash scripts/verify-import.sh --self-test` | `verify-import: self-test OK` (10 cases) | 22 s |
| `bash scripts/repo-lint.sh --self-test` | `repo-lint: self-test OK` (14 cases) | 55 s |
| Dry run of both imports in a throw-away clone: `import-upstream.sh`, commit, `verify-import.sh` | Thunder 1,680 files, eSignet 274 files: 0 differ, 0 missing, 0 extra | 2 to 3 s each |
| Same clone, `go-lint.sh thunder/backend` (v1.64.8, no team changes, cold cache) | 0 issues | 8 min 53 s |
| Same clone, `go-lint.sh esignet/esignet-service` (v2.14.0); then with one unused function added | 0 issues; then 1 issue (`unused`), exit 1 | 41 s |
| `openapi-lint.sh esignet/docs/esignet-openapi.yaml` in that clone; then with a broken `$ref` added | 0 errors, 8 warnings: OK; then 3 errors: FAILED | 6 s |
| `shellcheck -x scripts/*.sh scripts/hooks/pre-commit` (`koalaman/shellcheck:stable` container, settings in `scripts/.shellcheckrc`) | no findings | 2 s |
