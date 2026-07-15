# Architecture Documentation

## Overview

The ROSA Trusted Actions Server is a Go-based HTTP API server that provides a controlled interface for executing trusted actions against Red Hat managed clusters and AWS resources. The system enables operators to submit actions through the API, where each execution is tracked, dispatched, and produces artifacts (output and logs) that can be retrieved through the API.

## Project Structure

```
rosa-trusted-actions-server/
├── cmd/server/              # Main application entry point
├── internal/
│   ├── handlers/           # API request handlers (implements generated interfaces)
│   ├── middleware/         # HTTP middleware (logging, recovery, request ID)
│   ├── config/             # Configuration management
│   └── openapi/            # Generated code from OpenAPI specs (do not edit)
├── openapi/                # OpenAPI 3.0 specification source files
├── api-spec.yaml          # Bundled OpenAPI spec (generated)
└── Makefile               # Build automation
```

## API Design

### OpenAPI-First Development

The API is defined using OpenAPI 3.0 specifications in the `openapi/` directory. The build process:

1. Bundles multi-file OpenAPI specs into `api-spec.yaml`
2. Generates Go server interfaces and types using `oapi-codegen`
3. Custom handlers implement the generated `ServerInterface`

### Generated vs Custom Code

- **Generated**: `internal/openapi/api.go` - interfaces, types, routing (do not edit)
- **Custom**: `internal/handlers/api.go` - business logic implementing generated interfaces

## HTTP Server Architecture

### Server Configuration

The server is configured via:
- **CLI flags**: Server settings (listen address, log level, log format)
- **Environment variables**: Application configuration (AWS credentials, feature flags)

### Middleware Stack

Applied in order:
1. **Custom Logger**: Request/response logging with structured fields
2. **Request ID**: Generates unique ID for request tracing
3. **Recovery**: Panic recovery with stack trace logging  
4. **Real IP**: Processes X-Forwarded-For headers
5. **Timeout**: 60-second request timeout
6. **CORS**: Permissive CORS policy for development

### Request Routing

- Base path: `/api/v0/trusted-actions`
- Health check: `/health`
- API routes generated from OpenAPI specs and mounted via `openapi.HandlerFromMux`

## Configuration Management

### Structure

The `config.Config` struct separates:
- **Server config**: Overridden by CLI flags (listen address, logging)
- **AWS config**: Standard AWS environment variables
- **Storage config**: S3 bucket configuration
- **Security config**: Authentication and account restrictions

### Environment Variables

- `AWS_*`: Standard AWS SDK environment variables for authentication
- `ROSA_TA_S3_BUCKET`: S3 bucket for storing execution outputs and logs
- `ROSA_TA_ENABLE_AUTH`: Enable/disable authentication enforcement
- `ROSA_TA_ALLOWED_ACCOUNTS`: Comma-separated list of allowed AWS account IDs
- `DATABASE_URL`: Database connection string

## Security

- AWS Signature V4 authentication for request verification
- Account allowlisting through configuration
- Audit logging of all API calls with caller identity

## Build System

### Make Targets

- `make generate`: Bundle OpenAPI spec → generate Go code
- `make build`: Generate code → compile binary
- `make test`: Run Go tests
- `make validate-spec`: Validate OpenAPI specification
- `make dev`: Live reload development server

### Code Generation Pipeline

```
openapi/*.yaml → bundle-spec → api-spec.yaml → generate-api → internal/openapi/api.go
```

## Error Handling

### HTTP Responses

Errors follow the OpenAPI-defined `Error` schema:
- `kind`: Always "Error"
- `code`: HTTP status-based error codes
- `reason`: Human-readable error message

### Panic Recovery

The recovery middleware catches panics and:
- Logs panic with stack trace
- Returns 500 Internal Server Error
- Includes request ID for correlation

## Logging

### Structured Logging

Uses Logrus with configurable output:
- **JSON format**: For production/structured log aggregation
- **Text format**: For development/human readability
- **Log levels**: debug, info, warn, error

### Request Context

Logs include:
- Request ID for tracing
- HTTP method and URI
- Error details and stack traces (for panics)

## Data Flow

### Typical Workflow

1. **Discover actions**: `GET /` (catalog) or `GET /{action}` (describe)
2. **Execute action**: `POST /{action}/run` (returns `202 Accepted` with execution ID)
3. **Poll for completion**: `GET /runs/{id}` (metadata only by default)
4. **Retrieve results**: `GET /runs/{id}?include=output,logs` (opt-in content)
5. **Audit and reporting**: `GET /runs` (filter executions) or `GET /audit` (API call log)

### Storage Architecture

- **S3**: Execution outputs and logs stored as objects
- **Database**: Execution metadata, audit entries, and trusted action definitions
- **Key Structure**: S3 objects partitioned by account, cluster, and execution ID
