package k8s

import (
	"fmt"
	"strings"
	"sync"

	httpmetrics "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/http"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

const (
	// Annotation keys for NATS permissions
	AnnotationAllowedPubSubjects = "nats.io/allowed-pub-subjects"
	AnnotationAllowedSubSubjects = "nats.io/allowed-sub-subjects"
)

// Permissions represents the NATS publish and subscribe permissions for a ServiceAccount
type Permissions struct {
	Publish   []string
	Subscribe []string
}

// Cache is a thread-safe in-memory cache of ServiceAccount permissions
type Cache struct {
	mu     sync.RWMutex
	cache  map[string]*Permissions // key: "namespace/name"
	logger *zap.Logger
}

// NewCache creates a new empty ServiceAccount cache
func NewCache(logger *zap.Logger) *Cache {
	return &Cache{
		cache:  make(map[string]*Permissions),
		logger: logger,
	}
}

// Get retrieves the permissions for a ServiceAccount by namespace and name.
// Returns (pubPerms, subPerms, found) where found indicates if the SA exists in cache.
func (c *Cache) Get(namespace, name string) ([]string, []string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := makeKey(namespace, name)
	perms, found := c.cache[key]
	if !found {
		c.logger.Debug("ServiceAccount NOT found in cache",
			zap.String("namespace", namespace),
			zap.String("name", name),
			zap.String("key", key),
			zap.Int("cache_size", len(c.cache)))
		return nil, nil, false
	}

	c.logger.Debug("ServiceAccount found in cache",
		zap.String("namespace", namespace),
		zap.String("name", name),
		zap.String("key", key),
		zap.Int("pub_perms_count", len(perms.Publish)),
		zap.Int("sub_perms_count", len(perms.Subscribe)))

	return perms.Publish, perms.Subscribe, true
}

// upsert adds or updates a ServiceAccount in the cache
func (c *Cache) upsert(sa *corev1.ServiceAccount) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := makeKey(sa.Namespace, sa.Name)
	perms := buildPermissions(sa, c.logger)
	c.cache[key] = perms

	c.logger.Debug("ServiceAccount added to cache",
		zap.String("namespace", sa.Namespace),
		zap.String("name", sa.Name),
		zap.String("key", key),
		zap.Int("pub_perms_count", len(perms.Publish)),
		zap.Int("sub_perms_count", len(perms.Subscribe)),
		zap.Int("cache_size", len(c.cache)))
}

// delete removes a ServiceAccount from the cache
func (c *Cache) delete(namespace, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := makeKey(namespace, name)
	delete(c.cache, key)
}

// buildPermissions constructs NATS permissions from a ServiceAccount's annotations
func buildPermissions(sa *corev1.ServiceAccount, logger *zap.Logger) *Permissions {
	perms := &Permissions{}

	// Default: namespace scope (always included)
	defaultSubject := fmt.Sprintf("%s.>", sa.Namespace)
	// Publish: Only namespace scope (response publishing handled via Resp field in auth callout)
	perms.Publish = []string{defaultSubject}
	// Subscribe: Inbox patterns first, then namespace scope
	// - _INBOX.> for default convenience (works with standard NATS clients)
	// - _INBOX_<namespace>_<serviceaccount>.> for private inbox pattern (enhanced security)
	//   Note: Uses underscore separators to prevent _INBOX.> from matching the private inbox
	privateInbox := fmt.Sprintf("_INBOX_%s_%s.>", sa.Namespace, sa.Name)
	perms.Subscribe = []string{"_INBOX.>", privateInbox, defaultSubject}

	// Add additional subjects from annotations
	if pubAnnotation, ok := sa.Annotations[AnnotationAllowedPubSubjects]; ok {
		additionalPub, filteredPub := parseSubjects(pubAnnotation)
		if len(filteredPub) > 0 {
			logger.Warn("Filtered NATS internal subjects from ServiceAccount annotation",
				zap.String("namespace", sa.Namespace),
				zap.String("serviceaccount", sa.Name),
				zap.String("annotation", AnnotationAllowedPubSubjects),
				zap.Strings("filtered", filteredPub))

			// Increment metrics for each filtered subject
			for _, subject := range filteredPub {
				httpmetrics.IncrementFilteredSubjects(sa.Namespace, sa.Name, AnnotationAllowedPubSubjects, subject)
			}
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

			// Increment metrics for each filtered subject
			for _, subject := range filteredSub {
				httpmetrics.IncrementFilteredSubjects(sa.Namespace, sa.Name, AnnotationAllowedSubSubjects, subject)
			}
		}
		perms.Subscribe = append(perms.Subscribe, additionalSub...)
	}

	return perms
}

// parseSubjects parses a comma-separated list of NATS subjects from an annotation value.
// Filters out any _INBOX and _REPLY patterns as those are automatically managed by NATS.
// Returns both the parsed subjects and a list of filtered subjects.
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

// makeKey creates a cache key from namespace and name
func makeKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
