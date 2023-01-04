# Fleet Architecture

## Overview

At a Kubernetes level, a fleet consists of a hub cluster and zero or more member clusters.  Custom resources and associated controllers,
residing both on the hub and member(s), as well as validating webhooks on the hub, govern the behavior of features of the fleet.  This
document gives an architectural overview of those resources, controllers and validating webhooks.

Fleets are configured such that each member *m* is granted access to resources within its corresponding *fleet-member-m* namespace on the
hub.  Controllers on both the hub and the member are able to read, write and watch these resources to communicate between each-other.  The
hub is not granted access to resources on any member, nor does any member have access to any other member.

The following fleet use cases are implemented:

1. **Join/Leave**: member clusters can join and leave a fleet.
1. **Cluster resource placement (CRP)**: arbitrary Kubernetes resources can be replicated from a hub to one or more members, building upon
   the Kubernetes [Work API](https://github.com/Azure/k8s-work-api).
1. **Networking**: a single external L4 load balancer can front a service exported by one or more members, building on the Kubernetes
   [Multi-Cluster Services API](https://github.com/kubernetes-sigs/mcs-api)
   ([KEP-1645](https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api)).  See
   [Fleet Networking Architecture](https://github.com/Azure/fleet-networking/blob/master/docs/architecture.md) for more details.

## Resources

### Join/Leave

The following resources are defined for the Join/Leave use case:

1. [MemberCluster.fleet.azure.com](https://github.com/Azure/fleet/blob/master/apis/v1alpha1/membercluster_types.go#:~:text=type%20MemberCluster%20struct%20%7B)
   (hub cluster, not namespaced)

   Desired state of a member cluster (Join/Leave), cluster principal identity and heartbeat.  Administrative status and resource usage.
   Typically owned by the Fleet RP.

1. [InternalMemberCluster.fleet.azure.com](https://github.com/Azure/fleet/blob/master/apis/v1alpha1/internalmembercluster_types.go#:~:text=type%20InternalMemberCluster%20struct%20%7B)
   (hub cluster, fleet-member-\* namespaces)

   Internal mirror of MemberCluster resource, hosted in fleet-member-\* namespaces for communication with member clusters.  Owned by the
   (hub) membercluster controller.

### Cluster resource placement (CRP)

The following resources are defined for the Cluster resource placement (CRP) use case:

1. [ClusterResourcePlacement.fleet.azure.com](https://github.com/Azure/fleet/blob/main/apis/v1alpha1/clusterresourceplacement_types.go#:~:text=type%20ClusterResourcePlacement%20struct%20%7B)
   (hub cluster, end user namespaces)

   Lists names of cluster (non-namespaced) resources (e.g. *ClusterRoles*, *Namespaces*, etc.) to replicate from hub to member(s), as well
   as the selection of member(s) to which to replicate.  Replicating a namespace implies replicating all the contained resources as well.
   Owned by the end user.

1. [Work.multicluster.x-k8s.io](https://github.com/Azure/k8s-work-api/blob/master/pkg/apis/v1alpha1/work_types.go#:~:text=type%20Work%20struct%20%7B)
   (hub cluster, fleet-member-\* namespaces)

   Lists resource specifications that the member cluster should apply.  Owned by the (hub) clusterresourceplacement controller.

1. [AppliedWork.multicluster.x-k8s.io](https://github.com/Azure/k8s-work-api/blob/master/pkg/apis/v1alpha1/appliedwork_type.go#:~:text=type%20AppliedWork%20struct%20%7B)
   (member cluster, not namespaced)

   Lists metadata of resources applied locally on member cluster.  Owned by the (member) work controller.

## Controllers

### Join/Leave

The following controllers are implemented for the Join/Leave use case:

1. [pkg/controllers/membercluster](https://github.com/Azure/fleet/tree/main/pkg/controllers/membercluster) (hub cluster)

   * Watches *MemberClusters*, ensures fleet-member-\* *Namespace*, *Role*, *RoleBinding* and *InternalMemberCluster* resources.
   * Copies *InternalMemberCluster* status to *MemberCluster* status.

1. [pkg/controllers/internalmembercluster](https://github.com/Azure/fleet/tree/main/pkg/controllers/internalmembercluster) (member cluster)

   * Watches *InternalMemberCluster* on hub, ensures work controller running, updates heartbeat, sets resource and health status.

### Cluster resource placement (CRP)

Cluster resource placement (CRP) uses a shared [pkg/resourcewatcher](https://github.com/Azure/fleet/tree/main/pkg/resourcewatcher) component
to watch the hub for changes on *all* resources and enqueue changes of interest to different controllers as follows:

* Create/update/delete of *ClusterResourcePlacements* -> clusterresourceplacement controller.
* Update/delete of *Works* -> clusterresourceplacement controller.
* Update of *MemberClusters* -> memberclusterplacement controller.
* Create/update/delete of any[*] watchable resource -> resourcechange controller.

[*] resourcewatcher excludes some resources from being enqueued to the resourcechange controller, e.g.:
* Any resource under namespaces default, fleet-\*, kube-\*.
* Any resource of kind *.fleet.azure.com, *.coordination.k8s.io, *.events.k8s.io, *.metrics.k8s.io, Pod, Node, Work.multicluster.x-k8s.io,
  ServiceImport.networking.fleet.azure.com.
* ConfigMaps named kube-root-ca.crt.
* ServiceAccounts named default.
* Secrets of type kubernetes.io/service-account-token.
* Endpoints and EndpointSlices that are automatically generated.

The following controllers are implemented for the Cluster resource placement (CRP) use case:

1. [pkg/controllers/clusterresourceplacement](https://github.com/Azure/fleet/tree/main/pkg/controllers/clusterresourceplacement) (hub cluster)

   * Dequeues *ClusterResourcePlacements*, selects clusters, exact resources, writes Works, updates status.

1. [pkg/controllers/memberclusterplacement](https://github.com/Azure/fleet/tree/main/pkg/controllers/memberclusterplacement) (hub cluster)

   * Dequeues *MemberClusters*, enqueue all related *ClusterResourcePlacements* to the clusterresourceplacement controller.

1. [pkg/controllers/resourcechange](https://github.com/Azure/fleet/tree/main/pkg/controllers/resourcechange) (hub cluster)

   * Dequeues any type of watchable resource, enqueue all related *ClusterResourcePlacements* to the clusterresourceplacement controller.

1. [pkg/controllers/work](https://github.com/Azure/fleet/tree/main/pkg/controllers/work) (member cluster)

   * Watches *Works* on hub, apply contents to associated resources locally, updates status and *AppliedWorks* locally.

## Webhooks

### Join/Leave

The following webhooks are implemented for the Join/Leave use case:

1. [pkg/webhook/clusterresourceplacement](https://github.com/Azure/fleet/tree/main/pkg/webhook/clusterresourceplacement) (hub cluster)

   * Provides validation upon modification of *ClusterResourcePlacements*.

1. [pkg/webhook/pod](https://github.com/Azure/fleet/tree/main/pkg/webhook/pod) (hub cluster)

   * Prevents creation of Pods in namespaces except kube-\* and fleet-\*.

1. [pkg/webhook/replicaset](https://github.com/Azure/fleet/tree/main/pkg/webhook/replicaset) (hub cluster)

   * Prevents creation of ReplicaSets in namespaces except kube-\* and fleet-\*.
