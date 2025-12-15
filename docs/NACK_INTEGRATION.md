# NACK Integration with nats-k8s-oidc-callout

Guide for integrating NACK (NATS JetStream Controller) with the auth callout service.

**Note:** This integration is **optional**. NACK is only needed if you want to manage JetStream resources (Streams, Consumers, KeyValue stores) declaratively via Kubernetes CRDs. If you plan to manage JetStream resources programmatically or via the NATS CLI, you can skip this guide.

## Architecture Overview

[NACK](https://github.com/nats-io/nack) is a Kubernetes operator that manages NATS JetStream resources through Custom Resource Definitions. NACK and nats-k8s-oidc-callout are **complementary** and work together seamlessly.

```
┌─────────────────────────────────────────────┐
│ NATS Server                                 │
│ ├─ Account: NACK (NACK's credentials)       │
│ ├─ Account: DEFAULT (auth callout enabled)  │
└─────────────────────────────────────────────┘
         ↑                    ↑
         │                    │
    NACK Controller      Your Applications
    (uses JWT creds)     (use K8s SA JWTs + callout)
```

**Key Points:**
- **NACK** manages JetStream infrastructure using its own account credentials
- **nats-k8s-oidc-callout** validates client applications using service account JWTs
- Both can run simultaneously with **zero conflicts**

## Understanding the Account Model

**Quick Summary:**
- ✅ Use **ONE operator** for your deployment (simplest model)
- ✅ Create **FOUR accounts**: SYS (system), AUTH_SERVICE (callout), AUTH_ACCOUNT (clients), NACK (jetstream mgmt)
- ✅ **SYS is an account** (not a user) - it's the system account for NATS internals
- ✅ **NACK is NOT a system component** - it's a regular app that manages JetStream
- ✅ All accounts are signed by the same operator, providing security isolation
- 🔐 **JWTs are secrets** - never commit to git, use helm-secrets or secret managers

### Account Hierarchy

NATS uses a **hierarchical JWT-based authentication model**:

```
Operator (nats-system)
├── SYS Account (System Operations)
│   └── sys-user (User)
├── AUTH_SERVICE Account (Auth Callout Service)
│   └── auth-service (User)
├── AUTH_ACCOUNT Account (Validated Clients)
│   └── (Users assigned dynamically via auth callout)
└── NACK Account (JetStream Management)
    └── nack-controller (User)
```

**Key Concepts:**
- **Operator**: Top-level entity representing a security domain; signs and manages accounts
- **Account**: Security boundary for a group of users; defines resource limits and permissions
- **User**: Individual identity within an account; has specific publish/subscribe permissions

### Account Roles

| Account | Purpose | Type | Users |
|---------|---------|------|-------|
| **SYS** | NATS system operations, monitoring, server-to-server communication | System Account | sys-user (system admin) |
| **AUTH_SERVICE** | Auth callout service connects here to handle auth requests | Application Account | auth-service (callout service) |
| **AUTH_ACCOUNT** | Validated application clients are assigned here | Application Account | (assigned dynamically) |
| **NACK** | NACK controller manages JetStream resources | Application Account | nack-controller (NACK app) |

**Important Notes:**
- **SYS is a system account**, not a system user - it's for NATS server internals
- **NACK is NOT a system component** - it's a regular application that happens to manage infrastructure
- Each account provides security isolation and separate permission boundaries

## Prerequisites

- NATS server deployed with operator mode (see [OPERATOR_SETUP.md](OPERATOR_SETUP.md))
- nats-k8s-oidc-callout deployed and running
- `nsc` CLI tool installed
- `kubectl` configured

## Setup Steps

### 1. Create NACK Account and User

If you haven't already created the NACK account during NATS setup:

```bash
# Create NACK account
nsc add account NACK

# Set JetStream resource limits (unlimited)
nsc edit account NACK \
  --js-mem-storage -1 \
  --js-disk-storage -1 \
  --js-streams -1 \
  --js-consumer -1

# Create user for NACK controller
nsc add user --account NACK nack-controller

# Grant JetStream management permissions
nsc edit user --account NACK nack-controller \
  --allow-pub '$JS.>' \
  --allow-pub '_INBOX.>' \
  --allow-sub '$JS.>' \
  --allow-sub '_INBOX.>'

# Generate credentials
nsc generate creds --account NACK --name nack-controller > nack-controller.creds
```

### 2. Export NACK Account JWT

```bash
# Export NACK account JWT for NATS server
nsc describe account NACK --json | jq -r .jwt > nack-account.jwt

# Get account public key for reference
nsc describe account NACK --json | jq -r .sub
```

### 3. Update NATS Server Configuration

Add the NACK account JWT to your NATS server configuration:

```bash
# Create/update Secret with NACK JWT
kubectl create secret generic nats-jwt \
  --namespace nats-system \
  --from-file=SYS.jwt \
  --from-file=AUTH_SERVICE.jwt \
  --from-file=AUTH_ACCOUNT.jwt \
  --from-file=NACK.jwt=nack-account.jwt \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart NATS pods to load new JWT
kubectl rollout restart statefulset/nats -n nats-system
```

### 4. Deploy NACK Controller

```bash
# Add NACK Helm repository
helm repo add nack https://nats-io.github.io/k8s/helm/charts/
helm repo update

# Create namespace
kubectl create namespace nack-system

# Create secret with NACK credentials
kubectl create secret generic nack-nats-creds \
  --namespace nack-system \
  --from-file=nack.creds=./nack-controller.creds

# Install NACK
helm install nack nack/nack \
  --namespace nack-system \
  --set jetstream.nats.url=nats://nats.nats-system.svc.cluster.local:4222 \
  --set jetstream.nats.credentialsSecret=nack-nats-creds
```

### 5. Verify NACK Deployment

```bash
# Check NACK controller
kubectl get pods -n nack-system
kubectl logs -n nack-system -l app=nack

# Verify NACK can connect to NATS
kubectl logs -n nack-system -l app=nack | grep -i connected
```

## Using NACK

### Create JetStream Resources

Now you can create Streams and Consumers using Kubernetes CRDs:

```yaml
# example-stream.yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Stream
metadata:
  name: events
  namespace: default
spec:
  name: events
  subjects:
    - "events.>"
  storage: file
  replicas: 3
  maxAge: 24h
---
apiVersion: jetstream.nats.io/v1beta2
kind: Consumer
metadata:
  name: events-processor
  namespace: default
spec:
  streamName: events
  durableName: processor
  deliverPolicy: all
  ackPolicy: explicit
  ackWait: 30s
  maxDeliver: 5
```

Apply:
```bash
kubectl apply -f example-stream.yaml
```

### Verify JetStream Resources

```bash
# List streams and consumers
kubectl get streams,consumers -A

# Get stream details
kubectl describe stream events

# Check stream status in NATS
kubectl exec -n nats-system nats-box -- nats stream info events
```

## Optional: NACK Account CRD

You can also manage NACK connections via Account CRDs:

```yaml
# nack-account.yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Account
metadata:
  name: nack-account
  namespace: nack-system
spec:
  servers:
    - "nats://nats.nats-system.svc.cluster.local:4222"
  creds:
    secret:
      name: nack-nats-creds
      key: nack.creds
```

Apply:
```bash
kubectl apply -f nack-account.yaml
```

## Testing Integration

Test that both NACK and auth callout work together:

```bash
# 1. Create a stream via NACK
kubectl apply -f example-stream.yaml

# 2. Test client access with service account token
kubectl create serviceaccount test-client -n default
kubectl annotate serviceaccount test-client -n default \
  nats.io/allowed-pub-subjects="events.>" \
  nats.io/allowed-sub-subjects="events.>"

# 3. Create token and test
TOKEN=$(kubectl create token test-client -n default --audience=nats)
nats --server=nats://nats.nats-system.svc.cluster.local:4222 \
  --token=$TOKEN \
  pub events.test "hello from client"

# 4. Verify message was published
kubectl exec -n nats-system nats-box -- \
  nats stream info events --json | jq .state.messages
```

## Comparison: NACK vs. Auth Callout

| Feature | NACK | nats-k8s-oidc-callout |
|---------|------|------------------------|
| **Purpose** | Manage JetStream resources | Authenticate client applications |
| **Scope** | Infrastructure management | Authorization enforcement |
| **Authentication** | Uses dedicated account credentials | Validates Kubernetes service account JWTs |
| **Resources Managed** | Streams, Consumers, KeyValue, ObjectStore | Client permissions |
| **Conflict with Auth Callout?** | ✅ No - Complementary | N/A |

## Troubleshooting

### NACK Controller Not Starting

```bash
# Check logs
kubectl logs -n nack-system -l app=nack

# Common issues:
# 1. Credentials secret not found
kubectl get secret nack-nats-creds -n nack-system

# 2. NATS server not reachable
kubectl run -it --rm debug --image=natsio/nats-box --restart=Never -- \
  nats server check --server=nats://nats.nats-system.svc.cluster.local:4222

# 3. Account JWT not loaded by NATS
kubectl logs -n nats-system nats-0 | grep -i "NACK"
```

### Stream Creation Fails

```bash
# Check stream status
kubectl describe stream events

# Check NACK controller logs
kubectl logs -n nack-system -l app=nack | tail -50

# Verify NACK account has JetStream enabled
nsc describe account NACK | grep -A 5 "JetStream"
```

### Permission Denied Errors

```bash
# Verify NACK user permissions
nsc describe user --account NACK nack-controller

# Should show:
# Pub Allow: $JS.>, _INBOX.>
# Sub Allow: $JS.>, _INBOX.>

# Re-apply permissions if needed
nsc edit user --account NACK nack-controller \
  --allow-pub '$JS.>' --allow-pub '_INBOX.>' \
  --allow-sub '$JS.>' --allow-sub '_INBOX.>'

# Push updated JWT to NATS
nsc describe account NACK --json | jq -r .jwt > nack-account.jwt
kubectl create secret generic nats-jwt --from-file=NACK.jwt=nack-account.jwt \
  -n nats-system --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart statefulset/nats -n nats-system
```

## Best Practices

1. **Separate Accounts**: Use dedicated NACK account for infrastructure management
2. **Least Privilege**: Grant NACK only JetStream management permissions
3. **Credential Rotation**: Regularly rotate NACK credentials
4. **Monitoring**: Track both NACK operations and auth callout metrics
5. **Resource Naming**: Use clear namespaces for NACK-managed resources
6. **JetStream Limits**: Set appropriate resource limits based on usage patterns
7. **High Availability**: Run multiple NACK controller replicas for production

## Security Considerations

- **Store credentials securely**: Use Kubernetes Secrets, never commit to git
- **Rotate credentials**: Periodically rotate NACK user credentials
- **Monitor access**: Track NACK operations through NATS monitoring
- **Limit permissions**: NACK should only have JetStream management permissions
- **Network policies**: Restrict NACK controller network access to NATS only

## Further Reading

- [NACK GitHub Repository](https://github.com/nats-io/nack)
- [NATS JetStream Documentation](https://docs.nats.io/nats-concepts/jetstream)
- [NATS Operator and Account Setup](OPERATOR_SETUP.md)
- [NATS Security Best Practices](https://docs.nats.io/running-a-nats-service/configuration/securing_nats)
