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

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	toolsutils "go.goms.io/fleet/tools/utils"
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
			wantErr: errors.New("failed to get CRP name from binding test-crb1"),
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
					t.Errorf("fetchClusterResourcePlacementNamesToEvict test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
				}
				if diff := cmp.Diff(gotMap, tc.wantMap); diff != "" {
					t.Errorf("fetchClusterResourcePlacementNamesToEvict test %s failed (-got +want):\n%s", tc.name, diff)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("fetchClusterResourcePlacementNamesToEvict test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
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
		wantErr       error
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
			wantErr: nil,
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
			wantErr:       nil,
		},
		{
			name:          "crp not found",
			crpName:       "test-crp",
			crp:           nil,
			wantResources: nil,
			wantErr:       errors.New("failed to get ClusterResourcePlacement test-crp: clusterresourceplacements.placement.kubernetes-fleet.io \"test-crp\" not found"),
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

			gotResources, gotErr := h.collectClusterScopedResourcesSelectedByCRP(context.Background(), tc.crpName)
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("collectClusterScopedResourcesSelectedByCRP test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
				}
				if diff := cmp.Diff(gotResources, tc.wantResources); diff != "" {
					t.Errorf("collectClusterScopedResourcesSelectedByCRP (-got +want):\n%s", diff)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("collectClusterScopedResourcesSelectedByCRP test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
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
				t.Errorf("generateResourceIdentifierKey = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGenerateDrainEvictionName(t *testing.T) {
	tests := []struct {
		name          string
		crpName       string
		targetCluster string
		wantErr       error
	}{
		{
			name:          "valid names",
			crpName:       "test-crp",
			targetCluster: "test-cluster",
			wantErr:       nil,
		},
		{
			name:          "name has invalid characters",
			crpName:       "test-crp",
			targetCluster: "test_cluster$",
			wantErr:       errors.New("failed to format a qualified name for drain eviction object"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evictionName, gotErr := generateDrainEvictionName(tc.crpName, tc.targetCluster)
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("generateDrainEvictionName() got error %v, want error %v", gotErr, tc.wantErr)
				}
				// Verify the generated name follows the expected format
				prefix := fmt.Sprintf("drain-eviction-%s-%s-", tc.crpName, tc.targetCluster)
				if !strings.HasPrefix(evictionName, prefix) {
					t.Errorf("generateDrainEvictionName() = %v, want prefix %v", evictionName, prefix)
				}
				if len(evictionName) != len(prefix)+uuidLength {
					t.Errorf("generateDrainEvictionName() generated name length = %v, want %v", len(evictionName), len(prefix)+uuidLength)
				}
			} else if gotErr == nil || !strings.Contains(gotErr.Error(), tc.wantErr.Error()) {
				t.Errorf("generateDrainEvictionName() got error %v, want error %v", gotErr, tc.wantErr)
			}
		})
	}
}

func TestCordon(t *testing.T) {
	tests := []struct {
		name          string
		memberCluster *clusterv1beta1.MemberCluster
		wantTaints    []clusterv1beta1.Taint
		wantErr       error
	}{
		{
			name: "successfully add cordon taint, no other taints present",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{},
				},
			},
			wantTaints: []clusterv1beta1.Taint{toolsutils.CordonTaint},
			wantErr:    nil,
		},
		{
			name: "cordon taint already exists",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{toolsutils.CordonTaint},
				},
			},
			wantTaints: []clusterv1beta1.Taint{toolsutils.CordonTaint},
			wantErr:    nil,
		},
		{
			name: "successfully add cordon taint, cluster has other taints",
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "other-key",
							Value:  "other-value",
							Effect: "NoSchedule",
						},
					},
				},
			},
			wantTaints: []clusterv1beta1.Taint{
				{
					Key:    "other-key",
					Value:  "other-value",
					Effect: "NoSchedule",
				},
				toolsutils.CordonTaint,
			},
			wantErr: nil,
		},
		{
			name:          "member cluster not found",
			memberCluster: nil,
			wantTaints:    nil,
			wantErr:       errors.New("memberclusters.cluster.kubernetes-fleet.io \"test-cluster\" not found"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			if err := clusterv1beta1.AddToScheme(scheme); err != nil {
				t.Fatalf("failed to add cluster v1beta1 scheme: %v", err)
			}

			var objects []client.Object
			if tc.memberCluster != nil {
				objects = append(objects, tc.memberCluster)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			h := helper{
				hubClient:   fakeClient,
				clusterName: "test-cluster",
			}

			gotErr := h.cordon(context.Background())
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("cordon test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
				}
				var updatedCluster clusterv1beta1.MemberCluster
				if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "test-cluster"}, &updatedCluster); err != nil {
					t.Errorf("failed to get updated cluster: %v", err)
				}
				if diff := cmp.Diff(updatedCluster.Spec.Taints, tc.wantTaints); diff != "" {
					t.Errorf("cordon taints mismatch (-got +want):\n%s", diff)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("cordon test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
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
