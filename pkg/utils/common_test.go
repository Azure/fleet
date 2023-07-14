/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestExtractNumOfClustersFromPolicySnapshot tests the extractNumOfClustersFromPolicySnapshot function.
func TestExtractNumOfClustersFromPolicySnapshot(t *testing.T) {
	policyName := "test-policy"

	testCases := []struct {
		name              string
		policy            *fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantNumOfClusters int
		expectedToFail    bool
	}{
		{
			name: "valid annotation",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: "1",
					},
				},
			},
			wantNumOfClusters: 1,
		},
		{
			name: "no annotation",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			},
			expectedToFail: true,
		},
		{
			name: "invalid annotation: not an integer",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: "abc",
					},
				},
			},
			expectedToFail: true,
		},
		{
			name: "invalid annotation: negative integer",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: "-1",
					},
				},
			},
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			numOfClusters, err := ExtractNumOfClustersFromPolicySnapshot(tc.policy)
			if tc.expectedToFail {
				if err == nil {
					t.Fatalf("extractNumOfClustersFromPolicySnapshot() = %v, %v, want error", numOfClusters, err)
				}
				return
			}

			if numOfClusters != tc.wantNumOfClusters {
				t.Fatalf("extractNumOfClustersFromPolicySnapshot() = %v, %v, want %v, nil", numOfClusters, err, tc.wantNumOfClusters)
			}
		})
	}
}

func TestExtractSubindexFromClusterResourceSnapshot(t *testing.T) {
	snapshotName := "test-snapshot"

	testCases := []struct {
		name         string
		snapshot     *fleetv1beta1.ClusterResourceSnapshot
		wantExist    bool
		wantSubindex int
		wantError    bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
					},
				},
			},
			wantExist:    true,
			wantSubindex: 1,
		},
		{
			name: "no annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			wantExist:    false,
			wantSubindex: -1,
		},
		{
			name: "invalid annotation: not an integer",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "abc",
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid annotation: negative integer",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotExist, gotSubindex, err := ExtractSubindexFromClusterResourceSnapshot(tc.snapshot)
			if tc.wantError {
				if err == nil {
					t.Fatalf("ExtractSubindexFromClusterResourceSnapshot() = %v, %v, want error", gotExist, gotSubindex)
				}
				return
			}

			if gotExist != tc.wantExist || gotSubindex != tc.wantSubindex {
				t.Fatalf("ExtractSubindexFromClusterResourceSnapshot() = %v, %v, want %v, %v", gotExist, gotSubindex, tc.wantExist, tc.wantSubindex)
			}
		})
	}
}
