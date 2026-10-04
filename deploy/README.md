# Local stack

Docker Compose project `ps03-native-sso`: eSignet built from this repository (with the in-repo Thunder engine),
Postgres, the mock identity system and the oidc-ui login pages. The live e2e tests (`../e2e/`) and the Android
emulator use it.

## Services and ports

| Port on your machine | Service | Image |
|---|---|---|
| 38088 | eSignet API; issuer `http://localhost:38088` | `ps03-native-sso/esignet:local`, built by `esignet.Dockerfile` |
| 38300 | oidc-ui login pages (nginx proxies `/v1/esignet/` to `esignet:8080`) | `mosipid/oidc-ui:2.0.0` |
| 38082 | mock identity system | `mosipid/mock-identity-system:0.14.0` |
| 38455 | Postgres (`mosip_esignet`, `mosip_mockidentitysystem`) | `postgres:bookworm` |

- Upstream files reused unchanged: `../esignet/docker-compose/init.sql` (databases, schemas, seeded user) and
  `../esignet/docker-compose/nginx.conf`.
- Inside the compose network eSignet is `esignet:8080`. The issuer stays `http://localhost:38088` everywhere: it is
  the `iss` of every token and the base of every DPoP `htu`. The emulator reaches the same URL through
  `adb reverse tcp:38088 tcp:38088`; do not use `10.0.2.2`.
- Seeded user: UIN `1774231323`, PIN `545411`, OTP `111111` (any OTP request answers with this code).

## Commands

Run from the repository root (Git Bash on Windows).

| Command | What it does |
|---|---|
| `bash scripts/with-stack.sh <command...>` | takes the `docker-stack` lock, `up`, `smoke`, runs the command, always `down` |
| `bash deploy/stack.sh up` | builds the eSignet image, starts everything, waits until healthy; prints `stack up: ...` |
| `bash deploy/stack.sh smoke` | health, discovery (issuer, PAR, DPoP ES256), JWKS, mock identity, login pages; prints `stack smoke: PASS` |
| `bash deploy/stack.sh status` / `logs [service]` | `docker compose ps` / `docker compose logs` |
| `bash deploy/stack.sh down` | stops everything and deletes the volumes (keystore) |

- `deploy/stack.sh` alone does not take the `docker-stack` lock; hold it yourself (or use `with-stack.sh`) when
  other projects on the machine use Docker.
- `PS03_KEEP_STACK=1 bash scripts/with-stack.sh true` leaves the stack running after the command (for the emulator).
  Stop it with `bash deploy/stack.sh down`.
- Passwords: the Postgres password and the eSignet keystore password are random values written on first use to
  `$PS03_RUN_DIR/stack.env` (outside git; `PS03_RUN_DIR` is `../run` next to the clone when it exists, else
  `run/`, which is git-ignored). Delete the file to get new ones.
- Keystore: eSignet creates its PKCS#12 keystore on first start in the `keystore` volume; `down` deletes it, so
  every `up` has new signing keys.

## Image build

- `esignet.Dockerfile` uses the repository root as build context (upstream's `esignet/esignet-service/Dockerfile`
  cannot: it does not see `../../thunder/backend`, the `replace` target in `go.mod`). `esignet.Dockerfile.dockerignore`
  sends only `thunder/backend` and `esignet/esignet-service` (22 MB).
- Go module and build caches are BuildKit cache mounts. Measured 2026-10-05: first build 78 s; later
  `stack.sh up` runs, cached build included, were ready in 29 to 37 s.
- The image builds whatever is in the working tree, including uncommitted changes in `thunder/` and `esignet/`.

## Headless login (e2e)

`bash scripts/with-stack.sh bash e2e/run.sh [go test flags]` runs `e2e/` (build tag `e2e`) in a Go container on the
network `ps03-native-sso_default`. The client in `e2e/client/` does discovery, PKCE, DPoP (ES256), `private_key_jwt`
(ES256), PAR, authorize, the `/flow/execute` steps (identifier and captcha, OTP, consent), the callback, token and
userinfo, as the upstream Postman "FAPI2.0 flow" does. Each test registers its own clients with keys made at run time.

Findings while writing it (2026-10-05):

- The client assertion's `aud` must be the issuer (`http://localhost:38088`) at every endpoint, as FAPI 2.0
  Security Profile 5.3.2.1 says. The upstream Postman collection uses the endpoint URL and gets
  `invalid_client` / `Invalid client assertion` from this Thunder.
- EC P-256 client keys (ES256) are accepted at registration and for `private_key_jwt`; ES256 DPoP proofs work at
  PAR, token and userinfo. The access token is a JWT whose `cnf.jkt` is the DPoP key's thumbprint; userinfo without
  a DPoP proof answers 401.
- The authorization redirect goes to the login pages at `http://localhost:38300/...` with `authId` and
  `executionId`; the client reads those and does not open the page.
- `TestE2E_PSUTProbe`: two clients of one relying party get the same `sub`; a client of another relying party gets
  another (`psut-scope: relying-party`).

## Captcha

- The server has no captcha validator URL, so it skips the captcha check but still rejects an empty
  `captcha_token`; the e2e client sends a fixed non-empty value.
- The login page's hCaptcha box loads its script from `https://js.hcaptcha.com` (the only hCaptcha origin in the
  oidc-ui 2.0.0 bundle, `assets/index-Zy1Cptt7.js`, checked 2026-10-05). Without internet that script cannot
  load, so the box cannot work offline. Whether the page still lets you continue without it: NOT RUN (needs a
  browser with the network cut; to check on the emulator in WP-09).

## Resource use

Measured 2026-10-04 with the upstream 2.0.0 images on the same ports: mock identity 535 MiB, Postgres 98 MiB,
eSignet 43 MiB, oidc-ui 4.5 MiB. Measured 2026-10-05 with this stack: `stack.sh up` ready in 29 to 37 s (image
cached); the full acceptance run (up, smoke, two tests, down) 87 s.
