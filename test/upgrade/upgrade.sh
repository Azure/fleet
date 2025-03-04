#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

MEMBER_CLUSTER_COUNT=$1
HUB_CLUSTER="hub"
declare -a MEMBER_CLUSTERS=()

for (( i=1;i<=MEMBER_CLUSTER_COUNT;i++ ))
do
  MEMBER_CLUSTERS+=("cluster-$i")
done

export REGISTRY="${REGISTRY:-ghcr.io}"
export IMAGE_TAG="${IMAGE_TAG:-after-upgrade}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"
export HUB_AGENT_IMAGE="${HUB_AGENT_IMAGE:-hub-agent}"
export MEMBER_AGENT_IMAGE="${MEMBER_AGENT_IMAGE:-member-agent}"
export REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_IMAGE:-refresh-token}"
export GIT_TAG="${GIT_TAG:-}"
export UPGRADE_HUB_SIDE="${UPGRADE_HUB_SIDE:-}"
export UPGRADE_MEMBER_SIDE="${UPGRADE_MEMBER_SIDE:-}"

if [ -z "$UPGRADE_HUB_SIDE" ] && [ -z "$UPGRADE_MEMBER_SIDE" ]; then
    echo "No upgrade specified; set environment variable UPGRADE_HUB_SIDE and/or UPGRADE_MEMBER_SIDE to upgrade the hub and/or member agents."
    exit 1
fi

PREVIOUS_BRANCH=""
if [ -n "${GIT_TAG}" ]; then
    echo "A tag ($GIT_TAG) has been specified; re-build image using the given tag."

    PREVIOUS_BRANCH=$(git branch --show-current)

    echo "Fetch all tags..."
    git fetch --all
    git checkout $GIT_TAG
    echo "Checked out source code at $GIT_TAG."
fi

# Build the Fleet agent images.
echo "Building and the Fleet agent images..."

TAG=$IMAGE_TAG make -C "../.." docker-build-hub-agent
TAG=$IMAGE_TAG make -C "../.." docker-build-member-agent
TAG=$IMAGE_TAG make -C "../.." docker-build-refresh-token

# Load the Fleet agent images (for upgrading) into the kind clusters.

# Load the hub agent image into the hub cluster.
kind load docker-image --name $HUB_CLUSTER $REGISTRY/$HUB_AGENT_IMAGE:$IMAGE_TAG

# Load the member agent image and the refresh token image into the member clusters.
for i in "${MEMBER_CLUSTERS[@]}"
do
	kind load docker-image --name "$i" $REGISTRY/$MEMBER_AGENT_IMAGE:$IMAGE_TAG
    kind load docker-image --name "$i" $REGISTRY/$REFRESH_TOKEN_IMAGE:$IMAGE_TAG
done

# Upgrade the Fleet agent in the kind clusters.

# Upgrade the agent image in the hub cluster.
if [ -n "$UPGRADE_HUB_SIDE" ]; then
    echo "Upgrading the hub agent in the hub cluster..."
    kind export kubeconfig --name $HUB_CLUSTER
    helm upgrade hub-agent ../../charts/hub-agent/ \
        --set image.pullPolicy=Never \
        --set image.repository=$REGISTRY/$HUB_AGENT_IMAGE \
        --set image.tag=$IMAGE_TAG \
        --set namespace=fleet-system \
        --set logVerbosity=5 \
        --set enableWebhook=true \
        --set webhookClientConnectionType=service \
        --set forceDeleteWaitTime="1m0s" \
        --set clusterUnhealthyThreshold="3m0s" \
        --set logFileMaxSize=1000000
fi

# Query the URL of the hub cluster API server.
kind export kubeconfig --name $HUB_CLUSTER
HUB_SERVER_URL="https://$(docker inspect $HUB_CLUSTER-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443"

# Upgrade the agent images in all member clusters.
if [ -n "$UPGRADE_MEMBER_SIDE" ]; then
    echo "Upgrading the member agents in the member clusters..."
    for (( i=0; i<${MEMBER_CLUSTER_COUNT}; i++ ));
    do
        kind export kubeconfig --name "${MEMBER_CLUSTERS[$i]}"
        helm upgrade member-agent ../../charts/member-agent/ \
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
            --set enableV1Alpha1APIs=false \
            --set enableV1Beta1APIs=true
    done
fi

# Restore to the previous branch.
if [ -n "$PREVIOUS_BRANCH" ]; then
    git checkout $PREVIOUS_BRANCH
    echo "Checked out source code at $PREVIOUS_BRANCH."
fi
