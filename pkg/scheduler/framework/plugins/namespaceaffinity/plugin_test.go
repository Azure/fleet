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

package namespaceaffinity

import (
	"testing"
)

// TestNew tests the New function.
func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		wantName string
	}{
		{
			name:     "default options",
			opts:     nil,
			wantName: "NamespaceAffinity",
		},
		{
			name:     "custom name",
			opts:     []Option{WithName("CustomNamespaceAffinity")},
			wantName: "CustomNamespaceAffinity",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New(tc.opts...)
			if got := p.Name(); got != tc.wantName {
				t.Errorf("New() name = %v, want %v", got, tc.wantName)
			}
		})
	}
}
