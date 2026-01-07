# Deploying nats-k8s-oidc-callout

Production deployment guide for the NATS Kubernetes OIDC auth callout service.

## Prerequisites

- Kubernetes cluster (1.20+)
- `kubectl` configured
- `helm` (3.0+)
- `nsc` (NATS CLI tools) - for operator setup
- NATS server (2.9+) with auth callout support

## Quick Start

### 1. Install NATS Server

```bash
# Add NATS Helm repository
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm repo update

# Create namespace
kubectl create namespace nats-system

# Install NATS with basic configuration
helm install nats nats/nats \
  --namespace nats-system \
  --set config.cluster.enabled=true \
  --set config.jetstream.enabled=true
```

### 2. Set Up NATS Operator and Accounts

**For detailed operator setup, see [OPERATOR_SETUP.md](OPERATOR_SETUP.md)**

Quick version:

```bash
# Create operator
nsc add operator nats-system

# Create required accounts
nsc add account SYS
nsc edit account SYS --sk system
nsc add user --account SYS sys-user
nsc generate creds --account SYS --name sys-user > sys-user.creds

nsc add account AUTH_SERVICE
nsc add user --account AUTH_SERVICE auth-service
nsc edit user --account AUTH_SERVICE auth-service \
  --allow-pub '$SYS.REQ.USER.AUTH' \
  --allow-sub '_INBOX.>'
nsc generate creds --account AUTH_SERVICE --name auth-service > auth-service.creds

nsc add account AUTH_ACCOUNT

# Export JWTs
mkdir -p /tmp/nats-jwt
nsc describe operator --json | jq -r .jwt > /tmp/nats-jwt/operator.jwt
nsc describe account SYS --json | jq -r .jwt > /tmp/nats-jwt/SYS.jwt
nsc describe account AUTH_SERVICE --json | jq -r .jwt > /tmp/nats-jwt/AUTH_SERVICE.jwt
nsc describe account AUTH_ACCOUNT --json | jq -r .jwt > /tmp/nats-jwt/AUTH_ACCOUNT.jwt

# Get auth-service user public key (needed for NATS config)
AUTH_SERVICE_ISSUER=$(nsc describe user --account AUTH_SERVICE auth-service --json | jq -r .sub)
echo "Auth Service Issuer: $AUTH_SERVICE_ISSUER"
```

### 3. Configure NATS for Auth Callout

Create NATS configuration:

```yaml
# nats-values.yaml
config:
  cluster:
    enabled: true
  jetstream:
    enabled: true

  # Operator mode with JWT resolver
  resolver:
    enabled: true
    operator: /etc/nats-config/operator/operator.jwt
    systemAccount: SYS
    store:
      dir: /etc/nats-config/jwt
      size: 10Mi

  # Authorization callout configuration
  merge:
    authorization:
      auth_callout:
        # Replace with your auth-service user public key
        issuer: "UABC123XYZ..."
        auth_users: ["auth-service"]
        account: "AUTH_SERVICE"
```

Create Secrets and upgrade NATS:

```bash
# Create Secrets with JWTs
# Note: Account JWTs are public information (contain public keys), but we use
# Secrets for consistency with credential storage best practices
kubectl create secret generic nats-operator \
  --namespace nats-system \
  --from-file=operator.jwt=/tmp/nats-jwt/operator.jwt

kubectl create secret generic nats-jwt \
  --namespace nats-system \
  --from-file=SYS.jwt=/tmp/nats-jwt/SYS.jwt \
  --from-file=AUTH_SERVICE.jwt=/tmp/nats-jwt/AUTH_SERVICE.jwt \
  --from-file=AUTH_ACCOUNT.jwt=/tmp/nats-jwt/AUTH_ACCOUNT.jwt

# Upgrade NATS with operator mode and auth callout
helm upgrade nats nats/nats \
  --namespace nats-system \
  --values nats-values.yaml

# Verify NATS configuration
kubectl logs -n nats-system nats-0 | grep -E "operator|resolver|auth"
# Expected: Operator loaded, resolver enabled, auth callout enabled
```

### 4. Deploy Auth Callout Service

```bash
# Create namespace
kubectl create namespace nats-auth

# Create secret with auth service credentials
kubectl create secret generic nats-auth-creds \
  --namespace nats-auth \
  --from-file=credentials=./auth-service.creds

# Install auth callout service
helm install nats-k8s-oidc-callout ./helm/nats-k8s-oidc-callout \
  --namespace nats-auth \
  --set nats.url="nats://nats.nats-system.svc.cluster.local:4222" \
  --set nats.account="AUTH_ACCOUNT" \
  --set nats.credentials.existingSecret="nats-auth-creds"

# Verify deployment
kubectl get pods -n nats-auth
kubectl logs -n nats-auth -l app.kubernetes.io/name=nats-k8s-oidc-callout
```

### 5. Configure Client ServiceAccount

```yaml
# client-sa.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-nats-client
  namespace: my-app
  annotations:
    # Define allowed subjects for this client
    nats.io/allowed-pub-subjects: "my-app.requests.>,my-app.events.>"
    nats.io/allowed-sub-subjects: "my-app.responses.>,my-app.commands.>"
```

Apply:
```bash
kubectl apply -f client-sa.yaml
```

### 6. Test Authentication

```bash
# Create test token
kubectl create token my-nats-client \
  --namespace=my-app \
  --audience=nats \
  --duration=1h > test-token.txt

# Test with NATS CLI
nats --server=nats://nats.nats-system.svc.cluster.local:4222 \
  --token=$(cat test-token.txt) \
  pub my-app.requests.test "hello"

# Expected: Message published successfully
```

## Configuration Reference

### Helm Values

Key configuration options for the auth callout service:

```yaml
# values.yaml
replicaCount: 2  # Recommended for HA

nats:
  url: "nats://nats.nats-system.svc.cluster.local:4222"
  account: "AUTH_ACCOUNT"  # REQUIRED
  credentials:
    existingSecret: "nats-auth-creds"  # REQUIRED
    existingSecretKey: "credentials"

jwt:
  # These have smart defaults for in-cluster usage
  issuer: ""      # Default: https://kubernetes.default.svc
  audience: ""    # Default: nats
  jwksUrl: ""     # Default: https://kubernetes.default.svc/openid/v1/jwks

logLevel: info

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 256Mi
```

### ServiceAccount Annotations

Configure client permissions via annotations:

| Annotation | Description | Example |
|------------|-------------|---------|
| `nats.io/allowed-pub-subjects` | Comma-separated publish subjects | `app.*.requests,app.events.>` |
| `nats.io/allowed-sub-subjects` | Comma-separated subscribe subjects | `app.*.responses,app.commands.>` |

**Subject Patterns:**
- `*` - Single token wildcard (e.g., `app.*.requests` matches `app.foo.requests`)
- `>` - Multi-token wildcard (e.g., `app.>` matches `app.foo.bar.baz`)
- Must be last token if used

**Default Permissions:**
- Publish: `<namespace>.>` (namespace-scoped only)
- Subscribe: `<namespace>.>`, `_INBOX.>`, `_INBOX_<namespace>_<sa>.>`

## Production Deployment

### High Availability

```yaml
# Production values
replicaCount: 3

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: nats-k8s-oidc-callout
          topologyKey: kubernetes.io/hostname

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 512Mi
```

### Security

**Network Policies:**

```yaml
networkPolicy:
  enabled: true
  natsSelector:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: nats-system
      podSelector:
        matchLabels:
          app: nats
```

**Best Practices:**
- Use TLS for NATS connections in production
- Regularly rotate NATS credentials
- Enable audit logging for authorization decisions
- Monitor unauthorized access attempts
- Use sealed-secrets or external secret managers for credential storage

### Monitoring

**Prometheus Integration:**

```yaml
metrics:
  podMonitor:
    enabled: true
    interval: 30s
    labels:
      prometheus: kube-prometheus
```

**Key Metrics:**
- `nats_auth_requests_total` - Total auth requests
- `nats_auth_requests_success` - Successful authentications
- `nats_auth_requests_denied` - Denied authentications
- `nats_auth_cache_hits` - ServiceAccount cache hits
- `nats_auth_cache_misses` - ServiceAccount cache misses

**Alerts:**
- High authentication failure rate (>5%)
- Low cache hit rate (<90%)
- Service unavailability
- Credential expiration

### Logging

**Grafana Agent Integration:**

```yaml
logs:
  podLogs:
    enabled: true
    labels:
      job: nats-auth-callout
```

**Log Levels:**
- `debug` - Detailed JWT validation and permission checks
- `info` - Successful auth requests and service events (default)
- `warn` - Authentication failures and cache issues
- `error` - Service errors and critical failures

## Verification

### Check Service Health

```bash
# Check pods
kubectl get pods -n nats-auth

# Check logs
kubectl logs -n nats-auth -l app.kubernetes.io/name=nats-k8s-oidc-callout

# Check health endpoint
kubectl port-forward -n nats-auth svc/nats-k8s-oidc-callout 8080:8080
curl http://localhost:8080/health
# Expected: {"status":"ok"}
```

### Check Metrics

```bash
kubectl port-forward -n nats-auth svc/nats-k8s-oidc-callout 8080:8080
curl http://localhost:8080/metrics
```

### Test End-to-End

```bash
# 1. Create test ServiceAccount with permissions
kubectl create serviceaccount test-client -n default
kubectl annotate serviceaccount test-client -n default \
  nats.io/allowed-pub-subjects="test.>" \
  nats.io/allowed-sub-subjects="test.>"

# 2. Create token
TOKEN=$(kubectl create token test-client -n default --audience=nats)

# 3. Test publish (should succeed)
nats --server=nats://nats.nats-system.svc.cluster.local:4222 \
  --token=$TOKEN \
  pub test.hello "world"

# 4. Test unauthorized subject (should fail)
nats --server=nats://nats.nats-system.svc.cluster.local:4222 \
  --token=$TOKEN \
  pub other-namespace.forbidden "fail"
# Expected: Permission Denied
```

## Troubleshooting

### Connection Issues

```bash
# Verify NATS server is accessible
kubectl run -it --rm debug --image=natsio/nats-box --restart=Never -- \
  nats server check --server=nats://nats.nats-system.svc.cluster.local:4222

# Check auth service logs
kubectl logs -n nats-auth -l app.kubernetes.io/name=nats-k8s-oidc-callout | tail -50
```

### Authentication Failures

```bash
# Decode token to verify claims
echo "TOKEN" | jwt decode -

# Verify token issuer and audience
# issuer should match jwt.issuer config (default: https://kubernetes.default.svc)
# audience should match jwt.audience config (default: nats)

# Check JWKS endpoint accessibility
kubectl exec -it -n nats-auth deployment/nats-k8s-oidc-callout -- \
  wget -O- https://kubernetes.default.svc/openid/v1/jwks
```

### Permission Denials

```bash
# Check ServiceAccount annotations
kubectl get serviceaccount my-nats-client -n my-app -o yaml

# Check auth service logs for denial reasons
kubectl logs -n nats-auth -l app.kubernetes.io/name=nats-k8s-oidc-callout | grep "denied"

# Common issues:
# 1. Missing annotations -> uses default namespace-only permissions
# 2. Invalid subject patterns -> check wildcard usage
# 3. Wrong namespace -> subjects don't match namespace
```

### Cache Issues

```bash
# Check cache metrics
kubectl port-forward -n nats-auth svc/nats-k8s-oidc-callout 8080:8080
curl -s http://localhost:8080/metrics | grep cache

# ServiceAccount not found in cache
# - Check RBAC permissions for ServiceAccount access
# - Verify ServiceAccount exists: kubectl get sa -n <namespace>
# - Check informer logs: kubectl logs -n nats-auth ... | grep informer
```

### NATS Server Issues

```bash
# Check NATS logs for auth callout
kubectl logs -n nats-system nats-0 | grep -i auth

# Verify auth callout is enabled
# Expected: [INF] Authorization callout enabled

# Check resolver is working
kubectl logs -n nats-system nats-0 | grep -i resolver

# Test with natsBox
kubectl exec -it -n nats-system deployment/nats-box -- \
  nats account info
```

## Advanced Topics

### Custom JWT Validation

For external JWKS endpoints or custom JWT validation:

```yaml
jwt:
  issuer: "https://my-oidc-provider.com"
  audience: "my-custom-audience"
  jwksUrl: "https://my-oidc-provider.com/.well-known/jwks.json"
```

### TLS Configuration

For NATS connections with TLS:

```yaml
nats:
  url: "nats://nats.nats-system.svc.cluster.local:4222"
  tls:
    enabled: true
    caFile: /etc/nats-tls/ca.crt
    certFile: /etc/nats-tls/tls.crt
    keyFile: /etc/nats-tls/tls.key
```

### Multi-Namespace Deployment

Deploy auth service in multiple namespaces for isolation:

```bash
# Deploy in namespace A
helm install nats-auth-a ./helm/nats-k8s-oidc-callout \
  --namespace namespace-a \
  --set nats.account="AUTH_ACCOUNT_A"

# Deploy in namespace B
helm install nats-auth-b ./helm/nats-k8s-oidc-callout \
  --namespace namespace-b \
  --set nats.account="AUTH_ACCOUNT_B"
```

## Optional Integrations

### NACK (JetStream Controller)

For declarative JetStream management, see [NACK_INTEGRATION.md](NACK_INTEGRATION.md).

**Quick Summary:**
- NACK manages JetStream resources (Streams, Consumers) via Kubernetes CRDs
- Uses separate account (`NACK`) with JetStream permissions
- Complementary to auth callout - no conflicts
- Optional - only needed for declarative JetStream management

## Further Reading

- [OPERATOR_SETUP.md](OPERATOR_SETUP.md) - Detailed NATS operator and account setup
- [NACK_INTEGRATION.md](NACK_INTEGRATION.md) - Optional JetStream controller integration
- [NATS Documentation](https://docs.nats.io/)
- [NATS Auth Callout Reference](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
- [Kubernetes Service Account Tokens](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection)
