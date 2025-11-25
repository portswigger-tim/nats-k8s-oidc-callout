# Getting Started Guide

Welcome! This guide explains how the NATS Kubernetes OIDC auth callout service works, written for developers who are new to the codebase.

## 🎯 The Big Picture: What Problem Are We Solving?

Imagine you have a messaging system (NATS) and lots of apps running in Kubernetes that want to send/receive messages. The problem is: **How do we know who's allowed to send/receive which messages?**

**Traditional approach:** Every app needs a username and password. This is a pain to manage!

**Our solution:** Apps use their Kubernetes identity (which they already have) to prove who they are. No passwords needed!

## 🏗️ The Main Components (Building Blocks)

Think of this like a restaurant:

1. **NATS Server** = The restaurant (where messages are served)
2. **Your App** = A customer trying to get in
3. **Our Auth Service** = The bouncer at the door (checks if you're allowed in)
4. **Kubernetes** = The government (issues ID cards)

## 📊 The Complete Flow (Step-by-Step)

Here's what happens when your app tries to connect to NATS:

```
┌─────────────────────────────────────────────────────────────────┐
│                    1. APP WANTS TO CONNECT                       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
         ┌────────────────────────────────────┐
         │  Your App (in Kubernetes Pod)      │
         │  "I want to connect to NATS!"      │
         │  Shows Kubernetes ID token         │
         └────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│              2. NATS SERVER ASKS THE AUTH SERVICE                │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
         ┌────────────────────────────────────┐
         │  NATS Server                       │
         │  "Wait! Is this app allowed in?"   │
         │  Sends auth request ───────────────┼─────┐
         └────────────────────────────────────┘     │
                                                     │
                                                     ▼
┌─────────────────────────────────────────────────────────────────┐
│           3. OUR AUTH SERVICE SPRINGS INTO ACTION                │
└─────────────────────────────────────────────────────────────────┘
                                                     │
                              ┌──────────────────────┘
                              ▼
         ┌────────────────────────────────────┐
         │  Auth Service (Our Code!)          │
         │                                    │
         │  Step A: Extract the token         │
         │  Step B: Validate it's real        │
         │  Step C: Check permissions         │
         │  Step D: Build response            │
         └────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                  4. APP GETS ACCESS (OR NOT)                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
         ┌────────────────────────────────────┐
         │  NATS Server                       │
         │  ✅ "Welcome! You can send to:     │
         │      - my-namespace.*              │
         │      - events.>                    │
         │  ❌ Or: "Access Denied"            │
         └────────────────────────────────────┘
```

## 🔍 Deep Dive: Inside the Auth Service

Let's zoom into **Step 3** and see what our code actually does.

### File: `cmd/server/main.go` - The Starting Point

This is like the main entrance to our building:

```go
func main() {
    // 1. Load settings (from environment variables)
    //    Like: Where is NATS? Where is Kubernetes?
    config := loadConfig()

    // 2. Set up logging (so we can see what's happening)
    logger := setupLogger()

    // 3. Start all our workers (explained below)
    startEverything(config, logger)

    // 4. Wait until someone tells us to stop
    waitForShutdownSignal()
}
```

**What starts up:**
- 📡 HTTP server (for health checks - "Are you alive?")
- 🔐 JWT validator (checks if tokens are real)
- 👀 Kubernetes watcher (watches for permission changes)
- 📨 NATS client (listens for auth requests)

---

### The Token Validation Flow - Where the Magic Happens

#### Step A: Token Extraction (`internal/nats/client.go`)

```go
// When NATS asks "Is this app allowed?"
func handleAuthRequest(request) {
    // 1. Extract the token from the request
    //    The token is like an ID card
    token := extractToken(request)

    if token == "" {
        return "ACCESS DENIED" // No ID card? No entry!
    }

    // 2. Pass it to the auth handler
    response := authHandler.Authorize(token)

    // 3. Send response back to NATS
    return response
}
```

**Think of this like:** A bouncer taking your ID card and checking if it's valid.

---

#### Step B: Validate the Token (`internal/jwt/validator.go`)

```go
// Is this token real and not expired?
func Validate(token string) (*Claims, error) {
    // 1. Decode the token (it's in a special format called JWT)
    //    JWT = JSON Web Token (basically a digitally signed message)
    decoded := decodeJWT(token)

    // 2. Verify the signature
    //    Like checking if the hologram on your ID is real
    if !verifySignature(decoded) {
        return ERROR_FAKE_TOKEN
    }

    // 3. Check if it's expired
    //    Like checking if your driver's license is still valid
    if decoded.ExpirationTime < now() {
        return ERROR_EXPIRED
    }

    // 4. Extract who this is for
    //    namespace: "my-app"
    //    serviceAccount: "my-service"
    claims := extractKubernetesClaims(decoded)

    return claims // Token is good!
}
```

**Key concept - JWT (JSON Web Token):**

A JWT looks like: `xxxxx.yyyyy.zzzzz`

- `xxxxx` = Header (what kind of token)
- `yyyyy` = Payload (the actual data: who you are, when it expires)
- `zzzzz` = Signature (proof it's real, like a hologram)

---

#### Step C: Look Up Permissions (`internal/k8s/cache.go`)

```go
// What is this app allowed to do?
func GetPermissions(namespace, serviceAccount) {
    // 1. Look in our cache
    //    We keep a list of all ServiceAccounts and their permissions
    key := namespace + "/" + serviceAccount

    permissions := cache.Get(key)

    if permissions == nil {
        return "NOT FOUND" // This app doesn't exist!
    }

    // 2. Return what they can do
    return permissions {
        PublishTo: ["my-app.>", "events.>"]      // Can send messages here
        SubscribeTo: ["my-app.>", "commands.*"]  // Can receive messages here
    }
}
```

**How the cache works:**

```
Cache (like a phonebook):
┌──────────────────────┬────────────────────────────────────────────┐
│ Key                  │ Permissions                                │
├──────────────────────┼────────────────────────────────────────────┤
│ my-app/my-service   │ Publish: _INBOX.>, my-app.>, events.>     │
│                      │ Subscribe: _INBOX.>, my-app.>, commands.* │
├──────────────────────┼────────────────────────────────────────────┤
│ other-app/service-2 │ Publish: _INBOX.>, other-app.>            │
│                      │ Subscribe: _INBOX.>, other-app.>          │
└──────────────────────┴────────────────────────────────────────────┘
```

**Where do permissions come from?**

From Kubernetes ServiceAccount annotations:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service
  namespace: my-app
  annotations:
    # These are like writing notes on your ID card
    nats.io/allowed-pub-subjects: "events.>"
    nats.io/allowed-sub-subjects: "commands.*"
```

---

#### Step D: Build the Response (`internal/auth/handler.go`)

```go
// Put it all together
func Authorize(request) {
    // 1. Validate token
    claims := jwtValidator.Validate(request.Token)
    if claims == ERROR {
        return "ACCESS DENIED" // Bad token
    }

    // 2. Look up permissions
    pubPerms, subPerms := k8sClient.GetPermissions(
        claims.Namespace,
        claims.ServiceAccount
    )

    if pubPerms == nil {
        return "ACCESS DENIED" // ServiceAccount not found
    }

    // 3. Build success response
    return {
        Allowed: true,
        PublishPermissions: pubPerms,
        SubscribePermissions: subPerms
    }
}
```

---

## 🎬 A Complete Example: From Start to Finish

Let's follow a real request through the entire system.

### Setup

**1. Kubernetes has:**

```yaml
ServiceAccount: my-service
Namespace: production
Annotations:
  nats.io/allowed-pub-subjects: "orders.>, inventory.check"
  nats.io/allowed-sub-subjects: "orders.responses.*"
```

**2. Your app starts up and:**
- Reads its Kubernetes token from `/var/run/secrets/nats/token`
- Connects to NATS with this token

### The Flow

```
1. App: "Hey NATS, I want to connect!"
   Token: "eyJhbGc..." (a long encoded string)

2. NATS: "Hold on, let me check with the auth service..."
   → Sends auth request

3. Auth Service:
   ↓ Extract token from request
   ↓ Validate signature ✓
   ↓ Check expiration ✓
   ↓ Extract claims:
       namespace: "production"
       serviceAccount: "my-service"
   ↓ Look up in cache:
       Found! Permissions:
         Publish: ["_INBOX.>", "production.>", "orders.>", "inventory.check"]
         Subscribe: ["_INBOX.>", "production.>", "orders.responses.*"]
   ↓ Build response

4. NATS: "Welcome! You're authorized!"
   → App can now send/receive messages

5. App tries to publish:
   ✅ "orders.create" → Allowed (matches "orders.>")
   ✅ "inventory.check" → Allowed (exact match)
   ❌ "billing.invoice" → DENIED (not in permissions)
```

---

## 🔄 The Kubernetes Watcher (Background Worker)

While all this is happening, there's a background worker keeping the cache up-to-date:

```go
// File: internal/k8s/client.go

// This runs in the background, watching for changes
func WatchServiceAccounts() {
    for {
        event := waitForKubernetesEvent()

        switch event.Type {
        case "ADDED":
            // New ServiceAccount created
            cache.Add(event.ServiceAccount)

        case "UPDATED":
            // Permissions changed
            cache.Update(event.ServiceAccount)

        case "DELETED":
            // ServiceAccount removed
            cache.Remove(event.ServiceAccount)
        }
    }
}
```

**This means:** If you change annotations in Kubernetes, the auth service knows immediately (within seconds)!

---

## 🛡️ Security Features

### 1. Generic Errors (Security Best Practice)

```go
// We ALWAYS return the same error message
// No matter what went wrong
return "authorization failed"
```

**Why?** So attackers can't figure out:
- Is the token invalid?
- Does the ServiceAccount not exist?
- Are permissions missing?

They just get "authorization failed" - no hints!

### 2. Token Validation Layers

```
Token goes through multiple checks:
1. ✓ Is the format valid?
2. ✓ Is the signature real?
3. ✓ Is it expired?
4. ✓ Does the issuer match?
5. ✓ Does the audience match?
6. ✓ Does the ServiceAccount exist?
```

All must pass!

### 3. Default Permissions (Least Privilege)

Every ServiceAccount gets **namespace isolation** by default:

```
ServiceAccount "my-service" in namespace "production"
Default permissions:
  Publish: ["_INBOX.>", "production.>"]      # Request-reply + your namespace
  Subscribe: ["_INBOX.>", "production.>"]    # Request-reply + your namespace
```

**Note:** `_INBOX.>` is automatically included to enable NATS request-reply patterns.

To get more, you must explicitly add annotations.

---

## 📂 Code Organization (Project Structure)

```
cmd/server/main.go              ← Start here (main entry point)
    ↓
    ├── internal/config/        ← Load settings
    ├── internal/http/          ← Health checks
    ├── internal/jwt/           ← Validate tokens
    │   └── validator.go        ← Token checking logic
    ├── internal/k8s/           ← Talk to Kubernetes
    │   ├── cache.go            ← Store permissions
    │   └── client.go           ← Watch for changes
    ├── internal/auth/          ← Authorization logic
    │   └── handler.go          ← Main decision maker
    └── internal/nats/          ← Talk to NATS
        └── client.go           ← Handle auth requests
```

### Quick Reference: What Each Package Does

| Package | Purpose | Key Files |
|---------|---------|-----------|
| `cmd/server` | Application entry point | `main.go` |
| `internal/config` | Configuration loading | `config.go` |
| `internal/http` | Health checks & metrics | `server.go` |
| `internal/jwt` | JWT token validation | `validator.go` |
| `internal/k8s` | Kubernetes integration | `cache.go`, `client.go` |
| `internal/auth` | Authorization logic | `handler.go` |
| `internal/nats` | NATS integration | `client.go` |

---

## 🧪 How Tests Work

### Unit Tests - Test one piece at a time

```go
// Test: Does token validation work?
func TestValidateToken(t *testing.T) {
    validator := NewValidator()

    // Give it a valid token
    claims, err := validator.Validate(validToken)

    // Check if it worked
    assert.NoError(t, err)
    assert.Equal(t, "production", claims.Namespace)
    assert.Equal(t, "my-service", claims.ServiceAccount)
}
```

**Run unit tests:**
```bash
make test
# or
go test ./...
```

### Integration Tests - Test pieces working together

```go
// Test: Can we connect to a real NATS server?
func TestNATSConnection(t *testing.T) {
    // Start a real NATS server in Docker
    natsServer := startNATSContainer(t)
    defer natsServer.Terminate()

    // Try to connect
    client := NewNATSClient(natsServer.URL)

    // Check if it worked
    assert.True(t, client.IsConnected())
}
```

**Run integration tests:**
```bash
make test-integration
# or
go test -tags=integration ./internal/nats/
```

### E2E Tests - Test the whole system

```go
// Test: Does the entire auth flow work?
func TestCompleteAuthFlow(t *testing.T) {
    // 1. Start Kubernetes cluster (k3s)
    k8s := startK3sCluster(t)
    defer k8s.Terminate()

    // 2. Create ServiceAccount with permissions
    k8s.CreateServiceAccount("my-service", permissions)

    // 3. Start NATS server
    nats := startNATSServer(t)
    defer nats.Terminate()

    // 4. Start our auth service
    authService := startAuthService(k8s, nats)
    defer authService.Shutdown()

    // 5. Get a real Kubernetes token
    token := k8s.CreateToken("my-service", "nats")

    // 6. Try to connect to NATS with this token
    client := connectToNATS(nats.URL, token)
    defer client.Close()

    // 7. Check if it worked and permissions are correct
    assert.NoError(t, client.Publish("my-service.test", []byte("data")))
    assert.Error(t, client.Publish("other-service.test", []byte("data")))
}
```

**Run E2E tests:**
```bash
make test-e2e
# or
go test -tags=e2e -v ./e2e_test.go
```

### Test Coverage

Current coverage:
- `internal/auth`: **100.0%** - Authorization handler
- `internal/k8s`: **81.2%** - Kubernetes ServiceAccount cache
- `internal/jwt`: **72.3%** - JWT validation
- `internal/nats`: **28.9%** - NATS client

---

## 💡 Key Concepts for Beginners

### What is NATS?

A messaging system. Apps can:
- **Publish** messages to subjects (like "orders.create")
- **Subscribe** to subjects to receive messages

Think of it like a postal service for software:
- Subjects are like addresses: "orders.create", "inventory.check"
- Messages are letters you send
- Subscriptions are like mailboxes - you get messages for certain addresses

### What is Kubernetes?

A platform for running containerized apps. It gives each app an identity (ServiceAccount).

**Key Kubernetes concepts:**
- **Pod**: A running instance of your app
- **ServiceAccount**: The identity your pod uses
- **Namespace**: A way to group related pods
- **Annotations**: Key-value metadata you can attach to resources

### What is JWT?

A secure way to transmit information. Like a tamper-proof envelope with a hologram seal.

**Structure:**
```
eyJhbGc... (Header - describes the token)
.
eyJzdWI... (Payload - the actual data)
.
SflKxwR... (Signature - proof of authenticity)
```

**Real-world analogy:** Think of a driver's license:
- Photo and info = Payload (who you are)
- Hologram/watermark = Signature (proof it's real)
- Issue/expiry date = Claims (when it's valid)

### What is an Auth Callout?

Instead of NATS checking users itself, it "calls out" to our service: "Hey, is this user OK?"

**Traditional auth:** NATS has a user database
```
users = [
  {username: "app1", password: "secret123"},
  {username: "app2", password: "secret456"}
]
```

**Auth callout:** NATS asks our service
```
NATS: "Someone with token X wants to connect"
Our Service: "Yes, allow them with these permissions..."
NATS: "OK, they're in!"
```

### What are Annotations?

Like sticky notes you put on Kubernetes resources to store extra information.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service
  annotations:
    # These are annotations - key-value pairs
    nats.io/allowed-pub-subjects: "orders.>"
    description: "Service for processing orders"
```

---

## 🚀 Running Locally

### Prerequisites

```bash
# Install dependencies
brew install go      # Go 1.23+
brew install docker  # Docker for tests

# Clone the repository
git clone https://github.com/portswigger-tim/nats-k8s-oidc-callout.git
cd nats-k8s-oidc-callout
```

### Build

```bash
# Build for your local architecture
make build

# Build for all architectures (amd64, arm64)
make build-all

# Build Docker image
make docker-build
```

### Run Tests

```bash
# Unit tests only (fast, no Docker needed)
make test

# Integration tests (requires Docker)
make test-integration

# E2E tests (requires Docker)
make test-e2e

# All tests
make test-all

# With coverage
make coverage
```

### Run Locally

```bash
# Set required environment variables
export NATS_URL=nats://localhost:4222
export NATS_CREDS_FILE=/path/to/nats.creds
export NATS_ACCOUNT=MyAccount

# Optional (have smart defaults for in-cluster)
export JWKS_URL=https://kubernetes.default.svc/openid/v1/jwks
export JWT_ISSUER=https://kubernetes.default.svc
export JWT_AUDIENCE=nats

# Run the service
./out/nats-k8s-oidc-callout
```

---

## 🎯 Common Development Tasks

### Adding a New Permission Type

1. Update ServiceAccount annotations in `internal/k8s/cache.go`
2. Update permission parsing logic
3. Add tests for new permission type
4. Update documentation

### Debugging Auth Failures

```bash
# Enable debug logging
export LOG_LEVEL=debug

# Check auth service logs
kubectl logs -n nats-system deployment/nats-auth-callout -f

# Check for specific errors
kubectl logs -n nats-system deployment/nats-auth-callout | grep "authorization failed"

# Decode a JWT to see what's in it
echo "TOKEN_HERE" | cut -d. -f2 | base64 -d | jq
```

### Testing Token Validation

```bash
# Create a test token from Kubernetes
kubectl create token my-service \
  --namespace=my-app \
  --audience=nats \
  --duration=1h

# Test with the NATS CLI
nats --server=nats://nats:4222 \
  --token=$(cat token.txt) \
  pub test.subject "hello"
```

---

## 📚 Additional Resources

### Documentation
- [Client Usage Guide](CLIENT_USAGE.md) - How to use from your applications
- [Deployment Guide](../DEPLOY.md) - How to deploy to Kubernetes
- [Build Guide](../BUILD.md) - How to build and package

### External Resources
- [NATS Documentation](https://docs.nats.io/)
- [NATS Auth Callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
- [Kubernetes ServiceAccount Tokens](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/)
- [JWT Introduction](https://jwt.io/introduction)

---

## 🎯 Summary: The Entire Flow in One Sentence

**"An app shows its Kubernetes ID to NATS, NATS asks our auth service if it's legit, we check the ID and look up what the app is allowed to do, then tell NATS to let them in (or not)."**

That's it! Everything else is just making that process secure, fast, and reliable.

---

## 🤝 Contributing

Found something confusing? Want to improve this guide?

1. Open an issue describing what's unclear
2. Submit a PR with improvements
3. Ask questions in discussions

We want this guide to be helpful for everyone! 💙

---

## ❓ FAQ

### Q: Why not just use NATS built-in auth?
**A:** NATS built-in auth requires managing separate credentials for each app. Using Kubernetes identities means apps automatically get credentials, and permissions can be managed using familiar Kubernetes tools.

### Q: What if my Kubernetes cluster doesn't support OIDC?
**A:** The cluster must have OIDC token projection enabled (most modern clusters do). Check with your cluster administrator.

### Q: Can I use this with other identity providers?
**A:** Currently it's designed specifically for Kubernetes service account tokens. Supporting other identity providers would require extending the JWT validation logic.

### Q: How fast is the auth check?
**A:** Very fast! Typical auth checks take 1-5ms because:
- Permissions are cached in memory
- JWT validation uses efficient crypto libraries
- No external API calls after initial cache sync

### Q: What happens if the auth service goes down?
**A:** Existing NATS connections continue to work. New connections will fail until the auth service recovers. For high availability, run multiple replicas of the auth service.

### Q: Can I test this without a full Kubernetes cluster?
**A:** Yes! The E2E tests use k3s (a lightweight Kubernetes distribution) running in Docker. You can use the same approach for local testing.

---

Happy coding! 🚀
