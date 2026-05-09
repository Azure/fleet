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
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	policySnapshotName      = "test-placement-0"
	policySnapshotNamespace = "test-namespace"
	placementName           = "test-placement"
	policyHash              = "test-hash"
)

func TestDeletePolicySnapshots(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	testCases := []struct {
		name                       string
		placementObj               fleetv1beta1.PlacementObj
		existingSnapshots          []fleetv1beta1.PolicySnapshotObj
		expectedRemainingSnapshots []fleetv1beta1.PolicySnapshotObj
	}{
		{
			name: "delete cluster scoped policy snapshots",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementName,
				},
			},
			existingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedRemainingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
		},
		{
			name: "delete namespaced policy snapshots",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementName,
					Namespace: policySnapshotNamespace,
				},
			},
			existingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policySnapshotName,
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement-0",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedRemainingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement-0",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
		},
		{
			name: "delete multiple cluster scoped policy snapshots",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementName,
				},
			},
			existingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedRemainingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
		},
		{
			name: "delete multiple namespaced policy snapshots",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementName,
					Namespace: policySnapshotNamespace,
				},
			},
			existingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      policySnapshotName,
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-2",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement-0",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "different-placement-0",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "different-placement",
						},
					},
				},
			},
			expectedRemainingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement-0",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "different-placement-0",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "different-placement",
						},
					},
				},
			},
		},
		{
			name: "delete all cluster scoped policy snapshots for placement",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementName,
				},
			},
			existingSnapshots: []fleetv1beta1.PolicySnapshotObj{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: policySnapshotName,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
			},
			expectedRemainingSnapshots: nil,
		},
		{
			name: "no policy snapshots to delete",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementName,
				},
			},
			existingSnapshots:          []fleetv1beta1.PolicySnapshotObj{},
			expectedRemainingSnapshots: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Convert PolicySnapshotObj to client.Object for fake client
			var existingObjects []client.Object
			for _, snapshot := range tc.existingSnapshots {
				existingObjects = append(existingObjects, snapshot.(client.Object))
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingObjects...).
				Build()

			if err := DeletePolicySnapshots(ctx, fakeClient, tc.placementObj); err != nil {
				t.Fatalf("DeletePolicySnapshots() = %v, want nil", err)
			}

			// Verify remaining snapshots
			if tc.placementObj.GetNamespace() != "" {
				var remainingSnapshots fleetv1beta1.SchedulingPolicySnapshotList
				if err := fakeClient.List(ctx, &remainingSnapshots); err != nil {
					t.Fatalf("Failed to list remaining snapshots: %v", err)
				}

				// Convert items to PolicySnapshotObj slice for comparison
				var gotSnapshots []fleetv1beta1.PolicySnapshotObj
				for i := range remainingSnapshots.Items {
					gotSnapshots = append(gotSnapshots, &remainingSnapshots.Items[i])
				}

				// Use cmp.Diff to compare with proper sorting and ignoring ResourceVersion
				if diff := cmp.Diff(tc.expectedRemainingSnapshots, gotSnapshots,
					cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
					cmpopts.SortSlices(func(o1, o2 fleetv1beta1.PolicySnapshotObj) bool {
						return o1.GetName() < o2.GetName()
					})); diff != "" {
					t.Errorf("DeletePolicySnapshots() mismatch (-want +got):\n%s", diff)
				}
			} else {
				var remainingSnapshots fleetv1beta1.ClusterSchedulingPolicySnapshotList
				if err := fakeClient.List(ctx, &remainingSnapshots); err != nil {
					t.Fatalf("Failed to list remaining snapshots: %v", err)
				}

				// Convert items to PolicySnapshotObj slice for comparison
				var gotSnapshots []fleetv1beta1.PolicySnapshotObj
				for i := range remainingSnapshots.Items {
					gotSnapshots = append(gotSnapshots, &remainingSnapshots.Items[i])
				}

				// Use cmp.Diff to compare with proper sorting and ignoring ResourceVersion
				if diff := cmp.Diff(tc.expectedRemainingSnapshots, gotSnapshots,
					cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
					cmpopts.SortSlices(func(o1, o2 fleetv1beta1.PolicySnapshotObj) bool {
						return o1.GetName() < o2.GetName()
					})); diff != "" {
					t.Errorf("DeletePolicySnapshots() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestBuildPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name                string
		placementObj        fleetv1beta1.PlacementObj
		policySnapshotIndex int
		policyHash          string
		expectedSnapshot    fleetv1beta1.PolicySnapshotObj
	}{
		{
			name: "build cluster scoped policy snapshot",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       placementName,
					Generation: 1,
				},
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
				},
			},
			policySnapshotIndex: 0,
			policyHash:          policyHash,
			expectedSnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName,
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: placementName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PolicyIndexLabel:       "0",
					},
					Annotations: map[string]string{
						fleetv1beta1.CRPGenerationAnnotation: "1",
					},
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
					PolicyHash: []byte(policyHash),
				},
			},
		},
		{
			name: "build cluster scoped policy snapshot with selectN",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       placementName,
					Generation: 2,
				},
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(3)),
					},
				},
			},
			policySnapshotIndex: 1,
			policyHash:          policyHash,
			expectedSnapshot: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-placement-1",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: placementName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PolicyIndexLabel:       "1",
					},
					Annotations: map[string]string{
						fleetv1beta1.CRPGenerationAnnotation:    "2",
						fleetv1beta1.NumberOfClustersAnnotation: "3",
					},
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(3)),
					},
					PolicyHash: []byte(policyHash),
				},
			},
		},
		{
			name: "build namespaced policy snapshot",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       placementName,
					Namespace:  policySnapshotNamespace,
					Generation: 1,
				},
				Spec: fleetv1beta1.PlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
				},
			},
			policySnapshotIndex: 0,
			policyHash:          policyHash,
			expectedSnapshot: &fleetv1beta1.SchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policySnapshotName,
					Namespace: policySnapshotNamespace,
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: placementName,
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
						fleetv1beta1.PolicyIndexLabel:       "0",
					},
					Annotations: map[string]string{
						fleetv1beta1.CRPGenerationAnnotation: "1",
					},
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
					PolicyHash: []byte(policyHash),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := BuildPolicySnapshot(tc.placementObj, tc.policySnapshotIndex, tc.policyHash)
			if diff := cmp.Diff(tc.expectedSnapshot, snapshot); diff != "" {
				t.Errorf("BuildPolicySnapshot() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListPolicySnapshots(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	testCases := []struct {
		name              string
		placementKey      types.NamespacedName
		existingSnapshots []client.Object
		expectedSnapshots int
	}{
		{
			name: "list cluster scoped policy snapshots",
			placementKey: types.NamespacedName{
				Name: placementName,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedSnapshots: 2,
		},
		{
			name: "list cluster scoped policy snapshots - mixed setup",
			placementKey: types.NamespacedName{
				Name: placementName,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
				// Add namespaced snapshots to test mixed scenario
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-0",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
			},
			expectedSnapshots: 3, // Should only return cluster-scoped ones for cluster-scoped placement
		},
		{
			name: "list namespaced policy snapshots",
			placementKey: types.NamespacedName{
				Name:      placementName,
				Namespace: policySnapshotNamespace,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-0",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement-0",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedSnapshots: 2,
		},
		{
			name: "list namespaced policy snapshots - mixed setup",
			placementKey: types.NamespacedName{
				Name:      placementName,
				Namespace: policySnapshotNamespace,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-0",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement-0",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
				// Add cluster-scoped snapshots to test mixed scenario
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
						},
					},
				},
			},
			expectedSnapshots: 2, // Should only return namespaced ones for namespaced placement
		},
		{
			name: "no policy snapshots found",
			placementKey: types.NamespacedName{
				Name: placementName,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
				},
			},
			expectedSnapshots: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingSnapshots...).
				Build()

			snapshots, err := ListPolicySnapshots(ctx, fakeClient, tc.placementKey)
			if err != nil {
				t.Fatalf("ListPolicySnapshots() = %v, want nil", err)
			}

			if len(snapshots.GetPolicySnapshotObjs()) != tc.expectedSnapshots {
				t.Errorf("Expected %d snapshots, got %d", tc.expectedSnapshots, len(snapshots.GetPolicySnapshotObjs()))
			}
		})
	}
}

func TestIsLatestPolicySnapshot(t *testing.T) {
	snapshotWithLabels := func(labels map[string]string) fleetv1beta1.PolicySnapshotObj {
		return &fleetv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:   policySnapshotName,
				Labels: labels,
			},
		}
	}

	testCases := []struct {
		name     string
		snapshot fleetv1beta1.PolicySnapshotObj
		want     bool
		wantErr  bool
	}{
		{
			name:     "label true returns true",
			snapshot: snapshotWithLabels(map[string]string{fleetv1beta1.IsLatestSnapshotLabel: "true"}),
			want:     true,
			wantErr:  false,
		},
		{
			name:     "label false returns false",
			snapshot: snapshotWithLabels(map[string]string{fleetv1beta1.IsLatestSnapshotLabel: "false"}),
			want:     false,
			wantErr:  false,
		},
		{
			name:     "label missing returns false with unexpected-behavior error",
			snapshot: snapshotWithLabels(map[string]string{fleetv1beta1.PlacementTrackingLabel: placementName}),
			want:     false,
			wantErr:  true,
		},
		{
			name:     "nil labels returns false with unexpected-behavior error",
			snapshot: snapshotWithLabels(nil),
			want:     false,
			wantErr:  true,
		},
		{
			name:     "label malformed returns false with unexpected-behavior error",
			snapshot: snapshotWithLabels(map[string]string{fleetv1beta1.IsLatestSnapshotLabel: "yes"}),
			want:     false,
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsLatestPolicySnapshot(tc.snapshot)
			if (err != nil) != tc.wantErr {
				t.Errorf("IsLatestPolicySnapshot() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrUnexpectedBehavior) {
				t.Errorf("IsLatestPolicySnapshot() error = %v, want wrapping ErrUnexpectedBehavior", err)
			}
			if got != tc.want {
				t.Errorf("IsLatestPolicySnapshot() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLookupLatestPolicySnapshot(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	clusterKey := types.NamespacedName{Name: placementName}
	namespacedKey := types.NamespacedName{Name: placementName, Namespace: policySnapshotNamespace}

	clusterLatestSnapshot := func(name string) *fleetv1beta1.ClusterSchedulingPolicySnapshot {
		return &fleetv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					fleetv1beta1.PlacementTrackingLabel: placementName,
					fleetv1beta1.IsLatestSnapshotLabel:  "true",
				},
			},
		}
	}
	namespacedLatestSnapshot := func(name string) *fleetv1beta1.SchedulingPolicySnapshot {
		return &fleetv1beta1.SchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: policySnapshotNamespace,
				Labels: map[string]string{
					fleetv1beta1.PlacementTrackingLabel: placementName,
					fleetv1beta1.IsLatestSnapshotLabel:  "true",
				},
			},
		}
	}

	testCases := []struct {
		name              string
		placementKey      types.NamespacedName
		existingSnapshots []client.Object
		wantName          string
		wantErrSentinel   error // nil means a snapshot is expected; otherwise the error must wrap this sentinel.
	}{
		{
			name:              "exactly one cluster-scoped latest snapshot",
			placementKey:      clusterKey,
			existingSnapshots: []client.Object{clusterLatestSnapshot("test-placement-0")},
			wantName:          "test-placement-0",
		},
		{
			name:              "exactly one namespaced latest snapshot",
			placementKey:      namespacedKey,
			existingSnapshots: []client.Object{namespacedLatestSnapshot("test-placement-0")},
			wantName:          "test-placement-0",
		},
		{
			name:              "no latest snapshot returns ErrNoLatestPolicySnapshot",
			placementKey:      clusterKey,
			existingSnapshots: nil,
			wantErrSentinel:   ErrNoLatestPolicySnapshot,
		},
		{
			name:         "only non-latest snapshots present returns ErrNoLatestPolicySnapshot",
			placementKey: clusterKey,
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
			},
			wantErrSentinel: ErrNoLatestPolicySnapshot,
		},
		{
			name:         "multiple latest snapshots returns ErrMultipleActivePolicySnapshots",
			placementKey: clusterKey,
			existingSnapshots: []client.Object{
				clusterLatestSnapshot("test-placement-0"),
				clusterLatestSnapshot("test-placement-1"),
			},
			wantErrSentinel: ErrMultipleActivePolicySnapshots,
		},
		{
			name:         "ignores snapshots from other placements",
			placementKey: clusterKey,
			existingSnapshots: []client.Object{
				clusterLatestSnapshot("test-placement-0"),
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
			},
			wantName: "test-placement-0",
		},
		{
			name:         "ignores non-latest snapshots",
			placementKey: clusterKey,
			existingSnapshots: []client.Object{
				clusterLatestSnapshot("test-placement-1"),
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
			},
			wantName: "test-placement-1",
		},
		{
			name:         "cluster-scoped lookup ignores a same-named latest namespaced snapshot",
			placementKey: clusterKey,
			existingSnapshots: []client.Object{
				clusterLatestSnapshot("test-placement-0"),
				namespacedLatestSnapshot("test-placement-0"),
			},
			wantName: "test-placement-0",
		},
		{
			name:         "namespaced lookup ignores a same-named latest cluster snapshot",
			placementKey: namespacedKey,
			existingSnapshots: []client.Object{
				namespacedLatestSnapshot("test-placement-0"),
				clusterLatestSnapshot("test-placement-0"),
			},
			wantName: "test-placement-0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingSnapshots...).
				Build()

			got, err := LookupLatestPolicySnapshot(ctx, fakeClient, tc.placementKey)
			if (err != nil) != (tc.wantErrSentinel != nil) {
				t.Fatalf("LookupLatestPolicySnapshot() error = %v, wantErrSentinel %v", err, tc.wantErrSentinel)
			}
			if tc.wantErrSentinel != nil {
				if !errors.Is(err, tc.wantErrSentinel) {
					t.Errorf("LookupLatestPolicySnapshot() error = %v, want wrapping %v", err, tc.wantErrSentinel)
				}
				return
			}
			if got == nil || got.GetName() != tc.wantName {
				t.Errorf("LookupLatestPolicySnapshot() returned snapshot %v, want name %q", got, tc.wantName)
			}
		})
	}
}

// TestLookupLatestPolicySnapshot_ListError covers the API-error path: when the underlying List
// call fails with an error that isUnexpectedCacheError treats as unexpected (i.e. not a
// *apierrors.StatusError, context.Canceled, or context.DeadlineExceeded), NewAPIServerError
// promotes it to ErrUnexpectedBehavior.
func TestLookupLatestPolicySnapshot_ListError(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	listErr := fmt.Errorf("simulated cache failure")
	fakeClient := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return listErr
			},
		},
	)

	got, err := LookupLatestPolicySnapshot(context.Background(), fakeClient, types.NamespacedName{Name: placementName})
	if err == nil {
		t.Fatalf("LookupLatestPolicySnapshot() = %v, nil; want non-nil error", got)
	}
	if got != nil {
		t.Errorf("LookupLatestPolicySnapshot() returned snapshot %v, want nil", got)
	}
	if !errors.Is(err, ErrUnexpectedBehavior) {
		t.Errorf("LookupLatestPolicySnapshot() error = %v, want wrapping ErrUnexpectedBehavior", err)
	}
}
