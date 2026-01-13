# Variables
BINARY_NAME := nats-k8s-oidc-callout
OUT_DIR := out
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -w -s -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: test test-unit test-integration test-e2e test-helm test-all coverage lint clean
.PHONY: build build-all build-amd64 build-arm64 docker-build docker-push version help

# Default target
all: build-all

# Run unit tests (default test target)
test: test-unit

# Run unit tests only (no integration tests)
test-unit:
	@echo "Running unit tests..."
	go test ./...

# Run integration tests (requires Docker)
test-integration:
	@echo "Running integration tests..."
	@echo "Note: Requires Docker to be running"
	go test -tags=integration -v ./internal/nats/

# Run E2E tests (requires Docker)
test-e2e:
	@echo "Running E2E tests..."
	@echo "Note: Requires Docker to be running"
	@docker info > /dev/null 2>&1 || (echo "Error: Docker is not running" && exit 1)
	go test -tags=e2e -v -timeout=10m ./e2e_suite_test.go

# Run Helm unit tests (requires helm-unittest plugin)
test-helm:
	@echo "Running Helm unit tests..."
	@command -v helm >/dev/null 2>&1 || { echo "Error: helm is not installed"; exit 1; }
	@helm plugin list | grep -q unittest || { echo "Error: helm-unittest plugin not installed. Run: helm plugin install https://github.com/helm-unittest/helm-unittest"; exit 1; }
	helm unittest --strict helm/nats-k8s-oidc-callout

# Run all tests (unit + integration + e2e + helm)
test-all: test-unit test-integration test-e2e test-helm

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	go test -cover ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running golangci-lint..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Error: golangci-lint is not installed. Install: https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run --timeout=5m

# Run tests with verbose output
test-verbose:
	go test -v ./...

# Build targets
# ============================================================

# Build for current architecture
build:
	@echo "Building for current architecture..."
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY_NAME) ./cmd/server

# Build for all architectures
build-all: build-amd64 build-arm64
	@echo "All binaries built successfully in $(OUT_DIR)/"

# Build for amd64
build-amd64:
	@echo "Building for linux/amd64..."
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY_NAME)-linux-amd64 \
		./cmd/server

# Build for arm64
build-arm64:
	@echo "Building for linux/arm64..."
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
		-ldflags="$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY_NAME)-linux-arm64 \
		./cmd/server

# Docker targets
# ============================================================

# Build Docker image for local testing (amd64 only for --load compatibility)
docker-build: build-all
	@echo "Building Docker image for linux/amd64..."
	docker buildx build \
		--platform linux/amd64 \
		-t $(BINARY_NAME):$(VERSION) \
		-t $(BINARY_NAME):latest \
		--load \
		.

# Build and push multi-arch Docker image
docker-push: build-all
	@echo "Building and pushing multi-arch Docker image..."
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(BINARY_NAME):$(VERSION) \
		-t $(BINARY_NAME):latest \
		--push \
		.

# Utility targets
# ============================================================

# Display version information
version:
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

# Generate Helm chart documentation
helm-docs:
	@echo "Generating Helm chart documentation..."
	docker run --rm -v "$$(pwd):/helm-docs" -u $$(id -u) jnorwood/helm-docs:v1.14.2 \
		--chart-search-root=helm \
		--template-files=README.md.gotmpl

# Help message
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build targets:"
	@echo "  all           - Build binaries for all architectures (default)"
	@echo "  build         - Build binary for current architecture"
	@echo "  build-all     - Build binaries for amd64 and arm64"
	@echo "  build-amd64   - Build binary for linux/amd64"
	@echo "  build-arm64   - Build binary for linux/arm64"
	@echo ""
	@echo "Docker targets:"
	@echo "  docker-build  - Build multi-arch Docker image for local use"
	@echo "  docker-push   - Build and push multi-arch Docker image"
	@echo ""
	@echo "Test targets:"
	@echo "  test          - Run unit tests (default test)"
	@echo "  test-unit     - Run unit tests only"
	@echo "  test-integration - Run integration tests (requires Docker)"
	@echo "  test-e2e      - Run E2E tests (requires Docker)"
	@echo "  test-helm     - Run Helm unit tests (requires helm-unittest plugin)"
	@echo "  test-all      - Run all tests (unit + integration + e2e + helm)"
	@echo "  coverage      - Run tests with coverage"
	@echo "  lint          - Run golangci-lint (requires golangci-lint)"
	@echo ""
	@echo "Helm targets:"
	@echo "  helm-docs     - Generate Helm chart documentation"
	@echo "  test-helm     - Run Helm unit tests"
	@echo ""
	@echo "Utility targets:"
	@echo "  version       - Display version information"
	@echo "  clean         - Clean test cache and build artifacts"
	@echo "  help          - Display this help message"

# Clean test cache and build artifacts
clean:
	@echo "Cleaning test cache and build artifacts..."
	go clean -testcache
	rm -rf $(OUT_DIR)

# Run tests in short mode (skip slow tests)
test-short:
	go test -short ./...

# Check if Docker is running (for integration tests)
check-docker:
	@docker info > /dev/null 2>&1 || (echo "Error: Docker is not running" && exit 1)
	@echo "Docker is running"
