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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func TestFetchUpdateStrategyFromNamespacedName(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		strategyKey  types.NamespacedName
		objects      []client.Object
		wantErr      bool
		wantStrategy placementv1beta1.UpdateStrategyObj
	}{
		{
			name:        "cluster-scoped strategy key - ClusterStagedUpdateStrategy found",
			strategyKey: types.NamespacedName{Name: "test-strategy"},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-strategy",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-1",
							},
						},
					},
				},
			},
			wantErr: false,
			wantStrategy: &placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-strategy",
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: "stage-1",
						},
					},
				},
			},
		},
		{
			name:        "cluster-scoped strategy key - ClusterStagedUpdateStrategy not found",
			strategyKey: types.NamespacedName{Name: "test-strategy"},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-strategy",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-1",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-strategy",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-1",
							},
						},
					},
				},
			},
			wantErr:      true,
			wantStrategy: nil,
		},
		{
			name:        "namespaced strategy key - StagedUpdateStrategy found",
			strategyKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-strategy"},
			objects: []client.Object{
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-strategy",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-1",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-strategy",
						Namespace: "other-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-2",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-strategy",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-3",
							},
						},
					},
				},
			},
			wantErr: false,
			wantStrategy: &placementv1beta1.StagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-strategy",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: "stage-1",
						},
					},
				},
			},
		},
		{
			name:        "namespaced strategy key - StagedUpdateStrategy not found",
			strategyKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-strategy"},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-strategy",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-1",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-strategy",
						Namespace: "other-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-1",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-strategy",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "stage-2",
							},
						},
					},
				},
			},
			wantErr:      true,
			wantStrategy: nil,
		},
		{
			name:        "mixed setup - fetch cluster-scoped strategy",
			strategyKey: types.NamespacedName{Name: "test-strategy"},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-strategy",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "cluster-stage-1",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-strategy",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "namespaced-stage-1",
							},
						},
					},
				},
			},
			wantErr: false,
			wantStrategy: &placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-strategy",
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: "cluster-stage-1",
						},
					},
				},
			},
		},
		{
			name:        "mixed setup - fetch namespaced strategy",
			strategyKey: types.NamespacedName{Namespace: "test-namespace", Name: "test-strategy"},
			objects: []client.Object{
				&placementv1beta1.ClusterStagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-strategy",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "cluster-stage-1",
							},
						},
					},
				},
				&placementv1beta1.StagedUpdateStrategy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-strategy",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.UpdateStrategySpec{
						Stages: []placementv1beta1.StageConfig{
							{
								Name: "namespaced-stage-1",
							},
						},
					},
				},
			},
			wantErr: false,
			wantStrategy: &placementv1beta1.StagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-strategy",
					Namespace: "test-namespace",
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: "namespaced-stage-1",
						},
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

			got, err := FetchUpdateStrategyFromNamespacedName(ctx, fakeClient, tt.strategyKey)

			if (err != nil) != tt.wantErr {
				t.Fatalf("FetchUpdateStrategyFromNamespacedName() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.wantStrategy, got, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("FetchUpdateStrategyFromNamespacedName() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFetchUpdateStrategyFromNamespacedName_ClientError(t *testing.T) {
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

	_, err := FetchUpdateStrategyFromNamespacedName(ctx, fakeClient, types.NamespacedName{Name: "test-strategy"})

	if err == nil {
		t.Fatalf("FetchUpdateStrategyFromNamespacedName() expected error but got nil")
	}

	if !errors.Is(err, testError) {
		t.Fatalf("FetchUpdateStrategyFromNamespacedName() expected testError but got %v", err)
	}
}
