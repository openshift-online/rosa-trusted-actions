# Integration testing

Integration testing for rosa-trusted-actions requires multiple systems to be in place:

1. A cluster to run the rosa-trusted-actions against.
2. 2 authentication backends:
   1. OCM to verify access to trusted-actions.
   2. Backplane to gain access to a cluster.
3. An AWS account that can host the `CloudWatch` and `S3` buckets used for audit logging.

[PR #42](https://github.com/openshift-online/rosa-trusted-actions/pull/42) (mock auth) changes what's
needed to get started locally:

- `ROSA_TA_AUTH=disabled` swaps in `MockMiddleware`/`MockAuthzMiddleware` — a hardcoded
  `dev-user` identity with the SREP role, no OCM/AMS network calls at all.
- Setting `ROSA_TA_KUBECONFIG=<path>` makes `cmd/server/main.go` select `KubeconfigProvider`
  instead of `BackplaneProvider`, so cluster access doesn't need Backplane either.
- `internal/executor.Executor` is now wired into `internal/handlers/api.go`, so `CreateExecution`
  actually dispatches actions instead of stubbing a `202`.
- As a safety guard, the server refuses to start with `ROSA_TA_AUTH=disabled` and no
  `ROSA_TA_KUBECONFIG` set (mock auth against the real Backplane is not allowed).
- The same guard applies to AWS: `ROSA_TA_AUTH=disabled` also requires
  `ROSA_TA_AWS_ENDPOINT_URL` to be set. Mock auth hands every unauthenticated caller the
  SREP role, and the AWS SDK's default credential chain would otherwise pick up whatever
  ambient credentials the host or pod has — so AWS traffic must be pinned to a local
  emulator. Point it at localstack (`http://127.0.0.1:4566`).

In combination, `ROSA_TA_AUTH=disabled` + `ROSA_TA_KUBECONFIG=<path>` is a fully wired local
path with zero OCM/Backplane dependency — that's what Phase 1 below builds on. OCM and Backplane
are only mocked in later phases, once we actually need to exercise those code paths for real.

## Phased approach

### Phase 1 — Local execution path (compose + kind + server + GET smoke test)

**Goal:** prove a real execution request flows end-to-end — HTTP → auth middleware → executor →
a real (if disposable) cluster → response — with no OCM or Backplane dependency.

PR #42 (mock auth) is merged, so this phase is unblocked.

Setup:

- `make itest-up` (or `./integration/itest-up.sh` directly) creates a disposable kind cluster
  (reusing it if one with the same name already exists), starts `podman-compose.yml` —
  localstack (`localstack/localstack`, an AWS emulator) — and waits for both to be ready
  before returning.
- `make itest-run` (or `./integration/itest-run.sh` directly) retrieves a fresh kubeconfig from
  the kind cluster, exports `ROSA_TA_AUTH=disabled` / `ROSA_TA_KUBECONFIG` / `DATABASE_URL`
  plus the AWS variables above, starts the server, waits for `/health`, then exercises one
  action per execution scope, polling `GET /runs/{id}` until it asserts `status: succeeded`:
  - **kube-api** — `get`, listing pods in `kube-system` (no fixtures needed, that namespace
    exists in any stock kind cluster).
  - **aws-api** — `aws-get`, listing `ec2-instances`. The script first seeds a tagged EC2
    instance in localstack via `awslocal`, then asserts that instance appears in the action's
    output, so a read that succeeds while returning nothing still fails.
  - **aws-api, remaining resources** — the same assertion is then applied to every other
    `aws-get` resource localstack can serve: `ec2-vpcs`, `ec2-subnets`, `ec2-route-tables`,
    `ec2-security-groups`, `ec2-dhcp-options`, `route53-hosted-zones` and
    `route53-record-sets`. DHCP option sets and hosted zones do not exist in a stock
    localstack, so the script seeds one of each first. Finally it submits a request with a
    param the resource does not accept and asserts the execution *fails* — the input
    validation is only worth having if it survives the whole HTTP path.

  The server is stopped on exit either way. Requires `kind`, `go`, `curl`, `jq`, and `podman`
  on `PATH`.

**Caveat — five `aws-get` resources are not covered here.** `load-balancers`,
`load-balancers-classic`, `target-groups`, `target-health` and `cloudtrail-events` need the
`elb`, `elbv2` and `cloudtrail` services, which are LocalStack **Pro** features; the community
image used by `podman-compose.yml` answers them with `InternalFailure: Sorry, the <service>
service is not included in your LocalStack license`. They are covered by unit tests against
fakes in `internal/actions`, but nothing here exercises them against a real API
implementation, so a green `itest-run` does not mean the whole AWS read surface works.
- To poke at the server manually instead (e.g. to try other `params`), run `itest-up.sh` once,
  then:
  ```bash
  export ROSA_TA_AUTH=disabled
  export ROSA_TA_KUBECONFIG=integration/.kind-kubeconfig
  # Required by the AWS startup guard; localstack accepts any credentials.
  export ROSA_TA_AWS_ENDPOINT_URL=http://127.0.0.1:4566
  export AWS_REGION=us-east-1
  export AWS_ACCESS_KEY_ID=test
  export AWS_SECRET_ACCESS_KEY=test
  go run ./cmd/server --log-level debug
  ```
- `make itest-down` (or `./integration/itest-down.sh` directly) stops localstack, deletes the kind
  cluster, and removes the generated local artifacts (`.kind-kubeconfig`, `.trusted_actions.db`,
  `.server.log`). Safe to run even if nothing is up.

Reuse the existing `scripts/test-api.sh` / `make test-integration` pattern for broader API
coverage rather than inventing a second, parallel integration-test concept.

**Caveat — audit logging still has nothing to verify against.** The `aws-get` smoke test above
covers the *read* path (EC2 via `internal/awsapi`), but not audit storage:
`cmd/server/main.go` still constructs `audit.NewMockLogger(logger)` (`// TODO: replace with
persistent audit backend`), and `ROSA_TA_S3_BUCKET` is declared in `internal/config/config.go`
but not read anywhere in the codebase — there is no S3 or CloudWatch client yet. Revisit once a
persistent audit backend lands.

**Caveat — the localstack path uses one AWS account for every cluster.** There are two AWS
client providers, and `ROSA_TA_AWS_ENDPOINT_URL` chooses between them in `cmd/server/main.go`:

- **`awsapi.StaticProvider`** (endpoint override set) builds its clients from a single fixed
  set of credentials and ignores the cluster ID entirely. It is what these integration tests
  exercise, and it is fine against localstack, where there is only one account — but it offers
  no per-cluster isolation and must not be pointed at real AWS.
- **`awsapi.CloudCredentialsProvider`** (no endpoint override) is the production path. It mirrors
  configuration-anomaly-detection's `pkg/managedcloud`: it resolves the cluster in OCM, then
  hands the cluster and the OCM connection to `backplane-cli`, which walks the SRE assume-role
  chain from the payer account into the customer's own AWS account and returns short-lived STS
  credentials scoped to it. Because it needs a live OCM connection, it requires
  `ROSA_TA_AUTH=enabled`; the server refuses to start with neither an endpoint override nor an
  OCM client.

Two things about the production path matter when deploying it, neither of which the localstack
tests can catch:

- **Egress IP.** The assume-role chain only issues credentials when the caller's egress IP is
  inside the trusted range OCM publishes (backplane-cli checks this via
  `https://checkip.amazonaws.com/`). The server must run from a trusted network, or reach AWS
  through `ROSA_TA_AWS_PROXY_URL` (which falls back to `ROSA_TA_BACKPLANE_PROXY_URL`).
- **It is not covered end to end.** `internal/awsapi/credentials_test.go` tests everything around
  the `backplane-cli` call — cluster resolution and caching, region precedence, proxy
  selection, error reporting — but the credential call itself is stubbed. Unlike AWS, that
  chain (backplane + OCM + STS) has no emulator, so it is unverified against a live backplane
  in the same way `backplane.BackplaneProvider` is (see Phase 3).

The region a request reads differs accordingly: `StaticProvider` returns its one configured
region for every cluster, while `BackplaneProvider` returns the target cluster's own region.
Callers cannot choose — `aws-get` has no `region` param and rejects one if sent, because a
cluster's AWS resources live where the cluster does. The region allowlist
(`ROSA_TA_AWS_ALLOWED_REGIONS`) still judges the resolved region in both cases, so it must
cover every region the fleet runs in or reads against those clusters will be denied.

That allowlist is server-side only — there is no client or CLI flag for it, so nothing a caller
sends can widen it. It has **no default**: `authorization.DefaultAllowedRegions` is empty, and a
server started without `ROSA_TA_AWS_ALLOWED_REGIONS` (or `actions.aws_allowed_regions`) exits at
startup rather than running and denying every AWS request. Configured empty it denies everything
rather than allowing everything, unlike the Kubernetes namespace allowlist. Which regions a
deployment may read is therefore always an explicit decision, never an inherited one.

`action-cli` reads the same variable for the same reason, so running an AWS action locally needs
`ROSA_TA_AWS_ALLOWED_REGIONS` set too.

**The AWS write actions are behind a second, separate allowlist.** `aws-reboot` and
`aws-terminate` change a cluster's EC2 instances, and clearing the region gate says nothing
about being allowed to do that — being permitted to look at a region is not being permitted to
change what is in it. They are judged against `ROSA_TA_AWS_ALLOWED_OPERATIONS` (config key
`aws_allowed_operations`), a comma-separated list of operation names: `reboot`, `terminate`.
Unlike the region allowlist this one has no default to fall back to, so an unconfigured server
performs no AWS writes while still serving every read. The operation is taken from the action
itself, never from the request, so a caller cannot describe a terminate as a reboot to land on
a permitted entry.

Both writes additionally require the target instance to carry the cluster's ownership tag,
`kubernetes.io/cluster/<infraID>=owned` — the same tag configuration-anomaly-detection filters
on. The infra ID comes from OCM alongside the region, never from the request. This matters
because credentials obtained for one cluster can reach every instance in the AWS account it
lives in, which may include other clusters and the customer's own workloads; reaching an
instance is not evidence of being entitled to reboot it. A `shared` tag value is refused, since
shared means something else depends on the resource.

**Caveat — `itest-run` does not exercise the write path.** The smoke tests cover `aws-get`
only. Localstack does implement `RebootInstances` and `TerminateInstances`, so a write test is
possible, but it would also need the server started with `ROSA_TA_AWS_ALLOWED_OPERATIONS` set
and the seeded instance tagged with the static provider's infra ID
(`awsapi.StaticConfig.InfraID`, set from `--aws-infra-id` in `action-cli`). Until that exists,
the writes are covered by unit tests only — including the ownership check and both allowlist
gates — and a green `itest-run` is not evidence that they work end to end.

### Phase 2 — OCM mocking (not Phase 1)

**Goal:** exercise the real `auth.Middleware` / `auth.RoleAuthzMiddleware` / `ocm.Authorization`
code paths (`ROSA_TA_AUTH=enabled`), instead of bypassing them via mock auth.

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

Run this with `ROSA_TA_AUTH=enabled`, paired with Phase 1's kind cluster via
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

Build a fake HTTP server that implements the two endpoints `backplane.BackplaneProvider`
actually calls:

- `POST /backplane/trustedaction/{clusterID}` — verify the `X-Caller-ID`/`X-Timestamp`/
  `X-Signature` HMAC, return a fake `instanceId`.
- `/backplane/remediate/{clusterID}/{instanceID}/*` — reverse-proxy into the Phase 1 kind cluster.

Point `ROSA_TA_BACKPLANE_URL` at it and leave `ROSA_TA_KUBECONFIG` unset so `main.go` selects
`backplane.BackplaneProvider`.

**A second, separate flow hits the same service.** `awsapi.CloudCredentialsProvider` (see Phase 1's
AWS caveat) also talks to `ROSA_TA_BACKPLANE_URL`, but through `backplane-cli`'s cloud
credential endpoints rather than the HMAC-signed trusted-action ones above — so the fake
described here does not cover it. Faking it is a bigger job: `backplane-cli` drives the
response into a real STS assume-role chain and verifies the caller's egress IP against OCM's
trusted range, so a useful fake has to stand in for backplane, OCM and STS together. Until
then, `internal/awsapi/credentials_test.go` covers everything on this side of that call.

**Open design question for this phase:** should the fake mint RBAC-scoped kind credentials
(ServiceAccount + Role + RoleBinding) per the `rbacRules` submitted in the trustedaction request —
so kind's real RBAC engine enforces them, testing the actual authorization property — or proxy
with a fixed admin token, which only tests the HTTP/HMAC contract and defers RBAC enforcement
testing?
