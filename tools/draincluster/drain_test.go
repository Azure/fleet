/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestFetchClusterResourcePlacementNamesToEvict(t *testing.T) {
	tests := []struct {
		name          string
		targetCluster string
		bindings      []placementv1beta1.ClusterResourceBinding
		wantErr       error
		wantMap       map[string]bool
	}{
		{
			name:          "successfully collected CRPs to evict",
			targetCluster: "test-cluster1",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb2",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb3",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp2",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster1",
					},
				},
			},
			wantErr: nil,
			wantMap: map[string]bool{
				"test-crp1": true,
				"test-crp2": true,
			},
		},
		{
			name:          "no CRPs to evict",
			targetCluster: "test-cluster1",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster2",
					},
				},
			},
			wantErr: nil,
			wantMap: map[string]bool{},
		},
		{
			name:          "binding missing CRP label",
			targetCluster: "test-cluster1",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb1",
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster1",
					},
				},
			},
			wantErr: fmt.Errorf("failed to get CRP name from binding test-crb1"),
			wantMap: map[string]bool{},
		},
		{
			name:          "skip CRB with deletionTimestamp",
			targetCluster: "test-cluster1",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Finalizers:        []string{"test-finalizer"},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster1",
					},
				},
			},
			wantErr: nil,
			wantMap: map[string]bool{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			scheme := serviceScheme(t)
			for i := range tc.bindings {
				objects = append(objects, &tc.bindings[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			h := helper{
				hubClient:   fakeClient,
				clusterName: tc.targetCluster,
			}
			gotMap, gotErr := h.fetchClusterResourcePlacementNamesToEvict(context.Background())
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("fetchClusterResourcePlacementNamesToEvict() test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("fetchClusterResourcePlacementNamesToEvict() test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(gotMap, tc.wantMap); diff != "" {
				t.Errorf("fetchClusterResourcePlacementNamesToEvict() test %s failed (-got +want):\n%s", tc.name, diff)
			}
		})
	}
}

func TestCollectClusterScopedResourcesSelectedByCRP(t *testing.T) {
	tests := []struct {
		name          string
		crpName       string
		crp           *placementv1beta1.ClusterResourcePlacement
		wantResources []placementv1beta1.ResourceIdentifier
		wantErr       bool
	}{
		{
			name:    "successfully collect cluster scoped resources",
			crpName: "test-crp",
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
						{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "test-cm",
							Namespace: "test-ns",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRoleBinding",
							Name:    "test-cluster-role-binding",
						},
					},
				},
			},
			wantResources: []placementv1beta1.ResourceIdentifier{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRoleBinding",
					Name:    "test-cluster-role-binding",
				},
			},
			wantErr: false,
		},
		{
			name:    "no cluster scoped resources",
			crpName: "test-crp",
			crp: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crp",
				},
				Status: placementv1beta1.ClusterResourcePlacementStatus{
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Group:     "",
							Version:   "v1",
							Kind:      "ConfigMap",
							Name:      "test-cm",
							Namespace: "test-ns",
						},
					},
				},
			},
			wantResources: nil,
			wantErr:       false,
		},
		{
			name:          "crp not found",
			crpName:       "test-crp",
			crp:           nil,
			wantResources: nil,
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			var objects []client.Object
			if tc.crp != nil {
				objects = append(objects, tc.crp)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			h := helper{
				hubClient: fakeClient,
			}

			gotResources, err := h.collectClusterScopedResourcesSelectedByCRP(context.Background(), tc.crpName)
			if tc.wantErr {
				if err == nil {
					t.Error("collectClusterScopedResourcesSelectedByCRP() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Errorf("collectClusterScopedResourcesSelectedByCRP() error = %v, want nil", err)
				return
			}

			if diff := cmp.Diff(gotResources, tc.wantResources); diff != "" {
				t.Errorf("collectClusterScopedResourcesSelectedByCRP() (-got +want):\n%s", diff)
			}
		})
	}
}

func TestGenerateResourceIdentifierKey(t *testing.T) {
	tests := []struct {
		name     string
		resource placementv1beta1.ResourceIdentifier
		want     string
	}{
		{
			name: "cluster scoped resource with empty group",
			resource: placementv1beta1.ResourceIdentifier{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
				Name:    "test-ns",
			},
			want: "''/v1/Namespace/''/test-ns",
		},
		{
			name: "cluster scoped resource with non-empty group",
			resource: placementv1beta1.ResourceIdentifier{
				Group:   "rbac.authorization.k8s.io",
				Version: "v1",
				Kind:    "ClusterRole",
				Name:    "test-role",
			},
			want: "rbac.authorization.k8s.io/v1/ClusterRole/''/test-role",
		},
		{
			name: "namespaced resource with empty group",
			resource: placementv1beta1.ResourceIdentifier{
				Group:     "",
				Version:   "v1",
				Kind:      "ConfigMap",
				Name:      "test-cm",
				Namespace: "test-ns",
			},
			want: "''/v1/ConfigMap/test-ns/test-cm",
		},
		{
			name: "namespaced resource with non-empty group",
			resource: placementv1beta1.ResourceIdentifier{
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Name:      "test-deploy",
				Namespace: "test-ns",
			},
			want: "apps/v1/Deployment/test-ns/test-deploy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := generateResourceIdentifierKey(tc.resource)
			if got != tc.want {
				t.Errorf("generateResourceIdentifierKey() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGenerateDrainEvictionName(t *testing.T) {
	tests := []struct {
		name       string
		wantPrefix string
		wantErr    error
	}{
		{
			name:       "valid name generation",
			wantPrefix: "drain-eviction-",
			wantErr:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotErr := generateDrainEvictionName()
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("generateDrainEvictionName() test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
				}
				if !strings.HasPrefix(gotName, tc.wantPrefix) {
					t.Errorf("generateDrainEvictionName() = %v, want prefix %v", gotName, tc.wantPrefix)
				}
				// Check that the generated name ends with an 8-character UUID
				if len(gotName) != len(tc.wantPrefix)+uuidLength {
					t.Errorf("generateDrainEvictionName() generated name length = %v, want length = %v", len(gotName), len(tc.wantPrefix)+uuidLength)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("generateDrainEvictionName() test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
			}
		})
	}
}

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add placement v1beta1 scheme: %v", err)
	}
	return scheme
}
