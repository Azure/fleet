/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package informer

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	Cache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InformerManager manages dynamic shared informer for all resources, include Kubernetes resource and
// custom resources defined by CustomResourceDefinition.
type Manager interface {
	// AddDynamicResources builds a dynamicInformer for each resource in the resources list with the event handler.
	// A resource is dynamic if its definition can be created/deleted/updated during runtime.
	// Normally, it is a custom resource that is installed by users. The handler should not be nil.
	AddDynamicResources(resources []APIResourceMeta, handler cache.ResourceEventHandler, listComplete bool)

	// AddStaticResource creates a dynamicInformer for the static 'resource' and set its event handler.
	// A resource is static if its definition is pre-determined and immutable during runtime.
	// Normally, it is a resource that is pre-installed by the system.
	// This function can only be called once for each type of static resource during the initialization of the informer manager.
	AddStaticResource(resource APIResourceMeta, handler cache.ResourceEventHandler)

	// IsInformerSynced checks if the resource's informer is synced.
	IsInformerSynced(resource schema.GroupVersionKind) bool

	// Start will run all informers, the informers will keep running until the channel closed.
	// It is intended to be called after create new informer(s), and it's safe to call multi times.
	Start()

	// Stop stops all informers of in this manager. Once it is stopped, it will be not able to Start again.
	Stop()

	// Lister returns a generic lister used to get 'resource' from informer's store.
	// The informer for 'resource' will be created if not exist, but without any event handler.
	Lister(resource schema.GroupVersionResource) cache.GenericLister

	// GetNameSpaceScopedResources returns the list of namespace scoped resources we are watching.
	GetNameSpaceScopedResources() []schema.GroupVersionResource

	// IsClusterScopedResources returns if a resource is cluster scoped.
	IsClusterScopedResources(resource schema.GroupVersionKind) bool

	// WaitForCacheSync waits for the informer cache to populate.
	WaitForCacheSync()

	// GetClient returns the dynamic dynamicClient.
	GetClient() dynamic.Interface
}

// NewInformerManager constructs a new instance of informerManagerImpl.
// defaultResync with value '0' means no re-sync.
func NewInformerManager(client dynamic.Interface, scheme *runtime.Scheme, defaultResync time.Duration, parentCh <-chan struct{}) Manager {
	// TODO: replace this with plain context
	ctx, cancel := ContextForChannel(parentCh)

	// Get the rest config
	cfg := config.GetConfigOrDie()
	c, err := Cache.New(cfg, Cache.Options{Scheme: scheme, SyncPeriod: &defaultResync})
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to create informer cache")
	}
	return &informerManagerImpl{
		dynamicClient: client,
		ctx:           ctx,
		cancel:        cancel,
		cache:         c,
		scheme:        scheme,
		apiResources:  make(map[schema.GroupVersionKind]*APIResourceMeta),
	}
}

// APIResourceMeta contains the gvk and associated metadata about an api resource
type APIResourceMeta struct {
	// GroupVersionKind is the gvk of the resource.
	GroupVersionKind schema.GroupVersionKind

	// GroupVersionResource is the gvr of the resource.
	GroupVersionResource schema.GroupVersionResource

	// IsClusterScoped indicates if the resource is a cluster scoped resource.
	IsClusterScoped bool

	// isStaticResource indicates if the resource is a static resource that won't be deleted.
	isStaticResource bool

	// Registration is the resource event handler registration after it has been added.
	Registration cache.ResourceEventHandlerRegistration
}

// informerManagerImpl implements the InformerManager interface
type informerManagerImpl struct {
	// dynamicClient is the client-go built-in client that can do CRUD on any resource given gvr.
	dynamicClient dynamic.Interface

	// the context we use to start/stop the informers.
	ctx    context.Context
	cancel context.CancelFunc

	cache  Cache.Cache
	scheme *runtime.Scheme
	// the apiResources map collects all the api resources we watch
	apiResources  map[schema.GroupVersionKind]*APIResourceMeta
	resourcesLock sync.RWMutex
}

func (s *informerManagerImpl) AddDynamicResources(dynResources []APIResourceMeta, handler cache.ResourceEventHandler, listComplete bool) {
	newGVKs := make(map[schema.GroupVersionKind]bool, len(dynResources))
	addInformerFunc := func(newRes APIResourceMeta) {
		_, exist := s.apiResources[newRes.GroupVersionKind]
		if !exist {
			// TODO: how to add GVK to scheme?
			informer, err := s.cache.GetInformerForKind(s.ctx, newRes.GroupVersionKind)
			if err != nil {
				klog.ErrorS(err, "Failed to create informer for resource", "gvk", newRes.GroupVersionKind, "err", err)
				return
			}

			// if AddEventHandler returns an error, it is because the informer has stopped and cannot be restarted
			if newRes.Registration, err = informer.AddEventHandler(handler); err != nil {
				if s.ctx.Err() != nil {
					// context is done, so the error is expected
					return
				}
				panic(err)
			}

			s.apiResources[newRes.GroupVersionKind] = &newRes
			klog.V(2).InfoS("Added an informer for a new resource", "res", s.apiResources[newRes.GroupVersionKind])
		}
	}

	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	// Add the new dynResources that do not exist yet while build a map to speed up lookup
	for _, newRes := range dynResources {
		newGVKs[newRes.GroupVersionKind] = true
		addInformerFunc(newRes)
	}

	if !listComplete {
		// do not disable any informer if we know the resource list is not complete
		return
	}

	// mark the disappeared dynResources from the handler map
	var keysToDelete []schema.GroupVersionKind
	for gvk, dynRes := range s.apiResources {
		if !newGVKs[gvk] && !dynRes.isStaticResource {
			informer, err := s.cache.GetInformerForKind(s.ctx, dynRes.GroupVersionKind)
			if err != nil {
				klog.ErrorS(err, "Failed to get informer for resource", "gvk", dynRes.GroupVersionKind, "err", err)
				return
			}
			if err := informer.RemoveEventHandler(dynRes.Registration); err != nil {
				if s.ctx.Err() == nil {
					// context is not done, so the error is unexpected
					panic(err)
				}
				return
			}
			obj, err := s.scheme.New(gvk)
			if err != nil {
				klog.V(2).ErrorS(err, "Could not get object of a disappeared resource", "res", dynRes)
			}
			err = s.cache.RemoveInformer(s.ctx, obj.(client.Object))
			if err != nil {
				klog.V(2).ErrorS(err, "Could not remove informer manager for resource", "res", dynRes)
			}
			klog.V(2).InfoS("Disabled an informer for a disappeared resource", "res", dynRes)
			keysToDelete = append(keysToDelete, gvk)
		}
	}
	// delete the disappeared resources from the map
	for _, gvk := range keysToDelete {
		delete(s.apiResources, gvk)
	}
}

func (s *informerManagerImpl) AddStaticResource(resource APIResourceMeta, handler cache.ResourceEventHandler) {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	staticRes, exist := s.apiResources[resource.GroupVersionKind]
	if exist {
		klog.ErrorS(fmt.Errorf("a static resource is added already"), "existing res", staticRes)
	}

	resource.isStaticResource = true
	s.apiResources[resource.GroupVersionKind] = &resource
	informer, err := s.cache.GetInformerForKind(s.ctx, resource.GroupVersionKind)
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to create informer for resource", "gvk", resource.GroupVersionKind)
	}
	resource.Registration, err = informer.AddEventHandler(handler)
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to add event handler for resource", "gvk", resource.GroupVersionKind)
	}
}

func (s *informerManagerImpl) IsInformerSynced(resource schema.GroupVersionKind) bool {
	// TODO: use a lazy initialized sync map to reduce the number of informer sync look ups
	informer, err := s.cache.GetInformerForKind(s.ctx, resource)
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to get informer for resource", "gvk", resource)
	}
	return informer.HasSynced()
}

func (s *informerManagerImpl) Lister(resource schema.GroupVersionResource) cache.GenericLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		resource.String(): func(obj interface{}) ([]string, error) {
			return []string{obj.(*unstructured.Unstructured).GetKind()}, nil
		},
	})
	gr := schema.GroupResource{Resource: resource.Resource, Group: resource.Group}
	return cache.NewGenericLister(indexer, gr)
}

func (s *informerManagerImpl) Start() {
	err := s.cache.Start(s.ctx)
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to start informer manager")
	}
}

func (s *informerManagerImpl) GetClient() dynamic.Interface {
	return s.dynamicClient
}

func (s *informerManagerImpl) WaitForCacheSync() {
	s.cache.WaitForCacheSync(s.ctx)
}

func (s *informerManagerImpl) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	res := make([]schema.GroupVersionResource, 0, len(s.apiResources))
	for _, resource := range s.apiResources {
		if !resource.isStaticResource && !resource.IsClusterScoped {
			res = append(res, resource.GroupVersionResource)
		}
	}
	return res
}

func (s *informerManagerImpl) IsClusterScopedResources(gvk schema.GroupVersionKind) bool {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	resMeta, exist := s.apiResources[gvk]
	if !exist {
		return false
	}
	return resMeta.IsClusterScoped
}

func (s *informerManagerImpl) Stop() {
	s.cancel()
}

// ContextForChannel derives a child context from a parent channel.
//
// The derived context's Done channel is closed when the returned cancel function
// is called or when the parent channel is closed, whichever happens first.
//
// Note the caller must *always* call the CancelFunc, otherwise resources may be leaked.
func ContextForChannel(parentCh <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		select {
		case <-parentCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
