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

package readiness

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

func TestReadinessChecker(t *testing.T) {
	tests := []struct {
		name             string
		resourceInformer informer.Manager
		expectError      bool
		errorContains    string
	}{
		{
			name:             "nil informer",
			resourceInformer: nil,
			expectError:      true,
			errorContains:    "resource informer not initialized",
		},
		{
			name: "no resources registered",
			resourceInformer: &testinformer.FakeManager{
				APIResources: map[schema.GroupVersionKind]bool{},
			},
			expectError:   true,
			errorContains: "no resources registered",
		},
		{
			name: "all informers synced",
			resourceInformer: &testinformer.FakeManager{
				APIResources: map[schema.GroupVersionKind]bool{
					{Group: "", Version: "v1", Kind: "ConfigMap"}: true, // this boolean is ignored
					{Group: "", Version: "v1", Kind: "Secret"}:    true,
					{Group: "", Version: "v1", Kind: "Namespace"}: true,
				},
				InformerSynced: ptr.To(true), // this makes all informers synced
			},
			expectError: false,
		},
		{
			name: "some informers not synced",
			resourceInformer: &testinformer.FakeManager{
				APIResources: map[schema.GroupVersionKind]bool{
					{Group: "", Version: "v1", Kind: "ConfigMap"}: false, // this boolean is ignored
					{Group: "", Version: "v1", Kind: "Secret"}:    false,
					{Group: "", Version: "v1", Kind: "Namespace"}: false,
				},
				IsClusterScopedResource: true,
				InformerSynced:          ptr.To(false), // this makes all informers not synced
			},
			expectError:   true,
			errorContains: "informers not synced yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := InformerReadinessChecker(tt.resourceInformer)
			err := checker(nil)

			if tt.expectError {
				if err == nil {
					t.Errorf("ReadinessChecker() expected error, got nil")
				}
				if tt.errorContains != "" && err != nil {
					if got := err.Error(); !strings.Contains(got, tt.errorContains) {
						t.Errorf("error message should contain %q, got: %s", tt.errorContains, got)
					}
				}
			} else {
				if err != nil {
					t.Errorf("ReadinessChecker() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestReadinessChecker_NoneSync(t *testing.T) {
	// Test the case where we have multiple resources but none are synced
	mockManager := &testinformer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			{Group: "", Version: "v1", Kind: "ConfigMap"}:      false, // this boolean is ignored
			{Group: "", Version: "v1", Kind: "Secret"}:         false,
			{Group: "apps", Version: "v1", Kind: "Deployment"}: false,
			{Group: "", Version: "v1", Kind: "Namespace"}:      false,
		},
		InformerSynced: ptr.To(false), // this makes all informers not synced
	}

	checker := InformerReadinessChecker(mockManager)
	err := checker(nil)

	if err == nil {
		t.Fatal("ReadinessChecker() should return error when no informers are synced")
	}
	if got := err.Error(); !strings.Contains(got, "informers not synced yet") {
		t.Errorf("error message should contain 'informers not synced yet', got: %s", got)
	}
	// Should report 4 unsynced
	if got := err.Error(); !strings.Contains(got, "4/4") {
		t.Errorf("error message should contain '4/4', got: %s", got)
	}
}

func TestReadinessChecker_AllSyncedMultipleResources(t *testing.T) {
	// Test with many resources all synced
	mockManager := &testinformer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			{Group: "", Version: "v1", Kind: "ConfigMap"}:                            true, // this boolean is ignored
			{Group: "", Version: "v1", Kind: "Secret"}:                               true,
			{Group: "", Version: "v1", Kind: "Service"}:                              true,
			{Group: "apps", Version: "v1", Kind: "Deployment"}:                       true,
			{Group: "apps", Version: "v1", Kind: "StatefulSet"}:                      true,
			{Group: "", Version: "v1", Kind: "Namespace"}:                            true,
			{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}: true,
		},
		InformerSynced: ptr.To(true), // this makes all informers synced
	}

	checker := InformerReadinessChecker(mockManager)
	err := checker(nil)

	if err != nil {
		t.Errorf("ReadinessChecker() unexpected error when all informers are synced: %v", err)
	}
}
