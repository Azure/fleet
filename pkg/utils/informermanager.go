/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// InformerManager manages dynamic shared informer for all resources, include Kubernetes resource and
// custom resources defined by CustomResourceDefinition.
type InformerManager interface {
	// ForResource builds a dynamic shared informer for 'resource' then set event handler.
	// If the informer already exist, the event handler will be appended to the informer.
	// The handler should not be nil.
	ForResource(resource schema.GroupVersionResource, handler cache.ResourceEventHandler)

	// IsInformerSynced checks if the resource's informer is synced.
	IsInformerSynced(resource schema.GroupVersionResource) bool

	// Start will run all informers, the informers will keep running until the channel closed.
	// It is intended to be called after create new informer(s), and it's safe to call multi times.
	Start()

	// Lister returns a generic lister used to get 'resource' from informer's store.
	// The informer for 'resource' will be created if not exist, but without any event handler.
	Lister(resource schema.GroupVersionResource) cache.GenericLister

	// WaitForCacheSync waits for all caches to populate.
	WaitForCacheSync() map[schema.GroupVersionResource]bool

	// WaitForCacheSyncWithTimeout waits for all caches to populate with a definitive timeout.
	WaitForCacheSyncWithTimeout(cacheSyncTimeout time.Duration) map[schema.GroupVersionResource]bool

	// GetClient returns the dynamic dynamicClient.
	GetClient() dynamic.Interface
}

// NewInformerManager constructs a new instance of informerManagerImpl.
// defaultResync with value '0' means no re-sync.
func NewInformerManager(client dynamic.Interface, defaultResync time.Duration, parentCh <-chan struct{}) InformerManager {
	// TODO: replace this with plain context
	ctx, cancel := ContextForChannel(parentCh)
	return &informerManagerImpl{
		dynamicClient:   client,
		ctx:             ctx,
		cancel:          cancel,
		informerFactory: dynamicinformer.NewDynamicSharedInformerFactory(client, defaultResync),
	}
}

type informerManagerImpl struct {
	// dynamicClient is the client-go built-in client that can do CRUD on any resource given gvr
	dynamicClient dynamic.Interface

	// the context we use to start/stop the informers
	ctx    context.Context
	cancel context.CancelFunc

	// informerFactory is the client-go built-in informer factory that can create an informer given gvr
	informerFactory dynamicinformer.DynamicSharedInformerFactory

	// the handlers map collects the handler for each gvr
	handlers sync.Map
}

func (s *informerManagerImpl) ForResource(resource schema.GroupVersionResource, handler cache.ResourceEventHandler) {
	if _, loaded := s.handlers.LoadOrStore(resource, handler); loaded {
		// if the handler already exists for this gvr, just return, we've added the event handler
		return
	}
	s.informerFactory.ForResource(resource).Informer().AddEventHandler(handler)
}

func (s *informerManagerImpl) IsInformerSynced(resource schema.GroupVersionResource) bool {
	// TODO: use a lazy initialized sync map to reduce the number of informer sync look ups
	return s.informerFactory.ForResource(resource).Informer().HasSynced()
}

func (s *informerManagerImpl) Lister(resource schema.GroupVersionResource) cache.GenericLister {
	return s.informerFactory.ForResource(resource).Lister()
}

func (s *informerManagerImpl) Start() {
	s.informerFactory.Start(s.ctx.Done())
}

func (s *informerManagerImpl) Stop() {
	s.cancel()
}

func (s *informerManagerImpl) WaitForCacheSync() map[schema.GroupVersionResource]bool {
	return s.informerFactory.WaitForCacheSync(s.ctx.Done())
}

func (s *informerManagerImpl) WaitForCacheSyncWithTimeout(cacheSyncTimeout time.Duration) map[schema.GroupVersionResource]bool {
	ctx, cancel := context.WithTimeout(s.ctx, cacheSyncTimeout)
	defer cancel()

	return s.informerFactory.WaitForCacheSync(ctx.Done())
}

func (s *informerManagerImpl) GetClient() dynamic.Interface {
	return s.dynamicClient
}
