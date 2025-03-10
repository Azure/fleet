# Uncordon Member Cluster connected to a fleet

To uncordon a member cluster connected to a fleet, you can use the `uncordon-cluster` tool. This tool allows you to 
uncordon a member cluster that has been cordoned using the `cordon-cluster` tool. 

```
go run tools/cordon-cluster/main.go --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

the tool currently is a go program that also takes the hub cluster context and the member cluster name as arguments.

The command removes all taints add to a `MemberCluster` resource of the member cluster and hence if any 
`ClusterResourcePlacementEviction` object present which can propagate resources to the member cluster, it can continue 
to do so.
