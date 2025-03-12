# Uncordon Member Cluster connected to a fleet

To uncordon a member cluster connected to a fleet, you can use the `uncordon-cluster` tool. This tool allows you to 
uncordon a member cluster that has been cordoned using the `cordon-cluster` tool. 

```
go run tools/uncordoncluster/main.go --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

the tool currently is a go program that also takes the hub cluster context and the member cluster name as arguments.

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

The command removes all taints added to a `MemberCluster` resource and hence if any `ClusterResourcePlacementEviction` 
object present which can propagate resources to the member cluster, it can continue to do so.
