# NATS Operator and Account Setup

Detailed guide for setting up NATS in operator mode with proper account hierarchy.

## Overview

NATS uses a **hierarchical JWT-based authentication model** with operators and accounts. This guide shows how to set up the recommended account structure for nats-k8s-oidc-callout.

## Prerequisites

- `nsc` CLI tool installed
- NATS server deployed (Helm chart recommended)
- `kubectl` configured for your cluster

## Installing NSC

```bash
# macOS
brew install nsc

# Linux
curl -L https://github.com/nats-io/nsc/releases/latest/download/nsc-linux-amd64.zip -o nsc.zip
unzip nsc.zip && sudo mv nsc /usr/local/bin/ && chmod +x /usr/local/bin/nsc

# Verify installation
nsc --version
```

## Account Architecture

### Recommended Structure

```
Operator (nats-system)
├── SYS Account (System Operations)
│   └── sys-user (User)
├── AUTH_SERVICE Account (Auth Callout Service)
│   └── auth-service (User)
├── AUTH_ACCOUNT Account (Validated Clients)
│   └── (Users assigned dynamically via auth callout)
└── NACK Account (Optional - JetStream Management)
    └── nack-controller (User)
```

### Account Roles

| Account | Purpose | Required | System Account |
|---------|---------|----------|----------------|
| **SYS** | NATS server internals, monitoring | ✅ Yes | ✅ Yes |
| **AUTH_SERVICE** | Auth callout service connection | ✅ Yes | ❌ No |
| **AUTH_ACCOUNT** | Validated application clients | ✅ Yes | ❌ No |
| **NACK** | JetStream management (optional) | ⚠️ Optional | ❌ No |

## Step-by-Step Setup

### 1. Initialize NSC and Create Operator

```bash
# Initialize NSC environment
nsc env

# Create operator (represents your NATS deployment)
nsc add operator nats-system

# Verify operator was created
nsc list operators
# Output: nats-system
```

**NSC Store Location:** By default, NSC stores all JWTs in `~/.nsc/nats/nats-system/`

### 2. Create SYS Account (System Operations)

```bash
# Create SYS account
nsc add account SYS

# Mark SYS as the system account
nsc edit operator --system-account SYS

# Create system user
nsc add user --account SYS sys-user

# Generate credentials
nsc generate creds --account SYS --name sys-user > sys-user.creds
```

### 3. Create AUTH_SERVICE Account (Auth Callout)

```bash
# Create AUTH_SERVICE account
nsc add account AUTH_SERVICE

# Create user for auth service
nsc add user --account AUTH_SERVICE auth-service

# Set permissions for auth callout
nsc edit user --account AUTH_SERVICE auth-service \
  --allow-pub '$SYS.REQ.USER.AUTH' \
  --allow-sub '_INBOX.>'

# Generate credentials
nsc generate creds --account AUTH_SERVICE --name auth-service > auth-service.creds
```

### 4. Create AUTH_ACCOUNT (Client Account)

```bash
# Create AUTH_ACCOUNT
nsc add account AUTH_ACCOUNT

# Note: No users created here - clients are assigned dynamically by auth callout
```

### 5. (Optional) Create NACK Account

Only create this if you plan to use NACK for JetStream management:

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

### 6. Verify All Accounts

```bash
# List all accounts
nsc list accounts
# Output:
# ┌──────────────┬─────────────────────────────────────┐
# │ Account      │ Description                         │
# ├──────────────┼─────────────────────────────────────┤
# │ SYS          │                                     │
# │ AUTH_SERVICE │                                     │
# │ AUTH_ACCOUNT │                                     │
# │ NACK         │ (if created)                        │
# └──────────────┴─────────────────────────────────────┘

# View detailed account information
nsc describe account SYS
nsc describe account AUTH_SERVICE
nsc describe account AUTH_ACCOUNT
```

## Exporting JWTs for NATS Server

### 1. Create JWT Directory

```bash
mkdir -p /tmp/nats-jwt
```

### 2. Export Operator JWT

```bash
# Export operator JWT (used by NATS server)
nsc describe operator --json | jq -r .jwt > /tmp/nats-jwt/operator.jwt
```

### 3. Export Account JWTs

```bash
# Export all account JWTs (used by JWT resolver)
nsc describe account SYS --json | jq -r .jwt > /tmp/nats-jwt/SYS.jwt
nsc describe account AUTH_SERVICE --json | jq -r .jwt > /tmp/nats-jwt/AUTH_SERVICE.jwt
nsc describe account AUTH_ACCOUNT --json | jq -r .jwt > /tmp/nats-jwt/AUTH_ACCOUNT.jwt

# Export NACK account JWT (only if you created it)
nsc describe account NACK --json | jq -r .jwt > /tmp/nats-jwt/NACK.jwt
```

### 4. Get Public Keys

```bash
# Get account public keys for NATS configuration
echo "=== Account Public Keys ==="
echo "SYS Account:"
nsc describe account SYS --json | jq -r .sub

echo "AUTH_SERVICE Account:"
nsc describe account AUTH_SERVICE --json | jq -r .sub

echo "AUTH_ACCOUNT Account:"
nsc describe account AUTH_ACCOUNT --json | jq -r .sub

echo ""
echo "=== User Public Keys ==="
echo "auth-service User:"
nsc describe user --account AUTH_SERVICE auth-service --json | jq -r .sub
```

## NATS Server Configuration

### Create Kubernetes Secrets

```bash
# Create namespace
kubectl create namespace nats-system

# Create Secrets with JWTs
kubectl create secret generic nats-operator \
  --namespace nats-system \
  --from-file=operator.jwt=/tmp/nats-jwt/operator.jwt

kubectl create secret generic nats-jwt \
  --namespace nats-system \
  --from-file=SYS.jwt=/tmp/nats-jwt/SYS.jwt \
  --from-file=AUTH_SERVICE.jwt=/tmp/nats-jwt/AUTH_SERVICE.jwt \
  --from-file=AUTH_ACCOUNT.jwt=/tmp/nats-jwt/AUTH_ACCOUNT.jwt
  # Add --from-file=NACK.jwt=/tmp/nats-jwt/NACK.jwt if using NACK
```

### Helm Values for NATS

```yaml
# nats-operator-values.yaml
config:
  cluster:
    enabled: true
  jetstream:
    enabled: true

  # Operator mode with JWT resolver
  resolver:
    enabled: true
    operator: /etc/nats-config/operator/operator.jwt
    systemAccount: SYS  # Use SYS account for system operations
    store:
      dir: /etc/nats-config/jwt
      size: 10Mi

  # Authorization callout configuration
  merge:
    authorization:
      auth_callout:
        # User public key that handles auth requests
        issuer: "UABC123XYZ..."  # Replace with auth-service user public key from step 4
        # Username from AUTH_SERVICE account
        auth_users: ["auth-service"]
        # Account where auth service connects
        account: "AUTH_SERVICE"
```

### Deploy NATS

```bash
# Add NATS Helm repository
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm repo update

# Install NATS with operator mode
helm install nats nats/nats \
  --namespace nats-system \
  --values nats-operator-values.yaml

# Verify deployment
kubectl logs -n nats-system nats-0 | grep -E "operator|resolver|system|account"
# Expected output:
# - Operator JWT loaded
# - System account: SYS
# - Resolver directory configured
# - Authorization callout enabled
```

## Security Best Practices

### 🔐 JWT Security

JWTs contain sensitive cryptographic material and **MUST be treated as secrets**:

- ❌ **NEVER** commit JWTs to git repositories
- ❌ **NEVER** store JWTs in plain text in Helm values files
- ✅ Use Kubernetes Secrets to store JWTs
- ✅ Use [helm-secrets](https://github.com/jkroepke/helm-secrets) to encrypt sensitive Helm values
- ✅ Use [sealed-secrets](https://github.com/bitnami-labs/sealed-secrets) or external secret managers
- ✅ Enable RBAC to restrict access to secrets
- ✅ Rotate JWTs periodically

### Using helm-secrets

```bash
# Install helm-secrets plugin
helm plugin install https://github.com/jkroepke/helm-secrets

# Create encrypted values file
cat > secrets.yaml <<EOF
config:
  merge:
    authorization:
      auth_callout:
        issuer: "UABC123XYZ..."
        auth_users: ["auth-service"]
EOF

# Encrypt the file
helm secrets enc secrets.yaml

# Use encrypted values in deployment
helm secrets upgrade nats nats/nats \
  --namespace nats-system \
  -f values.yaml \
  -f secrets.yaml
```

## Optional: Configure natsBox Contexts

The NATS Helm chart includes a `natsBox` pod for debugging. Configure persistent CLI contexts:

```yaml
# Add to nats-operator-values.yaml
natsBox:
  enabled: true

  # Configure contexts with credentials
  contexts:
    # System context (for administrative operations)
    system:
      creds:
        secretName: nats-sys-creds
        key: sys.creds
      merge:
        url: nats://nats.nats-system.svc.cluster.local:4222
        description: "System account for NATS administration"

    # Auth service context (for testing auth callout)
    auth-service:
      creds:
        secretName: nats-auth-service-creds
        key: auth-service.creds
      merge:
        url: nats://nats.nats-system.svc.cluster.local:4222
        description: "Auth service account"

  # Set default context - system account is recommended for admin tasks
  defaultContextName: system
```

**Create secrets for natsBox:**

```bash
# Create system credentials secret
kubectl create secret generic nats-sys-creds \
  --namespace nats-system \
  --from-file=sys.creds=./sys-user.creds

# Create auth service credentials secret
kubectl create secret generic nats-auth-service-creds \
  --namespace nats-system \
  --from-file=auth-service.creds=./auth-service.creds
```

**Using natsBox contexts:**

```bash
# Access natsBox pod
kubectl exec -it -n nats-system deployment/nats-box -- sh

# List available contexts
nats context ls

# Test connection
nats account info

# Switch contexts
nats context select auth-service
```

## Troubleshooting

### Error: "set an operator"

```bash
# Problem: No operator created yet
nsc describe operator --json

# Solution: Create operator first
nsc add operator nats-system
```

### Account or User Not Found

```bash
# List all accounts
nsc list accounts

# List users in an account
nsc list users --account AUTH_SERVICE

# If missing, recreate
nsc add account AUTH_SERVICE
nsc add user --account AUTH_SERVICE auth-service
```

### Credential File Issues

```bash
# Regenerate credentials
nsc generate creds --account AUTH_SERVICE --name auth-service > auth-service.creds

# Verify credentials file format (should have JWT + NKey seed)
cat auth-service.creds | head -20

# Test credentials with NATS CLI
nats --creds=auth-service.creds server check
```

### Wrong System Account

```bash
# Verify which account is marked as system account
nsc describe operator | grep "System Account"

# If not marked, set system account on operator
nsc edit operator --system-account SYS
```

### JWT Resolver Not Finding Accounts

```bash
# Verify JWT files exist
ls -la /tmp/nats-jwt/

# Check JWT file contents are valid
cat /tmp/nats-jwt/AUTH_SERVICE.jwt | head -1

# Re-export if needed
nsc describe account AUTH_SERVICE --json | jq -r .jwt > /tmp/nats-jwt/AUTH_SERVICE.jwt
```

## Further Reading

- [NATS Security Documentation](https://docs.nats.io/running-a-nats-service/configuration/securing_nats)
- [NSC CLI Reference](https://docs.nats.io/using-nats/nats-tools/nsc)
- [NATS JWT Authentication](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/jwt)
- [NACK Integration](NACK_INTEGRATION.md) (optional)
