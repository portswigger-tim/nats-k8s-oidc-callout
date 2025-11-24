# Test Data

This directory contains real Kubernetes resources for testing the JWT validation and ServiceAccount caching functionality.

## Files

### `jwks.json`
JWKS (JSON Web Key Set) response from a Kubernetes cluster's OIDC discovery endpoint.
- Contains 13 RSA public keys for JWT signature verification
- Used to test JWT validation against real cluster keys
- Source: EKS cluster JWKS endpoint

### `token.jwt`
Valid service account JWT from a Kubernetes cluster.
- **Namespace**: `hakawai`
- **ServiceAccount**: `hakawai-litellm-proxy`
- **Issuer**: `https://oidc.eks.eu-west-1.amazonaws.com/id/B88E72...`
- **Audience**: `["sts.amazonaws.com"]`
- **Expiry**: 2034-11-24 (long-lived for testing)
- **Key ID**: `e3b1d185e0d794058a6346c32b2575ac0efb622e`

#### Decoded Claims
```json
{
  "aud": ["sts.amazonaws.com"],
  "exp": 1764056278,
  "iat": 1763969878,
  "iss": "https://oidc.eks.eu-west-1.amazonaws.com/id/B88E7287E54DB073AC9CDC2FD1BE0969",
  "jti": "1b20f55e-e39a-4010-96e3-5bba8e300ae7",
  "kubernetes.io": {
    "namespace": "hakawai",
    "node": {...},
    "pod": {...},
    "serviceaccount": {
      "name": "hakawai-litellm-proxy",
      "uid": "8180de1d-6687-4024-8c6c-bb5d4700897a"
    }
  },
  "nbf": 1763969878,
  "sub": "system:serviceaccount:hakawai:hakawai-litellm-proxy"
}
```

### `serviceaccount.yaml`
Kubernetes ServiceAccount resource with NATS annotations.
- **Namespace**: `hakawai`
- **Name**: `hakawai-litellm-proxy`
- **NATS Annotations**:
  - `nats.io/allowed-pub-subjects: "platform.events.>, shared.metrics.*"`
  - `nats.io/allowed-sub-subjects: "platform.commands.*, shared.status"`

#### Expected NATS Permissions
Based on the hybrid permission model:

**Publish subjects**:
- `hakawai.>` (default namespace scope)
- `platform.events.>` (from annotation)
- `shared.metrics.*` (from annotation)

**Subscribe subjects**:
- `hakawai.>` (default namespace scope)
- `platform.commands.*` (from annotation)
- `shared.status` (from annotation)

## Usage

These files can be used for:
1. Unit testing JWT validation logic
2. Integration testing ServiceAccount cache
3. End-to-end testing authorization flow
4. Manual testing with real cluster data

## Security Note

These are real tokens and keys from a development cluster. The JWT token has a long expiry for testing purposes. Do not use these in production environments.
