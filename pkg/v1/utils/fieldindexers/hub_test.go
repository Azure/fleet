/*
Copyright 2026 The KubeFleet Authors.

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

package fieldindexers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// TestClusterClaimOwnedByPlacementPolicyFieldExtractor pins that cluster claims are indexed by every
// identifying part of the placement policy reference except its version.
func TestClusterClaimOwnedByPlacementPolicyFieldExtractor(t *testing.T) {
	testCases := []struct {
		name    string
		obj     client.Object
		want    []string
		wantErr bool
	}{
		{
			name: "namespaced policy",
			obj: &placementv1alpha1.ClusterClaim{
				Spec: placementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: &placementv1alpha1.ObjectReference{
					APIGroup: "placement.kubefleet.dev", APIVersion: "v1alpha1", Kind: "PlacementPolicy", Namespace: "work", Name: "app",
				}},
			},
			want: []string{"placement.kubefleet.dev/PlacementPolicy/work/app"},
		},
		{
			name: "cluster-scoped policy",
			obj: &placementv1alpha1.ClusterClaim{
				Spec: placementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: &placementv1alpha1.ObjectReference{
					APIGroup: "placement.kubefleet.dev", APIVersion: "v1beta1", Kind: "ClusterPlacementPolicy", Name: "app",
				}},
			},
			want: []string{"placement.kubefleet.dev/ClusterPlacementPolicy//app"},
		},
		{
			name:    "no placement policy reference",
			obj:     &placementv1alpha1.ClusterClaim{ObjectMeta: metav1.ObjectMeta{Name: "orphan"}},
			wantErr: true,
		},
		{
			name:    "not a cluster claim",
			obj:     &placementv1alpha1.Work{},
			wantErr: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clusterClaimOwnedByPlacementPolicyFieldExtractor(tc.obj)
			if (err != nil) != tc.wantErr {
				t.Fatalf("clusterClaimOwnedByPlacementPolicyFieldExtractor() = %v, want error %t", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("clusterClaimOwnedByPlacementPolicyFieldExtractor() mismatch (-want +got):\n%s", diff)
			}
			// A lookup formats the policy's reference the same way the index does.
			if claim, ok := tc.obj.(*placementv1alpha1.ClusterClaim); ok && !tc.wantErr {
				if val := ClusterClaimOwnedByPlacementPolicyCustomFieldVal(claim.Spec.PlacementPolicyRef); val != got[0] {
					t.Errorf("ClusterClaimOwnedByPlacementPolicyCustomFieldVal() = %q, want the indexed value %q", val, got[0])
				}
			}
		})
	}
}

// TestPlacementBindingOwnedByFieldExtractor pins that both binding kinds are indexed
// by the name of their placement policy.
func TestPlacementBindingOwnedByFieldExtractor(t *testing.T) {
	testCases := []struct {
		name    string
		obj     client.Object
		want    []string
		wantErr bool
	}{
		{
			name: "placement binding",
			obj:  &placementv1alpha1.PlacementBinding{Spec: placementv1alpha1.PlacementBindingSpec{PlacementPolicyName: "app"}},
			want: []string{"app"},
		},
		{
			name: "cluster placement binding",
			obj:  &placementv1alpha1.ClusterPlacementBinding{Spec: placementv1alpha1.PlacementBindingSpec{PlacementPolicyName: "app"}},
			want: []string{"app"},
		},
		{
			name:    "no placement policy name",
			obj:     &placementv1alpha1.PlacementBinding{},
			wantErr: true,
		},
		{
			name:    "not a placement binding",
			obj:     &placementv1alpha1.Work{},
			wantErr: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := placementBindingOwnedByFieldExtractor(tc.obj)
			if (err != nil) != tc.wantErr {
				t.Fatalf("placementBindingOwnedByFieldExtractor() = %v, want error %t", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("placementBindingOwnedByFieldExtractor() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
