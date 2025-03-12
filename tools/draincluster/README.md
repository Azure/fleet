# Drain Member Cluster connected to a fleet

To drain a member cluster connected to a fleet, you can use the `drain` tool. This tool allows you remove all
resource propagated to the member cluster from the hub cluster by any `ClusterResourcePlacement` resource.
This is useful when you want to temporarily move all workloads off a member cluster in preparation for an 
event like upgrade or reconfiguration.

The `drain` tool can be used to drain a member cluster by running the following command:

```
go run tools/draincluster/main.go --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

the tool currently is a go program that takes the hub cluster context and the member cluster name as arguments.

- The `--hubClusterContext` flag specifies the context of the hub cluster
- The `--clusterName` flag specifies the name of the member cluster to cordon.

the user can run the following command to identify the context of the hub cluster:

```
kubectl config get-contexts
```

the output of the command will look like this:

```
CURRENT   NAME               CLUSTER            AUTHINFO                                            NAMESPACE         
*         hub                hub                clusterUser_clusterResourceGroup_hub   
```

The command adds a `Taint` the `MemberCluster` resource of the member cluster to prevent any new resources from being 
propagated to the member cluster. Then it creates `ClusterResourcePlacementEviction` objects for all the 
`ClusterResourcePlacement` objects that have propagated resources to the member cluster that needs to be cordoned.

>> **Note**: The `drain` tool is a best effort mechanism at the moment, and it may not remove all the resources that 
> have been propagated to the member cluster. To ensure that all resources are removed correctly the users can ensure
> that no new changes are being propagated to the member cluster when running the `drain` tool by any 
> `ClusterResourcePlacement` resource because at the time of draining the member cluster, a `ClusterResourcePlacement` 
> could be propagating resources to the member cluster which may not be removed by the `drain` tool.
