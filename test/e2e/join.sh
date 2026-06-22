#!/usr/bin/env bash

# This script joins member clusters to the hub cluster by creating MemberCluster CRs.
# It should be run after setup.sh has completed.

set -o errexit
set -o nounset
set -o pipefail

export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
MEMBER_CLUSTER_COUNT=$1

HUB_CLUSTER="hub"
declare -a MEMBER_CLUSTERS=()

for (( i=1;i<=MEMBER_CLUSTER_COUNT;i++ ))
do
  MEMBER_CLUSTERS+=("cluster-$i")
done

# Verify that the hub cluster exists.
if ! kind get clusters 2>/dev/null | grep -q "^${HUB_CLUSTER}$"; then
    echo "Error: Hub cluster '${HUB_CLUSTER}' not found. Run 'make setup-clusters' first."
    exit 1
fi

# Verify that the member clusters exist.
for i in "${MEMBER_CLUSTERS[@]}"
do
    if ! kind get clusters 2>/dev/null | grep -q "^${i}$"; then
        echo "Error: Member cluster '${i}' not found. Run 'make setup-clusters' first."
        exit 1
    fi
done

# Switch to the hub cluster context.
kind export kubeconfig --name "$HUB_CLUSTER"

# Verify that fleet-system namespace exists on the hub cluster.
if ! kubectl get namespace fleet-system &>/dev/null; then
    echo "Error: Namespace 'fleet-system' not found on hub cluster. Run 'make setup-clusters' first."
    exit 1
fi

# Create MemberCluster CRs for each member cluster.
echo "Creating MemberCluster CRs on the hub cluster..."

for i in "${MEMBER_CLUSTERS[@]}"
do
    echo "Creating MemberCluster CR for kind-${i}..."
    cat <<EOF | kubectl apply -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
  name: kind-${i}
spec:
  identity:
    name: fleet-member-agent-${i}
    kind: ServiceAccount
    namespace: fleet-system
    apiGroup: ""
  heartbeatPeriodSeconds: 15
EOF
done

# Wait for all member clusters to reach Joined state.
echo "Waiting for member clusters to join..."

TIMEOUT=120
for i in "${MEMBER_CLUSTERS[@]}"
do
    echo "Waiting for kind-${i} to join..."
    SECONDS=0
    while true; do
        JOINED=$(kubectl get membercluster "kind-${i}" -o jsonpath='{.status.conditions[?(@.type=="Joined")].status}' 2>/dev/null || echo "")
        if [ "$JOINED" = "True" ]; then
            echo "kind-${i} has joined successfully."
            break
        fi
        if [ $SECONDS -ge $TIMEOUT ]; then
            echo "Error: Timed out waiting for kind-${i} to join after ${TIMEOUT}s."
            kubectl get membercluster "kind-${i}" -o yaml 2>/dev/null || true
            exit 1
        fi
        sleep 2
    done
done

echo "All member clusters have joined the hub cluster."
