package v1alpha1

const (
	fleetPrefix = "kubernetes-fleet.io/"

	// ClusterResourceOverrideKind is the kind of the ClusterResourceOverride.
	ClusterResourceOverrideKind = "ClusterResourceOverride"

	// ClusterResourceOverrideSnapshotKind is the kind of the ClusterResourceOverrideSnapshot.
	ClusterResourceOverrideSnapshotKind = "ClusterResourceOverrideSnapshot"

	// ResourceOverrideKind is the kind of the ResourceOverride.
	ResourceOverrideKind = "ResourceOverride"

	// ResourceOverrideSnapshotKind is the kind of the ResourceOverrideSnapshot.
	ResourceOverrideSnapshotKind = "ResourceOverrideSnapshot"

	// OverrideClusterNameVariable is the reserved variable in the override value that will be replaced by the actual cluster name.
	OverrideClusterNameVariable = "${MEMBER-CLUSTER-NAME}"

	// OverrideClusterLabelKeyVariablePrefix is a reserved variable in the override expression.
	// We use this variable to find the associated the key following the prefix.
	// The key name ends with a "}" character (but not include it).
	// The key name must be a valid Kubernetes label name and case-sensitive.
	// The content of the string containing this variable will be replaced by the actual label value on the member cluster.
	// For example, if the string is "${MEMBER-CLUSTER-LABEL-KEY-kube-fleet.io/region}" then the key name is "kube-fleet.io/region".
	// If there is a label "kube-fleet.io/region": "us-west-1" on the member cluster, this string will be replaced by "us-west-1".
	OverrideClusterLabelKeyVariablePrefix = "${MEMBER-CLUSTER-LABEL-KEY-"
)
