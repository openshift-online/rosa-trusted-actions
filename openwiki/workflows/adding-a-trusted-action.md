---
type: workflow
title: "Workflow: Adding a New Trusted Action"
description: Canonical contributor workflow for authoring, registering, testing, reviewing, and shipping a new Trusted Action, covering both the Kube-action and AWS-action paths.
tags: [trusted-actions, contributor-workflow, rbac, aws, catalog, testing]
verified:
  - by: openwiki/0.7.1
    at: 2026-11-05T16:04:34.033Z
sources:
  - id: openwiki-source-80038ed0523fffd380beabcc
    resource: repo://.claude/skills/add-trusted-action/SKILL.md
  - id: openwiki-source-2bd1941dc3b0982040174920
    resource: repo://cmd/action-cli/main.go
  - id: openwiki-source-1a7c8c052eab226fd799d270
    resource: repo://docs/adding-a-trusted-action.md
  - id: openwiki-source-1db51bb85e8d5bbad4b67c8c
    resource: repo://internal/actions/aws_write.go
  - id: openwiki-source-66750043d991c1abb5dd1bfd
    resource: repo://internal/actions/get_pull_secret_email.go
generated: { by: "openwiki/0.7.1", at: "2026-11-05T16:04:34.033Z" }
---

# Workflow: Adding a New Trusted Action

A Trusted Action is a named, auditable operation the server runs against a
managed cluster or AWS account on a caller's behalf. This page is the
step-by-step contributor workflow for adding one. For the interfaces and types
involved (`Action`, `KubeAction`, `AWSAction`, `ActionDefinition`, etc.), see
[Trusted Actions Catalog](../concepts/trusted-actions-catalog.md); for how a
request actually flows through authorization, the backplane, and execution,
see [Execution Pipeline](../concepts/execution-pipeline.md). This workflow is
also codified as the repository's own guide,
[`docs/adding-a-trusted-action.md`](../../docs/adding-a-trusted-action.md),
and as the Claude Code
[`add-trusted-action`](../../.claude/skills/add-trusted-action/SKILL.md) skill,
which automates the same sequence of steps (including the local end-to-end
recipe) for AI-assisted contributions.

## The two mandatory registrations

An action is defined in **two independent places**, and both are required
before it works end to end:

| Layer | File | Purpose |
|-------|------|---------|
| Implementation | `internal/actions/<name>.go` | The behavior: what it does on the cluster/account and the exact RBAC/services it needs. Implements `actions.Action` (plus a scope-specific interface) and self-registers. |
| Catalog (policy) | `internal/catalog/catalog.go` | The public contract: action type, scope, allowed caller roles, parameters, and write-safety metadata. This is what the API exposes and validates against. |

They are deliberately decoupled — nothing cross-checks that one side agrees
with the other — so the public contract (catalog) can be reviewed separately
from the implementation. Registering only one half fails in two different,
non-obvious ways:

- **Catalog entry but no `internal/actions` implementation.** `POST /{action}/run`
  is accepted by the API (status `202`), but the background worker fails the
  execution asynchronously with `unknown action "<name>"` once it calls
  `actions.CreateAction` (`internal/worker/executor_runner.go`). The failure
  only surfaces when the caller polls `GET /runs/{id}`.
- **Implementation but no catalog entry.** `GET /{action}` and
  `POST /{action}/run` both return `404 Unknown action` immediately, because
  the handlers validate the action name against the catalog before anything
  else runs (`internal/handlers/api.go`).

Because of this split failure mode, the local end-to-end testing recipe
(below) deliberately includes removing each registration in turn to confirm
both are wired correctly before relying on either.

## Step 1 — Implement the action (`internal/actions/<name>.go`)

Every action implements the shared `actions.Action` interface (just `Name()`)
plus one scope-specific interface, defined in `internal/actions/actions.go`.
Which one you implement depends on where the action runs — see
[Trusted Actions Catalog](../concepts/trusted-actions-catalog.md) for the full
interface definitions. The two paths diverge in what privilege they request
and how that privilege is scoped:

### Kube-action path

A `KubeAction` declares `RequiredRBAC(target ResourceTarget) []backplane.RBACRule`
and `UsesPodExec() bool`, and executes against `actions.KubeClients`. The
executor builds a backplane client scoped to exactly the RBAC rules
`RequiredRBAC` returns for the given target, and only provisions a pod
executor if `UsesPodExec()` returns `true`. The action never constructs its
own cluster credentials — it only ever uses what the executor injects.

`internal/actions/get_pull_secret_email.go` is the reference implementation
for this path: it returns `false` from `UsesPodExec()`, declares the single
narrowest RBAC rule it needs (`get` on one named secret in one namespace via
`ResourceNames`), and reshapes the secret into a minimal `*ActionResult`
rather than returning raw object data. `internal/actions/list_alerts.go` is
the reference for the pod-exec variant: it returns `true` from
`UsesPodExec()`, declares the `pods/exec` `create` RBAC rule, and guards
against `clients.PodExecutor` being nil before using it.

### AWS-action path

An `AWSAction` declares `RequiredServices(target AWSTarget) []awsapi.Service`
instead of RBAC rules — there is no pod exec on this path — and executes
against `awsapi.Clients`. `internal/actions/aws_get.go` is the reference for
a read.

**A mutating AWS action must additionally implement `AWSWriteAction`**
(`Operation() string`, `Resource() string`). `AWSScope` decides whether to
treat an action as a write purely by type-asserting it to `AWSWriteAction`
(`internal/executor/aws.go`): an action that mutates AWS state but only
implements `AWSAction` is judged to be a read and runs unconditionally,
without ever being checked against the write allowlist
(`ROSA_TA_AWS_ALLOWED_OPERATIONS` / `actions.aws_allowed_operations`). Setting
`Type: openapi.Write` in the catalog does **not** enforce this — the catalog
field and the Go interface are never cross-checked, so getting the interface
right is solely the implementer's responsibility. `Operation()` names the
mutation (e.g. `"reboot"`, `"terminate"`) and must appear in the operation
allowlist for the action to be authorized; `Resource()` fixes the kind of
object acted on so a caller-supplied `resource` parameter cannot redirect the
write to a different kind of resource. `internal/actions/aws_write.go` is the
reference implementation: `AWSRebootAction` and `AWSTerminateAction` both set
`Operation()`/`Resource()` and additionally verify, inside `Execute`, that the
target instance carries the cluster's ownership tag before mutating it — a
check layered on top of (not a substitute for) the `AWSWriteAction` gate,
because reachable AWS credentials can otherwise span more than one cluster in
an account.

### Self-registration

Register the action's constructor in an `init()` function so it is
discoverable the moment the `internal/actions` package is imported:

```go
func init() {
	RegisterActionFactory(NewGetPullSecretEmailAction)
}
```

`RegisterActionFactory` takes a *factory* (`func() A`), not an instance, so
every execution gets a fresh value. `Name()` is the registry key used by
`actions.CreateAction`, and it **must match the catalog `Name` exactly** — a
mismatch here reproduces the "catalog but no implementation" failure mode
above even though a `.go` file exists.

### Cross-cutting guidelines

- **Least-privilege RBAC/services.** Prefer `Verbs: ["get"]` + `ResourceNames`
  over `list` whenever the target is a single named object; scope rules to a
  `Namespace` unless the resource is genuinely cluster-scoped; vary the rules
  by `target` when that lets you tighten them further (see
  `internal/actions/describe_nodes.go`).
- **Reshape output deliberately.** Return only the fields a caller needs,
  never raw objects, and never return `Secret` data. `setIfPresent`
  (`internal/actions/actions.go`) helps build sparse output maps.
- **Validate parameters at the boundary.** The API does not reject a request
  for omitting a `Required` catalog parameter — a missing param still returns
  `202` and only fails once `Execute` runs — so the action itself must check
  for and reject missing or malformed `req.Params` early, with a clear error
  (see `internal/actions/list_alerts.go` validating `severity`/`state`).
- **Context and errors.** Thread the passed `ctx` into every client call,
  apply per-call timeouts for exec/network calls, and wrap every error with
  `fmt.Errorf("...: %w", err)` for context.

## Step 2 — Declare policy in the catalog (`internal/catalog/catalog.go`)

Register an `ActionDefinition` in `catalog.New()`. This is what `GET /` and
`GET /{action}` expose to callers, and what the authorization middleware
enforces against:

- **`Name`** must match `Action.Name()` exactly.
- **`Type`** (`openapi.Read` / `openapi.Write`) must honestly reflect the
  behavior — it drives client expectations and auditing, and for AWS actions
  it is not cross-checked against `AWSWriteAction` (see above), so an
  inaccurate `Type` is purely a trust problem, not one the runtime catches.
- **`Scope`** is `openapi.KubeApi` or `openapi.AwsApi`.
- **`AllowedRoles`** is the set of caller roles permitted to invoke the
  action; the `internal/auth` middleware rejects any other role. Grant the
  narrowest set the use case requires rather than the `allRoles` convenience
  slice.
- **`Params`** documents every parameter read from `req.Params` (`Name`,
  `Description`, `Required`, `Default`); `Required` is descriptive only, not
  enforced by the API.
- **Write-safety metadata** (`DryRunAction`, `WriteCooldownSeconds`,
  `Approval`) should be set for any `openapi.Write` action. There is no
  approval *enforcement* system today, so when in doubt, default to
  `Approval: "manual"` and call it out explicitly for reviewers.

### Runtime validations that are independent of both registrations

Authorization runs before the action executes
(`internal/authorization/authorization.go`), and these checks apply
regardless of how carefully the action and catalog entry are written:

- **Namespace allowlist** — a request targeting a namespace outside
  `ROSA_TA_ALLOWED_NAMESPACES` / `actions.allowed_namespaces` is denied; a new
  action targeting a new namespace must have it added there for each
  environment (with no allowlist configured, the authorizer runs in
  permissive mode — local/dev only).
- **Secret allowlist** — access to `secrets` is denied unless the exact
  `namespace/name` is in `ROSA_TA_ALLOWED_SECRETS` / `actions.allowed_secrets`.
  This check validates the caller-supplied target, not what `RequiredRBAC`
  actually grants, and is skipped entirely in permissive mode, so it is
  necessary but not sufficient if an action's RBAC can reach a secret other
  than its declared target.
- **Cluster-scoped targets** — claimed executions are always treated as
  namespaced; `ClusterScoped` is never derived from a missing namespace
  (`internal/executor/kube.go`).

Update `ARCHITECTURE.md`'s Configuration section if the action introduces a
new required namespace or secret.

## Step 3 — Write unit tests

New code must include unit tests with patch coverage ≥ 80% (Codecov-enforced),
using the stdlib `testing` package only. Tests live next to the code
(`internal/actions/<name>_test.go`):

- **Dynamic client** — `dynamicfake.NewSimpleDynamicClientWithCustomListKinds`;
  see `get_pull_secret_email_test.go` for the fixture plus success/error-path
  pattern.
- **Pod executor** — a small fake satisfying `backplane.PodExecutor`; see
  `fakePodExecutor` in `list_alerts_test.go`.
- At minimum, cover: `Name()`'s return value; `RequiredRBAC` for both named
  and list targets; the `Execute` success path (reshaped output and
  `Message`); and error paths (missing resource, malformed data, invalid
  params, and — for pod-exec actions — a nil or failing executor).

See [Integration & Mocks](../testing/integration-and-mocks.md) and
[Testing Strategy](../testing/testing-strategy.md) for the broader test-double
and coverage conventions these tests follow. Run, in order:

```bash
go build ./...                         # fast compile check
go test ./internal/actions/...         # affected tests
go test ./internal/catalog/...         # catalog registration
make test-race                         # race detector
make lint                              # golangci-lint
```

## Step 4 — Local end-to-end verification

Unit tests mock the cluster/account entirely, so a focused local run against
real infrastructure is the fastest way to catch wiring mistakes — a
catalog/implementation mismatch, bad param handling, or a wrong output shape —
before sending the change for review. Two options, both running the same
`internal/executor.Executor` pipeline the server's worker pool uses:

- **`cmd/action-cli`** wires up the full pipeline (authorization, audit,
  backplane/AWS client construction, execution) without running the server,
  for a single focused invocation. It dispatches on the action's interface:
  a `KubeAction` runs through `executor.NewKubeScope`-style wiring against a
  kubeconfig, while an `AWSAction` runs through `executor.NewAWSScope`,
  reading the same `ROSA_TA_AWS_ALLOWED_REGIONS` / `ROSA_TA_AWS_ALLOWED_OPERATIONS`
  environment variables the server reads (there is no CLI-only override, so
  what the CLI can exercise never exceeds what the server would permit). It
  resolves `actions.CreateAction(action)` by name exactly as the worker does,
  so an action missing its `init()` registration fails here the same way it
  would in production.
- **The real server against a disposable `kind` cluster** exercises the HTTP
  surface too: run the server with `ROSA_TA_AUTH=disabled`, bound to loopback
  only (`--listen-addr 127.0.0.1:8080`), with `ROSA_TA_KUBECONFIG` pointed at
  the kind cluster (and, for AWS actions, `ROSA_TA_AWS_ENDPOINT_URL` at a
  loopback LocalStack endpoint plus `ROSA_TA_AWS_ALLOWED_REGIONS`, both
  required even with auth disabled). Then `curl` the catalog
  (`GET /api/v0/trusted-actions/`) and the action's describe endpoint
  (`GET /api/v0/trusted-actions/{action}`), `POST /{action}/run`, and poll
  `GET /runs/{id}` to completion — cross-checking output against `kubectl`
  directly rather than trusting `status: succeeded` alone. Skip
  `integration/itest-up.sh` unless the action needs the Localstack or
  Prometheus/Alertmanager fixtures it provisions (e.g. for `list-alerts`-style
  actions).

Neither path verifies RBAC *correctness* — a local `KubeconfigProvider`
ignores `RequiredRBAC` entirely — so least-privilege review still rests on
unit tests and code review. The most valuable extra check the first time
through: temporarily remove the action's `init()` registration (confirm the
run fails asynchronously with `unknown action`), then temporarily remove the
catalog entry instead (confirm `GET`/`POST` return `404`), then restore both —
this directly confirms the two-registration requirement from the top of this
page is actually satisfied, rather than assuming it from reading the diff.
The Claude Code
[`add-trusted-action`](../../.claude/skills/add-trusted-action/SKILL.md) skill
and its
[local e2e recipe](../../.claude/skills/add-trusted-action/references/local-e2e-testing.md)
automate this entire kind-cluster recipe step by step.

## Step 5 — Review

Trusted Actions run privileged operations on managed infrastructure, so
review is a hard gate:

- At least one approving review from a code owner listed in `OWNERS`.
- Explicit security review of the RBAC/services and policy: least-privilege
  `RequiredRBAC`/`RequiredServices`, an honest `Type`, the narrowest
  `AllowedRoles`, no secret data in output, boundary-validated parameters, and
  (for AWS writes) a correct `AWSWriteAction` implementation with its
  `Operation()` value documented for addition to the deployment's operation
  allowlist.
- Green CI — lint, unit tests, build, coverage, and pre-commit hooks
  (including `gitleaks`) — treated as authoritative; never bypassed with
  `--no-verify` or `SKIP=`.

## Step 6 — Ship

Actions are compiled into the server binary and registered at startup, when
the `internal/actions` package's `init()` functions run as the worker and
executor import it. After the PR merges and a new image rolls out:

1. The action appears automatically in `GET /` (the catalog) and
   `GET /{action}`.
2. Any new namespaces or secrets the action targets must be present in the
   deployed `ROSA_TA_ALLOWED_NAMESPACES` / `ROSA_TA_ALLOWED_SECRETS` (or the
   config file) for each environment, or the authorizer denies the action in
   production.
3. Confirm `AllowedRoles` matches how the action is actually meant to be
   used.
