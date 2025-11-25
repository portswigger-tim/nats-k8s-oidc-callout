# Kubernetes Client Package

Thread-safe ServiceAccount cache with Kubernetes informer integration.

## Components

- **Cache**: Thread-safe in-memory storage (`sync.RWMutex`)
- **Client**: K8s informer wrapper, handles ADD/UPDATE/DELETE events

## Permission Model

**Default:** Namespace isolation (`<namespace>.>`)

**Annotations:**
- `nats.io/allowed-pub-subjects`
- `nats.io/allowed-sub-subjects`

**Example:**
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-service
  namespace: production
  annotations:
    nats.io/allowed-pub-subjects: "platform.events.>, shared.metrics.*"
    nats.io/allowed-sub-subjects: "platform.commands.*, shared.status"
```

Results in:
- Pub: `production.>`, `platform.events.>`, `shared.metrics.*`
- Sub: `production.>`, `platform.commands.*`, `shared.status`

## Usage

```go
k8sClient := k8s.NewClient(informerFactory)

informerFactory.Start(stopCh)
informerFactory.WaitForCacheSync(stopCh)

pubPerms, subPerms, found := k8sClient.GetPermissions("production", "my-service")
```

## Testing

- **81.2% coverage** with TDD approach
- Tests: Event handling, annotation parsing, cache operations
- Run: `go test ./internal/k8s/`
