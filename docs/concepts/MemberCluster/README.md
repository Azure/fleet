# MemberCluster

## Overview

The fleet constitutes an implementation of a [`ClusterSet`](https://multicluster.sigs.k8s.io/api-types/cluster-set/) and 
encompasses the following attributes:
- A collective of clusters managed by a centralized authority.
- Typically characterized by a high level of mutual trust within the cluster set.
- Embraces the principle of Namespace Sameness across clusters:
  - Ensures uniform permissions and characteristics for a given namespace across all clusters.
  - While not mandatory for every cluster, namespaces exhibit consistent behavior across those where they are present.

The `MemberCluster` represents a cluster-scoped API established within the hub cluster, serving as a representation of 
a cluster within the fleet. This API offers a dependable, uniform, and automated approach for multi-cluster applications
(frameworks, toolsets) to identify registered clusters within a fleet. Additionally, it facilitates applications in querying
a list of clusters managed by the fleet or observing cluster statuses for subsequent actions.

Some illustrative use cases encompass:

- The Fleet Scheduler utilizing managed cluster statuses or specific cluster properties (e.g., labels, taints) of a `MemberCluster`
for resource scheduling.
- Automation tools like GitOps systems (e.g., ArgoCD or Flux) automatically registering/deregistering clusters in compliance
with the `MemberCluster` API.
- The [MCS API](https://multicluster.sigs.k8s.io/concepts/multicluster-services-api/) automatically generating `ServiceImport` CRs 
based on the `MemberCluster` CR defined within a fleet.

Moreover, it furnishes a user-friendly interface for human operators to monitor the managed clusters.

## MemberCluster Lifecycle

### Joining the Fleet

The process to join the Fleet involves creating a `MemberCluster`. The `MemberCluster` controller, a constituent of the 
hub-cluster-agent described in the [Component](../Components/README.md), watches the `MemberCluster` CR and generates 
a corresponding namespace for the member cluster within the hub cluster. It configures roles and role bindings within the
hub cluster, authorizing the specified member cluster identity (as detailed in the `MemberCluster` spec) access solely 
to resources within that namespace. To collate member cluster status, the controller generates another internal CR named
`InternalMemberCluster` within the newly formed namespace. Simultaneously, the `InternalMemberCluster` controller, a component
of the member-cluster-agent situated in the member cluster, gathers statistics on cluster usage, such as capacity utilization, 
and reports its status based on the `HeartbeatPeriodSeconds` specified in the CR. Meanwhile, the `MemberCluster` controller 
consolidates agent statuses and marks the cluster as `Joined`.

### Leaving the Fleet

Fleet administrators can deregister a cluster by deleting the `MemberCluster` CR. The leave process involves several steps 
coordinated between the hub and member clusters:

1. **Initiating the Leave Process**:
   - Upon deletion of the `MemberCluster` CR, the `MemberCluster` controller in the hub cluster detects this event.
   - It sets the corresponding `InternalMemberCluster` CR's state to `Leave` in the reserved namespace for that member cluster.

2. **Member Cluster Cleanup**:
   - The `InternalMemberCluster` controller in the member cluster detects the state change to `Leave`.
   - It initiates a cleanup process that removes all resources that were propagated to the member cluster via Fleet.
   - The `ApplyWorkReconciler` in the member agent removes finalizers from work objects, enabling them to be garbage collected.
   - Applied resources are removed from the member cluster through the deletion of `AppliedWork` objects.

3. **Completion of Leave Process**:
   - Once the cleanup is complete, the member agent marks the `InternalMemberCluster` CR as "left".
   - If there are any errors during the leave process, the `InternalMemberCluster` will be marked with a "leave failed" status.
   - When the leave process is successful, metrics are reported to indicate the member cluster has left.

4. **Hub Cluster Cleanup**:
   - After the member cluster completes the leave process, the `MemberCluster` controller deletes roles, role bindings, 
     and other resources created for that member cluster.
   - Finally, it removes the reserved namespace for the member cluster from the hub cluster.

After a member cluster leaves the fleet, resource placements targeting that cluster are no longer applied. Any resources 
that were previously placed on the cluster are removed as part of the cleanup process. The member agent can then be 
uninstalled from the cluster as described in the [how-to guide](../../howtos/clusters.md#setting-a-cluster-to-leave-a-fleet).

## Taints

Taints are a mechanism to prevent the Fleet Scheduler from scheduling resources to a `MemberCluster`. We adopt the concept of 
[taints and tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) introduced in Kubernetes to 
the multi-cluster use case.

The `MemberCluster` CR supports the specification of list of taints, which are applied to the `MemberCluster`. Each Taint object comprises
the following fields:
- `key`: The key of the taint.
- `value`: The value of the taint.
- `effect`: The effect of the taint, which can be `NoSchedule` for now.

Once a `MemberCluster` is tainted with a specific taint, it lets the Fleet Scheduler know that the `MemberCluster` should not receive resources 
as part of the workload propagation from the hub cluster.

The `NoSchedule` taint is a signal to the Fleet Scheduler to avoid scheduling resources from a `ClusterResourcePlacement` to the `MemberCluster`.
Any `MemberCluster` already selected for resource propagation will continue to receive resources even if a new taint is added.

Taints are only honored by `ClusterResourcePlacement` with **PickAll**, **PickN** placement policies. In the case of **PickFixed** placement policy
the taints are ignored because the user has explicitly specify the `MemberClusters` where the resources should be placed.

For detailed instructions, please refer to this [document](../../howtos/taint-toleration.md).

## What's next
* Get hands-on experience [how to add a member cluster to a fleet](../../howtos/clusters.md).
* Explore the [`ClusterResourcePlacement` concept to placement cluster scope resources among managed clusters](../ClusterResourcePlacement/README.md).


