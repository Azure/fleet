/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestRetrieveResourcePropertyValueFrom tests the retrieveResourcePropertyValueFrom function.
func TestRetrieveResourcePropertyValueFrom(t *testing.T) {
	testCases := []struct {
		name             string
		cluster          *clusterv1beta1.MemberCluster
		propertyName     string
		wantQuantity     *resource.Quantity
		wantErrStrPrefix string
	}{
		{
			name:             "invalid property name (incorrect seg count, too little)",
			propertyName:     "total",
			wantErrStrPrefix: "invalid resource property name",
		},
		{
			name:             "invalid property name (incorrect seg count, too many)",
			propertyName:     "total-super-cpu",
			wantErrStrPrefix: "invalid resource property name",
		},
		{
			name:             "invalid property name (no capacity type)",
			propertyName:     "-cpu",
			wantErrStrPrefix: "invalid resource property name",
		},
		{
			name:             "invalid property name (no resource type)",
			propertyName:     "total-",
			wantErrStrPrefix: "invalid resource property name",
		},
		{
			name:         "query total CPU capacity",
			propertyName: "total-cpu",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							"cpu": resource.MustParse("10"),
						},
					},
				},
			},
			wantQuantity: ptr.To(resource.MustParse("10")),
		},
		{
			name:         "query allocatable memory capacity",
			propertyName: "allocatable-memory",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Gi"),
						},
					},
				},
			},
			wantQuantity: ptr.To(resource.MustParse("100Gi")),
		},
		{
			// GPU is not a currently supported resource type.
			name:         "query available GPU capacity",
			propertyName: "available-gpu",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							"gpu": resource.MustParse("2"),
						},
					},
				},
			},
			wantQuantity: ptr.To(resource.MustParse("2")),
		},
		{
			name:             "query an non-existent capacity type",
			propertyName:     "capacity-cpu",
			wantErrStrPrefix: "invalid capacity type",
		},
		{
			name:         "query an non-existent resource type",
			propertyName: "total-foo",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							"cpu": resource.MustParse("2"),
						},
					},
				},
			},
			wantQuantity: nil,
		},
		{
			// GPU is not a currently supported resource type.
			name:         "query available GPU capacity (zero value)",
			propertyName: "available-gpu",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							"gpu": resource.MustParse("0"),
						},
					},
				},
			},
			wantQuantity: &resource.Quantity{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := retrieveResourcePropertyValueFrom(tc.cluster, tc.propertyName)
			if tc.wantErrStrPrefix != "" {
				if err == nil {
					t.Fatalf("retrieveResourcePropertyValueFrom(), got no error, want error with prefix %s", tc.wantErrStrPrefix)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrStrPrefix) {
					t.Fatalf("retrieveResourcePropertyValueFrom(), got error %v, expected error with prefix %s", err, tc.wantErrStrPrefix)
				}
				return
			}

			if q == nil {
				if tc.wantQuantity != nil {
					t.Fatalf("retrieveResourcePropertyValueFrom() = nil, expected %+v", tc.wantQuantity)
				}
				return
			}

			if q.Cmp(*tc.wantQuantity) != 0 {
				t.Fatalf("retrieveResourcePropertyValueFrom() = %+v, expected %+v", q, tc.wantQuantity)
			}
		})
	}
}

// TestRetrievePropertyValueFrom tests the retrievePropertyValueFrom function.
func TestRetrievePropertyValueFrom(t *testing.T) {
	testCases := []struct {
		name             string
		cluster          *clusterv1beta1.MemberCluster
		propertyName     string
		wantQuantity     *resource.Quantity
		wantErrStrPrefix string
	}{
		{
			name:         "retrieve resource metric",
			propertyName: availableCPUPropertyName,
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							"cpu": resource.MustParse("20"),
						},
					},
				},
			},
			wantQuantity: ptr.To(resource.MustParse("20")),
		},
		{
			name:         "retrieve non-resource metric",
			propertyName: nodeCountPropertyName,
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "2",
						},
					},
				},
			},
			wantQuantity: ptr.To(resource.MustParse("2")),
		},
		{
			name:         "retrieve non-resource metric (not found)",
			propertyName: nodeCountPropertyName,
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{},
				},
			},
		},
		{
			name:         "retrieve non-resource metric (zero value)",
			propertyName: nodeCountPropertyName,
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "0",
						},
					},
				},
			},
			wantQuantity: &resource.Quantity{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := retrievePropertyValueFrom(tc.cluster, tc.propertyName)
			if tc.wantErrStrPrefix != "" {
				if err == nil {
					t.Fatalf("retrievePropertyValueFrom(), got no error, want error with prefix %s", tc.wantErrStrPrefix)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrStrPrefix) {
					t.Fatalf("retrievePropertyValueFrom(), got error %v, expected error with prefix %s", err, tc.wantErrStrPrefix)
				}
				return
			}

			if q == nil {
				if tc.wantQuantity != nil {
					t.Fatalf("retrievePropertyValueFrom() = nil, expected %+v", tc.wantQuantity)
				}
				return
			}

			if q.Cmp(*tc.wantQuantity) != 0 {
				t.Fatalf("retrievePropertyValueFrom() = %+v, expected %+v", q, tc.wantQuantity)
			}
		})
	}
}

// TestClusterRequirementMatches tests the Matches method of the clusterRequirement type.
func TestClusterRequirementMatches(t *testing.T) {
	testCases := []struct {
		name             string
		r                clusterRequirement
		cluster          *clusterv1beta1.MemberCluster
		wantRes          bool
		wantErrStrPrefix string
	}{
		{
			name: "with label selector only, matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with label selector only, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
					},
				},
			},
		},
		{
			name: "with property expression, lt",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorLessThan,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "1",
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with property expression, lt, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorLessThan,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "4",
						},
					},
				},
			},
		},
		{
			name: "with property expression, gt",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "4",
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with property expression, gt, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "1",
						},
					},
				},
			},
		},
		{
			name: "with property expression, le",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "3",
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with property expression, le, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "4",
						},
					},
				},
			},
		},
		{
			name: "with property expression, ge",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "3",
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with property expression, ge, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "1",
						},
					},
				},
			},
		},
		{
			name: "with property expression, eq",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "3",
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with property expression, eq, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "2",
						},
					},
				},
			},
		},
		{
			name: "with property expression, ne",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorNotEqualTo,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "1",
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "with property expression, ne, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"3",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "3",
						},
					},
				},
			},
		},
		{
			name: "multple expressions",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"3",
							},
						},
						{
							Name:     availableCPUPropertyName,
							Operator: placementv1beta1.PropertySelectorLessThan,
							Values: []string{
								"20",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "4",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("10"),
						},
					},
				},
			},
			wantRes: true,
		},
		{
			name: "multple expressions, not matched",
			r: clusterRequirement(placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nodeCountPropertyName,
							Operator: placementv1beta1.PropertySelectorLessThan,
							Values: []string{
								"3",
							},
						},
						{
							Name:     availableCPUPropertyName,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"20",
							},
						},
					},
				},
			}),
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "1",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("10"),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.r.Matches(tc.cluster)
			if tc.wantErrStrPrefix != "" {
				if err == nil {
					t.Fatalf("Matches(), got no error, want error with prefix %s", tc.wantErrStrPrefix)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrStrPrefix) {
					t.Fatalf("Matches(), got error %v, expected error with prefix %s", err, tc.wantErrStrPrefix)
				}
				return
			}

			if res != tc.wantRes {
				t.Fatalf("Matches() = %t, expected %t", res, tc.wantRes)
			}
		})
	}
}

// TestInterpolateWeightFrom tests the interpolateWeightFrom function.
func TestInterpolateWeightFrom(t *testing.T) {
	testCases := []struct {
		name             string
		cluster          *clusterv1beta1.MemberCluster
		propertyName     string
		sortOrder        placementv1beta1.PropertySortOrder
		weight           int32
		ps               *pluginState
		wantWeight       int32
		wantErrStrPrefix string
	}{
		{
			name:         "interpolate weight in ascending order (non-resource property)",
			propertyName: nodeCountPropertyName,
			sortOrder:    placementv1beta1.Ascending,
			weight:       100,
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("1")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "2",
						},
					},
				},
			},
			wantWeight: 89,
		},
		{
			name:         "interpolate weight in descending order (non-resource property)",
			propertyName: nodeCountPropertyName,
			sortOrder:    placementv1beta1.Descending,
			weight:       100,
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("1")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "2",
						},
					},
				},
			},
			wantWeight: 11,
		},
		{
			name:         "interpolate weight in ascending order (resource property)",
			propertyName: availableCPUPropertyName,
			sortOrder:    placementv1beta1.Ascending,
			weight:       100,
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					availableCPUPropertyName: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("50")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							"cpu": resource.MustParse("20"),
						},
					},
				},
			},
			wantWeight: 75,
		},
		{
			name:         "interpolate weight in descending order (resource property)",
			propertyName: availableCPUPropertyName,
			sortOrder:    placementv1beta1.Descending,
			weight:       100,
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					availableCPUPropertyName: {
						min: ptr.To(resource.MustParse("10")),
						max: ptr.To(resource.MustParse("50")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: corev1.ResourceList{
							"cpu": resource.MustParse("20"),
						},
					},
				},
			},
			wantWeight: 25,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := interpolateWeightFor(tc.cluster, tc.propertyName, tc.sortOrder, tc.weight, tc.ps)
			if tc.wantErrStrPrefix != "" {
				if err == nil {
					t.Fatalf("interpolateWeightFor(), got no error, want error with prefix %s", tc.wantErrStrPrefix)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrStrPrefix) {
					t.Fatalf("interpolateWeightFor(), got error %v, expected error with prefix %s", err, tc.wantErrStrPrefix)
				}
				return
			}

			if w != tc.wantWeight {
				t.Fatalf("interpolateWeightFor() = %d, expected %d", w, tc.wantWeight)
			}
		})
	}
}

// TestClusterPreferenceScores tests the Scores method of the clusterPreference type.
func TestClusterPreferenceScores(t *testing.T) {
	testCases := []struct {
		name             string
		preference       clusterPreference
		ps               *pluginState
		cluster          *clusterv1beta1.MemberCluster
		wantWeight       int32
		wantErrStrPrefix string
	}{
		{
			name: "preference with no sorting, matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							regionLabelName: regionLabelValue1,
						},
					},
				},
			}),
			ps: &pluginState{},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
				Spec:   clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{},
			},
			wantWeight: 100,
		},
		{
			name: "preferece with sorting in ascending order, matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					PropertySorter: &placementv1beta1.PropertySortPreference{
						Name:      nodeCountPropertyName,
						SortOrder: placementv1beta1.Ascending,
					},
				},
			}),
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("5")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "5",
						},
					},
				},
			},
			wantWeight: 100,
		},
		{
			name: "preference with sorting in descending order, matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					PropertySorter: &placementv1beta1.PropertySortPreference{
						Name:      nodeCountPropertyName,
						SortOrder: placementv1beta1.Descending,
					},
				},
			}),
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("5")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "10",
						},
					},
				},
			},
			wantWeight: 100,
		},
		{
			name: "preference with no sorting, not matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							regionLabelName: regionLabelValue1,
						},
					},
				},
			}),
			ps: &pluginState{},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
					},
				},
				Spec:   clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{},
			},
			wantWeight: 0,
		},
		{
			name: "preference with sorting, not matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					PropertySorter: &placementv1beta1.PropertySortPreference{
						Name:      nodeCountPropertyName,
						SortOrder: placementv1beta1.Descending,
					},
				},
			}),
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("5")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "10",
						},
					},
				},
			},
			wantWeight: 100,
		},
		{
			name: "preference with label selector and sorting, matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							regionLabelName: regionLabelValue1,
						},
					},
					PropertySorter: &placementv1beta1.PropertySortPreference{
						Name:      nodeCountPropertyName,
						SortOrder: placementv1beta1.Descending,
					},
				},
			}),
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("5")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "10",
						},
					},
				},
			},
			wantWeight: 100,
		},
		{
			name: "preference with label selector and sorting, not matched",
			preference: clusterPreference(placementv1beta1.PreferredClusterSelector{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							regionLabelName: regionLabelValue2,
						},
					},
					PropertySorter: &placementv1beta1.PropertySortPreference{
						Name:      nodeCountPropertyName,
						SortOrder: placementv1beta1.Descending,
					},
				},
			}),
			ps: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					nodeCountPropertyName: {
						min: ptr.To(resource.MustParse("5")),
						max: ptr.To(resource.MustParse("10")),
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue1,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						nodeCountPropertyName: {
							Value: "10",
						},
					},
				},
			},
			wantWeight: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := tc.preference.Scores(tc.ps, tc.cluster)
			if tc.wantErrStrPrefix != "" {
				if err == nil {
					t.Fatalf("Scores(), got no error, want error with prefix %s", tc.wantErrStrPrefix)
				}

				if !strings.HasPrefix(err.Error(), tc.wantErrStrPrefix) {
					t.Fatalf("Scores(), got error %v, expected error with prefix %s", err, tc.wantErrStrPrefix)
				}
				return
			}

			if w != tc.wantWeight {
				t.Fatalf("Scores() = %d, expected %d", w, tc.wantWeight)
			}
		})
	}
}
