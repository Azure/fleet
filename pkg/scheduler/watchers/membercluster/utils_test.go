/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	crpName      = "test-crp"
	clusterName1 = "bravelion"
	clusterName2 = "jumpingcat"
	crpName1     = "crp-1"
	crpName2     = "crp-2"
	crpName3     = "crp-3"
	crpName4     = "crp-4"
	crpName5     = "crp-5"
	crpName6     = "crp-6"
	crpName7     = "crp-7"
)

var (
	numOfClusters = int32(10)
)

// TestIsCRPFullyScheduled tests the isCRPFullyScheduled function.
func TestIsPickNCRPFullyScheduled(t *testing.T) {
	testCases := []struct {
		name string
		crp  *fleetv1beta1.ClusterResourcePlacement
		want bool
	}{
		{
			name: "no scheduled condition",
			crp: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: fleetv1beta1.ClusterResourcePlacementStatus{
					Conditions: []metav1.Condition{},
				},
			},
		},
		{
			name: "scheduled condition is false",
			crp: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: fleetv1beta1.ClusterResourcePlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
		{
			name: "scheduled condition is true, observed generation is out of date",
			crp: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: fleetv1beta1.ClusterResourcePlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
						},
					},
				},
			},
		},
		{
			name: "fully scheduled",
			crp: &fleetv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crpName,
					Generation: 1,
				},
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
				},
				Status: fleetv1beta1.ClusterResourcePlacementStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
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
			scheduled := isCRPFullyScheduled(tc.crp)
			if scheduled != tc.want {
				t.Errorf("isPickNCRPFullyScheduled() = %v, want %v", scheduled, tc.want)
			}
		})
	}
}

// TestClassifyCRPs tests the classifyCRPs function.
func TestClassifyCRPs(t *testing.T) {
	testCases := []struct {
		name string
		crps []fleetv1beta1.ClusterResourcePlacement
		want []fleetv1beta1.ClusterResourcePlacement
	}{
		{
			name: "single crp, no policy",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
				},
			},
		},
		{
			name: "single crp, fixed list of clusters, not fully scheduled",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
			},
		},
		{
			name: "single crp, fixed list of clusters, fully scheduled",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Generation: 1,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
					Status: fleetv1beta1.ClusterResourcePlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{},
		},
		{
			name: "single crp, pick all placement type",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType: fleetv1beta1.PickAllPlacementType,
						},
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType: fleetv1beta1.PickAllPlacementType,
						},
					},
				},
			},
		},
		{
			name: "single crp, pick N placement type, not fully scheduled",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType:    fleetv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType:    fleetv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
		},
		{
			name: "single crp, pick N placement type, fully scheduled",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName,
						Generation: 1,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType:    fleetv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
					Status: fleetv1beta1.ClusterResourcePlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{},
		},
		{
			name: "mixed",
			crps: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       crpName5,
						Generation: 1,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType:    fleetv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
					Status: fleetv1beta1.ClusterResourcePlacementStatus{
						Conditions: []metav1.Condition{
							{
								Type:               string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType),
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName4,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType: fleetv1beta1.PickAllPlacementType,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType:    fleetv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
			want: []fleetv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName2,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							ClusterNames: []string{clusterName1},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName4,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType: fleetv1beta1.PickAllPlacementType,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName3,
					},
					Spec: fleetv1beta1.ClusterResourcePlacementSpec{
						Policy: &fleetv1beta1.PlacementPolicy{
							PlacementType:    fleetv1beta1.PickNPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toProcess := classifyCRPs(tc.crps)
			if diff := cmp.Diff(toProcess, tc.want); diff != "" {
				t.Errorf("classifyCRPs() toProcess (-got, +want): %s", diff)
			}
		})
	}
}
