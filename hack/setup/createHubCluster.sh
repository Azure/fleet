# This script creates a Hub CLuster from an AKS Cluster (AKS Cluster and Container Registry must be created beforehand).
kubectl config use-context $HUB_CLUSTER_CONTEXT

# Build the hub agent image
echo "Retrieving hub-agent image..."
export REGISTRY="${REGISTRY:-mcr.microsoft.com/aks/fleet}"
export TAG="${TAG:-$(curl "https://api.github.com/repos/Azure/fleet/tags" | jq -r '.[0].name')}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"


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
  --set hubAPIQPS=100 \
  --set hubAPIBurst=1000 \
  --set logFileMaxSize=100000000 \
  --set MaxFleetSizeSupported=100

# Check the status of the hub agent
kubectl get pods -n fleet-system

echo "Installing prometheus endpoint..."
# Update prometheus and grafana to the hub cluster
helm repo update

# Install prometheus fleet metrics
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