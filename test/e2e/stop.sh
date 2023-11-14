#!/usr/bin/env bash

# This script is provided as a utility to easily stop the test environment when running the
# suites locally.

set -o errexit
set -o nounset
set -o pipefail

HUB_CLUSTER="hub"
MEMBER_CLUSTER_1="cluster-1"
MEMBER_CLUSTER_2="cluster-2"
MEMBER_CLUSTER_3="cluster-3"
MEMBER_CLUSTER_COUNT=$1

if (( $MEMBER_CLUSTER_COUNT == 1 ))
then
  declare -a ALL_CLUSTERS=($HUB_CLUSTER $MEMBER_CLUSTER_1)
else
  declare -a ALL_CLUSTERS=($HUB_CLUSTER $MEMBER_CLUSTER_1 $MEMBER_CLUSTER_2 $MEMBER_CLUSTER_3)
fi

for i in "${ALL_CLUSTERS[@]}"
do
    kind delete cluster --name "$i"
done
