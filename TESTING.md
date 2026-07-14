# Testing Guide

This document describes the testing strategy, conventions, and tools for the ROSA Trusted Actions Server.

## Running Tests

```bash
make test               # Run all unit tests
make test-race          # Run with Go race detector enabled
make test-coverage      # Generate HTML coverage report
make test-integration   # Start server and run API tests
make test-api           # Run API tests against a running server
```

## Test Organization

Tests live alongside the code they test, following Go conventions:

```
internal/
  handlers/
    api.go              # Implementation
    api_test.go          # Unit tests for handlers
  middleware/
    logging.go
    middleware.go
  config/
    config.go
```

## Writing Tests

### Unit Tests

Use the standard `testing` package with `httptest` for HTTP handler tests:

```go
func TestAPIHandler_YourEndpoint(t *testing.T) {
    logger := logrus.New()
    handler := NewAPIHandler(logger)

    req := httptest.NewRequest("GET", "/api/v0/trusted-actions/your-endpoint", nil)
    w := httptest.NewRecorder()

    handler.YourEndpoint(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected status 200, got %d", w.Code)
    }
}
```

### Test Conventions

- **No test frameworks**: Use stdlib `testing` and `httptest`. No testify, gomega, etc.
- **Table-driven tests**: Use subtests (`t.Run`) for testing multiple cases.
- **Test naming**: `Test<Type>_<Method>` or `Test<Type>_<Method>_<Scenario>` (e.g., `TestAPIHandler_CreateExecution_InvalidJSON`).
- **Test isolation**: Each test creates its own handler instance and logger. No shared mutable state.
- **Assertions**: Use `t.Errorf` for non-fatal checks, `t.Fatalf` for preconditions that prevent further testing.

### Table-Driven Test Example

```go
func TestAPIHandler_Describe(t *testing.T) {
    logger := logrus.New()
    handler := NewAPIHandler(logger)

    tests := []struct {
        name       string
        action     string
        wantStatus int
    }{
        {"valid action", "cluster-info", http.StatusOK},
        {"unknown action", "nonexistent", http.StatusNotFound},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", "/api/v0/trusted-actions/"+tt.action, nil)
            w := httptest.NewRecorder()

            handler.Describe(w, req, tt.action)

            if w.Code != tt.wantStatus {
                t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
            }
        })
    }
}
```

## Coverage

### Requirements

- **Patch coverage**: >= 80% on all PRs (enforced by Codecov).
- **Focus on handler logic**: `internal/handlers/` and `internal/config/` are the primary targets for coverage.

### Generating Reports

```bash
# Generate coverage profile and HTML report
make test-coverage

# View the report
open coverage.html      # macOS
xdg-open coverage.html  # Linux
```

### Excluded from Coverage

The following are excluded from coverage metrics (see `codecov.yml`):

- `vendor/` — Third-party dependencies
- `node_modules/` — Node.js dependencies
- `internal/openapi/` — Generated code
- `**/*_test.go` — Test files themselves
- `tools.go` — Tool dependency declarations
- `docs/` — Documentation assets

## Integration Tests

Integration tests start the server and exercise the API end-to-end:

```bash
# Runs server, executes scripts/test-api.sh, then stops server
make test-integration
```

The integration test script (`scripts/test-api.sh`) sends HTTP requests to the running server and validates responses.

## CI Integration

Tests run automatically on every PR via CI pipelines:

1. **Lint** — `golangci-lint` with project configuration
2. **Test** — `go test -race -coverprofile=coverage.out -covermode=atomic ./...`
3. **Coverage upload** — Results sent to Codecov for patch coverage enforcement
4. **Build** — Ensures the binary compiles after lint and test pass
