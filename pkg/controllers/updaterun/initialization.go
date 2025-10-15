/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package updaterun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/annotations"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/overrider"
)

// initialize initializes the UpdateRun object with all the stages computed during the initialization.
// This function is called only once during the initialization of the UpdateRun.
func (r *Reconciler) initialize(
	ctx context.Context,
	updateRun placementv1beta1.UpdateRunObj,
) ([]placementv1beta1.BindingObj, []placementv1beta1.BindingObj, error) {
	// Validate the Placement object referenced by the UpdateRun.
	placementNamespacedName, err := r.validatePlacement(ctx, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Record the latest policy snapshot associated with the placement.
	latestPolicySnapshot, _, err := r.determinePolicySnapshot(ctx, placementNamespacedName, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Collect the scheduled clusters by the corresponding placement with the latest policy snapshot.
	scheduledBindings, toBeDeletedBindings, err := r.collectScheduledClusters(ctx, placementNamespacedName, latestPolicySnapshot, updateRun)
	if err != nil {
		return nil, nil, err
	}

	// Compute the stages based on the UpdateStrategy.
	if err := r.generateStagesByStrategy(ctx, scheduledBindings, toBeDeletedBindings, updateRun); err != nil {
		return nil, nil, err
	}
	// Record the override snapshots associated with each cluster.
	if err := r.recordOverrideSnapshots(ctx, placementNamespacedName, updateRun); err != nil {
		return nil, nil, err
	}

	return scheduledBindings, toBeDeletedBindings, r.recordInitializationSucceeded(ctx, updateRun)
}

// validatePlacement validates the Placement object referenced by the UpdateRun.
func (r *Reconciler) validatePlacement(ctx context.Context, updateRun placementv1beta1.UpdateRunObj) (types.NamespacedName, error) {
	updateRunRef := klog.KObj(updateRun)
	placementName := updateRun.GetUpdateRunSpec().PlacementName

	// Create NamespacedName for the placement.
	placementKey := types.NamespacedName{
		Name:      placementName,
		Namespace: updateRun.GetNamespace(),
	}

	// Fetch the placement object.
	placement, err := controller.FetchPlacementFromNamespacedName(ctx, r.Client, placementKey)
	if err != nil {
		if apierrors.IsNotFound(err) {
			placementNotFoundErr := controller.NewUserError(fmt.Errorf("parent placement not found"))
			klog.ErrorS(err, "Failed to get placement", "placement", placementKey, "updateRun", updateRunRef)
			return types.NamespacedName{}, fmt.Errorf("%w: %s", errInitializedFailed, placementNotFoundErr.Error())
		}
		klog.ErrorS(err, "Failed to get placement", "placement", placementKey, "updateRun", updateRunRef)
		return types.NamespacedName{}, controller.NewAPIServerError(true, err)
	}

	// fill out all the default values for placement, mutation webhook is not setup for resource placement.
	// TODO: setup mutation webhook for resource placement.
	defaulter.SetPlacementDefaults(placement)

	// Check if the Placement has an external rollout strategy.
	placementSpec := placement.GetPlacementSpec()
	if placementSpec.Strategy.Type != placementv1beta1.ExternalRolloutStrategyType {
		klog.V(2).InfoS("The placement does not have an external rollout strategy", "placement", placementKey, "updateRun", updateRunRef)
		wrongRolloutTypeErr := controller.NewUserError(errors.New("parent placement does not have an external rollout strategy, current strategy: " + string(placementSpec.Strategy.Type)))
		return types.NamespacedName{}, fmt.Errorf("%w: %s", errInitializedFailed, wrongRolloutTypeErr.Error())
	}

	updateRunStatus := updateRun.GetUpdateRunStatus()
	updateRunStatus.ApplyStrategy = placementSpec.Strategy.ApplyStrategy

	return placementKey, nil
}

// determinePolicySnapshot retrieves the latest policy snapshot associated with the Placement,
// and validates it and records it in the UpdateRun status.
func (r *Reconciler) determinePolicySnapshot(
	ctx context.Context,
	placementKey types.NamespacedName,
	updateRun placementv1beta1.UpdateRunObj,
) (placementv1beta1.PolicySnapshotObj, int, error) {
	updateRunRef := klog.KObj(updateRun)

	// Get the latest policy snapshot.
	policySnapshotList, err := controller.FetchLatestPolicySnapshot(ctx, r.Client, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list the latest policy snapshots", "placement", placementKey, "updateRun", updateRunRef)
		// err can be retried.
		return nil, -1, controller.NewAPIServerError(true, err)
	}

	policySnapshotObjs := policySnapshotList.GetPolicySnapshotObjs()
	if len(policySnapshotObjs) != 1 {
		if len(policySnapshotObjs) > 1 {
			err := controller.NewUnexpectedBehaviorError(fmt.Errorf("more than one (%d in actual) latest policy snapshots associated with the placement: `%s`", len(policySnapshotObjs), placementKey))
			klog.ErrorS(err, "Failed to find the latest policy snapshot", "placement", placementKey, "updateRun", updateRunRef)
			// no more retries for this error.
			return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
		}
		// no latest policy snapshot found.
		err := fmt.Errorf("no latest policy snapshot associated with the placement: `%s`", placementKey)
		klog.ErrorS(err, "Failed to find the latest policy snapshot", "placement", placementKey, "updateRun", updateRunRef)
		// again, no more retries here.
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}

	latestPolicySnapshot := policySnapshotObjs[0]
	policySnapshotRef := klog.KObj(latestPolicySnapshot)
	policyIndex, foundIndex := latestPolicySnapshot.GetLabels()[placementv1beta1.PolicyIndexLabel]
	if !foundIndex || len(policyIndex) == 0 {
		noIndexErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("policy snapshot `%s/%s` does not have a policy index label", latestPolicySnapshot.GetNamespace(), latestPolicySnapshot.GetName()))
		klog.ErrorS(noIndexErr, "Failed to get the policy index from the latestPolicySnapshot", "placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
		// no more retries here.
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, noIndexErr.Error())
	}

	updateRunStatus := updateRun.GetUpdateRunStatus()
	updateRunStatus.PolicySnapshotIndexUsed = policyIndex

	// For pickAll policy, the observed cluster count is not included in the policy snapshot.
	// We set it to -1. It will be validated in the binding stages.
	// If policy is nil, it's default to pickAll.
	clusterCount := -1
	policySnapshotSpec := latestPolicySnapshot.GetPolicySnapshotSpec()
	if policySnapshotSpec.Policy != nil {
		if policySnapshotSpec.Policy.PlacementType == placementv1beta1.PickNPlacementType {
			count, err := annotations.ExtractNumOfClustersFromPolicySnapshot(latestPolicySnapshot)
			if err != nil {
				annErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("%w: the policy snapshot `%s/%s` doesn't have valid cluster count annotation", err, latestPolicySnapshot.GetNamespace(), latestPolicySnapshot.GetName()))
				klog.ErrorS(annErr, "Failed to get the cluster count from the latestPolicySnapshot", "placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
				// no more retries here.
				return nil, -1, fmt.Errorf("%w, %s", errInitializedFailed, annErr.Error())
			}
			clusterCount = count
		} else if policySnapshotSpec.Policy.PlacementType == placementv1beta1.PickFixedPlacementType {
			clusterCount = len(policySnapshotSpec.Policy.ClusterNames)
		}
	}
	updateRunStatus.PolicyObservedClusterCount = clusterCount
	klog.V(2).InfoS("Found the latest policy snapshot", "policySnapshot", policySnapshotRef, "observedClusterCount", updateRunStatus.PolicyObservedClusterCount, "updateRun", updateRunRef)

	if !condition.IsConditionStatusTrue(latestPolicySnapshot.GetCondition(string(placementv1beta1.PolicySnapshotScheduled)), latestPolicySnapshot.GetGeneration()) {
		scheduleErr := fmt.Errorf("policy snapshot `%s` not fully scheduled yet", latestPolicySnapshot.GetName())
		klog.ErrorS(scheduleErr, "The policy snapshot is not scheduled successfully", "placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, scheduleErr.Error())
	}
	return latestPolicySnapshot, clusterCount, nil
}

// collectScheduledClusters retrieves the schedules clusters from the latest policy snapshot
// and lists all the bindings according to its SchedulePolicyTrackingLabel.
func (r *Reconciler) collectScheduledClusters(
	ctx context.Context,
	placementKey types.NamespacedName,
	latestPolicySnapshot placementv1beta1.PolicySnapshotObj,
	updateRun placementv1beta1.UpdateRunObj,
) ([]placementv1beta1.BindingObj, []placementv1beta1.BindingObj, error) {
	updateRunRef := klog.KObj(updateRun)
	policySnapshotRef := klog.KObj(latestPolicySnapshot)

	bindingObjs, err := controller.ListBindingsFromKey(ctx, r.Client, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list bindings", "placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
		// list err can be retried.
		return nil, nil, controller.NewAPIServerError(true, err)
	}
	var toBeDeletedBindings, selectedBindings []placementv1beta1.BindingObj
	for i, binding := range bindingObjs {
		bindingRef := klog.KObj(bindingObjs[i])
		bindingSpec := binding.GetBindingSpec()
		if bindingSpec.SchedulingPolicySnapshotName == latestPolicySnapshot.GetName() {
			if bindingSpec.State == placementv1beta1.BindingStateUnscheduled {
				klog.V(2).InfoS("Found an unscheduled binding with the latest policy snapshot, delete it", "binding", bindingRef, "placement", placementKey,
					"policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
				toBeDeletedBindings = append(toBeDeletedBindings, bindingObjs[i])
			} else {
				klog.V(2).InfoS("Found a scheduled binding", "binding", bindingRef, "placement", placementKey,
					"policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
				selectedBindings = append(selectedBindings, bindingObjs[i])
			}
		} else {
			if bindingSpec.State != placementv1beta1.BindingStateUnscheduled {
				stateErr := fmt.Errorf("binding `%s/%s` with old policy snapshot `%s/%s` has state %s, we might observe a transient state, need retry",
					binding.GetNamespace(), binding.GetName(), binding.GetNamespace(), bindingSpec.SchedulingPolicySnapshotName, bindingSpec.State)

				klog.V(2).InfoS("Found a not-unscheduled binding with old policy snapshot, retrying", "binding", bindingRef, "placement", placementKey,
					"policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
				// Transient state can be retried.
				return nil, nil, stateErr
			}
			klog.V(2).InfoS("Found an unscheduled binding with old policy snapshot", "binding", bindingRef, "cluster", bindingSpec.TargetCluster,
				"placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
			toBeDeletedBindings = append(toBeDeletedBindings, bindingObjs[i])
		}
	}

	if len(selectedBindings) == 0 && len(toBeDeletedBindings) == 0 {
		nobindingErr := fmt.Errorf("no scheduled or to-be-deleted bindings found for the latest policy snapshot %s/%s", latestPolicySnapshot.GetNamespace(), latestPolicySnapshot.GetName())
		klog.ErrorS(nobindingErr, "Failed to collect bindings", "placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
		// no more retries here.
		return nil, nil, fmt.Errorf("%w: %s", errInitializedFailed, nobindingErr.Error())
	}

	updateRunStatus := updateRun.GetUpdateRunStatus()
	if updateRunStatus.PolicyObservedClusterCount == -1 {
		// For pickAll policy, the observed cluster count is not included in the policy snapshot. We set it to the number of selected bindings.
		// TODO (wantjian): refactor this part to update PolicyObservedClusterCount in one place.
		updateRunStatus.PolicyObservedClusterCount = len(selectedBindings)
	} else if updateRunStatus.PolicyObservedClusterCount != len(selectedBindings) {
		countErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the number of selected bindings %d is not equal to the observed cluster count %d", len(selectedBindings), updateRunStatus.PolicyObservedClusterCount))
		klog.ErrorS(countErr, "Failed to collect bindings", "placement", placementKey, "policySnapshot", policySnapshotRef, "updateRun", updateRunRef)
		// no more retries here.
		return nil, nil, fmt.Errorf("%w: %s", errInitializedFailed, countErr.Error())
	}
	return selectedBindings, toBeDeletedBindings, nil
}

// generateStagesByStrategy computes the stages based on the UpdateStrategy referenced by the UpdateRun.
func (r *Reconciler) generateStagesByStrategy(
	ctx context.Context,
	scheduledBindings []placementv1beta1.BindingObj,
	toBeDeletedBindings []placementv1beta1.BindingObj,
	updateRun placementv1beta1.UpdateRunObj,
) error {
	updateRunRef := klog.KObj(updateRun)
	updateRunSpec := updateRun.GetUpdateRunSpec()

	// Create NamespacedName for the UpdateStrategy.
	strategyKey := types.NamespacedName{
		Name:      updateRunSpec.StagedUpdateStrategyName,
		Namespace: updateRun.GetNamespace(),
	}

	// Fetch the UpdateStrategy.
	updateStrategy, err := controller.FetchUpdateStrategyFromNamespacedName(ctx, r.Client, strategyKey)
	if err != nil {
		klog.ErrorS(err, "Failed to get UpdateStrategy", "updateStrategy", strategyKey, "updateRun", updateRunRef)
		if apierrors.IsNotFound(err) {
			// we won't continue or retry the initialization if the UpdateStrategy is not found.
			strategyNotFoundErr := controller.NewUserError(fmt.Errorf("referenced updateStrategy not found: `%s`", strategyKey))
			return fmt.Errorf("%w: %s", errInitializedFailed, strategyNotFoundErr.Error())
		}
		// other err can be retried.
		return controller.NewAPIServerError(true, err)
	}

	// This won't change even if the updateStrategy changes or is deleted after the updateRun is initialized.
	updateRunStatus := updateRun.GetUpdateRunStatus()
	updateStrategySpec := updateStrategy.GetUpdateStrategySpec()
	updateRunStatus.UpdateStrategySnapshot = updateStrategySpec

	// Remove waitTime from the updateRun status for AfterStageTask for type Approval.
	removeWaitTimeFromUpdateRunStatus(updateRun)

	// Compute the update stages.
	if err := r.computeRunStageStatus(ctx, scheduledBindings, updateRun); err != nil {
		return err
	}
	toBeDeletedClusters := make([]placementv1beta1.ClusterUpdatingStatus, len(toBeDeletedBindings))
	for i, binding := range toBeDeletedBindings {
		bindingSpec := binding.GetBindingSpec()
		klog.V(2).InfoS("Adding a cluster to the delete stage", "cluster", bindingSpec.TargetCluster, "updateStrategy", strategyKey, "updateRun", updateRunRef)
		toBeDeletedClusters[i].ClusterName = bindingSpec.TargetCluster
	}
	// Sort the clusters in the deletion stage by their names.
	sort.Slice(toBeDeletedClusters, func(i, j int) bool {
		return toBeDeletedClusters[i].ClusterName < toBeDeletedClusters[j].ClusterName
	})

	// Update deletion stage status using interface methods.
	updateRunStatus.DeletionStageStatus = &placementv1beta1.StageUpdatingStatus{
		StageName: placementv1beta1.UpdateRunDeleteStageName,
		Clusters:  toBeDeletedClusters,
	}
	return nil
}

// computeRunStageStatus computes the stages based on the UpdateStrategy and records them in the UpdateRun status.
func (r *Reconciler) computeRunStageStatus(
	ctx context.Context,
	scheduledBindings []placementv1beta1.BindingObj,
	updateRun placementv1beta1.UpdateRunObj,
) error {
	updateRunRef := klog.KObj(updateRun)
	updateRunSpec := updateRun.GetUpdateRunSpec()
	updateRunStatus := updateRun.GetUpdateRunStatus()

	// Create NamespacedName for the UpdateStrategy.
	strategyKey := types.NamespacedName{
		Name:      updateRunSpec.StagedUpdateStrategyName,
		Namespace: updateRun.GetNamespace(),
	}

	// Map to track clusters and ensure they appear in one and only one stage.
	allSelectedClusters := make(map[string]struct{}, len(scheduledBindings))
	allPlacedClusters := make(map[string]struct{})
	for _, binding := range scheduledBindings {
		allSelectedClusters[binding.GetBindingSpec().TargetCluster] = struct{}{}
	}
	stagesStatus := make([]placementv1beta1.StageUpdatingStatus, 0, len(updateRunStatus.UpdateStrategySnapshot.Stages))

	// Apply the label selectors from the UpdateStrategy to filter the clusters.
	for _, stage := range updateRunStatus.UpdateStrategySnapshot.Stages {
		if err := validateAfterStageTask(stage.AfterStageTasks); err != nil {
			klog.ErrorS(err, "Failed to validate the after stage tasks", "updateStrategy", strategyKey, "stageName", stage.Name, "updateRun", updateRunRef)
			// no more retries here.
			invalidAfterStageErr := controller.NewUserError(fmt.Errorf("the after stage tasks are invalid, updateStrategy: `%s`, stage: %s, err: %s", strategyKey, stage.Name, err.Error()))
			return fmt.Errorf("%w: %s", errInitializedFailed, invalidAfterStageErr.Error())
		}

		curStageUpdatingStatus := placementv1beta1.StageUpdatingStatus{StageName: stage.Name}
		var curStageClusters []clusterv1beta1.MemberCluster
		labelSelector, err := metav1.LabelSelectorAsSelector(stage.LabelSelector)
		if err != nil {
			klog.ErrorS(err, "Failed to convert label selector", "updateStrategy", strategyKey, "stageName", stage.Name, "labelSelector", stage.LabelSelector, "updateRun", updateRunRef)
			// no more retries here.
			invalidLabelErr := controller.NewUserError(fmt.Errorf("the stage label selector is invalid, updateStrategy: `%s`, stage: %s, err: %s", strategyKey, stage.Name, err.Error()))
			return fmt.Errorf("%w: %s", errInitializedFailed, invalidLabelErr.Error())
		}
		// List all the clusters that match the label selector.
		var clusterList clusterv1beta1.MemberClusterList
		if err := r.Client.List(ctx, &clusterList, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
			klog.ErrorS(err, "Failed to list clusters for the stage", "updateStrategy", strategyKey, "stageName", stage.Name, "labelSelector", stage.LabelSelector, "updateRun", updateRunRef)
			// list err can be retried.
			return controller.NewAPIServerError(true, err)
		}

		// Intersect the selected clusters with the clusters in the stage.
		for _, cluster := range clusterList.Items {
			if _, ok := allSelectedClusters[cluster.Name]; ok {
				if _, ok := allPlacedClusters[cluster.Name]; ok {
					// a cluster can only appear in one stage.
					dupErr := controller.NewUserError(fmt.Errorf("cluster `%s` appears in more than one stages", cluster.Name))
					klog.ErrorS(dupErr, "Failed to compute the stage", "updateStrategy", strategyKey, "stageName", stage.Name, "updateRun", updateRunRef)
					// no more retries here.
					return fmt.Errorf("%w: %s", errInitializedFailed, dupErr.Error())
				}
				if stage.SortingLabelKey != nil {
					// interpret the label values as integers.
					if _, err := strconv.Atoi(cluster.Labels[*stage.SortingLabelKey]); err != nil {
						keyErr := controller.NewUserError(fmt.Errorf("the sorting label `%s:%s` on cluster `%s` is not valid: %s", *stage.SortingLabelKey, cluster.Labels[*stage.SortingLabelKey], cluster.Name, err.Error()))
						klog.ErrorS(keyErr, "Failed to sort clusters in the stage", "updateStrategy", strategyKey, "stageName", stage.Name, "updateRun", updateRunRef)
						// no more retries here.
						return fmt.Errorf("%w: %s", errInitializedFailed, keyErr.Error())
					}
				}
				curStageClusters = append(curStageClusters, cluster)
				allPlacedClusters[cluster.Name] = struct{}{}
			}
		}

		// Check if the stage is empty.
		if len(curStageClusters) == 0 {
			// since we allow no selected bindings, a stage can be empty.
			klog.InfoS("No cluster is selected for the stage", "updateStrategy", strategyKey, "stageName", stage.Name, "updateRun", updateRunRef)
		} else {
			// Sort the clusters in the stage based on the SortingLabelKey and cluster name.
			sort.Slice(curStageClusters, func(i, j int) bool {
				if stage.SortingLabelKey == nil {
					return curStageClusters[i].Name < curStageClusters[j].Name
				}
				labelI, _ := strconv.Atoi(curStageClusters[i].Labels[*stage.SortingLabelKey])
				labelJ, _ := strconv.Atoi(curStageClusters[j].Labels[*stage.SortingLabelKey])
				if labelI != labelJ {
					return labelI < labelJ
				}
				return curStageClusters[i].Name < curStageClusters[j].Name
			})
		}

		// Record the clusters in the stage.
		curStageUpdatingStatus.Clusters = make([]placementv1beta1.ClusterUpdatingStatus, len(curStageClusters))
		for i, cluster := range curStageClusters {
			klog.V(2).InfoS("Adding a cluster to the stage", "cluster", cluster.Name, "updateStrategy", strategyKey, "stageName", stage.Name, "updateRun", updateRunRef)
			curStageUpdatingStatus.Clusters[i].ClusterName = cluster.Name
		}

		// Create the after stage tasks.
		curStageUpdatingStatus.AfterStageTaskStatus = make([]placementv1beta1.AfterStageTaskStatus, len(stage.AfterStageTasks))
		for i, task := range stage.AfterStageTasks {
			curStageUpdatingStatus.AfterStageTaskStatus[i].Type = task.Type
			if task.Type == placementv1beta1.AfterStageTaskTypeApproval {
				curStageUpdatingStatus.AfterStageTaskStatus[i].ApprovalRequestName = fmt.Sprintf(placementv1beta1.ApprovalTaskNameFmt, updateRun.GetName(), stage.Name)
			}
		}
		stagesStatus = append(stagesStatus, curStageUpdatingStatus)
	}
	updateRunStatus.StagesStatus = stagesStatus

	// Check if the clusters are all placed.
	if len(allPlacedClusters) != len(allSelectedClusters) {
		missingErr := controller.NewUserError(fmt.Errorf("some clusters are not placed in any stage"))
		missingClusters := make([]string, 0, len(allSelectedClusters)-len(allPlacedClusters))
		for cluster := range allSelectedClusters {
			if _, ok := allPlacedClusters[cluster]; !ok {
				missingClusters = append(missingClusters, cluster)
			}
		}
		// Sort the missing clusters by their names to generate a stable error message.
		sort.Strings(missingClusters)
		klog.ErrorS(missingErr, "Clusters are missing in any stage", "clusters", strings.Join(missingClusters, ", "), "updateStrategy", strategyKey, "updateRun", updateRunRef)
		// no more retries here, only show the first 10 missing clusters in the placement status.
		return fmt.Errorf("%w: %s, total %d, showing up to 10: %s", errInitializedFailed, missingErr.Error(), len(missingClusters), strings.Join(missingClusters[:min(10, len(missingClusters))], ", "))
	}
	return nil
}

// validateAfterStageTask valides the afterStageTasks in the stage defined in the UpdateStrategy.
// The error returned from this function is not retryable.
func validateAfterStageTask(tasks []placementv1beta1.AfterStageTask) error {
	if len(tasks) == 2 && tasks[0].Type == tasks[1].Type {
		return fmt.Errorf("afterStageTasks cannot have two tasks of the same type: %s", tasks[0].Type)
	}
	for i, task := range tasks {
		if task.Type == placementv1beta1.AfterStageTaskTypeTimedWait {
			if task.WaitTime == nil {
				return fmt.Errorf("task %d of type TimedWait has wait duration set to nil", i)
			}
			if task.WaitTime.Duration <= 0 {
				return fmt.Errorf("task %d of type TimedWait has wait duration <= 0", i)
			}
		}
	}
	return nil
}

// recordOverrideSnapshots finds all the override snapshots that are associated with each cluster and record them in the UpdateRun status.
func (r *Reconciler) recordOverrideSnapshots(ctx context.Context, placementKey types.NamespacedName, updateRun placementv1beta1.UpdateRunObj) error {
	updateRunRef := klog.KObj(updateRun)
	updateRunSpec := updateRun.GetUpdateRunSpec()
	placementName := placementKey.Name

	snapshotIndex, err := strconv.Atoi(updateRunSpec.ResourceSnapshotIndex)
	if err != nil || snapshotIndex < 0 {
		err := controller.NewUserError(fmt.Errorf("invalid resource snapshot index `%s` provided, expected an integer >= 0", updateRunSpec.ResourceSnapshotIndex))
		klog.ErrorS(err, "Failed to parse the resource snapshot index", "updateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}

	resourceSnapshotList, err := controller.ListAllResourceSnapshotWithAnIndex(ctx, r.Client, updateRunSpec.ResourceSnapshotIndex, placementName, placementKey.Namespace)
	if err != nil {
		klog.ErrorS(err, "Failed to list the resourceSnapshots associated with the placement",
			"placement", placementKey, "resourceSnapshotIndex", snapshotIndex, "updateRun", updateRunRef)
		// err can be retried.
		return controller.NewAPIServerError(true, err)
	}

	resourceSnapshotObjs := resourceSnapshotList.GetResourceSnapshotObjs()
	if len(resourceSnapshotObjs) == 0 {
		err := controller.NewUserError(fmt.Errorf("no resourceSnapshots with index `%d` found for placement `%s`", snapshotIndex, placementKey))
		klog.ErrorS(err, "No specified resourceSnapshots found", "updateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}

	// Look for the master resourceSnapshot.
	var masterResourceSnapshot placementv1beta1.ResourceSnapshotObj
	for _, resourceSnapshot := range resourceSnapshotObjs {
		// only master has this annotation.
		if len(resourceSnapshot.GetAnnotations()[placementv1beta1.ResourceGroupHashAnnotation]) != 0 {
			masterResourceSnapshot = resourceSnapshot
			break
		}
	}

	// No masterResourceSnapshot found.
	if masterResourceSnapshot == nil {
		err := controller.NewUnexpectedBehaviorError(fmt.Errorf("no master resourceSnapshot found for placement `%s` with index `%d`", placementKey, snapshotIndex))
		klog.ErrorS(err, "Failed to find master resourceSnapshot", "updateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	klog.V(2).InfoS("Found master resourceSnapshot", "placement", placementKey, "index", snapshotIndex, "updateRun", updateRunRef)

	resourceSnapshotRef := klog.KObj(masterResourceSnapshot)
	// Fetch all the matching overrides.
	matchedCRO, matchedRO, err := overrider.FetchAllMatchingOverridesForResourceSnapshot(ctx, r.Client, r.InformerManager, updateRunSpec.PlacementName, masterResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to find all matching overrides for the stagedUpdateRun", "resourceSnapshot", resourceSnapshotRef, "updateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}

	// Pick the overrides associated with each target cluster.
	updateRunStatus := updateRun.GetUpdateRunStatus()
	for _, stageStatus := range updateRunStatus.StagesStatus {
		for i := range stageStatus.Clusters {
			clusterStatus := &stageStatus.Clusters[i]
			clusterStatus.ClusterResourceOverrideSnapshots, clusterStatus.ResourceOverrideSnapshots, err =
				overrider.PickFromResourceMatchedOverridesForTargetCluster(ctx, r.Client, clusterStatus.ClusterName, matchedCRO, matchedRO)
			if err != nil {
				klog.ErrorS(err, "Failed to pick the override snapshots for cluster", "cluster", clusterStatus.ClusterName, "resourceSnapshot", resourceSnapshotRef, "updateRun", updateRunRef)
				// no more retries here.
				return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
			}
		}
	}

	return nil
}

// recordInitializationSucceeded records the successful initialization condition in the UpdateRun status.
func (r *Reconciler) recordInitializationSucceeded(ctx context.Context, updateRun placementv1beta1.UpdateRunObj) error {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionInitialized),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunInitializeSucceededReason,
		Message:            "ClusterStagedUpdateRun initialized successfully",
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the UpdateRun status as initialized", "updateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// recordInitializationFailed records the failed initialization condition in the updateRun status.
func (r *Reconciler) recordInitializationFailed(ctx context.Context, updateRun placementv1beta1.UpdateRunObj, message string) error {
	updateRunStatus := updateRun.GetUpdateRunStatus()
	meta.SetStatusCondition(&updateRunStatus.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionInitialized),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.GetGeneration(),
		Reason:             condition.UpdateRunInitializeFailedReason,
		Message:            message,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the updateRun status as failed to initialize", "updateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}
