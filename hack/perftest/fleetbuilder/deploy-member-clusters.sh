#!/bin/bash
set -e

# Check the required environment variables.
RESOURCE_GROUP_NAME=${RESOURCE_GROUP_NAME:?Environment variable RESOURCE_GROUP_NAME is not set}
LOCATION=${LOCATION:?Environment variable LOCATION is not set}
REGISTRY_NAME_WO_SUFFIX=${REGISTRY_NAME_WO_SUFFIX:?Environment variable REGISTRY_NAME_WO_SUFFIX is not set}
MEMBER_CLUSTER_NODE_COUNT=${MEMBER_CLUSTER_NODE_COUNT:-2}
MEMBER_CLUSTER_VM_SIZE=${MEMBER_CLUSTER_VM_SIZE:-Standard_D4s_v3}
CUSTOM_TAGS=${CUSTOM_TAGS:-perf_test=true}

while true; do
    # Retrieve a cluster name from the work queue.
    echo "Retrieving cluster name from the work queue..."
    CLUSTER_IDX=$(python3 dequeue.py)
    if [ -z "$CLUSTER_IDX" ]; then
        echo "No more clusters to create. Exiting."
        break
    fi
    CLUSTER_NAME="cluster-$CLUSTER_IDX"

    # Create the AKS cluster.
    echo "Creating AKS cluster $CLUSTER_NAME..."
    az aks create \
        -g "$RESOURCE_GROUP_NAME" \
        -n "$CLUSTER_NAME" \
        --location "$LOCATION" \
        --node-count "$MEMBER_CLUSTER_NODE_COUNT" \
        --node-vm-size "$MEMBER_CLUSTER_VM_SIZE" \
        --enable-aad \
        --enable-azure-rbac \
        --tier standard \
        --network-plugin azure \
        --attach-acr "$REGISTRY_NAME_WO_SUFFIX" \
        --tags "$CUSTOM_TAGS"
    
    # Sleep for a short while.
    echo "Cluster $CLUSTER_NAME created. Sleeping for 15 seconds before processing the next cluster..."
    sleep 15
done
