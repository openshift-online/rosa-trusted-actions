---
type: workflow
title: "Workflow: CLI Clients (rta, action-cli, test-client)"
description: Explains the three command-line entry points in the repository — rta (the production HTTP API client), action-cli (a direct-executor developer CLI), and test-client (a thin HTTP smoke-test tool) — and when to use each.
tags: [cli, rta, action-cli, test-client, developer-tooling, http-client, executor]
verified:
  - by: openwiki/0.7.1
    at: 2026-11-05T16:04:34.033Z
sources:
  - id: openwiki-source-2bd1941dc3b0982040174920
    resource: repo://cmd/action-cli/main.go
  - id: openwiki-source-61ec3ade0560d76893411e32
    resource: repo://cmd/action-cli/README.md
  - id: openwiki-source-95953b91d4fefaa8acda8d56
    resource: repo://cmd/rta/main.go
  - id: openwiki-source-32c0fff567fdde8528ad3f5e
    resource: repo://cmd/test-client/main.go
  - id: openwiki-source-c1b4564c79261d1dde804058
    resource: repo://internal/rta/action.go
  - id: openwiki-source-aed7cad05fcd20c3136ae406
    resource: repo://internal/rta/audit.go
  - id: openwiki-source-2f8d67e45d16bd5e4f090570
    resource: repo://internal/rta/auth.go
  - id: openwiki-source-a6e4e07d2ddc088397d6f6a4
    resource: repo://internal/rta/client.go
  - id: openwiki-source-c0a171685ca193a23a196f0a
    resource: repo://internal/rta/command.go
  - id: openwiki-source-3c2124c18e2ce41cc7673924
    resource: repo://internal/rta/execution.go
generated: { by: "openwiki/0.7.1", at: "2026-11-05T16:04:34.033Z" }
---

The repository ships three separate `cmd/` binaries that all "run a trusted action" in some sense, but they sit at different layers and serve different audiences. Knowing which one to reach for — and why they are not interchangeable — avoids confusion when debugging or writing documentation.

| Binary | Package | Talks to | Audience | Bypasses |
|---|---|---|---|---|
| `rta` (`cmd/rta`) | `internal/rta` | HTTP API (`/api/v0/trusted-actions/...`) | Real client — operators and automation | Nothing; exercises the full server stack |
| `action-cli` (`cmd/action-cli`) | builds the executor pipeline inline | Kubernetes/AWS directly via kubeconfig / AWS SDK | Developers verifying an action's logic | The HTTP server, the queue/worker, and the execution store entirely |
| `test-client` (`cmd/test-client`) | `main` only, uses `internal/openapi` types | HTTP API (same endpoints as `rta`) | Developers/integration tests exercising a running server | Nothing; it is a minimal HTTP caller |

`rta` is the one production clients and operators actually use. `action-cli` and `test-client` are developer/test tooling with overlapping but distinct scopes: `action-cli` drives the executor pipeline directly (no HTTP, no server process), while `test-client` drives the server's HTTP API like `rta` does, but with far less structure (no subcommands, no catalog/describe, no audit queries — just submit-and-poll).

## `rta`: the production client

`cmd/rta/main.go` is a two-line wrapper around `rta.NewCommand()` from `internal/rta`; all behavior lives in that package. `rta` is the client that production users (and `rosactl`-style tooling) are meant to install and run against a live Trusted Actions server. Per its root command's doc comment, the command tree is deliberately decoupled from global state so `rta` can later become a `rosactl` plugin.

### Command structure

```
rta
├── action
│   ├── list        # GET /api/v0/trusted-actions/
│   ├── describe    # GET /api/v0/trusted-actions/{action}
│   └── run         # POST /api/v0/trusted-actions/{action}/run, then polls for completion
├── execution
│   ├── list        # GET /api/v0/trusted-actions/runs
│   ├── get         # GET /api/v0/trusted-actions/runs/{id}
│   └── output      # GET /api/v0/trusted-actions/runs/{id}/output
├── audit
│   └── list        # GET /api/v0/trusted-actions/audit
└── version
```
(`internal/rta/command.go`, `internal/rta/action.go`, `internal/rta/execution.go`, `internal/rta/audit.go`)

Each command group (`action`, `execution`, `audit`) is built by a `commandGroup` that holds a shared Viper config (env prefix `RTA`) and builds an `internal/rta.Client` on demand via `newClient`. `action list`/`action describe` build an unauthenticated client; `action run`, `execution *`, and `audit list` require a bearer token.

### Flags and configuration

Persistent root flags, bound through Viper (`internal/rta/command.go`):

| Flag | Env | Purpose |
|---|---|---|
| `--server` | `RTA_SERVER` | Base URL of the Trusted Actions API server |
| `--token` | — | Bearer token; falls back to the active OCM CLI login (`ocm-cli` config's access token) via `resolveToken` in `internal/rta/auth.go` |
| `--timeout` | — | Deadline for non-interactive operations (default 5 minutes, `defaultTimeout`) |
| `--verbose` | — | Enable debug diagnostics |

`resolveToken` prefers an explicit `--token`; otherwise it loads the local OCM config, requires a non-empty access token, and checks it is not expired (`cfg.Armed(false)`) before using it — there is no automatic refresh yet (noted as a TODO).

### HTTP client and HTTPS-downgrade protection

`internal/rta.NewClient` (in `internal/rta/client.go`) builds the HTTP client used for every request. Two invariants matter operationally:

- **Credential safety over plain HTTP**: if a token is configured, `NewClient` refuses to proceed unless the server URL is HTTPS or a loopback address (`localhost`, `127.0.0.1`, `::1`); otherwise it returns an error immediately rather than sending a bearer token over plain HTTP to a remote host.
- **HTTPS-downgrade redirect protection**: the client's `CheckRedirect` hook (`rejectHTTPSDowngrade`) refuses to follow a redirect from an HTTPS URL to a non-HTTPS URL (which would otherwise leak the Authorization header to an insecure endpoint), and caps redirect chains at 10 hops.

All requests carry a `User-Agent: rta/<version> (<platform>)` header and an `Accept: application/json` header; authenticated requests add `Authorization: Bearer <token>`. Non-2xx responses are decoded into an `APIError` that preserves the API's `code`/`reason` fields when present, or a truncated raw body otherwise (`internal/rta/client.go`).

### Representative commands

```bash
# Discover what actions exist
rta action list
rta action list -o json

# Inspect an action's parameters
rta action describe get

# Run an action and wait for completion
rta action run get \
  --cluster 1234 --jira OHSS-1234 \
  --param resource=pods --param namespace=openshift-monitoring

# Submit without waiting, and check status/audit later
rta action run get --cluster 1234 --jira OHSS-1234 --param resource=pods --no-wait
rta execution get <execution-id>
rta audit list --action get --cluster 1234 -o json
```

`action run` polls `execution get`-equivalent calls until the execution reaches a terminal status, then fetches `execution output` to print the result (`internal/rta/action.go`, `g.waitForCompletion`). `--dry-run` and `--force` map directly to the `ExecutionRequest.DryRun`/`.Force` fields. See [Executing a Trusted Action](./executing-a-trusted-action.md) for the full request/execution lifecycle, and [API Surface](../architecture/api-surface.md) for the underlying endpoints.

## `action-cli`: direct-executor developer CLI

`cmd/action-cli` is a pre-HTTP-server test CLI: it wires the same executor pipeline the server uses — authorization, audit logging, a backplane/Kubernetes client (or an AWS static provider), and action execution — directly against a kubeconfig (or AWS credentials/environment), with **no HTTP server, queue, or execution store involved at all**. It exists to let a developer verify that an action behaves correctly end-to-end without standing up the server.

`cmd/action-cli/main.go` builds this by hand for each invocation:

1. Resolves the requested action via `actions.CreateAction(action)`.
2. Builds an `audit.NewMockLogger` (an in-process stand-in for the server's audit sink).
3. Branches on the action's scope:
   - For `actions.AWSAction` implementations, it builds `authorization.NewAWS` from the `ROSA_TA_AWS_ALLOWED_REGIONS` / `ROSA_TA_AWS_ALLOWED_OPERATIONS` environment variables (the same variables the server reads — there is deliberately no CLI-only allowlist flag, so what the CLI permits can never be broader than what the server permits) and an `awsapi.NewStaticProvider` wired from `--aws-region`/`--aws-endpoint-url`/`--aws-infra-id`, then an `executor.NewAWSScope`.
   - Otherwise, it builds `authorization.New` from `--allowed-namespaces`/`--allowed-secrets` (defaulting the namespace allowlist to `--namespace` if unset), a `backplane.NewKubeconfigProvider` from `--kubeconfig`, and an `executor.NewKubeScope`.
4. Calls `executor.New(logger, auditor, scope).Execute(ctx, executor.Request{...})` synchronously and prints `Allowed`, `Reason`, and (on success) the output message and any resources.

Because it enforces the same `authorization` package rules as the server (namespace allowlisting, cluster-scoped opt-in, secret deny-by-default), it is a faithful stand-in for verifying authorization decisions as well as the action's own logic — not just a bypass for convenience. See [Executing a Trusted Action](./executing-a-trusted-action.md) and [Execution Pipeline](../concepts/execution-pipeline.md) for how the server composes the same executor/authorization/audit pieces in the HTTP path.

### Representative commands

```bash
# GET: list configmaps in a namespace
action-cli run \
  --kubeconfig ~/.kube/config \
  --action get --namespace openshift-monitoring --resource configmaps

# PATCH: merge-patch a specific configmap
action-cli run \
  --kubeconfig ~/.kube/config \
  --action patch --namespace openshift-monitoring --resource configmaps \
  --name cluster-monitoring-config --patch '{"data":{"key":"value"}}'

# DELETE: remove a configmap
action-cli run \
  --kubeconfig ~/.kube/config \
  --action delete --namespace openshift-monitoring --resource configmaps \
  --name my-configmap
```

Omitting both `--namespace` and `--cluster-scoped` is rejected; secrets are denied unless explicitly allowlisted via `--allowed-secrets namespace/name`. Denied requests are still recorded through the mock audit logger and the CLI exits non-zero (`cmd/action-cli/README.md`).

## `test-client`: thin HTTP smoke-test tool

`cmd/test-client/main.go` is a single-file, dependency-light HTTP client used to exercise a *running* server's API for manual testing or integration tests. Unlike `rta`, it has no subcommand tree, no catalog/describe/audit support, and no OCM-config token resolution beyond shelling out to `ocm token` as a default — it exists purely to submit one action execution and poll it to completion.

Its flow (`cmd/test-client/main.go`):

1. Resolve a bearer token: use `--token` if given, otherwise run `ocm token` and use its stdout.
2. Build an `openapi.ExecutionRequest` from `--cluster-id`, `--jira`, repeatable `--param key=value` flags, `--dry-run`, and `--force`.
3. `POST {server}/api/v0/trusted-actions/{action}/run` and print the created execution's ID/status.
4. Poll `GET {server}/api/v0/trusted-actions/runs/{id}?include=output` every `--poll-interval` (default 2s) until the status is terminal (`succeeded`, `failed`, or `timed_out`).
5. Print the final execution as indented JSON and exit non-zero if the status is not `succeeded`.

It uses a plain `*http.Client{Timeout: 30 * time.Second}` with no HTTPS-downgrade protection or redirect hardening — appropriate for pointing at `http://localhost:8080` during local development, but not a hardened client the way `rta`'s is.

### Representative command

```bash
test-client \
  --server http://localhost:8080 \
  --action get --cluster-id 1234 --jira OHSS-1234 \
  --param resource=pods --param namespace=openshift-monitoring
```

See [Local Development](../operations/local-development.md) for how to run the server that `test-client` (and `rta`, when pointed at `localhost`) talk to.

## Choosing between them

- Validating a new or changed **action's business logic and authorization** without running the server at all → `action-cli`.
- Exercising the **running server's HTTP API** for a quick manual check or integration test, without needing the full `rta` feature set → `test-client`.
- Anything resembling real usage — discovering actions, running them against a real deployment, checking execution status/output, querying audit history → `rta`.
