/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/annotations"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
)

var errInitializedFailed = fmt.Errorf("%w: failed to initialize the StagedUpdateRun", errStagedUpdatedAborted)

// initialize initializes the ClusterStagedUpdateRun object with all the stages computed during the initialization.
// This function is called only once during the initialization of the ClusterStagedUpdateRun.
func (r *Reconciler) initialize(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) ([]*placementv1beta1.ClusterResourceBinding, []*placementv1beta1.ClusterResourceBinding, error) {
	// Validate the ClusterResourcePlacement object referenced by the ClusterStagedUpdateRun
	placementName, err := r.validateCRP(ctx, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Record the latest policy snapshot associated with the ClusterResourcePlacement
	latestPolicySnapshot, _, err := r.determinePolicySnapshot(ctx, placementName, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Collect the scheduled clusters by the corresponding ClusterResourcePlacement with the latest policy snapshot
	scheduledBinding, tobeDeleted, err := r.collectScheduledClusters(ctx, placementName, latestPolicySnapshot, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Compute the stages based on the StagedUpdateStrategy
	if err = r.generateStageByStrategy(ctx, scheduledBinding, tobeDeleted, updateRun); err != nil {
		return nil, nil, err
	}
	// Record the override snapshots associated with each cluster
	if err = r.recordOverrideSnapshots(ctx, updateRun); err != nil {
		return nil, nil, err
	}
	// Update the ClusterStagedUpdateRun's initialized condition
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StagedUpdateRunConditionInitialized),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunInitializeSucceededReason,
		Message:            "update run initialized successfully",
	})
	return scheduledBinding, tobeDeleted, r.Client.Status().Update(ctx, updateRun)
}

// validateCRP validates the ClusterResourcePlacement object referenced by the ClusterStagedUpdateRun.
func (r *Reconciler) validateCRP(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (string, error) {
	updateRunRef := klog.KObj(updateRun)
	// Fetch the ClusterResourcePlacement object
	clusterResourcePlacementName := updateRun.Spec.PlacementName
	var clusterResourcePlacement placementv1beta1.ClusterResourcePlacement
	if err := r.Get(ctx, client.ObjectKey{Name: clusterResourcePlacementName}, &clusterResourcePlacement); err != nil {
		klog.ErrorS(err, "Failed to get ClusterResourcePlacement", "clusterResourcePlacement", clusterResourcePlacementName, "stagedUpdateRun", updateRunRef)
		if apierrors.IsNotFound(err) {
			// we won't continue the initialization if the ClusterResourcePlacement is not found
			return "", fmt.Errorf("%w: %s", errInitializedFailed, "Parent placement not found")
		}
		return "", err
	}
	// Check if the ClusterResourcePlacement has an external rollout strategy
	if clusterResourcePlacement.Spec.Strategy.Type != placementv1beta1.ExternalRolloutStrategyType {
		klog.V(2).InfoS("The ClusterResourcePlacement does not have an external rollout strategy", "clusterResourcePlacement", clusterResourcePlacementName, "stagedUpdateRun", updateRunRef)
		return "", fmt.Errorf("%w: %s", errInitializedFailed, "The ClusterResourcePlacement does not have an external rollout strategy")
	}
	updateRun.Status.ApplyStrategy = clusterResourcePlacement.Spec.Strategy.ApplyStrategy
	return clusterResourcePlacement.Name, nil
}

// determinePolicySnapshot retrieves the latest policy snapshot associated with the ClusterResourcePlacement and validates it and records it in the ClusterStagedUpdateRun status.
func (r *Reconciler) determinePolicySnapshot(ctx context.Context, placementName string, updateRun *placementv1alpha1.ClusterStagedUpdateRun) (*placementv1beta1.ClusterSchedulingPolicySnapshot, int, error) {
	updateRunRef := klog.KObj(updateRun)
	// Get the latest policy snapshot
	var policySnapshotList placementv1beta1.ClusterSchedulingPolicySnapshotList
	latestPolicyMatcher := client.MatchingLabels{
		placementv1beta1.CRPTrackingLabel:      placementName,
		placementv1beta1.IsLatestSnapshotLabel: "true",
	}
	if err := r.List(ctx, &policySnapshotList, latestPolicyMatcher); err != nil {
		klog.ErrorS(err, "Failed to list the latest policy snapshots of a cluster resource placement", "clusterResourcePlacement", placementName, "stagedUpdateRun", updateRunRef)
		return nil, -1, err
	}
	if len(policySnapshotList.Items) != 1 {
		if len(policySnapshotList.Items) > 1 {
			err := fmt.Errorf("more than one latest policy snapshot associated with cluster resource placement: %s", placementName)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Failed to find the latest policy snapshot", "clusterResourcePlacement", placementName, "numberOfSnapshot", len(policySnapshotList.Items), "stagedUpdateRun", updateRunRef)
			return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
		}
		err := fmt.Errorf("no latest policy snapshot associated with cluster resource placement: %s", placementName)
		klog.ErrorS(err, "Failed to find the latest policy snapshot", "clusterResourcePlacement", placementName, "numberOfSnapshot", len(policySnapshotList.Items), "stagedUpdateRun", updateRunRef)
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	// Get the node count from the latest policy snapshot
	latestPolicySnapshot := policySnapshotList.Items[0]
	updateRun.Status.PolicySnapshotIndexUsed = latestPolicySnapshot.Name
	clusterCount, err := annotations.ExtractNumOfClustersFromPolicySnapshot(&latestPolicySnapshot)
	if err != nil {
		annErr := fmt.Errorf("%w, the policySnapshot `%s` doesn't have cluster count annotation", err, latestPolicySnapshot.Name)
		klog.ErrorS(controller.NewUnexpectedBehaviorError(annErr), "Failed to get the cluster count from the latestPolicySnapshot", "clusterResourcePlacement", placementName, "latestPolicySnapshot", latestPolicySnapshot.Name, "stagedUpdateRun", updateRunRef)
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, annErr.Error())
	}
	updateRun.Status.PolicyObservedClusterCount = clusterCount
	klog.V(2).InfoS("Found the corresponding policy snapshot", "policySnapshot", latestPolicySnapshot.Name, "observed CRP generation", updateRun.Status.PolicyObservedClusterCount, "stagedUpdateRun", updateRunRef)
	if !condition.IsConditionStatusTrue(latestPolicySnapshot.GetCondition(string(placementv1beta1.PolicySnapshotScheduled)), latestPolicySnapshot.Generation) {
		scheduleErr := fmt.Errorf("policy snapshot not fully scheduled yet")
		klog.ErrorS(scheduleErr, "The policy snapshot is not scheduled successfully", "clusterResourcePlacement", placementName, "latestPolicySnapshot", latestPolicySnapshot.Name, "stagedUpdateRun", updateRunRef)
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, scheduleErr.Error())
	}
	return &latestPolicySnapshot, clusterCount, nil
}

// collectScheduledClusters retrieves the scheduled clusters from the latest policy snapshot and lists all the bindings according to its SchedulePolicyTrackingLabel.
func (r *Reconciler) collectScheduledClusters(ctx context.Context, placementName string, latestPolicySnapshot *placementv1beta1.ClusterSchedulingPolicySnapshot,
	updateRun *placementv1alpha1.ClusterStagedUpdateRun) ([]*placementv1beta1.ClusterResourceBinding, []*placementv1beta1.ClusterResourceBinding, error) {
	updateRunRef := klog.KObj(updateRun)
	// List all the bindings according to the SchedulePolicyTrackingLabel
	var bindingsList placementv1beta1.ClusterResourceBindingList
	schedulePolicyMatcher := client.MatchingLabels{
		placementv1beta1.CRPTrackingLabel: placementName,
	}
	if err := r.List(ctx, &bindingsList, schedulePolicyMatcher); err != nil {
		klog.ErrorS(err, "Failed to list bindings according to the SchedulePolicyTrackingLabel", "policySnapshot", latestPolicySnapshot.Name, "stagedUpdateRun", updateRunRef)
		return nil, nil, err
	}
	var tobeDeleted, selectedBindings []*placementv1beta1.ClusterResourceBinding
	for i, binding := range bindingsList.Items {
		if binding.Spec.SchedulingPolicySnapshotName == latestPolicySnapshot.Name {
			if binding.Spec.State != placementv1beta1.BindingStateScheduled {
				return nil, nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("binding `%s`'s state %s is not scheduled", binding.Name, binding.Spec.State))
			}
			klog.V(2).InfoS("Found a scheduled binding", "binding", binding.Name, "policySnapshot", latestPolicySnapshot.Name, "stagedUpdateRun", updateRunRef)
			selectedBindings = append(selectedBindings, &bindingsList.Items[i])
		} else {
			klog.V(2).InfoS("Found a to be deleted binding", "binding", binding.Name, "policySnapshot", latestPolicySnapshot.Name, "stagedUpdateRun", updateRunRef)
			tobeDeleted = append(tobeDeleted, &bindingsList.Items[i])
		}
	}
	if len(selectedBindings) == 0 {
		err := fmt.Errorf("no scheduled bindings found for the policy snapshot: %s", latestPolicySnapshot.Name)
		klog.ErrorS(err, "Failed to find the scheduled bindings", "policySnapshot", latestPolicySnapshot.Name, "stagedUpdateRun", updateRunRef)
		return nil, nil, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	return selectedBindings, tobeDeleted, nil
}

// generateStageByStrategy computes the stages based on the StagedUpdateStrategy the ClusterStagedUpdateRun references.
func (r *Reconciler) generateStageByStrategy(ctx context.Context, scheduledBindings, tobeDeletedBindings []*placementv1beta1.ClusterResourceBinding, updateRun *placementv1alpha1.ClusterStagedUpdateRun) error {
	// Fetch the StagedUpdateStrategy referenced by StagedUpdateStrategyName
	var updateStrategy placementv1alpha1.ClusterStagedUpdateStrategy
	if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRun.Spec.StagedUpdateStrategyName}, &updateStrategy); err != nil {
		klog.ErrorS(err, "Failed to get StagedUpdateStrategy", "stagedUpdateStrategy", updateRun.Spec.StagedUpdateStrategyName)
		if apierrors.IsNotFound(err) {
			// we won't continue the initialization if the StagedUpdateStrategy is not found
			return fmt.Errorf("%w: %s", errInitializedFailed, "referenced update strategy not found")
		}
		return err
	}
	// this won't change even if the stagedUpdateStrategy changes or is deleted after the updateRun is initialized
	updateRun.Status.StagedUpdateStrategySnapshot = &updateStrategy.Spec
	// Record the stages in the ClusterStagedUpdateRun status
	err := r.computeRunStageStatus(ctx, scheduledBindings, updateRun)
	if err != nil {
		return err
	}
	// Record the clusters to be deleted
	tobeDeletedCluster := make([]placementv1alpha1.ClusterUpdatingStatus, len(tobeDeletedBindings))
	for i, binding := range tobeDeletedBindings {
		klog.V(2).InfoS("Add a cluster to the delete stage", "cluster", binding.Spec.TargetCluster, "stagedUpdateStrategy", updateRun.Spec.StagedUpdateStrategyName, "stagedUpdateRun", klog.KObj(updateRun))
		tobeDeletedCluster[i].ClusterName = binding.Spec.TargetCluster
	}
	// Sort the clusters in the stage based on the cluster name
	sort.Slice(tobeDeletedCluster, func(i, j int) bool {
		return tobeDeletedCluster[i].ClusterName < tobeDeletedCluster[j].ClusterName
	})
	updateRun.Status.DeletionStageStatus = &placementv1alpha1.StageUpdatingStatus{
		StageName: placementv1alpha1.UpdateRunDeleteStageName,
		Clusters:  tobeDeletedCluster,
	}
	return nil
}

// computeRunStageStatus computes the stages based on the StagedUpdateStrategy and scheduled run and records them in the ClusterStagedUpdateRun status.
func (r *Reconciler) computeRunStageStatus(ctx context.Context, scheduledBindings []*placementv1beta1.ClusterResourceBinding, updateRun *placementv1alpha1.ClusterStagedUpdateRun) error {
	updateRunRef := klog.KObj(updateRun)
	stagedUpdateStrategyName := updateRun.Spec.StagedUpdateStrategyName
	// Map to track clusters and ensure they appear in only one stage
	allSelectedClusters := make(map[string]bool, len(scheduledBindings))
	allPlacedClusters := make(map[string]bool)
	for _, binding := range scheduledBindings {
		allSelectedClusters[binding.Spec.TargetCluster] = true
	}
	// Apply the label selectors from the StagedUpdateStrategy to filter the clusters
	for _, stage := range updateRun.Status.StagedUpdateStrategySnapshot.Stages {
		if err := validateAfterStageTask(stage.AfterStageTasks); err != nil {
			klog.ErrorS(err, "Failed to validate the after stage tasks", "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "stagedUpdateRun", updateRunRef)

			return fmt.Errorf("%w: the after stage tasks are invalide, stagedUpdateStrategy is `%s`, stage Name is `%s`, err = %s", errInitializedFailed, stagedUpdateStrategyName, stage.Name, err.Error())
		}
		curSageUpdatingStatus := placementv1alpha1.StageUpdatingStatus{
			StageName: stage.Name,
		}
		var curStageClusters []clusterv1beta1.MemberCluster
		labelSelector, err := metav1.LabelSelectorAsSelector(stage.LabelSelector)
		if err != nil {
			klog.ErrorS(err, "Failed to convert label selector", "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "labelSelector", stage.LabelSelector, "stagedUpdateRun", updateRunRef)
			return fmt.Errorf("%w: the stage label selector is invalide, stagedUpdateStrategy is `%s`, stage Name is `%s`, err = %s", errInitializedFailed, stagedUpdateStrategyName, stage.Name, err.Error())
		}
		// List all the clusters that match the label selector
		clusterList := &clusterv1beta1.MemberClusterList{}
		listOptions := &client.ListOptions{LabelSelector: labelSelector}
		if err = r.List(ctx, clusterList, listOptions); err != nil {
			klog.ErrorS(err, "Failed to list clusters for the stage", "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "stagedUpdateRun", updateRunRef)
			return err
		}
		// intersect the selected clusters with the clusters in the stage
		for _, cluster := range clusterList.Items {
			if allSelectedClusters[cluster.Name] {
				if allPlacedClusters[cluster.Name] {
					err = fmt.Errorf("cluster `%s` appears in more than one stage", cluster.Name)
					klog.ErrorS(err, "Failed to compute the stages", "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "stagedUpdateRun", updateRunRef)
					return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
				}
				if stage.SortingLabelKey != nil {
					// interpret the label values as integers
					_, err = strconv.Atoi(cluster.Labels[*stage.SortingLabelKey])
					if err != nil {
						sortingKeyErr := fmt.Errorf("the sorting label `%s` on cluster `%s` is not valid", *stage.SortingLabelKey, cluster.Name)
						klog.ErrorS(sortingKeyErr, "The sorting label is not an integer", "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "stagedUpdateRun", updateRunRef)
						return fmt.Errorf("%w: %s", errInitializedFailed, sortingKeyErr.Error())
					}
				}
				curStageClusters = append(curStageClusters, cluster)
				allPlacedClusters[cluster.Name] = true
			}
		}
		// Check if the stage has any clusters selected
		if len(curStageClusters) == 0 {
			err = fmt.Errorf("stage '%s' has no clusters selected", stage.Name)
			klog.Error(err, "No cluster is selected for the stage", "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "stagedUpdateRun", updateRunRef)
			return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
		}
		// Sort the clusters in the stage based on the SortingLabelKey and cluster name
		sort.Slice(curStageClusters, func(i, j int) bool {
			if stage.SortingLabelKey == nil {
				return curStageClusters[i].Name < curStageClusters[j].Name
			}
			labelI := curStageClusters[i].Labels[*stage.SortingLabelKey]
			labelJ := curStageClusters[j].Labels[*stage.SortingLabelKey]
			intI, _ := strconv.Atoi(labelI)
			intJ, _ := strconv.Atoi(labelJ)
			if intI != intJ {
				return intI < intJ
			}
			return curStageClusters[i].Name < curStageClusters[j].Name
		})
		// Record the clusters in the stage status
		curSageUpdatingStatus.Clusters = make([]placementv1alpha1.ClusterUpdatingStatus, len(curStageClusters))
		for i, cluster := range curStageClusters {
			klog.V(2).InfoS("Add a cluster to stage", "cluster", cluster.Name, "stagedUpdateStrategy", stagedUpdateStrategyName, "stage", stage.Name, "stagedUpdateRun", updateRunRef)
			curSageUpdatingStatus.Clusters[i].ClusterName = cluster.Name
		}
		// create the after stage tasks status array
		curSageUpdatingStatus.AfterStageTaskStatus = make([]placementv1alpha1.AfterStageTaskStatus, len(stage.AfterStageTasks))
		// Record the after stage tasks in the stage status
		for i, afterStageTask := range stage.AfterStageTasks {
			curSageUpdatingStatus.AfterStageTaskStatus[i].Type = afterStageTask.Type
			if afterStageTask.Type == placementv1alpha1.AfterStageTaskTypeApproval {
				curSageUpdatingStatus.AfterStageTaskStatus[i].ApprovalRequestName = fmt.Sprintf(placementv1alpha1.ApprovalTaskNameFmt, updateRun.Name, stage.Name)
			}
		}
		updateRun.Status.StagesStatus = append(updateRun.Status.StagesStatus, curSageUpdatingStatus)
	}
	// Check if all the selected clusters are placed in a stage
	if len(allPlacedClusters) < len(allSelectedClusters) {
		err := fmt.Errorf("some clusters are not placed in any stage")
		for cluster := range allSelectedClusters {
			if !allPlacedClusters[cluster] {
				klog.ErrorS(err, "one cluster is not placed in any stage", "selectedCluster", cluster, "stagedUpdateStrategy", stagedUpdateStrategyName, "stagedUpdateRun", updateRunRef)
				r.recorder.Event(updateRun, corev1.EventTypeWarning, "MissingCluster", fmt.Sprintf("The cluster `%s` in not selected in any stage", cluster))
			}
		}
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	return nil
}

// validateAfterStageTask validates the afterStageTask in the stage defined in updateRunStrategy.
func validateAfterStageTask(afterStageTasks []placementv1alpha1.AfterStageTask) error {
	for _, afterStageTask := range afterStageTasks {
		if afterStageTask.Type == placementv1alpha1.AfterStageTaskTypeTimedWait {
			if afterStageTask.WaitTime.Duration == 0 {
				return fmt.Errorf("the wait task duration is 0")
			}
			if afterStageTask.WaitTime.Duration < 0 {
				return fmt.Errorf("the wait task duration is negative")
			}
		}
	}
	return nil
}

// recordOverrideSnapshots finds all the override snapshots that are associated with each cluster and record them in the ClusterStagedUpdateRun status.
// This is done only once during the initialization.
func (r *Reconciler) recordOverrideSnapshots(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun) error {
	updateRunRef := klog.KObj(updateRun)
	var masterResourceSnapshot placementv1beta1.ClusterResourceSnapshot
	if err := r.Get(ctx, types.NamespacedName{Name: updateRun.Spec.ResourceSnapshotIndex}, &masterResourceSnapshot); err != nil {
		klog.ErrorS(err, "Failed to get the master resource snapshot", "resourceSnapshot", updateRun.Spec.ResourceSnapshotIndex, "stagedUpdateRun", updateRunRef)
		return err
	}
	if len(masterResourceSnapshot.Annotations[placementv1beta1.ResourceGroupHashAnnotation]) == 0 {
		err := fmt.Errorf("the resource snapshot is not a master snapshot")
		klog.ErrorS(err, "Failed to get the master resource snapshot", "resourceSnapshot", updateRun.Spec.ResourceSnapshotIndex, "stagedUpdateRun", updateRunRef)
		return err
	}
	// Fetch all the matching overrides which selected the resources
	matchedCRO, matchedRO, err := controller.FetchAllMatchOverridesForResourceSnapshot(ctx, r.Client, r.InformerManager, updateRun.Spec.PlacementName, &masterResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to find all matching overrides for the update run", "stagedUpdateRun", updateRunRef, "masterResourceSnapshot", klog.KObj(&masterResourceSnapshot))
		return err
	}
	// Pick the overrides associated with the target cluster
	for _, stageStatus := range updateRun.Status.StagesStatus {
		for _, clusterStatus := range stageStatus.Clusters {
			// Fetch the override snapshots associated with the cluster
			clusterStatus.ClusterResourceOverrideSnapshots, clusterStatus.ResourceOverrideSnapshots, err =
				controller.PickFromResourceMatchedOverridesForTargetCluster(ctx, r.Client, clusterStatus.ClusterName, matchedCRO, matchedRO)
			if err != nil {
				klog.ErrorS(err, "Failed to pick the override snapshots for the cluster", "cluster", clusterStatus.ClusterName, "stagedUpdateRun", updateRunRef, "masterResourceSnapshot", klog.KObj(&masterResourceSnapshot))
				return err
			}
		}
	}
	return nil
}

// recordInitializationFailed records the failed initialization in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordInitializationFailed(ctx context.Context, updateRun *placementv1alpha1.ClusterStagedUpdateRun, message string) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1alpha1.StagedUpdateRunConditionInitialized),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunInitializeFailedReason,
		Message:            message,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as failed to initialize", "stagedUpdateRun", klog.KObj(updateRun))
		return updateErr
	}
	return nil
}
