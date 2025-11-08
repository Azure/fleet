#!/usr/bin/env bash

set -o errexit

# Perform some checks.
bash -c "which kubectl > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "The Kubernetes CLI tool, kubectl, is not installed in the system."
  #exit 1
fi

bash -c "which base64 > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "Utility base64 is not installed in the system."
  #exit 1
fi

bash -c "which curl > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "Utility curl is not installed in the system."
  #exit 1
fi

bash -c "which jq > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "Utility jq is not installed in the system."
  #exit 1
fi

bash -c "which helm > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "The Kubernetes package manager, Helm, is not installed in the system."
  #exit 1
fi

# Verify that the required environment variables are set.
[ -z "$HUB_CLUSTER_CONTEXT" ] && echo "Environment variable HUB_CLUSTER_CONTEXT is not set." #&& exit 1
bash -c "kubectl config get-contexts $HUB_CLUSTER_CONTEXT > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "The context $HUB_CLUSTER_CONTEXT does not exist."
  #exit 1
fi

[ -z "$HUB_CLUSTER_ADDRESS" ] && echo "Environment variable HUB_CLUSTER_ADDRESS is not set." #&& exit 1

[ -z "$MEMBER_CLUSTER" ] && echo "Environment variable MEMBER_CLUSTER is not set." #&& exit 1

[ -z "$MEMBER_CLUSTER_CONTEXT" ] && echo "Environment variable MEMBER_CLUSTER_CONTEXT is not set; will use the value of MEMBER_CLUSTER instead." #&& exit 1
bash -c "kubectl config get-contexts $MEMBER_CLUSTER_CONTEXT > /dev/null 2>&1"
if [ $? -ne 0 ];
then
  echo "The context $MEMBER_CLUSTER_CONTEXT does not exist."
  #exit 1
fi

REGISTRY="${REGISTRY:-mcr.microsoft.com/aks/fleet}"
FLEET_VERSION="${FLEET_VERSION:-$(curl "https://api.github.com/repos/Azure/fleet/tags" | jq -r '.[0].name')}"
MEMBER_AGENT_IMAGE="${MEMBER_AGENT_NAME:-member-agent}"
REFRESH_TOKEN_IMAGE="${REFRESH_TOKEN_NAME:-refresh-token}"

echo "docker pull images and load to kind"
docker image pull --platform linux/amd64 $REGISTRY/$MEMBER_AGENT_IMAGE:$FLEET_VERSION
docker image pull --platform linux/amd64 $REGISTRY/$REFRESH_TOKEN_IMAGE:$FLEET_VERSION
kind load docker-image --name $MEMBER_CLUSTER $REGISTRY/$MEMBER_AGENT_IMAGE:$FLEET_VERSION
kind load docker-image --name $MEMBER_CLUSTER $REGISTRY/$REFRESH_TOKEN_IMAGE:$FLEET_VERSION

echo "Preparing to join a cluster into a fleet..."
kubectl config use-context $HUB_CLUSTER_CONTEXT

echo "Setting up a service account..."

export SERVICE_ACCOUNT="$MEMBER_CLUSTER-hub-cluster-access"
kubectl delete serviceaccount $SERVICE_ACCOUNT -n fleet-system  --ignore-not-found
kubectl create serviceaccount $SERVICE_ACCOUNT -n fleet-system

echo "Creating a secret..."

export SERVICE_ACCOUNT_SECRET="$MEMBER_CLUSTER-hub-cluster-access-token"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
    name: "$SERVICE_ACCOUNT_SECRET"
    namespace: fleet-system
    annotations:
        kubernetes.io/service-account.name: "$SERVICE_ACCOUNT"
type: kubernetes.io/service-account-token
EOF

export TOKEN=$(kubectl get secret $SERVICE_ACCOUNT_SECRET -n fleet-system -o jsonpath='{.data.token}' | base64 -d)

echo "Registering the member cluster with the hub cluster..."
cat <<EOF | kubectl apply -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
    name: $MEMBER_CLUSTER
spec:
    identity:
        name: $SERVICE_ACCOUNT
        kind: ServiceAccount
        namespace: fleet-system
        apiGroup: ""
    heartbeatPeriodSeconds: 15
EOF

echo "Installing the member agent..."

kubectl config use-context $MEMBER_CLUSTER_CONTEXT
kubectl delete secret hub-kubeconfig-secret --ignore-not-found --wait
kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN
helm uninstall member-agent --ignore-not-found --wait
helm install member-agent charts/member-agent/ \
    --set config.hubURL=$HUB_CLUSTER_ADDRESS \
    --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
    --set image.tag=$FLEET_VERSION \
    --set image.pullPolicy=Never \
    --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
    --set refreshtoken.tag=$FLEET_VERSION \
    --set refreshtoken.pullPolicy=Never \
    --set config.memberClusterName="$MEMBER_CLUSTER" \
    --set logVerbosity=6 \
    --set namespace=fleet-system \
    --set enableV1Beta1APIs=true
