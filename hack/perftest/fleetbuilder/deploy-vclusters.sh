#!/bin/bash
set -e

# Portable in-place sed: macOS requires an explicit backup extension with -i,
# while GNU sed (Linux) treats an empty string as no backup.
sed_in_place() {
    if sed --version 2>/dev/null | grep -q 'GNU'; then
        sed -i "$@"
    else
        sed -i '' "$@"
    fi
}

# Check the required environment variables.
RESOURCE_GROUP_NAME=${RESOURCE_GROUP_NAME:?Environment variable RESOURCE_GROUP_NAME is not set}
KUBECONFIG_DIR=${KUBECONFIG_DIR:?Environment variable KUBECONFIG_DIR is not set}

PER_HOST_VCLUSTER_COUNT=${PER_HOST_VCLUSTER_COUNT:-25}

while true; do
    # Retrieve a cluster name from the work queue.
    echo "Retrieving cluster name from the work queue..."
    CLUSTER_IDX=$(python3 dequeue.py)
    if [ -z "$CLUSTER_IDX" ]; then
        echo "No more clusters to create. Exiting."
        break
    fi
    CLUSTER_NAME="host-cluster-$CLUSTER_IDX"

    # Retrieve the cluster credential.
    az aks get-credentials --resource-group "$RESOURCE_GROUP_NAME" --name "$CLUSTER_NAME" --file "$KUBECONFIG_DIR/$CLUSTER_NAME.kubeconfig"
    export KUBECONFIG="$KUBECONFIG_DIR/$CLUSTER_NAME.kubeconfig"

    VCLUSTER_START_IDX=$(( (CLUSTER_IDX - 1) * PER_HOST_VCLUSTER_COUNT + 1 ))
    VCLUSTER_END_IDX=$(( CLUSTER_IDX * PER_HOST_VCLUSTER_COUNT ))
    echo "Deploying vclusters with indices from $VCLUSTER_START_IDX to $VCLUSTER_END_IDX in cluster $CLUSTER_NAME..."
    for i in $(seq $VCLUSTER_START_IDX $VCLUSTER_END_IDX); do
        VCLUSTER_NAME="vcluster-$i"
        echo "Deploying vcluster $VCLUSTER_NAME in cluster $CLUSTER_NAME..."
        vcluster describe "$VCLUSTER_NAME" -n "$VCLUSTER_NAME" > /dev/null 2>&1 || vcluster create "$VCLUSTER_NAME" -n "$VCLUSTER_NAME" --values vcluster.yaml --add=false --connect=false

        echo "Patching the vcluster API server service to use the LoadBalancer type with an internal IP assigned..."
        # Each vcluster API service is exposed via an internal LB; this is to conserve public IPs and comply with best
        # security practices. The vcluster API server must be accessed via a jumpbox.
        kubectl patch svc "$VCLUSTER_NAME" -n "$VCLUSTER_NAME" --type=merge --patch '{"spec": {"type": "LoadBalancer"}, "metadata": {"annotations": {"service.beta.kubernetes.io/azure-load-balancer-internal": "true"}}}'

        echo "Retrieving the internal IP address of the vcluster API server service..."
        kubectl wait svc "$VCLUSTER_NAME" -n "$VCLUSTER_NAME" --for=jsonpath='{.status.loadBalancer.ingress[0].ip}' --timeout=300s
        VCLUSTER_API_SERVER_IP=$(kubectl get svc "$VCLUSTER_NAME" -n "$VCLUSTER_NAME" -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

        echo "Retrieving the KUBECONFIG of the vcluster..."
        # Wait until the secret appears.
        kubectl wait secret "vc-$VCLUSTER_NAME" -n "$VCLUSTER_NAME" --for=create --timeout=300s
        kubectl get secret "vc-$VCLUSTER_NAME" -n "$VCLUSTER_NAME" -o jsonpath='{.data.config}' | base64 --decode > "$KUBECONFIG_DIR/$VCLUSTER_NAME.kubeconfig"

        echo "Patching the KUBECONFIG to use the internal IP address of the vcluster API server service..."
        sed_in_place "s/https:\/\/localhost:8443/https:\/\/$VCLUSTER_API_SERVER_IP:443/g" "$KUBECONFIG_DIR/$VCLUSTER_NAME.kubeconfig"
    done
done
