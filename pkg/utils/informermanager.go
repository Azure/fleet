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
		handlers:        make(map[schema.GroupVersionResource][]cache.ResourceEventHandler),
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

	// the map to collect all the handlers for each gvr
	handlers    map[schema.GroupVersionResource][]cache.ResourceEventHandler
	handlerLock sync.RWMutex
}

func (s *informerManagerImpl) ForResource(resource schema.GroupVersionResource, handler cache.ResourceEventHandler) {
	// if handler already exist, just return, nothing changed.
	if s.doesHandlerExist(resource, handler) {
		return
	}

	s.informerFactory.ForResource(resource).Informer().AddEventHandler(handler)
	s.appendHandler(resource, handler)
}

func (s *informerManagerImpl) IsInformerSynced(resource schema.GroupVersionResource) bool {
	// TODO: use a lazy initialized cache to reduce the number of informer sync look ups
	return s.informerFactory.ForResource(resource).Informer().HasSynced()
}

// doesHandlerExist checks if handler already added to the informer that watches the 'resource'.
func (s *informerManagerImpl) doesHandlerExist(resource schema.GroupVersionResource, handler cache.ResourceEventHandler) bool {
	s.handlerLock.RLock()
	defer s.handlerLock.RUnlock()

	handlers, exist := s.handlers[resource]
	if !exist {
		return false
	}

	for _, h := range handlers {
		if h == handler {
			return true
		}
	}

	return false
}

func (s *informerManagerImpl) Lister(resource schema.GroupVersionResource) cache.GenericLister {
	return s.informerFactory.ForResource(resource).Lister()
}

func (s *informerManagerImpl) appendHandler(resource schema.GroupVersionResource, handler cache.ResourceEventHandler) {
	s.handlerLock.Lock()
	defer s.handlerLock.Unlock()

	// assume the handler list exist, caller should ensure for that.
	handlers := s.handlers[resource]

	// assume the handler not exist in it, caller should ensure for that.
	s.handlers[resource] = append(handlers, handler)
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
