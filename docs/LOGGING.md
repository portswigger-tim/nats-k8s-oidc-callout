# NATS OIDC Callout Logging Documentation

## Overview

The NATS OIDC Callout service uses structured JSON logging with automatic sensitive data redaction to provide comprehensive operational visibility while maintaining security compliance.

**Logging System**:
- **Framework**: Uber's Zap logger (high-performance, structured)
- **Format**: JSON (machine-parsable, optimized for log aggregation)
- **Security**: Automatic redaction of sensitive fields
- **Performance**: Zero-allocation logging in production mode

## Log Levels

### Available Levels

| Level | Purpose | Use Cases |
|-------|---------|-----------|
| `debug` | Detailed diagnostic information | Development, troubleshooting, performance analysis |
| `info` | General operational events | Normal operations, startup/shutdown, configuration |
| `warn` | Warning conditions | Deprecated features, non-critical issues, fallbacks |
| `error` | Error events | Request failures, validation errors, system errors |

### Configuration

Set the `LOG_LEVEL` environment variable:

```yaml
# Development: maximum verbosity
LOG_LEVEL: "debug"

# Production: operational visibility
LOG_LEVEL: "info"

# Quiet production: errors only
LOG_LEVEL: "error"
```

## Security Features

### Automatic Sensitive Data Redaction

The service automatically redacts sensitive information from all log messages:

**Protected Fields**:
- `password` → `"[REDACTED]"`
- `token` → `"[REDACTED]"`
- `secret` → `"[REDACTED]"`
- `authorization` → `"[REDACTED]"`
- `client_secret` → `"[REDACTED]"`

**Example**:
```json
{
  "level": "info",
  "msg": "Token introspection successful",
  "token": "[REDACTED]",
  "user": "john.doe@example.com"
}
```

**Coverage**: Case-insensitive matching across all log fields, nested objects, and arrays.

## Configuration Examples

### Development Environment

```yaml
# Maximum verbosity for troubleshooting
env:
  - name: LOG_LEVEL
    value: "debug"
```

**Output Example**:
```json
{
  "level": "debug",
  "ts": "2024-01-27T10:30:45.123Z",
  "caller": "main/server.go:125",
  "msg": "Processing authentication request",
  "user_claim": "sub",
  "scopes": ["read", "write"],
  "request_id": "abc-123"
}
```

### Production Environment

```yaml
# Operational visibility without excessive detail
env:
  - name: LOG_LEVEL
    value: "info"
```

**Output Example**:
```json
{
  "level": "info",
  "ts": "2024-01-27T10:30:45.123Z",
  "msg": "Token introspection successful",
  "user": "john.doe@example.com",
  "duration_ms": 45
}
```

## Example Log Outputs

### Successful Authentication

```json
{
  "level": "info",
  "ts": "2024-01-27T10:30:45.123Z",
  "msg": "Token introspection successful",
  "user": "john.doe@example.com",
  "scopes": ["nats.read", "nats.write"]
}
```

### Authentication Failure

```json
{
  "level": "error",
  "ts": "2024-01-27T10:30:46.456Z",
  "msg": "Token introspection failed",
  "error": "token expired",
  "status_code": 401
}
```

### Service Startup

```json
{
  "level": "info",
  "ts": "2024-01-27T10:00:00.000Z",
  "msg": "Starting NATS OIDC Callout service",
  "version": "1.0.0",
  "port": 8080,
  "oidc_endpoint": "https://auth.example.com"
}
```

## Monitoring Key Messages

### Critical Messages to Alert On

| Message | Level | Action |
|---------|-------|--------|
| `"Token introspection failed"` | error | Investigate OIDC provider connectivity |
| `"Invalid token format"` | warn | Check client token generation |
| `"Service startup failed"` | error | Check configuration and dependencies |
| `"High error rate detected"` | warn | Review system health metrics |

### Performance Indicators

Monitor these metrics in `info` level logs:
- `duration_ms`: Token introspection latency
- `request_rate`: Requests per second
- `error_rate`: Failed requests percentage

## Troubleshooting

### Enable Debug Logging

```bash
kubectl set env deployment/nats-oidc-callout LOG_LEVEL=debug
```

### View Logs

```bash
# Real-time logs
kubectl logs -f deployment/nats-oidc-callout

# Last 100 lines
kubectl logs deployment/nats-oidc-callout --tail=100

# JSON parsing with jq
kubectl logs deployment/nats-oidc-callout | jq 'select(.level=="error")'
```

### Common Issues

**Issue**: No logs appearing
- **Check**: Container is running (`kubectl get pods`)
- **Check**: LOG_LEVEL is not set too high (error-only mode)

**Issue**: Sensitive data visible in logs
- **Check**: Update to latest version with redaction support
- **Report**: Security issue if redaction fails

## Compliance Notes

**Regulatory Alignment**:
- **PCI DSS 3.2.1**: Requirement 3.4 (cardholder data protection)
- **GDPR**: Article 32 (security of processing)
- **SOC 2**: CC6.7 (logging and monitoring)
- **HIPAA**: §164.312(b) (audit controls)

**Retention Recommendations**:
- Production logs: 90 days minimum
- Security events: 1 year minimum
- Debug logs: 7-30 days (not for production)

---

**Last Updated**: 2024-01-27
**Version**: 1.0.0
