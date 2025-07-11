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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	require.NoError(t, fleetv1beta1.AddToScheme(scheme))

	tests := []struct {
		name             string
		placementKey     string
		objects          []client.Object
		expectedResult   fleetv1beta1.ResourceSnapshotObj
		expectedError    string
		setupClientError bool
	}{
		{
			name:         "successfully fetch master resource snapshot - namespaced",
			placementKey: "test-namespace/test-crp",
			objects: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:      "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
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
							fleetv1beta1.CRPTrackingLabel:      "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
				},
			},
			expectedResult: &fleetv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-snapshot-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel:      "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "hash123",
					},
				},
			},
		},
		{
			name:         "successfully fetch master resource snapshot - cluster-scoped",
			placementKey: "test-crp",
			objects: []client.Object{
				&fleetv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cluster-snapshot-1",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:      "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
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
						fleetv1beta1.CRPTrackingLabel:      "test-crp",
						fleetv1beta1.IsLatestSnapshotLabel: "true",
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation: "hash456",
					},
				},
			},
		},
		{
			name:         "no resource snapshots found",
			placementKey: "test-namespace/test-crp",
			objects:      []client.Object{},
		},
		{
			name:         "no master resource snapshot found",
			placementKey: "test-namespace/test-crp",
			objects: []client.Object{
				&fleetv1beta1.ResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-snapshot-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							fleetv1beta1.CRPTrackingLabel:      "test-crp",
							fleetv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
				},
			},
			expectedError: "no masterResourceSnapshot found for the placement test-namespace/test-crp",
		},
		{
			name:          "invalid placement key",
			placementKey:  "invalid//key",
			objects:       []client.Object{},
			expectedError: "invalid placement key format",
		},
		{
			name:             "client list error",
			placementKey:     "test-namespace/test-crp",
			objects:          []client.Object{},
			setupClientError: true,
			expectedError:    "failed to list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var k8Client client.Client
			if tt.setupClientError {
				k8Client = &errorClient{fake.NewClientBuilder().WithScheme(scheme).Build()}
			} else {
				k8Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			}

			result, err := FetchLatestMasterResourceSnapshot(context.Background(), k8Client, tt.placementKey)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			if tt.expectedResult == nil {
				assert.Nil(t, result)
				return
			}

			// Use cmp.Diff for comparison to provide better error messages
			if diff := cmp.Diff(tt.expectedResult, result, resourceSnapshotCmpOptions...); diff != "" {
				t.Errorf("FetchMasterResourceSnapshot() mismatch (-want +got):\n%s", diff)
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
