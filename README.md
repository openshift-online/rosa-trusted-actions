# ROSA Trusted Actions Server

A Chi-based HTTP server implementing the ROSA Trusted Actions API, automatically generated from OpenAPI specifications using oapi-codegen.

## Prerequisites

- Go 1.21+
- Node.js 16+ (for OpenAPI bundling)
- Make
- Git

## Quick Start

```bash
# Build and run
make build
make run

# The server starts on :8080 by default
```

## Local Development (mock auth)

The server normally requires live OCM credentials and a reachable JWKS endpoint.
Set `ROSA_TA_ENABLE_AUTH=false` to bypass both — the server injects a hardcoded
`dev-user` identity with the SREP role so every endpoint is reachable without a
token.

> **Warning:** never set `ROSA_TA_ENABLE_AUTH=false` outside a local or CI environment.

### Get a kubeconfig (OpenShift managed clusters)

For OSD / ROSA clusters, retrieve the admin kubeconfig via OCM using the
cluster's internal ID:

```bash
INTERNAL_ID="<your-cluster-internal-id>"
ocm get /api/clusters_mgmt/v1/clusters/${INTERNAL_ID}/credentials \
  | jq -r .kubeconfig \
  > /tmp/${INTERNAL_ID}.kubeconfig
```

### Start the server

```bash
export ROSA_TA_ENABLE_AUTH=false
export ROSA_TA_KUBECONFIG=/tmp/${INTERNAL_ID}.kubeconfig

go run ./cmd/server/ --log-level debug
```

You should see:

```text
WARN  Auth disabled — using mock identity 'dev-user' with SREP role. Do not use in production.
INFO  Using kubeconfig provider for cluster access (local mode)
INFO  Starting server  addr=:8080
```

### Verify the server is up

```bash
curl -s http://localhost:8080/health | jq .
```

```json
{"status":"healthy","version":"dev","build_date":"unknown","git_commit":"unknown"}
```

### List the action catalog

No `Authorization` header required — mock auth injects the identity automatically.

```bash
curl -s http://localhost:8080/api/v0/trusted-actions/ | jq .
```

### Execute a GET action against the cluster

The `get` action lists or fetches Kubernetes resources via `params`. `resource`, `version`, and
`namespace` are all required — nothing is defaulted server-side. Cluster-scoped resources (e.g.
`nodes`, `namespaces`) aren't supported yet: every request is currently treated as namespaced,
regardless of the resource type, so `namespace` must always be set.

```bash
# List pods in the default namespace
curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -d '{
    "target_cluster": "local",
    "params": {"resource": "pods", "version": "v1", "namespace": "default"}
  }' | jq .

# List pods in a different namespace
curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -d '{
    "target_cluster": "local",
    "params": {"resource": "pods", "version": "v1", "namespace": "kube-system"}
  }' | jq .

# Get a specific pod
curl -s -X POST http://localhost:8080/api/v0/trusted-actions/get/run \
  -H 'Content-Type: application/json' \
  -d '{
    "target_cluster": "local",
    "params": {
      "resource":  "pods",
      "namespace": "kube-system",
      "name":      "coredns-<id>",
      "version":   "v1"
    }
  }' | jq .
```

`POST .../run` itself only returns `202` with `status: pending` — execution happens
asynchronously on a background worker. Poll `GET /api/v0/trusted-actions/runs/{id}` for the
result:

```json
{
  "id": "...",
  "action": "get",
  "status": "succeeded",
  "target_cluster": "local",
  "completed_at": "..."
}
```

(`output`/`logs` retrieval via `?include=output,logs` is not implemented yet — it's a placeholder
pending a persistent audit/output backend.)

### Supported params

| Param       | Required | Description                                                           |
|-------------|----------|------------------------------------------------------------------------|
| `resource`  | yes      | Kubernetes resource type, plural (e.g. `pods`, `configmaps`)          |
| `version`   | yes      | API version (e.g. `v1`)                                               |
| `namespace` | yes      | Namespace to query — every request is currently treated as namespaced |
| `name`      | no       | Specific resource name — omit to list all                             |
| `group`     | no       | API group (omit for core resources)                                   |

## Development

### Code Generation

The OpenAPI specification lives in the `openapi/` directory. The build process bundles the multi-file spec and generates Go server code:

```bash
# Regenerate API code from the local spec
make generate

# Validate the OpenAPI spec
make validate-spec

# Preview API docs in the browser
npm run preview-docs
```

### Building and Testing

```bash
# Build
make build

# Run tests
make test

# Integration tests
make test-integration

# Code quality
make lint
make fmt
```

## Configuration

### CLI Flags (Server Settings)

```bash
./bin/rosa-trusted-actions-server --listen-addr=":3000" --log-level="debug" --log-json
```

### Environment Variables (Application Config)

```bash
# AWS Configuration
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="your-key"
export AWS_SECRET_ACCESS_KEY="your-secret"

# Application Configuration
export ROSA_TA_S3_BUCKET="trusted-actions-bucket"
export ROSA_TA_ALLOWED_ACCOUNTS="123456789012,987654321098"

# Authorization
export ROSA_TA_ROLES_CONFIG="configs/role_mapping.yaml"
export ROSA_TA_JWK_CERT_FILE=""  # optional
export ROSA_TA_JWK_CERT_URL="https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs"
export ROSA_TA_OCM_BASE_URL="https://api.openshift.com"
export ROSA_TA_OCM_CLIENT_ID="some-client-id"
export ROSA_TA_OCM_CLIENT_SECRET="xxxxxxxxxxxxxxx"
export ROSA_TA_OCM_TOKEN=""
```

Copy `.env.example` to `.env` and customize.

## API Endpoints

Base path: `/api/v0/trusted-actions`

- `GET /` - List available Trusted Actions
- `GET /{action}` - Describe a specific action
- `POST /{action}/run` - Execute a Trusted Action
- `GET /runs` - List executions
- `GET /runs/{id}` - Get execution details
- `GET /audit` - API call audit log
- `GET /health` - Health check

## Project Structure

```
rosa-trusted-actions-server/
├── cmd/server/              # Main application
├── docs/                   # API documentation (ReDoc)
├── internal/
│   ├── handlers/           # API handlers
│   ├── middleware/         # HTTP middleware
│   ├── config/             # Configuration
│   └── openapi/            # Generated code (committed, don't edit)
├── openapi/                # OpenAPI 3.0 specification (source of truth)
├── scripts/                # Helper scripts
└── Makefile               # Build automation
```

Run `make help` for all available targets.
