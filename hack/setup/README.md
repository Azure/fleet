# Setting up a Fleet

This how-to guide discusses how to create a fleet, specifically:

* how to create a hub cluster; and
* how to join clusters to hub cluster; and
* how to add labels to a member cluster

> Note
>
> To run these scripts, make sure that you have already installed the following tools in your
> system:
> * `kubectl`, the Kubernetes CLI
> * `helm`, a Kubernetes package manager
> * `curl`
> * `jq`
> * `base64`

## Create a hub cluster from a cluster 
For your convenience, Fleet provides a script that can automate the process of creating a hub cluster. To use script,
run the commands bellow:
```sh
# Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig context you use for
# accessing your hub cluster.
export HUB_CLUSTER_CONTEXT=<YOUR-HUB-CLUSTER-CONTEXT>
# Replace the value of HUB_CLUSTER_ADDRESS with the address of your hub cluster API server.
export HUB_CLUSTER_ADDRESS=<YOUR-HUB-CLUSTER-ADDRESS>

# Clone the Fleet GitHub repository, if necessary.
git clone https://github.com/Azure/fleet.git
# Go into fleet directory.
cd fleet

# Run the script.
chmod +x hack/setup/createHubCluster.sh
./hack/setup/createHubCluster.sh
```

It may take a few minutes for the script to finish running. Once it is completed, verify that the `hub-agent` has been installed:
```
kubectl get pods -n fleet-system
```

If you would like to add a prometheus server to access metrics, run the following: 
<details>
<summary> Add Prometheus Server</summary>

1.  Check the status of the service. Copy the `EXTERNAL-IP` of the `fleet-prometheus-endpoint` from the services for later.
    ````
    kubectl get service -n fleet-system

4. Install the Prometheus community Helm Chart
   ```
    helm install prom prometheus-community/kube-prometheus-stack -f prom1.yaml
   ```
   The `prom1.yaml` file should contain the following YAML code:
    ```yaml
    prometheus:
        service:
            type: LoadBalancer
        prometheusSpec:
            additionalScrapeConfigs:
            - job_name: "fleet"
              static_configs:
              - targets: [<EXTERNAL-IP>:8080"]
    ```
    Replace `<EXTERNAL-IP>` with the external IP address obtained previously.
</details>


## Joining a cluster onto hub cluster

A cluster can join in a hub cluster if:

* it runs a supported Kubernetes version; it is recommended that you use Kubernetes 1.28 or later
  versions, and
* it has network connectivity to the hub cluster.

> Note
>
> To run these scripts, make sure you have already created cluster(s) and gotten their credentials.
> 

For your convenience, Fleet provides a script that can automate the process of joining a cluster
onto a hub cluster. To use the script, run the commands below after creating needed AKS clusters:

In `joinMC.sh` file line 4, replace current values of `MC_NAMES` with the names of the AKS cluster you want to join.
```sh
# Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig context you use for
# accessing your hub cluster.
export HUB_CLUSTER_CONTEXT=YOUR-HUB-CLUSTER-CONTEXT
# Replace the value of HUB_CLUSTER_ADDRESS with the address of your hub cluster API server.
export HUB_CLUSTER_ADDRESS=YOUR-HUB-CLUSTER-ADDRESS

# Replace <MEMBER-CLUSTER-NAME1> and <MEMBER-CLUSTER-NAME-2> with a list of cluster names (separated by a space as a string)
# that you would like to join the fleet as member clusters. Their context will be used to access the cluster.
# Ex.: export MC_NAMES_STR="member member2"
export MC_NAMES_STR="<MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2>" 

# Clone the Fleet GitHub repository, if necessary.
git clone https://github.com/Azure/fleet.git
# Go into fleet directory.
cd fleet

# Run the script.
chmod +x hack/setup/joinMC.sh
./hack/setup/joinMC.sh
```

It may take a few minutes for the script to finish running. Once it is completed, verify
that the cluster has joined successfully with the command below:

```sh
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
```

If you see that the cluster is still in an unknown state, it might be that the member cluster
is still connecting to the hub cluster. Should this state persist for a prolonged
period, refer to the [Troubleshooting Guide](../../docs/troubleshooting/README.md) for
more information.

### Viewing the status of a member cluster

Similarly, you can use the `MemberCluster` API in the hub cluster to view the status of a
member cluster:

```sh
# Replace the value of MEMBER-CLUSTER with the name of the member cluster of which you would like
# to view the status.
export MEMBER_CLUSTER=YOUR-MEMBER-CLUSTER
kubectl get membercluster $MEMBER_CLUSTER -o jsonpath="{.status}"
```

The status consists of:

* an array of conditions, including:

    * the `ReadyToJoin` condition, which signals whether the hub cluster is ready to accept
      the member cluster; and
    * the `Joined` condition, which signals whether the cluster has joined the fleet; and
    * the `Healthy` condition, which signals whether the cluster is in a healthy state.

  Typically, a member cluster should have all three conditions set to true. Refer to the
  [Troubleshooting Guide](../../docs/troubleshooting/README.md) for help if a cluster fails to join
  into a fleet.

* the resource usage of the cluster; at this moment Fleet reports the capacity and
  the allocatable amount of each resource in the cluster, summed up from all nodes in the cluster.

* an array of agent status, which reports the status of specific Fleet agents installed in
  the cluster; each entry features:

    * an array of conditions, in which `Joined` signals whether the specific agent has been
      successfully installed in the cluster, and `Healthy` signals whether the agent is in a
      healthy state; and
    * the timestamp of the last received heartbeat from the agent.

## Adding labels to a member cluster

You can add labels to a `MemberCluster` object in the same as with any other Kubernetes object.
These labels can then be used for targeting specific clusters in resource placement. To add a label,
run the command below:

```sh
# Replace the values of MEMBER_CLUSTER, LABEL_KEY, and LABEL_VALUE with those of your own.
export MEMBER_CLUSTER=YOUR-MEMBER-CLUSTER
export LABEL_KEY=YOUR-LABEL-KEY
export LABEL_VALUE=YOUR-LABEL-VALUE
kubectl label membercluster $MEMBER_CLUSTER $LABEL_KEY=$LABEL_VALUE
```