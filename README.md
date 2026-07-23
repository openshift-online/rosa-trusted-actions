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
