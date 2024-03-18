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
> 
>  
> Also, make sure that you have already cloned the repo and are in the root directory.
> * `git clone https://github.com/Azure/fleet.git`
> * `cd fleet`

## Create a hub cluster from an AKS Cluster
> Note
> 
>  Make sure you have already created an AKS cluster and have gotten its credentials.
> Instructions to create an AKS cluster can be found [here](https://learn.microsoft.com/en-us/azure/aks/learn/quick-kubernetes-deploy-cli).
>

For your convenience, Fleet provides a script that can automate the process of creating a hub cluster. To use script,
run the commands bellow:
```sh
# Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig context you use for
# accessing your hub cluster.
export HUB_CLUSTER_CONTEXT=<YOUR-HUB-CLUSTER-CONTEXT>
# Replace the value of HUB_CLUSTER_ADDRESS with the address of your hub cluster API server located in the kubeconfig file.
export HUB_CLUSTER_ADDRESS=<YOUR-HUB-CLUSTER-ADDRESS>

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
    ````

2. Install the Prometheus community Helm Chart
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
> To run this script, make sure you have already created cluster(s) and gotten their credentials.
> 

For your convenience, Fleet provides a script that can automate the process of joining a cluster
onto a hub cluster. To use the script, run the commands below after creating needed AKS clusters:
```sh
# Pass in a list of cluster names (separated by a space) as arguments to the script that you would like to 
# join the fleet as member clusters. Their context will be used to access the cluster.
# Ex.: ./hack/setup/joinMC.sh member member2 member3 member4
# Run the script.
chmod +x hack/setup/joinMC.sh
./hack/setup/joinMC.sh <MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2>
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