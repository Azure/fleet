#!/bin/bash
set -e

# Check the required environment variables.
RESOURCE_GROUP_NAME=${RESOURCE_GROUP_NAME:?Environment variable RESOURCE_GROUP_NAME is not set}
LOCATION=${LOCATION:?Environment variable LOCATION is not set}
JUMPBOX_NAME=${JUMPBOX_NAME:?Environment variable JUMPBOX_NAME is not set}
JUMPBOX_VM_SIZE=${JUMPBOX_VM_SIZE:-Standard_D2s_v3}
VNET_NAME=${VNET_NAME:?Environment variable VNET_NAME is not set}
SUBNET_NAME=${SUBNET_NAME:-default}
CUSTOM_TAGS=${CUSTOM_TAGS:-perf_test=true}

# Create a jumpbox VM in the same VNet as the vcluster host clusters.
echo "Creating jumpbox VM $JUMPBOX_NAME in resource group $RESOURCE_GROUP_NAME..."
az vm create \
    -g "$RESOURCE_GROUP_NAME" \
    -n "$JUMPBOX_NAME" \
    --location "$LOCATION" \
    --image Ubuntu2204 \
    --size "$JUMPBOX_VM_SIZE" \
    --vnet-name "$VNET_NAME" \
    --subnet "$SUBNET_NAME" \
    --assign-identity \
    --admin-username azureuser \
    --generate-ssh-keys \
    --tags "$CUSTOM_TAGS"

# Grant the jumpbox VM access to the whole resource group as a contributor, so that it can
# access the vcluster host clusters and perform necessary operations.
echo "Granting jumpbox VM $JUMPBOX_NAME contributor access to resource group $RESOURCE_GROUP_NAME..."
JUMPBOX_PRINCIPAL_ID=$(az vm show -g "$RESOURCE_GROUP_NAME" -n "$JUMPBOX_NAME" --query "identity.principalId" -o tsv)
az role assignment create \
    --assignee "$JUMPBOX_PRINCIPAL_ID" \
    --role "Contributor" \
    --scope "/subscriptions/$(az account show --query id -o tsv)/resourceGroups/$RESOURCE_GROUP_NAME"

# Grant the jumpbox VM access to queues in the storage account, so that it can access the
# work queue for processing work items.
echo "Granting jumpbox VM $JUMPBOX_NAME Storage Queue Data Contributor access to resource group $RESOURCE_GROUP_NAME..."
az role assignment create \
    --assignee "$JUMPBOX_PRINCIPAL_ID" \
    --role "Storage Queue Data Contributor" \
    --scope "/subscriptions/$(az account show --query id -o tsv)/resourceGroups/$RESOURCE_GROUP_NAME"

# Grant the jumpbox VM access to all Kubernetes clusters in the resource group, so that it
# can access the vcluster host clusters and perform necessary operations.
echo "Granting jumpbox VM $JUMPBOX_NAME Azure Kubernetes Service RBAC Cluster Admin access to resource group $RESOURCE_GROUP_NAME..."
az role assignment create \
    --assignee "$JUMPBOX_PRINCIPAL_ID" \
    --role "Azure Kubernetes Service RBAC Cluster Admin" \
    --scope "/subscriptions/$(az account show --query id -o tsv)/resourceGroups/$RESOURCE_GROUP_NAME"
