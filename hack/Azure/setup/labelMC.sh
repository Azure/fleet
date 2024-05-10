# CAN ONLY BE RUN AFTER CREATING NEEDED AKS CLUSTERS AND HUB CLUSTER. This script add labels to specified number of
# member clusters at random.

label=$1
count=$2

# Get the list of member clusters
clusters=($(kubectl get memberclusters -A -o jsonpath='{.items[*].metadata.name}'))

# Shuffle the indices
shuffled_indices=($(shuf -i 0-$((${#clusters[@]} - 1))))

# Label the specified number of clusters
for index in "${shuffled_indices[@]:0:$count}"; do
  cluster=${clusters[$index]}
  kubectl label membercluster "$cluster" "$label" --overwrite
  echo "Labeled $cluster with label: $label"
done
