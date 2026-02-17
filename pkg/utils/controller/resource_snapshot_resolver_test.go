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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
	"github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

const (
	testCRPName         = "my-crp"
	placementGeneration = 15
)

var resourceSnapshotCmpOptions = []cmp.Option{
	// ignore the TypeMeta and ObjectMeta fields that may differ between expected and actual
	cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "Generation", "CreationTimestamp", "ManagedFields"),
	cmp.Comparer(func(t1, t2 metav1.Time) bool {
		// we're within the margin (1s) if x + margin >= y
		return !t1.Time.Add(1 * time.Second).Before(t2.Time)
	}),
}

var (
	fleetAPIVersion                   = fleetv1beta1.GroupVersion.String()
	sortClusterResourceSnapshotOption = cmpopts.SortSlices(func(r1, r2 fleetv1beta1.ClusterResourceSnapshot) bool {
		return r1.Name < r2.Name
	})

	singleRevisionLimit   = int32(1)
	multipleRevisionLimit = int32(2)
	invalidRevisionLimit  = int32(0)
)

func placementPolicyForTest() *fleetv1beta1.PlacementPolicy {
	return &fleetv1beta1.PlacementPolicy{
		PlacementType:    fleetv1beta1.PickNPlacementType,
		NumberOfClusters: ptr.To(int32(3)),
		Affinity: &fleetv1beta1.Affinity{
			ClusterAffinity: &fleetv1beta1.ClusterAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &fleetv1beta1.ClusterSelector{
					ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
		},
	}
}

func clusterResourcePlacementForTest() *fleetv1beta1.ClusterResourcePlacement {
	return &fleetv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testCRPName,
			Generation: placementGeneration,
		},
		Spec: fleetv1beta1.PlacementSpec{
			ResourceSelectors: []fleetv1beta1.ResourceSelectorTerm{
				{
					Group:   corev1.GroupName,
					Version: "v1",
					Kind:    "Service",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "east"},
					},
				},
			},
			Policy: placementPolicyForTest(),
		},
	}
}

func TestFetchAllResourceSnapshotsAlongWithMaster(t *testing.T) {
	tests := []struct {
		name      string
		master    fleetv1beta1.ResourceSnapshotObj
		snapshots []fleetv1beta1.ResourceSnapshotObj
		want      map[string]fleetv1beta1.ResourceSnapshotObj
		wantErr   error
	}{
		{
			name: "single cluster resource snapshot",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
		},
		{
			name: "single namespaced resource snapshot",
			master: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Namespace: defaultNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "def",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "def",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
		},
		{
			name: "multiple cluster resource snapshots",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
				},
			},
		},
		{
			name: "multiple namespaced resource snapshots",
			master: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Namespace: defaultNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "ghi",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "ghi",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
				},
			},
		},
		{
			name: "some of cluster resource snapshots have not been created yet",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
			},
			wantErr: ErrExpectedBehavior,
		},
		{
			name: "invalid numberOfResourceSnapshotsAnnotation",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "-1",
					},
				},
			},
			wantErr: ErrUnexpectedBehavior,
		},
		{
			name: "invalid resource index label of master resource snapshot",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "-2",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			wantErr: ErrUnexpectedBehavior,
		},
		{
			name: "mixed cluster and namespaced snapshots - fetch cluster-scoped",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "cluster-hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				// Cluster-scoped snapshot that should be included
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				// Namespaced snapshot that should NOT be included (different scope)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "cluster-hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
			},
		},
		{
			name: "mixed cluster and namespaced snapshots - fetch namespaced",
			master: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 1),
					Namespace: defaultNamespace,
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "1",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "namespaced-hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				// Cluster-scoped snapshot that should NOT be included (different scope)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				// Namespaced snapshots that should be included
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 0): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 1): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, defaultPlacementName, 1, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
				},
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 1): &fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 1),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "namespaced-hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
				},
			},
		},
		{
			name: "mixed cluster and namespaced snapshots - different placement names",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel:     "0",
						fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "mixed-hash",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			snapshots: []fleetv1beta1.ResourceSnapshotObj{
				// Cluster-scoped snapshot with different placement name (should NOT be included)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, "other-placement", 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: "other-placement",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "other-hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
				// Namespaced snapshot with same placement name but different scope (should NOT be included)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Namespace: defaultNamespace,
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "namespaced-hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
			want: map[string]fleetv1beta1.ResourceSnapshotObj{
				fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0): &fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, defaultPlacementName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: defaultPlacementName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "mixed-hash",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{tc.master}
			for _, snapshot := range tc.snapshots {
				objects = append(objects, snapshot)
			}

			// Determine placement key based on master snapshot type
			var placementKey string
			if tc.master.GetNamespace() == "" {
				// Cluster-scoped snapshot
				placementKey = defaultPlacementName
			} else {
				// Namespaced snapshot
				placementKey = defaultNamespace + namespaceSeparator + defaultPlacementName
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			got, err := FetchAllResourceSnapshotsAlongWithMaster(context.Background(), fakeClient, placementKey, tc.master)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("FetchAllResourceSnapshotsAlongWithMaster() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
			}
			sortedKeys := slices.Sorted(maps.Keys(got))
			for i := range sortedKeys {
				key := sortedKeys[i]
				wantResourceSnapshotObj := tc.want[key]
				if diff := cmp.Diff(wantResourceSnapshotObj, got[key], options...); diff != "" {
					t.Errorf("FetchAllResourceSnapshotsAlongWithMaster() mismatch (-want, +got):\n%s", diff)
				}
			}
		})
	}
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
		{
			name: "mixed environment - fetch cluster-scoped master",
			placementKey: types.NamespacedName{
				Name: "test-crp",
			},
			existingSnapshots: []client.Object{
				// Cluster-scoped master snapshot (should be returned)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-master",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "cluster-hash",
						},
					},
				},
				// Namespaced snapshot with same placement name (should be ignored)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-master",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "namespaced-hash",
						},
					},
				},
			},
			expectedResult: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-master",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "cluster-hash",
					},
				},
			},
		},
		{
			name: "mixed environment - fetch namespaced master",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-crp",
			},
			existingSnapshots: []client.Object{
				// Cluster-scoped snapshot with same placement name (should be ignored)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-master",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "cluster-hash",
						},
					},
				},
				// Namespaced master snapshot (should be returned)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-master",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "namespaced-hash",
						},
					},
				},
			},
			expectedResult: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-namespaced-master",
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.PlacementTrackingLabel: "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel:  "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "namespaced-hash",
					},
				},
			},
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
		{
			name: "mixed environment - list latest cluster-scoped snapshots",
			placementKey: types.NamespacedName{
				Name: "test-placement",
			},
			objects: []client.Object{
				// Cluster-scoped latest snapshots (should be included)
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
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
				// Namespaced snapshots with same placement name (should be ignored)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "mixed environment - list latest namespaced snapshots",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-placement",
			},
			objects: []client.Object{
				// Cluster-scoped snapshots with same placement name (should be ignored)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				// Namespaced latest snapshots (should be included)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
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
		{
			name: "mixed environment - list all cluster-scoped snapshots",
			placementKey: types.NamespacedName{
				Name: "test-placement",
			},
			objects: []client.Object{
				// Cluster-scoped snapshots (should be included)
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
				// Namespaced snapshots with same placement name (should be ignored)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "mixed environment - list all namespaced snapshots",
			placementKey: types.NamespacedName{
				Namespace: "test-namespace",
				Name:      "test-placement",
			},
			objects: []client.Object{
				// Cluster-scoped snapshots with same placement name (should be ignored)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
				// Namespaced snapshots (should be included)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
					},
				},
				// Different namespace (should be ignored)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-namespaced-snapshot",
						Namespace: "other-namespace",
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
		{
			name:                  "mixed environment - list cluster-scoped snapshots with index",
			resourceSnapshotIndex: "5",
			placementName:         "test-placement",
			placementNamespace:    "",
			objects: []client.Object{
				// Cluster-scoped snapshots with matching index (should be included)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "5",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "6",
						},
					},
				},
				// Namespaced snapshots with same placement name and index (should be ignored)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "5",
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name:                  "mixed environment - list namespaced snapshots with index",
			resourceSnapshotIndex: "7",
			placementName:         "test-placement",
			placementNamespace:    "test-namespace",
			objects: []client.Object{
				// Cluster-scoped snapshots with same placement name and index (should be ignored)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "7",
						},
					},
				},
				// Namespaced snapshots with matching index (should be included)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "7",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "8",
						},
					},
				},
				// Different namespace (should be ignored)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-namespaced-snapshot",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
							fleetv1beta1.ResourceIndexLabel:     "7",
						},
					},
				},
			},
			expectedCount: 1,
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
				tt.latestResourceSnapshotIndex,
				tt.resourceSnapshotCount,
				tt.envelopeObjCount,
				tt.placementObj.GetName(),
				tt.placementObj.GetNamespace(),
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
		{
			name: "mixed environment - delete cluster-scoped snapshots only",
			placementObj: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
			},
			objects: []client.Object{
				// Cluster-scoped snapshots (should be deleted)
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
						},
					},
				},
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-2",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
						},
					},
				},
				// Namespaced snapshots with same placement name (should NOT be deleted)
				// TODO: find a way to test this
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-crp",
						},
					},
				},
			},
		},
		{
			name: "mixed environment - delete namespaced snapshots only",
			placementObj: &fleetv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: "test-namespace",
				},
			},
			objects: []client.Object{
				// Cluster-scoped snapshots with same placement name (should NOT be deleted)
				// TODO: find a way to test this
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
				// Namespaced snapshots (should be deleted)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-namespaced-snapshot-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
				// Different namespace (should NOT be deleted)
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-namespaced-snapshot",
						Namespace: "other-namespace",
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
				},
			},
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

func TestGetOrCreateClusterResourceSnapshot(t *testing.T) {
	// test service is 383 bytes in size.
	serviceResourceContent := *resource.ServiceResourceContentForTest(t)
	// test deployment 390 bytes in size.
	deploymentResourceContent := *resource.DeploymentResourceContentForTest(t)
	// test secret is 152 bytes in size.
	secretResourceContent := *resource.SecretResourceContentForTest(t)

	jsonBytes, err := json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithSingleResourceHash hash: %v", err)
	}
	resourceSnapshotSpecWithEmptyResourceHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	jsonBytes, err = json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithSingleResource hash: %v", err)
	}
	resourceSnapshotSpecWithServiceResourceHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	jsonBytes, err = json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithMultipleResources hash: %v", err)
	}
	resourceSnapshotSpecWithTwoResourcesHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	jsonBytes, err = json.Marshal(&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent}})
	if err != nil {
		t.Fatalf("failed to create the resourceSnapshotSpecWithMultipleResources hash: %v", err)
	}
	resourceSnapshotSpecWithMultipleResourcesHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	now := metav1.Now()
	nowToString := now.Time.Format(time.RFC3339)
	tests := []struct {
		name                       string
		envelopeObjCount           int
		selectedResourcesSizeLimit int
		resourceSnapshotSpec       *fleetv1beta1.ResourceSnapshotSpec
		revisionHistoryLimit       *int32
		resourceSnapshots          []fleetv1beta1.ClusterResourceSnapshot
		wantResourceSnapshots      []fleetv1beta1.ClusterResourceSnapshot
		wantLatestSnapshotIndex    int // index of the wantPolicySnapshots array
		wantRequeue                bool
	}{
		{
			name:                 "new resourceSnapshot and no existing snapshots owned by my-crp",
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			revisionHistoryLimit: &invalidRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-crp-1",
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: "another-crp",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
				},
				// new resource snapshot owned by the my-crp
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                 "resource has no change",
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			revisionHistoryLimit: &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:             "resource has changed and there is no active snapshot with single revisionLimit",
			envelopeObjCount: 2,
			// It happens when last reconcile loop fails after setting the latest label to false and
			// before creating a new resource snapshot.
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
			revisionHistoryLimit: &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				// new resource snapshot
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithEmptyResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "2",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                 "resource has changed too fast and there is an active snapshot with multiple revisionLimit",
			envelopeObjCount:     3,
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
			revisionHistoryLimit: &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: nowToString,
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantRequeue:             true,
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                 "resource has changed and there is an active snapshot with multiple revisionLimit",
			envelopeObjCount:     3,
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
			revisionHistoryLimit: &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				// new resource snapshot
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithEmptyResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "3",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{}},
				},
			},
			wantLatestSnapshotIndex: 3,
		},
		{
			name:                 "resource has been changed and reverted back and there is no active snapshot",
			resourceSnapshotSpec: &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, no existing clusterResourceSnapshots",
			selectedResourcesSizeLimit: 600,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent}},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, master clusterResourceSnapshot created but not all sub-indexed clusterResourceSnapshots have been created",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent, deploymentResourceContent}},
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resources cross clusterResourceSnapshot limit, revision limit is 1, delete existing clusterResourceSnapshots & create new clusterResourceSnapshots",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "resource has changed too fast, selected resources cross clusterResourceSnapshot limit, revision limit is 1",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: nowToString,
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{deploymentResourceContent}},
				},
			},
			wantRequeue:             true,
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resources cross clusterResourceSnapshot limit, revision limit is 1, delete existing clusterResourceSnapshot with missing sub-indexed snapshots & create new clusterResourceSnapshots",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithMultipleResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "3",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resources cross clusterResourceSnapshot limit, revision limit is 2, don't delete existing clusterResourceSnapshots & create new clusterResourceSnapshots",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
						CreationTimestamp: metav1.NewTime(now.Time.Add(-1 * time.Hour)),
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:                          resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation:                  "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:                   "0",
							fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation: now.Add(-5 * time.Minute).Format(time.RFC3339),
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 2),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 2, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "2",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, all clusterResourceSnapshots remain the same since no change",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &singleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 0,
		},
		{
			name:                       "selected resource cross clusterResourceSnapshot size limit, all clusterResourceSnapshots remain the same, but IsLatestSnapshotLabel is set to false",
			selectedResourcesSizeLimit: 100,
			resourceSnapshotSpec:       &fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent, secretResourceContent}},
			revisionHistoryLimit:       &multipleRevisionLimit,
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
						CreationTimestamp: now,
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantResourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "false",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithServiceResourceHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         resourceSnapshotSpecWithTwoResourcesHash,
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
							fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{serviceResourceContent}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{SelectedResources: []fleetv1beta1.ResourceContent{secretResourceContent}},
				},
			},
			wantLatestSnapshotIndex: 1,
		},
	}
	originalResourceSnapshotResourceSizeLimit := resourceSnapshotResourceSizeLimit
	defer func() {
		resourceSnapshotResourceSizeLimit = originalResourceSnapshotResourceSizeLimit
	}()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			crp.Spec.RevisionHistoryLimit = tc.revisionHistoryLimit
			objects := []client.Object{crp}
			for i := range tc.resourceSnapshots {
				objects = append(objects, &tc.resourceSnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			resolver := NewResourceSnapshotResolver(fakeClient, scheme)
			resolver.Config = NewResourceSnapshotConfig(1*time.Minute, 0)
			limit := int32(defaulter.DefaultRevisionHistoryLimitValue)
			if tc.revisionHistoryLimit != nil {
				limit = *tc.revisionHistoryLimit
			}
			resourceSnapshotResourceSizeLimit = tc.selectedResourcesSizeLimit
			res, got, err := resolver.GetOrCreateResourceSnapshot(ctx, crp, tc.envelopeObjCount, tc.resourceSnapshotSpec, int(limit))
			if err != nil {
				t.Fatalf("failed to handle getOrCreateResourceSnapshot: %v", err)
			}
			if (res.RequeueAfter > 0) != tc.wantRequeue {
				t.Fatalf("GetOrCreateResourceSnapshot() got Requeue %v, want %v", (res.RequeueAfter > 0), tc.wantRequeue)
			}

			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "CreationTimestamp"),
				// Fake API server will add a newline for the runtime.RawExtension type.
				// ignoring the resourceContent field for now
				cmpopts.IgnoreFields(runtime.RawExtension{}, "Raw"),
			}
			if tc.wantRequeue {
				if res.RequeueAfter <= 0 {
					t.Fatalf("GetOrCreateResourceSnapshot() got RequeueAfter %v, want greater than zero value", res.RequeueAfter)
				}
			}
			annotationOption := cmp.Transformer("NormalizeAnnotations", func(m map[string]string) map[string]string {
				normalized := map[string]string{}
				for k, v := range m {
					if k == fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation {
						// Normalize the resource group hash annotation to a fixed value for comparison.
						if _, err := time.Parse(time.RFC3339, v); err != nil {
							normalized[k] = ""
						}
						normalized[k] = nowToString
					} else {
						normalized[k] = v
					}
				}
				return normalized
			})
			options = append(options, sortClusterResourceSnapshotOption, annotationOption)
			gotSnapshot, ok := got.(*fleetv1beta1.ClusterResourceSnapshot)
			if !ok {
				t.Fatalf("expected *fleetv1beta1.ClusterResourceSnapshot, got %T", got)
			}
			if diff := cmp.Diff(tc.wantResourceSnapshots[tc.wantLatestSnapshotIndex], *gotSnapshot, options...); diff != "" {
				t.Errorf("GetOrCreateResourceSnapshot() mismatch (-want, +got):\n%s", diff)
			}
			clusterResourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
			if err := fakeClient.List(ctx, clusterResourceSnapshotList); err != nil {
				t.Fatalf("clusterResourceSnapshot List() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantResourceSnapshots, clusterResourceSnapshotList.Items, options...); diff != "" {
				t.Errorf("clusterResourceSnapshot List() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestGetOrCreateClusterResourceSnapshot_failure(t *testing.T) {
	selectedResources := []fleetv1beta1.ResourceContent{
		*resource.ServiceResourceContentForTest(t),
	}
	resourceSnapshotSpecA := &fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}
	tests := []struct {
		name              string
		resourceSnapshots []fleetv1beta1.ClusterResourceSnapshot
	}{
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "existing active resource snapshot does not have resourceIndex label",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "existing active resource snapshot does not have hash annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "0",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and resourceSnapshot with invalid resourceIndex label",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and multiple resourceSnapshots with invalid resourceIndex label",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "abc",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and multiple resourceSnapshots with invalid subindex annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "0",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "0",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "abc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "1",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "abc",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "no active resource snapshot exists and multiple resourceSnapshots with invalid subindex (<0) annotation",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "0",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "0",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
							fleetv1beta1.ResourceIndexLabel:     "1",
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
							fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, testCRPName, 1, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
						},
					},
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterResourceSnapshot.
			name: "multiple active resource snapshot exist",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "0",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hashA",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "1",
							fleetv1beta1.IsLatestSnapshotLabel:  "true",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hashA",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
		},
		{
			// Should never hit this case unless there is a bug in the controller or customers manually modify the clusterPolicySnapshot.
			name: "no active resource snapshot exists and resourceSnapshot with invalid resourceIndex label (negative value)",
			resourceSnapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, testCRPName, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel:     "-12",
							fleetv1beta1.PlacementTrackingLabel: testCRPName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								Name:               testCRPName,
								BlockOwnerDeletion: ptr.To(true),
								Controller:         ptr.To(true),
								APIVersion:         fleetAPIVersion,
								Kind:               "ClusterResourcePlacement",
							},
						},
						Annotations: map[string]string{
							fleetv1beta1.ResourceGroupHashAnnotation: "hashA",
						},
					},
					Spec: *resourceSnapshotSpecA,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			crp := clusterResourcePlacementForTest()
			objects := []client.Object{crp}
			for i := range tc.resourceSnapshots {
				objects = append(objects, &tc.resourceSnapshots[i])
			}
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			resolver := NewResourceSnapshotResolver(fakeClient, scheme)
			res, _, err := resolver.GetOrCreateResourceSnapshot(ctx, crp, 0, resourceSnapshotSpecA, 1)
			if err == nil { // if error is nil
				t.Fatal("GetOrCreateClusterResourceSnapshot() = nil, want err")
			}
			if res.RequeueAfter > 0 {
				t.Fatal("GetOrCreateClusterResourceSnapshot() requeue = true, want false")
			}
			if !errors.Is(err, ErrUnexpectedBehavior) {
				t.Errorf("GetOrCreateClusterResourceSnapshot() got %v, want %v type", err, ErrUnexpectedBehavior)
			}
		})
	}
}

func TestShouldCreateNewResourceSnapshotNow(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name               string
		creationInterval   time.Duration
		collectionDuration time.Duration
		creationTime       time.Time
		annotationValue    string
		wantAnnoation      bool
		wantRequeue        ctrl.Result
	}{
		{
			name:               "ResourceSnapshotCreationMinimumInterval and ResourceChangesCollectionDuration are 0",
			creationInterval:   0,
			collectionDuration: 0,
			wantRequeue:        ctrl.Result{Requeue: false},
		},
		{
			name:               "ResourceSnapshotCreationMinimumInterval is 0",
			creationInterval:   0,
			collectionDuration: 30 * time.Second,
			annotationValue:    now.Add(-10 * time.Second).Format(time.RFC3339),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{RequeueAfter: 20 * time.Second},
		},
		{
			name:               "ResourceChangesCollectionDuration is 0",
			creationInterval:   300 * time.Second,
			collectionDuration: 0,
			creationTime:       now.Add(-5 * time.Second),
			// no annotation → sets it and requeues
			annotationValue: "",
			wantAnnoation:   true,
			wantRequeue:     ctrl.Result{RequeueAfter: 295 * time.Second},
		},
		{
			name:               "next detection time (now) + collection duration < latest resource snapshot creation time + creation interval",
			creationInterval:   300 * time.Second,
			collectionDuration: 30 * time.Second,
			creationTime:       now.Add(-5 * time.Second),
			// no annotation → sets it and requeues
			annotationValue: "",
			wantAnnoation:   true,
			wantRequeue:     ctrl.Result{RequeueAfter: 295 * time.Second},
		},
		{
			name:               "next detection time (annotation) + collection duration < latest resource snapshot creation time + creation interval",
			creationInterval:   300 * time.Second,
			collectionDuration: 30 * time.Second,
			creationTime:       now.Add(-10 * time.Second),
			annotationValue:    now.Add(-5 * time.Second).Format(time.RFC3339),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{RequeueAfter: 290 * time.Second},
		},
		{
			name:               "last resource snapshot created long time before",
			creationInterval:   60 * time.Second,
			collectionDuration: 30 * time.Second,
			creationTime:       now.Add(-1 * time.Hour),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{RequeueAfter: 30 * time.Second},
		},
		{
			name:               "next detection time (now) + collection duration >= latest resource snapshot creation time + creation interval",
			creationInterval:   60 * time.Second,
			collectionDuration: 60 * time.Second,
			creationTime:       now.Add(-40 * time.Second),
			wantAnnoation:      true,
			wantRequeue:        ctrl.Result{RequeueAfter: 60 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// initialize a snapshot with given creation time and annotation
			snapshot := &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-snapshot",
					CreationTimestamp: metav1.Time{Time: tc.creationTime},
					Annotations:       map[string]string{},
				},
			}
			if tc.annotationValue != "" {
				snapshot.Annotations[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation] = tc.annotationValue
			}

			// use fake client seeded with the snapshot
			scheme := serviceScheme(t)
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(snapshot.DeepCopy()).
				Build()

			resolver := NewResourceSnapshotResolver(client, nil)
			resolver.Config = NewResourceSnapshotConfig(tc.creationInterval, // Fast creation
				tc.collectionDuration, // Longer collection
			)

			ctx := context.Background()
			if err := client.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot); err != nil {
				t.Fatalf("Failed to get snapshot: %v", err)
			}
			got, err := resolver.shouldCreateNewResourceSnapshotNow(ctx, snapshot)
			if err != nil {
				t.Fatalf("shouldCreateNewResourceSnapshotNow() failed: %v", err)
			}
			cmpOptions := []cmp.Option{cmp.Comparer(func(d1, d2 time.Duration) bool {
				if d1 == 0 {
					return d2 == 0 // both are zero
				}
				return time.Duration.Abs(d1-d2) < 3*time.Second // allow 1 second difference
			})}
			if !cmp.Equal(got, tc.wantRequeue, cmpOptions...) {
				t.Errorf("shouldCreateNewResourceSnapshotNow() = %v, want %v", got, tc.wantRequeue)
			}
			if err := client.Get(ctx, types.NamespacedName{Name: snapshot.Name}, snapshot); err != nil {
				t.Fatalf("failed to get snapshot after shouldCreateNewResourceSnapshotNow: %v", err)
			}
			if gotAnnotation := len(snapshot.Annotations[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation]) != 0; tc.wantAnnoation != gotAnnotation {
				t.Errorf("shouldCreateNewResourceSnapshotNow() = annotation %v, want %v", snapshot.Annotations[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation], tc.wantAnnoation)
			}
		})
	}
}
