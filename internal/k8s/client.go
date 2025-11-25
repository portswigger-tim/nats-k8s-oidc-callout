package k8s

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Client manages Kubernetes ServiceAccount watching and caching
type Client struct {
	cache    *Cache
	informer cache.SharedIndexInformer
	stopCh   chan struct{}
	logger   *zap.Logger
}

// NewClient creates a new Kubernetes client with ServiceAccount informer
func NewClient(factory informers.SharedInformerFactory, logger *zap.Logger) *Client {
	saCache := NewCache(logger)

	// Get the ServiceAccount informer
	informer := factory.Core().V1().ServiceAccounts().Informer()

	client := &Client{
		cache:    saCache,
		informer: informer,
		stopCh:   make(chan struct{}),
		logger:   logger,
	}

	// Register event handlers
	_, err := informer.AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			sa, ok := obj.(*corev1.ServiceAccount)
			if !ok {
				runtime.HandleError(fmt.Errorf("unexpected object type: %T", obj))
				return
			}
			client.cache.upsert(sa)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			sa, ok := newObj.(*corev1.ServiceAccount)
			if !ok {
				runtime.HandleError(fmt.Errorf("unexpected object type: %T", newObj))
				return
			}
			client.cache.upsert(sa)
		},
		DeleteFunc: func(obj interface{}) {
			sa, ok := obj.(*corev1.ServiceAccount)
			if !ok {
				// Handle tombstone - when object is deleted but still in cache
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					runtime.HandleError(fmt.Errorf("unexpected object type: %T", obj))
					return
				}
				sa, ok = tombstone.Obj.(*corev1.ServiceAccount)
				if !ok {
					runtime.HandleError(fmt.Errorf("tombstone contained unexpected object: %T", tombstone.Obj))
					return
				}
			}
			client.cache.delete(sa.Namespace, sa.Name)
		},
	})

	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to add event handler: %w", err))
	}

	return client
}

// GetPermissions retrieves the NATS permissions for a ServiceAccount
func (c *Client) GetPermissions(namespace, name string) (pubPerms []string, subPerms []string, found bool) {
	return c.cache.Get(namespace, name)
}

// Shutdown gracefully shuts down the client
func (c *Client) Shutdown(ctx context.Context) error {
	close(c.stopCh)
	return nil
}
