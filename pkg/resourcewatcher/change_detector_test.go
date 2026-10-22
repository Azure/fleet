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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resourceeligibility"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
	testresource "github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

func TestChangeDetector_discoverResources(t *testing.T) {
	tests := []struct {
		name               string
		discoveryResources []*metav1.APIResourceList
		v0Checker          func(t *testing.T) resourceeligibility.Checker
		v1Checker          func(t *testing.T) resourceeligibility.Checker
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
			v0Checker: func(_ *testing.T) resourceeligibility.Checker { return resourceeligibility.AllowAll(nil) },
			v1Checker: func(_ *testing.T) resourceeligibility.Checker { return resourceeligibility.AllowAll(nil) },
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
			v0Checker: func(_ *testing.T) resourceeligibility.Checker { return resourceeligibility.AllowAll(nil) },
			v1Checker: func(_ *testing.T) resourceeligibility.Checker { return resourceeligibility.AllowAll(nil) },
		},
		{
			name: "respects eligibility checker filtering",
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						testresource.APIResourceConfigMap(),
						testresource.APIResourceSecret(),
					},
				},
			},
			v0Checker: func(t *testing.T) resourceeligibility.Checker {
				return secretDenyingChecker(t, resourceeligibility.DefaultForV0APIs(nil))
			},
			v1Checker: func(t *testing.T) resourceeligibility.Checker {
				return secretDenyingChecker(t, resourceeligibility.DefaultForV1APIs(nil))
			},
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
			v0Checker: func(_ *testing.T) resourceeligibility.Checker { return resourceeligibility.AllowAll(nil) },
			v1Checker: func(_ *testing.T) resourceeligibility.Checker { return resourceeligibility.AllowAll(nil) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake discovery client
			fakeClient := fake.NewClientset()
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
				DiscoveryClient:                     fakeDiscovery,
				RESTMapper:                          restMapper,
				InformerManager:                     fakeInformerManager,
				ResourceEligibilityCheckerForV0APIs: tt.v0Checker(t),
				ResourceEligibilityCheckerForV1APIs: tt.v1Checker(t),
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
	unstructuredReplicaSetWithNonControllerOwner := func(namespace, name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind(utils.ReplicaSetKind))
		u.SetNamespace(namespace)
		u.SetName(name)
		u.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "v1", Kind: utils.ConfigMapKind, Name: "owner"}})
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

	// Mirrors the namespace deny list the hub agent builds in production.
	newChecker := func(skippedNSNames ...string) resourceeligibility.Checker {
		c := resourceeligibility.New(resourceeligibility.CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
		c.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, skippedNSNames, []string{utils.KubeNSNamePrefix, utils.FleetNSNamePrefix})
		return c
	}

	tests := []struct {
		name string
		obj  any
		// v0Checker defaults to newChecker() when unset.
		v0Checker resourceeligibility.Checker
		v1Checker resourceeligibility.Checker
		want      bool
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
			// A relist-detected deletion arrives as a tombstone wrapping the object's final state.
			// The filter must judge the wrapped object, not the tombstone: rejecting tombstones
			// wholesale would silently drop every such deletion before the delete handler -- which
			// is what unwraps them for use -- ever saw it.
			name: "tombstone wrapping a watched object passes the filter",
			obj:  cache.DeletedFinalStateUnknown{Key: "default/cm", Obj: unstructuredConfigMap("default", "cm")},
			want: true,
		},
		{
			name: "tombstone wrapping an object in a skipped namespace is filtered out",
			obj:  cache.DeletedFinalStateUnknown{Key: "kube-system/cm", Obj: unstructuredConfigMap("kube-system", "cm")},
			want: false,
		},
		{
			name: "tombstone wrapping garbage is filtered out",
			obj:  cache.DeletedFinalStateUnknown{Key: "default/cm", Obj: "not-a-runtime-object"},
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
			name:      "object in user-skipped namespace is filtered out",
			obj:       unstructuredConfigMap("skip-me", "cm"),
			v0Checker: newChecker("skip-me"),
			want:      false,
		},
		{
			// Only the v0 APIs' checker is consulted for namespace eligibility.
			name:      "object in user-skipped namespace is filtered out even if it is eligible for the v1 APIs",
			obj:       unstructuredConfigMap("skip-me", "cm"),
			v0Checker: newChecker("skip-me"),
			v1Checker: resourceeligibility.AllowAll(nil),
			want:      false,
		},
		{
			name: "unstructured ConfigMap kube-root-ca.crt is filtered out by ShouldPropagateObj when no v1 APIs checker is set",
			obj:  unstructuredConfigMap("default", "kube-root-ca.crt"),
			want: false,
		},
		{
			name:      "unstructured ConfigMap kube-root-ca.crt is filtered out when the v1 APIs reject the object too",
			obj:       unstructuredConfigMap("default", "kube-root-ca.crt"),
			v1Checker: resourceeligibility.AllowAll(nil),
			want:      false,
		},
		{
			// ShouldPropagateObj rejects a ReplicaSet with any owner; the v1 APIs reject only controller owners.
			name:      "object rejected by ShouldPropagateObj is allowed when the v1 APIs accept it",
			obj:       unstructuredReplicaSetWithNonControllerOwner("default", "rs"),
			v1Checker: resourceeligibility.AllowAll(nil),
			want:      true,
		},
		{
			name:      "object rejected by ShouldPropagateObj is filtered out when its GVK is ineligible for the v1 APIs",
			obj:       unstructuredReplicaSetWithNonControllerOwner("default", "rs"),
			v1Checker: gvkDenyingChecker(t, appsv1.SchemeGroupVersion.WithKind(utils.ReplicaSetKind)),
			want:      false,
		},
		{
			name: "unstructured ConfigMap with regular name is allowed",
			obj:  unstructuredConfigMap("default", "user-config"),
			want: true,
		},
		{
			// Cluster-scoped objects have an empty namespace, which matches no reserved prefix and
			// no deny list entry.
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
			v0Checker := tt.v0Checker
			if v0Checker == nil {
				v0Checker = newChecker()
			}
			detector := &ChangeDetector{
				InformerManager:                     fakeInformerManager,
				ResourceEligibilityCheckerForV0APIs: v0Checker,
				ResourceEligibilityCheckerForV1APIs: tt.v1Checker,
			}

			got := detector.dynamicResourceFilter(tt.obj)
			if got != tt.want {
				t.Errorf("dynamicResourceFilter(%v) = %v, want %v", tt.obj, got, tt.want)
			}
		})
	}
}

// TestChangeDetector_dynamicResourceFilter_PlacementPolicyAPIs verifies the filter with the built-in checkers, wired
// as in production. With the placement policy (v1) APIs disabled, the v1 checker is unset and ShouldPropagateObj alone
// decides, as it did before the v1 APIs were added; with them enabled, an object that ShouldPropagateObj rejects
// still passes if the v1 APIs accept both its GVK and the object itself.
func TestChangeDetector_dynamicResourceFilter_PlacementPolicyAPIs(t *testing.T) {
	newObj := func(apiVersion, kind, namespace, name string, ownerRefs ...metav1.OwnerReference) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetAPIVersion(apiVersion)
		u.SetKind(kind)
		u.SetNamespace(namespace)
		u.SetName(name)
		u.SetOwnerReferences(ownerRefs)
		return u
	}
	isController := true
	appsGroupVersion := appsv1.SchemeGroupVersion.String()
	deploymentOwner := metav1.OwnerReference{APIVersion: appsGroupVersion, Kind: "Deployment", Name: "web", UID: "uid", Controller: &isController}
	nonControllerOwner := metav1.OwnerReference{APIVersion: "v1", Kind: utils.ConfigMapKind, Name: "owner", UID: "uid"}
	deleting := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		now := metav1.Now()
		u.SetDeletionTimestamp(&now)
		return u
	}

	tests := []struct {
		name           string
		obj            *unstructured.Unstructured
		wantV1Disabled bool
		wantV1Enabled  bool
	}{
		{
			name:           "regular config map",
			obj:            newObj("v1", "ConfigMap", "app", "user-config"),
			wantV1Disabled: true,
			wantV1Enabled:  true,
		},
		{
			name:           "kube-root-ca.crt config map",
			obj:            newObj("v1", "ConfigMap", "app", "kube-root-ca.crt"),
			wantV1Disabled: false,
			wantV1Enabled:  false,
		},
		{
			name:           "default service account",
			obj:            newObj("v1", "ServiceAccount", "app", "default"),
			wantV1Disabled: false,
			wantV1Enabled:  false,
		},
		{
			name:           "replica set owned by a deployment",
			obj:            newObj(appsGroupVersion, "ReplicaSet", "app", "web-abc", deploymentOwner),
			wantV1Disabled: false,
			wantV1Enabled:  false,
		},
		{
			// ShouldPropagateObj rejects a ReplicaSet with any owner; the v1 APIs reject only controller owners.
			name:           "replica set with a non-controller owner",
			obj:            newObj(appsGroupVersion, "ReplicaSet", "app", "rs-owned", nonControllerOwner),
			wantV1Disabled: false,
			wantV1Enabled:  true,
		},
		{
			name:           "replica set with a non-controller owner being deleted",
			obj:            deleting(newObj(appsGroupVersion, "ReplicaSet", "app", "rs-owned", nonControllerOwner)),
			wantV1Disabled: false,
			wantV1Enabled:  false,
		},
		{
			name:           "standalone replica set",
			obj:            newObj(appsGroupVersion, "ReplicaSet", "app", "rs"),
			wantV1Disabled: true,
			wantV1Enabled:  true,
		},
		{
			// The v1 APIs deny endpoints altogether.
			name:           "endpoints backed by a service",
			obj:            newObj("v1", "Endpoints", "app", "svc"),
			wantV1Disabled: false,
			wantV1Enabled:  false,
		},
		{
			name:           "standalone endpoints",
			obj:            newObj("v1", "Endpoints", "app", "standalone"),
			wantV1Disabled: true,
			wantV1Enabled:  true,
		},
		{
			name:           "config map in a reserved namespace",
			obj:            newObj("v1", "ConfigMap", "kube-system", "cm"),
			wantV1Disabled: false,
			wantV1Enabled:  false,
		},
	}

	for _, tt := range tests {
		for _, v1Enabled := range []bool{false, true} {
			subtestName := tt.name + "/placement policy APIs disabled"
			want := tt.wantV1Disabled
			if v1Enabled {
				subtestName = tt.name + "/placement policy APIs enabled"
				want = tt.wantV1Enabled
			}
			t.Run(subtestName, func(t *testing.T) {
				fakeInformerManager := &testinformer.FakeManager{
					APIResources: make(map[schema.GroupVersionKind]bool),
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ServiceGVR: {Objects: []runtime.Object{newObj("v1", "Service", "app", "svc")}},
					},
				}
				detector := &ChangeDetector{
					InformerManager:                     fakeInformerManager,
					ResourceEligibilityCheckerForV0APIs: resourceeligibility.DefaultForV0APIs(nil),
				}
				if v1Enabled {
					detector.ResourceEligibilityCheckerForV1APIs = resourceeligibility.DefaultForV1APIs(nil)
				}

				if got := detector.dynamicResourceFilter(tt.obj); got != want {
					t.Errorf("dynamicResourceFilter(%s %s/%s) = %v, want %v", tt.obj.GetKind(), tt.obj.GetNamespace(), tt.obj.GetName(), got, want)
				}
			})
		}
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
