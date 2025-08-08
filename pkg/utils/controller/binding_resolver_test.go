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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestListBindingsFromKey(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		placementKey types.NamespacedName
		objects      []client.Object
		wantErr      bool
		wantBindings []placementv1beta1.BindingObj
	}{
		{
			name:         "cluster-scoped placement key - no bindings found",
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects:      []client.Object{},
			wantErr:      false,
			wantBindings: []placementv1beta1.BindingObj{},
		},
		{
			name:         "cluster-scoped placement key - single binding found",
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "other-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
			placementKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
			placementKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name:         "mixed setup - list cluster-scoped bindings only",
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-binding-2",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "namespaced-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-3",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-binding-2",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
		},
		{
			name:         "mixed setup - list namespaced bindings only",
			placementKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "namespaced-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "namespaced-binding-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-3",
					},
				},
			},
			wantErr: false,
			wantBindings: []placementv1beta1.BindingObj{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "namespaced-binding-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "namespaced-binding-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-3",
					},
				},
			},
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

	_, err := ListBindingsFromKey(ctx, fakeClient, types.NamespacedName{Name: "test-placement"})

	if err == nil {
		t.Fatalf("Expected error but got nil")
	}

	if !errors.Is(err, ErrAPIServerError) {
		t.Errorf("Expected ErrAPIServerError but got: %v", err)
	}
}

func TestFetchBindingFromKey(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		placementKey types.NamespacedName
		objects      []client.Object
		wantErr      bool
		wantBinding  placementv1beta1.BindingObj
	}{
		{
			name:         "cluster-scoped placement key - ClusterResourceBinding found",
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantErr: false,
			wantBinding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-placement",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					TargetCluster: "cluster-1",
				},
			},
		},
		{
			name:         "cluster-scoped placement key - ClusterResourceBinding not found",
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: "test-placement",
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
							placementv1beta1.PlacementTrackingLabel: "other-placement",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantErr:     true,
			wantBinding: nil,
		},
		{
			name:         "namespaced placement key - ResourceBinding found",
			placementKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "other-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantErr: false,
			wantBinding: &placementv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					TargetCluster: "cluster-1",
				},
			},
		},
		{
			name:         "namespaced placement key - ResourceBinding not found",
			placementKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "other-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-placement",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			wantErr:     true,
			wantBinding: nil,
		},
		{
			name:         "mixed setup - fetch cluster-scoped binding",
			placementKey: types.NamespacedName{Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantErr: false,
			wantBinding: &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-placement",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					TargetCluster: "cluster-1",
				},
			},
		},
		{
			name:         "mixed setup - fetch namespaced binding",
			placementKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-placement"},
			objects: []client.Object{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-placement",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-placement",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			wantErr: false,
			wantBinding: &placementv1beta1.ResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-placement",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					TargetCluster: "cluster-2",
				},
			},
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

			got, err := FetchBindingFromKey(ctx, fakeClient, tt.placementKey)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error but got: %v", err)
			}

			// Use cmp.Diff to compare the actual result with expected binding
			if diff := cmp.Diff(got, tt.wantBinding,
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("FetchBindingFromKey() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestConvertCRBObjsToBindingObjs(t *testing.T) {
	tests := []struct {
		name     string
		bindings []placementv1beta1.ClusterResourceBinding
		want     []placementv1beta1.BindingObj
	}{
		{
			name:     "empty slice",
			bindings: []placementv1beta1.ClusterResourceBinding{},
			want:     nil,
		},
		{
			name:     "nil slice",
			bindings: nil,
			want:     nil,
		},
		{
			name: "single binding",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			want: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name: "multiple bindings",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			want: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertCRBObjsToBindingObjs(tt.bindings)

			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ConvertCRBObjsToBindingObjs() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestConvertCRBArrayToBindingObjs(t *testing.T) {
	tests := []struct {
		name     string
		bindings []*placementv1beta1.ClusterResourceBinding
		want     []placementv1beta1.BindingObj
	}{
		{
			name:     "empty slice",
			bindings: []*placementv1beta1.ClusterResourceBinding{},
			want:     nil,
		},
		{
			name:     "nil slice",
			bindings: nil,
			want:     nil,
		},
		{
			name: "single binding",
			bindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			want: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name: "multiple bindings",
			bindings: []*placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			want: []placementv1beta1.BindingObj{
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertCRBArrayToBindingObjs(tt.bindings)

			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ConvertCRBArrayToBindingObjs() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestConvertCRB2DArrayToBindingObjs(t *testing.T) {
	tests := []struct {
		name        string
		bindingSets [][]*placementv1beta1.ClusterResourceBinding
		want        [][]placementv1beta1.BindingObj
	}{
		{
			name:        "empty slice",
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{},
			want:        nil,
		},
		{
			name:        "nil slice",
			bindingSets: nil,
			want:        nil,
		},
		{
			name: "single set with single binding",
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-1",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-1",
						},
					},
				},
			},
			want: [][]placementv1beta1.BindingObj{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-1",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-1",
						},
					},
				},
			},
		},
		{
			name: "multiple sets with multiple bindings",
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-1",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-2",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-2",
						},
					},
				},
				{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-3",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-3",
						},
					},
				},
			},
			want: [][]placementv1beta1.BindingObj{
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-1",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-1",
						},
					},
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-2",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-2",
						},
					},
				},
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-3",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-3",
						},
					},
				},
			},
		},
		{
			name: "sets with empty bindings",
			bindingSets: [][]*placementv1beta1.ClusterResourceBinding{
				{},
				{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-1",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-1",
						},
					},
				},
			},
			want: [][]placementv1beta1.BindingObj{
				nil,
				{
					&placementv1beta1.ClusterResourceBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-binding-1",
						},
						Spec: placementv1beta1.ResourceBindingSpec{
							TargetCluster: "cluster-1",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertCRB2DArrayToBindingObjs(tt.bindingSets)

			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ConvertCRB2DArrayToBindingObjs() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestConvertRBArrayToBindingObjs(t *testing.T) {
	tests := []struct {
		name     string
		bindings []*placementv1beta1.ResourceBinding
		want     []placementv1beta1.BindingObj
	}{
		{
			name:     "empty slice",
			bindings: []*placementv1beta1.ResourceBinding{},
			want:     nil,
		},
		{
			name:     "nil slice",
			bindings: nil,
			want:     nil,
		},
		{
			name: "single binding",
			bindings: []*placementv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
			want: []placementv1beta1.BindingObj{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
			},
		},
		{
			name: "multiple bindings",
			bindings: []*placementv1beta1.ResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
			want: []placementv1beta1.BindingObj{
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-1",
					},
				},
				&placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-binding-2",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "cluster-2",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertRBArrayToBindingObjs(tt.bindings)

			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("ConvertRBArrayToBindingObjs() diff (-got +want):\n%s", diff)
			}
		})
	}
}

// failingListClient is a test helper that wraps a client and makes List calls fail
type failingListClient struct {
	client.Client
}

func (c *failingListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return fmt.Errorf("simulated client error")
}
