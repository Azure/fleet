# Frequently Asked Questions

## What are fleet-owned resources on the hub and member clusters? Can these fleet-owned resources be modified by the user?

Majority of the `internal` resources and fleet reserved namespaces described below are safeguarded by a series of validating webhooks, serving as a preventive measure to restrict users from making modifications to them.

The fleet reserved namespace are `fleet-system` and `fleet-member-{clusterName}` where clusterName is the name of each member cluster that has joined the fleet.

### Fleet hub cluster internal resources:
| Resource                           |
|------------------------------------|
| `InternalMemberCluster`            |
| `Work`                             |
| `ClusterResourceSnapshot`          |
| `ClusterSchedulingPolicySnapshot`  |
| `ClusterResourceBinding`           |

### Fleet member cluster internal resources:
| Resource                |
|-------------------------|
| `InternalMemberCluster` |
| `AppliedWork`           |

**Fleet APIs** are defined [here](https://github.com/Azure/fleet/tree/main/apis), **Fleet CRDs** are defined [here](https://github.com/Azure/fleet/tree/main/config/crd/bases).

### Fleet Networking hub cluster internal resources:
| Resource                |
|-------------------------|
| `EndpointSliceExport`   |
| `EndpointSliceImport`   |
| `InternalServiceExport` |
| `InternalServiceImport` |
| `ServiceImport`         |

**Fleet Networking APIs** are defined [here](https://github.com/Azure/fleet-networking/tree/main/api/v1alpha1), **Fleet Networking CRDs** are defined [here](https://github.com/Azure/fleet-networking/tree/main/config/crd/bases).

## What kind of the resources are allowed to be propagated from the hub cluster to the member clusters? How can I control the list?

The resources to be propagated from the hub cluster to the member clusters can be controlled by either an exclude/skip list or an include/allow list which are mutually exclusive.

`ClusterResourcePlacement` excludes certain groups/resources when propagating the resources by default. They are defined [here](https://github.com/Azure/fleet/blob/main/pkg/utils/apiresources.go).
- `k8s.io/api/events/v1` (group)
- `k8s.io/api/coordination/v1` (group)
- `k8s.io/metrics/pkg/apis/metrics/v1beta1` (group)
- `k8s.io/api/core/v1` (pod, node)
- `networking.fleet.azure.com` (service import resource)
- any resources in the "default" namespace

You can use `skipped-propagating-apis` and `skipped-propagating-namespaces` flag when installing the hub-agent to skip resources from being propagated by specifying their group/group-version/group-version-kind and namespaces.

You can use `allowed-propagating-apis` flag on the hub-agent to only allow propagation of desired set of resources specified in the form of group/group-version/group-version-kind. This flag is mutually exclusive with `skipped-propagating-apis`.

## What happens to existing resources in member clusters when their definitions conflict with the desired resources in the hub cluster?

In case of a conflict, where a resource already exists on the member cluster, the apply operation fails when trying to propagate the same resource from the hub cluster.

## What happens if modifies resources that were placed from hub to member clusters?

Possible scenarios:

- If the user `updates` the resource on the hub cluster, the update is propagated to all member clusters where the resource exists.
- If the user `deletes` the resource on the hub cluster, the resource is deleted on all clusters to which it was propagated.
- If the user `updates` the resource on the member cluster, no automatic action occurs as it's a user-made modification.
- If the user `deletes` the resource on the member cluster, the resource is automatically created again on the member cluster after reconciliation.
