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

package namespaceaffinity

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/scheduler/framework"
)

// PreFilter allows the plugin to connect to the PreFilter extension point in the scheduling framework.
func (p *Plugin) PreFilter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	ps placementv1beta1.PolicySnapshotObj,
) (status *framework.Status) {
	// Check if this is a namespace-scoped policy snapshot (ResourcePlacement).
	// ClusterResourcePlacement uses ClusterSchedulingPolicySnapshot which doesn't have a namespace,
	// so we only need to filter for ResourcePlacement (SchedulingPolicySnapshot).
	nsName := ps.GetNamespace()
	if nsName == "" {
		// This is a cluster-scoped policy (ClusterResourcePlacement).
		// Skip namespace affinity filtering.
		return framework.NewNonErrorStatus(framework.Skip, p.Name(), "cluster-scoped placement does not require namespace affinity filtering")
	}

	// For namespace-scoped placements, we need to ensure the target namespace exists on clusters.
	return nil
}

// Filter allows the plugin to connect to the Filter extension point in the scheduling framework.
func (p *Plugin) Filter(
	_ context.Context,
	_ framework.CycleStatePluginReadWriter,
	ps placementv1beta1.PolicySnapshotObj,
	cluster *clusterv1beta1.MemberCluster,
) (status *framework.Status) {
	// Get the target namespace for this ResourcePlacement.
	nsName := ps.GetNamespace()

	// Check if namespace collection is enabled for this cluster.
	// The condition can have three states:
	// 1. Missing: namespace collection is not enabled (backward compatibility - skip filtering)
	// 2. True: namespace collection is working normally
	// 3. False: namespace collection is enabled but degraded (limit reached - still use the data)
	cond := meta.FindStatusCondition(cluster.Status.Conditions, propertyprovider.NamespaceCollectionSucceededCondType)
	if cond == nil {
		// Namespace collection is not enabled, skip filtering for backward compatibility.
		return nil
	}

	// Check if the cluster has namespace information available.
	if cluster.Status.Namespaces == nil {
		// Namespace collection is enabled but no data is available.
		// This is unexpected, so we mark the cluster as unschedulable.
		return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "cluster has no namespace information available")
	}

	// Check if the target namespace exists on the cluster.
	if _, exists := cluster.Status.Namespaces[nsName]; !exists {
		// The namespace does not exist on this cluster.
		return framework.NewNonErrorStatus(framework.ClusterUnschedulable, p.Name(), "target namespace does not exist on cluster")
	}

	// The namespace exists on the cluster; mark it as eligible for resource placement.
	return nil
}
