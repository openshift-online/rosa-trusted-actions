# Integration testing

Integration testing for rosa-trusted-actions requires multiple systems to be in place:

1. A cluster to run the rosa-trusted-actions against.
2. 2 authentication backends:
   1. OCM to verify access to trusted-actions.
   2. Backplane to gain access to a cluster.
3. An AWS account that can host the `CloudWatch` and `S3` buckets used for audit logging.

[PR #42](https://github.com/openshift-online/rosa-trusted-actions/pull/42) (mock auth) changes what's
needed to get started locally:

- `ROSA_TA_ENABLE_AUTH=false` swaps in `MockMiddleware`/`MockAuthzMiddleware` — a hardcoded
  `dev-user` identity with the SREP role, no OCM/AMS network calls at all.
- Setting `ROSA_TA_KUBECONFIG=<path>` makes `cmd/server/main.go` select `KubeconfigProvider`
  instead of `BackplaneProvider`, so cluster access doesn't need Backplane either.
- `internal/executor.Executor` is now wired into `internal/handlers/api.go`, so `CreateExecution`
  actually dispatches actions instead of stubbing a `202`.
- As a safety guard, the server refuses to start with `ROSA_TA_ENABLE_AUTH=false` and no
  `ROSA_TA_KUBECONFIG` set (mock auth against the real Backplane is not allowed).

In combination, `ROSA_TA_ENABLE_AUTH=false` + `ROSA_TA_KUBECONFIG=<path>` is a fully wired local
path with zero OCM/Backplane dependency — that's what Phase 1 below builds on. OCM and Backplane
are only mocked in later phases, once we actually need to exercise those code paths for real.

## Phased approach

### Phase 1 — Local execution path (compose + kind + server + GET smoke test)

**Goal:** prove a real execution request flows end-to-end — HTTP → auth middleware → executor →
a real (if disposable) cluster → response — with no OCM or Backplane dependency.

PR #42 (mock auth) is merged, so this phase is unblocked.

Setup:

- `make itest-up` (or `./integration/itest-up.sh` directly) creates a disposable kind cluster
  (reusing it if one with the same name already exists), starts `podman-compose.yml` — MiniStack
  (`ministackorg/ministack`, a LocalStack-API-compatible AWS emulator — see the caveat below) —
  and waits for both to be ready before returning.
- `make itest-run` (or `./integration/itest-run.sh` directly) retrieves a fresh kubeconfig from
  the kind cluster, exports `ROSA_TA_ENABLE_AUTH=false` / `ROSA_TA_KUBECONFIG` / `DATABASE_URL`
  for it, starts the server (`go run ./cmd/server`), waits for `/health`, then exercises the
  catalog's `get` action — list pods in `kube-system` (no fixtures needed, that namespace exists
  in any stock kind cluster) — via `POST /api/v0/trusted-actions/get/run`, polling
  `GET /runs/{id}` until it asserts `status: succeeded`. The server is stopped on exit either way.
  Requires `kind`, `go`, `curl`, and `jq` on `PATH`.
- To poke at the server manually instead (e.g. to try other `params`), run `itest-up.sh` once,
  then:
  ```bash
  export ROSA_TA_ENABLE_AUTH=false
  export ROSA_TA_KUBECONFIG=integration/.kind-kubeconfig
  go run ./cmd/server --log-level debug
  ```
- `make itest-down` (or `./integration/itest-down.sh` directly) stops ministack, deletes the kind
  cluster, and removes the generated local artifacts (`.kind-kubeconfig`, `.trusted_actions.db`,
  `.server.log`). Safe to run even if nothing is up.

Reuse the existing `scripts/test-api.sh` / `make test-integration` pattern for broader API
coverage rather than inventing a second, parallel integration-test concept.

**Caveat — the AWS service has nothing to verify against yet.** As of PR #42,
`cmd/server/main.go` always constructs `audit.NewMockLogger(logger)` (`// TODO: replace with
persistent audit backend`), and `ROSA_TA_S3_BUCKET` is declared in `internal/config/config.go` but
not read anywhere in the codebase — there is no S3 or CloudWatch client at all yet. So starting
MiniStack in Phase 1 is forward-looking scaffolding, not something the smoke test can assert
against: verification in Phase 1 is scoped to the HTTP response only. Revisit once a persistent
audit backend lands.

### Phase 2 — OCM mocking (not Phase 1)

**Goal:** exercise the real `auth.Middleware` / `auth.RoleAuthzMiddleware` / `ocm.Authorization`
code paths (`ROSA_TA_ENABLE_AUTH=true`), instead of bypassing them via mock auth.

This splits into two independent pieces:

- **JWT / JWKS verification** — no new production code needed. `cmd/server/main.go` already
  supports `ROSA_TA_JWK_CERT_FILE`, which loads a local JWKS via
  `authentication.NewHandler().KeysFile(...)`. Generate a throwaway keypair once, publish it as a
  local JWKS file, and mint test JWTs signed with the private key (`username`/`email`/`clientId`
  claims per `internal/auth/context.go`).
- **AMS `AccessReview`** — `internal/ocm/authorization.go` makes a live call to
  `ROSA_TA_OCM_BASE_URL` via `ocm-sdk-go`. There's already a Go-level fake for unit tests
  (`ocm.ConfigurableMockAuthorization`), but it's not reachable from integration tests. For true
  wire-level coverage, stand up a small fake HTTP server implementing the access-review endpoint
  and point `ROSA_TA_OCM_BASE_URL` at it.

Run this with `ROSA_TA_ENABLE_AUTH=true`, paired with Phase 1's kind cluster via
`ROSA_TA_KUBECONFIG` to satisfy PR #42's startup guard (real auth + kubeconfig cluster access,
still no live Backplane needed).

### Phase 3 — Backplane fake server (not Phase 1)

**Goal:** exercise the real `backplane.BackplaneProvider` — currently flagged in
`internal/backplane/backplane.go`:

> `// TODO(ROSAENG-61966): This backplane client is NOT tested against a real backplane instance.
> Do not trust it in production until ROSAENG-61966 integration tests are complete.`

This is a different (and more valuable) target than `KubeconfigProvider`: `KubeconfigProvider`
already exists and is what Phase 1 uses for local dev, but it bypasses `BackplaneProvider`'s
request/response/HMAC contract entirely.

Build a fake HTTP server that implements the two endpoints `BackplaneProvider` actually calls:

- `POST /backplane/trustedaction/{clusterID}` — verify the `X-Caller-ID`/`X-Timestamp`/
  `X-Signature` HMAC, return a fake `instanceId`.
- `/backplane/remediate/{clusterID}/{instanceID}/*` — reverse-proxy into the Phase 1 kind cluster.

Point `ROSA_TA_BACKPLANE_URL` at it and leave `ROSA_TA_KUBECONFIG` unset so `main.go` selects
`BackplaneProvider`.

**Open design question for this phase:** should the fake mint RBAC-scoped kind credentials
(ServiceAccount + Role + RoleBinding) per the `rbacRules` submitted in the trustedaction request —
so kind's real RBAC engine enforces them, testing the actual authorization property — or proxy
with a fixed admin token, which only tests the HTTP/HMAC contract and defers RBAC enforcement
testing?
