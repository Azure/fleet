# Frequently Asked Questions

## What are fleet-owned resources on the hub and member clusters? Can these fleet-owned resources be modified by the user?

> Reserved namespaces used by fleet: `fleet-system`, `fleet-member-{clusterName}` where clusterName is the name of each member cluster that has joined the fleet.

### Fleet hub cluster resources:
| Resource                          | Resource Type        |
|-----------------------------------|----------------------|
| `MemberCluster`                   | user-facing resource |
| `InternalMemberCluster`           | internal resource    |
| `Work`                            | interna resource     |
| `ClusterResourcePlacement`        | user-facing resource |
| `ClusterResourceSnapshot`         | internal resource    |
| `ClusterSchedulingPolicySnapshot` | internal resource    |
| `ClusterResourceBinding`          | internal resource    |

### Fleet member cluster resource:
| Resource                | Resource Type     |
|-------------------------|-------------------|
| `InternalMemberCluster` | internal resource |
| `AppliedWork`           | internal resource |

**Fleet APIs** are defined [here](https://github.com/Azure/fleet/tree/main/apis), **Fleet CRDs** are defined [here](https://github.com/Azure/fleet/tree/main/config/crd/bases).

### Fleet Networking hub cluster resources:
| Resource                | Resource Type     |
|-------------------------|-------------------|
| `EndpointSliceExport`   | internal resource |
| `EndpointSliceImport`   | internal resource |
| `InternalServiceExport` | internal resource |
| `InternalServiceImport` | internal resource |
| `ServiceImport`         | internal resource |

### Fleet Networking member cluster resources:
| Resource              | Resource Type        |
|-----------------------|----------------------|
| `MultiClusterService` | user-facing resource |
| `ServiceExport`       | user-facing resource |

**Fleet Networking APIs** are defined [here](https://github.com/Azure/fleet-networking/tree/main/api/v1alpha1), **Fleet Networking CRDs** are defined [here](https://github.com/Azure/fleet-networking/tree/main/config/crd/bases).

`CRUD` operations are permitted for all the  `user-facing` resources, while `CRUD` operations are restricted for `internal` resources.  A majority of these `internal` resources are safeguarded by a series of validating webhooks, serving as a preventive measure to restrict users from making modifications to them

## What kind of the resources are allowed to be propagated from the hub cluster to the member clusters? How can I control the list?

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

- If the user `updates` the resource on the hub cluster, the update is propagated to all member clusters where the resource exists.
- If the user `deletes` the resource on the hub cluster, the resource is deleted on all clusters to which it was propagated.
- If the user `updates` the resource on the member cluster, no automatic action occurs as it's a user-made modification.
- If the user `deletes` the resource on the member cluster, the resource is automatically created again on the member cluster after reconciliation.
