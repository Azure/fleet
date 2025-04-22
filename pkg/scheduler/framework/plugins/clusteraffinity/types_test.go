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

package clusteraffinity

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// TestRetrieveResourceUsageFrom tests the retrieveResourceUsageFrom function.
func TestRetrieveResourceUsageFrom(t *testing.T) {
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName1,
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
		},
	}

	testCases := []struct {
		name           string
		cluster        *clusterv1beta1.MemberCluster
		propertyName   string
		wantQuantity   *resource.Quantity
		expectedToFail bool
	}{
		{
			name:           "invalid property name (multiple segments)",
			propertyName:   "resources.kubernetes-fleet.io/allocatable-cpu",
			expectedToFail: true,
		},
		{
			name:           "invalid property name (no capacity type)",
			propertyName:   "-cpu",
			expectedToFail: true,
		},
		{
			name:           "invalid property name (no resource name)",
			propertyName:   "allocatable-",
			expectedToFail: true,
		},
		{
			name:           "invalid property name (not a known capacity type)",
			propertyName:   "additional-",
			expectedToFail: true,
		},
		{
			name:         "resource not available",
			propertyName: "allocatable-gpu",
			cluster:      cluster,
		},
		{
			name:         "total capacity usage",
			propertyName: "total-cpu",
			cluster:      cluster,
			wantQuantity: ptr.To(resource.MustParse("10")),
		},
		{
			name:         "allocatable capacity usage",
			propertyName: "allocatable-memory",
			cluster:      cluster,
			wantQuantity: ptr.To(resource.MustParse("36Gi")),
		},
		{
			name:         "available capacity usage",
			propertyName: "available-cpu",
			cluster:      cluster,
			wantQuantity: ptr.To(resource.MustParse("2")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := retrieveResourceUsageFrom(tc.cluster, tc.propertyName)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("retrieveResourceUsageFrom(), want error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("retrieveResourceUsageFrom() = %v, want nil", err)
			}
			if diff := cmp.Diff(q, tc.wantQuantity); diff != "" {
				t.Errorf("retrieveResourceUsageFrom() quantity diff (-got, +want): %s\n", diff)
			}
		})
	}
}

// TestRetrievePropertyValueFrom tests the retrievePropertyValueFrom function.
func TestRetrievePropertyValueFrom(t *testing.T) {
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName1,
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
		wantQuantity   *resource.Quantity
		expectedToFail bool
	}{
		{
			name:           "invalid resource property (name format error)",
			propertyName:   "resources.kubernetes-fleet.io/allocatable",
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name:         "resource property retrieval",
			propertyName: propertyprovider.AvailableMemoryCapacityProperty,
			cluster:      cluster,
			wantQuantity: ptr.To(resource.MustParse("4Gi")),
		},
		{
			name:         "absent non-resource property",
			propertyName: nonExistentNonResourcePropertyName,
			cluster:      cluster,
		},
		{
			name:           "invalid non-resource property (value format error)",
			propertyName:   invalidNonResourcePropertyName,
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name:         "non-resource property retrieval",
			propertyName: propertyprovider.NodeCountProperty,
			wantQuantity: ptr.To(resource.MustParse("4")),
			cluster:      cluster,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := retrievePropertyValueFrom(tc.cluster, tc.propertyName)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("retrievePropertyValueFrom(), want error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("retrievePropertyValueFrom() = %v, want nil", err)
			}
			if diff := cmp.Diff(q, tc.wantQuantity); diff != "" {
				t.Errorf("retrievePropertyValueFrom() quantity diff (-got, +want): %s\n", diff)
			}
		})
	}
}

// TestClusterRequirementMatches tests the Matches method on clusterRequirement pointers.
func TestClusterRequirementMatches(t *testing.T) {
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
		name               string
		clusterRequirement *clusterRequirement
		cluster            *clusterv1beta1.MemberCluster
		want               bool
		expectedToFail     bool
	}{
		{
			name: "invalid label selector",
			clusterRequirement: &clusterRequirement{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      regionLabelName,
							Operator: metav1.LabelSelectorOperator("invalid"),
							Values: []string{
								regionLabelValue1,
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "label selector mismatches",
			clusterRequirement: &clusterRequirement{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						envLabelName: envLabelValue2,
					},
				},
			},
			cluster: cluster,
			want:    false,
		},
		{
			name: "label selector matches, no property selector",
			clusterRequirement: &clusterRequirement{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						envLabelName: envLabelValue1,
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "label selector matches, no expressions in the property selector",
			clusterRequirement: &clusterRequirement{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						envLabelName: envLabelValue1,
					},
				},
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "invalid resource property name",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     "resources.kubernetes-fleet.io/cpu",
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"2",
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "property not found",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     nonExistentNonResourcePropertyName,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"0",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
		{
			name: "multiple value options",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.NodeCountProperty,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"1",
								"2",
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "invalid property value",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     invalidNonResourcePropertyName,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"1",
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "invalid value option",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.NodeCountProperty,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"invalid",
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "invalid operator",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.NodeCountProperty,
							Operator: "invalid",
							Values: []string{
								"1",
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "op =, matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.NodeCountProperty,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"4",
							},
						},
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "op =, not matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.NodeCountProperty,
							Operator: placementv1beta1.PropertySelectorEqualTo,
							Values: []string{
								"8",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
		{
			name: "op !=, matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.TotalCPUCapacityProperty,
							Operator: placementv1beta1.PropertySelectorNotEqualTo,
							Values: []string{
								"11",
							},
						},
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "op !=, not matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.TotalCPUCapacityProperty,
							Operator: placementv1beta1.PropertySelectorNotEqualTo,
							Values: []string{
								"10",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
		{
			name: "op >, matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.AllocatableMemoryCapacityProperty,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"30Gi",
							},
						},
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "op >, not matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.AllocatableMemoryCapacityProperty,
							Operator: placementv1beta1.PropertySelectorGreaterThan,
							Values: []string{
								"40Gi",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
		{
			name: "op <, matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.AvailableCPUCapacityProperty,
							Operator: placementv1beta1.PropertySelectorLessThan,
							Values: []string{
								"4",
							},
						},
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "op <, not matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.AvailableCPUCapacityProperty,
							Operator: placementv1beta1.PropertySelectorLessThan,
							Values: []string{
								"1",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
		{
			name: "op >=, matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.TotalMemoryCapacityProperty,
							Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
							Values: []string{
								"40Gi",
							},
						},
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "op >=, not matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.TotalMemoryCapacityProperty,
							Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
							Values: []string{
								"41Gi",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
		{
			name: "op <=, matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.AllocatableCPUCapacityProperty,
							Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
							Values: []string{
								"8",
							},
						},
					},
				},
			},
			cluster: cluster,
			want:    true,
		},
		{
			name: "op <=, not matched",
			clusterRequirement: &clusterRequirement{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     propertyprovider.AllocatableCPUCapacityProperty,
							Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
							Values: []string{
								"7",
							},
						},
					},
				},
			},
			cluster: cluster,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := tc.clusterRequirement.Matches(tc.cluster)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("clusterRequirement.Matches(), want error, got nil")
				}
				return
			}

			if err != nil || matches != tc.want {
				t.Errorf("clusterRequirement.Matches() = %v, %v, want %v, nil", matches, err, tc.want)
			}
		})
	}
}

// TestClusterPreferenceScores tests the Scores method on clusterPreference pointers.
func TestClusterPreferenceScores(t *testing.T) {
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
		name              string
		clusterPreference *clusterPreference
		cluster           *clusterv1beta1.MemberCluster
		state             *pluginState
		want              int32
		expectedToFail    bool
	}{
		{
			name: "invalid label selector",
			clusterPreference: &clusterPreference{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      regionLabelName,
								Operator: metav1.LabelSelectorOperator("invalid"),
								Values: []string{
									regionLabelValue1,
								},
							},
						},
					},
				},
			},
			cluster:        cluster,
			expectedToFail: true,
		},
		{
			name: "label selector mismatches",
			clusterPreference: &clusterPreference{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelName: envLabelValue2,
						},
					},
					PropertySorter: &placementv1beta1.PropertySorter{
						Name:      propertyprovider.NodeCountProperty,
						SortOrder: placementv1beta1.Ascending,
					},
				},
			},
			cluster: cluster,
			want:    0,
		},
		{
			name: "label selector matches, no property sorter",
			clusterPreference: &clusterPreference{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelName: envLabelValue1,
						},
					},
				},
			},
			cluster: cluster,
			want:    100,
		},
		{
			name: "weight interpolation fails",
			clusterPreference: &clusterPreference{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelName: envLabelValue2,
						},
					},
					PropertySorter: &placementv1beta1.PropertySorter{
						Name:      propertyprovider.NodeCountProperty,
						SortOrder: placementv1beta1.Ascending,
					},
				},
			},
			cluster: cluster,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{},
			},
			want: 0,
		},
		{
			name: "weight interpolation succeeds",
			clusterPreference: &clusterPreference{
				Weight: 100,
				Preference: placementv1beta1.ClusterSelectorTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelName: envLabelValue1,
						},
					},
					PropertySorter: &placementv1beta1.PropertySorter{
						Name:      propertyprovider.NodeCountProperty,
						SortOrder: placementv1beta1.Ascending,
					},
				},
			},
			cluster: cluster,
			state: &pluginState{
				minMaxValuesByProperty: map[string]observedMinMaxValues{
					propertyprovider.NodeCountProperty: {
						min: ptr.To(resource.MustParse("2")),
						max: ptr.To(resource.MustParse("6")),
					},
				},
			},
			want: 50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := tc.clusterPreference.Scores(tc.state, tc.cluster)
			if tc.expectedToFail {
				if err == nil {
					t.Errorf("clusterPreference.Scores(), want error, got nil")
				}
				return
			}

			if err != nil || w != tc.want {
				t.Errorf("clusterPreference.Scores() = %v, %v, want %v, nil", w, err, tc.want)
			}
		})
	}
}

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
