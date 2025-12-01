# Helm Charts

This directory contains Helm charts for the NATS Kubernetes OIDC Auth Callout service.

## Charts

- `nats-k8s-oidc-callout/` - Main application chart

## Documentation

Chart documentation is automatically generated from `values.yaml` comments using [helm-docs](https://github.com/norwoodj/helm-docs).

### Updating Documentation

#### Option 1: Using Make (Recommended)

```bash
make helm-docs
```

#### Option 2: Using Docker directly

```bash
docker run --rm -v "$(pwd):/helm-docs" -u $(id -u) jnorwood/helm-docs:v1.14.2 \
  --chart-search-root=helm \
  --template-files=README.md.gotmpl
```

#### Option 3: Using pre-commit (Automatic)

Install pre-commit hooks:

```bash
pip install pre-commit
pre-commit install
```

Now helm-docs will run automatically on every commit that modifies files in the `helm/` directory.

### Manual pre-commit run

```bash
pre-commit run helm-docs --all-files
```

## values.yaml Documentation Format

Use helm-docs comment format in `values.yaml`:

```yaml
# -- Description of the parameter
parameterName: value

# -- Description with default override
# @default -- `custom-default-value`
parameterWithDefault: ""
```

The `README.md` will be regenerated from the `README.md.gotmpl` template.
