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

package labels

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
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
			gotIndex, err := ExtractResourceIndexFromResourceSnapshot(tc.snapshot)
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

func TestExtractLabelFromMemberCluster(t *testing.T) {
	tests := []struct {
		name      string
		cluster   *clusterv1beta1.MemberCluster
		labelKey  string
		wantValue string
		wantError bool
	}{
		{
			name: "cluster has subscription ID label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						"subID":       "12345678-1234-1234-1234-123456789012",
						"other-label": "other-value",
					},
				},
			},
			labelKey:  "subID",
			wantValue: "12345678-1234-1234-1234-123456789012",
		},
		{
			name: "cluster with has location label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						"location":    "westus",
						"other-label": "other-value",
					},
				},
			},
			labelKey:  "location",
			wantValue: "westus",
		},
		{
			name: "cluster does not have expected label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						"other-label": "other-value",
					},
				},
			},
			labelKey:  "label-not-exist",
			wantError: true,
		},
		{
			name: "cluster with no labels",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			labelKey:  "label-key",
			wantError: true,
		},
		{
			name:      "nil cluster",
			cluster:   nil,
			labelKey:  "label-key",
			wantError: true,
		},
		{
			name: "cluster with empty label value",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
					Labels: map[string]string{
						"empty-label": "",
					},
				},
			},
			labelKey:  "empty-label",
			wantValue: "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := ExtractLabelFromMemberCluster(tt.cluster, tt.labelKey)

			if (err != nil) != tt.wantError {
				t.Fatalf("ExtractLabelFromMemberCluster() error = %v, want %v", err, tt.wantError)
			}

			if value != tt.wantValue {
				t.Errorf("ExtractLabelFromMemberCluster() = %v, want %v", value, tt.wantValue)
			}
		})
	}
}
