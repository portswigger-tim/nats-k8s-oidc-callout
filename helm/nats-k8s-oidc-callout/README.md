# nats-k8s-oidc-callout

NATS authorization callout service for Kubernetes service account JWT validation

**Homepage:** <https://github.com/portswigger-tim/nats-k8s-oidc-callout>

## Overview

NATS authorization callout service for Kubernetes service account JWT validation. This service validates NATS client connections using Kubernetes service account tokens and enforces namespace-based permissions.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- NATS server with auth callout support
- NATS credentials for the auth callout service

## Installation

### From OCI Registry (Recommended)

The chart is published as an OCI artifact to GitHub Container Registry (ghcr.io) with each release.

#### Option 1: Let Helm create the secret

```bash
helm install nats-k8s-oidc-callout \
  oci://ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout \
  --version <VERSION> \
  --set nats.account=AUTH_ACCOUNT \
  --set nats.credentials.create=true \
  --set-file nats.credentials.content=/path/to/auth-callout.creds
```

#### Option 2: Use an existing secret

```bash
# Create the secret first
kubectl create secret generic nats-auth-creds \
  --from-file=credentials=/path/to/auth-callout.creds

# Install the chart
helm install nats-k8s-oidc-callout \
  oci://ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout \
  --version <VERSION> \
  --set nats.account=AUTH_ACCOUNT \
  --set nats.credentials.existingSecret=nats-auth-creds
```

**Finding the latest version:**

```bash
# List available versions
helm show chart oci://ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout

# Or visit the GitHub releases page
# https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases
```

### Verifying Chart Signatures

All released charts are signed with [Sigstore](https://www.sigstore.dev/) using keyless signing. The signatures are stored in the transparency log and can be verified to ensure the chart hasn't been tampered with.

#### Method 1: Using helm-sigstore Plugin (Recommended)

The [helm-sigstore](https://github.com/sigstore/helm-sigstore) plugin provides the simplest verification experience for Helm users.

**Install the plugin:**
```bash
helm plugin install https://github.com/sigstore/helm-sigstore
```

**Verify a chart:**
```bash
# Download the chart from GitHub releases
wget https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases/download/<VERSION>/nats-k8s-oidc-callout-<VERSION>.tgz

# Verify with helm-sigstore
helm sigstore verify nats-k8s-oidc-callout-<VERSION>.tgz
```

#### Method 2: Native Helm Verification (GPG)

Traditional Helm provenance verification using GPG signatures. Each release includes a GPG-signed `.prov` file and the public key used to sign it.

**Verify a chart:**
```bash
# Download chart, provenance, and public key from GitHub releases
wget https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases/download/<VERSION>/nats-k8s-oidc-callout-<VERSION>.tgz
wget https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases/download/<VERSION>/nats-k8s-oidc-callout-<VERSION>.tgz.prov
wget https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases/download/<VERSION>/nats-k8s-oidc-callout-<VERSION>.pub.key

# Import the public key for this release
gpg --import nats-k8s-oidc-callout-<VERSION>.pub.key

# Verify with native Helm command
helm verify nats-k8s-oidc-callout-<VERSION>.tgz
```

**Note:** Each release uses a unique GPG key pair. The public key is included in the release assets and must be imported before verification.

#### Method 3: Using Cosign Directly

If you prefer not to install the plugin, you can use [Cosign](https://github.com/sigstore/cosign) directly.

**Install Cosign:**
```bash
brew install cosign  # macOS
# or download from https://github.com/sigstore/cosign/releases
```

**Verify a chart:**
```bash
# Download the chart and signature bundle from GitHub releases
wget https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases/download/<VERSION>/nats-k8s-oidc-callout-<VERSION>.tgz
wget https://github.com/portswigger-tim/nats-k8s-oidc-callout/releases/download/<VERSION>/nats-k8s-oidc-callout-<VERSION>.tgz.bundle

# Verify the signature
cosign verify-blob \
  --bundle nats-k8s-oidc-callout-<VERSION>.tgz.bundle \
  --certificate-identity-regexp="https://github.com/portswigger-tim/nats-k8s-oidc-callout/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  nats-k8s-oidc-callout-<VERSION>.tgz
```

**What gets verified:**
- Certificate subject matches the GitHub repository
- Certificate issuer is GitHub Actions OIDC provider
- Signature is recorded in Rekor transparency log
- Chart hasn't been modified since signing

#### Method 4: Verify OCI Chart in Registry

Charts in the OCI registry are also signed and include SLSA provenance attestations. You can verify them directly from the registry.

**Verify OCI chart signature:**
```bash
cosign verify \
  --certificate-identity-regexp="https://github.com/portswigger-tim/nats-k8s-oidc-callout/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout:<VERSION>
```

**Verify SLSA provenance:**
```bash
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-identity-regexp="https://github.com/portswigger-tim/nats-k8s-oidc-callout/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout:<VERSION>
```

**View provenance details:**
```bash
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-identity-regexp="https://github.com/portswigger-tim/nats-k8s-oidc-callout/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout:<VERSION> | jq -r '.payload' | base64 -d | jq
```

### From Local Source (Development)

For development or testing with local modifications:

#### Option 1: Let Helm create the secret

```bash
helm install nats-k8s-oidc-callout ./helm/nats-k8s-oidc-callout \
  --set nats.account=AUTH_ACCOUNT \
  --set nats.credentials.create=true \
  --set-file nats.credentials.content=/path/to/auth-callout.creds
```

#### Option 2: Use an existing secret

```bash
# Create the secret first
kubectl create secret generic nats-auth-creds \
  --from-file=credentials=/path/to/auth-callout.creds

# Install the chart
helm install nats-k8s-oidc-callout ./helm/nats-k8s-oidc-callout \
  --set nats.account=AUTH_ACCOUNT \
  --set nats.credentials.existingSecret=nats-auth-creds
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity for pod assignment |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.repository | string | `"ghcr.io/portswigger-tim/nats-k8s-oidc-callout"` | Container image repository |
| image.tag | string | `""` | Overrides the image tag (default is the chart appVersion) |
| jwt.audience | string | `nats` | JWT audience for token validation |
| jwt.issuer | string | `https://kubernetes.default.svc` (in-cluster) | JWT issuer for token validation |
| jwt.jwksUrl | string | `https://kubernetes.default.svc/openid/v1/jwks` (in-cluster) | JWKS URL for JWT validation |
| logLevel | string | `"info"` | Log level (debug, info, warn, error) |
| nats.account | string | `""` | NATS account name for the auth callout service (REQUIRED) |
| nats.credentials.content | string | `""` | Content of the credentials file (required if create=true). Use `--set-file nats.credentials.content=path/to/file` |
| nats.credentials.create | bool | `false` | Create a new secret for NATS credentials |
| nats.credentials.existingSecret | string | `""` | Name of existing secret containing NATS credentials (required if create=false) |
| nats.credentials.existingSecretKey | string | `"credentials"` | Key in the existing secret that contains the credentials file |
| nats.url | string | `nats://nats:4222` | NATS server URL |
| nodeSelector | object | `{}` | Node labels for pod assignment |
| podAnnotations | object | `{}` | Annotations to add to the pod |
| podSecurityContext | object | `{"fsGroup":65532,"runAsNonRoot":true,"runAsUser":65532}` | Pod security context |
| rbac.create | bool | `true` | Create ClusterRole and ClusterRoleBinding for ServiceAccount access |
| replicaCount | int | `1` | Number of replicas |
| resources | object | `{"limits":{"cpu":"500m","memory":"256Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | Resource limits and requests |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Container security context |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created |
| serviceAccount.name | string | `""` | The name of the service account to use (generated if not set) |
| tolerations | list | `[]` | Tolerations for pod assignment |

## ServiceAccount Permissions

The service watches all ServiceAccounts cluster-wide. Configure cross-namespace access using annotations:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-app
  annotations:
    nats.io/allowed-pub-subjects: "app.events,app.commands"
    nats.io/allowed-sub-subjects: "app.replies"
```

Default permissions:
- Namespace isolation: `<namespace>.>` for both publish and subscribe
- Private inbox pattern: `_INBOX.>` and `_INBOX_<namespace>_<serviceaccount>.>`

## Example: Complete Installation

### From OCI Registry (Production)

#### Using Helm-managed secret

```bash
kubectl create namespace nats-system

helm install nats-k8s-oidc-callout \
  oci://ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout \
  --version <VERSION> \
  -n nats-system \
  --set nats.account=AUTH \
  --set nats.credentials.create=true \
  --set-file nats.credentials.content=./auth-callout.creds \
  --set nats.url=nats://nats.nats-system:4222
```

#### Using existing secret

```bash
kubectl create namespace nats-system

kubectl create secret generic nats-auth-creds \
  -n nats-system \
  --from-file=credentials=./auth-callout.creds

helm install nats-k8s-oidc-callout \
  oci://ghcr.io/portswigger-tim/charts/nats-k8s-oidc-callout \
  --version <VERSION> \
  -n nats-system \
  --set nats.account=AUTH \
  --set nats.credentials.existingSecret=nats-auth-creds \
  --set nats.url=nats://nats.nats-system:4222
```

### From Local Source (Development)

#### Using Helm-managed secret

```bash
kubectl create namespace nats-system

helm install nats-k8s-oidc-callout ./helm/nats-k8s-oidc-callout \
  -n nats-system \
  --set nats.account=AUTH \
  --set nats.credentials.create=true \
  --set-file nats.credentials.content=./auth-callout.creds \
  --set nats.url=nats://nats.nats-system:4222
```

#### Using existing secret

```bash
kubectl create namespace nats-system

kubectl create secret generic nats-auth-creds \
  -n nats-system \
  --from-file=credentials=./auth-callout.creds

helm install nats-k8s-oidc-callout ./helm/nats-k8s-oidc-callout \
  -n nats-system \
  --set nats.account=AUTH \
  --set nats.credentials.existingSecret=nats-auth-creds \
  --set nats.url=nats://nats.nats-system:4222
```

## Uninstallation

```bash
helm uninstall nats-k8s-oidc-callout -n nats-system
```

## Troubleshooting

### Check pod status
```bash
kubectl get pods -n nats-system -l app.kubernetes.io/name=nats-k8s-oidc-callout
```

### View logs
```bash
kubectl logs -n nats-system -l app.kubernetes.io/name=nats-k8s-oidc-callout
```

### Check health
```bash
kubectl port-forward -n nats-system svc/nats-k8s-oidc-callout 8080:8080
curl http://localhost:8080/healthz
```

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
