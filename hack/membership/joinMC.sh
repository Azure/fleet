# CAN ONLY BE RUN AFTER CREATING NEEDED AKS CLUSTERS AND HUB CLUSTER. This script creates member clusters for
# AKS Clusters and joins them onto the hub cluster.

# Perform validation to ensure the script can run correctly.

if [ "$#" -lt 3 ]; then
  echo "Usage: $0 <FLEET-IMAGE-TAG> <HUB-CLUSTER-NAME> <MEMBER-CLUSTER-NAME-1> [<MEMBER-CLUSTER-NAME-2> ...]"
  exit 1
fi

export IMAGE_TAG="$1"
if [[ $(curl "https://api.github.com/repos/Azure/fleet/tags") != *"$1"* ]] > /dev/null 2>&1; then
  echo "fleet image tag $1 does not exist"
  exit 1
fi

export HUB_CLUSTER="$2"
if [[ ! $(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")]}") ]] > /dev/null 2>&1; then
  echo "The cluster named $HUB_CLUSTER does not exist."
  exit 1
fi

for MEMBER_CLUSTER in "${@:3}"; do
if [[ ! $(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MEMBER_CLUSTER\")]}") ]] > /dev/null 2>&1; then
  echo "The cluster named $MEMBER_CLUSTER does not exist."
  exit 1
fi
done

# Steps to join the member clusters to the hub cluster.
export HUB_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")
export HUB_CLUSTER_ADDRESS=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$HUB_CLUSTER\")].cluster.server}")

echo "Switching into hub cluster context..."
kubectl config use-context $HUB_CLUSTER_CONTEXT

export NOT_FOUND="not found"
export CONNECT_TO_FLEET=connect-to-fleet

echo "Create namespace to host resources required to connect to fleet"
if [[ $NOT_FOUND == *$(kubectl get namespace $CONNECT_TO_FLEET)* ]]; then
  kubectl create namespace $CONNECT_TO_FLEET
else
  echo "namespace $CONNECT_TO_FLEET already exists"
fi

for MEMBER_CLUSTER in "${@:3}"; do
export SERVICE_ACCOUNT="$MEMBER_CLUSTER-hub-cluster-access"

# The service account can, in theory, be created in any namespace; for simplicity reasons,
# we create our own namespace `connect-to-fleet` to host the service account and the secret.
#
# Note that if you choose a different value, commands in some steps below need to be
# modified accordingly.
echo "Creating member service account..."
if [[ $NOT_FOUND == *$(kubectl get serviceaccount $SERVICE_ACCOUNT -n $CONNECT_TO_FLEET)* ]]; then
  kubectl create serviceaccount $SERVICE_ACCOUNT -n $CONNECT_TO_FLEET
else
  echo "member service account $SERVICE_ACCOUNT already exists in namespace $CONNECT_TO_FLEET"
fi

echo "Creating member service account secret..."
export SERVICE_ACCOUNT_SECRET="$MEMBER_CLUSTER-hub-cluster-access-token"
if [[ $NOT_FOUND == *$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n $CONNECT_TO_FLEET)* ]]; then
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
    name: $SERVICE_ACCOUNT_SECRET
    namespace: $CONNECT_TO_FLEET
    annotations:
        kubernetes.io/service-account.name: $SERVICE_ACCOUNT
type: kubernetes.io/service-account-token
EOF
else
  echo "member service account secret $SERVICE_ACCOUNT_SECRET already exists in namespace $CONNECT_TO_FLEET"
fi

echo "Extracting token from member service account secret..."
export TOKEN="$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n $CONNECT_TO_FLEET -o jsonpath='{.data.token}' | base64 --decode)"

echo "Creating member cluster CR..."
if [[ $NOT_FOUND == *$(kubectl get membercluster $MEMBER_CLUSTER)* ]]; then
cat <<EOF | kubectl apply -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
    name: $MEMBER_CLUSTER
spec:
    identity:
        name: $MEMBER_CLUSTER-hub-cluster-access
        kind: ServiceAccount
        namespace: $CONNECT_TO_FLEET
        apiGroup: ""
    heartbeatPeriodSeconds: 15
EOF
else
  echo "member cluster CR $MEMBER_CLUSTER already exists"
fi

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

# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MEMBER_CLUSTER\")].name}")
echo "Switching to member cluster context..."
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
        --set enableV1Beta1APIs=true

kubectl get pods -A
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
done
