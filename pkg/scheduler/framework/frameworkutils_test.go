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

package framework

import (
	"strings"
	"testing"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
)

func TestGenerateBinding(t *testing.T) {
	tests := []struct {
		name              string
		placementKey      queue.PlacementKey
		clusterName       string
		expectedType      string // "ClusterResourceBinding" or "ResourceBinding"
		expectedNamespace string // expected namespace for ResourceBinding, empty for ClusterResourceBinding
		expectedLabel     string // expected CRPTrackingLabel value
		expectedError     bool
	}{
		{
			name:              "cluster-scoped placement - success",
			placementKey:      queue.PlacementKey("test-placement"),
			clusterName:       "test-cluster",
			expectedType:      "ClusterResourceBinding",
			expectedNamespace: "",
			expectedLabel:     "test-placement",
			expectedError:     false,
		},
		{
			name:              "namespaced placement - success",
			placementKey:      queue.PlacementKey("test-namespace/test-placement"),
			clusterName:       "test-cluster",
			expectedType:      "ResourceBinding",
			expectedNamespace: "test-namespace",
			expectedLabel:     "test-placement",
			expectedError:     false,
		},
		{
			name:              "empty cluster name",
			placementKey:      queue.PlacementKey("test-placement"),
			clusterName:       "",
			expectedType:      "ClusterResourceBinding",
			expectedNamespace: "",
			expectedLabel:     "test-placement",
			expectedError:     false,
		},
		{
			name:              "long cluster name",
			placementKey:      queue.PlacementKey("test-placement"),
			clusterName:       "very-long-cluster-name-that-might-cause-issues-with-unique-name-generation",
			expectedType:      "ClusterResourceBinding",
			expectedNamespace: "",
			expectedLabel:     "test-placement",
			expectedError:     false,
		},
		{
			name:              "invalid placement key format",
			placementKey:      queue.PlacementKey("invalid/key/format/with/too/many/parts"),
			clusterName:       "test-cluster",
			expectedType:      "",
			expectedNamespace: "",
			expectedLabel:     "",
			expectedError:     true,
		},
		{
			name:              "invalid key with too many slashes",
			placementKey:      queue.PlacementKey("invalid/key/with/too/many/slashes/to/be/valid"),
			clusterName:       "test-cluster",
			expectedType:      "",
			expectedNamespace: "",
			expectedLabel:     "",
			expectedError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding, err := generateBinding(tt.placementKey, tt.clusterName)

			if tt.expectedError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if binding != nil {
					t.Errorf("expected nil binding when error occurs, got %v", binding)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if binding == nil {
				t.Error("expected binding, got nil")
				return
			}

			// Verify binding type and properties
			switch tt.expectedType {
			case "ClusterResourceBinding":
				crb, ok := binding.(*placementv1beta1.ClusterResourceBinding)
				if !ok {
					t.Errorf("expected ClusterResourceBinding, got %T", binding)
					return
				}
				if crb.Namespace != "" {
					t.Errorf("expected empty namespace for ClusterResourceBinding, got %s", crb.Namespace)
				}

			case "ResourceBinding":
				rb, ok := binding.(*placementv1beta1.ResourceBinding)
				if !ok {
					t.Errorf("expected ResourceBinding, got %T", binding)
					return
				}
				if rb.Namespace != tt.expectedNamespace {
					t.Errorf("expected namespace %s, got %s", tt.expectedNamespace, rb.Namespace)
				}

			default:
				t.Errorf("unexpected expected type: %s", tt.expectedType)
			}

			// Verify name pattern: placement-cluster-uuid (8 chars), the placement and cluster name part are already validated
			// in NewBindingName UT.
			nameParts := strings.Split(binding.GetName(), "-")
			if len(nameParts) < 3 {
				t.Errorf("expected binding name to have at least 3 parts separated by '-', got %s", binding.GetName())
			} else {
				// Last part should be 8-character UUID
				uuidPart := nameParts[len(nameParts)-1]
				if len(uuidPart) != 8 {
					t.Errorf("expected UUID part to be 8 characters, got %d characters: %s", len(uuidPart), uuidPart)
				}
			}

			// Verify labels
			if binding.GetLabels()[placementv1beta1.PlacementTrackingLabel] != tt.expectedLabel {
				t.Errorf("expected CRPTrackingLabel %s, got %s", tt.expectedLabel, binding.GetLabels()[placementv1beta1.PlacementTrackingLabel])
			}

			// Verify finalizers
			if len(binding.GetFinalizers()) != 1 || binding.GetFinalizers()[0] != placementv1beta1.SchedulerBindingCleanupFinalizer {
				t.Errorf("expected finalizer %s, got %v", placementv1beta1.SchedulerBindingCleanupFinalizer, binding.GetFinalizers())
			}
		})
	}
}
