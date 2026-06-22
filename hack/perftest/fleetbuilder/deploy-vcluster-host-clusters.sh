#!/bin/bash
set -e

# Check the required environment variables.
RESOURCE_GROUP_NAME=${RESOURCE_GROUP_NAME:?Environment variable RESOURCE_GROUP_NAME is not set}
LOCATION=${LOCATION:?Environment variable LOCATION is not set}
REGISTRY_NAME_WO_SUFFIX=${REGISTRY_NAME_WO_SUFFIX:?Environment variable REGISTRY_NAME_WO_SUFFIX is not set}
VCLUSTER_HOST_NODE_COUNT=${VCLUSTER_HOST_NODE_COUNT:-8}
VCLUSTER_HOST_VM_SIZE=${VCLUSTER_HOST_VM_SIZE:-Standard_D16s_v3}
VNET_NAME=${VNET_NAME:?Environment variable VNET_NAME is not set}
CUSTOM_TAGS=${CUSTOM_TAGS:-perf_test=true}

while true; do
    # Retrieve a cluster name from the work queue.
    echo "Retrieving cluster name from the work queue..."
    CLUSTER_IDX=$(python3 dequeue.py)
    if [ -z "$CLUSTER_IDX" ]; then
        echo "No more clusters to create. Exiting."
        break
    fi
    CLUSTER_NAME="host-cluster-$CLUSTER_IDX"

    # Create an AKS subnet for the host cluster.
    echo "Creating subnet $CLUSTER_NAME-subnet in VNet $VNET_NAME..."
    az network vnet subnet create \
        -g "$RESOURCE_GROUP_NAME" \
        --vnet-name "$VNET_NAME" \
        -n "$CLUSTER_NAME-subnet" \
        --address-prefixes "10.$CLUSTER_IDX.0.0/16"
    
    # Retrieve the subnet ID.
    SUBNET_ID=$(az network vnet subnet show \
        -g "$RESOURCE_GROUP_NAME" \
        --vnet-name "$VNET_NAME" \
        -n "$CLUSTER_NAME-subnet" \
        --query id -o tsv)

    # Create the AKS cluster.
    echo "Creating AKS cluster $CLUSTER_NAME..."
    az aks create \
        -g "$RESOURCE_GROUP_NAME" \
        -n "$CLUSTER_NAME" \
        --location "$LOCATION" \
        --node-count "$VCLUSTER_HOST_NODE_COUNT" \
        --node-vm-size "$VCLUSTER_HOST_VM_SIZE" \
        --max-pods 100 \
        --enable-aad \
        --enable-azure-rbac \
        --tier standard \
        --network-plugin azure \
        --vnet-subnet-id "$SUBNET_ID" \
        --service-cidr "172.16.0.0/16" \
        --dns-service-ip "172.16.0.10" \
        --attach-acr "$REGISTRY_NAME_WO_SUFFIX" \
        --tags "$CUSTOM_TAGS"
    
    # Sleep for a short while.
    echo "Cluster $CLUSTER_NAME created. Sleeping for 15 seconds before processing the next cluster..."
    sleep 15
done
