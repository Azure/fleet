package tainttoleration

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
)

var (
	reasonFmt = "taint %+v cannot be tolerated"
)

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *clusterv1beta1.MemberCluster,
) (status *framework.Status) {
	taint, isUntolerated := findUntoleratedTaint(cluster.Spec.Taints, policy.Tolerations())
	if !isUntolerated {
		return nil
	}
	policyRef := klog.KObj(policy)
	klog.V(2).InfoS("Cluster is unschedulable, because taint cannot be tolerated", "clusterSchedulingPolicySnapshot", policyRef, "taint", taint)
	return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), fmt.Sprintf(reasonFmt, taint))
}

func findUntoleratedTaint(taints []clusterv1beta1.Taint, tolerations []placementv1beta1.Toleration) (*clusterv1beta1.Taint, bool) {
	for _, taint := range taints {
		if !tolerationsTolerateTaint(taint, tolerations) {
			return &taint, true
		}
	}
	return nil, false
}

func tolerationsTolerateTaint(taint clusterv1beta1.Taint, tolerations []placementv1beta1.Toleration) bool {
	for _, toleration := range tolerations {
		if canTolerationTolerateTaint(taint, toleration) {
			return true
		}
	}
	return false
}

func canTolerationTolerateTaint(taint clusterv1beta1.Taint, toleration placementv1beta1.Toleration) bool {
	if toleration.Operator == corev1.TolerationOpExists {
		if toleration.Key == "" || toleration.Key == taint.Key {
			return toleration.Effect == taint.Effect || toleration.Effect == ""
		}
	}
	if toleration.Operator == corev1.TolerationOpEqual {
		if toleration.Key == taint.Key && toleration.Value == taint.Value {
			return toleration.Effect == taint.Effect || toleration.Effect == ""
		}
	}
	return false
}
