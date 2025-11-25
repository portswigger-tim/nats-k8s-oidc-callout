# NATS Kubernetes OIDC Auth Callout

A NATS auth callout service that authenticates and authorizes NATS clients using Kubernetes service account JWTs.

## Overview

This service integrates NATS with Kubernetes authentication, allowing workloads to connect to NATS using their projected service account tokens. It validates JWTs against the Kubernetes JWKS endpoint and maps Kubernetes identities to NATS subject permissions.

## Features

- **JWT Validation**: Validates Kubernetes service account tokens against JWKS endpoint
- **Namespace Isolation**: Default subject permissions scoped to pod namespace (`<namespace>.>`)
- **Cross-Namespace Access**: Opt-in via ServiceAccount annotations for broader permissions
- **Separate Pub/Sub Controls**: Fine-grained control over publish and subscribe permissions
- **Real-time Updates**: Kubernetes watch-based caching keeps permissions current
- **Cloud-Native**: 12-factor app design with environment-based configuration
- **Observability**: Health checks, Prometheus metrics, and structured logging

## Quick Start

### Prerequisites

- Kubernetes cluster with OIDC token projection configured
- NATS server with auth callout enabled
- Service account with permissions to watch ServiceAccounts cluster-wide

### Configuration

Configure via environment variables:

```bash
# NATS Connection
NATS_URL=nats://nats:4222
NATS_CREDS_FILE=/etc/nats/auth.creds
NATS_ACCOUNT=MyAccount

# Kubernetes JWT Validation
JWKS_URL=https://kubernetes.default.svc/openid/v1/jwks
JWT_ISSUER=https://kubernetes.default.svc
JWT_AUDIENCE=nats

# ServiceAccount Annotations
SA_ANNOTATION_PREFIX=nats.io/
CACHE_CLEANUP_INTERVAL=15m
```

### Granting Cross-Namespace Access

Annotate ServiceAccounts to grant additional subject permissions:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service
  namespace: foo
  annotations:
    nats.io/allowed-pub-subjects: "bar.>, platform.commands.*"
    nats.io/allowed-sub-subjects: "platform.events.*, shared.status"
```

This grants:
- **Publish**: `foo.>`, `bar.>`, `platform.commands.*`
- **Subscribe**: `foo.>`, `platform.events.*`, `shared.status`

## Development Status

### ✅ Implemented
- CLI application with graceful shutdown
- Environment-based configuration
- HTTP server with health checks and Prometheus metrics
- **JWT Validation** - Full token validation with:
  - JWKS-based signature verification (RS256)
  - Standard claims validation (iss, aud, exp, nbf, iat)
  - Kubernetes-specific claims extraction
  - Comprehensive test coverage using TDD
- **Kubernetes ServiceAccount Cache** - Real-time watch with:
  - Cluster-wide informer pattern
  - Annotation-based permission parsing
  - Default namespace isolation
  - 81.2% test coverage
- **Authorization Handler** - Request processing with:
  - Clean interface design
  - JWT validation integration
  - Permission building from ServiceAccount annotations
  - 100% test coverage
- **NATS Client** - Auth callout integration with:
  - Real-time auth request handling
  - NKey-based response signing
  - Integration tests with testcontainers

### ✅ Fully Implemented and Tested
- **Complete application** - All components working end-to-end
- **End-to-end tests** - Full system integration tests with k3s + NATS + auth callout
  - Real Kubernetes token creation and validation
  - Complete auth callout flow testing
  - Permission enforcement validation
  - Multiple test scenarios (valid token, wrong audience)
  - All tests passing (~10s execution time)
  - Run with: `make test-e2e` (requires Docker)

### 📋 Planned
- Deployment manifests and Helm chart
- Production deployment examples

## Documentation

- **[Client Usage Guide](docs/CLIENT_USAGE.md)** - Complete guide for configuring and using NATS authentication from your applications
- **[Design Document](docs/plans/2025-11-24-nats-k8s-auth-design.md)** - Detailed architecture and design decisions

## Architecture

### Key Components

- **JWT Validator**: ✅ Validates K8s tokens and claims
- **HTTP Server**: ✅ Health and metrics endpoints (port 8080)
- **NATS Client**: ✅ Subscribes to auth callout subjects and handles requests
- **ServiceAccount Cache**: ✅ Real-time watch of K8s ServiceAccounts with informer pattern
- **Authorization Handler**: ✅ Maps K8s identity to NATS permissions

## Observability

### Health Check

```bash
curl http://localhost:8080/health
```

### Metrics

Prometheus metrics available at `http://localhost:8080/metrics`:

- `nats_auth_requests_total` - Authorization request counts
- `jwt_validation_duration_seconds` - JWT validation latency
- `sa_cache_size` - Current cache size
- `k8s_api_calls_total` - Kubernetes API call counts

## Development

### Building

```bash
go build -o nats-k8s-auth ./cmd/server
```

### Testing

```bash
go test ./...
```

### Running Locally

```bash
# Set required environment variables
export NATS_URL=nats://localhost:4222
export JWKS_URL=https://kubernetes.default.svc/openid/v1/jwks
# ... other vars

./nats-k8s-auth
```

## License

See [LICENSE](LICENSE) file.

## Testing

### Running Tests

**Unit Tests** (fast, no external dependencies):
```bash
make test
# or
go test ./...
```

**Integration Tests** (requires Docker):
```bash
make test-integration
# or
go test -tags=integration -v ./internal/nats/
```

**All Tests**:
```bash
make test-all
```

**Coverage Report**:
```bash
make coverage
```

### Test Coverage

- `internal/auth`: **100.0%** - Authorization handler
- `internal/k8s`: **81.2%** - Kubernetes ServiceAccount cache
- `internal/jwt`: **72.3%** - JWT validation with JWKS
- `internal/nats`: **29.7%** - NATS auth callout client

### Test Organization

- **Unit tests**: Fast, no external dependencies, run by default
- **Integration tests**: Use testcontainers-go NATS module for real NATS server with auth callout
- **Build tags**: Integration tests use `-tags=integration` to avoid requiring Docker for unit tests

### Integration Test Features

The NATS integration tests (`internal/nats/integration_test.go`) validate:
- Real NATS server with auth callout enabled
- Auth service connection and subscription
- Authorization request processing
- JWT extraction and validation flow
- Simplified setup using `github.com/testcontainers/testcontainers-go/modules/nats`

