package k8s

import (
	"fmt"
	"strings"
	"sync"

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
	mu    sync.RWMutex
	cache map[string]*Permissions // key: "namespace/name"
}

// NewCache creates a new empty ServiceAccount cache
func NewCache() *Cache {
	return &Cache{
		cache: make(map[string]*Permissions),
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
		return nil, nil, false
	}

	return perms.Publish, perms.Subscribe, true
}

// upsert adds or updates a ServiceAccount in the cache
func (c *Cache) upsert(sa *corev1.ServiceAccount) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := makeKey(sa.Namespace, sa.Name)
	perms := buildPermissions(sa)
	c.cache[key] = perms
}

// delete removes a ServiceAccount from the cache
func (c *Cache) delete(namespace, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := makeKey(namespace, name)
	delete(c.cache, key)
}

// buildPermissions constructs NATS permissions from a ServiceAccount's annotations
func buildPermissions(sa *corev1.ServiceAccount) *Permissions {
	perms := &Permissions{}

	// Default: namespace scope (always included)
	defaultSubject := fmt.Sprintf("%s.>", sa.Namespace)
	// Both Publish and Subscribe include _INBOX.> for request-reply patterns AND namespace scope
	perms.Publish = []string{"_INBOX.>", defaultSubject}
	perms.Subscribe = []string{"_INBOX.>", defaultSubject}

	// Add additional subjects from annotations
	if pubAnnotation, ok := sa.Annotations[AnnotationAllowedPubSubjects]; ok {
		additionalPub := parseSubjects(pubAnnotation)
		perms.Publish = append(perms.Publish, additionalPub...)
	}

	if subAnnotation, ok := sa.Annotations[AnnotationAllowedSubSubjects]; ok {
		additionalSub := parseSubjects(subAnnotation)
		perms.Subscribe = append(perms.Subscribe, additionalSub...)
	}

	return perms
}

// parseSubjects parses a comma-separated list of NATS subjects from an annotation value
func parseSubjects(annotation string) []string {
	if annotation == "" {
		return []string{}
	}

	parts := strings.Split(annotation, ",")
	subjects := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			subjects = append(subjects, trimmed)
		}
	}

	return subjects
}

// makeKey creates a cache key from namespace and name
func makeKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
