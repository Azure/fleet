#!/usr/bin/env bash

# This script is provided as a utility to easily stop the test environment when running the
# suites locally.

set -o errexit
set -o nounset
set -o pipefail

HUB_CLUSTER="hub"
MEMBER_CLUSTER_1="bravelion"
MEMBER_CLUSTER_2="singingbutterfly"
MEMBER_CLUSTER_3="smartfish"
declare -a ALL_CLUSTERS=($HUB_CLUSTER $MEMBER_CLUSTER_1 $MEMBER_CLUSTER_2 $MEMBER_CLUSTER_3)

for i in "${ALL_CLUSTERS[@]}"
do
    kind delete cluster --name "$i"
done

