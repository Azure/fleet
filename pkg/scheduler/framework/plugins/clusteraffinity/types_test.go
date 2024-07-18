/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"fmt"
	"math"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
)

const (
	nonExistentNonResourcePropertyName = "non-existent-non-resource-property"
	invalidNonResourcePropertyName     = "invalid-non-resource-property"
)

// TestInterpolateWeightFor tests the interpolateWeightFor function.
func TestInterpolateWeightFor(t *testing.T) {
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName1,
			Labels: map[string]string{
				envLabelName:    envLabelValue1,
				regionLabelName: regionLabelValue1,
			},
		},
		Status: clusterv1beta1.MemberClusterStatus{
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10"),
					corev1.ResourceMemory: resource.MustParse("40Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("36Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "4",
				},
				invalidNonResourcePropertyName: {
					Value: "invalid",
				},
			},
		},
	}

	testCases := []struct {
		name           string
		cluster        *clusterv1beta1.MemberCluster
		propertyName   string
		sortOrder      placementv1beta1.PropertySortOrder
		weight         int32
		state          *pluginState
		want           int32
		expectedToFail bool
	}{
		{
			name:           "invalid resource property name",
			cluster:        cluster,
			propertyName:   "resources.kubernetes-fleet.io/available-",
			expectedToFail: true,
		},
		{
			name:           "invalid non-resource property value",
			cluster:        cluster,
			propertyName:   invalidNonResourcePropertyName,
			expectedToFail: true,
		},
		{
			name:         "property not found",
			cluster:      cluster,
			propertyName: nonExistentNonResourcePropertyName,
			want:         0,
		},
		{
			name:         "extremums not registered in state",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{},
			},
			expectedToFail: true,
		},
		{
			name:         "no minimum value",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						max: ptr.To(resource.MustParse("4")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "no maximum value",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("4")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "min value = inf",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse(fmt.Sprintf("%f", math.MaxFloat64) + "0")),
						max: ptr.To(resource.MustParse(fmt.Sprintf("%f", math.MaxFloat64) + "0")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "max value = inf",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("0")),
						max: ptr.To(resource.MustParse(fmt.Sprintf("%f", math.MaxFloat64) + "0")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "min value > max value",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("4")),
						max: ptr.To(resource.MustParse("0")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "min value == max value",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("4")),
						max: ptr.To(resource.MustParse("4")),
					},
				},
			},
			want: 0,
		},
		{
			name:         "observation out of range, < min",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("8")),
						max: ptr.To(resource.MustParse("16")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "observation out of range, > max",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("1")),
						max: ptr.To(resource.MustParse("2")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "invalid sort order",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    "invalid",
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("6")),
					},
				},
			},
			expectedToFail: true,
		},
		{
			name:         "descending, left bound",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Descending,
			weight:       100,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("4")),
						max: ptr.To(resource.MustParse("8")),
					},
				},
			},
			want: 0,
		},
		{
			name:         "descending, right bound",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Descending,
			weight:       100,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("4")),
					},
				},
			},
			want: 100,
		},
		{
			name:         "descending, round up",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Descending,
			weight:       7,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("7")),
					},
				},
			},
			want: 3,
		},
		{
			name:         "descending, round down",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Descending,
			weight:       8,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("7")),
					},
				},
			},
			want: 3,
		},
		{
			name:         "ascending, left bound",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Ascending,
			weight:       100,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("4")),
						max: ptr.To(resource.MustParse("8")),
					},
				},
			},
			want: 100,
		},
		{
			name:         "ascending, right bound",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Ascending,
			weight:       100,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("4")),
					},
				},
			},
			want: 0,
		},
		{
			name:         "ascending, round up",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Ascending,
			weight:       8,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("7")),
					},
				},
			},
			want: 5,
		},
		{
			name:         "ascending, round down",
			cluster:      cluster,
			propertyName: propertyprovider.NodeCountProperty,
			sortOrder:    placementv1beta1.Ascending,
			weight:       7,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("7")),
					},
				},
			},
			want: 4,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			weight, err := interpolateWeightFor(tc.cluster, tc.propertyName, tc.sortOrder, tc.weight, tc.state)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("interpolateWeightFor(), want error, got nil")
				}
				return
			}

			if err != nil || weight != tc.want {
				t.Errorf("interpolateWeightFor() = %d, %v, want %d, nil", weight, err, tc.want)
			}
		})
	}
}
