# kubectl-fleet

A kubectl plugin for KubeFleet cluster management operations, providing functionalities include draining workloads from member clusters for maintenance,
uncordoning them when ready to accept workloads again, as well as approving staged update run stage execution.

## Installation

### Building as a kubectl plugin

1. Build the plugin binary by running the following command in the root directory of the KubeFleet repo:

```bash
go build -o ./hack/tools/bin/kubectl-fleet ./tools/fleet/
```

2. Copy the binary to a directory in your `PATH` so that it can be run as a kubectl plugin. For example, you can move it to `/usr/local/bin`:

```bash
sudo cp ./hack/tools/bin/kubectl-fleet /usr/local/bin/
```

3. Make the binary executable by running the following command:

```bash
chmod +x /usr/local/bin/kubectl-fleet
```

4. Verify that the plugin is recognized by kubectl by running the following command:

```bash
kubectl plugin list
```

You should see the `fleet` plugin listed in the output:

```
The following compatible plugins are available:

/usr/local/bin/kubectl-fleet
```

Please refer to the [kubectl plugin documentation](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) for more information.

## Usage

### Getting Hub Cluster Context

Before using the plugin, you need to identify the context of your hub cluster. You can run the following command to see available contexts:

```bash
kubectl config get-contexts
```

The output will look like this:

```
CURRENT   NAME               CLUSTER            AUTHINFO                                            NAMESPACE         
*         hub                hub                clusterUser_clusterResourceGroup_hub   
```

Here you can see that the context of the hub cluster is called `hub` under the `NAME` column.

### Approve a ClusterApprovalRequest

Use the `approve` subcommand to approve ClusterApprovalRequest resources for staged update runs. This allows staged updates to proceed to the next stage by patching an "Approved" condition to the resource status.

```bash
kubectl fleet approve clusterapprovalrequest --hubClusterContext <hub-cluster-context> --name <approval-request-name>
```

Example:
```bash
kubectl fleet approve clusterapprovalrequest --hubClusterContext hub --name my-approval-request
```

### Drain a Member Cluster

Use the `draincluster` subcommand to remove all resources propagated to a member cluster from the hub cluster by any `Placement` resource. This is useful when you want to temporarily move all workloads off a member cluster in preparation for an event like upgrade or reconfiguration.

```bash
kubectl fleet draincluster --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

Example:
```bash
kubectl fleet draincluster --hubClusterContext hub --clusterName member-cluster-1
```

### Uncordon a Member Cluster

Use the `uncordoncluster` subcommand to uncordon a member cluster that has been previously drained, allowing resources to be propagated to the cluster again.

```bash
kubectl fleet uncordoncluster --hubClusterContext <hub-cluster-context> --clusterName <memberClusterName>
```

Example:
```bash
kubectl fleet uncordoncluster --hubClusterContext hub --clusterName member-cluster-1
```

## Subcommands

### approve

Approves ClusterApprovalRequest resources for staged update runs by:

1. **Status Update**: Patches the ClusterApprovalRequest resource with an "Approved" condition
2. **Stage Progression**: Allows staged updates to proceed to the next stage automatically

The approve command currently supports the following resource kinds:
- `clusterapprovalrequest`: Approve a ClusterApprovalRequest for staged updates

### draincluster

Drains a member cluster by performing the following actions:

1. **Cordoning**: Adds a `Taint` to the `MemberCluster` resource to prevent any new resources from being propagated to the member cluster
2. **Eviction**: Creates `Eviction` objects for all the `Placement` objects that have propagated resources to the member cluster and waits all evictions to complete

**Note**: The `draincluster` command is a best-effort mechanism. Once the command runs successfully, you must verify that all resources propagated by `Placement` resources are removed from the member cluster. Re-running the command is safe and recommended if you notice any resources still present on the member cluster.

### uncordoncluster

Uncordons a previously drained member cluster by:

1. **Taint Removal**: Removes the `cordon` taint that was added to the `MemberCluster` resource by the `draincluster` command
2. **Resource Propagation**: Allows resources to be propagated to the cluster again according to existing `Placement` objects

If the `cordon` taint is not present on the member cluster, the command will have no effect and complete successfully.

## Flags

The `approve` subcommand uses the following flags:
- `--hubClusterContext`: kubectl context for the hub cluster (required)
- `--name`: name of the resource to approve (required)

Both `draincluster` and `uncordoncluster` subcommands use the following flags:
- `--hubClusterContext`: kubectl context for the hub cluster (required)
- `--clusterName`: name of the member cluster to operate on (required)

## Examples

### Complete Maintenance Workflow

```bash
# 1. Identify available contexts
kubectl config get-contexts

# 2. Drain a cluster for maintenance
kubectl fleet draincluster --hubClusterContext production-hub --clusterName worker-node-1

# 3. Perform maintenance on the worker-node-1 cluster
# ... maintenance operations ...

# 4. After maintenance, uncordon the cluster to allow workloads back
kubectl fleet uncordoncluster --hubClusterContext production-hub --clusterName worker-node-1
```

### Additional Examples

```bash
# Approve a ClusterApprovalRequest for staged updates
kubectl fleet approve clusterapprovalrequest --hubClusterContext hub --name update-approval-stage-1

# Drain multiple clusters (run separately for each cluster)
kubectl fleet draincluster --hubClusterContext hub --clusterName east-cluster
kubectl fleet draincluster --hubClusterContext hub --clusterName west-cluster

# Uncordon clusters after maintenance
kubectl fleet uncordoncluster --hubClusterContext hub --clusterName east-cluster
kubectl fleet uncordoncluster --hubClusterContext hub --clusterName west-cluster
```

## Troubleshooting

### Verifying Approval Operation

After running the approval command, verify that the corresponding clusterApprovalRequest has been approved:

1. Check that the clusterApprovalRequest has `APPROVED` set to true
   ```
   kubectl get clusterapprovalrequest example-run-staging
   NAME                  UPDATE-RUN    STAGE     APPROVED   AGE
   example-run-staging   example-run   staging   True       2m46s
   ```
2. Verify the updateRun is not blocked by the approval after-stage task

### Verifying Drain Operation

After running the draincluster command, verify that resources have been removed from the member cluster:

1. Check that the cordon taint has been applied to the MemberCluster resource
2. Verify that eviction objects have been created for relevant placements
3. Confirm that workloads have been moved off the target cluster

If resources remain on the cluster after draining, it's safe to re-run the draincluster command.

### Verifying Uncordon Operation

After running the uncordoncluster command:

1. Check that the cordon taint has been removed from the MemberCluster resource
2. Monitor that new workloads can be scheduled to the cluster according to placement policies
