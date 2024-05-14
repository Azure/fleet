/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusteraffinity

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/propertyprovider/azure"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

const (
	clusterName1 = "cluster-1"
	clusterName2 = "cluster-2"

	regionLabelName   = "region"
	regionLabelValue1 = "eastus"
	regionLabelValue2 = "westus"

	envLabelName   = "env"
	envLabelValue1 = "prod"

	nodeCountPropertyValue1 = "3"

	availableCPUPropertyValue1 = "10"
)

var (
	p = New()

	ignoreStatusErrorField = cmpopts.IgnoreFields(framework.Status{}, "err")
)

// TestPreFilter tests the PreFilter extension point of the plugin.
func TestPreFilter(t *testing.T) {
	testCases := []struct {
		name       string
		ps         *placementv1beta1.ClusterSchedulingPolicySnapshot
		wantStatus *framework.Status
	}{
		{
			name: "has no scheduling policy",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: nil,
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity terms to enforce"),
		},
		{
			name: "has no affinity",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: nil,
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity terms to enforce"),
		},
		{
			name: "has no cluster affinity",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: nil,
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity terms to enforce"),
		},
		{
			name: "has no required cluster affinity terms",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: nil,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity terms to enforce"),
		},
		{
			name: "has no cluster selectors",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: nil,
								},
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.Skip, p.Name(), "no required cluster affinity terms to enforce"),
		},
		{
			name: "has required cluster selector term",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil, nil)
			status := p.PreFilter(ctx, state, tc.ps)

			if diff := cmp.Diff(
				status, tc.wantStatus,
				cmp.AllowUnexported(framework.Status{}),
				ignoreStatusErrorField,
			); diff != "" {
				t.Errorf("PreFilter() unexpected status (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestFilter tests the Filter extension point of the plugin.
func TestFilter(t *testing.T) {
	testCases := []struct {
		name       string
		ps         *placementv1beta1.ClusterSchedulingPolicySnapshot
		cluster    *clusterv1beta1.MemberCluster
		wantStatus *framework.Status
	}{
		{
			name: "single cluster selector term, matched",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
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
						propertyprovider.NodeCountProperty: {
							Value: "4",
						},
					},
				},
			},
		},
		{
			name: "single cluster cost based term, matched",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     azure.PerGBMemoryCostProperty,
														Operator: placementv1beta1.PropertySelectorLessThan,
														Values: []string{
															"0.2",
														},
													},
													{
														Name:     azure.PerCPUCoreCostProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															"0.06",
														},
													},
												},
											},
										},
									},
								},
							},
						},
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
						azure.PerGBMemoryCostProperty: {
							Value: "0.16",
						},
						azure.PerCPUCoreCostProperty: {
							Value: "0.06",
						},
					},
				},
			},
		},
		{
			name: "single cluster cost based term, less than not matched",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     azure.PerGBMemoryCostProperty,
														Operator: placementv1beta1.PropertySelectorLessThan,
														Values: []string{
															"0.12",
														},
													},
													{
														Name:     azure.PerCPUCoreCostProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															"0.06",
														},
													},
												},
											},
										},
									},
								},
							},
						},
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
						azure.PerGBMemoryCostProperty: {
							Value: "0.16",
						},
						azure.PerCPUCoreCostProperty: {
							Value: "0.06",
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster does not match with any of the required cluster affinity terms"),
		},
		{
			name: "multiple cluster cost based term, less than not matched, but one cluster selector term matched",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     azure.PerGBMemoryCostProperty,
														Operator: placementv1beta1.PropertySelectorLessThan,
														Values: []string{
															"0.12",
														},
													},
												},
											},
										},
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     azure.PerCPUCoreCostProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															"0.06",
														},
													},
												},
											},
										},
									},
								},
							},
						},
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
						azure.PerGBMemoryCostProperty: {
							Value: "0.16",
						},
						azure.PerCPUCoreCostProperty: {
							Value: "0.06",
						},
					},
				},
			},
		},
		{
			name: "multiple cluster selector terms, matched",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorLessThan,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
										{
											LabelSelector: &metav1.LabelSelector{
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envLabelValue1,
														},
													},
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.AvailableCPUCapacityProperty,
														Operator: placementv1beta1.PropertySelectorGreaterThan,
														Values: []string{
															availableCPUPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
						envLabelName:    envLabelValue1,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "4",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU: resource.MustParse("15"),
						},
					},
				},
			},
		},
		{
			name: "single cluster selector term, not matched (neither)",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
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
						propertyprovider.NodeCountProperty: {
							Value: "4",
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster does not match with any of the required cluster affinity terms"),
		},
		{
			name: "single cluster selector term, not matched (label selector)",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
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
						propertyprovider.NodeCountProperty: {
							Value: nodeCountPropertyValue1,
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster does not match with any of the required cluster affinity terms"),
		},
		{
			name: "single cluster selector term, not matched (property selector)",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue2,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorEqualTo,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "4",
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster does not match with any of the required cluster affinity terms"),
		},
		{
			name: "multiple cluster selector terms, not matched",
			ps: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						Affinity: &placementv1beta1.Affinity{
							ClusterAffinity: &placementv1beta1.ClusterAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													regionLabelName: regionLabelValue1,
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.NodeCountProperty,
														Operator: placementv1beta1.PropertySelectorNotEqualTo,
														Values: []string{
															nodeCountPropertyValue1,
														},
													},
												},
											},
										},
										{
											LabelSelector: &metav1.LabelSelector{
												MatchExpressions: []metav1.LabelSelectorRequirement{
													{
														Key:      envLabelName,
														Operator: metav1.LabelSelectorOpIn,
														Values: []string{
															envLabelValue1,
														},
													},
												},
											},
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     propertyprovider.AvailableCPUCapacityProperty,
														Operator: placementv1beta1.PropertySelectorLessThanOrEqualTo,
														Values: []string{
															availableCPUPropertyValue1,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName1,
					Labels: map[string]string{
						regionLabelName: regionLabelValue2,
						envLabelName:    envLabelValue1,
					},
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
				Status: clusterv1beta1.MemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "3",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Available: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU: resource.MustParse("15"),
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster does not match with any of the required cluster affinity terms"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			state := framework.NewCycleState(nil, nil, nil)
			status := p.Filter(ctx, state, tc.ps, tc.cluster)

			if diff := cmp.Diff(
				status, tc.wantStatus,
				cmp.AllowUnexported(framework.Status{}),
				ignoreStatusErrorField,
			); diff != "" {
				t.Errorf("Filter() unexpected status (-got, +want):\n%s", diff)
			}
		})
	}
}
