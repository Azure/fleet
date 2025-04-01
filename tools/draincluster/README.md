# Drain Member Cluster connected to a fleet

To drain a member cluster connected to a fleet, you can use the `draincluster` tool. This tool allows you to remove all
resources propagated to the member cluster from the hub cluster by any `Placement` resource.
This is useful when you want to temporarily move all workloads off a member cluster in preparation for an 
event like upgrade or reconfiguration.

The `draincluster` tool can be used to drain a member cluster by running the following command:

```
go run tools/draincluster/main.go --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

the tool currently is a go program that takes the hub cluster context and the member cluster name as arguments.

- The `--hubClusterContext` flag specifies the context of the hub cluster
- The `--clusterName` flag specifies the name of the member cluster to drain.

the user can run the following command to identify the context of the hub cluster:

```
kubectl config get-contexts
```

the output of the command will look like this:

```
CURRENT   NAME               CLUSTER            AUTHINFO                                            NAMESPACE         
*         hub                hub                clusterUser_clusterResourceGroup_hub   
```

Here you can see that the context of the hub cluster is called `hub` under the `NAME` column.

The command adds a `Taint` to the `MemberCluster` resource of the member cluster to prevent any new resources from being 
propagated to the member cluster. Then it creates `ClusterResourcePlacementEviction` objects for all the 
`ClusterResourcePlacement` objects that have propagated resources to the member cluster.

>> **Note**: The `draincluster` tool is a best-effort mechanism at the moment, so once the command is run successfully
> the user must verify if all resources propagated by `Placement` resources are removed from the member cluster.
> Re-running the command is safe and is recommended if the user notices any resources still present on the member cluster.
