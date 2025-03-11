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
	// The string will be replaced by the actual label value with the given the name of the key following the prefix.
	// The key name ends with a "}" character.
	// The key name has a maximum length of 63 characters and must be a valid label name.
	// For example, if the key name is "region", the variable will be "${MEMBER-CLUSTER-LABEL-KEY-region}"
	OverrideClusterLabelKeyVariablePrefix = "${MEMBER-CLUSTER-LABEL-KEY-"
)
