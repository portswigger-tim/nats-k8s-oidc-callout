# NATS Auth Callout Client Package

NATS client handling auth callout subscriptions and authorization integration.

## Architecture

Uses `synadia-io/callout.go` library for auth callout handling.

**Flow:**
```
NATS client connects → Server sends auth request → Extract JWT →
Auth handler → Build user claims → Sign JWT → Return to NATS
```

## Usage

```go
client, err := nats.NewClient("nats://localhost:4222", "/path/to/creds", authHandler, logger)
client.Start(ctx)
defer client.Shutdown(ctx)
```

## Token Extraction

Priority: `ConnectOptions.JWT` > `ConnectOptions.Token` > Empty (reject)

## Permission Mapping

```go
// Internal auth response
&auth.AuthResponse{
    PublishPermissions:   []string{"hakawai.>", "platform.events.>"},
    SubscribePermissions: []string{"hakawai.>", "platform.commands.*"},
}

// Mapped to NATS user claims
uc.Pub.Allow.Add("hakawai.>", "platform.events.>")
uc.Sub.Allow.Add("hakawai.>", "platform.commands.*")
uc.Expires = time.Now().Add(5 * time.Minute).Unix()
```

## Error Handling

- **Denied**: No JWT returned, timeout (security best practice)
- **Why timeout**: Prevents attackers distinguishing failure reasons

## Testing

- **29.7% coverage** unit tests + integration tests
- Integration: testcontainers with real NATS server
- Run unit: `go test ./internal/nats/`
- Run integration: `go test -tags=integration ./internal/nats/`

## Design Decisions

- **callout.go library**: Handles protocol, encryption, request/response
- **5-minute expiry**: Short-lived tokens, periodic re-auth
- **Generic errors**: Security via timeout, no detailed info to client
