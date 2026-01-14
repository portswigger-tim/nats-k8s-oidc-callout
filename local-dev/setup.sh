#!/bin/bash
set -e

echo "=== NATS K8s OIDC Auth Callout - Local Development Setup ==="
echo

# Check if nsc is installed
if ! command -v nsc &> /dev/null; then
    echo "ERROR: nsc CLI tool is not installed"
    echo "Please install nsc first:"
    echo "  macOS: brew install nsc"
    echo "  Linux: https://github.com/nats-io/nsc#installation"
    exit 1
fi

# Create temporary NSC environment
TEMP_DIR=$(mktemp -d)
export NSC_HOME="$TEMP_DIR/.nsc"
echo "Using temporary NSC directory: $NSC_HOME"

# Initialize NSC
echo "Initializing NSC..."
nsc env > /dev/null

# Create operator
echo "Creating operator..."
nsc add operator --name local-dev > /dev/null

# Create auth account
echo "Creating AUTH_ACCOUNT..."
nsc add account AUTH_ACCOUNT > /dev/null

# Get account public key and seed
ACCOUNT_PUB=$(nsc describe account AUTH_ACCOUNT --json | jq -r '.sub')
ACCOUNT_SEED=$(nsc describe account AUTH_ACCOUNT --json | jq -r '.nats.signing_keys[0]' 2>/dev/null || echo "")

# If no signing key in JWT, extract from the account key itself
if [ -z "$ACCOUNT_SEED" ] || [ "$ACCOUNT_SEED" == "null" ]; then
    echo "Extracting account seed directly..."
    # Get the account key file path
    ACCOUNT_KEY_FILE="$NSC_HOME/nats/local-dev/accounts/AUTH_ACCOUNT/AUTH_ACCOUNT.nk"
    if [ -f "$ACCOUNT_KEY_FILE" ]; then
        ACCOUNT_SEED=$(cat "$ACCOUNT_KEY_FILE")
    else
        echo "ERROR: Could not find account key file"
        exit 1
    fi
fi

echo
echo "Generated NATS Account:"
echo "  Public Key: $ACCOUNT_PUB"
echo "  Seed (first 20 chars): ${ACCOUNT_SEED:0:20}..."

# Write signing key to file (plain format)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "$ACCOUNT_SEED" > "$SCRIPT_DIR/signing.key"
echo
echo "✅ Signing key written to: $SCRIPT_DIR/signing.key"

# Update NATS server config with account public key
sed -i.bak "s/issuer: \"AABBCCDD\"/issuer: \"$ACCOUNT_PUB\"/" "$SCRIPT_DIR/nats-server.conf"
rm -f "$SCRIPT_DIR/nats-server.conf.bak"
echo "✅ NATS server config updated with issuer: $ACCOUNT_PUB"

# Check if kubeconfig exists (for Kubernetes integration)
if [ ! -f "$SCRIPT_DIR/kubeconfig.yaml" ]; then
    echo
    echo "⚠️  Warning: kubeconfig.yaml not found"
    echo "   Create a kubeconfig file or the auth service will fail to start"
    echo "   For testing without K8s, you can create a dummy kubeconfig:"
    echo "   $ kubectl config view --minify --flatten > $SCRIPT_DIR/kubeconfig.yaml"
fi

# Check if JWKS exists
if [ ! -f "$SCRIPT_DIR/jwks.json" ]; then
    echo
    echo "⚠️  Warning: jwks.json not found"
    echo "   Create a JWKS file or the auth service will fail to validate JWTs"
    echo "   For testing, you can extract JWKS from your cluster:"
    echo "   $ kubectl get --raw /openid/v1/jwks > $SCRIPT_DIR/jwks.json"
fi

# Clean up temp directory
rm -rf "$TEMP_DIR"

echo
echo "=== Setup Complete! ==="
echo
echo "Next steps:"
echo "  1. Start services: docker-compose up --build"
echo "  2. Check health:"
echo "     - NATS: curl http://localhost:8222/varz"
echo "     - Auth service: curl http://localhost:8080/health"
echo "  3. Test authentication with a valid Kubernetes JWT token"
echo
