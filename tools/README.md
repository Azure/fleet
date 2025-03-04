# Cordon and Uncordon Member Clusters connected to a fleet

## Cordon Member Cluster connected to a fleet

To cordon a member cluster connected to a fleet, you can use the `cordon-cluster` tool. This tool allows you remove all
resource propagated to the member cluster from the hub cluster by any `ClusterResourcePlacement` resource.
This is useful when you want to temporarily stop all workloads on a member cluster.

The `cordon-cluster` tool can be used to cordon a member cluster by running the following command:

```
go run tools/cordon-cluster/main/main.go --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
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

The command internally creates `ClusterResourcePlacementEviction` objects for all the `ClusterResourcePlacement` 
objects that have propagated resources to the member cluster that needs to be cordoned. And it also adds a `Taint` the 
`MemberCluster` resource of the member cluster to prevent any new resources from being propagated to the member cluster.

## Uncordon Member Cluster connected to a fleet

To uncordon a member cluster connected to a fleet, you can use the `uncordon-cluster` tool. This tool allows you to uncordon
a member cluster that has been cordoned using the `cordon-cluster` tool. 

```
go run tools/uncordon-cluster/main/main.go --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

the tool currently is a go program that also takes the hub cluster context and the member cluster name as arguments.

The command internally removes all taints add to a `MemberCluster` resource of the member cluster and hence if any 
`ClusterResourcePlacementEviction` object present which can propagate resources to the member cluster, it can continue to do so.