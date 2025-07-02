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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/queue"
)

func TestListBindingsFromKey(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		placementKey queue.PlacementKey
		objects      []client.Object
		wantErr      bool
		wantBindings []placementv1beta1.BindingObj
	}{
		{
			name:         "cluster-scoped placement key - no bindings found",
			placementKey: queue.PlacementKey("test-placement"),
			objects:      []client.Object{},
			wantErr:      false,
			wantBindings: []placementv1beta1.BindingObj{},
		},
		{
			name:         "cluster-scoped placement key - single binding found",
			placementKey: queue.PlacementKey("test-placement"),
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name:         "cluster-scoped placement key - multiple bindings found",
			placementKey: queue.PlacementKey("test-placement"),
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
		},
		{
			name:         "cluster-scoped placement key - excludes non-matching bindings",
			placementKey: queue.PlacementKey("test-placement"),
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-binding",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "other-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name:         "namespaced placement key - single binding found",
			placementKey: queue.PlacementKey("test-namespace/test-placement"),
			objects: []client.Object{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name:         "namespaced placement key - excludes wrong namespace",
			placementKey: queue.PlacementKey("test-namespace/test-placement"),
			objects: []client.Object{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-binding",
						Namespace: "other-namespace",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name:         "invalid placement key format - too many separators",
			placementKey: queue.PlacementKey("namespace/placement/extra"),
			objects:      []client.Object{},
			wantErr:      true,
			wantBindings: nil,
		},
		{
			name:         "invalid placement key format - empty parts",
			placementKey: queue.PlacementKey("namespace/"),
			objects:      []client.Object{},
			wantErr:      true,
			wantBindings: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = placementv1beta1.AddToScheme(scheme)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			got, err := ListBindingsFromKey(ctx, fakeClient, tt.placementKey)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if !errors.Is(err, ErrUnexpectedBehavior) {
					t.Errorf("Expected ErrUnexpectedBehavior but got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			// Use cmp.Diff to compare the actual result with expected bindings
			// Ignore resource version field and sort by name for consistent comparison
			if diff := cmp.Diff(got, tt.wantBindings,
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmpopts.SortSlices(func(b1, b2 placementv1beta1.BindingObj) bool {
					return b1.GetName() < b2.GetName()
				})); diff != "" {
				t.Errorf("ListBindingsFromKey() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestListBindingsFromKey_ClientError(t *testing.T) {
	ctx := context.Background()

	// Create a client that will return an error
	scheme := runtime.NewScheme()
	_ = placementv1beta1.AddToScheme(scheme)

	// Use a fake client but override List to return error
	fakeClient := &failingListClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}

	_, err := ListBindingsFromKey(ctx, fakeClient, queue.PlacementKey("test-placement"))

	if err == nil {
		t.Fatalf("Expected error but got nil")
	}

	if !errors.Is(err, ErrAPIServerError) {
		t.Errorf("Expected ErrAPIServerError but got: %v", err)
	}
}

// failingListClient is a test helper that wraps a client and makes List calls fail
type failingListClient struct {
	client.Client
}

func (c *failingListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return fmt.Errorf("simulated client error")
}
