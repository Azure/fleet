echo "Setting Subscription..."
export SUB=<subscription-id>

az account set -s ${SUB}

export RG_NAME=<resource-group-name>
export CLUSTER_NAME=<cluster-name>

echo "Retrieving AKS cluster credentials..."
az aks get-credentials --resource-group  ${RG_NAME} --name ${CLUSTER_NAME}

# Replace the value of HUB_CLUSTER_CONTEXT with the context and HUB_CLUSTER_ADDRESS with address of your hub cluster.
export HUB_CLUSTER_CONTEXT=<hub-cluster-context>
export HUB_CLUSTER_ADDRESS=<hub-cluster-address>

# Replace with the name of your registry and tag
export REGISTRY=<acr_name>.azurecr.io
export TAG=<tag>
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"


# Clone the Fleet repository from GitHub (if not done so already) and go into directory
git clone https://github.com/Azure/fleet.git
cd fleet

# Build the hub agent image
echo "Building hub-agent image..."
make docker-build-hub-agent

# Check if the image is built
docker images

echo "Logging into registry..."
# Replace <acr_name> with container registry name
az acr login -n <acr_name>

echo "Pushing image to registry..."
# Push the image to the registry
docker push <acr_name>.azurecr.io/hub-agent:<tag>

echo "Attaching registry to AKS Cluster..."
# Attach acr to the cluster
az aks update -n <cluster_name> -g <rg_name> --attach-acr <acr_name>

echo "Installing hub-agent..."
# Install the hub agent helm chart on the hub cluster
helm install hub-agent charts/hub-agent/ \
  --set image.pullPolicy=Always \
  --set image.repository=$REGISTRY/hub-agent \
  --set image.tag=$TAG \
  --set logVerbosity=2 \
  --set namespace=fleet-system \
  --set enableWebhook=false \
  --set webhookClientConnectionType=service \
  --set enableV1Alpha1APIs=false \
  --set enableV1Beta1APIs=true \
  --set resources.limits.cpu=4 \
  --set resources.limits.memory=4Gi \
  --set concurrentClusterPlacementSyncs=10 \
  --set ConcurrentRolloutSyncs=10 \
  --set ConcurrentResourceChangeSyncs=3 \
  --set hubAPIQPS=100 \
  --set hubAPIBurst=1000 \
  --set logFileMaxSize=100000000 \
  --set MaxFleetSizeSupported=100

# Check the status of the hub agent
kubectl get pods -n fleet-system

echo "Instlling prometheus endpoint..."
# Add prometheus and grafana to the hub cluster
helm repo add prometheus-community https://prometheus-community.github.io/helm-chart
helm repo update

# Install prometheus fleet testing metrics
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: fleet-prometheus-endpoint
  namespace: fleet-system
spec:
  selector:
    app.kubernetes.io/name: hub-agent
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
  type: LoadBalancer
EOF

# Check the status of the service
kubectl get service -n fleet-system