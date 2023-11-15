#!/usr/bin/env bash

# This script is provided as a utility to easily stop the test environment when running the
# suites locally.

set -o errexit
set -o nounset
set -o pipefail

HUB_CLUSTER="hub"
MEMBER_CLUSTER_COUNT=$1
declare -a ALL_CLUSTERS=(HUB_CLUSTER)

for (( i=1;i<=MEMBER_CLUSTER_COUNT;i++ ))
do
  ALL_CLUSTERS+=("cluster-$i")
done

for i in "${ALL_CLUSTERS[@]}"
do
    kind delete cluster --name "$i"
done
