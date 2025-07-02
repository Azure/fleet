#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

: "${KUBECONFIG:?Environment variable KUBECONFIG must be set}"

echo "Deleting all CRPs generated during the load test from the hub cluster..."
kubectl delete crp -l test=cl2-test
