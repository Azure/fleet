# CAN ONLY BE RUN AFTER CREATING NEEDED AKS CLUSTERS AND HUB CLUSTER. This script creates member clusters from
# AKS Cluster's and joins them onto the hub cluster.

# Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig context you use for
# accessing your hub cluster.
export HUB_CLUSTER_CONTEXT=<hub-cluster-context>
# Replace the value of HUB_CLUSTER_ADDRESS with the address of your hub cluster API server.
export HUB_CLUSTER_ADDRESS=<hub-cluster-address>

for i in {0..99}; do # change to how many clusters you want to join

# Replace the value of MEMBER_CLUSTER with the name you would like to assign to the new member
# cluster.
#
# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER=<member-cluster-name>-${i}
# Replace the value of MEMBER_CLUSTER_CONTEXT with the name of the kubeconfig context you use
# for accessing your member cluster.
export MEMBER_CLUSTER_CONTEXT=<member-cluster-context>-${i}
# Replace the value of HUB_CLUSTER_CONTEXT with the name of the kubeconfig
# context you use for accessing your hub cluster.

export SERVICE_ACCOUNT="$MEMBER_CLUSTER-hub-cluster-access"

kubectl config use-context $HUB_CLUSTER_CONTEXT
# The service account can, in theory, be created in any namespace; for simplicity reasons,
# here you will use the namespace reserved by Fleet installation, `fleet-system`.
#
# Note that if you choose a different value, commands in some steps below need to be
# modified accordingly.
kubectl create serviceaccount $SERVICE_ACCOUNT -n fleet-system

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

export TOKEN=$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n fleet-system -o jsonpath='{.data.token}' | base64 -d)

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
    heartbeatPeriodSeconds: 60
EOF

# # Clone the Fleet repository from GitHub (If not done so already and go into directory).
# git clone https://github.com/Azure/fleet.git
# cd fleet

# # Install the member agent helm chart on the member cluster.

# The variables below uses the Fleet images kept in the Microsoft Container Registry (MCR),
# and will retrieve the latest version from the Fleet GitHub repository.
#
# You can, however, build the Fleet images of your own; see the repository README for
# more information.
export REGISTRY="${REGISTRY:-mcr.microsoft.com/aks/fleet}"
export FLEET_VERSION="v0.9.1"
export MEMBER_AGENT_IMAGE="member-agent"
export REFRESH_TOKEN_IMAGE="refresh-token"

kubectl config use-context $MEMBER_CLUSTER_CONTEXT

# Create the secret with the token extracted previously for member agent to use.
kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN

helm install member-agent fleet/charts/member-agent/ \
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
        --set enableV1Alpha1APIs=false \
        --set enableV1Beta1APIs=true

kubectl get pods -A
kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl get membercluster $MEMBER_CLUSTER
done