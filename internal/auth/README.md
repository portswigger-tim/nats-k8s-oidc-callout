# Authorization Handler Package

Core authorization logic combining JWT validation with K8s ServiceAccount permissions.

## Architecture

Uses dependency injection with interfaces for testability:

```go
type JWTValidator interface {
    Validate(token string) (*jwt.Claims, error)
}

type PermissionsProvider interface {
    GetPermissions(namespace, name string) ([]string, []string, bool)
}
```

## Data Flow

```
AuthRequest (JWT) → JWT Validation → K8s Lookup → AuthResponse (permissions/error)
```

## Usage

```go
authHandler := auth.NewHandler(jwtValidator, k8sClient)

resp := authHandler.Authorize(&auth.AuthRequest{
    Token: "eyJhbGciOiJSUzI1NiIsImtpZCI6...",
})

if resp.Allowed {
    fmt.Printf("Pub: %v\nSub: %v\n", resp.PublishPermissions, resp.SubscribePermissions)
}
```

## Security

- **Generic errors**: "authorization failed" for all failures (prevents info leakage)
- **Validation flow**: Empty check → JWT → Permissions → Response
- **No logging**: Pure business logic, caller controls logging

## Testing

- **100% coverage** with TDD approach
- Tests: Valid auth, expired token, invalid signature, missing SA
- Run: `go test ./internal/auth/`
