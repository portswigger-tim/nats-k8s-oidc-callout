# NATS Kubernetes OIDC Auth Callout Service - Design Document

**Date:** 2025-11-24
**Status:** Approved

## Overview

A NATS auth callout service that validates Kubernetes service account JWTs and provides subject-based authorization for NATS clients running in Kubernetes clusters.

## Goals

- Authenticate NATS clients using Kubernetes service account tokens
- Provide namespace-based subject isolation by default
- Allow explicit cross-namespace access via ServiceAccount annotations
- Deploy as a 12-factor application (environment-based config)
- Provide health checks and Prometheus metrics for observability

## Architecture

### Core Components

1. **NATS Client** - Connects to NATS and subscribes to authorization request subjects
2. **JWT Validator** - Validates K8s service account JWTs against JWKS endpoint
3. **ServiceAccount Cache** - Watches K8s ServiceAccounts and caches annotations
4. **Permission Builder** - Constructs NATS user claims based on JWT + annotations
5. **HTTP Server** - Provides health and metrics endpoints on port 8080

### High-Level Flow

```
NATS Server → [Auth Request] → NATS Client → JWT Validator
                                              ↓
                                         Extract namespace + SA name
                                              ↓
ServiceAccount Cache ← Query annotations ← Permission Builder
         ↓                                    ↓
    Annotations → Build NATS permissions → [Auth Response] → NATS Server
```

## Configuration

### Required Environment Variables

```bash
# NATS Connection
NATS_URL=nats://nats:4222              # NATS server URL
NATS_CREDS_FILE=/etc/nats/auth.creds   # Path to NATS credentials file
NATS_ACCOUNT=MyAccount                  # NATS account name to assign clients

# Kubernetes JWT Validation
JWKS_URL=https://kubernetes.default.svc/openid/v1/jwks  # K8s JWKS endpoint
JWT_ISSUER=https://kubernetes.default.svc                # Expected issuer claim
JWT_AUDIENCE=nats                                        # Expected audience claim

# ServiceAccount Annotation Settings
SA_ANNOTATION_PREFIX=nats.io/           # Prefix for NATS-related annotations

# Cache & Cleanup
CACHE_CLEANUP_INTERVAL=15m              # Evict unused cache entries older than this
```

### Optional Environment Variables

```bash
PORT=8080                               # HTTP server port for health/metrics
K8S_IN_CLUSTER=true                     # Use in-cluster K8s config
K8S_NAMESPACE=                          # Empty = watch all namespaces
LOG_LEVEL=info                          # Logging level: debug, info, warn, error
```

## Permission Model

### Default Namespace-Based Permissions

Every authenticated service account gets default publish/subscribe permissions scoped to their namespace:

```
Namespace: "foo"
Default permissions:
  - pub: ["foo.>"]      # Can publish to any subject under foo.*
  - sub: ["foo.>"]      # Can subscribe to any subject under foo.*
```

### Annotation-Based Extensions

ServiceAccounts can extend permissions via annotations with separate publish and subscribe controls:

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

Results in combined permissions:
```
pub: ["foo.>", "bar.>", "platform.commands.*"]
sub: ["foo.>", "platform.events.*", "shared.status"]
```

If only one annotation is present, the other uses just the default namespace scope. If neither annotation is present, both get namespace-only access.

### NATS User Claims Structure

The auth service returns a JWT containing standard NATS user claims:

```json
{
  "jti": "<unique-id>",
  "iat": <issued-at>,
  "iss": "<auth-service-nkey>",
  "sub": "<k8s-namespace>/<service-account-name>",
  "nats": {
    "pub": { "allow": ["foo.>", "bar.>"] },
    "sub": { "allow": ["foo.>", "bar.>"] },
    "subs": -1,    // Unlimited subscriptions
    "data": -1,    // Unlimited data
    "payload": -1  // Unlimited payload size
  }
}
```

## JWT Validation

### Validation Steps

1. **Parse JWT** - Decode token from NATS authorization request
2. **Fetch JWKS keys** - Retrieve signing keys from configured JWKS endpoint (cached with refresh)
3. **Verify signature** - Validate JWT signature against JWKS public keys
4. **Validate standard claims:**
   - `iss` (issuer) matches `JWT_ISSUER`
   - `aud` (audience) matches `JWT_AUDIENCE`
   - `exp` (expiration) is in the future
   - `nbf` (not-before) is in the past
   - `iat` (issued-at) is reasonable (not future-dated)
5. **Validate K8s-specific claims:**
   - `kubernetes.io/serviceaccount/namespace` exists and is non-empty
   - `kubernetes.io/serviceaccount/service-account.name` exists and is non-empty

### Error Handling Strategy

All validation failures return a NATS authorization error response with a generic error message: `"authorization failed"`.

Detailed failure reasons are:
- **Logged** with structured fields (reason, namespace, service account, source IP)
- **Metriced** via Prometheus counters with labels (failure_reason: expired, invalid_signature, missing_claim, etc.)

### Common Failure Scenarios

- `jwt_parse_error` - Malformed JWT
- `invalid_signature` - Signature verification failed
- `jwt_expired` - Token past expiration
- `invalid_issuer` - Issuer doesn't match expected
- `invalid_audience` - Audience doesn't match expected
- `missing_k8s_claims` - Required K8s claims absent
- `k8s_api_error` - Failed to fetch ServiceAccount annotations

## ServiceAccount Cache

### Cache Structure

In-memory map keyed by namespace/serviceaccount name:

```go
type CacheEntry struct {
    Annotations  map[string]string
    LastAccessed time.Time
}

cache: map[string]CacheEntry
// Key format: "namespace/serviceaccount-name"
```

### Kubernetes Watch (Informer Pattern)

Uses client-go's `SharedInformerFactory` to watch ServiceAccount resources cluster-wide:

- **Add/Update events** - Upsert cache entry with annotations, update timestamp
- **Delete events** - Remove from cache immediately
- **Resync** - Periodic full resync (client-go default: 10 hours) rebuilds cache

### Lazy Load Fallback

On authorization request, if ServiceAccount not in cache:
1. Perform synchronous GET to K8s API for that specific ServiceAccount
2. Add to cache with current timestamp
3. Continue with authorization flow

This handles:
- Race conditions at startup (auth request before watch populates)
- Missed watch events (network issues, reconnection gaps)
- Newly created ServiceAccounts before watch notification arrives

### Cache Cleanup

Background goroutine runs every `CACHE_CLEANUP_INTERVAL` (default 15m):
- Iterates cache entries
- Removes entries where `LastAccessed` older than cleanup interval
- Only affects entries not updated by watch (deleted ServiceAccounts)

## NATS Integration

### Connection Lifecycle

1. **Startup** - Load credentials from `NATS_CREDS_FILE`
2. **Connect** - Establish connection to `NATS_URL` with credentials
3. **Subscribe** - Subscribe to authorization subject (exact pattern TBD during implementation)
4. **Process** - Handle incoming authorization requests
5. **Graceful shutdown** - Drain subscriptions, close connection on SIGTERM/SIGINT

### Authorization Request Processing

Processing flow:
1. Parse authorization request JWT from NATS
2. Extract K8s JWT from request
3. Validate K8s JWT (see JWT Validation section)
4. Extract namespace and service account name from validated claims
5. Look up ServiceAccount annotations in cache (lazy load if needed)
6. Build NATS permissions (see Permission Model section)
7. Generate user claims JWT signed by auth service
8. Publish authorization response

### Authorization Response

Success response:
```json
{
  "jwt": "<signed-user-claims-jwt>",
  "issuer_account": "<NATS_ACCOUNT public key>"
}
```

Failure response:
```json
{
  "error": "authorization failed"
}
```

## Observability

### HTTP Endpoints (Port 8080)

```
GET /health       - Health check endpoint
GET /metrics      - Prometheus metrics endpoint
```

### Health Check

Returns HTTP 200 if healthy, 503 if unhealthy:
```json
{
  "status": "healthy",
  "checks": {
    "nats_connected": true,
    "k8s_connected": true,
    "cache_initialized": true
  }
}
```

### Prometheus Metrics

```
# Authorization requests
nats_auth_requests_total{result="success|failure", failure_reason=""}

# JWT validation
jwt_validation_duration_seconds{result="success|failure"}
jwt_validation_errors_total{reason="expired|invalid_signature|missing_claim|..."}

# Cache operations
sa_cache_size                          # Current cache entry count
sa_cache_hits_total
sa_cache_misses_total
sa_cache_evictions_total
k8s_api_calls_total{operation="get|list|watch"}

# NATS operations
nats_connection_status{status="connected|disconnected"}
nats_messages_processed_total
nats_message_processing_duration_seconds
```

### Structured Logging

JSON-formatted logs with fields:
- `timestamp`, `level`, `msg`
- `namespace`, `service_account`, `client_ip`
- `failure_reason` (for errors)
- `duration_ms` (for operations)

## Project Structure

```
nats-k8s-oidc-callout/
├── cmd/
│   └── server/
│       └── main.go                 # Entry point, wiring
├── internal/
│   ├── config/
│   │   └── config.go              # Environment variable loading
│   ├── nats/
│   │   └── client.go              # NATS connection & subscription
│   ├── jwt/
│   │   └── validator.go           # JWT validation & JWKS handling
│   ├── k8s/
│   │   └── cache.go               # ServiceAccount informer & cache
│   ├── auth/
│   │   └── handler.go             # Authorization request handler
│   └── http/
│       └── server.go              # Health & metrics HTTP server
├── go.mod
├── go.sum
├── Dockerfile
└── README.md
```

## Dependencies

```go
// NATS
github.com/nats-io/nats.go         // NATS client
github.com/nats-io/nkeys           // NKey cryptography

// Kubernetes
k8s.io/client-go                    // K8s API client & informers
k8s.io/api                          // K8s resource types

// JWT
github.com/golang-jwt/jwt/v5       // JWT parsing & validation
github.com/MicahParks/keyfunc/v2   // JWKS key fetching & caching

// Observability
github.com/prometheus/client_golang // Prometheus metrics
go.uber.org/zap                     // Structured logging
```

## Testing Strategy

### Unit Tests

Each internal package should have comprehensive unit tests:

- **jwt/validator**: Mock JWKS responses, test signature validation, claim validation, error cases
- **k8s/cache**: Test cache operations, lazy loading, eviction logic (without real K8s API)
- **auth/handler**: Test permission building logic with various annotation combinations
- **config**: Test environment variable parsing, validation, defaults

### Integration Tests

- **NATS integration**: Use embedded NATS server to test request/response flow
- **Kubernetes integration**: Use envtest to spin up local API server and test informer/cache
- **End-to-end**: Combine both with real JWT validation flow

### Test Coverage Target

Aim for >80% coverage on business logic (auth, jwt, permission building).

### Manual Testing

Deploy to local kind/k3s cluster:
1. Create test ServiceAccounts with various annotations
2. Deploy NATS server with auth callout configured
3. Deploy auth service
4. Test NATS clients with different K8s service account tokens
5. Verify permissions work as expected
6. Check metrics and logs

## Security Considerations

1. **Generic error messages** - Don't leak validation details to clients
2. **Structured logging** - Capture detailed errors for debugging
3. **Principle of least privilege** - Default to namespace-only access
4. **Explicit cross-namespace** - Require annotations for broader access
5. **JWKS caching** - Minimize external calls while respecting key rotation
6. **Graceful degradation** - Handle K8s API failures without blocking all auth

## Open Questions

The following will be validated during implementation:

1. Exact NATS subject pattern for auth callout subscription
2. NATS authorization request/response JWT structure and encryption (XKey)
3. Auth service NKey generation and management
4. JWKS cache TTL and refresh strategy
