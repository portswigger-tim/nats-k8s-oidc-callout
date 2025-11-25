# Building nats-k8s-oidc-callout

Build instructions for the nats-k8s-oidc-callout application.

## Prerequisites

- Go 1.23+
- Docker with buildx (for multi-arch images)
- Make

## Quick Start

```bash
make build-all      # Build binaries for all architectures
make docker-build   # Build Docker image
```

## Build Process

Two-stage build:
1. **Binary compilation** - Native Go builds for amd64 + arm64
2. **Docker packaging** - Copy binaries into distroless images

Benefits: Faster builds, better caching, smaller images

## Make Targets

### Build
```bash
make build          # Current architecture
make build-all      # All architectures (amd64 + arm64)
make build-amd64    # amd64 only
make build-arm64    # arm64 only
```

### Docker
```bash
make docker-build   # Build multi-arch image locally
make docker-push    # Build and push to registry
```

### Test
```bash
make test           # Unit tests
make test-all       # All tests (unit + integration + e2e)
make coverage       # Coverage report
```

### Utility
```bash
make version        # Show version info
make clean          # Clean build artifacts
make help           # Show help
```

## Build Configuration

- **Binary**: `nats-k8s-oidc-callout`
- **Version**: From git tags (`git describe --tags`)
- **LDFLAGS**: Strips debug info (`-w -s`)
- **Output**: `out/` directory

## Docker Image

**Base image:** `gcr.io/distroless/static-debian12:nonroot`
- Minimal attack surface
- Non-root user (UID 65532)
- CA certificates included

**Multi-arch:** Automatic via `TARGETARCH` build argument

## CI/CD Example

```yaml
# GitHub Actions
name: Build and Push

on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.23'
      - name: Build
        run: make build-all
      - name: Docker
        run: make docker-push
```

## Manual Build

Without Make:

```bash
# Build binaries
mkdir -p out
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags="-w -s" \
  -o out/nats-k8s-oidc-callout-linux-amd64 \
  ./cmd/server

CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -ldflags="-w -s" \
  -o out/nats-k8s-oidc-callout-linux-arm64 \
  ./cmd/server

# Build Docker image
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t nats-k8s-oidc-callout:latest \
  --load \
  .
```

## Troubleshooting

**Docker buildx not available:**
```bash
docker buildx create --use
docker buildx inspect --bootstrap
```

**Binary size:** ~30MB per architecture (statically compiled, stripped)

**Cross-compilation issues:**
- Use Go 1.23+
- Set `CGO_ENABLED=0`
- Verify `GOOS` and `GOARCH`
