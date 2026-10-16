/*
Copyright 2026 The KubeFleet Authors.

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

package stagedupdaterun

import (
	"context"
	"sort"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

func (r *Reconciler) initialize(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
) error {
	initCond := meta.FindStatusCondition(stagedUpdateRun.GetStatus().Conditions, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	switch {
	case initCond == nil:
		// The staged update run has not been initialized yet; proceed with the initialization.
	case initCond.Status == metav1.ConditionTrue:
		// The staged update run has already been initialized successfully; skip the initialization.
		klog.V(2).InfoS("Staged update run has been initialized; skip the initialization step",
			"stagedUpdateRun", klog.KObj(stagedUpdateRun))
		// If the condition is no longer up to date (its observed generation is behind the current generation), resync it.
		return r.resyncStagedUpdateRunCondIfNeeded(ctx, stagedUpdateRun, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	case initCond.Status == metav1.ConditionFalse:
		// The staged update run has already been initialized and failed; no further processing is needed.
		klog.V(2).InfoS("Staged update run has failed to initialize; no further processing is needed",
			"stagedUpdateRun", klog.KObj(stagedUpdateRun))
		// If the condition is no longer up to date (its observed generation is behind the current generation), resync it.
		return r.resyncStagedUpdateRunCondIfNeeded(ctx, stagedUpdateRun, rolloutv1alpha1.StagedUpdateRunCondTypeInitialized)
	}

	// Retrieve the staged update strategy.
	stagedUpdateStrategy, err := r.retrieveStagedUpdateStrategy(ctx, stagedUpdateRun)
	switch {
	case err == nil:
		// The staged update strategy has been found; proceed with the initialization.
	case apierrors.IsNotFound(err):
		return errors.NewUserError(nil, "staged update run strategy cannot be found", errors.Args(err)...)
	default:
		// An API server error other than NotFound error has occurred.
		return errors.Wraps(err, "failed to find staged update strategy")
	}

	// List all the placement bindings from the target placement policy.
	//
	// As the staged update run has acquired the binding manager role, it is guaranteed that no binding will be created
	// or deleted when the staged update is still in progress. This is not to say that the read (list) op here is strongly
	// consistent though; if the cache is severely lagging, and the placement policy just has a scheduling decision
	// update, the binding list returned might be stale. Considering that the chances of this happening is low, no
	// prevention measure is taken here; it is recommended that the user re-attempt a staged update if the placement policy
	// still reports that some clusters are not yet synchronized.
	placementBindings, err := r.listAllPlacementBindings(ctx, placementPolicy)
	if err != nil {
		return errors.Wraps(err, "failed to find placement bindings")
	}

	// Retrieve all the clusters that are referenced by the placement bindings.
	//
	// Note that due to the pre-allocation, the order of the returned clusters is guaranteed to be the same as
	// the order of the placement bindings.
	referencedClusters, someClustersNotRetrieved, err := r.retrieveReferencedClusters(ctx, placementBindings)
	if err != nil {
		return errors.Wraps(err, "failed to retrieve referenced clusters")
	}

	if someClustersNotRetrieved {
		// Some of the referenced clusters could not be retrieved; this is not considered an error, and
		// the staged update run will continue with such bindings ignored.
		placementBindings, referencedClusters = filterBindingsWithReferencedClusters(placementBindings, referencedClusters)
	}
	if len(placementBindings) == 0 {
		return errors.NewUserError(nil, "no placement bindings are found; there are no clusters to roll out changes to")
	}

	// Group clusters into stages.
	perStageStatuses, err := r.groupClusters(stagedUpdateStrategy, placementBindings, referencedClusters)
	if err != nil {
		return errors.Wraps(err, "failed to group clusters into stages")
	}

	// Identify the resource snapshot to roll out.
	//
	// If one has been explicitly specified, check if it exists; otherwise, retrieve the latest snapshot or
	// request a new one if the latest is stale.
	resourceSnapshot, err := r.identifyResourceSnapshotToRollout(ctx, placementPolicy, stagedUpdateRun)
	if err != nil {
		return errors.Wraps(err, "failed to identify resource snapshot to roll out")
	}

	// Write the per-stage status to the staged update run.
	//
	// Once written, all follow-up processing will be based on the per-stage status, and the staged update run
	// will be considered initialized.
	if err := r.syncStagedUpdateRunInitializedStatus(ctx, stagedUpdateRun, perStageStatuses, resourceSnapshot.GetName()); err != nil {
		return errors.Wraps(err, "failed to mark the staged update run as initialized")
	}
	return nil
}

// filterBindingsWithReferencedClusters cross-references placement bindings with their referenced clusters.
//
// Note that the returned slices preserve the original order and have matching indexes.
func filterBindingsWithReferencedClusters(
	placementBindings []placementv1alpha1.PlacementBindingAccessor,
	clusters []*clusterv1beta1.MemberCluster,
) ([]placementv1alpha1.PlacementBindingAccessor, []*clusterv1beta1.MemberCluster) {
	filteredBindings := make([]placementv1alpha1.PlacementBindingAccessor, 0, len(placementBindings))
	filteredClusters := make([]*clusterv1beta1.MemberCluster, 0, len(clusters))
	for idx := range placementBindings {
		if clusters[idx] == nil {
			continue
		}
		filteredBindings = append(filteredBindings, placementBindings[idx])
		filteredClusters = append(filteredClusters, clusters[idx])
	}
	return filteredBindings, filteredClusters
}

func (r *Reconciler) groupClusters(
	stagedUpdateStrategy rolloutv1alpha1.StagedUpdateStrategyAccessor,
	placementBindings []placementv1alpha1.PlacementBindingAccessor,
	clusters []*clusterv1beta1.MemberCluster,
) (
	perStageStatuses []rolloutv1alpha1.PerStageStatus,
	err error,
) {
	checked := sets.Set[string]{}

	perStageStatuses = make([]rolloutv1alpha1.PerStageStatus, 0, len(stagedUpdateStrategy.GetSpec().Stages))

	stagesInStrategy := stagedUpdateStrategy.GetSpec().Stages
	for idx := range stagesInStrategy {
		stage := stagesInStrategy[idx]

		// Handle the special cases where the stage has nil or empty label selector.
		switch {
		case stage.LabelSelector == nil:
			// The stage has nil label selector; it includes no clusters at all.
			perStageStatus, err := buildPerStageStatus(stage, nil)
			if err != nil {
				return nil, errors.Wraps(err, "failed to build per stage status", "stage", stage.Name)
			}
			perStageStatuses = append(perStageStatuses, perStageStatus)
			continue
		case stage.LabelSelector != nil && (len(stage.LabelSelector.MatchLabels) == 0 && len(stage.LabelSelector.MatchExpressions) == 0):
			// The stage has an empty label selector; it includes all clusters.
			matched, err := matchAndSortBindings(placementBindings, clusters, nil, stage.SortingLabelKey, checked)
			if err != nil {
				return nil, errors.Wraps(err, "failed to match and sort bindings for empty label selector", "stage", stage.Name)
			}
			// Build the stage information.
			perStageStatus, err := buildPerStageStatus(stage, matched)
			if err != nil {
				return nil, errors.Wraps(err, "failed to build per stage status", "stage", stage.Name)
			}
			perStageStatuses = append(perStageStatuses, perStageStatus)
			continue
		}

		// The stage has a non-empty label selector; group and sort the clusters accordingly.
		labelSelector, err := metav1.LabelSelectorAsSelector(stage.LabelSelector)
		if err != nil {
			return nil, errors.NewUserError(err, "failed to convert label selector to selector", "stage", stage.Name)
		}

		// Find all the clusters that match the label selector, and their corresponding placement bindings.
		matched, err := matchAndSortBindings(placementBindings, clusters, labelSelector, stage.SortingLabelKey, checked)
		if err != nil {
			return nil, errors.Wraps(err, "failed to match and sort bindings for non-empty label selector", "stage", stage.Name)
		}
		// Build the stage information.
		perStageStatus, err := buildPerStageStatus(stage, matched)
		if err != nil {
			return nil, errors.Wraps(err, "failed to build per stage status", "stage", stage.Name)
		}
		perStageStatuses = append(perStageStatuses, perStageStatus)
	}

	return perStageStatuses, nil
}

type matchedCluster struct {
	cluster *clusterv1beta1.MemberCluster
	binding placementv1alpha1.PlacementBindingAccessor

	weight int
}

func matchAndSortBindings(
	bindings []placementv1alpha1.PlacementBindingAccessor,
	clusters []*clusterv1beta1.MemberCluster,
	labelSelector labels.Selector,
	sortingLabelKey *string,
	checked sets.Set[string],
) ([]*matchedCluster, error) {
	// Find all the clusters that match the label selector, and their corresponding placement bindings.

	// Pre-allocate with a reasonable capacity.
	matched := make([]*matchedCluster, 0, 10)
	for idx := range clusters {
		cluster := clusters[idx]
		binding := bindings[idx]

		if checked.Has(cluster.GetName()) {
			klog.V(2).InfoS("Cluster has already been included in a previous stage; skip it",
				"cluster", klog.KObj(cluster), "placementBinding", klog.KObj(binding))
			continue
		}

		if labelSelector != nil {
			if labelSelector.Matches(labels.Set(cluster.GetLabels())) {
				matched = append(matched, &matchedCluster{cluster: cluster, binding: binding})
				checked.Insert(cluster.GetName())

				klog.V(2).InfoS("Cluster matches the label selector; include it",
					"cluster", klog.KObj(cluster), "placementBinding", klog.KObj(binding))
			}
		} else {
			// No label selector is specified; include all clusters.
			matched = append(matched, &matchedCluster{cluster: cluster, binding: binding})
			checked.Insert(cluster.GetName())

			klog.V(2).InfoS("The stage has empty label selector; match all clusters",
				"cluster", klog.KObj(cluster), "placementBinding", klog.KObj(binding))
		}
	}

	// Sort the matched clusters if a sorting label key is specified.
	if sortingLabelKey != nil && len(*sortingLabelKey) > 0 {
		for idx := range matched {
			labelValue, ok := matched[idx].cluster.GetLabels()[*sortingLabelKey]
			if !ok {
				return nil, errors.NewUserError(nil, "a sorting label key is specified in staged update strategy, yet the label is missing on a cluster",
					"sortingLabelKey", *sortingLabelKey,
					"cluster", klog.KObj(matched[idx].cluster), "placementBinding", klog.KObj(matched[idx].binding))
			}

			sortValue, parseErr := strconv.Atoi(labelValue)
			if parseErr != nil {
				return nil, errors.NewUserError(parseErr, "failed to parse sorting label value as integer",
					"sortingLabelKey", *sortingLabelKey, "sortingLabelValue", labelValue,
					"cluster", klog.KObj(matched[idx].cluster), "placementBinding", klog.KObj(matched[idx].binding))
			}
			matched[idx].weight = sortValue
		}

		sort.SliceStable(matched, func(i, j int) bool {
			if matched[i].weight == matched[j].weight {
				return matched[i].cluster.GetName() < matched[j].cluster.GetName()
			}
			return matched[i].weight < matched[j].weight
		})
	} else {
		// Sort clusters by name in ascending order when no sorting label key is specified.
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].cluster.GetName() < matched[j].cluster.GetName()
		})
	}

	return matched, nil
}

func buildPerStageStatus(
	stage rolloutv1alpha1.Stage,
	matched []*matchedCluster,
) (rolloutv1alpha1.PerStageStatus, error) {
	// Record the clusters assigned to the stage, and their corresponding placement bindings.
	perClusterStatuses := make([]rolloutv1alpha1.PerClusterStatus, 0, len(matched))
	for idx := range matched {
		matchedCluster := matched[idx].cluster
		matchedBinding := matched[idx].binding

		perClusterStatuses = append(perClusterStatuses, rolloutv1alpha1.PerClusterStatus{
			ClusterName:          matchedCluster.GetName(),
			PlacementBindingName: matchedBinding.GetName(),
		})
	}

	// Record the before-stage tasks and after-stage tasks.
	beforeStageTaskStatuses := make([]rolloutv1alpha1.PerStageTaskStatus, 0, len(stage.BeforeStageTasks))
	for idx := range stage.BeforeStageTasks {
		beforeStageTask := stage.BeforeStageTasks[idx]
		beforeStageTaskStatus := rolloutv1alpha1.PerStageTaskStatus{
			Type: beforeStageTask.Type,
		}
		if beforeStageTask.Type != rolloutv1alpha1.StageTaskTypeApproval {
			return rolloutv1alpha1.PerStageStatus{}, errors.NewUserError(nil,
				"unsupported before-stage task type (only approval task can be added as a before stage task)",
				"taskType", beforeStageTask.Type,
				"stage", stage.Name)
		}
		beforeStageTaskStatuses = append(beforeStageTaskStatuses, beforeStageTaskStatus)
	}

	afterStageTaskStatuses := make([]rolloutv1alpha1.PerStageTaskStatus, 0, len(stage.AfterStageTasks))
	for idx := range stage.AfterStageTasks {
		afterStageTask := stage.AfterStageTasks[idx]
		afterStageTaskStatus := rolloutv1alpha1.PerStageTaskStatus{
			Type: afterStageTask.Type,
		}
		if afterStageTask.Type == rolloutv1alpha1.StageTaskTypeTimedWait {
			afterStageTaskStatus.WaitTime = afterStageTask.WaitTime
		}
		afterStageTaskStatuses = append(afterStageTaskStatuses, afterStageTaskStatus)
	}

	// Resolve the max concurrency value for the stage.
	maxConcurrencyValue := int32(1)
	if stage.MaxConcurrency != nil {
		resolvedMaxConcurrency, err := intstr.GetScaledValueFromIntOrPercent(stage.MaxConcurrency, len(matched), false)
		if err != nil {
			return rolloutv1alpha1.PerStageStatus{}, errors.NewUserError(err, "failed to resolve max concurrency",
				"stage", stage.Name, "totalClustersCnt", len(matched), "maxConcurrency", stage.MaxConcurrency)
		}
		if resolvedMaxConcurrency == 0 {
			resolvedMaxConcurrency = 1
		}
		maxConcurrencyValue = int32(resolvedMaxConcurrency)
	}

	return rolloutv1alpha1.PerStageStatus{
		StageName:        stage.Name,
		Clusters:         perClusterStatuses,
		BeforeStageTasks: beforeStageTaskStatuses,
		AfterStageTasks:  afterStageTaskStatuses,
		MaxConcurrency:   &maxConcurrencyValue,
	}, nil
}

func (r *Reconciler) identifyResourceSnapshotToRollout(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	stagedUpdateRun rolloutv1alpha1.StagedUpdateRunAccessor,
) (placementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	resourceSnapshotName := stagedUpdateRun.GetSpec().ResourceSnapshotName
	if resourceSnapshotName == "" {
		// If no resource snapshot is explicitly specified, retrieve the latest snapshot or request a new one if the latest is stale.
		resourceSnapshots, _, err := r.placementResourceSnapshotManager.SnapshotResourcesIfStale(
			ctx, placementPolicy)
		if err != nil {
			return nil, errors.Wraps(err, "failed to retrieve or create the latest resource snapshot")
		}
		return resourceSnapshots[0], nil
	}

	// A resource snapshot has been explicitly specified; check if it exists.
	var resourceSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
	snapshotKey := types.NamespacedName{
		Namespace: placementPolicy.GetNamespace(),
		Name:      resourceSnapshotName,
	}
	if snapshotKey.Namespace == "" {
		// The resource snapshot is cluster-scoped; retrieve the ClusterPlacementResourceSnapshot.
		resourceSnapshot = &placementv1alpha1.ClusterPlacementResourceSnapshot{}
	} else {
		// The resource snapshot is namespaced; retrieve the PlacementResourceSnapshot in its namespace.
		resourceSnapshot = &placementv1alpha1.PlacementResourceSnapshot{}
	}

	if err := r.hubClient.Get(ctx, snapshotKey, resourceSnapshot); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.NewUserError(err, "the explicitly specified resource snapshot does not exist",
				"resourceSnapshot", klog.KRef(snapshotKey.Namespace, snapshotKey.Name))
		}
		return nil, errors.NewAPIServerError(err, "failed to get the explicitly specified resource snapshot", true,
			"resourceSnapshot", klog.KRef(snapshotKey.Namespace, snapshotKey.Name))
	}

	// Check if the retrieved resource snapshot has been marked for deletion and if it is a primary resource snapshot.
	if !resourceSnapshot.GetDeletionTimestamp().IsZero() {
		// A resource snapshot marked for deletion is on its way out (most likely reclaimed by the
		// revision history limit); rolling out against it would race the deletion and is never
		// the user's intent, so this is reported back as a user error rather than retried.
		return nil, errors.NewUserError(nil, "the resource snapshot to roll out has been marked for deletion",
			"resourceSnapshot", klog.KRef(snapshotKey.Namespace, snapshotKey.Name))
	}
	if subIdx := resourceSnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]; subIdx != "0" {
		// Only the snapshot of the sub-index 0 is the primary snapshot of its index; a staged update
		// run can only be rolled out against a primary snapshot, since a secondary one does not carry
		// the full picture of the resources selected at that point in time.
		return nil, errors.NewUserError(nil, "the resource snapshot to roll out is not a primary resource snapshot",
			"resourceSnapshot", klog.KRef(snapshotKey.Namespace, snapshotKey.Name), "subIndex", subIdx)
	}

	// Check if the resource snapshot is actually owned by the placement policy.
	if !metav1.IsControlledBy(resourceSnapshot, placementPolicy) {
		return nil, errors.NewUserError(nil, "the resource snapshot to roll out is not owned by the linked placement policy",
			"resourceSnapshot", klog.KRef(snapshotKey.Namespace, snapshotKey.Name),
			"placementPolicy", klog.KObj(placementPolicy))
	}

	return resourceSnapshot, nil
}
