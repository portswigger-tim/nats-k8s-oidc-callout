# NATS Kubernetes OIDC Auth Callout - Claude Context

Go-based NATS auth callout service for Kubernetes service account JWT validation.

## Architecture

- **NATS auth callout**: Subscribes to NATS authorization request subjects
- **Kubernetes informer**: Watches ServiceAccounts cluster-wide
- **Lazy-load caching**: Cache on first auth, K8s watch keeps current
- **12-factor app**: Environment variable configuration

## Permission Model

- **Default**: Namespace isolation (`<namespace>.>`)
- **Opt-in**: ServiceAccount annotations for cross-namespace access
- **Separate controls**: `nats.io/allowed-pub-subjects`, `nats.io/allowed-sub-subjects`

## Security

- **Generic errors**: "authorization failed" for all failures
- **Detailed logging**: Specific failure reasons for debugging
- **Least privilege**: Minimal access by default
- **Full JWT validation**: Signature, claims, K8s-specific fields

## Status

### ✅ Complete
- CLI with graceful shutdown
- Configuration with smart defaults (K8s in-cluster)
- HTTP server (health + metrics)
- JWT validation (JWKS, RS256, auto-refresh)
- K8s client (ServiceAccount cache, informer)
- Authorization handler (100% test coverage)
- NATS client (auth callout integration)
- E2E tests (k3s + NATS, 4 scenarios)
- Helm chart with unit tests (deployment, service, RBAC, secrets)

## Project Structure

```
cmd/server/          - Entry point
internal/config/     - Environment config
internal/http/       - Health & metrics
internal/jwt/        - JWT validation
internal/k8s/        - ServiceAccount cache
internal/auth/       - Authorization logic
internal/nats/       - NATS connection
e2e_suite_test.go    - Integration tests
docs/                - Documentation
```

## Key Implementation Details

### JWT Validation
- JWKS from HTTP URL or file
- Automatic key refresh (1h interval, 5m rate limit)
- RS256 signature verification
- Standard claims: iss, aud, exp, nbf, iat
- K8s claims: namespace, serviceaccount

### ServiceAccount Cache
- Thread-safe in-memory cache
- Cluster-wide informer
- ADD/UPDATE/DELETE event handlers
- Default inbox patterns: `_INBOX.>`, `_INBOX_<namespace>_<sa>.>`

### Testing
- Unit: All internal packages
- Integration: testcontainers NATS
- E2E: k3s + NATS + real tokens
- Helm: Unit tests for all chart templates
- Coverage: auth 100%, k8s 81%, jwt 72%

## Development

**Build:**
```bash
make build-all      # All architectures
make docker-build   # Docker image
```

**Test:**
```bash
make test           # Unit tests
make test-e2e       # E2E tests (requires Docker)
make test-helm      # Helm unit tests (requires helm-unittest plugin)
make test-all       # All tests (unit + integration + e2e + helm)
```

**Run:**
```bash
export NATS_CREDS_FILE=/path/to/creds
export NATS_ACCOUNT=AUTH_ACCOUNT
./out/nats-k8s-oidc-callout
```

## Configuration

**Required:**
- `NATS_CREDS_FILE`
- `NATS_ACCOUNT`

**Optional (smart defaults):**
- `NATS_URL` (default: `nats://nats:4222`)
- `JWKS_URL` (default: K8s in-cluster)
- `JWT_ISSUER` (default: K8s in-cluster)
- `JWT_AUDIENCE` (default: `nats`)

## References

- [Initial design](docs/plans/2025-11-24-nats-k8s-auth-design.md)
- [E2E test design](docs/plans/2025-11-24-main-wiring-and-e2e-test-design.md)
- [NATS auth callout docs](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
