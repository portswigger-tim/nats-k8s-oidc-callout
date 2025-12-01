# NATS Kubernetes OIDC Auth Callout

A NATS auth callout service that authenticates NATS clients using Kubernetes service account JWTs.

## Features

- **JWT Validation**: Validates K8s service account tokens against JWKS endpoint
- **Namespace Isolation**: Default permissions scoped to pod namespace (`<namespace>.>`)
- **Opt-in Cross-Namespace**: ServiceAccount annotations for broader permissions
- **Real-time Updates**: Kubernetes watch keeps permissions current
- **Observability**: Health checks, Prometheus metrics, structured logging

## Quick Start

### Prerequisites

- Kubernetes cluster with OIDC token projection
- NATS server with auth callout enabled
- ServiceAccount with cluster-wide watch permissions

### Configuration

Required environment variables:

```bash
NATS_CREDS_FILE=/etc/nats/auth.creds
NATS_ACCOUNT=MyAccount
```

Optional (smart defaults for standard K8s deployments):

```bash
NATS_URL=nats://nats:4222                              # default
JWKS_URL=https://kubernetes.default.svc/openid/v1/jwks # default when K8S_IN_CLUSTER=true
JWT_ISSUER=https://kubernetes.default.svc              # default when K8S_IN_CLUSTER=true
JWT_AUDIENCE=nats                                       # default
```

### Granting Permissions

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

**Default Permissions:**
- Publish: `foo.>` (namespace only)
- Subscribe: `_INBOX.>`, `_INBOX_foo_my-service.>`, `foo.>`

**With Annotations:**
- Publish: `foo.>`, `bar.>`, `platform.commands.*`
- Subscribe: `_INBOX.>`, `_INBOX_foo_my-service.>`, `foo.>`, `platform.events.*`, `shared.status`

**Request-Reply:** Enabled via `allow_responses: true` (MaxMsgs: 1 per request)

### Inbox Patterns

Two inbox patterns for request-reply:

1. **Standard (`_INBOX.>`)** - Default convenience, works without configuration
2. **Private (`_INBOX_namespace_serviceaccount.>`)** - Opt-in isolation, prevents eavesdropping

See [Client Usage Guide](docs/CLIENT_USAGE.md) for implementation examples.

## Documentation

- **[Getting Started](docs/GETTING_STARTED.md)** - Complete walkthrough
- **[Client Usage](docs/CLIENT_USAGE.md)** - Go and Java examples
- **[Deployment](docs/DEPLOY.md)** - Kubernetes deployment
- **[Build](docs/BUILD.md)** - Build and package
- **[Design](docs/plans/2025-11-24-nats-k8s-auth-design.md)** - Architecture details

## Status

**✅ Complete:** Core implementation and testing
- JWT validation with JWKS
- ServiceAccount cache with K8s watch
- Authorization handler
- NATS auth callout integration
- E2E tests with k3s + NATS
- Helm chart with unit tests

## Testing

**Unit Tests:**
```bash
make test
```

**Integration Tests** (requires Docker):
```bash
make test-integration
```

**E2E Tests** (requires Docker):
```bash
make test-e2e
```

**Coverage:**
```bash
make coverage
```

## Observability

**Health Check:**
```bash
curl http://localhost:8080/health
```

**Metrics** (`http://localhost:8080/metrics`):
- `nats_auth_requests_total` - Auth request counts
- `jwt_validation_duration_seconds` - Validation latency
- `sa_cache_size` - Cache size
- `k8s_api_calls_total` - K8s API calls

## Development

**Build:**
```bash
go build -o nats-k8s-auth ./cmd/server
```

**Run:**
```bash
export NATS_URL=nats://localhost:4222
export NATS_CREDS_FILE=/path/to/auth.creds
export NATS_ACCOUNT=MyAccount
./nats-k8s-auth
```

## License

See [LICENSE](LICENSE) file.
