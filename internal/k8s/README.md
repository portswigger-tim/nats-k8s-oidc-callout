# Kubernetes Client Package

Thread-safe ServiceAccount cache with Kubernetes informer integration.

## Components

- **Cache**: Thread-safe in-memory storage (`sync.RWMutex`)
- **Client**: K8s informer wrapper, handles ADD/UPDATE/DELETE events

## Permission Model

**Default Publish:** Namespace isolation (`<namespace>.>`)

**Default Subscribe:**
- Namespace isolation: `<namespace>.>`
- Inbox patterns: `_INBOX.>`, `_INBOX_<namespace>_<serviceaccount>.>`

**Annotations:**
- `nats.io/allowed-pub-subjects` - Additional publish subjects
- `nats.io/allowed-sub-subjects` - Additional subscribe subjects

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
- Pub: `production.>` (default), `platform.events.>`, `shared.metrics.*`
- Sub: `_INBOX.>`, `_INBOX_production_my-service.>` (inbox defaults), `production.>` (default), `platform.commands.*`, `shared.status`

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
