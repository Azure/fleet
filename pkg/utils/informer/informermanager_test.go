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
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"

	testhandler "github.com/kubefleet-dev/kubefleet/test/utils/handler"
	testresource "github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

func TestGetAllResources(t *testing.T) {
	tests := []struct {
		name                     string
		namespaceScopedResources []APIResourceMeta
		clusterScopedResources   []APIResourceMeta
		staticResources          []APIResourceMeta
		expectedResourceCount    int
		expectedNamespacedCount  int
	}{
		{
			name: "mixed cluster and namespace scoped resources",
			namespaceScopedResources: []APIResourceMeta{
				{
					GroupVersionKind:     testresource.GVKConfigMap(),
					GroupVersionResource: testresource.GVRConfigMap(),
					IsClusterScoped:      false,
				},
				{
					GroupVersionKind:     testresource.GVKSecret(),
					GroupVersionResource: testresource.GVRSecret(),
					IsClusterScoped:      false,
				},
			},
			clusterScopedResources: []APIResourceMeta{
				{
					GroupVersionKind:     testresource.GVKNamespace(),
					GroupVersionResource: testresource.GVRNamespace(),
					IsClusterScoped:      true,
				},
			},
			staticResources: []APIResourceMeta{
				{
					GroupVersionKind:     testresource.GVKNode(),
					GroupVersionResource: testresource.GVRNode(),
					IsClusterScoped:      true,
					isStaticResource:     true,
				},
			},
			expectedResourceCount:   4, // All resources including static
			expectedNamespacedCount: 2, // Only namespace-scoped, excluding static
		},
		{
			name:                    "no resources",
			expectedResourceCount:   0,
			expectedNamespacedCount: 0,
		},
		{
			name: "only namespace scoped resources",
			namespaceScopedResources: []APIResourceMeta{
				{
					GroupVersionKind:     testresource.GVKDeployment(),
					GroupVersionResource: testresource.GVRDeployment(),
					IsClusterScoped:      false,
				},
			},
			expectedResourceCount:   1,
			expectedNamespacedCount: 1,
		},
		{
			name: "only cluster scoped resources",
			clusterScopedResources: []APIResourceMeta{
				{
					GroupVersionKind:     testresource.GVKClusterRole(),
					GroupVersionResource: testresource.GVRClusterRole(),
					IsClusterScoped:      true,
				},
			},
			expectedResourceCount:   1,
			expectedNamespacedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake dynamic client
			fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme)
			stopCh := make(chan struct{})
			defer close(stopCh)

			mgr := NewInformerManager(fakeClient, 0, stopCh)
			implMgr := mgr.(*informerManagerImpl)

			// Add namespace-scoped resources
			for _, res := range tt.namespaceScopedResources {
				res.isPresent = true
				implMgr.apiResources[res.GroupVersionKind] = &res
			}

			// Add cluster-scoped resources
			for _, res := range tt.clusterScopedResources {
				res.isPresent = true
				implMgr.apiResources[res.GroupVersionKind] = &res
			}

			// Add static resources
			for _, res := range tt.staticResources {
				res.isPresent = true
				implMgr.apiResources[res.GroupVersionKind] = &res
			}

			// Test GetAllResources
			allResources := mgr.GetAllResources()
			if got := len(allResources); got != tt.expectedResourceCount {
				t.Errorf("GetAllResources() returned %d resources, want %d", got, tt.expectedResourceCount)
			}

			// Verify all expected resources are present
			resourceMap := make(map[schema.GroupVersionResource]bool)
			for _, gvr := range allResources {
				resourceMap[gvr] = true
			}

			for _, res := range tt.namespaceScopedResources {
				if !resourceMap[res.GroupVersionResource] {
					t.Errorf("namespace-scoped resource %v should be in GetAllResources", res.GroupVersionResource)
				}
			}

			for _, res := range tt.clusterScopedResources {
				if !resourceMap[res.GroupVersionResource] {
					t.Errorf("cluster-scoped resource %v should be in GetAllResources", res.GroupVersionResource)
				}
			}

			for _, res := range tt.staticResources {
				if !resourceMap[res.GroupVersionResource] {
					t.Errorf("static resource %v should be in GetAllResources", res.GroupVersionResource)
				}
			}

			// Test GetNameSpaceScopedResources
			namespacedResources := mgr.GetNameSpaceScopedResources()
			if got := len(namespacedResources); got != tt.expectedNamespacedCount {
				t.Errorf("GetNameSpaceScopedResources() returned %d resources, want %d", got, tt.expectedNamespacedCount)
			}

			// Verify only namespace-scoped, non-static resources are present
			namespacedMap := make(map[schema.GroupVersionResource]bool)
			for _, gvr := range namespacedResources {
				namespacedMap[gvr] = true
			}

			for _, res := range tt.namespaceScopedResources {
				if !namespacedMap[res.GroupVersionResource] {
					t.Errorf("namespace-scoped resource %v should be in GetNameSpaceScopedResources", res.GroupVersionResource)
				}
			}

			// Verify cluster-scoped and static resources are NOT in namespace-scoped list
			for _, res := range tt.clusterScopedResources {
				if namespacedMap[res.GroupVersionResource] {
					t.Errorf("cluster-scoped resource %v should NOT be in GetNameSpaceScopedResources", res.GroupVersionResource)
				}
			}

			for _, res := range tt.staticResources {
				if namespacedMap[res.GroupVersionResource] {
					t.Errorf("static resource %v should NOT be in GetNameSpaceScopedResources", res.GroupVersionResource)
				}
			}
		})
	}
}

func TestGetAllResources_NotPresent(t *testing.T) {
	// Test that resources marked as not present are excluded
	fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme)
	stopCh := make(chan struct{})
	defer close(stopCh)

	mgr := NewInformerManager(fakeClient, 0, stopCh)
	implMgr := mgr.(*informerManagerImpl)

	// Add a resource that is present
	presentRes := APIResourceMeta{
		GroupVersionKind:     testresource.GVKConfigMap(),
		GroupVersionResource: testresource.GVRConfigMap(),
		IsClusterScoped:      false,
		isPresent:            true,
	}
	implMgr.apiResources[presentRes.GroupVersionKind] = &presentRes

	// Add a resource that is NOT present (deleted)
	notPresentRes := APIResourceMeta{
		GroupVersionKind:     testresource.GVKSecret(),
		GroupVersionResource: testresource.GVRSecret(),
		IsClusterScoped:      false,
		isPresent:            false,
	}
	implMgr.apiResources[notPresentRes.GroupVersionKind] = &notPresentRes

	allResources := mgr.GetAllResources()
	if got := len(allResources); got != 1 {
		t.Fatalf("GetAllResources() returned %d resources, want 1 (should only return present resources)", got)
	}
	if got := allResources[0]; got != presentRes.GroupVersionResource {
		t.Errorf("GetAllResources()[0] = %v, want %v", got, presentRes.GroupVersionResource)
	}
}

func TestAddEventHandlerToInformer(t *testing.T) {
	tests := []struct {
		name              string
		gvr               schema.GroupVersionResource
		callMultipleTimes bool
	}{
		{
			name:              "add handler to new informer",
			gvr:               testresource.GVRConfigMap(),
			callMultipleTimes: false,
		},
		{
			name:              "calling multiple times is idempotent - only registers once",
			gvr:               testresource.GVRDeployment(),
			callMultipleTimes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme)
			stopCh := make(chan struct{})
			defer close(stopCh)

			mgr := NewInformerManager(fakeClient, 0, stopCh)
			implMgr := mgr.(*informerManagerImpl)

			handler := &testhandler.TestHandler{
				OnAddFunc: func() {},
			}

			// Add the handler first time
			mgr.AddEventHandlerToInformer(tt.gvr, handler)

			// Verify handler is tracked as registered
			implMgr.resourcesLock.RLock()
			checkHandler(t, implMgr, tt.gvr)
			implMgr.resourcesLock.RUnlock()

			if tt.callMultipleTimes {
				// Call again with same GVR - should be idempotent
				mgr.AddEventHandlerToInformer(tt.gvr, handler)
				mgr.AddEventHandlerToInformer(tt.gvr, handler)
				checkHandler(t, implMgr, tt.gvr)
			}
		})
	}
}

func checkHandler(t *testing.T, implMgr *informerManagerImpl, gvr schema.GroupVersionResource) {
	t.Helper()
	implMgr.resourcesLock.RLock()
	defer implMgr.resourcesLock.RUnlock()
	if !implMgr.registeredHandlers[gvr] {
		t.Errorf("Expected handler for %v to be registered", gvr)
	}
	if len(implMgr.registeredHandlers) != 1 {
		t.Errorf("Expected 1 registered handler, got %d", len(implMgr.registeredHandlers))
	}
}

func TestCreateInformerForResource(t *testing.T) {
	tests := []struct {
		name           string
		resource       APIResourceMeta
		createTwice    bool
		markNotPresent bool // Mark resource as not present before second create
	}{
		{
			name: "create new informer",
			resource: APIResourceMeta{
				GroupVersionKind:     testresource.GVKConfigMap(),
				GroupVersionResource: testresource.GVRConfigMap(),
				IsClusterScoped:      false,
			},
			createTwice: false,
		},
		{
			name: "create informer twice (idempotent)",
			resource: APIResourceMeta{
				GroupVersionKind:     testresource.GVKDeployment(),
				GroupVersionResource: testresource.GVRDeployment(),
				IsClusterScoped:      false,
			},
			createTwice: true,
		},
		{
			name: "recreate informer for reappeared resource",
			resource: APIResourceMeta{
				GroupVersionKind:     testresource.GVKSecret(),
				GroupVersionResource: testresource.GVRSecret(),
				IsClusterScoped:      false,
			},
			createTwice:    true,
			markNotPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme)
			stopCh := make(chan struct{})
			defer close(stopCh)

			mgr := NewInformerManager(fakeClient, 0, stopCh)
			implMgr := mgr.(*informerManagerImpl)

			// Create the informer
			mgr.CreateInformerForResource(tt.resource)

			// Verify resource is tracked
			resMeta, exists := implMgr.apiResources[tt.resource.GroupVersionKind]
			if !exists {
				t.Fatal("Expected resource to be tracked in apiResources map")
			}
			if !resMeta.isPresent {
				t.Error("Expected resource to be marked as present")
			}
			if resMeta.IsClusterScoped != tt.resource.IsClusterScoped {
				t.Errorf("IsClusterScoped = %v, want %v", resMeta.IsClusterScoped, tt.resource.IsClusterScoped)
			}

			// Verify informer was created
			informer := implMgr.informerFactory.ForResource(tt.resource.GroupVersionResource).Informer()
			if informer == nil {
				t.Fatal("Expected informer to be created")
			}

			if tt.createTwice {
				if tt.markNotPresent {
					// Mark as not present (simulating resource deletion)
					resMeta.isPresent = false
				}

				// Create again
				mgr.CreateInformerForResource(tt.resource)

				// Verify it's marked as present again
				if !resMeta.isPresent {
					t.Error("Expected resource to be marked as present after recreation")
				}
			}
		})
	}
}

func TestCreateInformerForResource_IsIdempotent(t *testing.T) {
	// Use 3 attempts to verify idempotency works consistently across multiple calls,
	// not just a single retry scenario
	const createAttempts = 3

	// Test that creating the same informer multiple times doesn't cause issues
	fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme)
	stopCh := make(chan struct{})
	defer close(stopCh)

	mgr := NewInformerManager(fakeClient, 0, stopCh)
	implMgr := mgr.(*informerManagerImpl)

	resource := APIResourceMeta{
		GroupVersionKind:     testresource.GVKPod(),
		GroupVersionResource: testresource.GVRPod(),
		IsClusterScoped:      false,
	}

	// Create multiple times
	for i := 0; i < createAttempts; i++ {
		mgr.CreateInformerForResource(resource)
	}

	// Should only have one entry in apiResources after
	// we create the same informer multiple times
	if len(implMgr.apiResources) != 1 {
		t.Errorf("Expected 1 resource in apiResources, got %d", len(implMgr.apiResources))
	}

	// Verify resource is still tracked correctly
	resMeta, exists := implMgr.apiResources[resource.GroupVersionKind]
	if !exists {
		t.Fatal("Expected resource to be tracked")
	}
	if !resMeta.isPresent {
		t.Error("Expected resource to be marked as present")
	}
}
