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

package sameplacementaffinity

import (
	"context"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"

	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/framework"
)

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	state framework.CycleStatePluginReadWriter,
	_ *placementv1beta1.ClusterSchedulingPolicySnapshot,
	cluster *clusterv1beta1.MemberCluster,
) (status *framework.Status) {
	if !state.HasScheduledOrBoundBindingFor(cluster.Name) {
		// all done.
		return nil
	}

	reason := "resource placement has already been scheduled or bounded on the cluster"
	return framework.NewNonErrorStatus(framework.ClusterAlreadySelected, p.Name(), reason)
}
