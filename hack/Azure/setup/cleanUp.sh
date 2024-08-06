export CONNECT_TO_FLEET=connect-to-fleet

export HUB_CLUSTER="$1"
export HUB_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")
export HUB_CLUSTER_ADDRESS=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$HUB_CLUSTER\")].cluster.server}")

for MC in "${@:2}"; do

# Note that Fleet will recognize your cluster with this name once it joins.
export MEMBER_CLUSTER=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MC\")].name}")
export MEMBER_CLUSTER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MC\")].name}")

kubectl config use-context $HUB_CLUSTER_CONTEXT
kubectl delete secret $MEMBER_CLUSTER-hub-cluster-access-token -n connect-to-fleet
kubectl delete serviceaccount $MEMBER_CLUSTER-hub-cluster-access -n connect-to-fleet
kubectl config use-context $MEMBER_CLUSTER_CONTEXT
helm uninstall member-agent
helm uninstall member-net-controller-manager
helm uninstall mcs-controller-manager
done