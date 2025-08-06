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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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

func TestFetchLatestPolicySnapshot(t *testing.T) {
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
			name: "fetch latest cluster scoped policy snapshots",
			placementKey: types.NamespacedName{
				Name: placementName,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
			},
			expectedSnapshots: 1,
		},
		{
			name: "fetch latest cluster scoped policy snapshots - mixed setup",
			placementKey: types.NamespacedName{
				Name: placementName,
			},
			existingSnapshots: []client.Object{
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
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
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
			},
			expectedSnapshots: 1, // Should only return cluster-scoped ones for cluster-scoped placement
		},
		{
			name: "fetch latest namespaced policy snapshots",
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
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
			},
			expectedSnapshots: 1,
		},
		{
			name: "fetch latest namespaced policy snapshots - mixed setup",
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
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.SchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement-1",
						Namespace: policySnapshotNamespace,
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
				// Add cluster-scoped snapshots to test mixed scenario
				&fleetv1beta1.ClusterSchedulingPolicySnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement-0",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: placementName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
			},
			expectedSnapshots: 1, // Should only return namespaced ones for namespaced placement
		},
		{
			name: "no latest policy snapshots found",
			placementKey: types.NamespacedName{
				Name: placementName,
			},
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

			snapshots, err := FetchLatestPolicySnapshot(ctx, fakeClient, tc.placementKey)
			if err != nil {
				t.Fatalf("FetchLatestPolicySnapshot() = %v, want nil", err)
			}

			if len(snapshots.GetPolicySnapshotObjs()) != tc.expectedSnapshots {
				t.Errorf("Expected %d snapshots, got %d", tc.expectedSnapshots, len(snapshots.GetPolicySnapshotObjs()))
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
