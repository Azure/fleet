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
	"maps"
	"slices"
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
