# Filter NATS Internal Subjects from ServiceAccount Annotations

**Date:** 2025-11-25
**Status:** Approved
**Author:** Design Session

## Problem Statement

Users may mistakenly add `_INBOX` or `_REPLY` patterns to their ServiceAccount annotations, not realizing these are automatically managed by the auth callout service. This creates:

1. **Security confusion** - Users think manual specification is required
2. **Duplicate permissions** - Same subjects listed multiple times
3. **Documentation burden** - Need to explain what NOT to add

## Goals

1. **Filter internal patterns** - Automatically remove `_INBOX*` and `_REPLY*` from annotations
2. **Log warnings** - Help admins identify when users are confused
3. **Track metrics** - Monitor frequency of this user mistake
4. **Document clearly** - Explain automatic permissions and what not to add

## Non-Goals

- Changing how automatic inbox permissions work
- Adding configuration to disable automatic inbox permissions
- Supporting custom inbox patterns via annotations

## Design

### 1. Filtering Logic

**Location:** `internal/k8s/cache.go`

**Changes to parseSubjects():**

```go
// parseSubjects parses a comma-separated list of NATS subjects from an annotation value
// Filters out any _INBOX and _REPLY patterns as those are automatically managed by NATS
func parseSubjects(annotation string) (subjects []string, filtered []string) {
    if annotation == "" {
        return []string{}, []string{}
    }

    parts := strings.Split(annotation, ",")
    subjects = make([]string, 0, len(parts))
    filtered = make([]string, 0)

    for _, part := range parts {
        trimmed := strings.TrimSpace(part)
        if trimmed == "" {
            continue
        }

        // Filter out NATS internal patterns (automatically managed)
        if strings.HasPrefix(trimmed, "_INBOX") || strings.HasPrefix(trimmed, "_REPLY") {
            filtered = append(filtered, trimmed)
            continue
        }

        subjects = append(subjects, trimmed)
    }

    return subjects, filtered
}
```

**Filter Rules:**
- Remove any subject starting with `_INBOX` (case-sensitive)
- Remove any subject starting with `_REPLY` (case-sensitive)
- This catches: `_INBOX.>`, `_INBOX_custom.>`, `_REPLY.>`, etc.
- Preserves: All other subjects

### 2. Logging Strategy

**Level:** `Warn` (system still works correctly, but user confusion detected)

**Structured Fields:**
- `namespace` - ServiceAccount namespace
- `serviceaccount` - ServiceAccount name
- `annotation` - Which annotation had filtered subjects
- `filtered` - List of filtered subjects

**Example Log:**
```
WARN  Filtered NATS internal subjects from ServiceAccount annotation
  namespace=default
  serviceaccount=my-service
  annotation=nats.io/allowed-sub-subjects
  filtered=["_INBOX.>", "_REPLY.>"]
```

**Implementation:** Update `buildPermissions()` to handle filtered return values:

```go
func buildPermissions(sa *corev1.ServiceAccount, logger *zap.Logger) *Permissions {
    perms := &Permissions{}

    // ... (default permissions setup) ...

    // Add additional subjects from annotations
    if pubAnnotation, ok := sa.Annotations[AnnotationAllowedPubSubjects]; ok {
        additionalPub, filteredPub := parseSubjects(pubAnnotation)
        if len(filteredPub) > 0 {
            logger.Warn("Filtered NATS internal subjects from ServiceAccount annotation",
                zap.String("namespace", sa.Namespace),
                zap.String("serviceaccount", sa.Name),
                zap.String("annotation", AnnotationAllowedPubSubjects),
                zap.Strings("filtered", filteredPub))
        }
        perms.Publish = append(perms.Publish, additionalPub...)
    }

    if subAnnotation, ok := sa.Annotations[AnnotationAllowedSubSubjects]; ok {
        additionalSub, filteredSub := parseSubjects(subAnnotation)
        if len(filteredSub) > 0 {
            logger.Warn("Filtered NATS internal subjects from ServiceAccount annotation",
                zap.String("namespace", sa.Namespace),
                zap.String("serviceaccount", sa.Name),
                zap.String("annotation", AnnotationAllowedSubSubjects),
                zap.Strings("filtered", filteredSub))
        }
        perms.Subscribe = append(perms.Subscribe, additionalSub...)
    }

    return perms
}
```

### 3. Metrics

**Metric Definition:**

- **Name:** `nats_auth_filtered_internal_subjects_total`
- **Type:** Counter
- **Help:** "Total number of NATS internal subjects filtered from ServiceAccount annotations"
- **Labels:**
  - `namespace` - ServiceAccount namespace
  - `serviceaccount` - ServiceAccount name
  - `annotation` - Which annotation (pub/sub)
  - `pattern` - The filtered pattern prefix (`_INBOX` or `_REPLY`)

**Implementation in internal/http/metrics.go:**

```go
var (
    // ... existing metrics ...

    filteredSubjectsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nats_auth_filtered_internal_subjects_total",
            Help: "Total number of NATS internal subjects filtered from ServiceAccount annotations",
        },
        []string{"namespace", "serviceaccount", "annotation", "pattern"},
    )
)

func init() {
    prometheus.MustRegister(filteredSubjectsTotal)
}

// Helper function to increment metric
func IncrementFilteredSubjects(namespace, serviceaccount, annotation, subject string) {
    pattern := "_INBOX"
    if strings.HasPrefix(subject, "_REPLY") {
        pattern = "_REPLY"
    }

    filteredSubjectsTotal.WithLabelValues(
        namespace,
        serviceaccount,
        annotation,
        pattern,
    ).Inc()
}
```

### 4. Documentation Updates

#### docs/client-usage.md

Add new section after permissions examples:

```markdown
### Automatic Permissions

The auth callout service automatically grants the following permissions:

**Publish Permissions:**
- `<namespace>.>` - Your namespace scope

**Subscribe Permissions:**
- `_INBOX.>` - Standard NATS inbox for request-reply
- `_INBOX_<namespace>_<serviceaccount>.>` - Private inbox for enhanced security
- `<namespace>.>` - Your namespace scope

**Important:** Do NOT add `_INBOX*` or `_REPLY*` patterns to your ServiceAccount annotations.
These are automatically managed and will be filtered out if specified.
```

Add troubleshooting section:

```markdown
## Troubleshooting

### Warning: Filtered NATS internal subjects

If you see this warning in logs:
```
WARN Filtered NATS internal subjects from ServiceAccount annotation
```

**Cause:** Your ServiceAccount annotations include `_INBOX` or `_REPLY` patterns.

**Solution:** Remove these from your annotations - they are automatically granted:
- Standard inbox: `_INBOX.>` (always enabled)
- Private inbox: `_INBOX_<namespace>_<serviceaccount>.>` (always enabled)
- Reply subjects: Managed by NATS automatically

**Example Fix:**
```yaml
# ❌ Before (unnecessary)
annotations:
  nats.io/allowed-sub-subjects: "_INBOX.>, other.subjects.>"

# ✅ After (correct)
annotations:
  nats.io/allowed-sub-subjects: "other.subjects.>"
```
```

#### README.md

Update ServiceAccount annotation example:

```yaml
annotations:
  nats.io/allowed-pub-subjects: "events.>"
  nats.io/allowed-sub-subjects: "commands.>"
  # ⚠️ Do not add _INBOX or _REPLY patterns - they are automatic
```

## Implementation Plan

### Phase 1: Core Changes
1. Update `parseSubjects()` signature and implementation
2. Add metrics counter to `internal/http/metrics.go`
3. Update `buildPermissions()` to log warnings and increment metrics
4. Pass logger and metrics to Cache/buildPermissions

### Phase 2: Testing
1. Update unit tests for `parseSubjects()` new signature
2. Add unit tests for filtering behavior
3. Add unit tests for logging behavior
4. Add unit tests for metrics increment
5. Verify existing E2E tests still pass

### Phase 3: Documentation
1. Update `docs/client-usage.md` with automatic permissions section
2. Add troubleshooting section to `docs/client-usage.md`
3. Update `README.md` with warning in examples

## Testing Requirements

### Unit Tests

**parseSubjects() filtering:**
- Test filtering `_INBOX.>`
- Test filtering `_INBOX_custom.>`
- Test filtering `_REPLY.>`
- Test filtering multiple internal patterns
- Test preserving non-internal subjects
- Test mixed internal and regular subjects

**buildPermissions() logging:**
- Test warning logged when pub subjects filtered
- Test warning logged when sub subjects filtered
- Test no warning when no filtering needed
- Test structured log fields are correct

**Metrics:**
- Test counter incremented for `_INBOX` pattern
- Test counter incremented for `_REPLY` pattern
- Test labels are correct

### Integration Tests

- Verify filtered subjects don't appear in final permissions
- Verify metrics exposed via /metrics endpoint

## Risks and Mitigations

### Risk: Breaking existing users who explicitly specify inbox patterns

**Mitigation:**
- Warn logs make it visible
- Metrics track frequency
- Documentation explains the change
- Behavior is harmless (duplicates removed, permissions still work)

### Risk: Users actually need custom inbox patterns

**Mitigation:**
- If this becomes a real requirement, we can add an opt-out annotation
- Monitor metrics to see if this is actually needed
- Current design doesn't prevent future extension

## Alternatives Considered

### Alternative 1: Keep duplicates, document only

**Rejected because:**
- Still confusing to users
- Duplicate permissions in list look wrong
- Documentation burden remains high

### Alternative 2: Make inbox permissions opt-in

**Rejected because:**
- Breaks request-reply pattern for naive users
- Increases security risk (users might forget)
- Goes against NATS best practices

### Alternative 3: Allow custom inbox patterns

**Rejected because:**
- No known use case for this
- Adds complexity
- Can be added later if needed

## Success Criteria

1. All tests pass (unit + integration + E2E)
2. Metrics correctly track filtered subjects
3. Warning logs appear when internal patterns filtered
4. Documentation clearly explains automatic permissions
5. No duplicate permissions in final permission lists

## Future Enhancements

- Add opt-out annotation if users need custom inbox patterns (only if metrics show demand)
- Add dashboard/alerting for high filtered subject counts (indicates documentation gap)
