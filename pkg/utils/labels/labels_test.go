/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package labels

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	snapshotName = "test-snapshot"
)

func TestExtractResourceIndexFromClusterResourceSnapshot(t *testing.T) {
	testCases := []struct {
		name      string
		snapshot  *fleetv1beta1.ClusterResourceSnapshot
		wantIndex int
		wantError bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "1",
					},
				},
			},
			wantIndex: 1,
		},
		{
			name: "no label",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			wantIndex: -1,
		},
		{
			name: "invalid label: not an integer",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "abc",
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid label: negative integer",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "-1",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotIndex, err := ExtractResourceIndexFromClusterResourceSnapshot(tc.snapshot)
			if tc.wantError {
				if err == nil {
					t.Fatalf("ExtractResourceIndexFromClusterResourceSnapshot() =  %v, want error", gotIndex)
				}
				return
			}

			if gotIndex != tc.wantIndex {
				t.Fatalf("ExtractResourceIndexFromClusterResourceSnapshot() = %v, want %v", gotIndex, tc.wantIndex)
			}
		})
	}
}

func TestExtractResourceSnapshotIndexFromWork(t *testing.T) {
	testCases := []struct {
		name      string
		snapshot  *fleetv1beta1.Work
		wantIndex int
		wantError bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
					},
				},
			},
			wantIndex: 1,
		},
		{
			name: "no label",
			snapshot: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			wantIndex: -1,
		},
		{
			name: "invalid label: not an integer",
			snapshot: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "abc",
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid label: negative integer",
			snapshot: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "-1",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotIndex, err := ExtractResourceSnapshotIndexFromWork(tc.snapshot)
			if tc.wantError {
				if err == nil {
					t.Fatalf("ExtractResourceSnapshotIndexFromWork() =  %v, want error", gotIndex)
				}
				return
			}

			if gotIndex != tc.wantIndex {
				t.Fatalf("ExtractResourceSnapshotIndexFromWork() = %v, want %v", gotIndex, tc.wantIndex)
			}
		})
	}
}
