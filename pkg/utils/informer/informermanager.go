/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package informer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// InformerManager manages dynamic shared informer for all resources, include Kubernetes resource and
// custom resources defined by CustomResourceDefinition.
type InformerManager interface {
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
	IsInformerSynced(resource schema.GroupVersionResource) bool

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
	IsClusterScopedResources(resource schema.GroupVersionResource) bool

	// WaitForCacheSync waits for the informer cache to populate.
	WaitForCacheSync()

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
		apiResources:    make(map[schema.GroupVersionResource]*APIResourceMeta),
	}
}

// APIResourceMeta contains the gvk and associated metadata about an api resource
type APIResourceMeta struct {
	// GroupVersionResource is the gvr of the resource.
	GroupVersionResource schema.GroupVersionResource

	// IsClusterScoped indicates if the resource is a cluster scoped resource.
	IsClusterScoped bool

	// isStaticResource indicates if the resource is a static resource that won't be deleted.
	isStaticResource bool

	// isPresent indicates if the resource is still present in the system. We need this because
	// the dynamicInformerFactory does not support a good way to remove/stop an informer.
	isPresent bool
}

// informerManagerImpl implements the InformerManager interface
type informerManagerImpl struct {
	// dynamicClient is the client-go built-in client that can do CRUD on any resource given gvr.
	dynamicClient dynamic.Interface

	// the context we use to start/stop the informers.
	ctx    context.Context
	cancel context.CancelFunc

	// informerFactory is the client-go built-in informer factory that can create an informer given a gvr.
	informerFactory dynamicinformer.DynamicSharedInformerFactory

	// the apiResources map collects all the api resources we watch
	apiResources  map[schema.GroupVersionResource]*APIResourceMeta
	resourcesLock sync.RWMutex
}

func (s *informerManagerImpl) AddDynamicResources(dynResources []APIResourceMeta, handler cache.ResourceEventHandler, listComplete bool) {
	newGVRs := make(map[schema.GroupVersionResource]bool, len(dynResources))

	addInformerFunc := func(newRes APIResourceMeta) {
		dynRes, exist := s.apiResources[newRes.GroupVersionResource]
		if !exist {
			newRes.isPresent = true
			s.apiResources[newRes.GroupVersionResource] = &newRes
			s.informerFactory.ForResource(newRes.GroupVersionResource).Informer().AddEventHandler(handler)
			klog.InfoS("Added an informer for a new resource", "res", newRes)
		} else if !dynRes.isPresent {
			// we just mark it as enabled as we should not add another eventhandler to the informer as it's still
			// in the informerFactory
			// TODO: we have to find a way to stop/delete the informer from the informerFactory
			dynRes.isPresent = true
			klog.InfoS("Reactivated an informer for a reappeared resource", "res", dynRes)
		}
	}

	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	// Add the new dynResources that do not exist yet while build a map to speed up lookup
	for _, newRes := range dynResources {
		newGVRs[newRes.GroupVersionResource] = true
		addInformerFunc(newRes)
	}

	if !listComplete {
		// do not disable any informer if we know the resource list is not complete
		return
	}

	// mark the disappeared dynResources from the handler map
	for gvr, dynRes := range s.apiResources {
		if !newGVRs[gvr] && !dynRes.isStaticResource && dynRes.isPresent {
			// TODO: Disable the informer associated with the resource
			dynRes.isPresent = false
			klog.InfoS("Disabled an informer for a disappeared resource", "res", dynRes)
		}
	}
}

func (s *informerManagerImpl) AddStaticResource(resource APIResourceMeta, handler cache.ResourceEventHandler) {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	staticRes, exist := s.apiResources[resource.GroupVersionResource]
	if exist {
		klog.ErrorS(fmt.Errorf("a static resource is added already"), "existing res", staticRes)
	}

	resource.isStaticResource = true
	s.apiResources[resource.GroupVersionResource] = &resource
	s.informerFactory.ForResource(resource.GroupVersionResource).Informer().AddEventHandler(handler)
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

func (s *informerManagerImpl) GetClient() dynamic.Interface {
	return s.dynamicClient
}

func (s *informerManagerImpl) WaitForCacheSync() {
	s.informerFactory.WaitForCacheSync(s.ctx.Done())
}

func (s *informerManagerImpl) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	res := make([]schema.GroupVersionResource, 0, len(s.apiResources))
	for gvr, resource := range s.apiResources {
		if resource.isPresent && !resource.isStaticResource && !resource.IsClusterScoped {
			res = append(res, gvr)
		}
	}
	return res
}

func (s *informerManagerImpl) IsClusterScopedResources(resource schema.GroupVersionResource) bool {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	resMeta, exist := s.apiResources[resource]
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
