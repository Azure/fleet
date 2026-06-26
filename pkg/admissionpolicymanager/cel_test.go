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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestCELExprTreeNode_Raw tests the RawCELExprTreeNode implementation of the CELExprTreeNode interface.
func TestCELExprTreeNode_Raw(t *testing.T) {
	testCases := []struct {
		name             string
		expr             string
		wantBuilt        string
		wantErred        bool
		wantErrSubstring string
	}{
		{
			name:      "valid",
			expr:      "x = y",
			wantBuilt: "x = y",
			wantErred: false,
		},
		{
			name:             "empty expression",
			expr:             "",
			wantBuilt:        "",
			wantErred:        true,
			wantErrSubstring: "raw CEL expression cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := RawCELExpr(tc.expr)

			gotBuilt, err := node.Build()
			if tc.wantErred {
				if err == nil {
					t.Fatal("Build() = nil, want erred")
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstring) {
					t.Errorf("Build() error = %v, want error with substring %s", err, tc.wantErrSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("Build() = %v, want no error", err)
			}
			if !cmp.Equal(gotBuilt, tc.wantBuilt) {
				t.Errorf("Build() = %v, want %v", gotBuilt, tc.wantBuilt)
			}

			gotChildren := node.Children()
			wantChildren := []CELExprTreeNode(nil)
			if !cmp.Equal(gotChildren, wantChildren) {
				t.Errorf("Children() = %v, want %v", gotChildren, wantChildren)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalOr tests the orCELExprTreeNode implementation of the CELExprTreeNode interface.
func TestCELExprTreeNode_LogicalOr(t *testing.T) {
	testCases := []struct {
		name         string
		children     []CELExprTreeNode
		wantBuilt    string
		wantChildren []CELExprTreeNode
	}{
		{
			name:         "single child",
			children:     []CELExprTreeNode{RawCELExpr("x = y")},
			wantBuilt:    "(x = y)",
			wantChildren: []CELExprTreeNode{RawCELExpr("x = y")},
		},
		{
			name: "multiple children",
			children: []CELExprTreeNode{
				RawCELExpr("x = y"),
				RawCELExpr("y = z"),
				RawCELExpr("z = a"),
			},
			wantBuilt: "(x = y) || (y = z) || (z = a)",
			wantChildren: []CELExprTreeNode{
				RawCELExpr("x = y"),
				RawCELExpr("y = z"),
				RawCELExpr("z = a"),
			},
		},
		{
			name: "nested child",
			children: []CELExprTreeNode{
				RawCELExpr("x = y"),
				LogicalAnd(RawCELExpr("y = z"), RawCELExpr("z = a")),
			},
			wantBuilt: "(x = y) || ((y = z) && (z = a))",
			wantChildren: []CELExprTreeNode{
				RawCELExpr("x = y"),
				LogicalAnd(RawCELExpr("y = z"), RawCELExpr("z = a")),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := LogicalOr(tc.children...)

			gotBuilt, err := node.Build()
			if err != nil {
				t.Fatalf("Build() = %v", err)
			}
			if !cmp.Equal(gotBuilt, tc.wantBuilt) {
				t.Errorf("Build() = %v, want %v", gotBuilt, tc.wantBuilt)
			}

			gotChildren := node.Children()
			if !cmp.Equal(gotChildren, tc.wantChildren,
				cmp.AllowUnexported(rawCELExprTreeNode{}),
				cmp.AllowUnexported(andCELExprTreeNode{}),
				cmp.AllowUnexported(orCELExprTreeNode{}),
				cmp.AllowUnexported(notCELExprTreeNode{}),
			) {
				t.Errorf("Children() = %v, want %v", gotChildren, tc.wantChildren)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalOr_Erred tests LogicalOr error scenarios.
func TestCELExprTreeNode_LogicalOr_Erred(t *testing.T) {
	testCases := []struct {
		name             string
		children         []CELExprTreeNode
		wantErrSubstring string
	}{
		{
			name:             "no children",
			children:         nil,
			wantErrSubstring: "built to an empty expression",
		},
		{
			name:             "nil child",
			children:         []CELExprTreeNode{RawCELExpr("x = y"), nil},
			wantErrSubstring: "a child node is nil",
		},
		{
			name:             "child parse error",
			children:         []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("")},
			wantErrSubstring: "failed to build child node: raw CEL expression cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := LogicalOr(tc.children...)

			gotBuilt, err := node.Build()
			if err == nil {
				t.Fatalf("Build() = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Errorf("Build() error = %v, want error with substring %s", err, tc.wantErrSubstring)
			}
			if gotBuilt != "" {
				t.Errorf("Build() = %v, want empty string on error", gotBuilt)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalOr_Add tests Add behavior for LogicalOr nodes.
func TestCELExprTreeNode_LogicalOr_Add(t *testing.T) {
	testCases := []struct {
		name             string
		childrenToAdd    []CELExprTreeNode
		wantBuilt        string
		wantChildren     []CELExprTreeNode
		wantErred        bool
		wantErrSubstring string
	}{
		{
			name:          "append valid children",
			childrenToAdd: []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("y = z")},
			wantBuilt:     "(x = y) || (y = z)",
			wantChildren:  []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("y = z")},
			wantErred:     false,
		},
		{
			name:             "append nil child",
			childrenToAdd:    []CELExprTreeNode{RawCELExpr("x = y"), nil},
			wantBuilt:        "",
			wantChildren:     []CELExprTreeNode{RawCELExpr("x = y"), nil},
			wantErred:        true,
			wantErrSubstring: "a child node is nil",
		},
		{
			name:             "append child that fails parse",
			childrenToAdd:    []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("")},
			wantBuilt:        "",
			wantChildren:     []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("")},
			wantErred:        true,
			wantErrSubstring: "failed to build child node: raw CEL expression cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			orNode := LogicalOr()

			for _, child := range tc.childrenToAdd {
				orNode.Add(child)
			}

			gotBuilt, err := orNode.Build()
			if tc.wantErred {
				if err == nil {
					t.Fatal("Build() = nil, want erred")
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstring) {
					t.Errorf("Build() error = %v, want error with substring %s", err, tc.wantErrSubstring)
				}
			} else {
				if err != nil {
					t.Fatalf("Build() = %v, want no error", err)
				}
				if !cmp.Equal(gotBuilt, tc.wantBuilt) {
					t.Errorf("Build() = %v, want %v", gotBuilt, tc.wantBuilt)
				}
			}

			gotChildren := orNode.Children()
			if !cmp.Equal(gotChildren, tc.wantChildren,
				cmp.AllowUnexported(rawCELExprTreeNode{}),
				cmp.AllowUnexported(andCELExprTreeNode{}),
				cmp.AllowUnexported(orCELExprTreeNode{}),
				cmp.AllowUnexported(notCELExprTreeNode{}),
			) {
				t.Errorf("Children() = %v, want %v", gotChildren, tc.wantChildren)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalAnd tests the andCELExprTreeNode implementation of the CELExprTreeNode interface.
func TestCELExprTreeNode_LogicalAnd(t *testing.T) {
	testCases := []struct {
		name         string
		children     []CELExprTreeNode
		wantBuilt    string
		wantChildren []CELExprTreeNode
	}{
		{
			name:         "single child",
			children:     []CELExprTreeNode{RawCELExpr("x = y")},
			wantBuilt:    "(x = y)",
			wantChildren: []CELExprTreeNode{RawCELExpr("x = y")},
		},
		{
			name: "multiple children",
			children: []CELExprTreeNode{
				RawCELExpr("x = y"),
				RawCELExpr("y = z"),
				RawCELExpr("z = a"),
			},
			wantBuilt: "(x = y) && (y = z) && (z = a)",
			wantChildren: []CELExprTreeNode{
				RawCELExpr("x = y"),
				RawCELExpr("y = z"),
				RawCELExpr("z = a"),
			},
		},
		{
			name: "nested child",
			children: []CELExprTreeNode{
				RawCELExpr("x = y"),
				LogicalOr(RawCELExpr("y = z"), RawCELExpr("z = a")),
			},
			wantBuilt: "(x = y) && ((y = z) || (z = a))",
			wantChildren: []CELExprTreeNode{
				RawCELExpr("x = y"),
				LogicalOr(RawCELExpr("y = z"), RawCELExpr("z = a")),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := LogicalAnd(tc.children...)

			gotBuilt, err := node.Build()
			if err != nil {
				t.Fatalf("Build() = %v", err)
			}
			if !cmp.Equal(gotBuilt, tc.wantBuilt) {
				t.Errorf("Build() = %v, want %v", gotBuilt, tc.wantBuilt)
			}

			gotChildren := node.Children()
			if !cmp.Equal(gotChildren, tc.wantChildren,
				cmp.AllowUnexported(rawCELExprTreeNode{}),
				cmp.AllowUnexported(andCELExprTreeNode{}),
				cmp.AllowUnexported(orCELExprTreeNode{}),
				cmp.AllowUnexported(notCELExprTreeNode{}),
			) {
				t.Errorf("Children() = %v, want %v", gotChildren, tc.wantChildren)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalAnd_Erred tests LogicalAnd error scenarios.
func TestCELExprTreeNode_LogicalAnd_Erred(t *testing.T) {
	testCases := []struct {
		name             string
		children         []CELExprTreeNode
		wantErrSubstring string
	}{
		{
			name:             "no children",
			children:         nil,
			wantErrSubstring: "built to an empty expression",
		},
		{
			name:             "nil child",
			children:         []CELExprTreeNode{RawCELExpr("x = y"), nil},
			wantErrSubstring: "a child node is nil",
		},
		{
			name:             "child parse error",
			children:         []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("")},
			wantErrSubstring: "failed to build child node: raw CEL expression cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := LogicalAnd(tc.children...)

			gotBuilt, err := node.Build()
			if err == nil {
				t.Fatalf("Build() = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Errorf("Build() error = %v, want error with substring %s", err, tc.wantErrSubstring)
			}
			if gotBuilt != "" {
				t.Errorf("Build() = %v, want empty string on error", gotBuilt)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalAnd_Add tests Add behavior for LogicalAnd nodes.
func TestCELExprTreeNode_LogicalAnd_Add(t *testing.T) {
	testCases := []struct {
		name             string
		childrenToAdd    []CELExprTreeNode
		wantBuilt        string
		wantChildren     []CELExprTreeNode
		wantErred        bool
		wantErrSubstring string
	}{
		{
			name:          "append valid children",
			childrenToAdd: []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("y = z")},
			wantBuilt:     "(x = y) && (y = z)",
			wantChildren:  []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("y = z")},
			wantErred:     false,
		},
		{
			name:             "append nil child",
			childrenToAdd:    []CELExprTreeNode{RawCELExpr("x = y"), nil},
			wantBuilt:        "",
			wantChildren:     []CELExprTreeNode{RawCELExpr("x = y"), nil},
			wantErred:        true,
			wantErrSubstring: "a child node is nil",
		},
		{
			name:             "append child that fails parse",
			childrenToAdd:    []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("")},
			wantBuilt:        "",
			wantChildren:     []CELExprTreeNode{RawCELExpr("x = y"), RawCELExpr("")},
			wantErred:        true,
			wantErrSubstring: "failed to build child node: raw CEL expression cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			andNode := LogicalAnd()

			for _, child := range tc.childrenToAdd {
				andNode.Add(child)
			}

			gotBuilt, err := andNode.Build()
			if tc.wantErred {
				if err == nil {
					t.Fatal("Build() = nil, want erred")
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstring) {
					t.Errorf("Build() error = %v, want error with substring %s", err, tc.wantErrSubstring)
				}
			} else {
				if err != nil {
					t.Fatalf("Build() = %v, want no error", err)
				}
				if !cmp.Equal(gotBuilt, tc.wantBuilt) {
					t.Errorf("Build() = %v, want %v", gotBuilt, tc.wantBuilt)
				}
			}

			gotChildren := andNode.Children()
			if !cmp.Equal(gotChildren, tc.wantChildren,
				cmp.AllowUnexported(rawCELExprTreeNode{}),
				cmp.AllowUnexported(andCELExprTreeNode{}),
				cmp.AllowUnexported(orCELExprTreeNode{}),
				cmp.AllowUnexported(notCELExprTreeNode{}),
			) {
				t.Errorf("Children() = %v, want %v", gotChildren, tc.wantChildren)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalNot tests the notCELExprTreeNode implementation of the CELExprTreeNode interface.
func TestCELExprTreeNode_LogicalNot(t *testing.T) {
	testCases := []struct {
		name         string
		child        CELExprTreeNode
		wantBuilt    string
		wantChildren []CELExprTreeNode
	}{
		{
			name:         "raw child",
			child:        RawCELExpr("x = y"),
			wantBuilt:    "!(x = y)",
			wantChildren: []CELExprTreeNode{RawCELExpr("x = y")},
		},
		{
			name:         "and child",
			child:        LogicalAnd(RawCELExpr("x = y"), RawCELExpr("y = z")),
			wantBuilt:    "!((x = y) && (y = z))",
			wantChildren: []CELExprTreeNode{LogicalAnd(RawCELExpr("x = y"), RawCELExpr("y = z"))},
		},
		{
			name:         "or child",
			child:        LogicalOr(RawCELExpr("x = y"), RawCELExpr("y = z")),
			wantBuilt:    "!((x = y) || (y = z))",
			wantChildren: []CELExprTreeNode{LogicalOr(RawCELExpr("x = y"), RawCELExpr("y = z"))},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := LogicalNot(tc.child)

			gotBuilt, err := node.Build()
			if err != nil {
				t.Fatalf("Build() = %v", err)
			}
			if !cmp.Equal(gotBuilt, tc.wantBuilt) {
				t.Errorf("Build() = %v, want %v", gotBuilt, tc.wantBuilt)
			}

			gotChildren := node.Children()
			if !cmp.Equal(gotChildren, tc.wantChildren,
				cmp.AllowUnexported(rawCELExprTreeNode{}),
				cmp.AllowUnexported(andCELExprTreeNode{}),
				cmp.AllowUnexported(orCELExprTreeNode{}),
				cmp.AllowUnexported(notCELExprTreeNode{}),
			) {
				t.Errorf("Children() = %v, want %v", gotChildren, tc.wantChildren)
			}
		})
	}
}

// TestCELExprTreeNode_LogicalNot_Erred tests LogicalNot error scenarios.
func TestCELExprTreeNode_LogicalNot_Erred(t *testing.T) {
	testCases := []struct {
		name             string
		child            CELExprTreeNode
		wantErrSubstring string
	}{
		{
			name:             "nil child",
			child:            nil,
			wantErrSubstring: "child node is nil",
		},
		{
			name:             "child parse error",
			child:            RawCELExpr(""),
			wantErrSubstring: "failed to build child node: raw CEL expression cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := LogicalNot(tc.child)

			gotBuilt, err := node.Build()
			if err == nil {
				t.Fatalf("Build() = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Errorf("Build() error = %v, want error with substring %s", err, tc.wantErrSubstring)
			}
			if gotBuilt != "" {
				t.Errorf("Build() = %v, want empty string on error", gotBuilt)
			}
		})
	}
}
