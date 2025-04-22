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

// Package informer provides a fake informer manager for testing.
package informer

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
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
