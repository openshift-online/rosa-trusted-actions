---
name: add-trusted-action
description: Author a new Trusted Action end-to-end — implementation, catalog policy, tests, and review/deploy checklist. Use when asked to add, create, or implement a new Trusted Action (or action) for the ROSA Trusted Actions Server.
---

# Add a Trusted Action

Full reference: [`docs/adding-a-trusted-action.md`](../../../docs/adding-a-trusted-action.md).
Read it before starting if any step below is unclear — this skill summarizes it into an
actionable workflow but the doc is authoritative.

A Trusted Action requires **two mandatory, independent registrations**. Missing either one
breaks the action at runtime (not at compile time):

| Layer | File | Purpose |
|-------|------|---------|
| Implementation | `internal/actions/<name>.go` | What it does + the exact RBAC it needs. Implements `actions.Action`, self-registers via `init()`. |
| Catalog (policy) | `internal/catalog/catalog.go` | Public contract: type, scope, allowed roles, params, write-safety metadata. |

## Workflow

### 1. Clarify the action's shape before writing code
If not already specified, confirm with the user:
- Action name (kebab-case, stable — used in URL path, catalog, audit records).
- Read or Write (`openapi.Read` / `openapi.Write`)?
- Scope: `openapi.KubeApi` or `openapi.AwsApi`?
- Does it need pod exec (run a command / hit an HTTP endpoint inside a pod)?
- What's the minimum RBAC (verbs, resources, namespace(s), named resources vs. list)?
- Which caller roles should be allowed (narrowest set — see `config/config.yaml`)?
- Any new namespace or secret this action needs to touch?

### 2. Implement `internal/actions/<name>.go`
- Implement the `actions.Action` interface (`internal/actions/actions.go`):
  `Name()`, `RequiredRBAC(target ResourceTarget)`, `UsesPodExec()`,
  `Execute(ctx, clients, req) (*ActionResult, error)`.
- Register with `init() { RegisterActionFactory(NewXxxAction) }` — factory function,
  not an instance.
- `Name()` must match the catalog `Name` **exactly**.
- Least-privilege RBAC: prefer `Verbs: ["get"]` + `ResourceNames` over `list`;
  scope to a `Namespace` unless genuinely cluster-scoped; vary rules by `target`
  when that tightens them (see `internal/actions/describe_nodes.go`).
- Reshape output: return only needed fields, **never return `Secret` data**;
  use `setIfPresent` (`actions.go`) for sparse output maps.
- Validate `req.Params` at the boundary with clear errors (see `list_alerts.go`).
  A missing required param still returns `202` from the API — the action itself
  must reject it during `Execute`.
- Pod-exec actions: return `true` from `UsesPodExec()`, add a `pods/exec` `create`
  RBAC rule, use `clients.PodExecutor`, guard against it being nil. Reference:
  `internal/actions/list_alerts.go`.
- Thread `ctx` through every client call; apply per-call timeouts for exec/network
  calls.
- Wrap every error with `fmt.Errorf("...: %w", err)` for context.
- Simplest reference implementation: `internal/actions/get_pull_secret_email.go`.

### 3. Register the policy in `internal/catalog/catalog.go`
Add a `c.register(&catalog.ActionDefinition{...})` call in `catalog.New()`:
- `Name` — matches `Action.Name()` exactly.
- `Description` — short, caller-facing.
- `Type` — `openapi.Read` or `openapi.Write`, honestly reflecting behavior.
- `Scope` — `openapi.KubeApi` or `openapi.AwsApi`.
- `AllowedRoles` — narrowest set of roles from `config/config.yaml`
  (`allRoles` only if genuinely all roles need it).
- `Params` — one `ParamDefinition` (`Name`, `Description`, `Required`, `Default`)
  per param read from `req.Params`. Document every one.
- For `openapi.Write` actions, set write-safety metadata: `DryRunAction`
  (read-only counterpart), `WriteCooldownSeconds`, `Approval`.

### 4. Account for runtime authorization
These are enforced in `internal/authorization/authorization.go` **before** the
action runs — code review catches missing allowlist entries, so check these up front:
- **New namespace?** Add it to `ROSA_TA_ALLOWED_NAMESPACES` /
  `actions.allowed_namespaces` for the relevant environments.
- **Reads a secret?** Add the exact `namespace/name` to `ROSA_TA_ALLOWED_SECRETS` /
  `actions.allowed_secrets`. This checks the caller-supplied target, not what
  `RequiredRBAC` actually grants, and is skipped entirely in permissive mode
  (no namespace allowlist configured) — it's not sufficient on its own if the
  action's RBAC can reach a different resource than the target it was given.
- **Cluster-scoped target?** Remember claimed executions are always treated as
  namespaced by the worker.
- If a new required namespace/secret is introduced, update the Configuration
  section of `ARCHITECTURE.md`.

### 5. Write tests (`internal/actions/<name>_test.go`)
Patch coverage must be **≥ 80%** (Codecov-enforced). Stdlib `testing` only.
- Fake clients: `dynamicfake.NewSimpleDynamicClientWithCustomListKinds` for the
  dynamic client (pattern: `get_pull_secret_email_test.go`); a fake satisfying
  `backplane.PodExecutor` for pod-exec actions (pattern: `fakePodExecutor` in
  `list_alerts_test.go`).
- Cover at minimum: `Name()` value, `RequiredRBAC` for both named and list
  targets, `Execute` success path (reshaped output + `Message`), and error paths
  (missing resource, malformed data, invalid params, nil/failing pod executor).

Run, in order:
```bash
go build ./...
go test ./internal/actions/...
go test ./internal/catalog/...
make test-race
make lint
```

### 6. Local end-to-end test against a real cluster
Unit tests mock the cluster; this step runs the action through the real server
and a disposable `kind` cluster — no mocks in the execution path — and is the
quickest way to catch wiring mistakes (catalog/implementation mismatch, bad
param handling, wrong output shape) before review. It does **not** verify RBAC
correctness — the local `KubeconfigProvider` ignores `RequiredRBAC`, so rely on
unit tests and code review for that. Full step-by-step recipe:
[`references/local-e2e-testing.md`](references/local-e2e-testing.md).

Summary of the recipe:
1. `kind create cluster` → disposable kubeconfig.
2. `make build` and run the server locally with `ROSA_TA_AUTH=disabled` and
   `ROSA_TA_KUBECONFIG` pointed at the kind cluster (dev-only auth bypass).
3. `curl` the catalog (`GET /api/v0/trusted-actions/`) and the action's describe
   endpoint (`GET /api/v0/trusted-actions/{action}`) to confirm it's wired up.
4. `POST /{action}/run`, poll `GET /runs/{id}` to completion, and cross-check
   the output against `kubectl` directly rather than trusting `status:
   succeeded` alone.
5. Optional but valuable the first time: temporarily remove the `init()`
   registration (expect run to fail async with `unknown action`), then
   temporarily remove the catalog entry (expect `404`) — confirms the two
   mandatory registrations are actually both wired in, then restore both.
6. Tear down the `kind` cluster, kill the server process, and confirm
   `git status` is clean of throwaway test artifacts before committing.

Skip `integration/itest-up.sh` unless the action needs localstack or the
Prometheus/Alertmanager fixtures it sets up for `list-alerts`.

### 7. Self-check against the checklist before handing off for review
- [ ] `internal/actions/<name>.go` implements `actions.Action`, self-registers in `init()`.
- [ ] `RequiredRBAC` is least-privilege and scoped to the target.
- [ ] `UsesPodExec()` correct; pod-exec actions declare `pods/exec` and guard nil executor.
- [ ] `Execute` validates params, wraps errors with context, never leaks secret data.
- [ ] `internal/catalog/catalog.go` entry: matching `Name`, correct `Type`/`Scope`,
      narrowest `AllowedRoles`, documented `Params`, write-safety metadata if `Write`.
- [ ] New namespaces/secrets added to allowlists and documented in `ARCHITECTURE.md`.
- [ ] Unit tests cover name, RBAC, success, and error paths; coverage ≥ 80%.
- [ ] `go build ./...`, `make test-race`, `make lint` all pass.
- [ ] Verified end-to-end against a local `kind` cluster (step 6).

### 8. Review and deploy (human-gated, not something this skill automates)
- Needs an approving review from a code owner (`OWNERS`) plus explicit security
  review of the RBAC/policy (least privilege, honest `Type`, narrow
  `AllowedRoles`, no secret leakage, boundary validation).
- CI (lint, tests, build, coverage, pre-commit/gitleaks) must be green —
  never bypass with `--no-verify` or `SKIP=`.
- After merge/deploy: confirm the action appears in `GET /` and `GET /{action}`,
  and that any new namespace/secret allowlist entries are present in each
  target environment.

## Guardrails (from this repo's CLAUDE.md, apply throughout)
- Minimal diffs — don't reformat or refactor unrelated code.
- Never edit `internal/openapi/api.go` (generated).
- Follow existing patterns in `internal/actions/` and `internal/catalog/` rather
  than introducing new abstractions.
- Don't commit secrets; don't disable pre-commit hooks or bypass CI.
