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

package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var resourceSnapshotCmpOptions = []cmp.Option{
	// ignore the TypeMeta and ObjectMeta fields that may differ between expected and actual
	cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "Generation", "CreationTimestamp", "ManagedFields"),
	cmp.Comparer(func(t1, t2 metav1.Time) bool {
		// we're within the margin (1s) if x + margin >= y
		return !t1.Time.Add(1 * time.Second).Before(t2.Time)
	}),
}

func TestFetchMasterResourceSnapshot(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name              string
		placementKey      types.NamespacedName
		existingSnapshots []client.Object
		expectedResult    fleetv1beta1.ResourceSnapshotObj
		expectedError     string
		setupClientError  bool
	}{
		{
			name: "successfully fetch master resource snapshot - namespaced",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-crp",
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hash123",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
						},
					},
				},
			},
			expectedResult: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-snapshot-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "hash123",
					},
				},
			},
		},
		{
			name: "successfully fetch master resource snapshot - cluster-scoped",
			placementKey: types.NamespacedName{
				Name: "test-crp",
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hash456",
						},
					},
				},
			},
			expectedResult: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-snapshot-1",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "hash456",
					},
				},
			},
		},
		{
			name: "no resource snapshots found",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-crp",
			},
			existingSnapshots: []client.Object{},
		},
		{
			name: "no master resource snapshot found",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-crp",
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
			},
			expectedError: "no masterResourceSnapshot found for the placement test-namespace/test-crp",
		},
		{
			name: "client list error",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-crp",
			},
			existingSnapshots: []client.Object{},
			setupClientError:  true,
			expectedError:     "failed to list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var k8Client client.Client
			if tt.setupClientError {
				k8Client = &errorClient{fake.NewClientBuilder().WithScheme(scheme).Build()}
			} else {
				k8Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingSnapshots...).Build()
			}

			result, err := FetchLatestMasterResourceSnapshot(context.Background(), k8Client, tt.placementKey)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error to contain: %s, but got: %v", tt.expectedError, err)
				}
				if result != nil {
					t.Fatalf("Expected nil result but got: %v", result)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			if tt.expectedResult == nil {
				if result != nil {
					t.Fatalf("Expected nil result but got: %v", result)
				}
				return
			}

			// Use cmp.Diff for comparison to provide better error messages
			if diff := cmp.Diff(tt.expectedResult, result, resourceSnapshotCmpOptions...); diff != "" {
				t.Errorf("FetchMasterResourceSnapshot() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListLatestResourceSnapshots(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name          string
		placementKey  types.NamespacedName
		objects       []client.Object
		expectedCount int
		expectedError string
	}{
		{
			name: "list latest resource snapshots - namespaced",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-placement",
			},
			objects: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "list latest resource snapshots - cluster-scoped",
			placementKey: types.NamespacedName{
				Name: "test-placement",
			},
			objects: []client.Object{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "no latest resource snapshots found",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-placement",
			},
			objects:       []client.Object{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8Client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			result, err := ListLatestResourceSnapshots(context.Background(), k8Client, tt.placementKey)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error to contain: %s, but got: %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			if len(result.GetResourceSnapshotObjs()) != tt.expectedCount {
				t.Errorf("Expected %d resource snapshots, got %d", tt.expectedCount, len(result.GetResourceSnapshotObjs()))
			}
		})
	}
}

func TestListAllResourceSnapshots(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name          string
		placementKey  types.NamespacedName
		objects       []client.Object
		expectedCount int
		expectedError string
	}{
		{
			name: "list all resource snapshots - namespaced",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-placement",
			},
			objects: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-snapshot",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "list all resource snapshots - cluster-scoped",
			placementKey: types.NamespacedName{
				Name: "test-placement",
			},
			objects: []client.Object{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8Client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			result, err := ListAllResourceSnapshots(context.Background(), k8Client, tt.placementKey)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error to contain: %s, but got: %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			if len(result.GetResourceSnapshotObjs()) != tt.expectedCount {
				t.Errorf("Expected %d resource snapshots, got %d", tt.expectedCount, len(result.GetResourceSnapshotObjs()))
			}
		})
	}
}

func TestListAllResourceSnapshotWithAnIndex(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name                  string
		resourceSnapshotIndex string
		placementName         string
		placementNamespace    string
		objects               []client.Object
		expectedCount         int
		expectedError         string
	}{
		{
			name:                  "list resource snapshots with index - namespaced",
			resourceSnapshotIndex: "1",
			placementName:         "test-placement",
			placementNamespace:    "test-namespace",
			objects: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "1",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "1",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-3",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "2",
						},
					},
				},
			},
			expectedCount: 2,
		},
		{
			name:                  "list resource snapshots with index - cluster-scoped",
			resourceSnapshotIndex: "3",
			placementName:         "test-placement",
			placementNamespace:    "",
			objects: []client.Object{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "3",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "4",
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name:                  "no snapshots found with index",
			resourceSnapshotIndex: "99",
			placementName:         "test-placement",
			placementNamespace:    "test-namespace",
			objects:               []client.Object{},
			expectedCount:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8Client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			result, err := ListAllResourceSnapshotWithAnIndex(context.Background(), k8Client, tt.resourceSnapshotIndex, tt.placementName, tt.placementNamespace)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error to contain: %s, but got: %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			if len(result.GetResourceSnapshotObjs()) != tt.expectedCount {
				t.Errorf("Expected %d resource snapshots, got %d", tt.expectedCount, len(result.GetResourceSnapshotObjs()))
			}
		})
	}
}

func TestBuildMasterResourceSnapshot(t *testing.T) {
	tests := []struct {
		name                        string
		placementObj                fleetv1beta1.PlacementObj
		latestResourceSnapshotIndex int
		resourceSnapshotCount       int
		envelopeObjCount            int
		resourceHash                string
		selectedResources           []fleetv1beta1.ResourceContent
		expectedResult              fleetv1beta1.ResourceSnapshotObj
	}{
		{
			name: "build master resource snapshot - namespaced",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: "test-namespace",
				},
			},
			latestResourceSnapshotIndex: 5,
			resourceSnapshotCount:       3,
			envelopeObjCount:            10,
			resourceHash:                "hash123",
			selectedResources: []fleetv1beta1.ResourceContent{
				{
					RawExtension: runtime.RawExtension{
						Raw: []byte("test-content"),
					},
				},
			},
			expectedResult: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement-5-snapshot",
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-placement",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.ResourceIndexLabel:     "5",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash123",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "10",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						{
							RawExtension: runtime.RawExtension{
								Raw: []byte("test-content"),
							},
						},
					},
				},
			},
		},
		{
			name: "build master resource snapshot - cluster-scoped",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			latestResourceSnapshotIndex: 2,
			resourceSnapshotCount:       1,
			envelopeObjCount:            5,
			resourceHash:                "hash456",
			selectedResources: []fleetv1beta1.ResourceContent{
				{
					RawExtension: runtime.RawExtension{
						Raw: []byte("test-cluster-content"),
					},
				},
			},
			expectedResult: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp-2-snapshot",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.ResourceIndexLabel:     "2",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "hash456",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "5",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						{
							RawExtension: runtime.RawExtension{
								Raw: []byte("test-cluster-content"),
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildMasterResourceSnapshot(
				tt.placementObj,
				tt.latestResourceSnapshotIndex,
				tt.resourceSnapshotCount,
				tt.envelopeObjCount,
				tt.resourceHash,
				tt.selectedResources,
			)

			// Use cmp.Diff for comprehensive comparison
			if diff := cmp.Diff(tt.expectedResult, result, resourceSnapshotCmpOptions...); diff != "" {
				t.Errorf("BuildMasterResourceSnapshot() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeleteResourceSnapshots(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name          string
		placementObj  fleetv1beta1.PlacementObj
		objects       []client.Object
		expectedError string
	}{
		{
			name: "delete resource snapshots - namespaced",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: "test-namespace",
				},
			},
			objects: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
			},
		},
		{
			name: "delete resource snapshots - cluster-scoped",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			objects: []client.Object{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
						},
					},
				},
			},
		},
		{
			name: "delete resource snapshots - no snapshots to delete",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: "test-namespace",
				},
			},
			objects: []client.Object{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8Client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			err := DeleteResourceSnapshots(context.Background(), k8Client, tt.placementObj)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error to contain: %s, but got: %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			// Verify snapshots were deleted by checking they no longer exist
			placementKey := types.NamespacedName{
				Name:      tt.placementObj.GetName(),
				Namespace: tt.placementObj.GetNamespace(),
			}
			result, err := ListAllResourceSnapshots(context.Background(), k8Client, placementKey)
			if err != nil {
				t.Fatalf("Expected no error when listing snapshots after deletion, but got: %v", err)
			}

			if len(result.GetResourceSnapshotObjs()) != 0 {
				t.Errorf("Expected 0 resource snapshots after deletion, got %d", len(result.GetResourceSnapshotObjs()))
			}
		})
	}
}

// errorClient is a mock client that returns errors on List operations
type errorClient struct {
	client.Client
}

func (e *errorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return fmt.Errorf("failed to list")
}
