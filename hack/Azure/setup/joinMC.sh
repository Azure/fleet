# CAN ONLY BE RUN AFTER CREATING NEEDED AKS CLUSTERS AND HUB CLUSTER. This script creates member clusters for
# AKS Clusters and joins them onto the fleet hub cluster.

export IMAGE_TAG="$1"

export HUB_CLUSTER="$2"
export HUB_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")
export HUB_CLUSTER_ADDRESS=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$HUB_CLUSTER\")].cluster.server}")

echo "Switching into hub cluster context..."
kubectl config use-context $HUB_CLUSTER_CONTEXT

echo "Delete existing namespace to host resources required to connect to fleet"
kubectl delete namespace connect-to-fleet --ignore-not-found=true

echo "Create namespace to host resources required to connect to fleet"
kubectl create namespace connect-to-fleet

for MC in "${@:3}"; do

# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MC\")].name}")
export MEMBER_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MC\")].name}")

export SERVICE_ACCOUNT="$MEMBER_CLUSTER-hub-cluster-access"

# The service account can, in theory, be created in any namespace; for simplicity reasons,
# we create our own namespace `connect-to-fleet` to host the service account and the secret.
#
# Note that if you choose a different value, commands in some steps below need to be
# modified accordingly.
echo "Creating member service account..."
kubectl create serviceaccount $SERVICE_ACCOUNT -n connect-to-fleet

echo "Creating member service account secret..."
export SERVICE_ACCOUNT_SECRET="$MEMBER_CLUSTER-hub-cluster-access-token"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
    name: $SERVICE_ACCOUNT_SECRET
    namespace: connect-to-fleet
    annotations:
        kubernetes.io/service-account.name: $SERVICE_ACCOUNT
type: kubernetes.io/service-account-token
EOF

echo "Creating member cluster CR..."
export TOKEN="$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n connect-to-fleet -o jsonpath='{.data.token}' | base64 --decode)"
cat <<EOF | kubectl apply -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
    name: $MEMBER_CLUSTER
spec:
    identity:
        name: $MEMBER_CLUSTER-hub-cluster-access
        kind: ServiceAccount
        namespace: connect-to-fleet
        apiGroup: ""
    heartbeatPeriodSeconds: 15
EOF

# # Install the member agent helm chart on the member cluster.

# The variables below uses the Fleet images kept in the Microsoft Container Registry (MCR),
# and will retrieve the latest version from the Fleet GitHub repository.
#
# You can, however, build the Fleet images of your own; see the repository README for
# more information.
echo "Retrieving image..."
export REGISTRY="mcr.microsoft.com/aks/fleet"
export MEMBER_AGENT_IMAGE="member-agent"
export REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_NAME:-refresh-token}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"

echo "Switching to member cluster context.."
kubectl config use-context $MEMBER_CLUSTER_CONTEXT

# Create the secret with the token extracted previously for member agent to use.
echo "Creating secret..."
kubectl delete secret hub-kubeconfig-secret --ignore-not-found=true
kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN

echo "Uninstalling member-agent..."
helm uninstall member-agent --wait

echo "Installing member-agent..."
helm install member-agent charts/member-agent/ \
        --set config.hubURL=$HUB_CLUSTER_ADDRESS  \
        --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
        --set image.tag=$IMAGE_TAG \
        --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
        --set refreshtoken.tag=$IMAGE_TAG \
        --set image.pullPolicy=Always \
        --set refreshtoken.pullPolicy=Always \
        --set config.memberClusterName=$MEMBER_CLUSTER \
        --set logVerbosity=8 \
        --set namespace=fleet-system \
        --set enableV1Alpha1APIs=false \
        --set enableV1Beta1APIs=true

kubectl get pods -A
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster -A
done
