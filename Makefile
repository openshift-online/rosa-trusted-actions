# Variables
BINARY_NAME=rosa-trusted-actions-server
CLI_NAME=action-cli
MAIN_PATH=./cmd/server
CLI_PATH=./cmd/action-cli
BUILD_DIR=./bin
GENERATED_DIR=./internal/openapi
API_SPEC_PATH=./openapi/openapi.yaml
BUNDLED_SPEC_PATH=./api-spec.yaml

# Tools
OAPI_CODEGEN=go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
GO_LINT=golangci-lint

# Default target
.PHONY: help
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

.PHONY: all
all: clean deps generate build ## Clean, install dependencies, generate code, and build

# Dependencies
.PHONY: deps
deps: deps-go deps-npm ## Install all dependencies

.PHONY: deps-go
deps-go: ## Install Go dependencies
	go mod download
	go mod tidy

.PHONY: deps-npm
deps-npm: ## Install npm dependencies for OpenAPI tooling
	npm install

# Code generation
.PHONY: generate
generate: bundle-spec generate-api ## Bundle OpenAPI spec and generate Go code

.PHONY: bundle-spec
bundle-spec: deps-npm ## Bundle multi-file OpenAPI spec into single file
	@echo "Bundling OpenAPI spec from $(API_SPEC_PATH)"
	npm run bundle-spec

.PHONY: generate-api
generate-api: $(BUNDLED_SPEC_PATH) ## Generate Go server code from OpenAPI spec
	@echo "Generating Go server code from $(BUNDLED_SPEC_PATH)"
	@mkdir -p $(GENERATED_DIR)
	$(OAPI_CODEGEN) -config oapi-codegen.yaml $(BUNDLED_SPEC_PATH)

.PHONY: validate-spec
validate-spec: deps-npm ## Validate OpenAPI specification
	npm run validate-spec

# Build
.PHONY: build
build: generate ## Build the server binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

.PHONY: build-cli
build-cli: ## Build the action-cli binary
	@echo "Building $(CLI_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(CLI_NAME) $(CLI_PATH)

.PHONY: build-linux
build-linux: generate ## Build Linux binary
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux $(MAIN_PATH)

# Run
.PHONY: run
run: generate ## Run the server locally
	go run $(MAIN_PATH)

.PHONY: dev
dev: ## Run in development mode with auto-reload (requires air)
	@which air > /dev/null || (echo "Installing air for live reload..." && go install github.com/cosmtrek/air@latest)
	air

# Testing
.PHONY: test
test: generate ## Run tests
	go test -v ./...

.PHONY: test-race
test-race: generate ## Run tests with race detection
	go test -race -v ./...

.PHONY: test-coverage
test-coverage: generate ## Run tests with coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

.PHONY: itest-up
itest-up: ## Create kind cluster + start ministack, waiting for both to be ready
	./integration/itest-up.sh

.PHONY: itest-run
itest-run: ## Start the server against the kind cluster and smoke test the 'get' action
	./integration/itest-run.sh

.PHONY: itest-down
itest-down: ## Tear down the kind cluster and ministack, removing generated artifacts
	./integration/itest-down.sh

.PHONY: test-api
test-api: ## Test API endpoints (requires server to be running)
	@echo "Testing API endpoints..."
	./scripts/test-api.sh

.PHONY: test-integration
test-integration: ## Run integration tests (starts server, runs tests, stops server)
	@echo "Starting server for integration tests..."
	$(MAKE) run &
	@sleep 3
	@echo "Running API tests..."
	./scripts/test-api.sh || (pkill -f rosa-trusted-actions-server && exit 1)
	@pkill -f rosa-trusted-actions-server || true
	@echo "Integration tests completed"

# Linting
.PHONY: lint
lint: ## Run linter
	@which $(GO_LINT) > /dev/null || (echo "Installing golangci-lint..." && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin)
	$(GO_LINT) run

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...
	goimports -w .

# Cleaning
.PHONY: clean
clean: ## Clean build artifacts and generated code
	rm -rf $(BUILD_DIR)
	rm -f $(BUNDLED_SPEC_PATH)
	rm -f coverage.out coverage.html

.PHONY: clean-all
clean-all: clean ## Clean everything including node_modules
	rm -rf node_modules

# Watch for changes and regenerate (requires entr)
.PHONY: watch-spec
watch-spec: ## Watch OpenAPI spec for changes and regenerate
	@which entr > /dev/null || (echo "entr not found. Install with: brew install entr (macOS) or apt-get install entr (Ubuntu)" && exit 1)
	@echo "Watching OpenAPI spec for changes..."
	find openapi -name "*.yaml" -o -name "*.yml" | entr -s 'make generate'

# Debug targets
.PHONY: debug-vars
debug-vars: ## Show Makefile variables
	@echo "BINARY_NAME: $(BINARY_NAME)"
	@echo "MAIN_PATH: $(MAIN_PATH)"
	@echo "BUILD_DIR: $(BUILD_DIR)"
	@echo "GENERATED_DIR: $(GENERATED_DIR)"
	@echo "API_SPEC_PATH: $(API_SPEC_PATH)"
	@echo "BUNDLED_SPEC_PATH: $(BUNDLED_SPEC_PATH)"
