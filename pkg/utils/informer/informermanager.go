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

	// GetAllResources returns the list of all resources (both cluster-scoped and namespace-scoped) we are watching.
	GetAllResources() []schema.GroupVersionResource

	// IsClusterScopedResources returns if a resource is cluster scoped.
	IsClusterScopedResources(resource schema.GroupVersionKind) bool

	// WaitForCacheSync waits for the informer cache to populate.
	WaitForCacheSync()

	// GetClient returns the dynamic dynamicClient.
	GetClient() dynamic.Interface

	// AddEventHandlerToInformer adds an event handler to an existing informer for the given resource.
	// If the informer doesn't exist, it will be created. This is used by the leader's ChangeDetector
	// to add event handlers to informers that were created by the InformerPopulator.
	AddEventHandlerToInformer(resource schema.GroupVersionResource, handler cache.ResourceEventHandler)

	// CreateInformerForResource creates an informer for the given resource without adding any event handlers.
	// This is used by InformerPopulator to create informers on all pods (leader and followers) so they have
	// synced caches for webhook validation. The leader's ChangeDetector will add event handlers later.
	CreateInformerForResource(resource APIResourceMeta)
}

// NewInformerManager constructs a new instance of informerManagerImpl.
// defaultResync with value '0' means no re-sync.
func NewInformerManager(client dynamic.Interface, defaultResync time.Duration, parentCh <-chan struct{}) Manager {
	// TODO: replace this with plain context
	ctx, cancel := ContextForChannel(parentCh)
	return &informerManagerImpl{
		dynamicClient:      client,
		ctx:                ctx,
		cancel:             cancel,
		informerFactory:    dynamicinformer.NewDynamicSharedInformerFactory(client, defaultResync),
		apiResources:       make(map[schema.GroupVersionKind]*APIResourceMeta),
		registeredHandlers: make(map[schema.GroupVersionResource]bool),
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

	// registeredHandlers tracks which GVRs already have event handlers registered
	// to prevent duplicate registrations and goroutine leaks
	registeredHandlers map[schema.GroupVersionResource]bool
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

func (s *informerManagerImpl) GetAllResources() []schema.GroupVersionResource {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	res := make([]schema.GroupVersionResource, 0, len(s.apiResources))
	for _, resource := range s.apiResources {
		if resource.isPresent {
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

// AddEventHandlerToInformer adds an event handler to an existing informer for the given resource.
// If the informer doesn't exist, it will be created. This is used by the leader's ChangeDetector
// to add event handlers to informers that were created by the InformerPopulator.
// This method is idempotent - calling it multiple times for the same resource will only register
// the handler once, preventing goroutine leaks from duplicate registrations.
func (s *informerManagerImpl) AddEventHandlerToInformer(resource schema.GroupVersionResource, handler cache.ResourceEventHandler) {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	// Check if handler already registered for this resource
	if s.registeredHandlers[resource] {
		return
	}

	informer := s.getOrCreateInformerWithTransform(resource)

	// AddEventHandler returns (ResourceEventHandlerRegistration, error). The registration handle
	// can be used to remove the handler later, but we never remove handlers dynamically -
	// they persist for the lifetime of the informer, so we discard the handle.
	if _, err := informer.AddEventHandler(handler); err != nil {
		klog.Fatal(err, "Failed to add event handler to informer - leader cannot function", "gvr", resource)
	}

	// Mark this resource as having a handler registered
	s.registeredHandlers[resource] = true
	klog.V(2).InfoS("Added event handler to informer", "gvr", resource)
}

func (s *informerManagerImpl) CreateInformerForResource(resource APIResourceMeta) {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	dynRes, exist := s.apiResources[resource.GroupVersionKind]
	if !exist {
		// Register this resource in our tracking map
		resource.isPresent = true
		resource.isStaticResource = false
		s.apiResources[resource.GroupVersionKind] = &resource

		// Create the informer without adding any event handler, with transform set
		_ = s.getOrCreateInformerWithTransform(resource.GroupVersionResource)

		klog.V(3).InfoS("Created informer without handler", "res", resource)
	} else if !dynRes.isPresent {
		// Mark it as present again (resource reappeared)
		dynRes.isPresent = true
		klog.V(3).InfoS("Reactivated informer for reappeared resource", "res", dynRes)
	}
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

// getOrCreateInformerWithTransform gets or creates an informer for the given resource and ensures
// the ManagedFields transform is set. This is idempotent - if the informer exists, we get the same
// instance.
func (s *informerManagerImpl) getOrCreateInformerWithTransform(resource schema.GroupVersionResource) cache.SharedIndexInformer {
	// Get or create the informer (this is idempotent - if it exists, we get the same instance)
	// The idempotent behavior is important because this method may be called multiple times,
	// potentially concurrently, and relies on the shared informer instance from the factory.
	informer := s.informerFactory.ForResource(resource).Informer()

	// Set the transform to strip ManagedFields. This is safe to call even if
	// already set, since we get the same informer instance. If the informer has already
	// started, this will fail silently (which is fine).
	if err := informer.SetTransform(ctrlcache.TransformStripManagedFields()); err != nil {
		klog.V(4).InfoS("Transform already set or informer started", "gvr", resource, "err", err)
	}

	return informer
}
