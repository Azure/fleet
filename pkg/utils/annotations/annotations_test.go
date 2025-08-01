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

package annotations

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	policyName   = "test-policy"
	snapshotName = "test-snapshot"
)

// TestExtractNumOfClustersFromPolicySnapshot tests the extractNumOfClustersFromPolicySnapshot function.
func TestExtractNumOfClustersFromPolicySnapshot(t *testing.T) {
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

// TestExtractObservedCRPGenerationFromPolicySnapshot tests the ExtractObservedCRPGenerationFromPolicySnapshot function.
func TestExtractObservedCRPGenerationFromPolicySnapshot(t *testing.T) {
	testCases := []struct {
		name              string
		policy            *fleetv1beta1.ClusterSchedulingPolicySnapshot
		wantCRPGeneration int64
		expectedToFail    bool
	}{
		{
			name: "valid annotation",
			policy: &fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
					Annotations: map[string]string{
						fleetv1beta1.CRPGenerationAnnotation: "1",
					},
				},
			},
			wantCRPGeneration: 1,
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
						fleetv1beta1.CRPGenerationAnnotation: "abc",
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
						fleetv1beta1.CRPGenerationAnnotation: "-1",
					},
				},
			},
			expectedToFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			observedCRPGeneration, err := ExtractObservedPlacementGenerationFromPolicySnapshot(tc.policy)
			if tc.expectedToFail {
				if err == nil {
					t.Fatalf("ExtractObservedCRPGenerationFromPolicySnapshot() = %v, %v, want error", observedCRPGeneration, err)
				}
				return
			}

			if observedCRPGeneration != tc.wantCRPGeneration {
				t.Fatalf("ExtractObservedCRPGenerationFromPolicySnapshot() = %v, %v, want %v, nil", observedCRPGeneration, err, tc.wantCRPGeneration)
			}
		})
	}
}

func TestExtractNumberOfResourceSnapshots(t *testing.T) {
	snapshotName := "test-snapshot"

	testCases := []struct {
		name      string
		snapshot  *fleetv1beta1.ClusterResourceSnapshot
		want      int
		wantError bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: 1,
		},
		{
			name: "no annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			wantError: true,
		},
		{
			name: "invalid annotation: not an integer",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "abc",
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
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "-1",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractNumberOfResourceSnapshotsFromResourceSnapshot(tc.snapshot)
			if gotErr := err != nil; gotErr != tc.wantError {
				t.Fatalf("ExtractNumberOfResourceSnapshotsFromResourceSnapshot() got err %v, want err %v", err, tc.wantError)
			}
			if !tc.wantError && got != tc.want {
				t.Fatalf("ExtractNumberOfResourceSnapshotsFromResourceSnapshot() = %v, want err %v", got, tc.want)
			}
		})
	}
}

func TestExtractNumberOfEnvelopeObjFromResourceSnapshot(t *testing.T) {
	testCases := []struct {
		name      string
		snapshot  *fleetv1beta1.ClusterResourceSnapshot
		want      int
		wantError bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation: "0",
					},
				},
			},
			want: 0,
		},
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation: "1",
					},
				},
			},
			want: 1,
		},
		{
			name: "no annotation means no enveloped objects",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			want: 0,
		},
		{
			name: "invalid annotation: not an integer",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation: "abc",
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
						fleetv1beta1.NumberOfEnvelopedObjectsAnnotation: "-1",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractNumberOfEnvelopeObjFromResourceSnapshot(tc.snapshot)
			if gotErr := err != nil; gotErr != tc.wantError {
				t.Fatalf("ExtractNumberOfEnvelopeObjFromResourceSnapshot() got err %v, want err %v", err, tc.wantError)
			}
			if !tc.wantError && got != tc.want {
				t.Fatalf("ExtractNumberOfEnvelopeObjFromResourceSnapshot() got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot(t *testing.T) {
	validTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	testCases := []struct {
		name      string
		snapshot  *fleetv1beta1.ClusterResourceSnapshot
		want      time.Time
		wantError bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: validTime.Format(time.RFC3339),
					},
				},
			},
			want: validTime,
		},
		{
			name: "no annotation means no next detection time",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			want: time.Time{},
		},
		{
			name: "invalid annotation format",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: "invalid-time",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot(tc.snapshot)
			if gotErr := err != nil; gotErr != tc.wantError {
				t.Fatalf("ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot() got err %v, want err %v", err, tc.wantError)
			}
			if !tc.wantError && got != tc.want {
				t.Errorf("ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot() got %s, want %s", got, tc.want)
			}
		})
	}
}
func TestParseResourceGroupHashFromAnnotation(t *testing.T) {
	testCases := []struct {
		name      string
		snapshot  *fleetv1beta1.ClusterResourceSnapshot
		want      string
		wantError bool
	}{
		{
			name: "valid annotation",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "abc123",
					},
				},
			},
			want: "abc123",
		},
		{
			name: "no annotations",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			wantError: true,
		},
		{
			name: "annotation not set",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						"other-annotation": "value",
					},
				},
			},
			wantError: true,
		},
		{
			name: "empty annotation value",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "",
					},
				},
			},
			want: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseResourceGroupHashFromAnnotation(tc.snapshot)
			if gotErr := err != nil; gotErr != tc.wantError {
				t.Fatalf("ParseResourceGroupHashFromAnnotation() got err %v, want err %v", err, tc.wantError)
			}
			if !tc.wantError && got != tc.want {
				t.Fatalf("ParseResourceGroupHashFromAnnotation() got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestExtractSubindexFromResourceSnapshot(t *testing.T) {
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
			name: "no annotations",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
				},
			},
			wantExist:    false,
			wantSubindex: 0,
		},
		{
			name: "annotation not set",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						"other-annotation": "value",
					},
				},
			},
			wantExist:    false,
			wantSubindex: 0,
		},
		{
			name: "empty annotation value",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "",
					},
				},
			},
			wantExist:    false,
			wantSubindex: 0,
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
		{
			name: "valid annotation with zero value",
			snapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: snapshotName,
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
					},
				},
			},
			wantExist:    true,
			wantSubindex: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotExist, gotSubindex, err := ExtractSubindexFromResourceSnapshot(tc.snapshot)
			if gotErr := err != nil; gotErr != tc.wantError {
				t.Fatalf("ExtractSubindexFromResourceSnapshot() got err %v, want err %v", err, tc.wantError)
			}
			if !tc.wantError {
				if gotExist != tc.wantExist || gotSubindex != tc.wantSubindex {
					t.Fatalf("ExtractSubindexFromResourceSnapshot() = %v, %v, want %v, %v", gotExist, gotSubindex, tc.wantExist, tc.wantSubindex)
				}
			}
		})
	}
}
