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

	// ClusterStagedUpdateRunKind is the kind of the ClusterStagedUpdateRun.
	ClusterStagedUpdateRunKind = "ClusterStagedUpdateRun"

	// ClusterStagedUpdateStrategyKind is the kind of the ClusterStagedUpdateStrategy.
	ClusterStagedUpdateStrategyKind = "ClusterStagedUpdateStrategy"

	// ClusterApprovalRequestKind is the kind of the ClusterApprovalRequest.
	ClusterApprovalRequestKind = "ClusterApprovalRequest"

	// ClusterStagedUpdateRunFinalizer is used by the ClusterStagedUpdateRun controller to make sure that the ClusterStagedUpdateRun
	// object is not deleted until all its dependent resources are deleted.
	ClusterStagedUpdateRunFinalizer = fleetPrefix + "stagedupdaterun-finalizer"

	// TargetUpdateRunLabel indicates the target update run on a staged run related object.
	TargetUpdateRunLabel = fleetPrefix + "targetupdaterun"

	// UpdateRunDeleteStageName is the name of delete stage in the staged update run.
	UpdateRunDeleteStageName = fleetPrefix + "deleteStage"

	// IsLatestUpdateRunApprovalLabel indicates if the approval is the latest approval on a staged run.
	IsLatestUpdateRunApprovalLabel = fleetPrefix + "isLatestUpdateRunApproval"

	// TargetUpdatingStageNameLabel indicates the updating stage name on a staged run related object.
	TargetUpdatingStageNameLabel = fleetPrefix + "targetUpdatingStage"

	// ApprovalTaskNameFmt is the format of the approval task name.
	ApprovalTaskNameFmt = "%s-%s"

	// OverrideClusterNameVariable is the reserved variable in the override value that will be replaced by the actual cluster name.
	OverrideClusterNameVariable = "${MEMBER-CLUSTER-NAME}"
)
