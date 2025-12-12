# Deploying nats-k8s-oidc-callout

Guide for deploying the NATS Kubernetes OIDC auth callout service.

## Prerequisites

- Kubernetes cluster (1.20+)
- kubectl configured
- nsc (NATS CLI tools)
- NATS server (2.9+) with auth callout support

## Deployment Steps

### 1. Install NATS Server

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm repo update

kubectl create namespace nats

helm install nats nats/nats \
  --namespace nats \
  --set config.cluster.enabled=true \
  --set config.jetstream.enabled=true
```

### 2. Generate NATS Credentials

```bash
# Create operator and accounts
nsc add operator --name auth-operator
nsc add account --name AUTH_SERVICE
nsc add user --account AUTH_SERVICE --name auth-service
nsc add account --name AUTH_ACCOUNT

# Generate credentials
nsc generate creds --account AUTH_SERVICE --user auth-service > auth-service.creds

# Extract the user's public NKEY (needed for NATS config)
nsc describe user --account AUTH_SERVICE auth-service | grep "Issuer Key" | awk '{print $3}'
# Output example: UAABC...XYZ (save this for step 3)

# Create Kubernetes secret
kubectl create namespace nats-auth
kubectl create secret generic nats-auth-creds \
  --from-file=credentials=auth-service.creds \
  --namespace=nats-auth
```

### 3. Configure NATS Server for Auth Callout

Update your NATS Helm deployment to enable the authorization callout.

**Note**: The auth service will connect using the credentials file (nkey-based authentication). The NATS server needs to know which user's public key to trust for signing authorization responses.

```yaml
# nats-auth-values.yaml
config:
  cluster:
    enabled: true
  jetstream:
    enabled: true

  # Merge custom authorization configuration
  merge:
    authorization:
      # Define the auth service user with nkey authentication
      users:
        - nkey: "UAABC...XYZ"  # Public NKEY from step 2

      # Auth callout configuration
      auth_callout:
        # Public NKEY that signs authorization responses (same as above)
        issuer: "UAABC...XYZ"
        # Users authorized to handle auth requests (reference by nkey)
        auth_users: ["UAABC...XYZ"]
```

Apply the configuration:

```bash
# Upgrade NATS with auth callout enabled
helm upgrade nats nats/nats \
  --namespace nats \
  --values nats-auth-values.yaml
```

**Alternative: Simple Password Authentication (Testing Only)**

For testing/development, you can use simple password authentication like the E2E tests:

```yaml
# nats-auth-simple.yaml
config:
  cluster:
    enabled: true
  jetstream:
    enabled: true

  merge:
    authorization:
      # Simple password authentication (testing only)
      users:
        - user: "auth-service"
          password: "auth-service-pass"

      auth_callout:
        # Public NKEY from step 2 that signs authorization responses
        issuer: "UAABC...XYZ"
        auth_users: ["auth-service"]
```

Then connect with: `nats://auth-service:auth-service-pass@nats.nats.svc:4222`

#### With Operator/Resolver Mode (Recommended for Production)

For operator-based JWT authentication:

```yaml
# nats-operator-values.yaml
config:
  cluster:
    enabled: true
  jetstream:
    enabled: true

  # Resolver for operator mode
  resolver:
    enabled: true
    operator: /etc/nats-config/operator/operator.jwt
    systemAccount: AUTH_SERVICE

  # Authorization callout
  merge:
    authorization:
      auth_callout:
        # Public NKEY of auth-service user
        issuer: "UAABC...XYZ"
        # Auth service users from AUTH_SERVICE account
        auth_users: ["auth-service"]
        # Reference the AUTH_SERVICE account
        account: "AUTH_SERVICE"

# Mount operator JWT
configMap:
  operator:
    operator.jwt: |
      -----BEGIN NATS OPERATOR JWT-----
      <your operator JWT from nsc describe operator -J>
      -----END NATS OPERATOR JWT-----
```

Apply:
```bash
helm upgrade nats nats/nats \
  --namespace nats \
  --values nats-operator-values.yaml
```

#### Verify NATS Configuration

```bash
# Check NATS server logs for auth callout
kubectl logs -n nats nats-0 | grep -i auth

# Expected output:
# [INF] Authorization callout enabled
# [INF] Authorization users configured: 1
```

### 4. Configure Kubernetes RBAC

```yaml
# rbac.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nats-auth-callout
  namespace: nats-auth
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: nats-auth-callout
rules:
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: nats-auth-callout
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: nats-auth-callout
subjects:
  - kind: ServiceAccount
    name: nats-auth-callout
    namespace: nats-auth
```

Apply:
```bash
kubectl apply -f rbac.yaml
```

### 5. Deploy Auth Service

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nats-auth-callout
  namespace: nats-auth
spec:
  replicas: 2
  selector:
    matchLabels:
      app: nats-auth-callout
  template:
    metadata:
      labels:
        app: nats-auth-callout
    spec:
      serviceAccountName: nats-auth-callout
      containers:
        - name: nats-auth-callout
          image: nats-k8s-oidc-callout:latest
          ports:
            - containerPort: 8080
              name: http
          env:
            - name: NATS_URL
              value: "nats://nats.nats.svc.cluster.local:4222"
            - name: NATS_CREDS_FILE
              value: "/etc/nats/credentials"
            - name: NATS_ACCOUNT
              value: "AUTH_ACCOUNT"
            - name: K8S_IN_CLUSTER
              value: "true"
            - name: JWT_AUDIENCE
              value: "nats"
            - name: LOG_LEVEL
              value: "info"
          volumeMounts:
            - name: nats-creds
              mountPath: /etc/nats
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 256Mi
      volumes:
        - name: nats-creds
          secret:
            secretName: nats-auth-creds
            items:
              - key: credentials
                path: credentials
---
apiVersion: v1
kind: Service
metadata:
  name: nats-auth-callout
  namespace: nats-auth
spec:
  type: ClusterIP
  ports:
    - port: 8080
      targetPort: http
      name: http
  selector:
    app: nats-auth-callout
```

Apply:
```bash
kubectl apply -f deployment.yaml
```

### 6. Configure Client ServiceAccount

```yaml
# client-sa.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-nats-client
  namespace: my-app
  annotations:
    nats.io/allowed-pub-subjects: "my-app.requests.>,my-app.events.>"
    nats.io/allowed-sub-subjects: "my-app.responses.>,my-app.commands.>"
```

Apply:
```bash
kubectl apply -f client-sa.yaml
```

## Integration with NACK (JetStream Controller) - Optional

**Note:** This section is **optional**. NACK is only needed if you want to manage JetStream resources (Streams, Consumers, KeyValue stores) declaratively via Kubernetes CRDs. If you plan to manage JetStream resources programmatically or via the NATS CLI, you can skip this section.

[NACK](https://github.com/nats-io/nack) is a Kubernetes operator that manages NATS JetStream resources through Custom Resource Definitions. NACK and nats-k8s-oidc-callout are **complementary** and work together seamlessly.

### Architecture Overview

```
┌─────────────────────────────────────────────┐
│ NATS Server                                 │
│ ├─ Account: NACK (NACK's credentials)      │
│ ├─ Account: DEFAULT (auth callout enabled) │
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

### Understanding NATS Operators and Accounts

**TL;DR:**
- ✅ **NACK integration is OPTIONAL** - only needed for declarative JetStream management
- ✅ Use **ONE operator** for your deployment (simplest model)
- ✅ Create **THREE or FOUR accounts**: SYS (system), AUTH_SERVICE (callout), AUTH_ACCOUNT (clients), and optionally NACK (jetstream mgmt)
- ✅ **SYS is an account** (not a user) - it's the system account for NATS internals
- ✅ **NACK is NOT a system component** - it's a regular app that manages JetStream
- ✅ All accounts are signed by the same operator, providing security isolation
- 🔐 **JWTs are secrets** - never commit to git, use helm-secrets or secret managers

#### Operator and Account Hierarchy

NATS uses a **hierarchical JWT-based authentication model**:

```
Operator (Security Domain)
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

**Why One Operator?**

Use a **single operator** for your deployment because:
- ✅ Simplest deployment model (NATS best practice: pick the simplest model that works)
- ✅ Single security domain (same organization/cluster)
- ✅ Easier to manage and maintain
- ✅ Accounts provide sufficient isolation between NACK and auth callout

**When to use multiple operators:** Only when you have separate security domains (different organizations, business units, or multi-tenancy requirements).

#### Account Roles Explained

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

### Setting Up NACK with NSC

#### 0. Initialize NSC Environment

Before creating accounts, initialize NSC and create the operator:

```bash
# Install NSC (if not already installed)
# macOS
brew install nsc

# Linux
curl -L https://github.com/nats-io/nsc/releases/latest/download/nsc-linux-amd64.zip -o nsc.zip
unzip nsc.zip && sudo mv nsc /usr/local/bin/ && chmod +x /usr/local/bin/nsc

# Verify installation
nsc --version

# Initialize NSC environment (creates ~/.nsc directory)
nsc env

# Create operator (represents your NATS deployment/security domain)
nsc add operator nats-system

# Verify operator was created
nsc list operators
# Output: nats-system
```

**NSC Store Location:** By default, NSC stores all JWTs in `~/.nsc/nats/nats-system/`

#### 1. Create All Accounts and Users

Create accounts for each component in your deployment:

```bash
# ============================================
# SYS Account (System Operations)
# ============================================
nsc add account SYS
nsc edit account SYS --sk system  # Mark as system account
nsc add user --account SYS sys-user
nsc generate creds --account SYS --name sys-user > sys-user.creds

# ============================================
# AUTH_SERVICE Account (Auth Callout Service)
# ============================================
nsc add account AUTH_SERVICE
nsc add user --account AUTH_SERVICE auth-service

# Set permissions for auth callout
nsc edit user --account AUTH_SERVICE auth-service \
  --allow-pub '$SYS.REQ.USER.AUTH' \
  --allow-sub '_INBOX.>'

nsc generate creds --account AUTH_SERVICE --name auth-service > auth-service.creds

# ============================================
# AUTH_ACCOUNT Account (Validated Clients)
# ============================================
nsc add account AUTH_ACCOUNT
# Note: No users created here - clients are assigned dynamically by auth callout

# ============================================
# NACK Account (JetStream Management) - OPTIONAL
# ============================================
# Only create this if you plan to use NACK for declarative JetStream management

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

nsc generate creds --account NACK --name nack-controller > nack-controller.creds

# ============================================
# Verify All Accounts
# ============================================
nsc list accounts
# Output:
# ┌──────────────┬─────────────────────────────────────┐
# │ Account      │ Description                         │
# ├──────────────┼─────────────────────────────────────┤
# │ SYS          │                                     │
# │ AUTH_SERVICE │                                     │
# │ AUTH_ACCOUNT │                                     │
# │ NACK         │                                     │
# └──────────────┴─────────────────────────────────────┘

# View detailed account information
nsc describe account SYS
nsc describe account AUTH_SERVICE
nsc describe account AUTH_ACCOUNT
nsc describe account NACK
```

#### 2. Export JWTs for NATS Server

Export all JWTs needed by the NATS server:

```bash
# Create JWT directory
mkdir -p /tmp/nats-jwt

# Export operator JWT (used by NATS server)
nsc describe operator --json | jq -r .jwt > /tmp/nats-jwt/operator.jwt

# Export all account JWTs (used by JWT resolver)
nsc describe account SYS --json | jq -r .jwt > /tmp/nats-jwt/SYS.jwt
nsc describe account AUTH_SERVICE --json | jq -r .jwt > /tmp/nats-jwt/AUTH_SERVICE.jwt
nsc describe account AUTH_ACCOUNT --json | jq -r .jwt > /tmp/nats-jwt/AUTH_ACCOUNT.jwt

# Export NACK account JWT (only if you created the NACK account)
nsc describe account NACK --json | jq -r .jwt > /tmp/nats-jwt/NACK.jwt

# Get public keys for NATS server configuration
echo "=== Account Public Keys ==="
echo "SYS Account:"
nsc describe account SYS --json | jq -r .sub

echo "AUTH_SERVICE Account:"
nsc describe account AUTH_SERVICE --json | jq -r .sub

echo "AUTH_ACCOUNT Account:"
nsc describe account AUTH_ACCOUNT --json | jq -r .sub

echo "NACK Account:"
nsc describe account NACK --json | jq -r .sub

echo ""
echo "=== User Public Keys ==="
echo "auth-service User:"
nsc describe user --account AUTH_SERVICE auth-service --json | jq -r .sub
```

**🔐 Security Warning: JWTs Are Secrets**

JWTs contain sensitive cryptographic material and **MUST be treated as secrets**:
- ❌ **NEVER** commit JWTs to git repositories
- ❌ **NEVER** store JWTs in plain text in Helm values files
- ✅ Use Kubernetes Secrets to store JWTs
- ✅ Use [helm-secrets](https://github.com/jkroepke/helm-secrets) to encrypt sensitive Helm values
- ✅ Use [sealed-secrets](https://github.com/bitnami-labs/sealed-secrets) or external secret managers (AWS Secrets Manager, Vault, etc.)
- ✅ Enable RBAC to restrict access to secrets
- ✅ Rotate JWTs periodically

**Recommended: Using helm-secrets**

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

configMap:
  operator:
    operator.jwt: |
      <operator JWT>
  jwt:
    SYS.jwt: |
      <SYS account JWT>
    AUTH_SERVICE.jwt: |
      <AUTH_SERVICE account JWT>
    # ... other JWTs
EOF

# Encrypt the file
helm secrets enc secrets.yaml

# Use encrypted values in deployment
helm secrets upgrade nats nats/nats \
  --namespace nats \
  -f values.yaml \
  -f secrets.yaml
```

#### 3. Configure NATS Server for All Accounts

Update NATS Helm values to support operator mode with all accounts:

```yaml
# nats-values-with-nack.yaml
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
        # User public key that handles auth requests (from step 2)
        issuer: "UABC123XYZ..."  # auth-service user public key
        # Username from AUTH_SERVICE account
        auth_users: ["auth-service"]
        # Account where auth service connects
        account: "AUTH_SERVICE"

# Mount operator and account JWTs
configMap:
  operator:
    operator.jwt: |
      -----BEGIN NATS OPERATOR JWT-----
      <paste operator JWT from /tmp/nats-jwt/operator.jwt>
      -----END NATS OPERATOR JWT-----

  jwt:
    SYS.jwt: |
      <paste SYS account JWT from /tmp/nats-jwt/SYS.jwt>
    AUTH_SERVICE.jwt: |
      <paste AUTH_SERVICE account JWT>
    AUTH_ACCOUNT.jwt: |
      <paste AUTH_ACCOUNT account JWT>
    # NACK.jwt - Only include if using NACK
    # NACK.jwt: |
    #   <paste NACK account JWT>
```

**Important:** Store these JWTs securely using helm-secrets, sealed-secrets, or an external secret manager. See the security warning above for details.

**Account Configuration Summary:**

| Account | Used By | System Account? | JetStream? | Required? |
|---------|---------|-----------------|------------|-----------|
| **SYS** | NATS server internals | ✅ Yes | Optional | ✅ Required |
| **AUTH_SERVICE** | nats-k8s-oidc-callout | ❌ No | Optional | ✅ Required |
| **AUTH_ACCOUNT** | Validated application clients | ❌ No | ✅ Yes | ✅ Required |
| **NACK** | NACK controller | ❌ No | ✅ Yes | ⚠️ Optional |

Apply the configuration:

```bash
# Create ConfigMaps with required account JWTs
kubectl create configmap nats-jwt \
  --namespace nats \
  --from-file=/tmp/nats-jwt/SYS.jwt \
  --from-file=/tmp/nats-jwt/AUTH_SERVICE.jwt \
  --from-file=/tmp/nats-jwt/AUTH_ACCOUNT.jwt
  # Add NACK JWT if using NACK:
  # --from-file=/tmp/nats-jwt/NACK.jwt

kubectl create configmap nats-operator \
  --namespace nats \
  --from-file=/tmp/nats-jwt/operator.jwt

# Upgrade NATS with new configuration
helm upgrade nats nats/nats \
  --namespace nats \
  --values nats-values-with-nack.yaml

# Verify NATS server configuration
kubectl logs -n nats nats-0 | grep -E "operator|resolver|system|account"
# Expected output should show:
# - Operator JWT loaded
# - System account: SYS
# - Resolver directory configured
# - Authorization callout enabled
```

#### 3a. Configure natsBox Contexts (Optional but Recommended)

The NATS Helm chart includes a `natsBox` pod with the NATS CLI pre-installed. You can configure persistent CLI contexts for easy debugging and testing:

```yaml
# Add to your nats-values-with-nack.yaml
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
        description: "System account for NATS administration"

    # Auth service context (for testing auth callout)
    auth-service:
      creds:
        secretName: nats-auth-service-creds
        key: auth-service.creds
      merge:
        description: "Auth service account"

    # NACK context (for JetStream management)
    nack:
      creds:
        secretName: nack-nats-creds
        key: nack.creds
      merge:
        description: "NACK controller account"

  # Set default context
  defaultContextName: system
```

**Create secrets for natsBox contexts:**

```bash
# Create system credentials secret
kubectl create secret generic nats-sys-creds \
  --namespace nats \
  --from-file=sys.creds=./sys-user.creds

# Create auth service credentials secret
kubectl create secret generic nats-auth-service-creds \
  --namespace nats \
  --from-file=auth-service.creds=./auth-service.creds

# Create NACK credentials secret (if using NACK)
kubectl create secret generic nack-nats-creds \
  --namespace nats \
  --from-file=nack.creds=./nack-controller.creds
```

**Using natsBox contexts:**

```bash
# Access natsBox pod
kubectl exec -it -n nats deployment/nats-box -- sh

# List available contexts
nats context ls

# Select a context
nats context select system

# Test connection
nats account info

# Switch to NACK context
nats context select nack

# List JetStream streams
nats stream ls

# Test with auth service context
nats context select auth-service
nats pub '$SYS.REQ.USER.AUTH' '{"user_nkey":"UABC...XYZ"}'
```

**Benefits of natsBox contexts:**
- ✅ Pre-configured connections persist across pod restarts
- ✅ Easy switching between accounts for testing
- ✅ No need to remember connection strings or credential paths
- ✅ Useful for debugging auth callout and JetStream operations

#### 4. Deploy NACK Controller

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
  --set jetstream.nats.url=nats://nats.nats.svc.cluster.local:4222 \
  --set jetstream.nats.credentialsSecret=nack-nats-creds
```

#### 5. Create NACK Account CRD (Alternative Configuration)

If you want to manage NACK connections via Account CRDs:

```yaml
# nack-account.yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Account
metadata:
  name: nack-account
  namespace: nack-system
spec:
  servers:
    - "nats://nats.nats.svc.cluster.local:4222"
  creds:
    secret:
      name: nack-nats-creds
      key: nack.creds
```

Apply:
```bash
kubectl apply -f nack-account.yaml
```

#### 6. Create JetStream Resources

Now you can create Streams and Consumers using NACK:

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
  # Optional: reference specific account
  # account: nack-account
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

### Verification

```bash
# Check NACK controller
kubectl get pods -n nack-system
kubectl logs -n nack-system -l app=nack

# Verify JetStream resources
kubectl get streams,consumers -A

# Test with NATS CLI (using service account token)
kubectl create token my-nats-client \
  --namespace=my-app \
  --audience=nats > test-token.txt

nats --server=nats://nats.nats.svc.cluster.local:4222 \
  --token=$(cat test-token.txt) \
  stream info events
```

### Comparison: NACK vs. Auth Callout

| Feature | NACK | nats-k8s-oidc-callout |
|---------|------|------------------------|
| **Purpose** | Manage JetStream resources | Authenticate client applications |
| **Scope** | Infrastructure management | Authorization enforcement |
| **Authentication** | Uses dedicated account credentials | Validates Kubernetes service account JWTs |
| **Resources Managed** | Streams, Consumers, KeyValue, ObjectStore | Client permissions |
| **Conflict with Auth Callout?** | ✅ No - Complementary | N/A |

### Best Practices

1. **Separate Accounts**: Use dedicated NACK account for infrastructure management
2. **Least Privilege**: Grant NACK only JetStream management permissions
3. **Credential Rotation**: Regularly rotate NACK credentials
4. **Monitoring**: Track both NACK operations and auth callout metrics
5. **Resource Naming**: Use clear namespaces for NACK-managed resources
6. **System Account**: Always use SYS as the system account, not application accounts
7. **Operator Management**: Keep operator private keys secure; use signing keys for production

### Common NSC Troubleshooting

#### Error: "set an operator"

```bash
# Problem: No operator created yet
nsc describe operator --json

# Solution: Create operator first
nsc add operator MyCluster
```

#### Error: Account or user not found

```bash
# List all accounts
nsc list accounts

# List users in an account
nsc list users --account NACK

# If missing, recreate
nsc add account NACK
nsc add user --account NACK nack-controller
```

#### Credential file issues

```bash
# Regenerate credentials
nsc generate creds --account NACK --name nack-controller > nack-controller.creds

# Verify credentials file format (should have JWT + NKey seed)
cat nack-controller.creds | head -20

# Test credentials with NATS CLI
nats --creds=nack-controller.creds server check
```

#### Wrong system account configured

```bash
# Verify which account is marked as system account
nsc describe account SYS | grep "Signing Keys"

# If not marked, add system flag
nsc edit account SYS --sk system
```

#### JWT resolver not finding accounts

```bash
# Verify JWT files exist
ls -la /tmp/nats-jwt/

# Check JWT file contents are valid
cat /tmp/nats-jwt/NACK.jwt | head -1

# Re-export if needed
nsc describe account NACK --json | jq -r .jwt > /tmp/nats-jwt/NACK.jwt
```

## Verification

### Check Deployment

```bash
# Check pods
kubectl get pods -n nats-auth

# Check logs
kubectl logs -n nats-auth -l app=nats-auth-callout

# Check health
kubectl port-forward -n nats-auth svc/nats-auth-callout 8080:8080
curl http://localhost:8080/healthz
```

### Test Authentication

```bash
# Create test token
kubectl create token my-nats-client \
  --namespace=my-app \
  --audience=nats \
  --duration=1h > test-token.txt

# Test with NATS CLI
nats --server=nats://nats.nats.svc.cluster.local:4222 \
  --token=$(cat test-token.txt) \
  pub my-app.test "hello"
```

### View Metrics

```bash
kubectl port-forward -n nats-auth svc/nats-auth-callout 8080:8080
curl http://localhost:8080/metrics
```

## Troubleshooting

### Connection Issues

```bash
# Verify NATS server is accessible
kubectl run -it --rm debug --image=natsio/nats-box --restart=Never \
  -- nats server check --server=nats://nats.nats.svc.cluster.local:4222
```

### Token Validation Failures

```bash
# Decode token
echo "TOKEN" | jwt decode -

# Verify issuer and audience match configuration
```

### Permission Denials

```bash
# Check ServiceAccount annotations
kubectl get serviceaccount my-nats-client -n my-app -o yaml

# Check auth service logs
kubectl logs -n nats-auth -l app=nats-auth-callout | grep "permission"
```

### JWKS Errors

```bash
# Verify JWKS endpoint is accessible
kubectl exec -it -n nats-auth <pod-name> -- \
  wget -O- https://kubernetes.default.svc/openid/v1/jwks
```

## Configuration Reference

### Required Environment Variables
- `NATS_CREDS_FILE` - Path to NATS credentials
- `NATS_ACCOUNT` - NATS account to assign clients

### Optional (with smart defaults)
- `NATS_URL` - Default: `nats://nats:4222`
- `JWKS_URL` - Default: `https://kubernetes.default.svc/openid/v1/jwks`
- `JWT_ISSUER` - Default: `https://kubernetes.default.svc`
- `JWT_AUDIENCE` - Default: `nats`
- `LOG_LEVEL` - Default: `info`

### ServiceAccount Annotations
- `nats.io/allowed-pub-subjects` - Comma-separated publish subjects
- `nats.io/allowed-sub-subjects` - Comma-separated subscribe subjects

Subject patterns:
- `*` - Single token wildcard
- `>` - Multi-token wildcard (must be last token)

## Production Considerations

### High Availability
- Run 2-3 replicas minimum
- Use pod anti-affinity
- Configure resource requests/limits

### Security
- Use Network Policies
- Regularly rotate NATS credentials
- Monitor unauthorized access attempts
- Use TLS for NATS connections

### Monitoring
- Alert on authentication failures
- Monitor cache hit rates
- Track permission denials
- Create dashboards for key metrics

### Scaling
- Auth service is stateless
- Use HorizontalPodAutoscaler
- Consider connection pooling
- Tune cache for large clusters
