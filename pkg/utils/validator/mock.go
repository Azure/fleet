package validator

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"go.goms.io/fleet/pkg/utils/informer"
)

var _ informer.Manager = MockResourceInformer{}

type MockResourceInformer struct{}

func (m MockResourceInformer) AddDynamicResources(_ []informer.APIResourceMeta, _ cache.ResourceEventHandler, _ bool) {
}

func (m MockResourceInformer) AddStaticResource(_ informer.APIResourceMeta, _ cache.ResourceEventHandler) {
}

func (m MockResourceInformer) IsInformerSynced(_ schema.GroupVersionResource) bool {
	return false
}

func (m MockResourceInformer) Start() {}

func (m MockResourceInformer) Stop() {}

func (m MockResourceInformer) Lister(_ schema.GroupVersionResource) cache.GenericLister {
	return nil
}

func (m MockResourceInformer) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{}
}

func (m MockResourceInformer) IsClusterScopedResources(resource schema.GroupVersionKind) bool {
	return resource.Kind != "Role"
}

func (m MockResourceInformer) WaitForCacheSync() {}

func (m MockResourceInformer) GetClient() dynamic.Interface { return nil }
