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

package membercluster

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	crpName       = "test-crp"
	rpName        = "test-rp"
	clusterName1  = "bravelion"
	clusterName2  = "jumpingcat"
	crpName1      = "crp-1"
	crpName2      = "crp-2"
	crpName3      = "crp-3"
	crpName4      = "crp-4"
	crpName5      = "crp-5"
	crpName6      = "crp-6"
	rpName1       = "rp-1"
	rpName2       = "rp-2"
	rpName3       = "rp-3"
	rpName4       = "rp-4"
	rpName5       = "rp-5"
	rpName6       = "rp-6"
	testNamespace = "test-namespace"
)

var (
	numOfClusters = int32(10)
)

// TestIsPlacementFullyScheduled tests the isPlacementFullyScheduled function.
func TestIsPlacementFullyScheduled(t *testing.T) {
	testCases := []struct {
		name      string
		placement placementv1beta1.PlacementObj
		want      bool
	}{
		{
			name: "no scheduled condition in CRP",
			placement: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{},
				},
			},
		},
		{
			name: "scheduled condition is false in CRP",
			placement: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
		{
			name: "scheduled condition is true, observed generation is out of date in CRP",
			placement: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Generation: 1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
						},
					},
				},
			},
		},
		{
			name: "resourcePlacementScheduled condition is true in CRP (should not happen)",
			placement: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Generation: 1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
		},
		{
			name: "fully scheduled CRP",
			placement: &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Generation: 1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "no scheduled condition in RP",
			placement: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{},
				},
			},
		},
		{
			name: "scheduled condition is false in RP",
			placement: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(placementv1beta1.ResourcePlacementScheduledConditionType),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
		{
			name: "scheduled condition is true, observed generation is out of date in RP",
			placement: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  testNamespace,
					Generation: 1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
						},
					},
				},
			},
		},
		{
			name: "clusterResourcePlacementScheduled condition is true in RP (should not happen)",
			placement: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  testNamespace,
					Generation: 1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
		},
		{
			name: "fully scheduled RP",
			placement: &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       rpName,
					Namespace:  testNamespace,
					Generation: 1,
				},
				Spec: placementv1beta1.PlacementSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: placementv1beta1.PlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheduled := isPlacementFullyScheduled(tc.placement)
			if scheduled != tc.want {
				t.Errorf("isPlacementFullyScheduled() = %v, want %v", scheduled, tc.want)
			}
		})
	}
}

// TestClassifyPlacements tests the classifyPlacements function.
func TestClassifyPlacements(t *testing.T) {
	testCases := []struct {
		name       string
		placements []placementv1beta1.PlacementObj
		want       []placementv1beta1.PlacementObj
	}{
		{
			name: "single crp, no policy",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
				},
			},
		},
		{
			name: "single crp, fixed list of clusters, not fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
			},
		},
		{
			name: "single crp, fixed list of clusters, fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{},
		},
		{
			name: "single crp, pick all placement type",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
		},
		{
			name: "single crp, pick N placement type, not fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
		},
		{
			name: "single crp, pick N placement type, fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{},
		},
		{
			name: "mixed crps",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName5,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName4,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName4,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
		},
		{
			name: "single rp, no policy",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: testNamespace,
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: testNamespace,
					},
				},
			},
		},
		{
			name: "single rp, fixed list of clusters, not fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
			},
		},
		{
			name: "single rp, fixed list of clusters, fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       rpName,
						Namespace:  testNamespace,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{},
		},
		{
			name: "single rp, pick all placement type, fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       rpName,
						Namespace:  testNamespace,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       rpName,
						Namespace:  testNamespace,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
		},
		{
			name: "single rp, pick N placement type, not fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
		},
		{
			name: "single rp, pick N placement type, fully scheduled",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       rpName,
						Namespace:  testNamespace,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{},
		},
		{
			name: "mixed rps",
			placements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName2,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName1,
						Namespace: testNamespace,
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:       rpName5,
						Namespace:  testNamespace,
						Generation: 1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
					Status: placementv1beta1.PlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(placementv1beta1.ResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName4,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName3,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
			want: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName2,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName1,
						Namespace: testNamespace,
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName4,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rpName3,
						Namespace: testNamespace,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toProcess := classifyPlacements(tc.placements)
			if diff := cmp.Diff(toProcess, tc.want); diff != "" {
				t.Errorf("classifyPlacements() toProcess (-got, +want): %s", diff)
			}
		})
	}
}

// TestConvertCRPArrayToPlacementObjs tests the convertCRPArrayToPlacementObjs function.
func TestConvertCRPArrayToPlacementObjs(t *testing.T) {
	testCases := []struct {
		name           string
		crps           []placementv1beta1.ClusterResourcePlacement
		wantPlacements []placementv1beta1.PlacementObj
	}{
		{
			name:           "empty array",
			crps:           []placementv1beta1.ClusterResourcePlacement{},
			wantPlacements: []placementv1beta1.PlacementObj{},
		},
		{
			name: "single crp",
			crps: []placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
			wantPlacements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
		},
		{
			name: "multiple crps",
			crps: []placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1, clusterName2},
						},
					},
				},
			},
			wantPlacements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
				&placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1, clusterName2},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			placements := convertCRPArrayToPlacementObjs(tc.crps)

			if diff := cmp.Diff(placements, tc.wantPlacements); diff != "" {
				t.Errorf("ConvertCRPArrayToPlacementObjs() diff (-got +want):\n%s", diff)
			}
		})
	}
}

// TestConvertRPArrayToPlacementObjs tests the convertRPArrayToPlacementObjs function.
func TestConvertRPArrayToPlacementObjs(t *testing.T) {
	testCases := []struct {
		name           string
		rps            []placementv1beta1.ResourcePlacement
		wantPlacements []placementv1beta1.PlacementObj
	}{
		{
			name:           "empty array",
			rps:            []placementv1beta1.ResourcePlacement{},
			wantPlacements: []placementv1beta1.PlacementObj{},
		},
		{
			name: "single rp",
			rps: []placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
			wantPlacements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
			},
		},
		{
			name: "multiple rps",
			rps: []placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1, clusterName2},
						},
					},
				},
			},
			wantPlacements: []placementv1beta1.PlacementObj{
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
				&placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: placementv1beta1.PlacementSpec{
						Policy: &placementv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1, clusterName2},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			placements := convertRPArrayToPlacementObjs(tc.rps)

			if diff := cmp.Diff(placements, tc.wantPlacements); diff != "" {
				t.Errorf("ConvertRPArrayToPlacementObjs() diff (-got +want):\n%s", diff)
			}
		})
	}
}
