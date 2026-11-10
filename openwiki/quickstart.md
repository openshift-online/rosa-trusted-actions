---
type: orientation
title: Quickstart
description: What the ROSA Trusted Actions Server is, the minimal local run path with mock auth, and a task-routing map into the rest of the wiki.
tags: [quickstart, onboarding, getting-started, local-development, overview]
sources:
  - id: openwiki-source-362e06c30ccfdafd87339cb0
    resource: repo://ARCHITECTURE.md
  - id: openwiki-source-0f6ed11169bd8e6b34b44280
    resource: repo://cmd/server/main.go
  - id: openwiki-source-296fa323b7900233255b1f1c
    resource: repo://internal/openapi/api.go
  - id: openwiki-source-23775c3de52f3ab95a13cb8b
    resource: repo://README.md
generated: { by: "openwiki/0.7.1", at: "2026-11-10T16:03:16.899Z" }
verified:
  - by: openwiki/0.7.1
    at: 2026-11-10T16:03:16.899Z
---

## What this is

The ROSA Trusted Actions Server is a [Chi](https://github.com/go-chi/chi)-based Go HTTP
server that executes **Trusted Actions** — Kubernetes operations against ROSA/OSD managed
clusters (via Backplane or a local kubeconfig) and AWS operations against a caller's AWS
account — on behalf of an authenticated, authorized caller. Its API surface (routes, types,
server interfaces) is generated from OpenAPI specs via `oapi-codegen`; hand-written handlers in
`internal/handlers` implement the generated `ServerInterface`. Submitted actions run asynchronously on a
background worker pool, with every API call audited and every execution's metadata, output, and
logs retrievable afterward.
(`repo://README.md#L5-L7`, `repo://ARCHITECTURE.md#L1-L5`, `repo://ARCHITECTURE.md#L79-L90`)

## Run it locally in under a minute

The server normally needs live OCM credentials, a reachable JWKS endpoint, a real Backplane
connection, and real AWS credentials. Setting three environment variables bypasses all of that
for local development:

```bash
export ROSA_TA_AUTH=disabled
export ROSA_TA_KUBECONFIG=/tmp/<cluster>.kubeconfig        # or a local kind cluster's kubeconfig
export ROSA_TA_AWS_ENDPOINT_URL=http://127.0.0.1:4566      # e.g. a local LocalStack

make build
make run
```

`ROSA_TA_AUTH=disabled` injects a hardcoded `dev-user` identity carrying the SREP role for every
request, so no `Authorization` header is needed. The server refuses to start under
`ROSA_TA_AUTH=disabled` unless both `ROSA_TA_KUBECONFIG` and `ROSA_TA_AWS_ENDPOINT_URL` are also
set — without them, a mock-authenticated (effectively unauthenticated) caller could reach
production cluster or AWS infrastructure. The server listens on `:8080` by default.
(`repo://README.md#L22-L32`, `repo://cmd/server/main.go#L186-L200`, `repo://cmd/server/main.go#L63`)

Verify it's up and list the action catalog:

```bash
curl -s http://localhost:8080/health | jq .
curl -s http://localhost:8080/api/v0/trusted-actions/ | jq .
```

From there, `POST /api/v0/trusted-actions/{action}/run` submits an action (returns `202` with
`status: pending`) and `GET /api/v0/trusted-actions/runs/{id}` polls for its result — see
[Workflow: Executing a Trusted Action](workflows/executing-a-trusted-action.md) for the full
request lifecycle, and
[Operations: Local Development Environment](operations/local-development.md) for Postgres/SQLite
setup, helper scripts, and why each mock-auth guard exists.

## Where to go next

| I want to... | Read |
|---|---|
| Understand the overall system design, process structure, and request flow | [Architecture Overview](architecture/overview.md) |
| See how the OpenAPI spec is bundled and generated into server code | [API Surface & Code Generation](architecture/api-surface.md) |
| Learn what a Trusted Action is and how the catalog declares its policy | [Trusted Actions & Catalog](concepts/trusted-actions-catalog.md) |
| Understand how an execution is authorized and run independent of HTTP | [Execution Pipeline & Scopes](concepts/execution-pipeline.md) |
| Understand role- and resource-level authorization | [Authorization Model](concepts/authorization-model.md) |
| See the Execution/AuditEntry data model and storage backends | [Data Model & Persistence](concepts/data-model-and-persistence.md) |
| Add a brand-new Trusted Action end to end | [Workflow: Adding a New Trusted Action](workflows/adding-a-trusted-action.md) |
| Trace one execution from HTTP submission to audited result | [Workflow: Executing a Trusted Action](workflows/executing-a-trusted-action.md) |
| Know which CLI (`rta`, `action-cli`, `test-client`) to use | [Workflow: CLI Clients](workflows/cli-clients.md) |
| Understand OCM authentication/authorization integration | [Integration: OCM](integrations/ocm.md) |
| Understand how the server reaches a cluster's Kubernetes API | [Integration: Backplane](integrations/backplane.md) |
| Understand how the server obtains AWS credentials per cluster | [Integration: AWS Access & Actions](integrations/aws.md) |
| Find every config flag, env var, and config-file key | [Operations: Configuration Reference](operations/configuration.md) |
| Understand how the service is deployed and its CI pipelines | [Operations: Deployment Architecture](operations/deployment.md) |
| Set up day-to-day local development (build/run/lint, DB setup) | [Operations: Local Development Environment](operations/local-development.md) |
| Learn unit test conventions and coverage expectations | [Testing: Unit Test Conventions & Coverage](testing/testing-strategy.md) |
| Run or extend the kind+LocalStack integration harness or Backplane test doubles | [Testing: Integration Harnesses & Mocks](testing/integration-and-mocks.md) |

## Everyday commands

```bash
make generate         # bundle OpenAPI spec + generate Go server code
make build             # generate + compile the server binary
make test              # run unit tests
make test-integration  # run integration tests (starts the server)
make lint              # run golangci-lint
make fmt               # auto-format code
```

Run `make help` for the full list of targets, including `build-cli`, `build-rta`,
`build-test-client`, `run-postgres`, `run-sqlite`, and the `itest-*` kind/LocalStack harness
targets. (`repo://Makefile#L1-L30`, `repo://Makefile#L179-L190`)
