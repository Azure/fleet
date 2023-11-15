#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Before updating the default kind image to use, verify that the version is supported
# by the current kind release.
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.25.11}"
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
MEMBER_CLUSTER_COUNT=$1

HUB_CLUSTER="hub"
MEMBER_CLUSTER_1="cluster-1"
MEMBER_CLUSTER_2="cluster-2"
MEMBER_CLUSTER_3="cluster-3"
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

# Create the kind clusters
echo "Creating the kind clusters..."

# Create the hub cluster
kind create cluster --name $HUB_CLUSTER --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG

# Create the member clusters
for i in "${MEMBER_CLUSTERS[@]}"
do
  kind create cluster --name  "$i" --image=$KIND_IMAGE --kubeconfig=$KUBECONFIG
done

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
    --set logVerbosity=2 \
    --set namespace=fleet-system \
    --set enableWebhook=true \
    --set webhookClientConnectionType=service \
    --set enableV1Alpha1APIs=false \
    --set enableV1Beta1APIs=true

# Instal the member agent and related components to the member clusters

# Retrieve an access token from the hub cluster
TOKEN=$(kubectl get secret hub-kubeconfig-secret -n fleet-system -o jsonpath='{.data.token}' | base64 -d)
for i in "${MEMBER_CLUSTERS[@]}"
do
    kind export kubeconfig --name "$i"
    kubectl delete secret hub-kubeconfig-secret --ignore-not-found
    kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN
done

# Query the URL of the hub cluster API server
kind export kubeconfig --name $HUB_CLUSTER
HUB_SERVER_URL="https://$(docker inspect $HUB_CLUSTER-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443"

# Install the member agents and related components
for i in "${MEMBER_CLUSTERS[@]}"
do
    kind export kubeconfig --name "$i"
    helm install member-agent ../../charts/member-agent/ \
        --set config.hubURL=$HUB_SERVER_URL \
        --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
        --set image.tag=$TAG \
        --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
        --set refreshtoken.tag=$TAG \
        --set image.pullPolicy=Never \
        --set refreshtoken.pullPolicy=Never \
        --set config.memberClusterName="kind-$i" \
        --set logVerbosity=5 \
        --set namespace=fleet-system \
        --set enableV1Alpha1APIs=false \
        --set enableV1Beta1APIs=true
done
