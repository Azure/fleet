#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Before updating the default kind image to use, verify that the version is supported
# by the current kind release.
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.33.4}"
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
MEMBER_CLUSTER_COUNT=$1

HUB_CLUSTER="hub"
declare -a MEMBER_CLUSTERS=()

for (( i=1;i<=MEMBER_CLUSTER_COUNT;i++ ))
do
  MEMBER_CLUSTERS+=("cluster-$i")
done

export REGISTRY="${REGISTRY:-ghcr.io}"
export TAG="${TAG:-e2e}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"
export HUB_AGENT_IMAGE="${HUB_AGENT_IMAGE:-hub-agent}"
export MEMBER_AGENT_IMAGE="${MEMBER_AGENT_IMAGE:-member-agent}"
export REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_IMAGE:-refresh-token}"
export PROPERTY_PROVIDER="${PROPERTY_PROVIDER:-azure}"
export USE_PREDEFINED_REGIONS="${USE_PREDEFINED_REGIONS:-false}"
export RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL="${RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL:-0m}"
export RESOURCE_CHANGES_COLLECTION_DURATION="${RESOURCE_CHANGES_COLLECTION_DURATION:-0m}"

# The pre-defined regions; if the AKS property provider is used.
#
# Note that for a specific cluster, if a predefined region is not set, the node region must
# be set, so that the AKS property provider can auto-discover the region.
REGIONS=("" "" "eastasia")
# The regions that should be set on each node of the respective clusters; if the AKS property
# provider is used.
#
# Note that for a specific cluster, if a predefined region is set, the node region should use
# the same value.
AKS_NODE_REGIONS=("westus" "northeurope" "eastasia")
# The SKUs that should be set on each node of the respective clusters; if the AKS property
# provider is used. See the AKS documentation for specifics.
#
# Note that this is for information only; kind nodes always use the same fixed setup
# (total/allocatable capacity = host capacity).
AKS_NODE_SKUS=("Standard_B2ats_v2" "Standard_B2ts_v2" "Standard_D8s_v5" "Standard_E16_v5" "Standard_M16ms")
AKS_SKU_COUNT=${#AKS_NODE_SKUS[@]}
# The number of clusters that has pre-defined configuration for testing purposes.
RESERVED_CLUSTER_COUNT=${MEMBER_CLUSTER_COUNT}

# Create the kind clusters
echo "Creating the kind clusters..."

# Create the hub cluster
kind create cluster --name $HUB_CLUSTER --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG

# Create the member clusters
for (( i=0; i<${MEMBER_CLUSTER_COUNT}; i++ ));
do
    if [ "$i" -lt $RESERVED_CLUSTER_COUNT ]; then
        kind create cluster --name  "${MEMBER_CLUSTERS[$i]}" --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG --config ./kindconfigs/${MEMBER_CLUSTERS[$i]}.yaml
    else
        kind create cluster --name  "${MEMBER_CLUSTERS[$i]}" --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG
    fi
done

# Set up the nodes in the member clusters, if the AKS property provider is used.
if [ "$PROPERTY_PROVIDER" = "azure" ]
then
    echo "Setting up nodes in each member cluster..."

    for (( i=0; i<$RESERVED_CLUSTER_COUNT; i++ ));
    do
        kind export kubeconfig --name "${MEMBER_CLUSTERS[$i]}"
        NODE_NAMES=$(kubectl get nodes -o 'jsonpath={.items[*].metadata.name}')
        # Use an if to avoid non-zero exit code.
        if read -ra NODES -d '' <<<"$NODE_NAMES"; then :; fi

        NODE_COUNT=${#NODES[@]}
        for (( j=0; j<${NODE_COUNT}; j++ ));
        do
            # Use the eastus region if the index overflows.
            kubectl label node "${NODES[$j]}" topology.kubernetes.io/region=${AKS_NODE_REGIONS[$i]:-eastus}

            # Set up a SKU in random.
            #
            # This is an expedient solution, but good enough for the E2E scenarios.
            k=$(( RANDOM % AKS_SKU_COUNT ))
            kubectl label node "${NODES[$j]}" beta.kubernetes.io/instance-type=${AKS_NODE_SKUS[$k]}
        done
    done
fi

# Build the Fleet agent images
echo "Building and the Fleet agent images..."

make -C "../.." docker-build-hub-agent
make -C "../.." docker-build-member-agent
make -C "../.." docker-build-refresh-token

# Load the Fleet agent images into the kind clusters

# Load the hub agent image into the hub cluster
kind load docker-image --name $HUB_CLUSTER $REGISTRY/$HUB_AGENT_IMAGE:$TAG

# Load the member agent image and the refresh token image into the member clusters
for i in "${MEMBER_CLUSTERS[@]}"
do
	kind load docker-image --name "$i" $REGISTRY/$MEMBER_AGENT_IMAGE:$TAG
    kind load docker-image --name "$i" $REGISTRY/$REFRESH_TOKEN_IMAGE:$TAG
done

# Install the helm charts

# Install the hub agent to the hub cluster
kind export kubeconfig --name $HUB_CLUSTER
helm install hub-agent ../../charts/hub-agent/ \
    --set image.pullPolicy=Never \
    --set image.repository=$REGISTRY/$HUB_AGENT_IMAGE \
    --set image.tag=$TAG \
    --set namespace=fleet-system \
    --set logVerbosity=5 \
    --set enableWebhook=true \
    --set webhookClientConnectionType=service \
    --set forceDeleteWaitTime="1m0s" \
    --set clusterUnhealthyThreshold="3m0s" \
    --set logFileMaxSize=100000 \
    --set MaxConcurrentClusterPlacement=200 \
    --set resourceSnapshotCreationMinimumInterval=$RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL \
    --set resourceChangesCollectionDuration=$RESOURCE_CHANGES_COLLECTION_DURATION

# Download CRDs from Fleet networking repo
export ENDPOINT_SLICE_EXPORT_CRD_URL=https://raw.githubusercontent.com/Azure/fleet-networking/v0.2.7/config/crd/bases/networking.fleet.azure.com_endpointsliceexports.yaml
export INTERNAL_SERVICE_EXPORT_CRD_URL=https://raw.githubusercontent.com/Azure/fleet-networking/v0.2.7/config/crd/bases/networking.fleet.azure.com_internalserviceexports.yaml
export INTERNAL_SERVICE_IMPORT_CRD_URL=https://raw.githubusercontent.com/Azure/fleet-networking/v0.2.7/config/crd/bases/networking.fleet.azure.com_internalserviceimports.yaml
curl $ENDPOINT_SLICE_EXPORT_CRD_URL | kubectl apply -f -
curl $INTERNAL_SERVICE_EXPORT_CRD_URL | kubectl apply -f -
curl $INTERNAL_SERVICE_IMPORT_CRD_URL | kubectl apply -f -

# Install the member agent and related components to the member clusters

# Set up a service account for each member in the hub cluster.
#
# Note that these service account has no permission set up at all; the authorization will be
# configured by the hub agent.
for i in "${MEMBER_CLUSTERS[@]}"
do
    kubectl create serviceaccount fleet-member-agent-$i -n fleet-system
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Secret
    metadata:
        name: fleet-member-agent-$i-sa
        namespace: fleet-system
        annotations:
            kubernetes.io/service-account.name: fleet-member-agent-$i
    type: kubernetes.io/service-account-token
EOF
done

for i in "${MEMBER_CLUSTERS[@]}"
do
    kind export kubeconfig --name $HUB_CLUSTER
    TOKEN=$(kubectl get secret fleet-member-agent-$i-sa -n fleet-system -o jsonpath='{.data.token}' | base64 -d)
    kind export kubeconfig --name "$i"
    kubectl delete secret hub-kubeconfig-secret --ignore-not-found
    kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN
done

# Query the URL of the hub cluster API server
kind export kubeconfig --name $HUB_CLUSTER
HUB_SERVER_URL="https://$(docker inspect $HUB_CLUSTER-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443"

# Install the member agents and related components
for (( i=0; i<${MEMBER_CLUSTER_COUNT}; i++ ));
do
    kind export kubeconfig --name "${MEMBER_CLUSTERS[$i]}"
    if [ "$i" -lt $RESERVED_CLUSTER_COUNT ]; then
        helm install member-agent ../../charts/member-agent/ \
            --set config.hubURL=$HUB_SERVER_URL \
            --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
            --set image.tag=$TAG \
            --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
            --set refreshtoken.tag=$TAG \
            --set image.pullPolicy=Never \
            --set refreshtoken.pullPolicy=Never \
            --set config.memberClusterName="kind-${MEMBER_CLUSTERS[$i]}" \
            --set logVerbosity=5 \
            --set namespace=fleet-system \
            --set enableV1Beta1APIs=true \
            --set propertyProvider=$PROPERTY_PROVIDER \
            --set region=${REGIONS[$i]} \
            $( [ "$PROPERTY_PROVIDER" = "azure" ] && echo "-f azure_valid_config.yaml" )
    else
        helm install member-agent ../../charts/member-agent/ \
            --set config.hubURL=$HUB_SERVER_URL \
            --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
            --set image.tag=$TAG \
            --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
            --set refreshtoken.tag=$TAG \
            --set image.pullPolicy=Never \
            --set refreshtoken.pullPolicy=Never \
            --set config.memberClusterName="kind-${MEMBER_CLUSTERS[$i]}" \
            --set logVerbosity=5 \
            --set namespace=fleet-system \
            --set enableV1Beta1APIs=true \
            --set propertyProvider=$PROPERTY_PROVIDER \
            $( [ "$PROPERTY_PROVIDER" = "azure" ] && echo "-f azure_valid_config.yaml" )
    fi
done

# Create tools directory if it doesn't exist
mkdir -p ../../hack/tools/bin

# Build fleet plugin binary
echo "Building fleet kubectl-plugin binary..."
go build -o ../../hack/tools/bin/kubectl-fleet ../../tools/fleet
