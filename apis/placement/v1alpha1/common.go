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

	// OverrideClusterLabelKeyVariablePrefix is the reserved variable prefix in the override expression.
	// The string will be replaced by the actual label value associated with the key following the prefix.
	// The key name ends with a "}" character (but not include it).
	// The key name must be a valid label name and case-sensitive.
	// For example, if the key name is "kube-fleet.io/region", the variable will be "${MEMBER-CLUSTER-LABEL-KEY-kube-fleet.io/region}"
	OverrideClusterLabelKeyVariablePrefix = "${MEMBER-CLUSTER-LABEL-KEY-"
)
