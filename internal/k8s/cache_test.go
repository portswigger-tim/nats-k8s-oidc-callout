package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestCache_Get tests retrieving ServiceAccount permissions from cache
func TestCache_Get(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		saName        string
		setupCache    func(*Cache)
		wantPubPerms  []string
		wantSubPerms  []string
		wantFound     bool
	}{
		{
			name:      "ServiceAccount exists with both pub and sub annotations",
			namespace: "hakawai",
			saName:    "hakawai-litellm-proxy",
			setupCache: func(c *Cache) {
				sa := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hakawai-litellm-proxy",
						Namespace: "hakawai",
						Annotations: map[string]string{
							"nats.io/allowed-pub-subjects": "platform.events.>, shared.metrics.*",
							"nats.io/allowed-sub-subjects": "platform.commands.*, shared.status",
						},
					},
				}
				c.upsert(sa)
			},
			wantPubPerms: []string{"_INBOX.>", "hakawai.>", "platform.events.>", "shared.metrics.*"},
			wantSubPerms: []string{"_INBOX.>", "hakawai.>", "platform.commands.*", "shared.status"},
			wantFound:    true,
		},
		{
			name:      "ServiceAccount exists with only pub annotation",
			namespace: "default",
			saName:    "test-sa",
			setupCache: func(c *Cache) {
				sa := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sa",
						Namespace: "default",
						Annotations: map[string]string{
							"nats.io/allowed-pub-subjects": "external.>",
						},
					},
				}
				c.upsert(sa)
			},
			wantPubPerms: []string{"_INBOX.>", "default.>", "external.>"},
			wantSubPerms: []string{"_INBOX.>", "default.>"},
			wantFound:    true,
		},
		{
			name:      "ServiceAccount exists with no NATS annotations (default namespace only)",
			namespace: "production",
			saName:    "minimal-sa",
			setupCache: func(c *Cache) {
				sa := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minimal-sa",
						Namespace: "production",
						Annotations: map[string]string{
							"unrelated.io/annotation": "value",
						},
					},
				}
				c.upsert(sa)
			},
			wantPubPerms: []string{"_INBOX.>", "production.>"},
			wantSubPerms: []string{"_INBOX.>", "production.>"},
			wantFound:    true,
		},
		{
			name:      "ServiceAccount does not exist",
			namespace: "missing",
			saName:    "nonexistent",
			setupCache: func(c *Cache) {
				// Don't add anything to cache
			},
			wantPubPerms: nil,
			wantSubPerms: nil,
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache()
			tt.setupCache(cache)

			pubPerms, subPerms, found := cache.Get(tt.namespace, tt.saName)

			if found != tt.wantFound {
				t.Errorf("Get() found = %v, want %v", found, tt.wantFound)
			}

			if !equalStringSlices(pubPerms, tt.wantPubPerms) {
				t.Errorf("Get() pubPerms = %v, want %v", pubPerms, tt.wantPubPerms)
			}

			if !equalStringSlices(subPerms, tt.wantSubPerms) {
				t.Errorf("Get() subPerms = %v, want %v", subPerms, tt.wantSubPerms)
			}
		})
	}
}

// TestCache_Upsert tests adding and updating ServiceAccounts in cache
func TestCache_Upsert(t *testing.T) {
	cache := NewCache()

	// Add initial ServiceAccount
	sa1 := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "default",
			Annotations: map[string]string{
				"nats.io/allowed-pub-subjects": "initial.>",
			},
		},
	}
	cache.upsert(sa1)

	pubPerms, _, found := cache.Get("default", "test-sa")
	if !found {
		t.Fatal("Expected ServiceAccount to be in cache after upsert")
	}
	if !equalStringSlices(pubPerms, []string{"_INBOX.>", "default.>", "initial.>"}) {
		t.Errorf("Initial pubPerms = %v, want [_INBOX.> default.> initial.>]", pubPerms)
	}

	// Update with new annotations
	sa2 := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "default",
			Annotations: map[string]string{
				"nats.io/allowed-pub-subjects": "updated.>, another.*",
			},
		},
	}
	cache.upsert(sa2)

	pubPerms, _, found = cache.Get("default", "test-sa")
	if !found {
		t.Fatal("Expected ServiceAccount to still be in cache after update")
	}
	if !equalStringSlices(pubPerms, []string{"_INBOX.>", "default.>", "updated.>", "another.*"}) {
		t.Errorf("Updated pubPerms = %v, want [_INBOX.> default.> updated.> another.*]", pubPerms)
	}
}

// TestCache_Delete tests removing ServiceAccounts from cache
func TestCache_Delete(t *testing.T) {
	cache := NewCache()

	// Add ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "default",
			Annotations: map[string]string{
				"nats.io/allowed-pub-subjects": "test.>",
			},
		},
	}
	cache.upsert(sa)

	// Verify it exists
	_, _, found := cache.Get("default", "test-sa")
	if !found {
		t.Fatal("Expected ServiceAccount to be in cache after upsert")
	}

	// Delete it
	cache.delete("default", "test-sa")

	// Verify it's gone
	_, _, found = cache.Get("default", "test-sa")
	if found {
		t.Error("Expected ServiceAccount to be removed from cache after delete")
	}
}

// TestParseSubjects tests parsing comma-separated NATS subjects from annotations
func TestParseSubjects(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		want       []string
	}{
		{
			name:       "Multiple subjects with whitespace",
			annotation: "platform.events.>, shared.metrics.*",
			want:       []string{"platform.events.>", "shared.metrics.*"},
		},
		{
			name:       "Single subject",
			annotation: "platform.commands.*",
			want:       []string{"platform.commands.*"},
		},
		{
			name:       "Empty annotation",
			annotation: "",
			want:       []string{},
		},
		{
			name:       "Multiple subjects with extra whitespace",
			annotation: "  a.> ,  b.* , c  ",
			want:       []string{"a.>", "b.*", "c"},
		},
		{
			name:       "Trailing comma",
			annotation: "a.>, b.*,",
			want:       []string{"a.>", "b.*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSubjects(tt.annotation)
			if !equalStringSlices(got, tt.want) {
				t.Errorf("parseSubjects() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to compare string slices
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
