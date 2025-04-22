package tainttoleration

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
)

var (
	cmpStatusOptions = cmp.Options{
		cmpopts.IgnoreFields(framework.Status{}, "err"),
		cmp.AllowUnexported(framework.Status{}),
	}
)

func TestFilter(t *testing.T) {
	p := New()
	tests := []struct {
		name           string
		cluster        *clusterv1beta1.MemberCluster
		policySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot
		wantStatus     *framework.Status
	}{
		{
			name: "empty taints",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "empty tolerations",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key1", Value: "value1", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "taints can be tolerated based on key, value & effect with Equal operator, every taint has matching toleration - nil status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
								Effect:   corev1.TaintEffectNoSchedule,
							},
							{
								Key:      "key2",
								Operator: corev1.TolerationOpEqual,
								Value:    "value2",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "taints can be tolerated based on key, value & empty effect with Equal operator, every taint has matching toleration - nil status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
							},
							{
								Key:      "key2",
								Operator: corev1.TolerationOpEqual,
								Value:    "value2",
							},
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "taints can be tolerated based on key, empty value, effect with Equal operator, every taint has matching toleration - nil status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
							},
							{
								Key:      "key2",
								Operator: corev1.TolerationOpEqual,
							},
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "taints can be tolerated based on key, effect with Exists operator, every taint has matching toleration - nil status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key3",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoSchedule,
							},
							{
								Key:      "key2",
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoSchedule,
							},
							{
								Key:      "key3",
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "taints can be tolerated by one toleration based on effect with Exists operator, all taints tolerated by one toleration - nil status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key3",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "taints can be tolerated by one toleration based on empty effect with Exists operator, all taints tolerated by one toleration - nil status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Value:  "value2",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key3",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Operator: corev1.TolerationOpExists,
							},
						},
					},
				},
			},
			wantStatus: nil,
		},
		{
			name: "taint with key, value, effect cannot be tolerated, values don't match, operator is equal - ClusterUnschedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value2",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key1", Value: "value1", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "taint with key, value, effect cannot be tolerated, keys don't match, operator is equal - ClusterUnschedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key2",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key1", Value: "value1", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "taint with key, effect cannot be tolerated, keys don't match, values don't match, operator is equal - ClusterUnSchedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
								Effect:   corev1.TaintEffectNoSchedule,
							},
							{
								Key:      "key2",
								Operator: corev1.TolerationOpEqual,
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key1", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "one of multiple taints cannot be tolerated, missing toleration, operator is equal - ClusterUnSchedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key2", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "taints with key, value, effect cannot be tolerated, keys don't match, operator is exists - ClusterUnschedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key2",
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key1", Value: "value1", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "taints with key, effect cannot be tolerated, keys don't match, operator is exists - ClusterUnschedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key2",
								Operator: corev1.TolerationOpExists,
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key1", Effect: corev1.TaintEffectNoSchedule})),
		},
		{
			name: "one of multiple taints cannot be tolerated, missing toleration, operator is exists - ClusterUnSchedulable status",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Taints: []clusterv1beta1.Taint{
						{
							Key:    "key1",
							Value:  "value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "key2",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			policySnapshot: &placementv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csp-1",
				},
				Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpExists,
							},
						},
					},
				},
			},
			wantStatus: framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, &clusterv1beta1.Taint{Key: "key2", Effect: corev1.TaintEffectNoSchedule})),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Filter(context.TODO(), nil, tc.policySnapshot, tc.cluster)
			if diff := cmp.Diff(tc.wantStatus, got, cmpStatusOptions); diff != "" {
				t.Errorf("Filter() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
