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

package admissionpolicymanager

import (
	"fmt"
	"strings"
)

// CELExprTreeNode is the interface for CEL tree nodes. It helps build CEL expressions in a
// structured way.
type CELExprTreeNode interface {
	// Build returns the CEL expression represented by the node and its children.
	Build() (string, error)
	// Children returns the child nodes of the current node.
	Children() []CELExprTreeNode
}

// CELExprTreeEditableNode is the interface for CEL tree nodes that can be edited (i.e., have child nodes added to them).
//
// This is added to simplify cases where a CEL tree node need to be built step-by-step.
type CELExprTreeEditableNode interface {
	CELExprTreeNode
	// Add adds a child node to the current node.
	Add(child CELExprTreeNode)
}

var (
	// Verify that all node types implement the CELExprTreeNode interface.
	_ CELExprTreeNode = &rawCELExprTreeNode{}
	_ CELExprTreeNode = &orCELExprTreeNode{}
	_ CELExprTreeNode = &andCELExprTreeNode{}
	_ CELExprTreeNode = &notCELExprTreeNode{}

	_ CELExprTreeEditableNode = &orCELExprTreeNode{}
	_ CELExprTreeEditableNode = &andCELExprTreeNode{}
)

// rawCELExprTreeNode represents a raw CEL expression. It is a leaf node in the CEL expression tree.
type rawCELExprTreeNode struct {
	expr string
}

// Build returns the raw CEL expression.
func (n *rawCELExprTreeNode) Build() (string, error) {
	if len(n.expr) == 0 {
		return "", fmt.Errorf("raw CEL expression cannot be empty")
	}
	return n.expr, nil
}

// Children returns the child nodes of the current node.
//
// For rawCELExprTreeNode, it always returns nil.
func (n *rawCELExprTreeNode) Children() []CELExprTreeNode {
	return nil
}

// RawCELExpr returns a new rawCELExprTreeNode with the given expression.
func RawCELExpr(expr string) CELExprTreeNode {
	return &rawCELExprTreeNode{expr: expr}
}

// orCELExprTreeNode represents a logical OR of its child nodes' expressions.
type orCELExprTreeNode struct {
	children []CELExprTreeNode
}

// Build returns the CEL expression representing the logical OR of its child nodes' expressions.
func (n *orCELExprTreeNode) Build() (string, error) {
	exprs := make([]string, len(n.children))
	for i, child := range n.children {
		if child == nil {
			return "", fmt.Errorf("a child node is nil")
		}
		expr, err := child.Build()
		if err != nil {
			return "", fmt.Errorf("failed to build child node: %w", err)
		}
		if len(expr) == 0 {
			return "", fmt.Errorf("a child node built to an empty expression")
		}
		exprs[i] = fmt.Sprintf("(%s)", expr)
	}

	res := strings.Join(exprs, " || ")
	if len(res) == 0 {
		return "", fmt.Errorf("built to an empty expression")
	}
	return res, nil
}

// Children returns the child nodes of the current node.
func (n *orCELExprTreeNode) Children() []CELExprTreeNode {
	return n.children
}

// Add adds a child node to the current node.
func (n *orCELExprTreeNode) Add(child CELExprTreeNode) {
	n.children = append(n.children, child)
}

// LogicalOr returns a new orCELExprTreeNode with the given child nodes.
func LogicalOr(children ...CELExprTreeNode) CELExprTreeEditableNode {
	return &orCELExprTreeNode{children: children}
}

// andCELExprTreeNode represents a logical AND of its child nodes' expressions.
type andCELExprTreeNode struct {
	children []CELExprTreeNode
}

// Build returns the CEL expression representing the logical AND of its child nodes' expressions.
func (n *andCELExprTreeNode) Build() (string, error) {
	exprs := make([]string, len(n.children))
	for i, child := range n.children {
		if child == nil {
			return "", fmt.Errorf("a child node is nil")
		}
		expr, err := child.Build()
		if err != nil {
			return "", fmt.Errorf("failed to build child node: %w", err)
		}
		if len(expr) == 0 {
			return "", fmt.Errorf("a child node built to an empty expression")
		}
		exprs[i] = fmt.Sprintf("(%s)", expr)
	}

	res := strings.Join(exprs, " && ")
	if len(res) == 0 {
		return "", fmt.Errorf("built to an empty expression")
	}
	return res, nil
}

// Children returns the child nodes of the current node.
func (n *andCELExprTreeNode) Children() []CELExprTreeNode {
	return n.children
}

// Add adds a child node to the current node.
func (n *andCELExprTreeNode) Add(child CELExprTreeNode) {
	n.children = append(n.children, child)
}

// LogicalAnd returns a new andCELExprTreeNode with the given child nodes.
func LogicalAnd(children ...CELExprTreeNode) CELExprTreeEditableNode {
	return &andCELExprTreeNode{children: children}
}

// notCELExprTreeNode represents a logical NOT of its child node's expression.
type notCELExprTreeNode struct {
	child CELExprTreeNode
}

// Build returns the CEL expression representing the logical NOT of its child node's expression.
func (n *notCELExprTreeNode) Build() (string, error) {
	if n.child == nil {
		return "", fmt.Errorf("child node is nil")
	}
	expr, err := n.child.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build child node: %w", err)
	}
	if len(expr) == 0 {
		return "", fmt.Errorf("child node built to an empty expression")
	}
	return fmt.Sprintf("!(%s)", expr), nil
}

// Children returns the child node of the current node.
func (n *notCELExprTreeNode) Children() []CELExprTreeNode {
	return []CELExprTreeNode{n.child}
}

// LogicalNot returns a new notCELExprTreeNode with the given child node.
func LogicalNot(child CELExprTreeNode) CELExprTreeNode {
	return &notCELExprTreeNode{child: child}
}
