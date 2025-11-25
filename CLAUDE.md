# NATS Kubernetes OIDC Auth Callout - Claude Context

## Project Overview

This is a Go-based NATS auth callout service that validates Kubernetes service account JWTs and provides subject-based authorization for NATS clients running in Kubernetes clusters.

## Key Design Decisions

### Architecture Pattern
- **NATS subject-based auth callout**: Service subscribes to NATS authorization request subjects
- **Kubernetes informer pattern**: Watch ServiceAccounts cluster-wide for annotation changes
- **Lazy-load caching**: Cache on first auth request, with K8s watch keeping cache up-to-date
- **12-factor app**: All configuration via environment variables

### Permission Model
- **Default**: Namespace isolation (services can only pub/sub to `<namespace>.>`)
- **Opt-in cross-namespace**: ServiceAccounts use annotations to grant additional access
- **Separate pub/sub controls**: `nats.io/allowed-pub-subjects` and `nats.io/allowed-sub-subjects`

### Security Principles
- **Generic errors to clients**: "authorization failed" for all validation failures
- **Detailed logging/metrics**: Capture specific failure reasons for debugging
- **Principle of least privilege**: Default to minimal access, explicit grants only
- **Full JWT validation**: Signature, standard claims, K8s-specific claims

## Implementation Status

### ✅ Completed
- **CLI scaffolding** (`cmd/server/main.go`) - Entry point with graceful shutdown ✅
- **Configuration** (`internal/config/`) - Environment variable loading with validation ✅
  - Smart defaults for standard Kubernetes deployments:
    - NATS_URL defaults to "nats://nats:4222" (standard K8s service name)
    - JWKS_URL and JWT_ISSUER auto-default to K8s endpoints when K8S_IN_CLUSTER=true
    - JWT_AUDIENCE defaults to "nats"
  - Only NATS_CREDS_FILE and NATS_ACCOUNT remain required
  - Comprehensive test coverage with 11 test cases covering all scenarios
  - 100% test coverage validating defaults, overrides, and validation logic
- **HTTP server** (`internal/http/`) - Health checks and Prometheus metrics on port 8080 ✅
- **JWT validation** (`internal/jwt/`) - Full JWKS-based validation with time mocking for tests ✅
  - JWKS loading from file and HTTP URL
  - RS256 signature verification
  - Standard claims validation (iss, aud, exp, nbf, iat)
  - Kubernetes claims extraction (namespace, service account)
  - Typed error handling
  - Comprehensive test coverage with TDD approach
  - Automatic key refresh with rate limiting
  - `Validate()` method added to implement auth.JWTValidator interface
  - Time mocking fixed for test token validity window (Nov 24-25, 2025)
  - All unit tests passing ✅
- **Kubernetes client** (`internal/k8s/`) - ServiceAccount cache with informer pattern ✅
  - Thread-safe in-memory cache
  - Cluster-wide ServiceAccount informer
  - Annotation parsing for NATS permissions
  - Default namespace isolation (namespace.>)
  - Opt-in cross-namespace permissions via annotations
  - Event handlers for ADD/UPDATE/DELETE
  - 81.2% test coverage with TDD approach
  - All unit tests passing ✅
- **Authorization handler** (`internal/auth/`) - Request processing and permission building ✅
  - Clean interface design with dependency injection
  - JWT validation integration
  - ServiceAccount permissions lookup
  - Generic error responses (security best practice)
  - 100% test coverage with TDD approach
  - All unit tests passing ✅
- **NATS client** (`internal/nats/`) - Connection and auth callout subscription handling ✅
  - Uses `synadia-io/callout.go` library for auth callout handling
  - Automatic NKey generation for response signing
  - JWT token extraction from NATS connection options (Token field)
  - Bridges NATS auth requests to internal auth handler
  - Converts auth responses to NATS user claims with permissions
  - 28.9% test coverage with comprehensive unit tests
  - Integration tests using testcontainers-go NATS module ✅
  - End-to-end auth callout flow validated with real NATS server ✅
  - All unit and integration tests passing ✅
- **Main application** (`cmd/server/main.go`) - Application wiring and startup ✅
  - Configuration loading and logger initialization
  - JWT validator setup with JWKS URL (fixed constructor arguments)
  - Kubernetes client with informer factory
  - ServiceAccount cache initialization and sync
  - Auth handler wiring (fixed interface compatibility)
  - NATS client connection and auth callout service
  - HTTP server with health and metrics endpoints (simple liveness check)
  - Graceful shutdown handling
  - **Compiles successfully** ✅

- **End-to-end tests** (`e2e_test.go`) - Full system integration tests ✅
  - ✅ **TestE2E**: Full auth callout flow validation
    - Real k3s cluster with ServiceAccount annotations
    - NATS server with auth callout configuration
    - Real Kubernetes token creation via TokenRequest API
    - Complete auth flow: JWT → K8s lookup → permissions → NATS
    - Permission enforcement testing (pub/sub subjects)
    - Request-reply pattern validation
  - ✅ **TestE2E_WrongAudience**: JWT audience validation
    - Verifies tokens with incorrect audience are rejected
  - ✅ **TestE2E_MaxMsgsOneResponseLimit**: Response security
    - Validates `MaxMsgs: 1` response limitation
    - Tests `allow_responses: true` security pattern
  - ✅ **TestE2E_PrivateInboxPattern**: Private inbox isolation
    - Two ServiceAccounts with private inbox permissions
    - Service-a uses custom inbox prefix (`_INBOX_default_service-a`)
    - Service-b cannot eavesdrop on service-a's private inbox
    - Standard inbox still works for convenience
    - Validates cross-ServiceAccount isolation
  - Build tag: `// +build e2e` for separation from unit tests
  - Run with: `make test-e2e` or `go test -tags=e2e ./e2e_test.go`
  - All tests passing (execution time: ~10s per test)

**Note:** Health checks are complete - simple liveness checks without upstream dependency checks (correct design per best practices)

### 📋 Pending
- **Deployment resources**
  - Kubernetes manifests (Deployment, Service, RBAC)
  - Helm chart for easy installation
  - Production deployment guide with examples

## Project Structure

```
cmd/server/main.go          - ✅ Entry point, complete application wiring
internal/config/            - ✅ Environment variable configuration
internal/http/              - ✅ Health & metrics endpoints (simple liveness)
internal/jwt/               - ✅ JWT validation & JWKS handling
internal/k8s/               - ✅ ServiceAccount cache (informer pattern)
internal/auth/              - ✅ Authorization handler & permission builder
internal/nats/              - ✅ NATS connection & subscription handling
testdata/                   - ✅ Real test data (JWKS, token, ServiceAccount)
e2e_test.go                 - ✅ End-to-end integration tests (k3s + NATS + auth callout)
docs/                       - ✅ Comprehensive documentation (plans, client usage, deployment)
```

## Dependencies

- `github.com/nats-io/nats.go` - NATS client
- `github.com/nats-io/nkeys` - NKey cryptography
- `k8s.io/client-go` - Kubernetes API client
- `github.com/golang-jwt/jwt/v5` - JWT parsing
- `github.com/MicahParks/keyfunc/v2` - JWKS key fetching
- `github.com/prometheus/client_golang` - Metrics
- `go.uber.org/zap` - Structured logging

## JWT Validation Details

The JWT validator (`internal/jwt/`) provides comprehensive token validation:

### Features Implemented
- **JWKS from HTTP URL**: Fetches JWKS from Kubernetes OIDC endpoint with automatic refresh
  - Production: `NewValidatorFromURL()` - HTTP fetch with caching
  - Testing: `NewValidatorFromFile()` - Load from file
- **Automatic key refresh**: Keys refreshed every hour with 5-minute rate limiting
- **Key rotation support**: Automatically refetches when unknown key ID encountered
- **Signature validation**: RS256 algorithm with key rotation support
- **Standard claims**: Validates issuer, audience, expiration, not-before, issued-at
- **K8s claims**: Extracts `kubernetes.io/serviceaccount/namespace` and `name`
- **Time mocking**: Injectable time function for testing expiration logic
- **Error types**: `ErrExpiredToken`, `ErrInvalidSignature`, `ErrInvalidClaims`, `ErrMissingK8sClaims`

### JWKS Caching Strategy
- **Refresh interval**: 1 hour (configurable)
- **Rate limiting**: Max one refresh per 5 minutes
- **Timeout**: 10 seconds per refresh request
- **Unknown KID handling**: Automatic refresh on unknown key ID
- **Library**: Uses `github.com/MicahParks/keyfunc/v2` for automatic management

### Testing Approach
- TDD (red-green-refactor) methodology
- Real test data from EKS cluster (testdata/)
- Time-based testing without external mocking libraries
- 6 test cases covering success and failure scenarios
- File-based JWKS loading for tests (no HTTP dependency)

## Testing Strategy

- **Unit tests**: Each internal package with mocks - ✅ Completed
- **Integration tests**: testcontainers-go NATS module - ✅ Completed
  - Simplified setup using `github.com/testcontainers/testcontainers-go/modules/nats`
  - Real NATS server with auth callout configuration
  - End-to-end auth callout flow validation
  - No temporary files needed (config via `strings.NewReader`)
- **E2E test**: testcontainers k3s + NATS - 📋 Designed, pending implementation
  - Auth callout verification with real ServiceAccount
  - Tests complete JWT validation → k8s lookup → permission building → NATS auth flow
  - Validates pub/sub permissions work correctly
  - See: `docs/plans/2025-11-24-main-wiring-and-e2e-test-design.md`
- **Manual testing**: Deploy to kind/k3s with real NATS server - 📋 Future
- **Coverage achieved**:
  - internal/auth: 100.0%
  - internal/k8s: 81.2%
  - internal/jwt: 72.3%
  - internal/nats: 29.7%

## Test Status Summary

### Unit Tests
```
✅ internal/auth:   100.0% coverage - ALL PASSING
✅ internal/config: 100.0% coverage - ALL PASSING (11 test cases)
✅ internal/jwt:     71.6% coverage - ALL PASSING
✅ internal/k8s:     81.2% coverage - ALL PASSING
✅ internal/nats:    28.9% coverage - ALL PASSING
✅ Application builds successfully
```

### Integration Tests
```
✅ internal/nats/integration_test.go - PASSING
   - Real NATS server with auth callout config
   - Auth service connection and subscription
   - Token rejection working correctly
```

### E2E Tests
```
✅ e2e_test.go - COMPLETE AND PASSING
   - ✅ k3s cluster startup and configuration
   - ✅ ServiceAccount creation with NATS annotations
   - ✅ NATS server with auth callout configuration
   - ✅ Real K8s token creation (TokenRequest API with "nats" audience)
   - ✅ Token extraction and validation
   - ✅ Complete auth callout flow (JWT → K8s → permissions → NATS)
   - ✅ Permission enforcement testing (pub/sub subjects)
   - ✅ Multiple test scenarios (valid token, wrong audience)
   - ✅ All tests passing (~10s execution time)
```

## Recent Accomplishments (2025-11-25)

### Implementation Complete ✅
1. **Full application implementation**
   - All components implemented and tested
   - Main application wiring complete
   - Health checks implemented (simple liveness)
   - All unit tests passing with high coverage

2. **End-to-end integration tests**
   - Complete E2E test suite with k3s + NATS + auth callout (4 test scenarios)
   - Real Kubernetes token creation via TokenRequest API
   - Full auth flow validation (JWT → K8s → permissions → NATS)
   - Permission enforcement testing (pub/sub subjects)
   - JWT audience validation (wrong audience rejection)
   - Response security (`MaxMsgs: 1` validation)
   - Private inbox pattern isolation (cross-ServiceAccount eavesdropping prevention)
   - All tests passing (~10s execution time per test)

3. **Comprehensive documentation**
   - Complete client usage guide (Go and Java examples)
   - Deployment guide with Kubernetes manifests
   - Build instructions with multi-arch support
   - Internal package documentation
   - Design documents

### Ready for Production
- ✅ All components implemented and tested
- ✅ Integration tests passing
- ✅ E2E tests passing
- ✅ Documentation complete
- 📋 Remaining: Helm chart and deployment manifests

## Related Documentation

- **Initial design**: `docs/plans/2025-11-24-nats-k8s-auth-design.md`
- **Wiring & E2E test design**: `docs/plans/2025-11-24-main-wiring-and-e2e-test-design.md`
- **NATS auth callout docs**: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout
- **NATS auth callout example**: https://natsbyexample.com/examples/auth/callout/cli

## Development Guidelines

- Follow standard Go project layout
- Use structured logging (zap) with consistent fields
- Instrument all operations with Prometheus metrics
- Handle errors gracefully with proper context
- Write tests alongside implementation
- Document public APIs and complex logic
