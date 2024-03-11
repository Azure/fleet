# This script creates a Hub CLuster from an AKS Cluster (AKS Cluster and Container Registry must be created beforehand).

# Go into fleet directory
cd fleet

# Build the hub agent image
echo "Building hub-agent image..."
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"
make docker-build-hub-agent

# Check if the image is built
docker images

echo "Logging into registry..."
az acr login -n $REGISTRY

echo "Pushing image to registry..."
# Push the image to the registry
docker push $REGISTRY.azurecr.io/hub-agent:$TAG

echo "Attaching registry to AKS Cluster..."
# Attach acr to the cluster
az aks update -n $CLUSTER_NAME -g $RESOURCE_GROUP --attach-acr $REGISTRY

echo "Installing hub-agent..."
# Install the hub agent helm chart on the hub cluster
helm install hub-agent charts/hub-agent/ \
  --set image.pullPolicy=Always \
  --set image.repository=$REGISTRY.azurecr.io/hub-agent \
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

echo "Installing prometheus endpoint..."
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