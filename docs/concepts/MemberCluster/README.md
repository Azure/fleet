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

Fleet administrators can deregister a cluster by deleting the `MemberCluster` CR. Upon detection of deletion events by 
the `MemberCluster` controller within the hub cluster, it removes the corresponding `InternalMemberCluster` CR in the 
reserved namespace of the member cluster. It awaits completion of the "leave" process by the `InternalMemberCluster` 
controller of member agents, and then deletes role and role bindings and other resources including the member cluster reserved
namespaces on the hub cluster.


## What's next
* Get hands-on experience [how to add a member cluster to a fleet](../../howtos/clusters.md).
* Explore the [`ClusterResourcePlacement` concept to placement cluster scope resources among managed clusters](../ClusterResourcePlacement/README.md).


