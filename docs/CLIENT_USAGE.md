# Client Usage Guide

This guide explains how to configure your Kubernetes workloads to authenticate with NATS using service account tokens and the NATS Kubernetes OIDC Auth Callout service.

## Overview

Your application needs to:
1. Mount a projected service account token with the `nats` audience
2. Read the token file at runtime
3. Use the token for NATS authentication

## Kubernetes Configuration

### Projected Service Account Token Volume

Kubernetes can project service account tokens into your pod with a custom audience. This is the recommended approach for authenticating with external services.

Add this volume configuration to your Deployment, StatefulSet, or Pod spec:

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
              audience: nats              # Must match JWT_AUDIENCE in auth callout config
              expirationSeconds: 3600     # Token lifetime (1 hour)
              path: token                 # File name in the mount path
```

**Key Configuration Points:**

- `audience: nats` - Must match the `JWT_AUDIENCE` environment variable in the auth callout service (default: "nats")
- `expirationSeconds: 3600` - Token lifetime. Shorter is more secure, but your app must handle rotation. Kubernetes automatically rotates the token when it reaches 80% of its lifetime.
- `path: token` - The filename within the volume mount. Combined with `mountPath`, this becomes `/var/run/secrets/nats/token`

### ServiceAccount Permissions

By default, your service can only publish and subscribe to subjects in its own namespace (`<namespace>.>`).

To grant additional permissions, annotate your ServiceAccount:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service-account
  namespace: my-namespace
  annotations:
    # Allow publishing to additional subjects
    nats.io/allowed-pub-subjects: "platform.events.>, shared.commands.*"
    # Allow subscribing to additional subjects
    nats.io/allowed-sub-subjects: "platform.notifications.*, shared.status"
```

**Resulting Permissions:**

- **Publish**: `my-namespace.>`, `platform.events.>`, `shared.commands.*`
- **Subscribe**: `_INBOX.>`, `_INBOX_my-namespace_my-service-account.>`, `my-namespace.>`, `platform.notifications.*`, `shared.status`
- **Request-Reply**: Enabled via `allow_responses: true` (MaxMsgs: 1, no time limit)

### Request-Reply Security

The auth service provides **layered security** for request-reply patterns:

#### 1. Standard Inbox Pattern (Default)

**Permission**: `_INBOX.>` subscribe access

**Use Case**: Default convenience - works with standard NATS clients without configuration

```go
// Standard usage - no special configuration needed
nc, err := nats.Connect(natsURL, nats.Token(token))
response, err := nc.Request("service.endpoint", []byte("data"), 2*time.Second)
```

**Security**: Suitable when ServiceAccounts represent trusted workload boundaries. Other workloads with the same permissions could theoretically subscribe to `_INBOX.>` to observe replies.

#### 2. Private Inbox Pattern (Enhanced Security)

**Permission**: `_INBOX_namespace_serviceaccount.>` subscribe access

**Use Case**: Multi-tenant isolation - prevents eavesdropping between workloads

```go
// Enhanced security with custom inbox prefix
nc, err := nats.Connect(
    natsURL,
    nats.Token(token),
    nats.CustomInboxPrefix("_INBOX_my-namespace_my-service-account."), // Matches permission
)
response, err := nc.Request("service.endpoint", []byte("data"), 2*time.Second)
```

**Security**: Complete isolation - only this ServiceAccount can receive replies on its private inbox. Other workloads cannot eavesdrop even if they have `_INBOX.>` access.

#### Response Publishing Security

Both patterns use `allow_responses: true` instead of `_INBOX.>` publish permissions:
- **Granular Control**: Responders can only publish replies during active request handling
- **Automatic Expiration**: Response permission expires after one message (MaxMsgs: 1)
- **No Abuse**: Cannot publish to arbitrary inbox subjects
- **NATS Best Practice**: Follows official NATS security recommendations

### Choosing the Right Pattern

| Pattern | Convenience | Security | Use When |
|---------|-------------|----------|----------|
| **Standard Inbox** | ✅ High (no config) | ⚠️ Moderate | ServiceAccounts = trust boundaries |
| **Private Inbox** | ⚠️ Requires config | ✅ High (full isolation) | Multi-tenant or high-security scenarios |

**Recommendation**: Start with standard inbox for simplicity. Upgrade to private inbox when you need defense-in-depth security.

## Client Implementation

### Go Example

Using the official NATS Go client:

```go
package main

import (
    "fmt"
    "log"
    "os"
    "time"

    "github.com/nats-io/nats.go"
)

func main() {
    // Read configuration from environment
    natsURL := getEnv("NATS_URL", "nats://nats:4222")
    tokenFile := getEnv("NATS_TOKEN_FILE", "/var/run/secrets/nats/token")

    // Read the service account token
    token, err := os.ReadFile(tokenFile)
    if err != nil {
        log.Fatalf("Failed to read token file: %v", err)
    }

    // Connect to NATS with token authentication
    nc, err := nats.Connect(
        natsURL,
        nats.Token(string(token)),
        nats.Name("my-nats-client"),
        nats.MaxReconnects(-1),           // Infinite reconnects
        nats.ReconnectWait(2*time.Second),
        nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
            if err != nil {
                log.Printf("Disconnected: %v", err)
            }
        }),
        nats.ReconnectHandler(func(nc *nats.Conn) {
            log.Printf("Reconnected to %s", nc.ConnectedUrl())
        }),
    )
    if err != nil {
        log.Fatalf("Failed to connect to NATS: %v", err)
    }
    defer nc.Close()

    log.Println("Connected to NATS successfully")

    // Example: Subscribe to a subject
    sub, err := nc.Subscribe("my-namespace.events", func(msg *nats.Msg) {
        log.Printf("Received message: %s", string(msg.Data))
    })
    if err != nil {
        log.Fatalf("Failed to subscribe: %v", err)
    }
    defer sub.Unsubscribe()

    // Example: Publish a message
    if err := nc.Publish("my-namespace.events", []byte("Hello NATS!")); err != nil {
        log.Fatalf("Failed to publish: %v", err)
    }

    // Keep the connection alive
    select {}
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

Using the official NATS Java client:

```java
package com.example.natsclient;

import io.nats.client.Connection;
import io.nats.client.Nats;
import io.nats.client.Options;
import io.nats.client.Dispatcher;
import io.nats.client.Message;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.time.Duration;

public class NatsClient {

    public static void main(String[] args) {
        try {
            // Read configuration from environment
            String natsUrl = System.getenv().getOrDefault("NATS_URL", "nats://nats:4222");
            String tokenFile = System.getenv().getOrDefault("NATS_TOKEN_FILE", "/var/run/secrets/nats/token");

            // Read the service account token
            String token = readTokenFile(tokenFile);

            // Configure NATS connection with token authentication
            Options options = new Options.Builder()
                .server(natsUrl)
                .token(token.toCharArray())
                .connectionName("my-nats-client")
                .maxReconnects(-1)  // Infinite reconnects
                .reconnectWait(Duration.ofSeconds(2))
                .connectionListener((conn, type) -> {
                    switch (type) {
                        case DISCONNECTED:
                            System.out.println("Disconnected from NATS");
                            break;
                        case RECONNECTED:
                            System.out.println("Reconnected to " + conn.getConnectedUrl());
                            break;
                        default:
                            break;
                    }
                })
                .build();

            // Connect to NATS
            Connection nc = Nats.connect(options);
            System.out.println("Connected to NATS successfully");

            // Example: Subscribe to a subject
            Dispatcher dispatcher = nc.createDispatcher((msg) -> {
                String data = new String(msg.getData());
                System.out.println("Received message: " + data);
            });
            dispatcher.subscribe("my-namespace.events");

            // Example: Publish a message
            nc.publish("my-namespace.events", "Hello NATS!".getBytes());

            // Keep the connection alive
            Runtime.getRuntime().addShutdownHook(new Thread(() -> {
                try {
                    nc.close();
                    System.out.println("NATS connection closed");
                } catch (InterruptedException e) {
                    e.printStackTrace();
                }
            }));

            // Wait indefinitely
            Thread.currentThread().join();

        } catch (Exception e) {
            System.err.println("Error: " + e.getMessage());
            e.printStackTrace();
            System.exit(1);
        }
    }

    private static String readTokenFile(String tokenFile) throws IOException {
        byte[] tokenBytes = Files.readAllBytes(Paths.get(tokenFile));
        return new String(tokenBytes).trim();
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

**Dependencies (Gradle):**

```gradle
implementation 'io.nats:jnats:2.17.0'
```

### Private Inbox Examples

For enhanced security, clients can configure custom inbox prefixes to prevent eavesdropping.

#### Go with Private Inbox

```go
package main

import (
    "fmt"
    "log"
    "os"
    "github.com/nats-io/nats.go"
)

func main() {
    natsURL := os.Getenv("NATS_URL")
    tokenFile := os.Getenv("NATS_TOKEN_FILE")
    namespace := os.Getenv("K8S_NAMESPACE")        // e.g., "my-namespace"
    serviceAccount := os.Getenv("K8S_SA_NAME")     // e.g., "my-service-account"

    token, _ := os.ReadFile(tokenFile)

    // Configure private inbox prefix matching the permission
    inboxPrefix := fmt.Sprintf("_INBOX_%s_%s.", namespace, serviceAccount)

    nc, err := nats.Connect(
        natsURL,
        nats.Token(string(token)),
        nats.CustomInboxPrefix(inboxPrefix),  // Enable private inbox
    )
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer nc.Close()

    log.Printf("Connected with private inbox prefix: %s", inboxPrefix)

    // Request-reply now uses private inbox - no eavesdropping possible
    response, err := nc.Request("service.endpoint", []byte("data"), 2*time.Second)
    if err != nil {
        log.Fatalf("Request failed: %v", err)
    }

    log.Printf("Received reply: %s", string(response.Data))
}
```

#### Java with Private Inbox

```java
package com.example.natsclient;

import io.nats.client.Connection;
import io.nats.client.Nats;
import io.nats.client.Options;
import java.nio.file.Files;
import java.nio.file.Paths;

public class SecureNatsClient {
    public static void main(String[] args) {
        try {
            String natsUrl = System.getenv("NATS_URL");
            String tokenFile = System.getenv("NATS_TOKEN_FILE");
            String namespace = System.getenv("K8S_NAMESPACE");
            String serviceAccount = System.getenv("K8S_SA_NAME");

            String token = new String(Files.readAllBytes(Paths.get(tokenFile))).trim();

            // Configure private inbox prefix matching the permission
            String inboxPrefix = String.format("_INBOX_%s_%s.", namespace, serviceAccount);

            Options options = new Options.Builder()
                .server(natsUrl)
                .token(token.toCharArray())
                .inboxPrefix(inboxPrefix)  // Enable private inbox
                .build();

            Connection nc = Nats.connect(options);
            System.out.println("Connected with private inbox prefix: " + inboxPrefix);

            // Request-reply now uses private inbox - no eavesdropping possible
            byte[] response = nc.request("service.endpoint", "data".getBytes()).getData();
            System.out.println("Received reply: " + new String(response));

            nc.close();
        } catch (Exception e) {
            e.printStackTrace();
        }
    }
}
```

**Security Benefit**: With private inbox prefix configured, only this ServiceAccount can receive replies. Other workloads cannot eavesdrop even if they have `_INBOX.>` permissions.

## Token Rotation Handling

Kubernetes automatically rotates projected tokens when they reach 80% of their expiration time. Your application should handle token rotation gracefully:

### Go Token Rotation

```go
package main

import (
    "context"
    "log"
    "os"
    "time"

    "github.com/nats-io/nats.go"
)

type TokenRefresher struct {
    tokenFile string
    nc        *nats.Conn
    interval  time.Duration
}

func NewTokenRefresher(tokenFile string, nc *nats.Conn) *TokenRefresher {
    return &TokenRefresher{
        tokenFile: tokenFile,
        nc:        nc,
        interval:  5 * time.Minute, // Check every 5 minutes
    }
}

func (tr *TokenRefresher) Start(ctx context.Context) {
    ticker := time.NewTicker(tr.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := tr.refreshToken(); err != nil {
                log.Printf("Failed to refresh token: %v", err)
            }
        case <-ctx.Done():
            return
        }
    }
}

func (tr *TokenRefresher) refreshToken() error {
    token, err := os.ReadFile(tr.tokenFile)
    if err != nil {
        return err
    }

    // Reconnect with new token
    if !tr.nc.IsClosed() {
        tr.nc.Close()
    }

    // Note: In production, you'd want to reconfigure the connection
    // with the new token. This is a simplified example.
    log.Println("Token refreshed from file")
    return nil
}
```

### Java Token Rotation

```java
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

public class TokenRefresher {
    private final String tokenFile;
    private final Connection connection;
    private final ScheduledExecutorService scheduler;

    public TokenRefresher(String tokenFile, Connection connection) {
        this.tokenFile = tokenFile;
        this.connection = connection;
        this.scheduler = Executors.newSingleThreadScheduledExecutor();
    }

    public void start() {
        // Check for token rotation every 5 minutes
        scheduler.scheduleAtFixedRate(
            this::refreshToken,
            5, 5, TimeUnit.MINUTES
        );
    }

    private void refreshToken() {
        try {
            byte[] token = Files.readAllBytes(Paths.get(tokenFile));
            // Note: In production, you'd need to reconnect with the new token
            // The NATS Java client doesn't support runtime token updates,
            // so you'd need to close and reopen the connection
            System.out.println("Token refreshed from file");
        } catch (Exception e) {
            System.err.println("Failed to refresh token: " + e.getMessage());
        }
    }

    public void stop() {
        scheduler.shutdown();
    }
}
```

## Troubleshooting

### Connection Fails with "Authorization Violation"

**Causes:**
1. Token file not found or empty
2. Token audience doesn't match auth callout configuration
3. Token expired (check `expirationSeconds`)
4. ServiceAccount doesn't exist or was deleted

**Solutions:**
- Verify token file is mounted correctly: `kubectl exec <pod> -- cat /var/run/secrets/nats/token`
- Check token claims: `kubectl exec <pod> -- cat /var/run/secrets/nats/token | cut -d. -f2 | base64 -d | jq`
- Verify audience matches: `JWT_AUDIENCE` in auth callout = `audience` in projected volume
- Check auth callout logs: `kubectl logs -n nats-system deployment/nats-k8s-auth`

### Permission Denied on Publish/Subscribe

**Causes:**
1. Trying to access subjects outside your namespace without annotations
2. ServiceAccount annotations not properly formatted
3. Auth callout cache hasn't synced yet (rare)

**Solutions:**
- Verify ServiceAccount annotations: `kubectl get sa <name> -n <namespace> -o yaml`
- Check auth callout metrics for cache status: `curl http://<auth-callout>:8080/metrics | grep sa_cache_size`
- Review auth callout logs for denied requests: `kubectl logs -n nats-system deployment/nats-k8s-auth | grep denied`

### Token Expiration Issues

**Causes:**
1. `expirationSeconds` too short for your use case
2. Token rotation not handled properly
3. Clock skew between Kubernetes and auth callout

**Solutions:**
- Increase `expirationSeconds` (e.g., 3600 = 1 hour)
- Implement token rotation in your application (see examples above)
- Verify system clocks are synchronized (NTP)

## Best Practices

1. **Token Lifetime**: Use 3600 seconds (1 hour) as a balance between security and practicality
2. **Connection Resilience**: Enable automatic reconnection with exponential backoff
3. **Error Handling**: Log authentication failures and implement retry logic
4. **Least Privilege**: Only grant the minimum subject permissions needed
5. **Monitoring**: Monitor connection status and authentication failures
6. **Testing**: Test authentication in development clusters before production

## Complete Example Deployment

Here's a complete example showing all the pieces together:

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-nats-client
  namespace: my-app
  annotations:
    # Grant access to platform-wide event streams
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
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
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

- [NATS Go Client Documentation](https://github.com/nats-io/nats.go)
- [NATS Java Client Documentation](https://github.com/nats-io/nats.java)
- [Kubernetes Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)
- [Service Account Token Projection](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection)
- [NATS Auth Callout Documentation](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
