/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package agentstatus features some utilties for verifying if a member cluster is eligible
// for resource placement.
package agentstatus

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// IsClusterEligible returns if a cluster is eligible for resource placement; if not, it will
// also return the reason.
func IsClusterEligible(cluster *fleetv1beta1.MemberCluster, clusterHeartbeatTimeout, clusterHealthCheckTimeout time.Duration) (eligible bool, reason string) {
	// Filter out clusters that have left the fleet.
	if cluster.Spec.State == fleetv1beta1.ClusterStateLeave {
		return false, "cluster has left the fleet"
	}

	// Note that the following checks are performed against one specific agent, i.e., the member
	// agent, which is critical for the work orchestration related tasks in the fleet; non-related
	// agents (e.g., networking) are not accounted for in this plugin.

	// Filter out clusters that are no longer connected to the fleet, i.e., its heartbeat signals
	// have stopped for a prolonged period of time.
	memberAgentStatus := cluster.GetAgentStatus(fleetv1beta1.MemberAgent)
	if memberAgentStatus == nil {
		// The member agent has not updated its status with the hub cluster yet.
		return false, "cluster is not connected to the fleet: member agent not online yet"
	}

	memberAgentJoinedCond := cluster.GetAgentCondition(fleetv1beta1.MemberAgent, fleetv1beta1.AgentJoined)
	if memberAgentJoinedCond == nil || memberAgentJoinedCond.Status != metav1.ConditionTrue {
		// The member agent has not joined yet; i.e., some of the controllers have not been
		// spun up.
		return false, "cluster is not connected to the fleet: member agent not joined yet"
	}

	sinceLastHeartbeat := time.Since(memberAgentStatus.LastReceivedHeartbeat.Time)
	if sinceLastHeartbeat > clusterHeartbeatTimeout {
		// The member agent has not sent heartbeat signals for a prolonged period of time.
		//
		// Note that this plugin assumes minimum clock drifts between clusters in the fleet.
		return false, fmt.Sprintf("cluster is not connected to the fleet: no recent heartbeat signals (last received %.2f minutes ago)", sinceLastHeartbeat.Minutes())
	}

	memberAgentHealthyCond := cluster.GetAgentCondition(fleetv1beta1.MemberAgent, fleetv1beta1.AgentHealthy)
	if memberAgentHealthyCond == nil {
		// The health condition is absent.
		return false, "cluster is not connected to the fleet: health condition not available"
	}

	sinceLastTransition := time.Since(memberAgentHealthyCond.LastTransitionTime.Time)
	if memberAgentHealthyCond.Status != metav1.ConditionTrue && sinceLastTransition > clusterHealthCheckTimeout {
		// The cluster health check fails.
		//
		// Note that sporadic (isolated) health check failures will not preclude a cluster.
		//
		// Also note that at this moment, the member cluster will report a cluster as unhealth if
		// and only if it fails to list all the nodes in the cluster; this could simply be the
		// result of a temporary network issue, and the report will be disregarded by this plugin
		// if the health check passes within a reasonable amount of time.
		//
		// Note that this plugin assumes minimum clock drifts between clusters in the fleet.
		return false, fmt.Sprintf("cluster is not connected to the fleet: unhealthy for a prolonged period of time (last transitioned %.2f minutes ago)", sinceLastTransition.Minutes())
	}

	return true, ""
}
