# CAN ONLY BE RUN AFTER CREATING NEEDED AKS CLUSTERS AND HUB CLUSTER. This script creates member clusters from
# AKS Cluster's and joins them onto the hub cluster.

export HUB_CLUSTER="$1"
export HUB_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")
export HUB_CLUSTER_ADDRESS=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$HUB_CLUSTER\")].cluster.server}")

for MC in "${@:2}"; do

# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MC\")].name}")
export MEMBER_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MC\")].name}")

export SERVICE_ACCOUNT="$MEMBER_CLUSTER-hub-cluster-access"

#echo "Switching into hub cluster context..."
kubectl config use-context $HUB_CLUSTER_CONTEXT
# The service account can, in theory, be created in any namespace; for simplicity reasons,
# here you will use the namespace reserved by Fleet installation, `fleet-system`.
#
# Note that if you choose a different value, commands in some steps below need to be
# modified accordingly.
echo "Creating member service account..."
kubectl create serviceaccount $SERVICE_ACCOUNT -n fleet-system

echo "Creating member service account secret..."
export SERVICE_ACCOUNT_SECRET="$MEMBER_CLUSTER-hub-cluster-access-token"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
    name: $SERVICE_ACCOUNT_SECRET
    namespace: fleet-system
    annotations:
        kubernetes.io/service-account.name: $SERVICE_ACCOUNT
type: kubernetes.io/service-account-token
EOF

echo "Creating member cluster CR..."
export TOKEN="$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n fleet-system -o jsonpath='{.data.token}' | base64 --decode)"
cat <<EOF | kubectl apply -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
    name: $MEMBER_CLUSTER
spec:
    identity:
        name: $MEMBER_CLUSTER-hub-cluster-access
        kind: ServiceAccount
        namespace: fleet-system
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
export FLEET_VERSION="${FLEET_VERSION:-$(curl "https://api.github.com/repos/Azure/fleet/tags" | jq -r '.[0].name')}"
export MEMBER_AGENT_IMAGE="member-agent"
export REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_NAME:-refresh-token}"
export OUTPUT_TYPE="${OUTPUT_TYPE:-type=docker}"

echo "Switching to member cluster context.."
kubectl config use-context $MEMBER_CLUSTER_CONTEXT

# Create the secret with the token extracted previously for member agent to use.
echo "Creating secret..."
kubectl delete secret hub-kubeconfig-secret
kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN

echo "Uninstalling member-agent..."
helm uninstall member-agent --wait

echo "Installing member-agent..."
helm install member-agent charts/member-agent/ \
        --set config.hubURL=$HUB_CLUSTER_ADDRESS  \
        --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
        --set image.tag=$FLEET_VERSION \
        --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
        --set refreshtoken.tag=$FLEET_VERSION \
        --set image.pullPolicy=Always \
        --set refreshtoken.pullPolicy=Always \
        --set config.memberClusterName=$MEMBER_CLUSTER \
        --set logVerbosity=5 \
        --set namespace=fleet-system \
        --set enableV1Beta1APIs=true

kubectl get pods -A
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
done
