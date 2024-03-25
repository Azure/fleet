/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package informer provides a fake informer manager for testing.
package informer

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"go.goms.io/fleet/pkg/utils/informer"
)

// FakeManager is a fake informer manager.
type FakeManager struct {
	// APIResources map collects all the api resources we watch.
	APIResources map[schema.GroupVersionKind]bool
	// IsClusterScopedResource defines whether the APIResources store the cluster scope resources or not.
	// If true, the map stores all the cluster scoped resource. If the resource is not in the map, it will be treated
	// as the namespace scoped resource.
	// If false, the map stores all the namespace scoped resource. If the resource is not in the map, it will be treated
	// as the cluster scoped resource.
	IsClusterScopedResource bool
}

func (m *FakeManager) AddDynamicResources(_ []informer.APIResourceMeta, _ cache.ResourceEventHandler, _ bool) {
}

func (m *FakeManager) AddStaticResource(_ informer.APIResourceMeta, _ cache.ResourceEventHandler) {
}

func (m *FakeManager) IsInformerSynced(_ schema.GroupVersionResource) bool {
	return false
}

func (m *FakeManager) Start() {
}

func (m *FakeManager) Stop() {
}

func (m *FakeManager) Lister(_ schema.GroupVersionResource) cache.GenericLister {
	return nil
}

func (m *FakeManager) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	return nil
}

func (m *FakeManager) IsClusterScopedResources(gvk schema.GroupVersionKind) bool {
	return m.APIResources[gvk] == m.IsClusterScopedResource
}

func (m *FakeManager) WaitForCacheSync() {
}

func (m *FakeManager) GetClient() dynamic.Interface {
	return nil
}
