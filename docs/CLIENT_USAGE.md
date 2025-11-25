# Client Usage Guide

How to configure Kubernetes workloads to authenticate with NATS using service account tokens.

## Requirements

Your application needs to:
1. Mount a projected service account token with `nats` audience
2. Read the token file at runtime
3. Use the token for NATS authentication

## Kubernetes Configuration

### Projected Service Account Token

Add this to your Deployment/StatefulSet/Pod spec:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-nats-client
  namespace: my-namespace
spec:
  template:
    spec:
      serviceAccountName: my-service-account
      containers:
      - name: app
        image: my-app:latest
        env:
        - name: NATS_URL
          value: "nats://nats:4222"
        - name: NATS_TOKEN_FILE
          value: "/var/run/secrets/nats/token"
        volumeMounts:
        - name: nats-token
          mountPath: /var/run/secrets/nats
          readOnly: true
      volumes:
      - name: nats-token
        projected:
          sources:
          - serviceAccountToken:
              audience: nats              # Must match JWT_AUDIENCE (default: "nats")
              expirationSeconds: 3600     # 1 hour (auto-rotates at 80%)
              path: token
```

### ServiceAccount Permissions

**Default permissions** (automatic):
- Publish: `<namespace>.>`
- Subscribe: `_INBOX.>`, `_INBOX_<namespace>_<serviceaccount>.>`, `<namespace>.>`

**Grant additional permissions** via annotations:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service-account
  namespace: my-namespace
  annotations:
    nats.io/allowed-pub-subjects: "platform.events.>, shared.commands.*"
    nats.io/allowed-sub-subjects: "platform.notifications.*, shared.status"
```

**⚠️ Note:** Do NOT add `_INBOX*` or `_REPLY*` patterns - they're automatically managed.

### Request-Reply Security

Two inbox patterns available:

1. **Standard Inbox (`_INBOX.>`)** - Default, works without configuration
2. **Private Inbox (`_INBOX_namespace_serviceaccount.>`)** - Opt-in isolation

**Response publishing:** Uses `allow_responses: true` (MaxMsgs: 1) instead of `_INBOX.>` publish permissions.

## Client Implementation

### Go Example

```go
package main

import (
    "log"
    "os"
    "time"
    "github.com/nats-io/nats.go"
)

func main() {
    natsURL := getEnv("NATS_URL", "nats://nats:4222")
    tokenFile := getEnv("NATS_TOKEN_FILE", "/var/run/secrets/nats/token")

    token, err := os.ReadFile(tokenFile)
    if err != nil {
        log.Fatalf("Failed to read token: %v", err)
    }

    nc, err := nats.Connect(
        natsURL,
        nats.Token(string(token)),
        nats.MaxReconnects(-1),
        nats.ReconnectWait(2*time.Second),
    )
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer nc.Close()

    log.Println("Connected to NATS")

    // Subscribe
    nc.Subscribe("my-namespace.events", func(msg *nats.Msg) {
        log.Printf("Received: %s", string(msg.Data))
    })

    // Publish
    nc.Publish("my-namespace.events", []byte("Hello NATS!"))

    select {} // Keep alive
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

**Dependencies:**
```bash
go get github.com/nats-io/nats.go
```

### Java Example

```java
package com.example.natsclient;

import io.nats.client.Connection;
import io.nats.client.Nats;
import io.nats.client.Options;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.time.Duration;

public class NatsClient {
    public static void main(String[] args) {
        try {
            String natsUrl = System.getenv().getOrDefault("NATS_URL", "nats://nats:4222");
            String tokenFile = System.getenv().getOrDefault("NATS_TOKEN_FILE", "/var/run/secrets/nats/token");

            String token = new String(Files.readAllBytes(Paths.get(tokenFile))).trim();

            Options options = new Options.Builder()
                .server(natsUrl)
                .token(token.toCharArray())
                .maxReconnects(-1)
                .reconnectWait(Duration.ofSeconds(2))
                .build();

            Connection nc = Nats.connect(options);
            System.out.println("Connected to NATS");

            // Subscribe
            nc.createDispatcher((msg) -> {
                System.out.println("Received: " + new String(msg.getData()));
            }).subscribe("my-namespace.events");

            // Publish
            nc.publish("my-namespace.events", "Hello NATS!".getBytes());

            Thread.currentThread().join();
        } catch (Exception e) {
            e.printStackTrace();
            System.exit(1);
        }
    }
}
```

**Dependencies (Maven):**
```xml
<dependency>
    <groupId>io.nats</groupId>
    <artifactId>jnats</artifactId>
    <version>2.17.0</version>
</dependency>
```

### Private Inbox Pattern

For enhanced security, configure custom inbox prefix:

**Go:**
```go
namespace := os.Getenv("K8S_NAMESPACE")
serviceAccount := os.Getenv("K8S_SA_NAME")
inboxPrefix := fmt.Sprintf("_INBOX_%s_%s", namespace, serviceAccount)

nc, err := nats.Connect(
    natsURL,
    nats.Token(string(token)),
    nats.CustomInboxPrefix(inboxPrefix),
)
```

**Java:**
```java
String inboxPrefix = String.format("_INBOX_%s_%s", namespace, serviceAccount);

Options options = new Options.Builder()
    .server(natsUrl)
    .token(token.toCharArray())
    .inboxPrefix(inboxPrefix)
    .build();
```

## Troubleshooting

### Connection Fails with "Authorization Violation"

**Check:**
- Token file mounted: `kubectl exec <pod> -- cat /var/run/secrets/nats/token`
- Token claims: `kubectl exec <pod> -- cat /var/run/secrets/nats/token | cut -d. -f2 | base64 -d | jq`
- Audience matches: `JWT_AUDIENCE` in auth callout = `audience` in volume
- Auth logs: `kubectl logs -n nats-system deployment/nats-k8s-auth`

### Permission Denied

**Check:**
- ServiceAccount annotations: `kubectl get sa <name> -n <namespace> -o yaml`
- Cache status: `curl http://<auth-callout>:8080/metrics | grep sa_cache_size`
- Auth logs: `kubectl logs -n nats-system deployment/nats-k8s-auth | grep denied`

### Warning: Filtered NATS internal subjects

**Cause:** ServiceAccount annotations include `_INBOX*` or `_REPLY*` patterns (automatically managed).

**Fix:** Remove these from annotations:

```yaml
# ❌ Wrong
annotations:
  nats.io/allowed-sub-subjects: "_INBOX.>, other.subjects.>"

# ✅ Correct
annotations:
  nats.io/allowed-sub-subjects: "other.subjects.>"
```

## Best Practices

1. **Token Lifetime**: Use 3600s (1 hour) for balance between security and practicality
2. **Reconnection**: Enable automatic reconnection with exponential backoff
3. **Least Privilege**: Only grant minimum required permissions
4. **Monitoring**: Track connection status and auth failures
5. **Testing**: Test in dev clusters before production

## Complete Example

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-nats-client
  namespace: my-app
  annotations:
    nats.io/allowed-sub-subjects: "platform.events.>"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-nats-client
  namespace: my-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: my-nats-client
  template:
    metadata:
      labels:
        app: my-nats-client
    spec:
      serviceAccountName: my-nats-client
      containers:
      - name: app
        image: my-nats-client:latest
        env:
        - name: NATS_URL
          value: "nats://nats.nats-system:4222"
        - name: NATS_TOKEN_FILE
          value: "/var/run/secrets/nats/token"
        volumeMounts:
        - name: nats-token
          mountPath: /var/run/secrets/nats
          readOnly: true
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "200m"
      volumes:
      - name: nats-token
        projected:
          sources:
          - serviceAccountToken:
              audience: nats
              expirationSeconds: 3600
              path: token
```

## Additional Resources

- [NATS Go Client](https://github.com/nats-io/nats.go)
- [NATS Java Client](https://github.com/nats-io/nats.java)
- [Kubernetes Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)
- [NATS Auth Callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
