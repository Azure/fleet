/*
Copyright 2026 The KubeFleet Authors.

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

package resourceeligibility

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var (
	checkerCmpOptions = cmp.Options{
		cmp.AllowUnexported(checker{}, gvkMatcherTrie{}, gvkMatcherTrieNode{}),
		// The REST mapper is a shared dependency rather than owned state.
		cmpopts.IgnoreInterfaces(struct{ meta.RESTMapper }{}),
		cmpopts.EquateEmpty(),
	}

	deploymentGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	configMapGVK  = corev1.SchemeGroupVersion.WithKind(utils.ConfigMapKind)
	nodeGVK       = corev1.SchemeGroupVersion.WithKind("Node")

	deploymentGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	nodeGVR       = corev1.SchemeGroupVersion.WithResource("nodes")
	unknownGVR    = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
)

// newTestRESTMapper returns a REST mapper with a few commonly used resources registered.
func newTestRESTMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion, {Group: "apps", Version: "v1"}})
	m.Add(deploymentGVK, meta.RESTScopeNamespace)
	m.Add(configMapGVK, meta.RESTScopeNamespace)
	m.Add(nodeGVK, meta.RESTScopeRoot)
	return m
}

// fakeRESTMapper is a REST mapper stub that allows the KindsFor results to be set directly.
type fakeRESTMapper struct {
	meta.RESTMapper

	gvks []schema.GroupVersionKind
	err  error
}

func (m *fakeRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return m.gvks, m.err
}

// TestSetGVKCheckList tests the SetGVKCheckList method of the checker type.
func TestSetGVKCheckList(t *testing.T) {
	testCases := []struct {
		name        string
		mode        GVKEligibilityMode
		gvks        []schema.GroupVersionKind
		wantErr     bool
		wantChecker *checker
	}{
		{
			name: "valid GVKs, allow list",
			mode: GVKEligibilityModeAllowList,
			gvks: []schema.GroupVersionKind{deploymentGVK, {Group: "batch"}},
			wantChecker: &checker{
				nsEligibilityMode:  NamespaceEligibilityModeDenyList,
				gvkEligibilityMode: GVKEligibilityModeAllowList,
				customGVKList: gvkMatcherTrie{
					children: map[string]*gvkMatcherTrieNode{
						"apps": node(map[string]*gvkMatcherTrieNode{
							"v1": node(map[string]*gvkMatcherTrieNode{
								"Deployment": leaf(),
							}),
						}),
						"batch": node(map[string]*gvkMatcherTrieNode{
							wildcard: node(map[string]*gvkMatcherTrieNode{
								wildcard: leaf(),
							}),
						}),
					},
				},
			},
		},
		{
			name: "valid GVKs, deny list",
			mode: GVKEligibilityModeDenyList,
			gvks: []schema.GroupVersionKind{nodeGVK},
			wantChecker: &checker{
				nsEligibilityMode:  NamespaceEligibilityModeDenyList,
				gvkEligibilityMode: GVKEligibilityModeDenyList,
				customGVKList: gvkMatcherTrie{
					children: map[string]*gvkMatcherTrieNode{
						"": node(map[string]*gvkMatcherTrieNode{
							"v1": node(map[string]*gvkMatcherTrieNode{
								"Node": leaf(),
							}),
						}),
					},
				},
			},
		},
		{
			name:    "invalid GVK leaves the checker untouched, allow list",
			mode:    GVKEligibilityModeAllowList,
			gvks:    []schema.GroupVersionKind{deploymentGVK, {Group: wildcard}},
			wantErr: true,
			wantChecker: &checker{
				nsEligibilityMode:  NamespaceEligibilityModeDenyList,
				gvkEligibilityMode: GVKEligibilityModeDenyList,
			},
		},
		{
			name:    "invalid GVK leaves the checker untouched, deny list",
			mode:    GVKEligibilityModeDenyList,
			gvks:    []schema.GroupVersionKind{{Group: wildcard, Kind: "Node"}},
			wantErr: true,
			wantChecker: &checker{
				nsEligibilityMode:  NamespaceEligibilityModeDenyList,
				gvkEligibilityMode: GVKEligibilityModeDenyList,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(nil, nil)
			gotErr := c.SetGVKCheckList(tc.mode, tc.gvks)
			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("SetGVKCheckList(%s) error = %v, wantErr %t", tc.mode, gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(c, tc.wantChecker, checkerCmpOptions); diff != "" {
				t.Errorf("checker after SetGVKCheckList(%s) mismatch (-got, +want):\n%s", tc.mode, diff)
			}
		})
	}
}

// TestSetNamespaceCheckList tests the SetNamespaceCheckList method of the checker type.
func TestSetNamespaceCheckList(t *testing.T) {
	c := New(nil, nil)

	c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
	wantAllowListChecker := &checker{
		nsEligibilityMode:  NamespaceEligibilityModeAllowList,
		customNSList:       sets.New("work"),
		customNSPrefixList: sets.New("app-"),
		gvkEligibilityMode: GVKEligibilityModeDenyList,
	}
	if diff := cmp.Diff(c, wantAllowListChecker, checkerCmpOptions); diff != "" {
		t.Errorf("checker after SetNamespaceCheckList(%s) mismatch (-got, +want):\n%s", NamespaceEligibilityModeAllowList, diff)
	}

	// A second call overwrites the mode and the list from the first one.
	c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
	wantDenyListChecker := &checker{
		nsEligibilityMode:  NamespaceEligibilityModeDenyList,
		customNSList:       sets.New("default"),
		customNSPrefixList: sets.New(utils.KubeNSNamePrefix),
		gvkEligibilityMode: GVKEligibilityModeDenyList,
	}
	if diff := cmp.Diff(c, wantDenyListChecker, checkerCmpOptions); diff != "" {
		t.Errorf("checker after SetNamespaceCheckList(%s) mismatch (-got, +want):\n%s", NamespaceEligibilityModeDenyList, diff)
	}
}

// TestSetNamespaceCheckListSkipsEmptyEntries tests that the namespace check list setter of the checker
// type drops empty names and prefixes; an empty prefix would otherwise match every namespace.
func TestSetNamespaceCheckListSkipsEmptyEntries(t *testing.T) {
	c := New(nil, nil)

	c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"", "work"}, []string{"", "app-"})
	wantAllowListChecker := &checker{
		nsEligibilityMode:  NamespaceEligibilityModeAllowList,
		customNSList:       sets.New("work"),
		customNSPrefixList: sets.New("app-"),
		gvkEligibilityMode: GVKEligibilityModeDenyList,
	}
	if diff := cmp.Diff(c, wantAllowListChecker, checkerCmpOptions); diff != "" {
		t.Errorf("checker after SetNamespaceCheckList(%s) mismatch (-got, +want):\n%s", NamespaceEligibilityModeAllowList, diff)
	}

	c = New(nil, nil)

	c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{""}, []string{""})
	wantDenyListChecker := &checker{
		nsEligibilityMode:  NamespaceEligibilityModeDenyList,
		customNSList:       sets.New[string](),
		customNSPrefixList: sets.New[string](),
		gvkEligibilityMode: GVKEligibilityModeDenyList,
	}
	if diff := cmp.Diff(c, wantDenyListChecker, checkerCmpOptions); diff != "" {
		t.Errorf("checker after SetNamespaceCheckList(%s) mismatch (-got, +want):\n%s", NamespaceEligibilityModeDenyList, diff)
	}
	if got := c.AllowsNamespace("work"); !got {
		t.Errorf("AllowsNamespace(work) = %t, want %t", got, true)
	}
}

// TestCheckerWraps tests the Wraps method of the checker type.
func TestCheckerWraps(t *testing.T) {
	inner := New(nil, nil)
	if err := inner.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
		t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
	}

	c := New(nil, nil)
	if got := c.AllowsGVK(deploymentGVK); !got {
		t.Fatalf("AllowsGVK(%v) = %t, want %t", deploymentGVK, got, true)
	}

	c.Wraps(inner)
	if got := c.AllowsGVK(deploymentGVK); got {
		t.Errorf("AllowsGVK(%v) = %t, want %t", deploymentGVK, got, false)
	}
}

// TestCheckerDeepCopy tests the DeepCopy method of the checker type.
func TestCheckerDeepCopy(t *testing.T) {
	restMapper := newTestRESTMapper()
	c := New(restMapper, Baseline(restMapper))
	c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
	if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
		t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
	}

	cp := c.DeepCopy()
	if diff := cmp.Diff(cp, c, checkerCmpOptions); diff != "" {
		t.Fatalf("DeepCopy() mismatch (-got, +want):\n%s", diff)
	}

	// Mutate the copy; the original checker should stay intact.
	cp.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, nil)
	if err := cp.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{configMapGVK}); err != nil {
		t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
	}
	if got := c.AllowsNamespace("work-1"); !got {
		t.Errorf("AllowsNamespace(work-1) on the original checker = %t, want %t", got, true)
	}
	if got := c.AllowsGVK(configMapGVK); !got {
		t.Errorf("AllowsGVK(%v) on the original checker = %t, want %t", configMapGVK, got, true)
	}
	if got := c.AllowsGVK(deploymentGVK); got {
		t.Errorf("AllowsGVK(%v) on the original checker = %t, want %t", deploymentGVK, got, false)
	}

	// Mutate the wrapped checker of the copy; the original checker should stay intact.
	cpChecker, ok := cp.(*checker)
	if !ok {
		t.Fatalf("DeepCopy() returned a checker of type %T, want *checker", cp)
	}
	cpChecker.wrapped.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
	if got := c.AllowsNamespace("work"); !got {
		t.Errorf("AllowsNamespace(work) on the original checker = %t, want %t", got, true)
	}
}

// TestAllowsNamespace tests the AllowsNamespace method of the checker type.
func TestAllowsNamespace(t *testing.T) {
	testCases := []struct {
		name      string
		checker   Checker
		namespace string
		want      bool
	}{
		{
			name:      "no list configured",
			checker:   New(nil, nil),
			namespace: "work",
			want:      true,
		},
		{
			name: "deny list, name match",
			checker: func() Checker {
				c := New(nil, nil)
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
				return c
			}(),
			namespace: "default",
			want:      false,
		},
		{
			name: "deny list, prefix match",
			checker: func() Checker {
				c := New(nil, nil)
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
				return c
			}(),
			namespace: "kube-system",
			want:      false,
		},
		{
			name: "deny list, no match",
			checker: func() Checker {
				c := New(nil, nil)
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
				return c
			}(),
			namespace: "work",
			want:      true,
		},
		{
			name: "allow list, name match",
			checker: func() Checker {
				c := New(nil, nil)
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
				return c
			}(),
			namespace: "work",
			want:      true,
		},
		{
			name: "allow list, prefix match",
			checker: func() Checker {
				c := New(nil, nil)
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
				return c
			}(),
			namespace: "app-1",
			want:      true,
		},
		{
			name: "allow list, no match",
			checker: func() Checker {
				c := New(nil, nil)
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
				return c
			}(),
			namespace: "other",
			want:      false,
		},
		{
			name: "wrapped checker further restricts the eligibility",
			checker: func() Checker {
				inner := New(nil, nil)
				inner.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
				c := New(nil, inner)
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work", "app"}, nil)
				return c
			}(),
			namespace: "work",
			want:      false,
		},
		{
			name: "wrapped checker allows the namespace",
			checker: func() Checker {
				inner := New(nil, nil)
				inner.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
				c := New(nil, inner)
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work", "app"}, nil)
				return c
			}(),
			namespace: "app",
			want:      true,
		},
		{
			name:      "baseline checker denies the fleet reserved namespaces",
			checker:   Baseline(nil),
			namespace: utils.FleetSystemNamespace,
			want:      false,
		},
		{
			name:      "allow all checker",
			checker:   AllowAll(nil),
			namespace: "kube-system",
			want:      true,
		},
		{
			// Cluster-scoped resources carry an empty namespace; they stay eligible even when the
			// configured allow list does not name them.
			name: "empty namespace is always eligible",
			checker: func() Checker {
				c := New(nil, Baseline(nil))
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, nil)
				return c
			}(),
			namespace: "",
			want:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.checker.AllowsNamespace(tc.namespace); got != tc.want {
				t.Errorf("AllowsNamespace(%s) = %t, want %t", tc.namespace, got, tc.want)
			}
		})
	}
}

// TestAllowsGVK tests the AllowsGVK method of the checker type.
func TestAllowsGVK(t *testing.T) {
	testCases := []struct {
		name    string
		checker Checker
		gvk     schema.GroupVersionKind
		want    bool
	}{
		{
			name:    "no list configured",
			checker: New(nil, nil),
			gvk:     deploymentGVK,
			want:    true,
		},
		{
			name: "deny list, match",
			checker: func() Checker {
				c := New(nil, nil)
				if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{nodeGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  nodeGVK,
			want: false,
		},
		{
			name: "deny list, no match",
			checker: func() Checker {
				c := New(nil, nil)
				if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{nodeGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  configMapGVK,
			want: true,
		},
		{
			name: "allow list, match",
			checker: func() Checker {
				c := New(nil, nil)
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  deploymentGVK,
			want: true,
		},
		{
			name: "allow list, no match",
			checker: func() Checker {
				c := New(nil, nil)
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  configMapGVK,
			want: false,
		},
		{
			name: "wrapped checker further restricts the eligibility",
			checker: func() Checker {
				inner := New(nil, nil)
				if err := inner.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
				}
				c := New(nil, inner)
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{deploymentGVK, configMapGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  deploymentGVK,
			want: false,
		},
		{
			name:    "baseline checker denies node resources",
			checker: Baseline(nil),
			gvk:     nodeGVK,
			want:    false,
		},
		{
			name:    "baseline checker denies the fleet cluster API group",
			checker: Baseline(nil),
			gvk:     clusterv1beta1.GroupVersion.WithKind("MemberCluster"),
			want:    false,
		},
		{
			name:    "baseline checker denies the fleet networking resources",
			checker: Baseline(nil),
			gvk:     schema.GroupVersionKind{Group: utils.NetworkingGroupName, Version: "v1alpha1", Kind: "ServiceImport"},
			want:    false,
		},
		{
			name:    "baseline checker allows other resources in the fleet networking API group",
			checker: Baseline(nil),
			gvk:     schema.GroupVersionKind{Group: utils.NetworkingGroupName, Version: "v1alpha1", Kind: "InternalServiceExport"},
			want:    true,
		},
		{
			name:    "baseline checker allows regular resources",
			checker: Baseline(nil),
			gvk:     deploymentGVK,
			want:    true,
		},
		{
			name:    "v0 default checker allows envelope resources",
			checker: DefaultForV0APIs(nil),
			gvk:     placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourceEnvelopeKind),
			want:    true,
		},
		{
			name:    "v1 default checker denies envelope resources",
			checker: DefaultForV1APIs(nil),
			gvk:     placementv1beta1.GroupVersion.WithKind(placementv1beta1.ResourceEnvelopeKind),
			want:    false,
		},
		{
			name:    "v1 default checker denies endpoints resources",
			checker: DefaultForV1APIs(nil),
			gvk:     corev1.SchemeGroupVersion.WithKind("Endpoints"),
			want:    false,
		},
		{
			name:    "v1 default checker inherits the baseline restrictions",
			checker: DefaultForV1APIs(nil),
			gvk:     nodeGVK,
			want:    false,
		},
		{
			name:    "allow all checker",
			checker: AllowAll(nil),
			gvk:     nodeGVK,
			want:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.checker.AllowsGVK(tc.gvk); got != tc.want {
				t.Errorf("AllowsGVK(%v) = %t, want %t", tc.gvk, got, tc.want)
			}
		})
	}
}

// TestAllowsGVR tests the AllowsGVR method of the checker type.
func TestAllowsGVR(t *testing.T) {
	testCases := []struct {
		name        string
		checker     Checker
		gvr         schema.GroupVersionResource
		want        bool
		wantErr     bool
		wantNoMatch bool
	}{
		{
			name:    "eligible resource",
			checker: Baseline(newTestRESTMapper()),
			gvr:     deploymentGVR,
			want:    true,
		},
		{
			name:    "ineligible resource",
			checker: Baseline(newTestRESTMapper()),
			gvr:     nodeGVR,
			want:    false,
		},
		{
			name:    "allow all checker",
			checker: AllowAll(newTestRESTMapper()),
			gvr:     nodeGVR,
			want:    true,
		},
		{
			name:        "unregistered resource",
			checker:     Baseline(newTestRESTMapper()),
			gvr:         unknownGVR,
			want:        false,
			wantErr:     true,
			wantNoMatch: true,
		},
		{
			name:        "no kind is mapped to the resource",
			checker:     Baseline(&fakeRESTMapper{}),
			gvr:         deploymentGVR,
			want:        false,
			wantErr:     true,
			wantNoMatch: true,
		},
		{
			name:    "REST mapper failure",
			checker: Baseline(&fakeRESTMapper{err: errors.New("mapper failure")}),
			gvr:     deploymentGVR,
			want:    false,
			wantErr: true,
		},
		{
			name: "multiple kinds are mapped to the resource, all eligible",
			checker: Baseline(&fakeRESTMapper{
				gvks: []schema.GroupVersionKind{deploymentGVK, configMapGVK},
			}),
			gvr:  deploymentGVR,
			want: true,
		},
		{
			name: "multiple kinds are mapped to the resource, one ineligible",
			checker: Baseline(&fakeRESTMapper{
				gvks: []schema.GroupVersionKind{deploymentGVK, nodeGVK},
			}),
			gvr:  deploymentGVR,
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.checker.AllowsGVR(tc.gvr)
			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("AllowsGVR(%v) error = %v, wantErr %t", tc.gvr, gotErr, tc.wantErr)
			}
			if gotNoMatch := meta.IsNoMatchError(gotErr); gotNoMatch != tc.wantNoMatch {
				t.Errorf("IsNoMatchError(AllowsGVR(%v)) = %t, want %t", tc.gvr, gotNoMatch, tc.wantNoMatch)
			}
			if got != tc.want {
				t.Errorf("AllowsGVR(%v) = %t, want %t", tc.gvr, got, tc.want)
			}
		})
	}
}

// TestAllowsObject tests the AllowsObject method of the checker type.
func TestAllowsObject(t *testing.T) {
	now := metav1.Now()

	configMapObj := func(name string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(configMapGVK)
		obj.SetNamespace("work")
		obj.SetName(name)
		return obj
	}

	testCases := []struct {
		name    string
		checker Checker
		obj     *unstructured.Unstructured
		want    bool
		wantErr bool
	}{
		{
			name:    "regular resource",
			checker: DefaultForV1APIs(nil),
			obj:     configMapObj("app-config"),
			want:    true,
		},
		{
			name:    "resource being deleted",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetDeletionTimestamp(&now)
				return obj
			}(),
			want:    false,
			wantErr: true,
		},
		{
			name:    "resource owned by another object",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetOwnerReferences([]metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "app",
						UID:        "1",
					},
				})
				return obj
			}(),
			want:    false,
			wantErr: true,
		},
		{
			name:    "built-in CA certificate config map",
			checker: DefaultForV1APIs(nil),
			obj:     configMapObj("kube-root-ca.crt"),
			want:    false,
			wantErr: true,
		},
		{
			name:    "default service account",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))
				obj.SetNamespace("work")
				obj.SetName("default")
				return obj
			}(),
			want:    false,
			wantErr: true,
		},
		{
			name:    "non-default service account",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))
				obj.SetNamespace("work")
				obj.SetName("app")
				return obj
			}(),
			want: true,
		},
		{
			name:    "Kubernetes API service",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(utils.ServiceKind))
				obj.SetNamespace("default")
				obj.SetName("kubernetes")
				return obj
			}(),
			want:    false,
			wantErr: true,
		},
		{
			name:    "service of the same name in another namespace",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(utils.ServiceKind))
				obj.SetNamespace("work")
				obj.SetName("kubernetes")
				return obj
			}(),
			want: true,
		},
		{
			name:    "service account token secret",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"type": string(corev1.SecretTypeServiceAccountToken),
					},
				}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
				obj.SetNamespace("work")
				obj.SetName("app-token")
				return obj
			}(),
			want:    false,
			wantErr: true,
		},
		{
			name:    "regular secret",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"type": string(corev1.SecretTypeOpaque),
					},
				}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
				obj.SetNamespace("work")
				obj.SetName("app-secret")
				return obj
			}(),
			want: true,
		},
		{
			name:    "secret with a malformed type field",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"type": int64(1),
					},
				}
				obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
				obj.SetNamespace("work")
				obj.SetName("app-secret")
				return obj
			}(),
			want:    false,
			wantErr: true,
		},
		{
			name:    "wrapped checker further restricts the eligibility",
			checker: New(nil, &alwaysIneligibleChecker{}),
			obj:     configMapObj("app-config"),
			want:    false,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.checker.AllowsObject(tc.obj)
			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("AllowsObject() error = %v, wantErr %t", gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("AllowsObject() = %t, want %t", got, tc.want)
			}
		})
	}
}

// alwaysIneligibleChecker is a Checker stub that rejects every resource object.
type alwaysIneligibleChecker struct {
	Checker
}

func (c *alwaysIneligibleChecker) AllowsObject(_ *unstructured.Unstructured) (bool, error) {
	return false, errors.New("ineligible")
}
