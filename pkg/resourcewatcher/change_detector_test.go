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

package resourcewatcher

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
	testresource "github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

func TestChangeDetector_discoverResources(t *testing.T) {
	tests := []struct {
		name               string
		discoveryResources []*metav1.APIResourceList
		resourceConfig     *utils.ResourceConfig
	}{
		{
			name: "discovers and adds handlers for watchable resources",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceConfigMap(),
						testresource.APIResourceSecret(),
					},
				},
			},
			resourceConfig: nil, // Allow all resources
		},
		{
			name: "skips resources without list/watch verbs",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceWithVerbs("configmaps", "ConfigMap", true, []string{"get", "delete"}), // Missing list/watch
					},
				},
			},
			resourceConfig: nil,
		},
		{
			name: "respects resource config filtering",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceConfigMap(),
						testresource.APIResourceSecret(),
					},
				},
			},
			resourceConfig: func() *utils.ResourceConfig {
				rc := utils.NewResourceConfig(false) // Skip mode
				_ = rc.Parse("v1/Secret")            // Skip secrets
				return rc
			}(),
		},
		{
			name: "discovers apps group resources",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "apps/v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceDeployment(),
						testresource.APIResourceStatefulSet(),
					},
				},
			},
			resourceConfig: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake discovery client
			fakeClient := fake.NewSimpleClientset()
			fakeDiscovery, ok := fakeClient.Discovery().(*fakediscovery.FakeDiscovery)
			if !ok {
				t.Fatal("Failed to cast to FakeDiscovery")
			}
			fakeDiscovery.Resources = tt.discoveryResources

			// Create REST mapper
			groupResources := []*restmapper.APIGroupResources{}
			for _, resourceList := range tt.discoveryResources {
				gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
				if err != nil {
					t.Fatalf("Failed to parse group version: %v", err)
				}

				groupResources = append(groupResources, &restmapper.APIGroupResources{
					Group: metav1.APIGroup{
						Name: gv.Group,
						Versions: []metav1.GroupVersionForDiscovery{
							{GroupVersion: resourceList.GroupVersion, Version: gv.Version},
						},
						PreferredVersion: metav1.GroupVersionForDiscovery{
							GroupVersion: resourceList.GroupVersion,
							Version:      gv.Version,
						},
					},
					VersionedResources: map[string][]metav1.APIResource{
						gv.Version: resourceList.APIResources,
					},
				})
			}
			restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)

			// Create fake informer manager
			fakeInformerManager := &testinformer.FakeManager{
				APIResources: make(map[schema.GroupVersionKind]bool),
			}

			// Track handler additions
			testHandler := cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj any) {},
			}

			// Create ChangeDetector with the interface type
			detector := &ChangeDetector{
				DiscoveryClient: fakeDiscovery,
				RESTMapper:      restMapper,
				InformerManager: fakeInformerManager,
				ResourceConfig:  tt.resourceConfig,
			}

			// Test discoverResources which discovers resources and adds handlers
			detector.discoverResources(testHandler)

			// The main goal is to verify no panics occur during discovery and handler addition
		})
	}
}

func TestChangeDetector_NeedLeaderElection(t *testing.T) {
	detector := &ChangeDetector{}

	// ChangeDetector SHOULD need leader election so only the leader processes events
	if !detector.NeedLeaderElection() {
		t.Error("ChangeDetector should need leader election")
	}
}

func TestChangeDetector_dynamicResourceFilter(t *testing.T) {
	unstructuredConfigMap := func(namespace, name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(utils.ConfigMapKind))
		u.SetNamespace(namespace)
		u.SetName(name)
		return u
	}
	unstructuredClusterRole := func(name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"})
		u.SetName(name)
		return u
	}
	typedSecret := func(namespace, name string) *corev1.Secret {
		return &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		}
	}

	tests := []struct {
		name              string
		obj               any
		skippedNamespaces map[string]bool
		want              bool
	}{
		{
			name: "non-runtime object is filtered out",
			obj:  "not-a-runtime-object",
			want: false,
		},
		{
			name: "nil object is filtered out",
			obj:  nil,
			want: false,
		},
		{
			// Tombstones from informer cache deletions are not unwrapped by ClusterWideKeyFunc,
			// so the filter rejects them. The downstream delete handler unwraps tombstones separately.
			name: "tombstone object is filtered out",
			obj:  cache.DeletedFinalStateUnknown{Key: "default/cm", Obj: unstructuredConfigMap("default", "cm")},
			want: false,
		},
		{
			name: "object in fleet-prefixed namespace is filtered out",
			obj:  unstructuredConfigMap("fleet-system", "cm"),
			want: false,
		},
		{
			name: "object in kube-prefixed namespace is filtered out",
			obj:  unstructuredConfigMap("kube-system", "cm"),
			want: false,
		},
		{
			name:              "object in user-skipped namespace is filtered out",
			obj:               unstructuredConfigMap("skip-me", "cm"),
			skippedNamespaces: map[string]bool{"skip-me": true},
			want:              false,
		},
		{
			name: "unstructured ConfigMap kube-root-ca.crt is filtered out by ShouldPropagateObj",
			obj:  unstructuredConfigMap("default", "kube-root-ca.crt"),
			want: false,
		},
		{
			name: "unstructured ConfigMap with regular name is allowed",
			obj:  unstructuredConfigMap("default", "user-config"),
			want: true,
		},
		{
			// Cluster-scoped objects have an empty namespace; ShouldPropagateNamespace returns true
			// for "" (no reserved prefix match, not in skip-list).
			name: "cluster-scoped unstructured object is allowed",
			obj:  unstructuredClusterRole("admin"),
			want: true,
		},
		{
			// Typed objects bypass the ShouldPropagateObj check because the type assertion to
			// *unstructured.Unstructured fails. In production the dynamic informer only emits
			// unstructured objects, but this case documents the bypass.
			name: "typed object bypasses ShouldPropagateObj and is allowed",
			obj:  typedSecret("default", "my-secret"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeInformerManager := &testinformer.FakeManager{
				APIResources: make(map[schema.GroupVersionKind]bool),
			}
			detector := &ChangeDetector{
				InformerManager:   fakeInformerManager,
				SkippedNamespaces: tt.skippedNamespaces,
			}

			got := detector.dynamicResourceFilter(tt.obj)
			if got != tt.want {
				t.Errorf("dynamicResourceFilter(%v) = %v, want %v", tt.obj, got, tt.want)
			}
		})
	}
}

// TestNewFilteringHandlerOnAllEvents verifies that the helper composes a
// FilteringResourceEventHandler that gates each event type on the supplied filter:
// callbacks fire only when the filter accepts the object.
func TestNewFilteringHandlerOnAllEvents(t *testing.T) {
	cm := func(name string) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}

	tests := []struct {
		name    string
		event   string // "add", "update", or "delete"
		obj     *corev1.ConfigMap
		wantHit bool
	}{
		{name: "add fires when filter accepts", event: "add", obj: cm("keep"), wantHit: true},
		{name: "add skipped when filter rejects", event: "add", obj: cm("drop"), wantHit: false},
		{name: "update fires when filter accepts", event: "update", obj: cm("keep"), wantHit: true},
		{name: "update skipped when filter rejects", event: "update", obj: cm("drop"), wantHit: false},
		{name: "delete fires when filter accepts", event: "delete", obj: cm("keep"), wantHit: true},
		{name: "delete skipped when filter rejects", event: "delete", obj: cm("drop"), wantHit: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var addCalls, updateCalls, deleteCalls int
			filter := func(obj any) bool {
				c, ok := obj.(*corev1.ConfigMap)
				return ok && c.Name == "keep"
			}
			handler := newFilteringHandlerOnAllEvents(filter,
				func(_ any) { addCalls++ },
				func(_, _ any) { updateCalls++ },
				func(_ any) { deleteCalls++ },
			)

			if _, ok := handler.(*cache.FilteringResourceEventHandler); !ok {
				t.Fatalf("newFilteringHandlerOnAllEvents() = %T, want *cache.FilteringResourceEventHandler", handler)
			}

			switch tt.event {
			case "add":
				handler.OnAdd(tt.obj, false)
			case "update":
				handler.OnUpdate(tt.obj, tt.obj)
			case "delete":
				handler.OnDelete(tt.obj)
			}

			gotHit := addCalls+updateCalls+deleteCalls == 1
			if gotHit != tt.wantHit {
				t.Errorf("newFilteringHandlerOnAllEvents() %s callback fired = %v, want %v (counts add=%d update=%d delete=%d)",
					tt.event, gotHit, tt.wantHit, addCalls, updateCalls, deleteCalls)
			}
		})
	}
}
