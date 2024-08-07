# Setting up a Fleet

This how-to guide describes how to create a fleet using Azure Kubernetes Service, specifically:

* how to create an AKS cluster as the  hub cluster; and
* how to join any k8s clusters to the AKS hub cluster; and
* how to add labels to a member cluster representation on the hub cluster

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
For your convenience, Fleet provides a script that can automate the process of creating a hub cluster. To use script,
run the commands bellow:
```sh
# Replace the value of <AZURE-SUBSCRIPTION-ID> with your Azure subscription ID.
export SUB=<AZURE-SUBSCRIPTION-ID>
export RESOURCE_GROUP=<HUB_RESOURCE_GROUP>
export LOCATION=<HUB_LOCATION>

# Run the script. Be sure to replace the values of <HUB-CLUSTER-NAME> with those of your own.
chmod +x hack/Azure/setup/createHubCluster.sh
./hack/Azure/setup/createHubCluster.sh <HUB-CLUSTER-NAME>
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

2. Get the Prometheus community Helm Chart
   ```
   helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
   helm repo update
   ```
   
3. Edit the prometheus.yaml file in this directory to replace `<EXTERNAL-IP>` with the external IP address obtained previously.:
    ```yaml
    prometheus:
        service:
            type: LoadBalancer
        prometheusSpec:
            additionalScrapeConfigs:
            - job_name: "fleet"
              static_configs:
              - targets: ["<EXTERNAL-IP>:8080"]
    ```
4. Install the Prometheus server
    ```
    helm install prom prometheus-community/kube-prometheus-stack -f ./hack/Azure/setup/prometheus.yaml
    ```
</details>


## Joining a cluster onto hub cluster

A cluster can join in a hub cluster if:

* it runs a supported Kubernetes version; it is recommended that you use Kubernetes 1.28 or later
  versions, and
* it has network connectivity to the hub cluster.

> Note
>
> To run this script, make sure you have already created cluster(s) and gotten their credentials in the same 
> Kubeconfig file as the hub cluster.
> 

For your convenience, Fleet provides a script that can automate the process of joining a cluster
onto a hub cluster. To use the script, run the commands below after creating needed AKS clusters:
```sh
# Pass in the hub cluster name and a list of cluster context names (separated by a space) as arguments to the script that you would like to 
# join the fleet as member clusters. Their context will be used to access the cluster.
# Ex.: ./hack/setup/joinMC.sh test-hub member member2 member3 
# Run the script.
chmod +x hack/Azure/setup/joinMC.sh
./hack/Azure/setup/joinMC.sh <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> <MEMBER-CLUSTER-NAME-2> <MEMBER-CLUSTER-NAME-3> <MEMBER-CLUSTER-NAME-4>
```

It may take a few minutes for the script to finish running. Once it is completed, verify
that the cluster has joined successfully with the command below:

```sh
kubectl get membercluster $MEMBER_CLUSTER
```

If you see that the cluster is still in an unknown state, it might be that the member cluster
is still connecting to the hub cluster. Should this state persist for a prolonged
period, refer to the [Troubleshooting Guide](../../../docs/troubleshooting/README.md) for
more information.

## Adding labels to a member cluster

You can add labels to a `MemberCluster` object in the same as with any other Kubernetes object.
These labels can then be used for targeting specific clusters in resource placement. To add a label,
run the commands below:

```sh
# Replace the values of <YOUR-MEMBER_CLUSTER>, <YOUR-LABEL_KEY>, and <YOUR-LABEL_VALUE> with those of your own.
export MEMBER_CLUSTER=<YOUR-MEMBER-CLUSTER>
export LABEL_KEY=<YOUR-LABEL-KEY>
export LABEL_VALUE=<YOUR-LABEL-VALUE>
kubectl label membercluster $MEMBER_CLUSTER $LABEL_KEY=$LABEL_VALUE
```

Or, you can add the same label to multiple clusters at once by running the following script:
```sh
# Replace the value <number-of-member-cluster> to the desired number of member clusters you want to label.
chmod +x hack/Azure/setup/labelMC.sh
./hack/Azure/setup/labelMC.sh $LABEL_KEY=$LABEL_VALUE <number-of-member-clusters>
```
>**NOTE:** The script will label the number of member clusters at random.

To view the labels your current member clusters have been assigned, run the following command:
```
kubectl get membercluster --show-labels
```