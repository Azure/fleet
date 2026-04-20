#!/bin/bash
set -e

# Check the required environment variables.
RESOURCE_GROUP_NAME=${RESOURCE_GROUP_NAME:?Environment variable RESOURCE_GROUP_NAME is not set}
LOCATION=${LOCATION:?Environment variable LOCATION is not set}
REGISTRY_NAME=${REGISTRY_NAME:?Environment variable REGISTRY_NAME is not set}
REGISTRY_NAME_WO_SUFFIX=${REGISTRY_NAME_WO_SUFFIX:?Environment variable REGISTRY_NAME_WO_SUFFIX is not set}
HUB_CLUSTER_NAME=${HUB_CLUSTER_NAME:-hub}
HUB_CLUSTER_NODE_COUNT=${HUB_CLUSTER_NODE_COUNT:-2}
HUB_CLUSTER_VM_SIZE=${HUB_CLUSTER_VM_SIZE:-Standard_D16s_v3}
CUSTOM_TAGS=${CUSTOM_TAGS:-perf_test=true}

HUB_AGENT_IMAGE_NAME=${HUB_AGENT_IMAGE_NAME:-hub-agent}
HUB_AGENT_CRD_INSTALLER_IMAGE_NAME=${HUB_AGENT_CRD_INSTALLER_IMAGE_NAME:-crd-installer}
COMMON_CORE_IMAGE_TAG=${COMMON_CORE_IMAGE_TAG:-experimental}

KUBEFLEET_SRC_REPO=${KUBEFLEET_SRC_REPO:?Environment variable KUBEFLEET_SRC_REPO is not set}

INSTALL_NETWORKING_AGENTS=${INSTALL_NETWORKING_AGENTS:-true}
INSTALL_NETWORKING_AGENTS_HELM_FLAG_VALUE="false"
if [ "$INSTALL_NETWORKING_AGENTS" = "true" ]; then
    FLEET_NETWORKING_SRC_REPO=${FLEET_NETWORKING_SRC_REPO:?Environment variable FLEET_NETWORKING_SRC_REPO is not set}
    HUB_NET_AGENT_IMAGE_NAME=${HUB_NET_AGENT_IMAGE_NAME:-hub-net-controller-manager}
    HUB_NET_AGENT_CRD_INSTALLER_IMAGE_NAME=${HUB_NET_AGENT_CRD_INSTALLER_IMAGE_NAME:-net-crd-installer}
    COMMON_NETWORKING_IMAGE_TAG=${COMMON_NETWORKING_IMAGE_TAG:-experimental}

    INSTALL_NETWORKING_AGENTS_HELM_FLAG_VALUE="true"
fi


echo "Creating the AKS cluster $HUB_CLUSTER_NAME in resource group $RESOURCE_GROUP_NAME..."
az aks create \
    --resource-group "$RESOURCE_GROUP_NAME" \
    --name "$HUB_CLUSTER_NAME" \
    --location "$LOCATION" \
    --node-count "$HUB_CLUSTER_NODE_COUNT" \
    --node-vm-size "$HUB_CLUSTER_VM_SIZE" \
    --enable-aad \
    --enable-azure-rbac \
    --tier standard \
    --network-plugin azure \
    --attach-acr "$REGISTRY_NAME_WO_SUFFIX" \
    --tags "$CUSTOM_TAGS"

# Retrieve the hub cluster credential.
echo "Retrieving the credential for hub cluster $HUB_CLUSTER_NAME..."
az aks get-credentials --resource-group "$RESOURCE_GROUP_NAME" --name "$HUB_CLUSTER_NAME"

# Install the hub agent.
kubectl config use-context "$HUB_CLUSTER_NAME"

echo "Installing the hub agent in cluster $HUB_CLUSTER_NAME..."
pushd "$KUBEFLEET_SRC_REPO"
helm upgrade hub-agent charts/hub-agent/ \
    --install \
    --set image.pullPolicy=Always \
    --set "image.repository=$REGISTRY_NAME/$HUB_AGENT_IMAGE_NAME" \
    --set "image.tag=$COMMON_CORE_IMAGE_TAG" \
    --set resources.requests.cpu=1 \
    --set resources.requests.memory=1Gi \
    --set resources.limits.cpu=12 \
    --set resources.limits.memory=24Gi \
    --set namespace=fleet-system \
    --set logVerbosity=2 \
    --set logFileMaxSize=100000 \
    --set enableWebhook=true \
    --set enableGuardRail=true \
    --set enableWorkload=false \
    --set webhookClientConnectionType=service \
    --set forceDeleteWaitTime="5m0s" \
    --set clusterUnhealthyThreshold="3m0s" \
    --set networkingAgentsEnabled="$INSTALL_NETWORKING_AGENTS_HELM_FLAG_VALUE"

popd

# Install the Kubernetes Prometheus monitoring stack.
#
# Note: if you see 401 Forbidden errors when trying to access the chart, add the Helm chart repository
# with the command `helm repo add prometheus-community https://prometheus-community.github.io/helm-charts`
# and install the chart with `helm upgrade kube-prometheus-stack prometheus-community/kube-prometheus-stack ...`
# instead.
echo "Installing the Kubernetes Prometheus monitoring stack in cluster $HUB_CLUSTER_NAME..."
helm upgrade kube-prometheus-stack oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack \
    --version 82.15.1 \
    --install \
    -n monitoring \
    --create-namespace \
    --set prometheus.prometheusSpec.scrapeConfigSelectorNilUsesHelmValues=true \
    --set-json 'prometheus.prometheusSpec.scrapeConfigSelector={"matchLabels":{"prom": "monitoring"}}'

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: fleet-metrics
  namespace: fleet-system
spec:
  selector:
    app.kubernetes.io/name: hub-agent
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
  type: ClusterIP
EOF

cat <<EOF | kubectl apply -f -
apiVersion: monitoring.coreos.com/v1alpha1
kind: ScrapeConfig
metadata:
  name: fleet-metrics-scrape-config
  namespace: monitoring
  labels:
    prom: monitoring
spec:
  staticConfigs:
  - targets:
    - fleet-metrics.fleet-system.svc.cluster.local:8080
EOF

if [ "$INSTALL_NETWORKING_AGENTS" = "true" ]; then
    echo "Installing the fleet hub networking agent..."
    pushd "$FLEET_NETWORKING_SRC_REPO"
    echo "Installing the fleet networking CRDs..."
    kubectl apply -f config/crd/*
    helm upgrade fleet-networking charts/hub-net-controller-manager \
        --install \
        --set "image.repository=$REGISTRY_NAME/$HUB_NET_AGENT_IMAGE_NAME" \
        --set "image.tag=$COMMON_NETWORKING_IMAGE_TAG" \
        --set image.pullPolicy=Always \
        --set resources.requests.cpu=0.1 \
        --set resources.requests.memory=128Mi \
        --set resources.limits.cpu=1 \
        --set resources.limits.memory=1Gi \
        --set logVerbosity=2 \
        --set namespace=fleet-system

    popd
fi
