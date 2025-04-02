# Steps to build draincluster as a kubectl plugin

1. Build the binary for the `draincluster` tool by running the following command in the root directory of the fleet repo:

```bash
go build -o ./hack/tools/bin/kubectl-draincluster ./tools/draincluster/main.go
```

2. Move the binary to a directory in your `PATH` so that it can be run as a kubectl plugin. For example, you can move it to
`/usr/local/bin`:

```bash
sudo cp ./hack/tools/bin/kubectl-draincluster /usr/local/bin/
```

3. Make the binary executable by running the following command:

```bash
chmod +x /usr/local/bin/kubectl-draincluster
```

4. Verify that the plugin is recognized by kubectl by running the following command:

```bash
kubectl plugin list
```

you should see the `draincluster` plugin listed in the output:

```
The following compatible plugins are available:

/usr/local/bin/kubectl-draincluster
```

please refer to the [kubectl plugin documentation](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) for 
more information.

# Drain Member Cluster connected to a fleet

After following the steps above to build the `draincluster` tool as a kubectl plugin, you can use it to remove all 
resources propagated to the member cluster from the hub cluster by any `Placement` resource. This is useful when you 
want to temporarily move all workloads off a member cluster in preparation for an event like upgrade or reconfiguration.

The `draincluster` tool can be used to drain a member cluster by running the following command:

```
kubectl draincluster --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
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
