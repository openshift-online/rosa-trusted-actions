---
type: operations-guide
title: "Operations: Local Development Environment"
description: How to build, run, lint, and test the ROSA Trusted Actions server locally, including the mock-auth path, local Postgres via compose, and the helper scripts that scaffold staging credentials and smoke-test the API.
tags: [operations, local-development, makefile, mock-auth, postgres, pre-commit, developer-workflow]
verified:
  - by: openwiki/0.7.1
    at: 2026-11-05T16:04:34.033Z
sources:
  - id: openwiki-source-727b56653fd0103b1858eee2
    resource: repo://.gitleaks.toml
  - id: openwiki-source-3bea3530072bb2f5acdc3c30
    resource: repo://.golangci.yml
  - id: openwiki-source-4d1645cb6317345817452838
    resource: repo://.pre-commit-config.yaml
  - id: openwiki-source-0f6ed11169bd8e6b34b44280
    resource: repo://cmd/server/main.go
  - id: openwiki-source-3f7a5edfb918ff68652fa887
    resource: repo://compose.postgres.yml
  - id: openwiki-source-860fa193cf581f1afb221430
    resource: repo://DEVELOPMENT.md
  - id: openwiki-source-f011a030ad13c406633402c0
    resource: repo://docker-compose.test.yml
  - id: openwiki-source-4600843467c65f57d591fb00
    resource: repo://integration/README.md
  - id: openwiki-source-012f2c78e3b1446dfc35803f
    resource: repo://Makefile
  - id: openwiki-source-23775c3de52f3ab95a13cb8b
    resource: repo://README.md
  - id: openwiki-source-7c05e19619b30303c3da9075
    resource: repo://scripts/setup-local-staging.sh
  - id: openwiki-source-fa1a0bbc32b5e8ec42ac2415
    resource: repo://scripts/test-api.sh
generated: { by: "openwiki/0.7.1", at: "2026-11-05T16:04:34.033Z" }
---

# Operations: Local Development Environment

This page covers the day-to-day developer loop for the ROSA Trusted Actions server: the
`make` targets that build/run/lint/test the project, the local Postgres compose stacks, the
mock-auth path that lets you run the server without live OCM/Backplane/AWS credentials, and
the helper scripts under `scripts/`. For infrastructure, Terraform, and CI pipeline content,
see [Deployment](deployment.md). For environment variable semantics, see
[Configuration](configuration.md). For the mock backplane, proxy backplane, and `kind`-based
integration harness, see
[Testing: Integration and Mocks](../testing/integration-and-mocks.md).

## Prerequisites

| Tool          | Version | Purpose               |
|---------------|---------|------------------------|
| Go            | 1.24+   | Primary language       |
| Node.js       | 16+     | OpenAPI spec bundling  |
| Make          | any     | Build automation       |
| Git           | any     | Version control        |
| golangci-lint | 1.61+   | Go linting             |
| pre-commit    | any     | Git hook management    |

Initial setup clones the repo, installs dependencies, installs the pre-commit hooks, and runs
a full generate+build:

```bash
git clone https://github.com/openshift-online/rosa-trusted-actions.git
cd rosa-trusted-actions

make deps              # go mod download/tidy + npm install

pip install pre-commit
pre-commit install

make all               # clean, deps, generate, build
```
(`repo://DEVELOPMENT.md#L16-L32`, `repo://Makefile#L31-L46`)

## Make targets

`Makefile` is the single source of truth for the developer loop; `make help` lists every
target with its one-line description. The groups most relevant to day-to-day work:

**Dependencies and code generation**

| Target | Effect |
|---|---|
| `deps` | Runs `deps-go` (`go mod download && go mod tidy`) and `deps-npm` (`npm install`) |
| `generate` | `bundle-spec` (bundles the multi-file OpenAPI spec with `npm run bundle-spec`) then `generate-api` (runs `oapi-codegen` against the bundled spec into `internal/openapi/`) |
| `validate-spec` | Validates the OpenAPI spec without generating code (`npm run validate-spec`) |

The generated file `internal/openapi/api.go` is committed to the repository; after `make
generate`, commit the regenerated file alongside any OpenAPI spec changes.
(`repo://Makefile#L34-L64`, `repo://DEVELOPMENT.md#L57-L67`)

**Build and run**

| Target | Effect |
|---|---|
| `build` | `generate`, then `go build` the server binary into `./bin/rosa-trusted-actions-server` |
| `build-linux` | `generate`, then cross-compiles `GOOS=linux GOARCH=amd64` to `./bin/rosa-trusted-actions-server-linux` |
| `build-cli`, `build-rta`, `build-test-client` | Build the `action-cli`, `rta`, and `test-client` companion binaries (`rta` is version-stamped via `-ldflags` with git describe/commit/date) |
| `run` | `generate`, then `go run ./cmd/server`, picking up `DATABASE_URL` and other config from the environment |
| `run-postgres` | `generate` + `postgres-up`, then runs the server against the local composed Postgres at `postgres://postgres:postgres@localhost:5432/trusted_actions?sslmode=disable` |
| `run-sqlite` | `generate`, then runs the server with `DATABASE_URL=trusted_actions.db` (a local SQLite file) |
| `dev` | Installs `air` if missing, then runs it for auto-reload on file changes |

(`repo://Makefile#L66-L123`)

The server listens on `:8080` by default; override with CLI flags, e.g.
`./bin/rosa-trusted-actions-server --listen-addr=":3000" --log-level="debug" --log-json`.
(`repo://DEVELOPMENT.md#L43-L55`)

**Quality gates**

| Target | Effect |
|---|---|
| `lint` | Installs `golangci-lint` if missing, then `golangci-lint run` using `.golangci.yml` |
| `fmt` | `go fmt ./...` then `goimports -w .` |

(`repo://Makefile#L231-L240`)

**Test family**

| Target | Effect |
|---|---|
| `test` | `generate`, compiles (but does not run) the `integration`-tagged tests in `./tests/actions/` to catch build breaks, then runs `go test -v ./...` (SQLite-backed; Postgres-backed store tests are skipped unless `TEST_POSTGRES_URL` is set) |
| `test-race` | `generate`, then `go test -race -v ./...` |
| `test-coverage` | `go test -coverprofile=coverage.out ./...`, then renders `coverage.html` |
| `test-postgres-up` / `test-postgres-down` | Start/stop the `docker-compose.test.yml` Postgres container used by store tests |
| `test-postgres` | Starts the test Postgres container, runs `./internal/store/...` tests against it with `TEST_POSTGRES_URL=postgres://postgres:postgres@localhost:5433/trusted_actions_test?sslmode=disable`, then tears it down (tearing down even on failure) |
| `test-api` | Runs `scripts/test-api.sh` against an already-running server |
| `test-integration` | Starts the server in the background via `make run`, waits, runs `scripts/test-api.sh`, then kills the server |
| `test-actions` | Runs `./tests/actions/...` integration tests against the mock backplane (`TEST_BACKPLANE=mock`, requires `make mock-backplane` running) |
| `test-actions-proxy` | Runs the `Proxy`-tagged subset of the same tests against the proxy backplane (requires `make proxy-backplane` and a `KUBECONFIG`) |
| `itest-up` / `itest-run` / `itest-down` | Create/use a `kind` cluster plus ministack, run the server against it and smoke-test the `get` action, then tear everything down |

(`repo://Makefile#L124-L230`)

**Backplane doubles**

| Target | Effect |
|---|---|
| `mock-backplane` | Starts the Imposter-based backplane mock server (native engine) defined under `tests/mock-backplane/`, on `MOCK_BACKPLANE_PORT` (default `8087`, same default as `make run` — override it to run the mock alongside the real server) |
| `proxy-backplane` | `go run ./tests/proxy-backplane`, a reverse proxy to a real cluster, listening on `PROXY_BACKPLANE_PORT` (default `8088`) |

(`repo://Makefile#L13-L19`, `repo://Makefile#L216-L229`)

See [Testing: Integration and Mocks](../testing/integration-and-mocks.md) for how the mock and
proxy backplanes, `itest-*`, and `tests/actions/...` fit into the broader test strategy.

## Local Postgres

Two independent compose stacks exist, intentionally bound to different ports so both can run
at once without colliding:

- **`compose.postgres.yml`** — a persistent development instance (`postgres:16`), container
  name `rosa-ta-postgres`, exposed on `127.0.0.1:5432`, database `trusted_actions`, credentials
  `postgres`/`postgres`, backed by a named volume (`rosa_ta_pgdata`) so data survives restarts.
  Its credentials match the `DATABASE_URL` default in `.env.example`. Managed via:
  - `make postgres-up` — start detached, wait for the healthcheck (`pg_isready`)
  - `make postgres-down` — stop and remove the container *and* its volume
  - `make postgres-logs` — tail container logs
  (`repo://compose.postgres.yml`, `repo://Makefile#L152-L163`)

- **`docker-compose.test.yml`** — an ephemeral instance for the store test suite, database
  `trusted_actions_test`, exposed on `127.0.0.1:5433` (no named volume, no persistence). Started
  under a distinct compose project name (`rosa-ta-postgres-test`) so it doesn't collide with the
  development stack. Managed via `make test-postgres-up` / `test-postgres-down`, or end-to-end
  via `make test-postgres`, which starts it, runs `./internal/store/...` against
  `TEST_POSTGRES_URL`, and always tears the container down (even on test failure).
  (`repo://docker-compose.test.yml`, `repo://Makefile#L164-L177`)

Both targets resolve a `COMPOSE` command at Makefile-parse time: if `podman-compose` or
`docker-compose` is on the `PATH` *and* `podman` itself is available, `podman compose` is used;
otherwise the Makefile falls back to `docker compose`. (`repo://Makefile#L140-L150`)

If you don't need Postgres-specific behavior, `make run-sqlite` runs the server against a local
SQLite file (`trusted_actions.db`) instead, with no compose stack required.
(`repo://Makefile#L114-L117`)

## Quality gates: pre-commit, golangci-lint, gitleaks

`.pre-commit-config.yaml` defines the local hook set that mirrors the CI lint pipeline
(`ci/prow/lint`); CI remains the authoritative gate, and every pre-commit check also runs
there. Install once with `pip install pre-commit && pre-commit install`; run manually with
`pre-commit run` (staged files) or `pre-commit run --all-files` (full repo, e.g. first-time
setup or before a CI run). Hooks run fastest-first and each carries a timeout guard, targeting
a total run under 10s (hard limit 60s) on a 10-file changeset. The configured hooks are:

1. **File hygiene / YAML syntax** (`pre-commit-hooks`) — `check-merge-conflict`,
   `trailing-whitespace` (auto-fixes), `end-of-file-fixer` (auto-fixes), and `check-yaml`
   scoped to `deploy/*.yaml` manifests.
2. **Secret detection** (`gitleaks`, pinned to `v8.18.0`) — scans all file types for
   hardcoded secrets; configuration and allowlisting live in `.gitleaks.toml` (test fixtures,
   `*_test.go`, `vendor/`, `node_modules/`, and generated `internal/openapi/` are excluded, and
   a few known-safe patterns like `fake_token` or AWS's documented example key are allowlisted).
   Gitleaks findings are never bypassable, even for agents.
3. **Static analysis** (`golangci-lint`, pinned to `v2.0.2`) — runs with `--config=./.golangci.yml`
   and a 120s timeout, mirroring the CI `go-check` step exactly (same version and config).
   `.golangci.yml` enables linters across four themes: error handling/security (`errcheck`,
   `govet`, `staticcheck`, `gosec`, `bodyclose`, `sqlclosecheck`, `contextcheck`, `noctx`),
   error prevention (`errorlint`, `nilerr`, `nilnil`, `revive`), code quality (`ineffassign`,
   `unconvert`, `unparam`, `unused`, `misspell`), and maintainability (`prealloc`, `nolintlint`,
   `gocyclo` at a minimum complexity of 15, `exhaustive`, `makezero`, `containedctx`). Generated
   code in `internal/openapi/` is excluded from linting.
4. **Local hooks** — `go-build` (`go build ./...`, catches import cycles/type errors before
   golangci-lint runs), `go-mod-tidy` (fails if `go mod tidy` would change `go.mod`/`go.sum`),
   and `rbac-wildcard-check` (rejects wildcard RBAC verbs/resources in `deploy/*.yaml`).

To skip a single hook when it must be bypassed, use `SKIP=hook-id git commit` — never
`git commit --no-verify`, and security hooks are never bypassable regardless.
(`repo://.pre-commit-config.yaml`, `repo://.golangci.yml#L1-L60`, `repo://.gitleaks.toml#L1-L40`)

`make lint` and `make fmt` (above) are the equivalent entry points for running golangci-lint
and auto-formatting outside of a commit.

## The mock-auth local run path

By default the server requires live OCM credentials and a reachable JWKS endpoint to
authenticate and authorize every request, plus a real Backplane connection to reach a cluster
and real AWS credentials for cloud-resource actions. The mock-auth path removes all three
dependencies for local development:

```bash
export ROSA_TA_AUTH=disabled
export ROSA_TA_KUBECONFIG=/tmp/<cluster>.kubeconfig
export ROSA_TA_AWS_ENDPOINT_URL=http://127.0.0.1:4566   # e.g. a local LocalStack

go run ./cmd/server/ --log-level debug
```

Setting `ROSA_TA_AUTH=disabled` swaps the real authentication/authorization middleware for
`auth.NewMockAuthMiddleware()` / `auth.NewMockAuthzMiddleware()`, which inject a hardcoded
`dev-user` identity carrying the SREP role for every request — no `Authorization` header, OCM
call, or AMS check is needed. The server logs a `WARN` on startup
(`Auth disabled — using mock identity 'dev-user' with SREP role. Do not use in production.`)
to make the bypass visible. (`repo://README.md#L22-L32`, `repo://cmd/server/main.go#L179-L182`)

This is more permissive than normal operation, so `cmd/server/main.go` enforces two startup
guards whenever `ROSA_TA_AUTH=disabled` is selected — the server refuses to start (`Fatal`) if
either is missing:

- **`ROSA_TA_KUBECONFIG` must be set.** Without it, an unauthenticated caller would receive the
  hardcoded SREP role and the server would still route cluster access through the real
  `BackplaneProvider`, reaching production cluster infrastructure with no actual
  authentication behind it. Setting `ROSA_TA_KUBECONFIG=<path>` instead selects
  `KubeconfigProvider`, pointing cluster access at whatever cluster the kubeconfig describes
  (e.g. a local `kind` cluster), bypassing Backplane entirely.
- **`ROSA_TA_AWS_ENDPOINT_URL` must be set.** The same hazard exists on the AWS side: the AWS
  SDK's default credential chain would otherwise pick up whatever ambient credentials the host
  or process has, letting a mock-authenticated (effectively unauthenticated) caller reach a
  real AWS account. Requiring an endpoint override pins AWS traffic at a local emulator — point
  it at LocalStack (`http://127.0.0.1:4566`) — instead of letting the default credential chain
  resolve.

In combination, `ROSA_TA_AUTH=disabled` + `ROSA_TA_KUBECONFIG=<path>` +
`ROSA_TA_AWS_ENDPOINT_URL=<url>` is a fully wired local path with zero OCM/Backplane/real-AWS
dependency, suitable for exercising the full HTTP → auth middleware → executor → cluster/cloud
→ response pipeline against disposable local infrastructure (e.g. `kind` + LocalStack) with no
external credentials at all. (`repo://cmd/server/main.go#L184-L200`, `repo://integration/README.md#L11-L30`)

Once the server is up, verify and exercise it:

```bash
curl -s http://localhost:8080/health | jq .

# No Authorization header required — mock auth injects the identity automatically
curl -s http://localhost:8080/api/v0/trusted-actions/ | jq .

curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -d '{
    "target_cluster": "local",
    "params": {"resource": "pods", "version": "v1", "namespace": "kube-system"}
  }' | jq .
```

`POST .../run` only returns `202 Accepted` with `status: pending`; execution happens
asynchronously on a background worker — poll `GET /api/v0/trusted-actions/runs/{id}` for the
result. (`repo://README.md#L46-L131`)

> **Warning:** never set `ROSA_TA_AUTH=disabled` (or `ROSA_TA_AUTH` at all) outside a local or
> CI environment — the mock identity grants the SREP role unconditionally.

For fetching a real kubeconfig for an OSD/ROSA cluster to feed into `ROSA_TA_KUBECONFIG`, see
`README.md`'s "Get a kubeconfig" section (`ocm get
/api/clusters_mgmt/v1/clusters/<internal-id>/credentials | jq -r .kubeconfig`). For the broader
`kind`-cluster + LocalStack integration harness that automates this entire path end to end, see
`make itest-up` / `make itest-run` above and
[Testing: Integration and Mocks](../testing/integration-and-mocks.md). For the full set of
`ROSA_TA_AUTH` policies (`enabled`, `ocmconfig`, `disabled`) and their precedence rules, see
[Configuration](configuration.md).

## Helper scripts

### `scripts/setup-local-staging.sh`

Sourced (not executed) to populate the current shell with staging OCM credentials fetched
from Vault, so the server can run locally against the staging Backplane instead of mock auth:

```bash
source scripts/setup-local-staging.sh
make run
```

It requires the `vault` CLI, `jq`, and OIDC access to `vault.devshift.net`. It logs into Vault
(`vault login -method=oidc -token-only`), reads the
`osd-sre/trusted-actions/ocm/staging` KV path, and exports `ROSA_TA_OCM_CLIENT_ID`,
`ROSA_TA_OCM_CLIENT_SECRET`, and `ROSA_TA_OCM_BASE_URL=https://api.stage.openshift.com`, plus
`ROSA_TA_ALLOWED_NAMESPACES` and `ROSA_TA_ALLOWED_SECRETS` defaults for staging. It
intentionally unsets `VAULT_ADDR`/`VAULT_TOKEN` afterward so the Vault session token doesn't
linger in the shell. It does not set Backplane credentials
(`ROSA_TA_BACKPLANE_URL`/`_CLIENT_ID`/`_CLIENT_SECRET`) — the script prints a warning reminding
you to set those manually if unset, since the staging Backplane service-account wiring is still
pending. Any failure at a step (`vault login`, `vault kv get`, missing `CLIENT_ID`/
`CLIENT_SECRET`) aborts via early `return 1` and unsets the partially-set Vault variables first.
(`repo://scripts/setup-local-staging.sh`)

### `scripts/test-api.sh`

A smoke-test script exercised directly (`make test-api`) or as part of `make
test-integration` (which starts the server first). It targets `SERVER_URL`
(`http://localhost:8080` by default) and exercises, in order:

1. `GET /health` — aborts immediately (with a reminder to `make run`) if the server isn't
   responding.
2. `GET /api/v0/trusted-actions/` — list the action catalog.
3. `GET /api/v0/trusted-actions/get` — describe the `get` action.
4. `POST /api/v0/trusted-actions/get/run` — execute a dry-run `get` action
   (`"dry_run": true`) against a `test-cluster`.
5. `GET /api/v0/trusted-actions/runs?limit=5` — list recent executions.
6. `GET /api/v0/trusted-actions/runs/<random-uuid>?include=output,logs` — fetch (a
   nonexistent) execution's details.
7. `GET /api/v0/trusted-actions/audit?limit=5` — fetch the audit log.

Each check prints the HTTP status and pretty-prints the JSON response body (via `jq` if
available); a 2xx status is treated as pass, anything else fails loudly with the raw response
body. The script sets `AWS_REGION` and `ROSA_TA_S3_BUCKET` defaults itself so it can run against
a server started with minimal configuration. Note that step 6 targeting a random, nonexistent
execution ID is expected to exercise the "not found" path rather than assert success — read
the script's exit-status handling before wiring it into stricter automation.
(`repo://scripts/test-api.sh`, `repo://Makefile#L191-L204`)

## Related pages

- [Configuration](configuration.md) — full reference for environment variables, CLI flags, and
  the `ROSA_TA_AUTH` policy set.
- [Deployment](deployment.md) — Terraform, container image, and CI/CD pipeline concerns (out of
  scope for this page).
- [Testing: Integration and Mocks](../testing/integration-and-mocks.md) — the mock backplane,
  proxy backplane, and `kind`/LocalStack integration harness in depth.
- [CLI Clients](../workflows/cli-clients.md) — the `action-cli`, `rta`, and `test-client`
  binaries built by `make build-cli` / `build-rta` / `build-test-client`.
