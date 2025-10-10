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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestFetchUpdateRunFromRequest(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		request ctrl.Request
		objects []client.Object
		wantErr bool
		wantRun placementv1beta1.UpdateRunObj
	}{
		{
			name: "cluster-scoped request - ClusterStagedUpdateRun found",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-run"},
			},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-run",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
			},
			wantErr: false,
			wantRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-run",
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "snapshot-1",
					StagedUpdateStrategyName: "test-strategy",
				},
			},
		},
		{
			name: "cluster-scoped request - ClusterStagedUpdateRun not found",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-run"},
			},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-run",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-run",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
			},
			wantErr: true,
			wantRun: nil,
		},
		{
			name: "namespaced request - StagedUpdateRun found",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-run"},
			},
			objects: []client.Object{
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-run",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-run",
						Namespace: "other-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "other-placement",
						ResourceSnapshotIndex:    "snapshot-2",
						StagedUpdateStrategyName: "other-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-run",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
			},
			wantErr: false,
			wantRun: &placementv1beta1.StagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-run",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "snapshot-1",
					StagedUpdateStrategyName: "test-strategy",
				},
			},
		},
		{
			name: "namespaced request - StagedUpdateRun not found",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-run"},
			},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-run",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-run",
						Namespace: "other-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-run",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "test-placement",
						ResourceSnapshotIndex:    "snapshot-1",
						StagedUpdateStrategyName: "test-strategy",
					},
				},
			},
			wantErr: true,
			wantRun: nil,
		},
		{
			name: "mixed setup - fetch cluster-scoped run",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-run"},
			},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-run",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "cluster-placement",
						ResourceSnapshotIndex:    "cluster-snapshot-1",
						StagedUpdateStrategyName: "cluster-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-run",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "namespaced-placement",
						ResourceSnapshotIndex:    "namespaced-snapshot-1",
						StagedUpdateStrategyName: "namespaced-strategy",
					},
				},
			},
			wantErr: false,
			wantRun: &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-run",
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "cluster-placement",
					ResourceSnapshotIndex:    "cluster-snapshot-1",
					StagedUpdateStrategyName: "cluster-strategy",
				},
			},
		},
		{
			name: "mixed setup - fetch namespaced run",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-run"},
			},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-run",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "cluster-placement",
						ResourceSnapshotIndex:    "cluster-snapshot-1",
						StagedUpdateStrategyName: "cluster-strategy",
					},
				},
				&placementv1beta1.StagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-run",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            "namespaced-placement",
						ResourceSnapshotIndex:    "namespaced-snapshot-1",
						StagedUpdateStrategyName: "namespaced-strategy",
					},
				},
			},
			wantErr: false,
			wantRun: &placementv1beta1.StagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-run",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "namespaced-placement",
					ResourceSnapshotIndex:    "namespaced-snapshot-1",
					StagedUpdateStrategyName: "namespaced-strategy",
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

			got, err := FetchUpdateRunFromRequest(ctx, fakeClient, tt.request)

			if (err != nil) != tt.wantErr {
				t.Fatalf("FetchUpdateRunFromRequest() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.wantRun, got, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("FetchUpdateRunFromRequest() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFetchUpdateRunFromRequest_ClientError(t *testing.T) {
	ctx := context.Background()

	// Create a client that will return an error.
	scheme := runtime.NewScheme()
	_ = placementv1beta1.AddToScheme(scheme)

	testError := errors.New("test client error")

	// Use interceptor to make Get calls fail.
	fakeClient := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return testError
			},
		},
	)

	tests := []struct {
		name    string
		request ctrl.Request
	}{
		{
			name: "cluster-scoped request with client error",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-run"},
			},
		},
		{
			name: "namespaced request with client error",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-run"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FetchUpdateRunFromRequest(ctx, fakeClient, tt.request)

			if err == nil {
				t.Fatalf("FetchUpdateRunFromRequest() want error but got nil")
			}

			if !errors.Is(err, testError) {
				t.Fatalf("FetchUpdateRunFromRequest() want testError but got %v", err)
			}
		})
	}
}
