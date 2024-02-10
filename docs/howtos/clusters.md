# Managing Clusters

This how-to guide discusses how to manage clusters in a fleet, specifically:

* how to join a cluster into a fleet; and
* how to set a cluster to leave a fleet; and
* how to add labels to a member cluster

## Joining a cluster into a fleet

A cluster can join in a fleet if:

* it runs a supported Kubernetes version; it is recommended that you use Kubernetes 1.24 or later
versions, and
* it has network connectivity to the hub cluster of the fleet.

For your convenience, Fleet provides a script that can automate the process of joining a cluster
into a fleet. To use the script, run the commands below:

> Note
>
> To run this script, make sure that you have already installed the following tools in your
> system:
> * `kubectl`, the Kubernetes CLI
> * `helm`, a Kubernetes package manager
> * `curl`
> * `jq`
> * `base64`

```sh
# Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig context you use for
# accessing your hub cluster.
export HUB_CLUSTER_CONTEXT=YOUR-HUB-CLUSTER-CONTEXT
# Replace the value of HUB_CLUSTER_ADDRESS with the address of your hub cluster API server.
export HUB_CLUSTER_ADDRESS=YOUR-HUB-CLUSTER-ADDRESS
# Replace the value of MEMBER_CLUSTER with the name you would like to assign to the new member
# cluster.
#
# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER=YOUR-MEMBER-CLUSTER
# Replace the value of MEMBER_CLUSTER_CONTEXT with the name of the kubeconfig context you use
# for accessing your member cluster.
export MEMBER_CLUSTER_CONTEXT=YOUR-MEMBER-CLUSTER-CONTEXT

# Clone the Fleet GitHub repository.
git clone https://github.com/Azure/fleet.git

# Run the script.
chmod +x fleet/hack/membership/join.sh
./fleet/hack/membership/join.sh
```

It may take a few minutes for the script to finish running. Once it is completed, verify
that the cluster has joined successfully with the command below:

```sh
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
```

If you see that the cluster is still in an unknown state, it might be that the member cluster
is still connecting to the hub cluster. Should this state persist for a prolonged
period, refer to the [Troubleshooting Guide](../troubleshooting/README.md) for
more information.

Alternatively, if you would like to find out the exact steps the script performs, or if you feel
like fine-tuning some of the steps, you may join a cluster manually to your fleet with the
instructions below:

<details>
<summary>Joining a member cluster manually</summary>

1. Make sure that you have installed `kubectl`, `helm`, `curl`, `jq`, and `base64` in your
system.

2. Create a Kubernetes service account in your hub cluster:

    ```sh
    # Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig
    # context you use for accessing your hub cluster.
    export HUB_CLUSTER_CONTEXT="YOUR-HUB-CLUSTER-CONTEXT"
    # Replace the value of MEMBER_CLUSTER with a name you would like to assign to the new
    # member cluster.
    #
    # Note that the value of MEMBER_CLUSTER will be used as the name the member cluster registers
    # with the hub cluster.
    export MEMBER_CLUSTER="YOUR-MEMBER-CLUSTER"

    export SERVICE_ACCOUNT="$MEMBER_CLUSTER-hub-cluster-access"

    kubectl config use-context $HUB_CLUSTER_CONTEXT
    # The service account can, in theory, be created in any namespace; for simplicity reasons,
    # here you will use the namespace reserved by Fleet installation, `fleet-system`.
    #
    # Note that if you choose a different value, commands in some steps below need to be
    # modified accordingly.
    kubectl create serviceaccount $SERVICE_ACCOUNT -n fleet-system
    ```

3. Create a Kubernetes secret of the service account token type, which the member cluster will
use to access the hub cluster.

    ```sh
    export SERVICE_ACCOUNT_SECRET="$MEMBER_CLUSTER-hub-cluster-access-token"
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Secret
    metadata:
        name: $SERVICE_ACCOUNT_SECRET
        namespace: fleet-system
        annotations:
            kubernetes.io/service-account.name: $SERVICE_ACCOUNT
    type: kubernetes.io/service-account-token
    EOF
    ```

    After the secret is created successfully, extract the token from the secret:

    ```sh
    export TOKEN=$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n fleet-system -o jsonpath='{.data.token}' | base64 -d)
    ```

    > Note
    >
    > Keep the token in a secure place; anyone with access to this token can access the hub cluster
    > in the same way as the Fleet member cluster does.

    You may have noticed that at this moment, no access control has been set on the service
    account; Fleet will set things up when the member cluster joins. The service account will be
    given the minimally viable set of permissions for the Fleet member cluster to connect to the
    hub cluster; its access will be restricted to one namespace, specifically reserved for the
    member cluster, as per security best practices.

4. Register the member cluster with the hub cluster; Fleet manages cluster membership using the
`MemberCluster` API:

    ```sh
    cat <<EOF | kubectl apply -f -
    apiVersion: cluster.kubernetes-fleet.io/v1beta1
    kind: MemberCluster
    metadata:
        name: $MEMBER_CLUSTER
    spec:
        identity:
            name: $SERVICE_ACCOUNT
            kind: ServiceAccount
            namespace: fleet-system
            apiGroup: ""
        heartbeatPeriodSeconds: 60
    EOF
    ```

5. Set up the member agent, the Fleet component that works on the member cluster end, to enable
Fleet connection:

    ```sh
    # Clone the Fleet repository from GitHub.
    git clone https://github.com/Azure/fleet.git

    # Install the member agent helm chart on the member cluster.

    # Replace the value of MEMBER_CLUSTER_CONTEXT with the name of the kubeconfig context you use
    # for member cluster access.
    export MEMBER_CLUSTER_CONTEXT="YOUR-MEMBER-CLUSTER-CONTEXT"

    # Replace the value of HUB_CLUSTER_ADDRESS with the address of the hub cluster API server.
    export HUB_CLUSTER_ADDRESS="YOUR-HUB-CLUSTER-ADDRESS"

    # The variables below uses the Fleet images kept in the Microsoft Container Registry (MCR),
    # and will retrieve the latest version from the Fleet GitHub repository.
    #
    # You can, however, build the Fleet images of your own; see the repository README for
    # more information.
    export REGISTRY="mcr.microsoft.com/aks/fleet"
    export FLEET_VERSION=$(curl "https://api.github.com/repos/Azure/fleet/tags" | jq -r '.[0].name')
    export MEMBER_AGENT_IMAGE="member-agent"
    export REFRESH_TOKEN_IMAGE="refresh-token"

    kubectl config use-context $MEMBER_CLUSTER_CONTEXT
    # Create the secret with the token extracted previously for member agent to use.
    kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN
    helm install member-agent fleet/charts/member-agent/ \
        --set config.hubURL=$HUB_CLUSTER_ADDRESS \
        --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
        --set image.tag=$FLEET_VERSION \
        --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
        --set refreshtoken.tag=$FLEET_VERSION \
        --set image.pullPolicy=Always \
        --set refreshtoken.pullPolicy=Always \
        --set config.memberClusterName="$MEMBER_CLUSTER" \
        --set logVerbosity=5 \
        --set namespace=fleet-system \
        --set enableV1Alpha1APIs=false \
        --set enableV1Beta1APIs=true
    ```

6. Verify that the installation of the member agent is successful:

    ```sh
    kubectl get pods -n fleet-system
    ```

    You should see that all the returned pods are up and running. Note that it may take a few
    minutes for the member agent to get ready.

7. Verify that the member cluster has joined the fleet successfully:

    ```sh
    kubectl config use-context $HUB_CLUSTER_CONTEXT
    kubectl get membercluster $MEMBER_CLUSTER
    ```

</details>

## Setting a cluster to leave a fleet

Fleet uses the `MemberCluster` API to manage cluster memberships. To remove a member cluster
from a fleet, simply delete its corresponding `MemberCluster` object from your hub cluster:

```sh
# Replace the value of MEMBER-CLUSTER with the name of the member cluster you would like to
# remove from a fleet.
export MEMBER_CLUSTER=YOUR-MEMBER-CLUSTER
kubectl delete membercluster $MEMBER_CLUSTER
```

It may take a while before the member cluster leaves the fleet successfully. Fleet will perform
some cleanup; all the resources placed onto the cluster will be removed.

After the member cluster leaves, you can remove the member agent installation from it using Helm:

```sh
# Replace the value of MEMBER_CLUSTER_CONTEXT with the name of the kubeconfig context you use
# for member cluster access.
export MEMBER_CLUSTER_CONTEXT=YOUR-MEMBER-CLUSTER-CONTEXT
kubectl config use-context $MEMBER_CLUSTER_CONTEXT
helm uninstall member-agent
```

It may take a few moments before the uninstallation completes.

## Viewing the status of a member cluster

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
    [Troubleshooting Guide](../troubleshooting/README.md) for help if a cluster fails to join
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
