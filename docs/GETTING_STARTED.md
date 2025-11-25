# Getting Started Guide

This guide explains how the NATS Kubernetes OIDC auth callout service works.

## The Problem We're Solving

Apps running in Kubernetes need to connect to NATS and prove their identity. Instead of managing passwords, we use Kubernetes service account tokens (which apps already have).

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. Your App (K8s Pod)                                           │
│    "I want to connect to NATS!"                                 │
│    Shows: Kubernetes JWT token                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. NATS Server                                                  │
│    "Is this app allowed?"                                       │
│    Sends: Auth callout request                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. Auth Service (Our Code)                                      │
│    ✓ Validates JWT token                                        │
│    ✓ Looks up permissions in K8s cache                          │
│    ✓ Builds permission response                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. NATS Server                                                  │
│    ✅ "Welcome! You can publish/subscribe to: ..."              │
│    ❌ Or: "Access Denied"                                       │
└─────────────────────────────────────────────────────────────────┘
```

## Architecture Components

### Entry Point (`cmd/server/main.go`)
1. Load configuration from environment
2. Start HTTP server (health checks)
3. Start JWT validator
4. Start Kubernetes watcher (monitors ServiceAccount changes)
5. Start NATS client (handles auth requests)

### Authorization Flow

**Step 1: Extract Token** (`internal/nats/client.go`)
- Receives auth request from NATS
- Extracts JWT token from request

**Step 2: Validate Token** (`internal/jwt/validator.go`)
- Verifies JWT signature using JWKS
- Checks expiration, issuer, audience
- Extracts namespace and service account claims

**Step 3: Lookup Permissions** (`internal/k8s/cache.go`)
- Retrieves ServiceAccount from cache
- Reads permission annotations:
  - `nats.io/allowed-pub-subjects`
  - `nats.io/allowed-sub-subjects`
- Applies default namespace isolation

**Step 4: Build Response** (`internal/auth/handler.go`)
- Combines default + annotated permissions
- Returns to NATS with allow/deny decision

## Permissions Model

### Default Permissions
Every ServiceAccount gets namespace isolation:

```yaml
ServiceAccount: my-service
Namespace: production
Default:
  Publish: ["_INBOX.>", "_INBOX_production_my-service.>", "production.>"]
  Subscribe: ["_INBOX.>", "_INBOX_production_my-service.>", "production.>"]
```

### Custom Permissions
Add annotations to grant additional access:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service
  namespace: production
  annotations:
    nats.io/allowed-pub-subjects: "orders.>, inventory.check"
    nats.io/allowed-sub-subjects: "orders.responses.*"
```

Result:
- Publish: `_INBOX.>`, `production.>`, `orders.>`, `inventory.check`
- Subscribe: `_INBOX.>`, `production.>`, `orders.responses.*`

## Key Concepts

### JWT (JSON Web Token)
A secure token with three parts: `header.payload.signature`
- Header: Token metadata
- Payload: Identity claims (namespace, service account, expiration)
- Signature: Cryptographic proof of authenticity

### Auth Callout
Instead of storing credentials, NATS delegates authentication to our service via pub/sub.

### Kubernetes Watch
A background process that monitors ServiceAccount changes in real-time, keeping the permission cache current.

## Development

### Build
```bash
make build              # Local architecture
make build-all          # All architectures
make docker-build       # Docker image
```

### Test
```bash
make test               # Unit tests (fast)
make test-integration   # Integration tests (requires Docker)
make test-e2e           # End-to-end tests (requires Docker)
make coverage           # Coverage report
```

### Run Locally
```bash
export NATS_CREDS_FILE=/path/to/nats.creds
export NATS_ACCOUNT=MyAccount
# Optional: NATS_URL, JWKS_URL, JWT_ISSUER, JWT_AUDIENCE

./out/nats-k8s-oidc-callout
```

## Debugging

**View logs:**
```bash
kubectl logs -n nats-system deployment/nats-auth-callout -f
```

**Create test token:**
```bash
kubectl create token my-service --namespace=my-app --audience=nats --duration=1h
```

**Decode JWT:**
```bash
echo "TOKEN" | cut -d. -f2 | base64 -d | jq
```

## Security Features

1. **Generic errors** - All failures return "authorization failed" (no hints for attackers)
2. **Multi-layer validation** - Format, signature, expiration, issuer, audience, existence
3. **Namespace isolation** - Default least-privilege permissions
4. **Real-time updates** - Permission changes take effect immediately

## FAQ

**Q: Why not use NATS built-in auth?**
A: Kubernetes identities eliminate credential management overhead.

**Q: How fast are auth checks?**
A: 1-5ms - permissions cached in memory, no external calls.

**Q: What if auth service goes down?**
A: Existing connections continue; new connections fail until recovery. Run multiple replicas for HA.

**Q: Can I test without a full cluster?**
A: Yes - E2E tests use k3s in Docker.

## Additional Resources

- [Client Usage Guide](CLIENT_USAGE.md) - Application integration examples
- [Deployment Guide](DEPLOY.md) - Kubernetes deployment
- [Build Guide](BUILD.md) - Build and package instructions
- [NATS Auth Callout Docs](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
