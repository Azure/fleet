#!/usr/bin/env bash

# Note: this script is used to set up the before upgrade environment for the
# version compatibility test **in the current commit**.

set -o errexit
set -o nounset
set -o pipefail

# Before updating the default kind image to use, verify that the version is supported
# by the current kind release.
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.30.0}"
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
MEMBER_CLUSTER_COUNT=$1

HUB_CLUSTER="hub"
declare -a MEMBER_CLUSTERS=()

for (( i=1;i<=MEMBER_CLUSTER_COUNT;i++ ))
do
  MEMBER_CLUSTERS+=("cluster-$i")
done

export REGISTRY="${REGISTRY:-ghcr.io}"
export IMAGE_TAG="${IMAGE_TAG:-before-upgrade}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"
export HUB_AGENT_IMAGE="${HUB_AGENT_IMAGE:-hub-agent}"
export MEMBER_AGENT_IMAGE="${MEMBER_AGENT_IMAGE:-member-agent}"
export REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_IMAGE:-refresh-token}"

# Build the Fleet agent images.
echo "Building and the Fleet agent images..."

TAG=$IMAGE_TAG make docker-build-hub-agent
TAG=$IMAGE_TAG make docker-build-member-agent
TAG=$IMAGE_TAG make docker-build-refresh-token

# Create the kind clusters.
echo "Creating the kind clusters..."

# Create the hub cluster.
kind create cluster --name $HUB_CLUSTER --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG

# Create the member clusters.
for (( i=0; i<${MEMBER_CLUSTER_COUNT}; i++ ));
do
    kind create cluster --name  "${MEMBER_CLUSTERS[$i]}" --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG
done

# Load the Fleet agent images into the kind clusters.

# Load the hub agent image into the hub cluster.
kind load docker-image --name $HUB_CLUSTER $REGISTRY/$HUB_AGENT_IMAGE:$IMAGE_TAG

# Load the member agent image and the refresh token image into the member clusters.
for i in "${MEMBER_CLUSTERS[@]}"
do
	kind load docker-image --name "$i" $REGISTRY/$MEMBER_AGENT_IMAGE:$IMAGE_TAG
    kind load docker-image --name "$i" $REGISTRY/$REFRESH_TOKEN_IMAGE:$IMAGE_TAG
done

# Install the helm charts.

# Install the hub agent to the hub cluster.
kind export kubeconfig --name $HUB_CLUSTER
helm install hub-agent charts/hub-agent/ \
    --set image.pullPolicy=Never \
    --set image.repository=$REGISTRY/$HUB_AGENT_IMAGE \
    --set image.tag=$IMAGE_TAG \
    --set namespace=fleet-system \
    --set logVerbosity=5 \
    --set enableWebhook=false \
    --set webhookClientConnectionType=service \
    --set forceDeleteWaitTime="1m0s" \
    --set clusterUnhealthyThreshold="3m0s" \
    --set logFileMaxSize=100000

# Install the member agent and related components to the member clusters.

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

# Query the URL of the hub cluster API server.
kind export kubeconfig --name $HUB_CLUSTER
HUB_SERVER_URL="https://$(docker inspect $HUB_CLUSTER-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443"

# Install the member agents and related components.
for (( i=0; i<${MEMBER_CLUSTER_COUNT}; i++ ));
do
    kind export kubeconfig --name "${MEMBER_CLUSTERS[$i]}"
    helm install member-agent charts/member-agent/ \
        --set config.hubURL=$HUB_SERVER_URL \
        --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
        --set image.tag=$IMAGE_TAG \
        --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
        --set refreshtoken.tag=$IMAGE_TAG \
        --set image.pullPolicy=Never \
        --set refreshtoken.pullPolicy=Never \
        --set config.memberClusterName="kind-${MEMBER_CLUSTERS[$i]}" \
        --set logVerbosity=5 \
        --set namespace=fleet-system \
        --set enableV1Beta1APIs=true
done

echo "Setup for the before upgrade environment has been completed."
