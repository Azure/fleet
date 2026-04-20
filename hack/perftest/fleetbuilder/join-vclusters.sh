#!/bin/bash
set -e

# Check the required environment variables.
REGISTRY_NAME=${REGISTRY_NAME:?Environment variable REGISTRY_NAME is not set}
KUBECONFIG_DIR=${KUBECONFIG_DIR:?Environment variable KUBECONFIG_DIR is not set}
KUBEFLEET_SRC_REPO=${KUBEFLEET_SRC_REPO:?Environment variable KUBEFLEET_SRC_REPO is not set}

HUB_CLUSTER_NAME=${HUB_CLUSTER_NAME:-hub}
HUB_CLUSTER_API_SERVER_ADDR=${HUB_CLUSTER_API_SERVER_ADDR:?Environment variable HUB_CLUSTER_API_SERVER_ADDR is not set}

MEMBER_AGENT_IMAGE_NAME="${MEMBER_AGENT_IMAGE_NAME:-member-agent}"
REFRESH_TOKEN_IMAGE_NAME="${REFRESH_TOKEN_IMAGE_NAME:-refresh-token}"
CRD_INSTALLER_IMAGE_NAME="${CRD_INSTALLER_IMAGE_NAME:-crd-installer}"
PROPERTY_PROVIDER="${PROPERTY_PROVIDER:-azure}"
COMMON_CORE_IMAGE_TAG=${COMMON_CORE_IMAGE_TAG:-experimental}

INSTALL_NETWORKING_AGENTS=${INSTALL_NETWORKING_AGENTS:-true}
if [ "$INSTALL_NETWORKING_AGENTS" = "true" ]; then
    FLEET_NETWORKING_SRC_REPO=${FLEET_NETWORKING_SRC_REPO:?Environment variable FLEET_NETWORKING_SRC_REPO is not set}
    MEMBER_NET_AGENT_IMAGE_NAME=${MEMBER_NET_AGENT_IMAGE_NAME:-member-net-controller-manager}
    MCS_AGENT_IMAGE_NAME=${MCS_AGENT_IMAGE_NAME:-mcs-controller-manager}
    MEMBER_NET_AGENT_CRD_INSTALLER_IMAGE_NAME=${MEMBER_NET_AGENT_CRD_INSTALLER_IMAGE_NAME:-net-crd-installer}
    COMMON_NETWORKING_IMAGE_TAG=${COMMON_NETWORKING_IMAGE_TAG:-experimental}
fi

while true; do
    # Retrieve a cluster name from the work queue.
    echo "Retrieving cluster name from the work queue..."
    CLUSTER_IDX=$(python3 dequeue.py)
    if [ -z "$CLUSTER_IDX" ]; then
        echo "No more clusters to create. Exiting."
        break
    fi
    VCLUSTER_NAME="vcluster-$CLUSTER_IDX"

    # Set up a service account for the member cluster in the hub cluster.
    echo "Setting up service account for member cluster $VCLUSTER_NAME in the hub cluster..."
    kubectl create serviceaccount "fleet-member-agent-$VCLUSTER_NAME" -n fleet-system --context "$HUB_CLUSTER_NAME"
cat <<EOF | kubectl apply --context "$HUB_CLUSTER_NAME" -f -
apiVersion: v1
kind: Secret
metadata:
    name: fleet-member-agent-$VCLUSTER_NAME-sa
    namespace: fleet-system
    annotations:
        kubernetes.io/service-account.name: fleet-member-agent-$VCLUSTER_NAME
type: kubernetes.io/service-account-token
EOF

    echo "Retrieving the service account token for member cluster $VCLUSTER_NAME..."
    kubectl wait secret "fleet-member-agent-$VCLUSTER_NAME-sa" -n fleet-system --context "$HUB_CLUSTER_NAME" --for=jsonpath='{.data.token}' --timeout=300s
    TOKEN=$(kubectl get secret "fleet-member-agent-$VCLUSTER_NAME-sa" -n fleet-system --context "$HUB_CLUSTER_NAME" -o jsonpath='{.data.token}' | base64 --decode)

    echo "Setting up MemberCluster CR in the hub cluster..."
cat <<EOF | kubectl apply --context "$HUB_CLUSTER_NAME" -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
    name: $VCLUSTER_NAME
spec:
    identity:
        name: fleet-member-agent-$VCLUSTER_NAME
        kind: ServiceAccount
        namespace: fleet-system
        apiGroup: ""
    heartbeatPeriodSeconds: 120
EOF
    
    export KUBECONFIG="$KUBECONFIG_DIR/$VCLUSTER_NAME.kubeconfig"

    echo "Installing the service account token secret in member cluster $VCLUSTER_NAME..."
    kubectl delete secret hub-kubeconfig-secret --ignore-not-found
    kubectl create secret generic hub-kubeconfig-secret --from-literal=token="$TOKEN"

    echo "Installing the member agent in member cluster $VCLUSTER_NAME..."
    pushd "$KUBEFLEET_SRC_REPO"
    helm upgrade member-agent charts/member-agent/ \
        --install \
        --set config.hubURL="$HUB_CLUSTER_API_SERVER_ADDR" \
        --set image.repository="$REGISTRY_NAME/$MEMBER_AGENT_IMAGE_NAME" \
        --set image.tag="$COMMON_CORE_IMAGE_TAG" \
        --set refreshtoken.repository="$REGISTRY_NAME/$REFRESH_TOKEN_IMAGE_NAME" \
        --set refreshtoken.tag="$COMMON_CORE_IMAGE_TAG" \
        --set image.pullPolicy=Always \
        --set refreshtoken.pullPolicy=Always \
        --set resources.requests.cpu=1 \
        --set resources.requests.memory=1Gi \
        --set resources.limits.cpu=4 \
        --set resources.limits.memory=16Gi \
        --set config.memberClusterName="$VCLUSTER_NAME" \
        --set logVerbosity=2 \
        --set namespace=fleet-system \
        --set enableV1Beta1APIs=true \
        --set propertyProvider="$PROPERTY_PROVIDER" \
        --set priorityQueue.enabled=true

    popd

    if [ "$INSTALL_NETWORKING_AGENTS" = "true" ]; then
        echo "Installing fleet member networking agents..."
        pushd "$FLEET_NETWORKING_SRC_REPO"
        echo "Installing the fleet networking CRDs..."
        kubectl apply -f config/crd/*
        helm upgrade fleet-networking charts/member-net-controller-manager \
            --install \
            --set image.repository="$REGISTRY_NAME/$MEMBER_NET_AGENT_IMAGE_NAME" \
            --set image.tag="$COMMON_NETWORKING_IMAGE_TAG" \
            --set image.pullPolicy=Always \
            --set config.hubURL="$HUB_CLUSTER_API_SERVER_ADDR" \
            --set config.memberClusterName="$VCLUSTER_NAME" \
            --set resources.requests.cpu=0.1 \
            --set resources.requests.memory=128Mi \
            --set resources.limits.cpu=1 \
            --set resources.limits.memory=512Mi \
            --set logVerbosity=2 \
            --set namespace=fleet-system \
            --set refreshtoken.repository="$REGISTRY_NAME/$REFRESH_TOKEN_IMAGE_NAME" \
            --set refreshtoken.tag="$COMMON_CORE_IMAGE_TAG" \
            --set refreshtoken.pullPolicy=Always \
            --set enableNetworkingFeatures=false

        helm upgrade fleet-networking-mcs charts/mcs-controller-manager \
            --install \
            --set image.repository="$REGISTRY_NAME/$MCS_AGENT_IMAGE_NAME" \
            --set image.tag="$COMMON_NETWORKING_IMAGE_TAG" \
            --set image.pullPolicy=Always \
            --set config.hubURL="$HUB_CLUSTER_API_SERVER_ADDR" \
            --set config.memberClusterName="$VCLUSTER_NAME" \
            --set resources.requests.cpu=0.1 \
            --set resources.requests.memory=128Mi \
            --set resources.limits.cpu=1 \
            --set resources.limits.memory=512Mi \
            --set logVerbosity=2 \
            --set namespace=fleet-system \
            --set refreshtoken.repository="$REGISTRY_NAME/$REFRESH_TOKEN_IMAGE_NAME" \
            --set refreshtoken.tag="$COMMON_CORE_IMAGE_TAG" \
            --set refreshtoken.pullPolicy=Always \
            --set enableNetworkingFeatures=false
        popd
    fi
done
