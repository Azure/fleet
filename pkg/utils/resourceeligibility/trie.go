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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	wildcard = "*"
)

type gvkMatcherTrie gvkMatcherTrieNode

type gvkMatcherTrieNode struct {
	children map[string]*gvkMatcherTrieNode
}

// Register adds the given GVK to the matcher trie.
func (t *gvkMatcherTrie) Register(gvk schema.GroupVersionKind) error {
	group := gvk.Group
	if group == wildcard {
		return errors.NewUserError(nil, "cannot use wildcards for API groups", "gvk", gvk)
	}

	version := gvk.Version
	if version == "" {
		version = wildcard
	}
	kind := gvk.Kind
	if kind == "" {
		kind = wildcard
	}

	node := (*gvkMatcherTrieNode)(t)
	for _, edge := range []string{group, version, kind} {
		if node.children == nil {
			node.children = make(map[string]*gvkMatcherTrieNode)
		}
		child, found := node.children[edge]
		if !found {
			child = &gvkMatcherTrieNode{}
			node.children[edge] = child
		}
		node = child
	}
	return nil
}

// DeepCopy returns a deep copy of the matcher trie.
func (t *gvkMatcherTrie) DeepCopy() gvkMatcherTrie {
	return gvkMatcherTrie(*(*gvkMatcherTrieNode)(t).deepCopy())
}

func (n *gvkMatcherTrieNode) deepCopy() *gvkMatcherTrieNode {
	cp := &gvkMatcherTrieNode{}
	if n.children == nil {
		return cp
	}

	cp.children = make(map[string]*gvkMatcherTrieNode, len(n.children))
	for edge, child := range n.children {
		cp.children[edge] = child.deepCopy()
	}
	return cp
}

// Match reports whether the given GVK is covered by an entry registered in the trie.
func (t *gvkMatcherTrie) Match(gvk schema.GroupVersionKind) bool {
	edges := []string{gvk.Group, gvk.Version, gvk.Kind}

	// Do a DFS walk.
	var walk func(node *gvkMatcherTrieNode, depth int) bool
	walk = func(node *gvkMatcherTrieNode, depth int) bool {
		if depth == len(edges) {
			// All the edges have been consumed; a registered path has been found.
			return true
		}

		for _, edge := range []string{wildcard, edges[depth]} {
			if child, found := node.children[edge]; found && walk(child, depth+1) {
				return true
			}
		}
		return false
	}
	return walk((*gvkMatcherTrieNode)(t), 0)
}
