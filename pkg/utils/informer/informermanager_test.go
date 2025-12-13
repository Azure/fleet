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
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "ConfigMap",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "configmaps",
					},
					IsClusterScoped: false,
				},
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Secret",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "secrets",
					},
					IsClusterScoped: false,
				},
			},
			clusterScopedResources: []APIResourceMeta{
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
					IsClusterScoped: true,
				},
			},
			staticResources: []APIResourceMeta{
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Node",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "nodes",
					},
					IsClusterScoped:  true,
					isStaticResource: true,
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
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
					},
					IsClusterScoped: false,
				},
			},
			expectedResourceCount:   1,
			expectedNamespacedCount: 1,
		},
		{
			name: "only cluster scoped resources",
			clusterScopedResources: []APIResourceMeta{
				{
					GroupVersionKind: schema.GroupVersionKind{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
					},
					GroupVersionResource: schema.GroupVersionResource{
						Group:    "rbac.authorization.k8s.io",
						Version:  "v1",
						Resource: "clusterroles",
					},
					IsClusterScoped: true,
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
		GroupVersionKind: schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		},
		GroupVersionResource: schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "configmaps",
		},
		IsClusterScoped: false,
		isPresent:       true,
	}
	implMgr.apiResources[presentRes.GroupVersionKind] = &presentRes

	// Add a resource that is NOT present (deleted)
	notPresentRes := APIResourceMeta{
		GroupVersionKind: schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Secret",
		},
		GroupVersionResource: schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "secrets",
		},
		IsClusterScoped: false,
		isPresent:       false,
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
