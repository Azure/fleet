package v1alpha1

const (
	fleetPrefix = "kubernetes-fleet.io/"

	// ClusterResourceOverrideKind is the kind of the ClusterResourceOverride.
	ClusterResourceOverrideKind = "ClusterResourceOverride"

	// ClusterResourceOverrideSnapshotKind is the kind of the ClusterResourceOverrideSnapshot.
	ClusterResourceOverrideSnapshotKind = "ClusterResourceOverrideSnapshot"

	// ResourceOverrideKind is the kind of the ResourceOverride.
	ResourceOverrideKind = "ResourceOverride"

	// ResourceOverrideSnapshotKind is the kind of the ResourceOverrideSnapshotKind.
	ResourceOverrideSnapshotKind = "ResourceOverrideSnapshot"

	// StagedUpdateRunFinalizer is used by the staged update run controller to make sure that the stagedUpdateRun
	// object is not deleted until all its dependent resources are deleted.
	StagedUpdateRunFinalizer = fleetPrefix + "stagedupdaterun-finalizer"

	// TargetUpdateRunLabel is the label that indicates the target update run on a staged run related object.
	TargetUpdateRunLabel = fleetPrefix + "targetupdaterun"

	// The name of delete stage in the staged update run
	UpdateRunDeleteStageName = fleetPrefix + "deleteStage"

	// IsLatestUpdateRunApprovalLabel is the label that indicates if the apporavl is the latest  approval on a staged run.
	IsLatestUpdateRunApprovalLabel = fleetPrefix + "isLatestUpdateRunApproval"

	// UpdatingStageNameLabel is the label that indicates the updating stage name on a staged run related object.
	TargetUpdatingStageNameLabel = fleetPrefix + "targetUpdatingStage"

	// ApprovalTaskNameFmt is the format of the approval task name.
	ApprovalTaskNameFmt = "%s-%s"
)
