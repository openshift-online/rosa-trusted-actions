<!--
  Sync Impact Report
  Version change: N/A → 1.0.0 (initial ratification)
  Added principles:
    - I. OpenAPI-First
    - II. Contract-First Interface
    - III. Security by Default
    - IV. Enum-Driven Domain Modeling
    - V. Separation of Concerns
    - VI. Testing Discipline
    - VII. Build Automation
    - VIII. Go Idioms
  Added sections:
    - Architecture Constraints
    - Development Workflow
    - Governance
  Removed sections: none
  Templates requiring updates:
    - .specify/templates/plan-template.md ✅ compatible (Constitution Check section exists)
    - .specify/templates/spec-template.md ✅ compatible (no principle-specific content)
    - .specify/templates/tasks-template.md ✅ compatible (phase structure aligns)
  Follow-up TODOs: none
-->

# ROSA Trusted Actions Constitution

## Core Principles

### I. OpenAPI-First

The OpenAPI 3.0 specification under `openapi/` is the single source of truth for the API surface. Every API change MUST begin as a spec change. Go server code is generated from the bundled spec via `oapi-codegen`. The generated file `internal/openapi/api.go` is committed to the repository but MUST NOT be hand-edited. The spec MUST pass `make validate-spec` (Redocly) before code generation proceeds.

### II. Contract-First Interface

The generated `ServerInterface` is the compile-time contract between the API surface and handler implementations. Handlers in `internal/handlers/` implement this interface. Adding, removing, or changing an endpoint MUST follow this sequence: modify the OpenAPI spec, regenerate (`make generate`), then implement or update the handler. No endpoint logic may exist outside a `ServerInterface` implementation.

### III. Security by Default

AWS SigV4 is the global authentication scheme. All endpoints are authenticated unless they explicitly opt out via `security: []` in the OpenAPI spec. The `EnableAuth` configuration flag defaults to `true`. Opting an endpoint out of authentication MUST be a deliberate, documented decision in the spec. Access control decisions (allowed accounts, caller identity) are derived from the SigV4 signature, never from application-layer tokens or headers.

### IV. Enum-Driven Domain Modeling

Domain states — `ExecutionStatus`, `ApprovalState`, `ActionType`, `Scope`, `OutputStatus` — MUST be defined as OpenAPI enum types. This generates type-safe Go constants and prevents stringly-typed state comparisons. New domain states MUST be added to the spec as enums, not as bare strings in handler code.

### V. Separation of Concerns

Generated code (`internal/openapi/`) and hand-written code (`internal/handlers/`, `internal/middleware/`, `internal/config/`) occupy strictly separate packages. Each concern — routing, business logic, middleware, configuration — gets its own package. Generated code MUST NOT be imported into handler logic except through the `ServerInterface` and generated types. Cross-cutting concerns (logging, request IDs, panic recovery) live in `internal/middleware/`.

### VI. Testing Discipline

Unit tests live alongside the code they test (e.g., `internal/handlers/api_test.go`). Integration tests exercise the running server via `make test-integration`. Race detection is available via `make test-race`. Tests MUST verify HTTP status codes and response shapes against the OpenAPI contract. New handlers MUST ship with corresponding test coverage.

### VII. Build Automation

The Makefile is the canonical entry point for all development workflows. `make generate` bundles the spec and produces Go code. `make build` compiles the binary. `make test` runs the test suite. `make lint` and `make fmt` enforce code quality. No build step should require manual invocation of tools outside the Makefile.

### VIII. Go Idioms

The project follows standard Go project layout conventions: `cmd/` for entry points, `internal/` for private packages. The HTTP layer uses Chi. Structured logging uses logrus. CLI flags use Cobra. The server supports graceful shutdown with configurable timeout. New code MUST follow these conventions rather than introducing alternative frameworks.

## Architecture Constraints

- **Runtime**: Go 1.21+, Chi router, deployed behind an API gateway at `/api/v0/trusted-actions`
- **Authentication**: AWS SigV4 exclusively; no session tokens, no JWTs, no API keys
- **Storage**: S3 for execution artifacts (output, logs); artifacts have independent upload status tracking
- **Domain model**: Execution lifecycle (`pending → running → succeeded | failed | timed_out`), approval workflow (`not_required → pending → approved | rejected`), write cooldowns, max-concurrency limits, dry-run support
- **Audit**: Every API call is logged with caller identity (account ID, ARN, operator), action, target cluster, and Jira reference
- **Jira integration**: Execution requests MUST include a Jira ticket reference in `PROJECT-NUMBER` format

## Development Workflow

1. **Spec change**: Modify YAML files under `openapi/` (one file per path, one file per schema)
2. **Validate**: Run `make validate-spec` to catch structural issues before generation
3. **Generate**: Run `make generate` to produce the bundled spec and regenerate Go code
4. **Implement**: Write or update handlers in `internal/handlers/` to satisfy the updated `ServerInterface`
5. **Test**: Run `make test` for unit tests, `make test-integration` for end-to-end validation
6. **Lint**: Run `make lint` and `make fmt` before committing
7. **Review**: API surface changes require review of both the spec diff and the generated code diff

## Governance

This constitution governs all development decisions for the ROSA Trusted Actions project. Amendments require:

1. A documented rationale for the change
2. Review and approval from project maintainers
3. A migration plan if the amendment affects existing code or workflows
4. Version increment following semantic versioning (MAJOR for principle removals/redefinitions, MINOR for additions, PATCH for clarifications)

All pull requests and reviews MUST verify compliance with these principles. Complexity beyond what these principles prescribe MUST be justified in the PR description.

**Version**: 1.0.0 | **Ratified**: 2026-07-14 | **Last Amended**: 2026-07-14