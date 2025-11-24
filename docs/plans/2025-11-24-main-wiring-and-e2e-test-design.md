# Main Application Wiring and E2E Test Design

**Date:** 2025-11-24
**Status:** Approved
**Author:** Design session with user

## Overview

This document describes the design for completing the main application wiring (specifically health checks) and implementing an end-to-end integration test using testcontainers for both k3s and NATS.

## Current State

The `cmd/server/main.go` is mostly complete with:
- Configuration loading
- Logger initialization
- JWT validator setup
- Kubernetes client with informer
- ServiceAccount cache
- Auth handler creation
- NATS client initialization
- HTTP server with health endpoints
- Graceful shutdown handling

### Gaps to Address

1. **Health check TODOs** (lines 110-121) - placeholder functions that always return `true`
2. **No E2E test** - Need full integration test with both k3s and NATS

## Design Part 1: Health Check Implementation

### Approach

Add `IsHealthy()` methods to the NATS Client and K8s Client packages. The main.go will call these methods in the health check closures.

### NATS Client Health Check

**File:** `internal/nats/client.go`

```go
// IsHealthy returns true if connected to NATS
func (c *Client) IsHealthy() bool {
    return c.conn != nil && c.conn.IsConnected()
}
```

**Implementation details:**
- Uses existing `conn.IsConnected()` from nats.go library
- Simple nil check for safety
- Returns false if connection is nil or disconnected

### K8s Client Health Check

**File:** `internal/k8s/client.go`

```go
// IsHealthy returns true if the informer cache has synced
func (c *Client) IsHealthy() bool {
    return c.informer.HasSynced()
}
```

**Implementation details:**
- Uses existing `informer.HasSynced()` from client-go
- Confirms the ServiceAccount cache is ready
- Returns false if informer hasn't completed initial sync

### Main.go Wiring

Replace the TODO placeholders (lines 109-122) with:

```go
httpSrv := httpserver.New(cfg.Port, logger, httpserver.HealthChecks{
    NatsConnected:    func() bool { return natsClient.IsHealthy() },
    K8sConnected:     func() bool { return k8sClient.IsHealthy() },
    CacheInitialized: func() bool { return k8sClient.IsHealthy() },
})
```

**Note:** We use the same check for `K8sConnected` and `CacheInitialized` since they're both based on informer sync status. This is appropriate because the informer handles both connectivity and cache synchronization.

### Trade-offs

**Chosen approach:** Add methods to each component
- ✅ Encapsulates health logic with the component
- ✅ Cleaner main.go, just calls methods
- ✅ More maintainable for production use
- ⚠️ Requires modifying internal packages (minimal change)

**Alternative considered:** Keep health check logic in main.go using closures
- ❌ Health logic separated from component code
- ✅ No changes to internal packages

## Design Part 2: E2E Test Architecture

### Test Scope

**Goal:** Auth callout verification test

Test the complete auth flow (JWT validation, k8s lookup, permission building, NATS response) without the complexity of managing multiple test NATS clients.

### Test Setup Flow

1. **Start k3s container** (testcontainers)
2. **Deploy a test ServiceAccount** with NATS permission annotations
3. **Start NATS server container** with auth callout configuration
4. **Start our service** (as goroutine, not container) connecting to both
5. **Connect NATS client with test JWT** - triggers auth callout
6. **Verify permissions** by attempting pub/sub operations

### Key Design Decisions

#### Service Execution Strategy

**Chosen:** Run in-process as goroutine
- ✅ Simpler and faster than containerizing
- ✅ Still tests all real components (k8s client, NATS client, JWT validation)
- ✅ Easier to debug and see logs
- ✅ Can reuse existing wiring logic

**Alternative:** Run service in container
- ❌ More complex setup
- ❌ Slower test execution
- ✅ Closer to production deployment (not needed for this test)

#### JWT Token Generation

**Chosen:** Generate dynamically in test
- ✅ Full control over namespace/SA name
- ✅ Can test different scenarios
- ✅ Use `github.com/golang-jwt/jwt/v5` to create and sign test tokens

**Alternative:** Use existing testdata JWT
- ❌ Fixed namespace/SA name
- ❌ Less flexible for different test scenarios

#### Auth Callout Request Simulation

**Chosen:** Real NATS client connection with JWT
- ✅ Tests the actual integration flow
- ✅ Validates auth callout protocol handling
- ✅ Most realistic test scenario

**Alternative:** Programmatically send auth callout request
- ❌ More complex protocol handling
- ❌ Less realistic

#### NATS Server Configuration

NATS server needs auth callout configuration. Example:

```
authorization {
    auth_callout {
        issuer: "test-account"
        auth_users: [ "auth-service" ]
    }
}
```

The auth callout will use the NATS subject that our service subscribes to.

### Test File Structure

**Location:** `e2e_test.go` in project root

**Build tag:** `// +build e2e` to separate from unit tests

**Package:** `package main_test` to test as external consumer

## Design Part 3: E2E Test Implementation

### Test Structure

```go
// +build e2e

package main_test

import (
    "context"
    "testing"
    "time"

    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/k3s"
    natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"

    natsclient "github.com/nats-io/nats.go"
    "github.com/golang-jwt/jwt/v5"
)
```

### Test Implementation Steps

#### Step 1: K3s Setup

```go
// Start k3s container
k3sContainer, err := k3s.RunContainer(ctx)
require.NoError(t, err)
defer k3sContainer.Terminate(ctx)

// Get kubeconfig
kubeconfig, err := k3sContainer.GetKubeConfig(ctx)
require.NoError(t, err)

// Create k8s client from kubeconfig
// Deploy test ServiceAccount with annotations:
//   nats.io/allowed-pub-subjects: "test.>"
//   nats.io/allowed-sub-subjects: "test.>,other.>"
```

#### Step 2: Generate Test JWT

```go
// Create JWT with claims:
// - iss: matching our JWKS issuer
// - aud: nats audience
// - kubernetes.io/serviceaccount/namespace: "default"
// - kubernetes.io/serviceaccount/name: "test-sa"

claims := jwt.MapClaims{
    "iss": "test-issuer",
    "aud": "nats",
    "exp": time.Now().Add(5 * time.Minute).Unix(),
    "kubernetes.io/serviceaccount/namespace": "default",
    "kubernetes.io/serviceaccount/name": "test-sa",
}

// Sign with test key pair
token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
testJWT, err := token.SignedString(privateKey)
```

#### Step 3: NATS Server Setup

```go
natsContainer, err := natscontainer.RunContainer(ctx,
    natscontainer.WithConfig(authCalloutConfig),
)
require.NoError(t, err)
defer natsContainer.Terminate(ctx)

natsURL := natsContainer.MustConnectionString(ctx)
```

#### Step 4: Start Our Service

```go
// Wire up components similar to main.go:
// - Use k3s kubeconfig for k8s client
// - Use natsURL for NATS connection
// - Use test JWKS (matching our test JWT signing key)

// Run in goroutine, track errors
serviceErr := make(chan error, 1)
go func() {
    serviceErr <- runService(ctx, k8sClient, natsURL, jwksURL)
}()

// Wait for service to be ready (check health endpoint)
```

#### Step 5: Test Auth Flow

```go
// Connect NATS client with test JWT - this triggers auth callout
nc, err := natsclient.Connect(natsURL,
    natsclient.UserJWT(func() (string, error) {
        return testJWT, nil
    }),
)
require.NoError(t, err)
defer nc.Close()

// Test 1: Publish to allowed subject (should succeed)
err = nc.Publish("test.hello", []byte("data"))
assert.NoError(t, err)

// Test 2: Publish to denied subject (should fail or timeout)
err = nc.Publish("denied.topic", []byte("data"))
assert.Error(t, err) // or verify timeout

// Test 3: Subscribe to allowed subject (should succeed)
sub, err := nc.SubscribeSync("test.messages")
assert.NoError(t, err)
defer sub.Unsubscribe()

// Test 4: Subscribe to denied subject (should fail)
_, err = nc.SubscribeSync("denied.messages")
assert.Error(t, err)
```

### Test Data Requirements

**Test JWKS:**
- Create a test RSA key pair
- Generate JWKS JSON with public key
- Serve via HTTP (could use httptest.Server or mount in container)

**Test ServiceAccount:**
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: test-sa
  namespace: default
  annotations:
    nats.io/allowed-pub-subjects: "test.>"
    nats.io/allowed-sub-subjects: "test.>,other.>"
```

### Makefile Integration

Add E2E test target:

```makefile
.PHONY: test-e2e

test-e2e:
	@echo "Running E2E tests (requires Docker)..."
	@docker info > /dev/null 2>&1 || (echo "Error: Docker is not running" && exit 1)
	go test -tags=e2e -v ./e2e_test.go
```

## Success Criteria

### Health Checks
- ✅ NATS health check returns true when connected, false when disconnected
- ✅ K8s health check returns true when cache synced, false otherwise
- ✅ `/health` endpoint reflects actual component status

### E2E Test
- ✅ Test successfully starts k3s and NATS containers
- ✅ Service connects to both k3s and NATS
- ✅ NATS client can connect with valid JWT
- ✅ Publishing to allowed subjects succeeds
- ✅ Publishing to denied subjects fails appropriately
- ✅ Subscribing to allowed subjects succeeds
- ✅ Subscribing to denied subjects fails appropriately
- ✅ Test cleans up containers on completion

## Implementation Order

1. Add `IsHealthy()` method to `internal/nats/client.go`
2. Add `IsHealthy()` method to `internal/k8s/client.go`
3. Update health check wiring in `cmd/server/main.go`
4. Create `e2e_test.go` with test structure
5. Implement test helper functions (JWT generation, k8s client setup, etc.)
6. Implement main E2E test function
7. Add `test-e2e` target to Makefile
8. Run and validate E2E test

## Future Enhancements

- Add more E2E test scenarios (missing annotations, invalid JWT, expired token)
- Test namespace isolation (cross-namespace pub/sub should fail)
- Add performance/load testing
- Test graceful shutdown and reconnection scenarios
