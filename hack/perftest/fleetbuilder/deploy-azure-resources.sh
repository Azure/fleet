#!/bin/bash
set -e

RESOURCE_GROUP_NAME=${RESOURCE_GROUP_NAME:?Environment variable RESOURCE_GROUP_NAME is not set}
LOCATION=${LOCATION:?Environment variable LOCATION is not set}
REGISTRY_NAME_WO_SUFFIX=${REGISTRY_NAME_WO_SUFFIX:?Environment variable REGISTRY_NAME_WO_SUFFIX is not set}
VNET_NAME=${VNET_NAME:?Environment variable VNET_NAME is not set}
STORAGE_ACCOUNT_NAME=${STORAGE_ACCOUNT_NAME:?Environment variable STORAGE_ACCOUNT_NAME is not set}
QUEUE_NAME=${QUEUE_NAME:?Environment variable QUEUE_NAME is not set}
CUSTOM_TAGS=${CUSTOM_TAGS:-perf_test=true}

# Create an Azure resource group.
echo "Creating resource group $RESOURCE_GROUP_NAME in location $LOCATION..."
az group create \
    -n "$RESOURCE_GROUP_NAME" \
    -l "$LOCATION" \
    --tags "$CUSTOM_TAGS"

# Create an Azure Container Registry.
echo "Creating Azure Container Registry $REGISTRY_NAME_WO_SUFFIX in resource group $RESOURCE_GROUP_NAME..."
az acr create \
    -n "$REGISTRY_NAME_WO_SUFFIX" \
    -g "$RESOURCE_GROUP_NAME" \
    -l "$LOCATION" \
    --sku Basic \
    --tags "$CUSTOM_TAGS"

# Create an Azure VNet for the host clusters.
echo "Creating VNet $VNET_NAME in resource group $RESOURCE_GROUP_NAME..."
az network vnet create \
    -g "$RESOURCE_GROUP_NAME" \
    -n "$VNET_NAME" \
    --location "$LOCATION" \
    --address-prefixes "10.0.0.0/8" \
    --subnet-name "default" \
    --subnet-prefixes "10.0.0.0/16" \
    --tags "$CUSTOM_TAGS"

# Create an Azure storage account.
echo "Creating storage account $STORAGE_ACCOUNT_NAME in resource group $RESOURCE_GROUP_NAME..."
az storage account create \
    -n "$STORAGE_ACCOUNT_NAME" \
    -g "$RESOURCE_GROUP_NAME" \
    -l "$LOCATION" \
    --sku Standard_LRS \
    --tags "$CUSTOM_TAGS"

# Create an Azure storage queue.
echo "Creating storage queue $QUEUE_NAME in storage account $STORAGE_ACCOUNT_NAME..."
az storage queue create \
    -n "$QUEUE_NAME" \
    --account-name "$STORAGE_ACCOUNT_NAME"
