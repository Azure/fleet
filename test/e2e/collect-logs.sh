#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Configuration
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
MEMBER_CLUSTER_COUNT="${1:-3}"
NAMESPACE="fleet-system"
LOG_DIR="${LOG_DIR:-logs}"
TIMESTAMP=$(date '+%Y%m%d-%H%M%S')
LOG_DIR="${LOG_DIR}/${TIMESTAMP}"

# Cluster names
HUB_CLUSTER="hub"
declare -a MEMBER_CLUSTERS=()

for (( i=1;i<=MEMBER_CLUSTER_COUNT;i++ ))
do
  MEMBER_CLUSTERS+=("cluster-$i")
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Create log directory
mkdir -p "${LOG_DIR}"

echo -e "${GREEN}Starting log collection at ${TIMESTAMP}${NC}"
echo "Logs will be saved to: ${LOG_DIR}"
echo ""

# Function to collect logs from a pod
collect_pod_logs() {
    local pod_name=$1
    local cluster_name=$2
    local log_file_prefix=$3
    
    echo -e "${YELLOW}Collecting logs from pod ${pod_name} in cluster ${cluster_name}${NC}"
    
    # Get all containers in the pod
    containers=$(kubectl get pod "${pod_name}" -n "${NAMESPACE}" -o jsonpath='{.spec.containers[*].name}' 2>/dev/null || echo "")
    
    if [ -z "$containers" ]; then
        echo -e "${RED}No containers found in pod ${pod_name}${NC}"
        return
    fi
    
    # Collect logs for each container
    for container in $containers; do
        log_file="${log_file_prefix}-${container}.log"
        echo "  - Container ${container} -> ${log_file}"
        
        # Get current logs
        kubectl logs "${pod_name}" -n "${NAMESPACE}" -c "${container}" > "${log_file}" 2>&1 || \
            echo "Failed to get logs for container ${container}" > "${log_file}"
        
        # Try to get previous logs if pod was restarted
        previous_log_file="${log_file_prefix}-${container}-previous.log"
        if kubectl logs "${pod_name}" -n "${NAMESPACE}" -c "${container}" --previous > "${previous_log_file}" 2>&1; then
            echo "  - Previous logs for ${container} -> ${previous_log_file}"
        else
            rm -f "${previous_log_file}"
        fi
    done
}

# Collect hub cluster logs
echo -e "${GREEN}=== Collecting Hub Cluster Logs ===${NC}"
kind export kubeconfig --name "${HUB_CLUSTER}" 2>/dev/null || {
    echo -e "${RED}Failed to export kubeconfig for hub cluster${NC}"
    exit 1
}

# Create hub logs directory
HUB_LOG_DIR="${LOG_DIR}/hub"
mkdir -p "${HUB_LOG_DIR}"

# Get all hub-agent pods
hub_pods=$(kubectl get pods -n "${NAMESPACE}" -l app.kubernetes.io/name=hub-agent -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

if [ -z "$hub_pods" ]; then
    echo -e "${RED}No hub-agent pods found${NC}"
else
    for pod in $hub_pods; do
        collect_pod_logs "${pod}" "${HUB_CLUSTER}" "${HUB_LOG_DIR}/${pod}"
    done
fi

# Collect member cluster logs
for cluster in "${MEMBER_CLUSTERS[@]}"; do
    echo -e "${GREEN}=== Collecting Member Cluster Logs: ${cluster} ===${NC}"
    
    # Export kubeconfig for the member cluster
    if ! kind export kubeconfig --name "${cluster}" 2>/dev/null; then
        echo -e "${RED}Failed to export kubeconfig for cluster ${cluster}, skipping...${NC}"
        continue
    fi
    
    # Create member logs directory
    MEMBER_LOG_DIR="${LOG_DIR}/${cluster}"
    mkdir -p "${MEMBER_LOG_DIR}"
    
    # Get all member-agent pods
    member_pods=$(kubectl get pods -n "${NAMESPACE}" -l app.kubernetes.io/name=member-agent -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -z "$member_pods" ]; then
        echo -e "${RED}No member-agent pods found in cluster ${cluster}${NC}"
    else
        for pod in $member_pods; do
            collect_pod_logs "${pod}" "${cluster}" "${MEMBER_LOG_DIR}/${pod}"
        done
    fi
    
    echo ""
done

# Collect additional debugging information
echo -e "${GREEN}=== Collecting Additional Debug Information ===${NC}"

# Hub cluster debug info
kind export kubeconfig --name "${HUB_CLUSTER}" 2>/dev/null
{
    echo "=== Hub Cluster Pod Status ==="
    kubectl get pods -n "${NAMESPACE}" -o wide
    echo ""
    echo "=== Hub Cluster Events ==="
    kubectl get events -n "${NAMESPACE}" --sort-by='.lastTimestamp'
} > "${LOG_DIR}/hub-debug-info.txt" 2>&1

# Member clusters debug info
for cluster in "${MEMBER_CLUSTERS[@]}"; do
    if kind export kubeconfig --name "${cluster}" 2>/dev/null; then
        {
            echo "=== ${cluster} Pod Status ==="
            kubectl get pods -n "${NAMESPACE}" -o wide
            echo ""
            echo "=== ${cluster} Events ==="
            kubectl get events -n "${NAMESPACE}" --sort-by='.lastTimestamp'
        } > "${LOG_DIR}/${cluster}-debug-info.txt" 2>&1
    fi
done

# Create a summary file
echo -e "${GREEN}=== Creating Summary ===${NC}"
{
    echo "Log Collection Summary"
    echo "====================="
    echo "Timestamp: ${TIMESTAMP}"
    echo "Hub Cluster: ${HUB_CLUSTER}"
    echo "Member Clusters: ${MEMBER_CLUSTERS[*]}"
    echo ""
    echo "Directory Structure:"
    find "${LOG_DIR}" -type f -name "*.log" -o -name "*.txt" | sort
} > "${LOG_DIR}/summary.txt"

echo ""
echo -e "${GREEN}Log collection completed!${NC}"
echo "All logs saved to: ${LOG_DIR}"
echo ""
echo "To view the summary:"
echo "  cat ${LOG_DIR}/summary.txt"
echo ""
echo "To search across all logs:"
echo "  grep -r 'ERROR' ${LOG_DIR}"
