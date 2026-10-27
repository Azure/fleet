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
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	trieCmpOptions = cmp.Options{
		cmp.AllowUnexported(gvkMatcherTrie{}, gvkMatcherTrieNode{}),
	}
)

// node is a helper that builds a trie node with the given children.
func node(children map[string]*gvkMatcherTrieNode) *gvkMatcherTrieNode {
	return &gvkMatcherTrieNode{children: children}
}

// leaf is a helper that builds a childless trie node.
func leaf() *gvkMatcherTrieNode {
	return &gvkMatcherTrieNode{}
}

// TestGVKMatcherTrieRegister tests the Register method of the gvkMatcherTrie type.
func TestGVKMatcherTrieRegister(t *testing.T) {
	testCases := []struct {
		name     string
		gvks     []schema.GroupVersionKind
		wantErr  bool
		wantTrie gvkMatcherTrie
	}{
		{
			name: "full GVK",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "group only (empty version and kind are treated as wildcards)",
			gvks: []schema.GroupVersionKind{
				{Group: "apps"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						wildcard: node(map[string]*gvkMatcherTrieNode{
							wildcard: leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "group and version only",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							wildcard: leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "core API group (empty group name)",
			gvks: []schema.GroupVersionKind{
				{Version: "v1", Kind: "ConfigMap"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"ConfigMap": leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "explicit wildcards for version and kind",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: wildcard, Kind: wildcard},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						wildcard: node(map[string]*gvkMatcherTrieNode{
							wildcard: leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "multiple GVKs sharing a group",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
				{Group: "apps", Version: "v1beta1", Kind: "Deployment"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Deployment":  leaf(),
							"StatefulSet": leaf(),
						}),
						"v1beta1": node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "multiple GVKs across groups",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "batch", Version: "v1", Kind: "Job"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
					}),
					"batch": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Job": leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "duplicate registration",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "a wildcard registered before a more specific GVK",
			gvks: []schema.GroupVersionKind{
				{Group: "apps"},
				{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						wildcard: node(map[string]*gvkMatcherTrieNode{
							wildcard: leaf(),
						}),
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
					}),
				},
			},
		},
		{
			// A wildcard version with a specific kind does not cover a specific version with a
			// different kind, so the two must not share a branch.
			name: "a partial wildcard does not absorb a more specific GVK",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						wildcard: node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
						"v1": node(map[string]*gvkMatcherTrieNode{
							"StatefulSet": leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "a wildcard registered after a more specific GVK",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps"},
			},
			wantTrie: gvkMatcherTrie{
				children: map[string]*gvkMatcherTrieNode{
					"apps": node(map[string]*gvkMatcherTrieNode{
						"v1": node(map[string]*gvkMatcherTrieNode{
							"Deployment": leaf(),
						}),
						wildcard: node(map[string]*gvkMatcherTrieNode{
							wildcard: leaf(),
						}),
					}),
				},
			},
		},
		{
			name: "wildcard API group",
			gvks: []schema.GroupVersionKind{
				{Group: wildcard, Version: "v1", Kind: "Deployment"},
			},
			wantErr:  true,
			wantTrie: gvkMatcherTrie{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			trie := gvkMatcherTrie{}
			var gotErr error
			for _, gvk := range tc.gvks {
				if err := trie.Register(gvk); err != nil {
					gotErr = err
					break
				}
			}

			if (gotErr != nil) != tc.wantErr {
				t.Fatalf("Register() error = %v, wantErr %v", gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(trie, tc.wantTrie, trieCmpOptions); diff != "" {
				t.Errorf("trie after Register() mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestGVKMatcherTrieDeepCopy tests the DeepCopy method of the gvkMatcherTrie type.
func TestGVKMatcherTrieDeepCopy(t *testing.T) {
	testCases := []struct {
		name string
		gvks []schema.GroupVersionKind
	}{
		{
			name: "empty trie",
		},
		{
			name: "trie with multiple entries",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
				{Group: "batch"},
				{Version: "v1", Kind: "ConfigMap"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			trie := gvkMatcherTrie{}
			for _, gvk := range tc.gvks {
				if err := trie.Register(gvk); err != nil {
					t.Fatalf("Register(%v) = %v, want no error", gvk, err)
				}
			}

			cp := trie.DeepCopy()
			if diff := cmp.Diff(cp, trie, trieCmpOptions); diff != "" {
				t.Fatalf("DeepCopy() mismatch (-got, +want):\n%s", diff)
			}

			// Register a new GVK in the copy; the original trie should stay intact.
			newGVK := schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}
			if err := cp.Register(newGVK); err != nil {
				t.Fatalf("Register(%v) = %v, want no error", newGVK, err)
			}
			if trie.Match(newGVK) {
				t.Errorf("Match(%v) on the original trie = true, want false", newGVK)
			}

			// Mutate the original trie; the copy should stay intact.
			anotherGVK := schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}
			if err := trie.Register(anotherGVK); err != nil {
				t.Fatalf("Register(%v) = %v, want no error", anotherGVK, err)
			}
			if cp.Match(anotherGVK) {
				t.Errorf("Match(%v) on the copied trie = true, want false", anotherGVK)
			}
		})
	}
}

// TestGVKMatcherTrieMatch tests the Match method of the gvkMatcherTrie type.
func TestGVKMatcherTrieMatch(t *testing.T) {
	registered := []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "batch", Version: "v1"},
		{Group: "networking.k8s.io"},
		{Version: "v1", Kind: "ConfigMap"},
	}

	testCases := []struct {
		name      string
		gvks      []schema.GroupVersionKind
		gvk       schema.GroupVersionKind
		wantMatch bool
	}{
		{
			name:      "exact match",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			wantMatch: true,
		},
		{
			name:      "kind not registered",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			wantMatch: false,
		},
		{
			name:      "version not registered",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "Deployment"},
			wantMatch: false,
		},
		{
			name:      "group not registered",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
			wantMatch: false,
		},
		{
			name:      "kind wildcard match",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"},
			wantMatch: true,
		},
		{
			name:      "kind wildcard match, version mismatch",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "batch", Version: "v1beta1", Kind: "Job"},
			wantMatch: false,
		},
		{
			name:      "version and kind wildcard match",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1beta1", Kind: "Ingress"},
			wantMatch: true,
		},
		{
			name:      "core API group match",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
			wantMatch: true,
		},
		{
			name:      "core API group, kind not registered",
			gvks:      registered,
			gvk:       schema.GroupVersionKind{Version: "v1", Kind: "Secret"},
			wantMatch: false,
		},
		{
			name:      "empty trie",
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			wantMatch: false,
		},
		{
			name: "a more specific entry is registered alongside a wildcard one",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps"},
			},
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v2", Kind: "Deployment"},
			wantMatch: true,
		},
		{
			name: "a wildcard version with a specific kind, matching version and kind",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			},
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			wantMatch: true,
		},
		{
			// The StatefulSet entry is pinned to v1, so it must not be widened by the
			// wildcard version registered for Deployment.
			name: "a wildcard version with a specific kind does not widen another kind",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			},
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "StatefulSet"},
			wantMatch: false,
		},
		{
			name: "a wildcard version still matches its own kind in any version",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			},
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "Deployment"},
			wantMatch: true,
		},
		{
			name: "partial path registered only",
			gvks: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
			gvk:       schema.GroupVersionKind{Group: "apps", Version: "v1"},
			wantMatch: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			trie := gvkMatcherTrie{}
			for _, gvk := range tc.gvks {
				if err := trie.Register(gvk); err != nil {
					t.Fatalf("Register(%v) = %v, want no error", gvk, err)
				}
			}

			if gotMatch := trie.Match(tc.gvk); gotMatch != tc.wantMatch {
				t.Errorf("Match(%v) = %t, want %t", tc.gvk, gotMatch, tc.wantMatch)
			}
		})
	}
}
