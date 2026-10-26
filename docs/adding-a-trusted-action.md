# Adding a New Trusted Action

This guide walks through the full lifecycle of authoring a new Trusted Action:
implement it, declare its policy, test it, get it reviewed, and ship it.

If you only need a refresher on the runtime, see
[ARCHITECTURE.md](../ARCHITECTURE.md). For local setup see
[DEVELOPMENT.md](../DEVELOPMENT.md), and for testing conventions see
[TESTING.md](../TESTING.md).

If you're using Claude Code, the
[`add-trusted-action`](../.claude/skills/add-trusted-action/SKILL.md) skill
packages this guide into an actionable workflow, including a local
`kind`-cluster end-to-end testing recipe.

## What a Trusted Action is

A Trusted Action is a named, auditable operation that the server runs against a
managed cluster on a caller's behalf. Callers never talk to the cluster
directly — they submit an action through the API, the server authorizes it,
requests a least-privilege backplane session scoped to exactly the RBAC the
action declares, runs it, and records an audit entry.

An action is defined in **two** places, and both are required:

| Layer | File | Purpose |
|-------|------|---------|
| **Implementation** | `internal/actions/<name>.go` | The behavior: what it does on the cluster and the exact RBAC it needs. Implements the `actions.Action` interface and self-registers. |
| **Catalog (policy)** | `internal/catalog/catalog.go` | The policy and contract: action type, scope, allowed caller roles, parameters, and write-safety metadata. This is what the API exposes and validates against. |

> **Both registrations are mandatory.** They are deliberately independent so the
> public contract (catalog) is reviewed separately from the implementation.
> If you register only one:
> - **Catalog but no implementation** — the API accepts `POST /{action}/run`,
>   but the background worker fails the execution with `unknown action "<name>"`
>   (see `worker/executor_runner.go` → `actions.CreateAction`).
> - **Implementation but no catalog** — `GET /{action}` and
>   `POST /{action}/run` return `404 Unknown action` (handlers validate against
>   the catalog in `internal/handlers/api.go`).

### Execution pipeline

Both the server's worker pool and `cmd/action-cli` run actions through the same
`internal/executor.Executor`:

```
request → authorize (namespace/secret allowlist) → backplane session (scoped to RequiredRBAC)
        → Action.Execute → audit record
```

The executor builds a backplane client from `Action.RequiredRBAC(target)` and,
only if `Action.UsesPodExec()` is true, also a pod executor. The action receives
these as `actions.Clients` and never constructs its own cluster credentials.

## Step 1 — Implement the action

Create `internal/actions/<name>.go` and implement the `actions.Action`
interface (`internal/actions/actions.go`):

```go
type Action interface {
	Name() string
	RequiredRBAC(target ResourceTarget) []backplane.RBACRule
	UsesPodExec() bool
	Execute(ctx context.Context, clients Clients, req ActionRequest) (*ActionResult, error)
}
```

Self-register the action with an `init()` function so it is discoverable the
moment the package is imported:

```go
func init() {
	RegisterActionFactory(NewGetPullSecretEmailAction)
}
```

`RegisterActionFactory` takes a factory (`func() A`) rather than an instance, so
each execution gets a fresh action value. `Name()` is the registry key and MUST
match the catalog `Name` exactly.

### Worked example

`internal/actions/get_pull_secret_email.go` is the simplest full custom action.
Key points:

- `Name()` returns the stable action name (`"get-pull-secret-email"`), used in
  the URL path, the catalog, and audit records.
- `UsesPodExec()` returns `false` — it only talks to the Kubernetes API, so the
  executor never provisions a pod executor.
- `RequiredRBAC` returns the **minimum** permissions: `get` on the single named
  secret in one namespace.
- `Execute` uses the injected `clients.Dynamic` client, returns an
  `*ActionResult` with the reshaped output and a short human-readable `Message`,
  and wraps every error with `fmt.Errorf("...: %w", err)` for context.

### Guidelines

- **Least privilege RBAC.** Narrow `RequiredRBAC` to the exact
  groups/resources/verbs, and prefer `ResourceNames` and `Verbs: ["get"]` over
  `list` whenever the target is a single named object. Scope rules to a
  `Namespace` unless the resource is genuinely cluster-scoped. Make the rules
  depend on the `target` when that lets you tighten them (see
  `describe_nodes.go`, which switches between `get`+`ResourceNames` and `list`).
- **Reshape output deliberately.** Return only the fields a caller needs rather
  than raw objects, and never return `Secret` data. `setIfPresent` (in
  `actions.go`) helps build sparse output maps.
- **Validate parameters at the boundary.** Reject unknown or malformed
  `req.Params` values early with a clear error (see `list_alerts.go`, which
  validates `severity` and `state`).
- **Pod-exec actions.** If the action must reach a pod's HTTP endpoint or run a
  command in a container, return `true` from `UsesPodExec()`, add the
  `pods/exec` `create` RBAC rule, and use `clients.PodExecutor`. Guard against a
  nil executor. `list_alerts.go` is the reference implementation (it queries
  Prometheus/Alertmanager via `wget` inside the pod).
- **Context.** Thread the passed `ctx` into every client call; apply per-call
  timeouts for exec/network calls as `list_alerts.go` does.

## Step 2 — Declare policy in the catalog

Register an `ActionDefinition` in `catalog.New()` in
`internal/catalog/catalog.go`. This is the public contract and the policy the
API enforces:

```go
c.register(&catalog.ActionDefinition{
	Name:         "get-pull-secret-email",       // MUST match Action.Name()
	Description:  "Get the registered email from the cluster pull secret",
	Type:         openapi.Read,                   // Read (read-only) or Write (mutating)
	Scope:        openapi.KubeApi,                // KubeApi or AwsApi
	AllowedRoles: allRoles,                        // caller roles permitted to run it
	Params: []catalog.ParamDefinition{
		{Name: "severity", Description: "Filter by severity", Default: ""},
		{Name: "resource", Description: "Resource type (plural)", Required: true},
	},
})
```

Fields that make up the policy:

- **`Type`** — `openapi.Read` for read-only actions, `openapi.Write` for
  mutating ones. Be honest here: it drives client expectations and auditing.
- **`Scope`** — `openapi.KubeApi` or `openapi.AwsApi`.
- **`AllowedRoles`** — the caller roles allowed to invoke the action. The
  authz middleware (`internal/auth`) rejects callers whose role is not listed.
  Roles are defined in `config/config.yaml` (e.g. `SREP`,
  `ConfigurationAnomalyDetection`, `ROSAAiAgent`); `allRoles` in `catalog.go` is
  the convenience slice of all of them. Grant the narrowest set that the use
  case requires.
- **`Params`** — each `ParamDefinition` sets `Name`, `Description`, `Required`,
  and `Default`. These surface through `GET /{action}` so callers know the
  contract. Document every param you read from `req.Params`. `Required` is
  descriptive only — the API does not reject a request for omitting a
  required param. A missing param still returns `202` and only fails
  asynchronously once `Execute` runs, so the action itself must check for it
  and return an error (see "Validate parameters at the boundary" above).
- **Write-safety metadata** (write actions): `DryRunAction` names a read-only
  counterpart used for dry runs, `WriteCooldownSeconds` throttles repeat
  mutations, and `Approval` records an approval requirement. Set these for any
  `openapi.Write` action.

### Runtime validations you must account for

Authorization runs **before** the action executes
(`internal/authorization/authorization.go`):

- **Namespace allowlist** — requests targeting a namespace not in
  `ROSA_TA_ALLOWED_NAMESPACES` are denied. If your action targets a new
  namespace, it must be added to the allowlist (env var or `config.yaml`
  `actions.allowed_namespaces`) in the relevant environments, or the action will
  be denied in production. (With no allowlist configured the authorizer runs in
  permissive mode — local/dev only.)
- **Secret deny-by-default** — any access to `secrets` is denied unless the
  exact `namespace/name` is in `ROSA_TA_ALLOWED_SECRETS` /
  `actions.allowed_secrets`. An action that reads a secret must have that secret
  allowlisted.
- **Cluster-scoped targets** — claimed executions are always treated as
  namespaced (the worker never trusts a caller-supplied cluster-scoped flag; see
  the comment in `worker/executor_runner.go`). Keep this in mind for actions
  whose targets are cluster-scoped.

Update `ARCHITECTURE.md` (the Configuration section) if your action introduces a
new required namespace or secret.

## Step 3 — Test the action

New code must include unit tests, and patch coverage must be **≥ 80%**
(enforced by Codecov — see [CONTRIBUTING.md](../CONTRIBUTING.md)). Use the
stdlib `testing` package only — no third-party test frameworks.

### Unit tests

Tests live next to the code (`internal/actions/<name>_test.go`). Use the
Kubernetes fake clients so no real cluster is needed:

- **Dynamic client** — `dynamicfake.NewSimpleDynamicClientWithCustomListKinds`.
  See `get_pull_secret_email_test.go` for the fixture + success/error-path
  pattern.
- **Pod executor** — implement a small fake satisfying
  `backplane.PodExecutor`. See `fakePodExecutor` in `list_alerts_test.go`.

Cover, at minimum:

1. `Name()` returns the expected value.
2. `RequiredRBAC` returns least-privilege rules for both named and list targets.
3. `Execute` success path (asserting reshaped output and `Message`).
4. Error paths: missing resource, malformed data, invalid params, and (for
   pod-exec actions) a nil or failing executor.

Run:

```bash
go build ./...                         # fast compile check
go test ./internal/actions/...         # affected tests
go test ./internal/catalog/...         # catalog registration
make test-race                         # race detector
make lint                              # golangci-lint
```

### End-to-end check against a real cluster

For the primitive `get` / `patch` / `delete` actions, `cmd/action-cli` wires up
the full executor pipeline (authorization, audit, backplane, execution) without
running the server — see [cmd/action-cli/README.md](../cmd/action-cli/README.md).
For a bespoke action, run it through the server locally (`ROSA_TA_AUTH=disabled`,
a kubeconfig via `ROSA_TA_KUBECONFIG`) as described in the
[README](../README.md), then `POST /{action}/run` and poll `GET /runs/{id}`.

## Step 4 — Review requirements

Trusted Actions run privileged operations on managed clusters, so review is a
hard gate, not a formality.

- **At least one approving review from a code owner** listed in
  [OWNERS](../OWNERS) (`/lgtm` + `/approve` Prow flow).
- **Security review of the RBAC and policy.** Reviewers specifically check that:
  - `RequiredRBAC` is least-privilege (no blanket `list`/`*`, scoped namespaces,
    named resources where possible).
  - `Type` honestly reflects read vs. write, and write actions set the
    appropriate dry-run / cooldown / approval metadata.
  - `AllowedRoles` is the narrowest set needed.
  - No secret data is returned in output and any secret access is allowlisted.
  - Parameters are validated at the boundary.
- **CI must be green** — lint, unit tests, build, coverage, and the pre-commit
  hooks (including `gitleaks`). CI is the authoritative gate; never bypass it
  (no `--no-verify`, no `SKIP=`).
- AI-assisted contributions follow the same standards and must carry
  `Co-Authored-By` trailers (see [CONTRIBUTING.md](../CONTRIBUTING.md)).

## Step 5 — Deploy

Actions are compiled into the server binary and registered at startup (their
package `init()` functions run when `internal/actions` is imported by the worker
and executor). Once your PR merges and a new image is built and rolled out:

1. The action appears in `GET /` (catalog) and `GET /{action}` automatically.
2. Ensure any new **namespaces** or **secrets** the action targets are present
   in the deployed `ROSA_TA_ALLOWED_NAMESPACES` / `ROSA_TA_ALLOWED_SECRETS` (or
   the config file) for each environment — otherwise the authorizer denies it in
   production.
3. Confirm the caller roles in `AllowedRoles` match how the action is meant to
   be used.

## Checklist

- [ ] `internal/actions/<name>.go` implements `actions.Action` and self-registers in `init()`.
- [ ] `RequiredRBAC` is least-privilege and scoped to the target.
- [ ] `UsesPodExec()` is correct; pod-exec actions declare `pods/exec` and guard a nil executor.
- [ ] `Execute` validates params, wraps errors with context, and never leaks secret data.
- [ ] `internal/catalog/catalog.go` registers an `ActionDefinition` with a matching `Name`, correct `Type`/`Scope`, narrowest `AllowedRoles`, documented `Params`, and write-safety metadata for write actions.
- [ ] New namespaces/secrets added to the allowlists and documented in `ARCHITECTURE.md`.
- [ ] Unit tests cover name, RBAC, success, and error paths; patch coverage ≥ 80%.
- [ ] `go build ./...`, `make test-race`, and `make lint` pass.
- [ ] PR approved by a code owner with security review; CI green.
