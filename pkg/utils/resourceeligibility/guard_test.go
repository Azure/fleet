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
	"k8s.io/utils/ptr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kfrolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
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
			c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
	c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})

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
	c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})

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

	c = New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})

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
	inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
	if err := inner.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
		t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
	}

	c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
	c := New(CheckerOptions{RestMapper: restMapper, Wrapped: Baseline(restMapper), SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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

	c = New(CheckerOptions{RestMapper: restMapper, SkipNSCheck: true, SkipGVKRCheck: true, SkipObjectCheck: true})
	cp = c.DeepCopy()
	if diff := cmp.Diff(cp, c, checkerCmpOptions); diff != "" {
		t.Fatalf("DeepCopy() with skip flags mismatch (-got, +want):\n%s", diff)
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
			checker:   New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false}),
			namespace: "work",
			want:      true,
		},
		{
			name:      "skip check without wrapped checker allows everything in deny list mode",
			checker:   New(CheckerOptions{RestMapper: nil, SkipNSCheck: true, SkipGVKRCheck: false, SkipObjectCheck: false}),
			namespace: "work",
			want:      true,
		},
		{
			name: "skip check delegates to wrapped checker",
			checker: func() Checker {
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c := New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: true, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
				return c
			}(),
			namespace: "work",
			want:      true,
		},
		{
			name: "skip check without wrapped checker denies everything in allow list mode",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: true, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, nil)
				return c
			}(),
			namespace: "work",
			want:      false,
		},
		{
			name:      "skip check without wrapped checker allows the empty namespace",
			checker:   New(CheckerOptions{RestMapper: nil, SkipNSCheck: true, SkipGVKRCheck: false, SkipObjectCheck: false}),
			namespace: "",
			want:      true,
		},
		{
			name: "skip check delegates to wrapped checker that denies the namespace",
			checker: func() Checker {
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				inner.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
				return New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: true, SkipGVKRCheck: false, SkipObjectCheck: false})
			}(),
			namespace: "work",
			want:      false,
		},
		{
			name: "deny list, name match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
				return c
			}(),
			namespace: "default",
			want:      false,
		},
		{
			name: "deny list, prefix match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
				return c
			}(),
			namespace: "kube-system",
			want:      false,
		},
		{
			name: "deny list, no match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"default"}, []string{utils.KubeNSNamePrefix})
				return c
			}(),
			namespace: "work",
			want:      true,
		},
		{
			name: "allow list, name match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
				return c
			}(),
			namespace: "work",
			want:      true,
		},
		{
			name: "allow list, prefix match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
				return c
			}(),
			namespace: "app-1",
			want:      true,
		},
		{
			name: "allow list, no match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work"}, []string{"app-"})
				return c
			}(),
			namespace: "other",
			want:      false,
		},
		{
			name: "wrapped checker further restricts the eligibility",
			checker: func() Checker {
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				inner.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
				c := New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c.SetNamespaceCheckList(NamespaceEligibilityModeAllowList, []string{"work", "app"}, nil)
				return c
			}(),
			namespace: "work",
			want:      false,
		},
		{
			name: "wrapped checker allows the namespace",
			checker: func() Checker {
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				inner.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
				c := New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
			// The default namespace itself is blocked by the object check; resources within it are eligible.
			name:      "baseline checker allows the default namespace",
			checker:   Baseline(nil),
			namespace: "default",
			want:      true,
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
				c := New(CheckerOptions{RestMapper: nil, Wrapped: Baseline(nil), SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
			checker: New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false}),
			gvk:     deploymentGVK,
			want:    true,
		},
		{
			name:    "skip check without wrapped checker allows everything in deny list mode",
			checker: New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: false}),
			gvk:     deploymentGVK,
			want:    true,
		},
		{
			name: "skip check delegates to wrapped checker",
			checker: func() Checker {
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				c := New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: false})
				if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  deploymentGVK,
			want: true,
		},
		{
			name: "skip check without wrapped checker denies everything in allow list mode",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: false})
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  deploymentGVK,
			want: false,
		},
		{
			name: "skip check delegates to wrapped checker that denies the GVK",
			checker: func() Checker {
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				if err := inner.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
				}
				return New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: false})
			}(),
			gvk:  deploymentGVK,
			want: false,
		},
		{
			name: "deny list, match",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
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
				inner := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				if err := inner.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
				}
				c := New(CheckerOptions{RestMapper: nil, Wrapped: inner, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{deploymentGVK, configMapGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  deploymentGVK,
			want: false,
		},
		{
			name: "custom allow list for the core API group does not override the wrapped baseline checker",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, Wrapped: Baseline(nil), SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{{Group: corev1.GroupName}}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  nodeGVK,
			want: false,
		},
		{
			name: "custom allow list for the core API group allows other core resources via the wrapped baseline checker",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, Wrapped: Baseline(nil), SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false})
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{{Group: corev1.GroupName}}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvk:  configMapGVK,
			want: true,
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
			name:    "baseline checker denies the placement policy API group",
			checker: Baseline(nil),
			gvk:     kfplacementv1alpha1.GroupVersion.WithKind(kfplacementv1alpha1.PlacementPolicyKind),
			want:    false,
		},
		{
			name:    "baseline checker denies the staged update run API group",
			checker: Baseline(nil),
			gvk:     kfrolloutv1alpha1.GroupVersion.WithKind(kfrolloutv1alpha1.StagedUpdateRunKind),
			want:    false,
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
			name:    "skip check without wrapped checker allows everything in deny list mode",
			checker: New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: false}),
			gvr:     deploymentGVR,
			want:    true,
		},
		{
			name: "skip check delegates to wrapped checker",
			checker: New(CheckerOptions{
				RestMapper:      &fakeRESTMapper{err: errors.New("outer mapper must not be called")},
				Wrapped:         Baseline(newTestRESTMapper()),
				SkipNSCheck:     false,
				SkipGVKRCheck:   true,
				SkipObjectCheck: false,
			}),
			gvr:  deploymentGVR,
			want: true,
		},
		{
			name: "skip check without wrapped checker denies everything in allow list mode",
			checker: func() Checker {
				c := New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: false})
				if err := c.SetGVKCheckList(GVKEligibilityModeAllowList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
					t.Fatalf("SetGVKCheckList(AllowList) = %v, want no error", err)
				}
				return c
			}(),
			gvr:  deploymentGVR,
			want: false,
		},
		{
			name: "skip check propagates the error of the wrapped checker",
			checker: New(CheckerOptions{
				RestMapper:      newTestRESTMapper(),
				Wrapped:         Baseline(&fakeRESTMapper{err: errors.New("mapper failure")}),
				SkipNSCheck:     false,
				SkipGVKRCheck:   true,
				SkipObjectCheck: false,
			}),
			gvr:     deploymentGVR,
			want:    false,
			wantErr: true,
		},
		{
			name: "wrapped checker further restricts the eligibility",
			checker: New(CheckerOptions{
				RestMapper:      newTestRESTMapper(),
				Wrapped:         Baseline(newTestRESTMapper()),
				SkipNSCheck:     false,
				SkipGVKRCheck:   false,
				SkipObjectCheck: false,
			}),
			gvr:  nodeGVR,
			want: false,
		},
		{
			name: "wrapped checker error is propagated",
			checker: New(CheckerOptions{
				RestMapper:      newTestRESTMapper(),
				Wrapped:         Baseline(&fakeRESTMapper{err: errors.New("mapper failure")}),
				SkipNSCheck:     false,
				SkipGVKRCheck:   false,
				SkipObjectCheck: false,
			}),
			gvr:     deploymentGVR,
			want:    false,
			wantErr: true,
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
		reason  string
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
			want:   false,
			reason: "the resource is being deleted and cannot be placed",
		},
		{
			name:    "skip check without wrapped checker allows otherwise ineligible object",
			checker: New(CheckerOptions{RestMapper: nil, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: true}),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetDeletionTimestamp(&now)
				return obj
			}(),
			want: true,
		},
		{
			name:    "skip check delegates to wrapped checker",
			checker: New(CheckerOptions{RestMapper: nil, Wrapped: &alwaysIneligibleChecker{}, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: true}),
			obj:     configMapObj("app-config"),
			want:    false,
			reason:  "ineligible",
		},
		{
			name:    "baseline checker rejects a resource being deleted",
			checker: Baseline(nil),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetDeletionTimestamp(&now)
				return obj
			}(),
			want:   false,
			reason: "the resource is being deleted and cannot be placed",
		},
		{
			name:    "baseline checker that skips the object check allows a resource being deleted",
			checker: baseline(nil, true),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetDeletionTimestamp(&now)
				return obj
			}(),
			want: true,
		},
		{
			name:    "v0 default checker allows a resource being deleted",
			checker: DefaultForV0APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetDeletionTimestamp(&now)
				return obj
			}(),
			want: true,
		},
		{
			name:    "v0 default checker allows a resource owned by another object",
			checker: DefaultForV0APIs(nil),
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
			want: true,
		},
		{
			name:    "skip check propagates the error of the wrapped checker",
			checker: New(CheckerOptions{RestMapper: nil, Wrapped: &failingChecker{}, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: true}),
			obj:     configMapObj("app-config"),
			want:    false,
			wantErr: true,
		},
		{
			name:    "wrapped checker error is propagated",
			checker: New(CheckerOptions{RestMapper: nil, Wrapped: &failingChecker{}, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false}),
			obj:     configMapObj("app-config"),
			want:    false,
			wantErr: true,
		},
		{
			name:    "resource owned by a controller",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := configMapObj("app-config")
				obj.SetOwnerReferences([]metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "app",
						UID:        "1",
						Controller: ptr.To(true),
					},
				})
				return obj
			}(),
			want:   false,
			reason: "the resource is under the management of a controller; place the owner object instead if applicable",
		},
		{
			name:    "resource owned by another object that is not its controller",
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
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "another-app",
						UID:        "2",
						Controller: ptr.To(false),
					},
				})
				return obj
			}(),
			want: true,
		},
		{
			name:    "default namespace",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(utils.NamespaceGVK)
				obj.SetName("default")
				return obj
			}(),
			want:   false,
			reason: "the default namespace itself cannot be placed",
		},
		{
			name:    "non-default namespace",
			checker: DefaultForV1APIs(nil),
			obj: func() *unstructured.Unstructured {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(utils.NamespaceGVK)
				obj.SetName("work")
				return obj
			}(),
			want: true,
		},
		{
			name:    "built-in CA certificate config map",
			checker: DefaultForV1APIs(nil),
			obj:     configMapObj("kube-root-ca.crt"),
			want:    false,
			reason:  "the built-in CA certificate config map is created automatically in every namespace and cannot be placed",
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
			want:   false,
			reason: "the default service account is created automatically in every namespace and cannot be placed",
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
			want:   false,
			reason: "the Kubernetes API service is created automatically in the default namespace and cannot be placed",
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
			want:   false,
			reason: "service account token secrets are created automatically along with their service accounts and cannot be placed",
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
			checker: New(CheckerOptions{RestMapper: nil, Wrapped: &alwaysIneligibleChecker{}, SkipNSCheck: false, SkipGVKRCheck: false, SkipObjectCheck: false}),
			obj:     configMapObj("app-config"),
			want:    false,
			reason:  "ineligible",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotReason, gotErr := tc.checker.AllowsObject(tc.obj)
			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("AllowsObject() error = %v, wantErr %t", gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("AllowsObject() = %t, want %t", got, tc.want)
			}
			if gotReason != tc.reason {
				t.Errorf("AllowsObject() reason = %q, want %q", gotReason, tc.reason)
			}
		})
	}
}

// alwaysIneligibleChecker is a Checker stub that rejects every resource object.
type alwaysIneligibleChecker struct {
	Checker
}

func (c *alwaysIneligibleChecker) AllowsObject(_ *unstructured.Unstructured) (bool, string, error) {
	return false, "ineligible", nil
}

// failingChecker is a Checker stub that fails every resource object check.
type failingChecker struct {
	Checker
}

func (c *failingChecker) AllowsObject(_ *unstructured.Unstructured) (bool, string, error) {
	return false, "", errors.New("check failure")
}

// TestSkipFlagsAreIndependent tests that each skip flag of the checker type disables only its own check.
func TestSkipFlagsAreIndependent(t *testing.T) {
	now := metav1.Now()
	deletingConfigMap := &unstructured.Unstructured{}
	deletingConfigMap.SetGroupVersionKind(configMapGVK)
	deletingConfigMap.SetNamespace("work")
	deletingConfigMap.SetName("app-config")
	deletingConfigMap.SetDeletionTimestamp(&now)

	testCases := []struct {
		name            string
		skipNSCheck     bool
		skipGVKRCheck   bool
		skipObjectCheck bool
		wantNS          bool
		wantGVK         bool
		wantGVR         bool
		wantObject      bool
	}{
		{
			name: "no check is skipped",
		},
		{
			name:        "namespace check is skipped",
			skipNSCheck: true,
			wantNS:      true,
		},
		{
			name:          "GVK and GVR checks are skipped",
			skipGVKRCheck: true,
			wantGVK:       true,
			wantGVR:       true,
		},
		{
			name:            "object check is skipped",
			skipObjectCheck: true,
			wantObject:      true,
		},
		{
			name:            "all checks are skipped",
			skipNSCheck:     true,
			skipGVKRCheck:   true,
			skipObjectCheck: true,
			wantNS:          true,
			wantGVK:         true,
			wantGVR:         true,
			wantObject:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Every check is set to reject its input unless it is skipped.
			c := New(CheckerOptions{RestMapper: newTestRESTMapper(), SkipNSCheck: tc.skipNSCheck, SkipGVKRCheck: tc.skipGVKRCheck, SkipObjectCheck: tc.skipObjectCheck})
			c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)
			if err := c.SetGVKCheckList(GVKEligibilityModeDenyList, []schema.GroupVersionKind{deploymentGVK}); err != nil {
				t.Fatalf("SetGVKCheckList(DenyList) = %v, want no error", err)
			}

			if got := c.AllowsNamespace("work"); got != tc.wantNS {
				t.Errorf("AllowsNamespace(work) = %t, want %t", got, tc.wantNS)
			}
			if got := c.AllowsGVK(deploymentGVK); got != tc.wantGVK {
				t.Errorf("AllowsGVK(%v) = %t, want %t", deploymentGVK, got, tc.wantGVK)
			}
			gotGVR, err := c.AllowsGVR(deploymentGVR)
			if err != nil {
				t.Fatalf("AllowsGVR(%v) = %v, want no error", deploymentGVR, err)
			}
			if gotGVR != tc.wantGVR {
				t.Errorf("AllowsGVR(%v) = %t, want %t", deploymentGVR, gotGVR, tc.wantGVR)
			}
			gotObject, _, err := c.AllowsObject(deletingConfigMap)
			if err != nil {
				t.Fatalf("AllowsObject() = %v, want no error", err)
			}
			if gotObject != tc.wantObject {
				t.Errorf("AllowsObject() = %t, want %t", gotObject, tc.wantObject)
			}
		})
	}
}

// TestCheckerDeepCopyWithWrappedSkippedCheckers tests that the DeepCopy method of the checker type preserves
// the skip flags at every level of a chain of wrapped checkers.
func TestCheckerDeepCopyWithWrappedSkippedCheckers(t *testing.T) {
	restMapper := newTestRESTMapper()
	inner := New(CheckerOptions{RestMapper: restMapper, SkipNSCheck: false, SkipGVKRCheck: true, SkipObjectCheck: true})
	c := New(CheckerOptions{RestMapper: restMapper, Wrapped: inner, SkipNSCheck: true, SkipGVKRCheck: false, SkipObjectCheck: true})
	c.SetNamespaceCheckList(NamespaceEligibilityModeDenyList, []string{"work"}, nil)

	cp := c.DeepCopy()
	if diff := cmp.Diff(cp, c, checkerCmpOptions); diff != "" {
		t.Fatalf("DeepCopy() mismatch (-got, +want):\n%s", diff)
	}

	// The copy should behave in the same way as the original checker.
	if got, want := cp.AllowsNamespace("work"), c.AllowsNamespace("work"); got != want {
		t.Errorf("AllowsNamespace(work) on the copy = %t, want %t", got, want)
	}

	// Mutate the wrapped checker of the copy; the wrapped checker of the original should stay intact.
	cpWrapped, ok := cp.(*checker).wrapped.(*checker)
	if !ok {
		t.Fatalf("DeepCopy() returned a wrapped checker of type %T, want *checker", cp.(*checker).wrapped)
	}
	cpWrapped.skipGVKRCheck = false
	if !inner.(*checker).skipGVKRCheck {
		t.Errorf("skipGVKRCheck on the original wrapped checker = %t, want %t", false, true)
	}
}
