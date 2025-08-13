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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
)

// FakeLister is a simple fake lister for testing.
type FakeLister struct {
	Objects []runtime.Object
	Err     error
}

func (f *FakeLister) List(selector labels.Selector) ([]runtime.Object, error) {
	if f.Err != nil {
		return nil, f.Err
	}

	if selector == nil {
		return f.Objects, nil
	}

	var filtered []runtime.Object
	for _, obj := range f.Objects {
		if uObj, ok := obj.(*unstructured.Unstructured); ok {
			if selector.Matches(labels.Set(uObj.GetLabels())) {
				filtered = append(filtered, obj)
			}
		}
	}
	return filtered, nil
}

func (f *FakeLister) Get(name string) (runtime.Object, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	for _, obj := range f.Objects {
		if obj.(*unstructured.Unstructured).GetName() == name {
			return obj, nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "test"}, name)
}

func (f *FakeLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return &FakeNamespaceLister{Objects: f.Objects, Namespace: namespace, Err: f.Err}
}

// FakeNamespaceLister implements cache.GenericNamespaceLister.
type FakeNamespaceLister struct {
	Objects   []runtime.Object
	Namespace string
	Err       error
}

func (f *FakeNamespaceLister) List(selector labels.Selector) ([]runtime.Object, error) {
	if f.Err != nil {
		return nil, f.Err
	}

	var filtered []runtime.Object
	for _, obj := range f.Objects {
		if uObj, ok := obj.(*unstructured.Unstructured); ok {
			// Filter by namespace first
			if uObj.GetNamespace() != f.Namespace {
				continue
			}
			// Then filter by label selector if provided
			if selector == nil || selector.Matches(labels.Set(uObj.GetLabels())) {
				filtered = append(filtered, obj)
			}
		}
	}
	return filtered, nil
}

func (f *FakeNamespaceLister) Get(name string) (runtime.Object, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	for _, obj := range f.Objects {
		if uObj := obj.(*unstructured.Unstructured); uObj.GetName() == name && uObj.GetNamespace() == f.Namespace {
			return obj, nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "test"}, name)
}

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
	// Listers provides fake listers for testing.
	Listers map[schema.GroupVersionResource]*FakeLister
	// NamespaceScopedResources is the list of namespace-scoped resources for testing.
	NamespaceScopedResources []schema.GroupVersionResource
}

func (m *FakeManager) AddDynamicResources(_ []informer.APIResourceMeta, _ cache.ResourceEventHandler, _ bool) {
}

func (m *FakeManager) AddStaticResource(_ informer.APIResourceMeta, _ cache.ResourceEventHandler) {
}

func (m *FakeManager) IsInformerSynced(_ schema.GroupVersionResource) bool {
	return true
}

func (m *FakeManager) Start() {
}

func (m *FakeManager) Stop() {
}

func (m *FakeManager) Lister(gvr schema.GroupVersionResource) cache.GenericLister {
	if lister, exists := m.Listers[gvr]; exists {
		return lister
	}
	return &FakeLister{Objects: []runtime.Object{}}
}

func (m *FakeManager) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	return m.NamespaceScopedResources
}

func (m *FakeManager) IsClusterScopedResources(gvk schema.GroupVersionKind) bool {
	return m.APIResources[gvk] == m.IsClusterScopedResource
}

func (m *FakeManager) WaitForCacheSync() {
}

func (m *FakeManager) GetClient() dynamic.Interface {
	return nil
}
