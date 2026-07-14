# Feature Specification: Catalog Endpoint

**Feature Branch**: `001-catalog-endpoint`

**Created**: 2026-07-14

**Status**: Draft

**Input**: User description: "Implement the Catalog endpoint (GET /) that lists all available Trusted Actions from a real backing store, replacing the current hardcoded mock data."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Discover Available Actions (Priority: P1)

An operator opens the Trusted Actions API to see what actions are available before executing one. They call `GET /` and receive a complete, alphabetically sorted list of all registered Trusted Actions with their name, scope, type, and description.

**Why this priority**: This is the primary purpose of the Catalog endpoint. Without it, operators cannot discover what actions exist and must rely on out-of-band documentation.

**Independent Test**: Can be fully tested by calling `GET /` against a server with a known set of action definitions loaded and verifying the response matches the expected catalog.

**Acceptance Scenarios**:

1. **Given** the backing store contains 5 registered Trusted Actions, **When** an operator calls `GET /`, **Then** the response is `200 OK` with a JSON body containing `items` (array of 5 action summaries) and `total: 5`.
2. **Given** the backing store contains registered Trusted Actions, **When** an operator calls `GET /`, **Then** the `items` array is sorted alphabetically by the `name` field.
3. **Given** each Trusted Action has a name, scope, type, and description, **When** the catalog is returned, **Then** every item in `items` includes all four fields with non-empty values.

---

### User Story 2 - Empty Catalog (Priority: P2)

An operator calls the Catalog endpoint on a fresh deployment where no Trusted Actions have been registered yet. The system returns a valid but empty catalog rather than an error.

**Why this priority**: Operators and automation tooling need predictable responses even when no actions are configured. An error response for an empty catalog would break clients that poll for available actions.

**Independent Test**: Can be tested by calling `GET /` against a server with an empty backing store and verifying a `200 OK` with an empty `items` array and `total: 0`.

**Acceptance Scenarios**:

1. **Given** the backing store contains no Trusted Actions, **When** an operator calls `GET /`, **Then** the response is `200 OK` with `{"items": [], "total": 0}`.

---

### User Story 3 - Catalog Under Backing Store Failure (Priority: P3)

An operator calls the Catalog endpoint but the backing store is unreachable or returns an error. The system returns an appropriate error response rather than a partial or corrupted catalog.

**Why this priority**: Operators need to distinguish between "no actions available" and "the system is broken." Silent failures would erode trust in the API.

**Independent Test**: Can be tested by calling `GET /` with the backing store connection intentionally broken and verifying a `500` error response with a meaningful error code.

**Acceptance Scenarios**:

1. **Given** the backing store is unreachable, **When** an operator calls `GET /`, **Then** the response is `500 Internal Server Error` with an `Error` body containing `code: "store-error"` and a human-readable `reason`.

---

### Edge Cases

- What happens when the backing store contains a Trusted Action with missing or null fields (e.g., no description)? The system MUST still return the action but with an empty string for the missing field, never omitting a required field from the response.
- What happens when the backing store is slow to respond? The request MUST respect the server's configured timeout and return a `500` error if the store does not respond in time.
- Audit logging for catalog requests is conditional on the authentication decision: if authenticated, catalog calls MUST produce audit entries; if unauthenticated, standard middleware request logging is sufficient.

## Clarifications

### Session 2026-07-14

- Q: How many Trusted Actions will the catalog contain? → A: Small catalog (under 100 actions), no pagination needed. Full list returned in a single response.
- Q: Should the catalog endpoint require authentication? → A: **OPEN — requires further clarification.** The current OpenAPI spec declares `security: []` (unauthenticated), but this decision needs explicit confirmation from the team. The answer affects FR-005 and may require an OpenAPI spec change.
- Q: Should catalog requests be recorded in the audit log? → A: Depends on the auth decision. If the endpoint is authenticated, catalog requests get audit entries (caller identity is available). If public, standard request logging in middleware is sufficient (no caller identity to audit).
- Q: Are Trusted Action definitions static configuration or dynamic database records? → A: **OPEN — requires further clarification.** The answer determines the backing store technology (config files vs. database) and whether an admin API for managing definitions is needed. Deferred to team decision.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The `GET /` endpoint MUST return a `TrustedActionCatalog` response containing all registered Trusted Actions from the backing store.
- **FR-002**: Each item in the catalog MUST include exactly four fields: `name`, `scope`, `type`, and `description` (matching the `TrustedActionSummary` schema). Parameters and authorization details MUST NOT be included — those belong to the Describe endpoint.
- **FR-003**: The `items` array MUST be sorted alphabetically by the `name` field.
- **FR-004**: The `total` field MUST equal the length of the `items` array.
- **FR-005**: [NEEDS CLARIFICATION: Authentication requirement is undecided. The current OpenAPI spec declares `security: []` (unauthenticated), but this needs team confirmation. If authentication is required, the OpenAPI spec must be updated to remove the `security: []` override, and the handler must validate AWS SigV4 credentials.]
- **FR-006**: When the backing store is empty, the endpoint MUST return `200 OK` with an empty `items` array and `total: 0`.
- **FR-007**: When the backing store is unreachable or returns an error, the endpoint MUST return `500 Internal Server Error` with an `Error` response body.
- **FR-008**: Trusted Action definitions MUST be loaded from a backing store, not hardcoded in handler code.

### Key Entities

- **TrustedActionSummary**: A lightweight representation of a Trusted Action containing `name` (string), `scope` (enum: `kube-api` | `aws-api`), `type` (enum: `read` | `write`), and `description` (string). This is the catalog's unit of content.
- **TrustedActionCatalog**: The response envelope containing `items` (array of TrustedActionSummary, sorted by name) and `total` (integer count).
- **Backing Store**: The persistence layer where Trusted Action definitions are registered. The catalog reads from it but never writes to it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Operators can retrieve the full catalog of available actions in a single request without prior knowledge of action names.
- **SC-002**: The catalog response is consistent — calling `GET /` twice with no changes to the backing store produces identical responses.
- **SC-003**: An empty backing store produces a valid, parseable response (not an error), enabling automation clients to handle the "no actions yet" state gracefully.
- **SC-004**: Backing store failures produce a distinguishable error response (not an empty catalog), enabling operators and monitoring to detect infrastructure problems.

## Assumptions

- The backing store technology will be determined during the planning phase. This spec is intentionally agnostic to whether actions are stored in a database, S3, configuration files, or another medium. Whether definitions are static configuration or dynamic records is also an open question that affects the backing store choice.
- The set of registered Trusted Actions changes infrequently (administrative operation), so caching strategies may be applied but are not required for this initial implementation.
- The catalog endpoint's authentication requirement is currently an open question. The existing OpenAPI spec declares it unauthenticated (`security: []`), but this decision needs explicit team confirmation before implementation.
- The `TrustedActionSummary` and `TrustedActionCatalog` schemas in the OpenAPI spec are already correct and do not need modification for this feature.