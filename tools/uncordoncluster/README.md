# Steps to build uncordoncluster as a kubectl plugin

1. Build the binary for the `uncordoncluster` tool by running the following command in the root directory of the fleet repo:

```bash
go build -o ./hack/tools/bin/kubectl-uncordoncluster ./tools/uncordoncluster/main.go
```

2. Copy the binary to a directory in your `PATH` so that it can be run as a kubectl plugin. For example, you can move it to
   `/usr/local/bin`:

```bash
sudo cp ./hack/tools/bin/kubectl-uncordoncluster /usr/local/bin/
```

3. Make the binary executable by running the following command:

```bash
chmod +x /usr/local/bin/kubectl-uncordoncluster
```

4. Verify that the plugin is recognized by kubectl by running the following command:

```bash
kubectl plugin list
```

you should see the `uncordoncluster` plugin listed in the output:

```
The following compatible plugins are available:

/usr/local/bin/kubectl-uncordoncluster
```

please refer to the [kubectl plugin documentation](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) for
more information.


# Uncordon Member Cluster connected to a fleet

After following the steps above to build the `uncordoncluster` tool as a kubectl plugin, you can use it to uncordon a 
member cluster that has been cordoned using the `draincluster` tool. 

```
kubectl uncordoncluster --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
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

Here you can see that the context of the hub cluster is called `hub` under the `NAME` column.

The command removes the `cordon` taint added to a `MemberCluster` resource by the `draincluster` tool. If the `cordon` 
taint is not present, the command will not have any effect.
