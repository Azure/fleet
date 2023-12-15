# Frequently Asked Questions

## ## What are fleet-owned resources on the hub and member clusters? Can these fleet-owned resources be modified by the user?

- **Namespaces:**
    - `fleet-system` (Hub Cluster)
    - `fleet-member-{clusterName}` (Hub Cluster)
- **Custom Resource Definitions**
- **Custom Resources:**
    - `MemberCluster` (Hub Cluster, user-facing resource)
    - `InternalMemberCluster` (Hub Cluster, internal resource)
    - `Work` (Hub Cluster, internal resource)
    - `AppliedWork` (Member Cluster, internal resource)
    - `ClusterResourcePlacement` (Hub Cluster, user-facing resource)
    - `ClusterResourceSnapshot` (Hub Cluster, internal resource)
    - `ClusterSchedulingPolicySnapshot` (Hub Cluster, internal resource)
    - `ClusterResourceBinding` (Hub Cluster, internal resource)
    - `EndpointSliceExport` (Hub Cluster, internal resource)
    - `EndpointSliceImport` (Hub Cluster, internal resource)
    - `InternalServiceExport` (Hub Cluster, internal resource)
    - `InternalServiceImport` (Hub Cluster, internal resource)
    - `MultiClusterService` (Member Cluster, user-facing resource)
    - `ServiceExport` (Member Cluster, user-facing resource)
    - `ServiceImport` (Hub Cluster, internal resource)

Most of these resources are protected by a set of validating webhooks, preventing users from modifying them. For more information about fleet networking resource please refer to this link [here](https://github.com/Azure/fleet-networking).

## ## What kind of the resources are allowed to be propagated from the hub cluster to the member clusters? How can I control the list?

`ClusterResourcePlacement` excludes certain groups/resources when propagating the resources by default. They are defined [here](https://github.com/Azure/fleet/blob/main/pkg/utils/apiresources.go).
- `k8s.io/api/events/v1` (group)
- `k8s.io/api/coordination/v1` (group)
- `k8s.io/metrics/pkg/apis/metrics/v1beta1` (group)
- `k8s.io/api/core/v1` (pod, node)
- `networking.fleet.azure.com` (service import resource)
- any resources in the "default" namespace

You can use `skipped-propagating-apis` and `skipped-propagating-namespaces` flag when installing the hub-agent to skip resources from being propagated by specifying their group/group-version/group-version-kind and namespaces.

## What happens to existing resources in member clusters when their definitions conflict with the desired resources in the hub cluster?

If there is a conflict because a particular resource already exists on the member cluster, the apply fails when attempting to propagate such a resource from the hub cluster.

## What happens if modifies resources that were placed from hub to member clusters?

Possible scenarios:

- If the user updates the resource on the hub cluster, the update is propagated to all member clusters where the resource exists.
- If the user deletes the resource on the hub cluster, the resource is deleted on all clusters to which it was propagated.
- If the user modifies the resource on the member cluster, no automatic action occurs as it's a user-made modification.
