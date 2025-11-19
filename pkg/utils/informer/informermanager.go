/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
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
	IsClusterScopedResources(resource schema.GroupVersionKind) bool

	// WaitForCacheSync waits for the informer cache to populate.
	WaitForCacheSync()

	// GetClient returns the dynamic dynamicClient.
	GetClient() dynamic.Interface
}

// NewInformerManager constructs a new instance of informerManagerImpl.
// defaultResync with value '0' means no re-sync.
func NewInformerManager(client dynamic.Interface, defaultResync time.Duration, parentCh <-chan struct{}) Manager {
	// TODO: replace this with plain context
	ctx, cancel := ContextForChannel(parentCh)
	return &informerManagerImpl{
		dynamicClient:   client,
		ctx:             ctx,
		cancel:          cancel,
		informerFactory: dynamicinformer.NewDynamicSharedInformerFactory(client, defaultResync),
		apiResources:    make(map[schema.GroupVersionKind]*APIResourceMeta),
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
	apiResources  map[schema.GroupVersionKind]*APIResourceMeta
	resourcesLock sync.RWMutex
}

func (s *informerManagerImpl) AddDynamicResources(dynResources []APIResourceMeta, handler cache.ResourceEventHandler, listComplete bool) {
	newGVKs := make(map[schema.GroupVersionKind]bool, len(dynResources))

	addInformerFunc := func(newRes APIResourceMeta) {
		dynRes, exist := s.apiResources[newRes.GroupVersionKind]
		if !exist {
			newRes.isPresent = true
			s.apiResources[newRes.GroupVersionKind] = &newRes
			// TODO (rzhang): remember the ResourceEventHandlerRegistration and remove it when the resource is deleted
			// TODO: handle error which only happens if the informer is stopped
			informer := s.informerFactory.ForResource(newRes.GroupVersionResource).Informer()
			// Strip away the ManagedFields info from objects to save memory.
			//
			// TO-DO (chenyu1): evaluate if there are other fields, e.g., owner refs, status, that can also be stripped
			// away to save memory.
			if err := informer.SetTransform(ctrlcache.TransformStripManagedFields()); err != nil {
				// The SetTransform func would only fail if the informer has already started. In this case,
				// no further action is needed.
				klog.ErrorS(err, "Failed to set transform func for informer", "gvr", newRes.GroupVersionResource)
			}
			_, _ = informer.AddEventHandler(handler)
			klog.InfoS("Added an informer for a new resource", "res", newRes)
		} else if !dynRes.isPresent {
			// we just mark it as enabled as we should not add another eventhandler to the informer as it's still
			// in the informerFactory
			// TODO: add the Event handler back
			dynRes.isPresent = true
			klog.InfoS("Reactivated an informer for a reappeared resource", "res", dynRes)
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
	for gvk, dynRes := range s.apiResources {
		if !newGVKs[gvk] && !dynRes.isStaticResource && dynRes.isPresent {
			// TODO: Remove the Event handler from the informer using the resourceEventHandlerRegistration during creat
			dynRes.isPresent = false
			klog.InfoS("Disabled an informer for a disappeared resource", "res", dynRes)
		}
	}
}

func (s *informerManagerImpl) AddStaticResource(resource APIResourceMeta, handler cache.ResourceEventHandler) {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	staticRes, exist := s.apiResources[resource.GroupVersionKind]
	if exist {
		klog.ErrorS(fmt.Errorf("a static resource is added already"), "existing res", staticRes)
	}
	klog.InfoS("Added an informer for a static resource", "res", resource)
	resource.isStaticResource = true
	s.apiResources[resource.GroupVersionKind] = &resource
	_, _ = s.informerFactory.ForResource(resource.GroupVersionResource).Informer().AddEventHandler(handler)
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
	for _, resource := range s.apiResources {
		if resource.isPresent && !resource.isStaticResource && !resource.IsClusterScoped {
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
