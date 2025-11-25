package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

// TestClient_Informer tests that the client properly watches ServiceAccount events
func TestClient_Informer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)

	// Create our client with the fake informer
	client := NewClient(informerFactory)

	// Start the informer
	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	// Test 1: ADD event
	t.Run("ADD ServiceAccount", func(t *testing.T) {
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: "default",
				Annotations: map[string]string{
					"nats.io/allowed-pub-subjects": "test.>",
				},
			},
		}

		// Create the ServiceAccount in fake client
		_, err := fakeClient.CoreV1().ServiceAccounts("default").Create(ctx, sa, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create ServiceAccount: %v", err)
		}

		// Give the informer time to process
		time.Sleep(100 * time.Millisecond)

		// Verify it's in the cache
		pubPerms, _, found := client.GetPermissions("default", "test-sa")
		if !found {
			t.Fatal("Expected ServiceAccount to be in cache after ADD event")
		}
		if len(pubPerms) != 3 || pubPerms[0] != "_INBOX.>" || pubPerms[1] != "default.>" || pubPerms[2] != "test.>" {
			t.Errorf("Got pubPerms = %v, want [_INBOX.> default.> test.>]", pubPerms)
		}
	})

	// Test 2: UPDATE event
	t.Run("UPDATE ServiceAccount", func(t *testing.T) {
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: "default",
				Annotations: map[string]string{
					"nats.io/allowed-pub-subjects": "updated.>, another.*",
				},
			},
		}

		// Update the ServiceAccount
		_, err := fakeClient.CoreV1().ServiceAccounts("default").Update(ctx, sa, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("Failed to update ServiceAccount: %v", err)
		}

		// Give the informer time to process
		time.Sleep(100 * time.Millisecond)

		// Verify the cache was updated
		pubPerms, _, found := client.GetPermissions("default", "test-sa")
		if !found {
			t.Fatal("Expected ServiceAccount to still be in cache after UPDATE event")
		}
		if len(pubPerms) != 4 || pubPerms[0] != "_INBOX.>" || pubPerms[1] != "default.>" || pubPerms[2] != "updated.>" || pubPerms[3] != "another.*" {
			t.Errorf("Got pubPerms = %v, want [_INBOX.> default.> updated.> another.*]", pubPerms)
		}
	})

	// Test 3: DELETE event
	t.Run("DELETE ServiceAccount", func(t *testing.T) {
		// Delete the ServiceAccount
		err := fakeClient.CoreV1().ServiceAccounts("default").Delete(ctx, "test-sa", metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Failed to delete ServiceAccount: %v", err)
		}

		// Give the informer time to process
		time.Sleep(100 * time.Millisecond)

		// Verify it's removed from cache
		_, _, found := client.GetPermissions("default", "test-sa")
		if found {
			t.Error("Expected ServiceAccount to be removed from cache after DELETE event")
		}
	})
}

// TestClient_GetPermissions tests the GetPermissions method
func TestClient_GetPermissions(t *testing.T) {
	// Create client with empty informer
	fakeClient := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
	client := NewClient(informerFactory)

	// Manually add to cache for testing
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "default",
			Annotations: map[string]string{
				"nats.io/allowed-pub-subjects": "test.>",
				"nats.io/allowed-sub-subjects": "sub.*",
			},
		},
	}
	client.cache.upsert(sa)

	pubPerms, subPerms, found := client.GetPermissions("default", "test-sa")
	if !found {
		t.Fatal("Expected to find ServiceAccount")
	}

	expectedPub := []string{"_INBOX.>", "default.>", "test.>"}
	expectedSub := []string{"_INBOX.>", "default.>", "sub.*"}

	if !equalStringSlices(pubPerms, expectedPub) {
		t.Errorf("pubPerms = %v, want %v", pubPerms, expectedPub)
	}
	if !equalStringSlices(subPerms, expectedSub) {
		t.Errorf("subPerms = %v, want %v", subPerms, expectedSub)
	}
}

// TestClient_Shutdown tests graceful shutdown
func TestClient_Shutdown(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
	client := NewClient(informerFactory)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the client
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)

	// Shutdown should not hang
	err := client.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}
