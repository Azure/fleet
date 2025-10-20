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

// Package placement features a controller to reconcile the clusterResourcePlacement or resourcePlacement changes.
package placement

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	hubmetrics "github.com/kubefleet-dev/kubefleet/pkg/metrics/hub"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/annotations"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/labels"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
	fleettime "github.com/kubefleet-dev/kubefleet/pkg/utils/time"
)

// The max size of an object in k8s is 1.5MB because of ETCD limit https://etcd.io/docs/v3.3/dev-guide/limit/.
// We choose 800KB as the soft limit for all the selected resources within one resourceSnapshot object because of this test in k8s which checks
// if object size is greater than 1MB https://github.com/kubernetes/kubernetes/blob/db1990f48b92d603f469c1c89e2ad36da1b74846/test/integration/master/synthetic_master_test.go#L337
var resourceSnapshotResourceSizeLimit = 800 * (1 << 10) // 800KB

// We use a safety resync period to requeue all the finished request just in case there is a bug in the system.
// TODO: unify all the controllers with this pattern and make this configurable in place of the controller runtime resync period.
const controllerResyncPeriod = 30 * time.Minute

// Reconciler reconciles a cluster resource placement object
type Reconciler struct {
	// the informer contains the cache for all the resources we need.
	InformerManager informer.Manager

	// RestMapper is used to convert between gvk and gvr on known resources.
	RestMapper meta.RESTMapper

	// Client is used to update objects which goes to the api server directly.
	Client client.Client

	// UncachedReader is the uncached read-only client for accessing Kubernetes API server; in most cases client should
	// be used instead, unless consistency becomes a serious concern.
	// It's only needed by v1beta1 APIs.
	UncachedReader client.Reader

	// ResourceConfig contains all the API resources that we won't select based on allowed or skipped propagating APIs option.
	ResourceConfig *utils.ResourceConfig

	// SkippedNamespaces contains the namespaces that we should not propagate.
	SkippedNamespaces map[string]bool

	Recorder record.EventRecorder

	Scheme *runtime.Scheme

	// ResourceSnapshotCreationMinimumInterval is the minimum interval to create a new resourcesnapshot
	// to avoid too frequent updates.
	ResourceSnapshotCreationMinimumInterval time.Duration

	// ResourceChangesCollectionDuration is the duration for collecting resource changes into one snapshot.
	ResourceChangesCollectionDuration time.Duration
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	placementKey, ok := key.(string)
	if !ok {
		err := fmt.Errorf("get place key %+v not of type string", key)
		klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "We have encountered a fatal error that can't be retried, requeue after a day")
		return ctrl.Result{}, nil // ignore this unexpected error
	}

	startTime := time.Now()
	klog.V(2).InfoS("Placement reconciliation starts", "placementKey", placementKey)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Placement reconciliation ends", "placementKey", placementKey, "latency", latency)
	}()

	placementObj, err := controller.FetchPlacementFromKey(ctx, r.Client, queue.PlacementKey(placementKey))
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound placement", "placementKey", placementKey)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get placement", "placementKey", placementKey)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	if placementObj.GetDeletionTimestamp() != nil {
		return r.handleDelete(ctx, placementObj)
	}

	// register finalizer
	if !controllerutil.ContainsFinalizer(placementObj, fleetv1beta1.PlacementCleanupFinalizer) {
		controllerutil.AddFinalizer(placementObj, fleetv1beta1.PlacementCleanupFinalizer)
		if err := r.Client.Update(ctx, placementObj); err != nil {
			klog.ErrorS(err, "Failed to add placement finalizer", "placement", klog.KObj(placementObj))
			return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
		}
	}
	defer emitPlacementStatusMetric(placementObj)
	return r.handleUpdate(ctx, placementObj)
}

func (r *Reconciler) handleDelete(ctx context.Context, placementObj fleetv1beta1.PlacementObj) (ctrl.Result, error) {
	placementKObj := klog.KObj(placementObj)
	if !controllerutil.ContainsFinalizer(placementObj, fleetv1beta1.PlacementCleanupFinalizer) {
		klog.V(4).InfoS("placement is being deleted and no cleanup work needs to be done by the placement controller, waiting for the scheduler to cleanup the bindings", "placement", placementKObj)
		return ctrl.Result{}, nil
	}
	klog.V(2).InfoS("Removing snapshots created by placement", "placement", placementKObj)
	if err := controller.DeletePolicySnapshots(ctx, r.Client, placementObj); err != nil {
		return ctrl.Result{}, err
	}
	if err := controller.DeleteResourceSnapshots(ctx, r.Client, placementObj); err != nil {
		return ctrl.Result{}, err
	}
	// change the metrics to add nameplace of namespace
	hubmetrics.FleetPlacementStatusLastTimeStampSeconds.DeletePartialMatch(prometheus.Labels{"namespace": placementObj.GetNamespace(), "name": placementObj.GetName()})
	controllerutil.RemoveFinalizer(placementObj, fleetv1beta1.PlacementCleanupFinalizer)
	if err := r.Client.Update(ctx, placementObj); err != nil {
		klog.ErrorS(err, "Failed to remove placement finalizer", "placement", placementKObj)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Removed placement-cleanup finalizer", "placement", placementKObj)
	r.Recorder.Event(placementObj, corev1.EventTypeNormal, "PlacementCleanupFinalizerRemoved", "Deleted the snapshots and removed the placement cleanup finalizer")
	return ctrl.Result{}, nil
}

// handleUpdate handles the create/update placement event.
// It creates corresponding clusterSchedulingPolicySnapshot and clusterResourceSnapshot if needed and updates the status based on
// clusterSchedulingPolicySnapshot status and work status.
// If the error type is ErrUnexpectedBehavior, the controller will skip the reconciling.
func (r *Reconciler) handleUpdate(ctx context.Context, placementObj fleetv1beta1.PlacementObj) (ctrl.Result, error) {
	revisionLimit := int32(defaulter.DefaultRevisionHistoryLimitValue)
	placementKObj := klog.KObj(placementObj)
	oldPlacement := placementObj.DeepCopyObject().(fleetv1beta1.PlacementObj)
	placementSpec := placementObj.GetPlacementSpec()

	if placementSpec.RevisionHistoryLimit != nil {
		if revisionLimit <= 0 {
			err := fmt.Errorf("invalid placement %s/%s: invalid revisionHistoryLimit %d", placementObj.GetNamespace(), placementObj.GetName(), revisionLimit)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Invalid revisionHistoryLimit value and using default value instead", "placement", placementKObj)
		} else {
			revisionLimit = *placementSpec.RevisionHistoryLimit
		}
	}

	// Validate namespace selector consistency for NamespaceAccessible CRPs.
	if isNamespaceAccessibleCRP(placementObj) {
		isValid, err := r.validateNamespaceSelectorConsistency(ctx, placementObj)
		if err != nil {
			klog.V(2).ErrorS(err, "Namespace resource selector validation failed for NamespaceAccessible CRP", "placement", placementKObj)
			return ctrl.Result{}, err
		}
		if !isValid {
			klog.V(2).InfoS("Invalid Namespace resource selector specified for NamespaceAccessible CRP, stopping reconciliation", "placement", placementKObj)
			return ctrl.Result{}, nil
		}
	}

	// validate the resource selectors first before creating any snapshot
	envelopeObjCount, selectedResources, selectedResourceIDs, err := r.selectResourcesForPlacement(placementObj)
	if err != nil {
		klog.ErrorS(err, "Failed to select the resources", "placement", placementKObj)
		if !errors.Is(err, controller.ErrUserError) {
			return ctrl.Result{}, err
		}

		// TODO, create a separate user type error struct to improve the user facing messages
		scheduleCondition := metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               getPlacementScheduledConditionType(placementObj),
			Reason:             condition.InvalidResourceSelectorsReason,
			Message:            fmt.Sprintf("The resource selectors are invalid: %v", err),
			ObservedGeneration: placementObj.GetGeneration(),
		}
		placementObj.SetConditions(scheduleCondition)

		if updateErr := r.Client.Status().Update(ctx, placementObj); updateErr != nil {
			klog.ErrorS(updateErr, "Failed to update the status", "placement", placementKObj)
			return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(updateErr)
		}
		klog.V(2).InfoS("Updated the placement status with scheduled condition", "placement", placementKObj)

		if isNamespaceAccessibleCRP(placementObj) {
			if err := r.handleNamespaceAccessibleCRP(ctx, placementObj); err != nil {
				return ctrl.Result{}, err
			}
		}

		// no need to retry faster, the user needs to fix the resource selectors
		return ctrl.Result{RequeueAfter: controllerResyncPeriod}, nil
	}

	latestSchedulingPolicySnapshot, err := r.getOrCreateSchedulingPolicySnapshot(ctx, placementObj, int(revisionLimit))
	if err != nil {
		klog.ErrorS(err, "Failed to select resources for placement", "placement", placementKObj)
		return ctrl.Result{}, err
	}

	createResourceSnapshotRes, latestResourceSnapshot, err := r.getOrCreateResourceSnapshot(ctx, placementObj, envelopeObjCount,
		&fleetv1beta1.ResourceSnapshotSpec{SelectedResources: selectedResources}, int(revisionLimit))
	if err != nil {
		return ctrl.Result{}, err
	}

	// We don't requeue the request here immediately so that placement can keep tracking the rollout status.
	if createResourceSnapshotRes.Requeue {
		latestResourceSnapshotKObj := klog.KObj(latestResourceSnapshot)
		// We cannot create the resource snapshot immediately because of the resource snapshot creation interval.
		// Rebuild the seletedResourceIDs using the latestResourceSnapshot.
		latestResourceSnapshotIndex, err := labels.ExtractResourceIndexFromResourceSnapshot(latestResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to extract the resource index from the resourceSnapshot", "placement", placementKObj, "resourceSnapshot", latestResourceSnapshotKObj)
			return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
		}
		placementKey := controller.GetObjectKeyFromNamespaceName(placementObj.GetNamespace(), placementObj.GetName())
		selectedResourceIDs, err = controller.CollectResourceIdentifiersUsingMasterResourceSnapshot(ctx, r.Client, placementKey, latestResourceSnapshot, strconv.Itoa(latestResourceSnapshotIndex))
		if err != nil {
			klog.ErrorS(err, "Failed to collect resource identifiers from the resourceSnapshot", "placement", placementKObj, "resourceSnapshot", latestResourceSnapshotKObj)
			return ctrl.Result{}, err
		}
		klog.V(2).InfoS("Fetched the selected resources from the lastestResourceSnapshot", "placement", placementKObj, "resourceSnapshot", latestResourceSnapshotKObj, "generation", placementObj.GetGeneration())
	}

	// isScheduleFullfilled is to indicate whether we need to requeue the placement request to track the rollout status.
	isScheduleFullfilled, err := r.setPlacementStatus(ctx, placementObj, selectedResourceIDs, latestSchedulingPolicySnapshot, latestResourceSnapshot)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Client.Status().Update(ctx, placementObj); err != nil {
		klog.ErrorS(err, "Failed to update the status", "placement", placementKObj)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Updated the placement status", "placement", placementKObj)

	if isNamespaceAccessibleCRP(placementObj) {
		if err := r.handleNamespaceAccessibleCRP(ctx, placementObj); err != nil {
			return ctrl.Result{}, err
		}
	}

	// We skip checking the last resource condition (available) because it will be covered by checking isRolloutCompleted func.
	for i := condition.RolloutStartedCondition; i < condition.TotalCondition-1; i++ {
		var oldCond, newCond *metav1.Condition
		conditionType := getPlacementConditionType(placementObj, i)
		oldCond = oldPlacement.GetCondition(conditionType)
		newCond = placementObj.GetCondition(conditionType)
		if !condition.IsConditionStatusTrue(oldCond, oldPlacement.GetGeneration()) &&
			condition.IsConditionStatusTrue(newCond, placementObj.GetGeneration()) {
			klog.V(2).InfoS("Placement resource condition status has been changed to true", "placement", placementKObj, "generation", placementObj.GetGeneration(), "condition", conditionType)
			r.Recorder.Event(placementObj, corev1.EventTypeNormal, i.EventReasonForTrue(), i.EventMessageForTrue())
		}
	}

	// Rollout is considered to be completed when all the expected condition types are set to the
	// True status.
	if isRolloutCompleted(placementObj) {
		if !isRolloutCompleted(oldPlacement) {
			klog.V(2).InfoS("Placement has finished the rollout process and reached the desired status", "placement", placementKObj, "generation", placementObj.GetGeneration())
			r.Recorder.Event(placementObj, corev1.EventTypeNormal, "PlacementRolloutCompleted", "Placement has finished the rollout process and reached the desired status")
		}
		if createResourceSnapshotRes.Requeue {
			klog.V(2).InfoS("Requeue the request to handle the new resource snapshot", "placement", placementKObj, "generation", placementObj.GetGeneration())
			// We requeue the request to handle the resource snapshot.
			return createResourceSnapshotRes, nil
		}
		// We don't need to requeue any request now by watching the binding changes
		return ctrl.Result{}, nil
	}

	if !isScheduleFullfilled {
		// Note:
		// If the scheduledCondition is failed, it means the placement requirement cannot be satisfied fully. For example,
		// pickN deployment requires 5 clusters and scheduler schedules the resources on 3 clusters. And the appliedCondition
		// could be true when resources are applied successfully on these 3 clusters and the detailed the resourcePlacementStatuses
		// need to be populated.
		// So that we cannot rely on the scheduledCondition as false to decide whether to requeue the request.

		// When isScheduleFullfilled is false, either scheduler has not finished the scheduling or none of the clusters could be selected.
		// Once the policy snapshot status changes, the policy snapshot watcher should enqueue the request.
		// Here we requeue the request to prevent a bug in the watcher.
		klog.V(2).InfoS("Scheduler has not scheduled any cluster yet and requeue the request as a backup",
			"placement", placementKObj, "scheduledCondition", placementObj.GetCondition(string(fleetv1beta1.ClusterResourcePlacementScheduledConditionType)), "generation", placementObj.GetGeneration())
		if createResourceSnapshotRes.Requeue {
			klog.V(2).InfoS("Requeue the request to handle the new resource snapshot", "placement", placementKObj, "generation", placementObj.GetGeneration())
			// We requeue the request to handle the resource snapshot.
			return createResourceSnapshotRes, nil
		}
		return ctrl.Result{RequeueAfter: controllerResyncPeriod}, nil
	}
	klog.V(2).InfoS("Placement rollout has not finished yet and requeue the request", "placement", placementKObj, "status", placementObj.GetPlacementStatus(), "generation", placementObj.GetGeneration())
	if createResourceSnapshotRes.Requeue {
		klog.V(2).InfoS("Requeue the request to handle the new resource snapshot", "placement", placementKObj, "generation", placementObj.GetGeneration())
		// We requeue the request to handle the resource snapshot.
		return createResourceSnapshotRes, nil
	}
	// no need to requeue the request as the binding status will be changed but we add a long resync loop just in case.
	return ctrl.Result{RequeueAfter: controllerResyncPeriod}, nil
}

func (r *Reconciler) getOrCreateSchedulingPolicySnapshot(ctx context.Context, placementObj fleetv1beta1.PlacementObj, revisionHistoryLimit int) (fleetv1beta1.PolicySnapshotObj, error) {
	placementKObj := klog.KObj(placementObj)
	placementSpec := placementObj.GetPlacementSpec()
	schedulingPolicy := placementSpec.Policy.DeepCopy()
	if schedulingPolicy != nil {
		schedulingPolicy.NumberOfClusters = nil // will exclude the numberOfClusters
	}
	policyHash, err := resource.HashOf(schedulingPolicy)
	if err != nil {
		klog.ErrorS(err, "Failed to generate policy hash of placement", "placement", placementKObj)
		return nil, controller.NewUnexpectedBehaviorError(err)
	}

	// latestPolicySnapshotIndex should be -1 when there is no snapshot.
	latestPolicySnapshot, latestPolicySnapshotIndex, err := r.lookupLatestSchedulingPolicySnapshot(ctx, placementObj)
	if err != nil {
		return nil, err
	}

	if latestPolicySnapshot != nil && string(latestPolicySnapshot.GetPolicySnapshotSpec().PolicyHash) == policyHash {
		if err := r.ensureLatestPolicySnapshot(ctx, placementObj, latestPolicySnapshot); err != nil {
			return nil, err
		}
		klog.V(2).InfoS("Policy has not been changed and updated the existing policySnapshot", "placement", placementKObj, "policySnapshot", klog.KObj(latestPolicySnapshot))
		return latestPolicySnapshot, nil
	}

	// Need to create new snapshot when 1) there is no snapshots or 2) the latest snapshot hash != current one.
	// mark the last policy snapshot as inactive if it is different from what we have now
	if latestPolicySnapshot != nil &&
		string(latestPolicySnapshot.GetPolicySnapshotSpec().PolicyHash) != policyHash &&
		latestPolicySnapshot.GetLabels()[fleetv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		// set the latest label to false first to make sure there is only one or none active policy snapshot
		labels := latestPolicySnapshot.GetLabels()
		labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
		latestPolicySnapshot.SetLabels(labels)
		if err := r.Client.Update(ctx, latestPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "placement", placementKObj, "policySnapshot", klog.KObj(latestPolicySnapshot))
			return nil, controller.NewUpdateIgnoreConflictError(err)
		}
		klog.V(2).InfoS("Marked the existing policySnapshot as inactive", "placement", placementKObj, "policySnapshot", klog.KObj(latestPolicySnapshot))
	}

	// delete redundant snapshot revisions before creating a new snapshot to guarantee that the number of snapshots
	// won't exceed the limit.
	if err := r.deleteRedundantSchedulingPolicySnapshots(ctx, placementObj, revisionHistoryLimit); err != nil {
		return nil, err
	}

	// create a new policy snapshot
	latestPolicySnapshotIndex++
	newPolicySnapshot := controller.BuildPolicySnapshot(placementObj, latestPolicySnapshotIndex, policyHash)

	policySnapshotKObj := klog.KObj(newPolicySnapshot)
	if err := controllerutil.SetControllerReference(placementObj, newPolicySnapshot, r.Scheme); err != nil {
		klog.ErrorS(err, "Failed to set owner reference", "policySnapshot", policySnapshotKObj)
		// should never happen
		return nil, controller.NewUnexpectedBehaviorError(err)
	}

	if err := r.Client.Create(ctx, newPolicySnapshot); err != nil {
		klog.ErrorS(err, "Failed to create new policySnapshot", "policySnapshot", policySnapshotKObj)
		return nil, controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Created new policySnapshot", "placement", placementKObj, "policySnapshot", policySnapshotKObj)
	return newPolicySnapshot, nil
}

func (r *Reconciler) deleteRedundantSchedulingPolicySnapshots(ctx context.Context, placementObj fleetv1beta1.PlacementObj, revisionHistoryLimit int) error {
	sortedList, err := r.listSortedSchedulingPolicySnapshots(ctx, placementObj)
	if err != nil {
		return err
	}

	items := sortedList.GetPolicySnapshotObjs()
	if len(items) < revisionHistoryLimit {
		return nil
	}

	if len(items)-revisionHistoryLimit > 0 {
		// We always delete before creating a new snapshot, the snapshot size should never exceed the limit as there is
		// no finalizer added and object should be deleted immediately.
		klog.Warning("The number of policySnapshots exceeds the revisionHistoryLimit and it should never happen", "placement", klog.KObj(placementObj), "numberOfSnapshots", len(items), "revisionHistoryLimit", revisionHistoryLimit)
	}

	// In normal situation, The max of len(sortedList) should be revisionHistoryLimit.
	// We just need to delete one policySnapshot before creating a new one.
	// As a result of defensive programming, it will delete any redundant snapshots which could be more than one.
	for i := 0; i <= len(items)-revisionHistoryLimit; i++ { // need to reserve one slot for the new snapshot
		if err := r.Client.Delete(ctx, items[i]); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete policySnapshot", "placement", klog.KObj(placementObj), "policySnapshot", klog.KObj(items[i]))
			return controller.NewAPIServerError(false, err)
		}
	}
	return nil
}

// deleteRedundantResourceSnapshots handles multiple snapshots in a group.
func (r *Reconciler) deleteRedundantResourceSnapshots(ctx context.Context, placementObj fleetv1beta1.PlacementObj, revisionHistoryLimit int) error {
	sortedList, err := r.listSortedResourceSnapshots(ctx, placementObj)
	if err != nil {
		return err
	}

	items := sortedList.GetResourceSnapshotObjs()
	if len(items) < revisionHistoryLimit {
		// If the number of existing snapshots is less than the limit no matter how many snapshots in a group, we don't
		// need to delete any snapshots.
		// Skip the checking and deleting.
		return nil
	}

	placementKObj := klog.KObj(placementObj)
	lastGroupIndex := -1
	groupCounter := 0

	// delete the snapshots from the end as there are could be multiple snapshots in a group in order to keep the latest
	// snapshots from the end.
	for i := len(items) - 1; i >= 0; i-- {
		snapshotKObj := klog.KObj(items[i])
		ii, err := labels.ExtractResourceIndexFromResourceSnapshot(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "placement", placementKObj, "resourceSnapshot", snapshotKObj)
			return controller.NewUnexpectedBehaviorError(err)
		}
		if ii != lastGroupIndex {
			groupCounter++
			lastGroupIndex = ii
		}
		if groupCounter < revisionHistoryLimit { // need to reserve one slot for the new snapshot
			// When the number of group is less than the revision limit, skipping deleting the snapshot.
			continue
		}
		if err := r.Client.Delete(ctx, items[i]); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete resourceSnapshot", "placement", placementKObj, "resourceSnapshot", snapshotKObj)
			return controller.NewAPIServerError(false, err)
		}
	}
	if groupCounter-revisionHistoryLimit > 0 {
		// We always delete before creating a new snapshot, the snapshot group size should never exceed the limit
		// as there is no finalizer added and the object should be deleted immediately.
		klog.Warning("The number of resourceSnapshot groups exceeds the revisionHistoryLimit and it should never happen", "placement", placementKObj, "numberOfSnapshotGroups", groupCounter, "revisionHistoryLimit", revisionHistoryLimit)
	}
	return nil
}

// getOrCreateResourceSnapshot gets or creates a resource snapshot for the given placement.
// It returns the latest resource snapshot if it exists and is up to date, otherwise it creates a new one.
// It also returns the ctrl.Result to indicate whether the request should be requeued or not.
// Note: when the ctrl.Result.Requeue is true, it still returns the current latest resourceSnapshot so that
// placement can update the rollout status.
func (r *Reconciler) getOrCreateResourceSnapshot(ctx context.Context, placement fleetv1beta1.PlacementObj, envelopeObjCount int, resourceSnapshotSpec *fleetv1beta1.ResourceSnapshotSpec, revisionHistoryLimit int) (ctrl.Result, fleetv1beta1.ResourceSnapshotObj, error) {
	placementKObj := klog.KObj(placement)
	resourceHash, err := resource.HashOf(resourceSnapshotSpec)
	if err != nil {
		klog.ErrorS(err, "Failed to generate resource hash", "placement", placementKObj)
		return ctrl.Result{}, nil, controller.NewUnexpectedBehaviorError(err)
	}

	// latestResourceSnapshotIndex should be -1 when there is no snapshot.
	latestResourceSnapshot, latestResourceSnapshotIndex, err := r.lookupLatestResourceSnapshot(ctx, placement)
	if err != nil {
		return ctrl.Result{}, nil, err
	}

	latestResourceSnapshotHash := ""
	numberOfSnapshots := -1
	if latestResourceSnapshot != nil {
		latestResourceSnapshotHash, err = annotations.ParseResourceGroupHashFromAnnotation(latestResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to get the ResourceGroupHashAnnotation", "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, nil, controller.NewUnexpectedBehaviorError(err)
		}
		numberOfSnapshots, err = annotations.ExtractNumberOfResourceSnapshotsFromResourceSnapshot(latestResourceSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to get the NumberOfResourceSnapshotsAnnotation", "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, nil, controller.NewUnexpectedBehaviorError(err)
		}
	}

	shouldCreateNewMasterResourceSnapshot := true
	// This index indicates the selected resource in the split selectedResourceList, if this index is zero we start
	// from creating the master resourceSnapshot if it's greater than zero it means that the master resourceSnapshot
	// got created but not all sub-indexed resourceSnapshots have been created yet. It covers the corner case where the
	// controller crashes in the middle.
	resourceSnapshotStartIndex := 0
	if latestResourceSnapshot != nil && latestResourceSnapshotHash == resourceHash {
		if err := r.ensureLatestResourceSnapshot(ctx, latestResourceSnapshot); err != nil {
			return ctrl.Result{}, nil, err
		}
		// check to see all that the master cluster resource snapshot and sub-indexed snapshots belonging to the same group index exists.
		resourceSnapshotList, err := controller.ListAllResourceSnapshotWithAnIndex(ctx, r.Client, latestResourceSnapshot.GetLabels()[fleetv1beta1.ResourceIndexLabel], placement.GetName(), placement.GetNamespace())
		if err != nil {
			klog.ErrorS(err, "Failed to list the latest group resourceSnapshots associated with the placement", "placement", placementKObj)
			return ctrl.Result{}, nil, controller.NewAPIServerError(true, err)
		}
		if len(resourceSnapshotList.GetResourceSnapshotObjs()) == numberOfSnapshots {
			klog.V(2).InfoS("resourceSnapshots have not changed", "placement", placementKObj, "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, latestResourceSnapshot, nil
		}
		// we should not create a new master cluster resource snapshot.
		shouldCreateNewMasterResourceSnapshot = false
		// set resourceSnapshotStartIndex to start from this index, so we don't try to recreate existing sub-indexed cluster resource snapshots.
		resourceSnapshotStartIndex = len(resourceSnapshotList.GetResourceSnapshotObjs())
	}

	// Need to create new snapshot when 1) there is no snapshots or 2) the latest snapshot hash != current one.
	// mark the last resource snapshot as inactive if it is different from what we have now or 3) when some
	// sub-indexed cluster resource snapshots belonging to the same group have not been created, the master
	// cluster resource snapshot should exist and be latest.
	if latestResourceSnapshot != nil && latestResourceSnapshotHash != resourceHash && latestResourceSnapshot.GetLabels()[fleetv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		// When the latest resource snapshot without the isLastest label, it means it fails to create the new
		// resource snapshot in the last reconcile and we don't need to check and delay the request.
		res, error := r.shouldCreateNewResourceSnapshotNow(ctx, latestResourceSnapshot)
		if error != nil {
			return ctrl.Result{}, nil, error
		}
		if res.Requeue {
			// If the latest resource snapshot is not ready to be updated, we requeue the request.
			return res, latestResourceSnapshot, nil
		}
		shouldCreateNewMasterResourceSnapshot = true
		// set the latest label to false first to make sure there is only one or none active resource snapshot
		labels := latestResourceSnapshot.GetLabels()
		labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
		latestResourceSnapshot.SetLabels(labels)
		if err := r.Client.Update(ctx, latestResourceSnapshot); err != nil {
			klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "resourceSnapshot", klog.KObj(latestResourceSnapshot))
			return ctrl.Result{}, nil, controller.NewUpdateIgnoreConflictError(err)
		}
		klog.V(2).InfoS("Marked the existing resourceSnapshot as inactive", "placement", placementKObj, "resourceSnapshot", klog.KObj(latestResourceSnapshot))
	}

	// only delete redundant resource snapshots and increment the latest resource snapshot index if new master resource snapshot is to be created.
	if shouldCreateNewMasterResourceSnapshot {
		// delete redundant snapshot revisions before creating a new master resource snapshot to guarantee that the number of snapshots won't exceed the limit.
		if err := r.deleteRedundantResourceSnapshots(ctx, placement, revisionHistoryLimit); err != nil {
			return ctrl.Result{}, nil, err
		}
		latestResourceSnapshotIndex++
	}
	// split selected resources as list of lists.
	selectedResourcesList := controller.SplitSelectedResources(resourceSnapshotSpec.SelectedResources, resourceSnapshotResourceSizeLimit)
	var resourceSnapshot fleetv1beta1.ResourceSnapshotObj
	for i := resourceSnapshotStartIndex; i < len(selectedResourcesList); i++ {
		if i == 0 {
			resourceSnapshot = BuildMasterResourceSnapshot(latestResourceSnapshotIndex, len(selectedResourcesList), envelopeObjCount, placement.GetName(), placement.GetNamespace(), resourceHash, selectedResourcesList[i])
			latestResourceSnapshot = resourceSnapshot
		} else {
			resourceSnapshot = BuildSubIndexResourceSnapshot(latestResourceSnapshotIndex, i-1, placement.GetName(), placement.GetNamespace(), selectedResourcesList[i])
		}
		if err = r.createResourceSnapshot(ctx, placement, resourceSnapshot); err != nil {
			return ctrl.Result{}, nil, err
		}
	}
	// shouldCreateNewMasterResourceSnapshot is used here to be defensive in case of the regression.
	if shouldCreateNewMasterResourceSnapshot && len(selectedResourcesList) == 0 {
		resourceSnapshot = BuildMasterResourceSnapshot(latestResourceSnapshotIndex, 1, envelopeObjCount, placement.GetName(), placement.GetNamespace(), resourceHash, []fleetv1beta1.ResourceContent{})
		latestResourceSnapshot = resourceSnapshot
		if err = r.createResourceSnapshot(ctx, placement, resourceSnapshot); err != nil {
			return ctrl.Result{}, nil, err
		}
	}
	return ctrl.Result{}, latestResourceSnapshot, nil
}

// shouldCreateNewResourceSnapshotNow checks whether it is ready to create the new resource snapshot to avoid too frequent creation
// based on the configured resourceSnapshotCreationMinimumInterval and resourceChangesCollectionDuration.
func (r *Reconciler) shouldCreateNewResourceSnapshotNow(ctx context.Context, latestResourceSnapshot fleetv1beta1.ResourceSnapshotObj) (ctrl.Result, error) {
	if r.ResourceSnapshotCreationMinimumInterval <= 0 && r.ResourceChangesCollectionDuration <= 0 {
		return ctrl.Result{}, nil
	}

	// We respect the ResourceChangesCollectionDuration to allow the controller to bundle all the resource changes into one snapshot.
	snapshotKObj := klog.KObj(latestResourceSnapshot)
	now := time.Now()
	nextResourceSnapshotCandidateDetectionTime, err := annotations.ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot(latestResourceSnapshot)
	if nextResourceSnapshotCandidateDetectionTime.IsZero() || err != nil {
		if err != nil {
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Failed to get the NextResourceSnapshotCandidateDetectionTimeAnnotation", "resourceSnapshot", snapshotKObj)
		}
		// If the annotation is not set, set next resource snapshot candidate detection time is now.
		if latestResourceSnapshot.GetAnnotations() == nil {
			latestResourceSnapshot.SetAnnotations(make(map[string]string))
		}
		latestResourceSnapshot.GetAnnotations()[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation] = now.Format(time.RFC3339)
		if err := r.Client.Update(ctx, latestResourceSnapshot); err != nil {
			klog.ErrorS(err, "Failed to update the NextResourceSnapshotCandidateDetectionTime annotation", "resourceSnapshot", snapshotKObj)
			return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
		}
		nextResourceSnapshotCandidateDetectionTime = now
		klog.V(2).InfoS("Updated the NextResourceSnapshotCandidateDetectionTime annotation", "resourceSnapshot", snapshotKObj, "nextResourceSnapshotCandidateDetectionTimeAnnotation", now.Format(time.RFC3339))
	}
	nextCreationTime := fleettime.MaxTime(nextResourceSnapshotCandidateDetectionTime.Add(r.ResourceChangesCollectionDuration), latestResourceSnapshot.GetCreationTimestamp().Add(r.ResourceSnapshotCreationMinimumInterval))
	if now.Before(nextCreationTime) {
		// If the next resource snapshot creation time is not reached, we requeue the request to avoid too frequent update.
		klog.V(2).InfoS("Delaying the new resourceSnapshot creation",
			"resourceSnapshot", snapshotKObj, "nextCreationTime", nextCreationTime, "latestResourceSnapshotCreationTime", latestResourceSnapshot.GetCreationTimestamp(),
			"resourceSnapshotCreationMinimumInterval", r.ResourceSnapshotCreationMinimumInterval, "resourceChangesCollectionDuration", r.ResourceChangesCollectionDuration,
			"afterDuration", nextCreationTime.Sub(now))
		return ctrl.Result{Requeue: true, RequeueAfter: nextCreationTime.Sub(now)}, nil
	}
	return ctrl.Result{}, nil
}

// TODO: move this to library package
// buildMasterResourceSnapshot builds and returns the master resource snapshot for the latest resource snapshot index and selected resources.
func BuildMasterResourceSnapshot(latestResourceSnapshotIndex, resourceSnapshotCount, envelopeObjCount int, placementName, placementNamespace, resourceHash string, selectedResources []fleetv1beta1.ResourceContent) fleetv1beta1.ResourceSnapshotObj {
	labels := map[string]string{
		fleetv1beta1.PlacementTrackingLabel: placementName,
		fleetv1beta1.IsLatestSnapshotLabel:  strconv.FormatBool(true),
		fleetv1beta1.ResourceIndexLabel:     strconv.Itoa(latestResourceSnapshotIndex),
	}
	annotations := map[string]string{
		fleetv1beta1.ResourceGroupHashAnnotation:         resourceHash,
		fleetv1beta1.NumberOfResourceSnapshotsAnnotation: strconv.Itoa(resourceSnapshotCount),
		fleetv1beta1.NumberOfEnvelopedObjectsAnnotation:  strconv.Itoa(envelopeObjCount),
	}
	spec := fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}
	if placementNamespace == "" {
		// Cluster-scoped placement
		return &fleetv1beta1.ClusterResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, placementName, latestResourceSnapshotIndex),
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	} else {
		// Namespace-scoped placement
		return &fleetv1beta1.ResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, placementName, latestResourceSnapshotIndex),
				Namespace:   placementNamespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	}
}

// TODO: move this to library package
// BuildSubIndexResourceSnapshot builds and returns the sub index resource snapshot for both cluster-scoped and namespace-scoped placements.
// Returns a ClusterResourceSnapshot for cluster-scoped placements (empty namespace) or ResourceSnapshot for namespace-scoped placements.
func BuildSubIndexResourceSnapshot(latestResourceSnapshotIndex, resourceSnapshotSubIndex int, placementName, placementNamespace string, selectedResources []fleetv1beta1.ResourceContent) fleetv1beta1.ResourceSnapshotObj {
	labels := map[string]string{
		fleetv1beta1.PlacementTrackingLabel: placementName,
		fleetv1beta1.ResourceIndexLabel:     strconv.Itoa(latestResourceSnapshotIndex),
	}
	annotations := map[string]string{
		fleetv1beta1.SubindexOfResourceSnapshotAnnotation: strconv.Itoa(resourceSnapshotSubIndex),
	}
	spec := fleetv1beta1.ResourceSnapshotSpec{
		SelectedResources: selectedResources,
	}
	if placementNamespace == "" {
		// Cluster-scoped placement
		return &fleetv1beta1.ClusterResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, placementName, latestResourceSnapshotIndex, resourceSnapshotSubIndex),
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	} else {
		// Namespace-scoped placement
		return &fleetv1beta1.ResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, placementName, latestResourceSnapshotIndex, resourceSnapshotSubIndex),
				Namespace:   placementNamespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: spec,
		}
	}
}

// createResourceSnapshot sets placement owner reference on the resource snapshot and creates it.
// Now supports both cluster-scoped and namespace-scoped placements using interface types.
func (r *Reconciler) createResourceSnapshot(ctx context.Context, placementObj fleetv1beta1.PlacementObj, resourceSnapshot fleetv1beta1.ResourceSnapshotObj) error {
	resourceSnapshotKObj := klog.KObj(resourceSnapshot)
	if err := controllerutil.SetControllerReference(placementObj, resourceSnapshot, r.Scheme); err != nil {
		klog.ErrorS(err, "Failed to set owner reference", "resourceSnapshot", resourceSnapshotKObj)
		// should never happen
		return controller.NewUnexpectedBehaviorError(err)
	}
	if err := r.Client.Create(ctx, resourceSnapshot); err != nil {
		klog.ErrorS(err, "Failed to create new resourceSnapshot", "resourceSnapshot", resourceSnapshotKObj)
		return controller.NewAPIServerError(false, err)
	}
	klog.V(2).InfoS("Created new resourceSnapshot", "placement", klog.KObj(placementObj), "resourceSnapshot", resourceSnapshotKObj)
	return nil
}

// ensureLatestPolicySnapshot ensures the latest policySnapshot has the isLatest label and the numberOfClusters are updated for interface types.
func (r *Reconciler) ensureLatestPolicySnapshot(ctx context.Context, placementObj fleetv1beta1.PlacementObj, latest fleetv1beta1.PolicySnapshotObj) error {
	needUpdate := false
	latestKObj := klog.KObj(latest)
	labels := latest.GetLabels()
	if labels[fleetv1beta1.IsLatestSnapshotLabel] != strconv.FormatBool(true) {
		// When latestPolicySnapshot.Spec.PolicyHash == policyHash,
		// It could happen when the controller just sets the latest label to false for the old snapshot, and fails to
		// create a new policy snapshot.
		// And then the customers revert back their policy to the old one again.
		// In this case, the "latest" snapshot without isLatest label has the same policy hash as the current policy.
		labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(true)
		latest.SetLabels(labels)
		needUpdate = true
	}
	placementGeneration, err := annotations.ExtractObservedPlacementGenerationFromPolicySnapshot(latest)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the placement generation from the annotations", "policySnapshot", latestKObj)
		return controller.NewUnexpectedBehaviorError(err)
	}
	if placementGeneration != placementObj.GetGeneration() {
		annotations := latest.GetAnnotations()
		annotations[fleetv1beta1.CRPGenerationAnnotation] = strconv.FormatInt(placementObj.GetGeneration(), 10)
		latest.SetAnnotations(annotations)
		needUpdate = true
	}

	// Handle NumberOfClusters annotation for selectN type placements
	placementSpec := placementObj.GetPlacementSpec()
	if placementSpec.Policy != nil &&
		placementSpec.Policy.PlacementType == fleetv1beta1.PickNPlacementType &&
		placementSpec.Policy.NumberOfClusters != nil {
		oldCount, err := annotations.ExtractNumOfClustersFromPolicySnapshot(latest)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the numberOfClusterAnnotation", "policySnapshot", latestKObj)
			return controller.NewUnexpectedBehaviorError(err)
		}
		newCount := int(*placementSpec.Policy.NumberOfClusters)
		if oldCount != newCount {
			annotations := latest.GetAnnotations()
			annotations[fleetv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(newCount)
			latest.SetAnnotations(annotations)
			needUpdate = true
		}
	}
	if !needUpdate {
		return nil
	}
	if err := r.Client.Update(ctx, latest); err != nil {
		klog.ErrorS(err, "Failed to update the policySnapshot", "policySnapshot", latestKObj)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	return nil
}

// ensureLatestResourceSnapshot ensures the latest resourceSnapshot has the isLatest label, working with interface types.
func (r *Reconciler) ensureLatestResourceSnapshot(ctx context.Context, latest fleetv1beta1.ResourceSnapshotObj) error {
	labels := latest.GetLabels()
	if labels[fleetv1beta1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		return nil
	}
	// It could happen when the controller just sets the latest label to false for the old snapshot, and fails to
	// create a new resource snapshot.
	// And then the customers revert back their resource to the old one again.
	// In this case, the "latest" snapshot without isLatest label has the same resource hash as the current one.
	labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(true)
	latest.SetLabels(labels)
	if err := r.Client.Update(ctx, latest); err != nil {
		klog.ErrorS(err, "Failed to update the resourceSnapshot", "resourceSnapshot", klog.KObj(latest))
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("ResourceSnapshot's IsLatestSnapshotLabel was updated to true", "resourceSnapshot", klog.KObj(latest))
	return nil
}

// lookupLatestSchedulingPolicySnapshot finds the latest snapshots and its policy index.
// There will be only one active policy snapshot if exists.
// It first checks whether there is an active policy snapshot.
// If not, it finds the one whose policyIndex label is the largest.
// The policy index will always start from 0.
// Return error when 1) cannot list the snapshots 2) there are more than one active policy snapshots 3) snapshot has the
// invalid label value.
// 2 & 3 should never happen.
func (r *Reconciler) lookupLatestSchedulingPolicySnapshot(ctx context.Context, placement fleetv1beta1.PlacementObj) (fleetv1beta1.PolicySnapshotObj, int, error) {
	placementKey := types.NamespacedName{Name: placement.GetName(), Namespace: placement.GetNamespace()}
	snapshotList, err := controller.FetchLatestPolicySnapshot(ctx, r.Client, placementKey)
	if err != nil {
		return nil, -1, err
	}
	placementKObj := klog.KObj(placement)
	policySnapshotItems := snapshotList.GetPolicySnapshotObjs()
	if len(policySnapshotItems) == 1 {
		policyIndex, err := labels.ParsePolicyIndexFromLabel(policySnapshotItems[0])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the policy index label", "placement", placementKObj, "policySnapshot", klog.KObj(policySnapshotItems[0]))
			return nil, -1, controller.NewUnexpectedBehaviorError(err)
		}
		return policySnapshotItems[0], policyIndex, nil
	} else if len(policySnapshotItems) > 1 {
		// It means there are multiple active snapshots and should never happen.
		err := fmt.Errorf("there are %d active schedulingPolicySnapshots owned by placement %v", len(policySnapshotItems), placementKey)
		klog.ErrorS(err, "Invalid schedulingPolicySnapshots", "placement", placementKObj)
		return nil, -1, controller.NewUnexpectedBehaviorError(err)
	}
	// When there are no active snapshots, find the one who has the largest policy index.
	// It should be rare only when placement controller is crashed before creating the new active snapshot.
	sortedList, err := r.listSortedSchedulingPolicySnapshots(ctx, placement)
	if err != nil {
		return nil, -1, err
	}

	if len(sortedList.GetPolicySnapshotObjs()) == 0 {
		// The policy index of the first snapshot will start from 0.
		return nil, -1, nil
	}
	latestSnapshot := sortedList.GetPolicySnapshotObjs()[len(sortedList.GetPolicySnapshotObjs())-1]
	policyIndex, err := labels.ParsePolicyIndexFromLabel(latestSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the policy index label", "placement", placementKObj, "policySnapshot", klog.KObj(latestSnapshot))
		return nil, -1, controller.NewUnexpectedBehaviorError(err)
	}
	return latestSnapshot, policyIndex, nil
}

// listSortedSchedulingPolicySnapshots returns the policy snapshots sorted by the policy index.
// Now works with both cluster-scoped and namespaced policy snapshots using interface types.
func (r *Reconciler) listSortedSchedulingPolicySnapshots(ctx context.Context, placementObj fleetv1beta1.PlacementObj) (fleetv1beta1.PolicySnapshotList, error) {
	placementKey := types.NamespacedName{
		Namespace: placementObj.GetNamespace(),
		Name:      placementObj.GetName(),
	}

	snapshotList, err := controller.ListPolicySnapshots(ctx, r.Client, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list all policySnapshots", "placement", klog.KObj(placementObj))
		// placement controller needs a scheduling policy snapshot watcher to enqueue the placement request.
		// So the snapshots should be read from cache.
		return nil, controller.NewAPIServerError(true, err)
	}

	items := snapshotList.GetPolicySnapshotObjs()
	var errs []error
	sort.Slice(items, func(i, j int) bool {
		ii, err := labels.ParsePolicyIndexFromLabel(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the policy index label", "placement", klog.KObj(placementObj), "policySnapshot", klog.KObj(items[i]))
			errs = append(errs, err)
		}
		ji, err := labels.ParsePolicyIndexFromLabel(items[j])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the policy index label", "placement", klog.KObj(placementObj), "policySnapshot", klog.KObj(items[j]))
			errs = append(errs, err)
		}
		return ii < ji
	})

	if len(errs) > 0 {
		return nil, controller.NewUnexpectedBehaviorError(utilerrors.NewAggregate(errs))
	}

	return snapshotList, nil
}

// lookupLatestResourceSnapshot finds the latest snapshots and.
// There will be only one active resource snapshot if exists.
// It first checks whether there is an active resource snapshot.
// If not, it finds the one whose resourceIndex label is the largest.
// The resource index will always start from 0.
// lookupLatestResourceSnapshot finds the latest resource snapshots for the given placement.
// It works with both cluster-scoped (ClusterResourcePlacement) and namespace-scoped (ResourcePlacement) placements.
// There will be only one active resource snapshot if exists.
// It first checks whether there is an active resource snapshot.
// If not, it finds the one whose resourceIndex label is the largest.
// The resource index will always start from 0.
// Return error when 1) cannot list the snapshots 2) there are more than one active resource snapshots 3) snapshot has the
// invalid label value.
// 2 & 3 should never happen.
func (r *Reconciler) lookupLatestResourceSnapshot(ctx context.Context, placement fleetv1beta1.PlacementObj) (fleetv1beta1.ResourceSnapshotObj, int, error) {
	placementKObj := klog.KObj(placement)

	// Use the existing FetchLatestMasterResourceSnapshot function to get the master snapshot
	masterSnapshot, err := controller.FetchLatestMasterResourceSnapshot(ctx, r.Client, types.NamespacedName{Namespace: placement.GetNamespace(), Name: placement.GetName()})
	if err != nil {
		return nil, -1, err
	}
	if masterSnapshot != nil {
		// Extract resource index from the master snapshot
		resourceIndex, err := labels.ExtractResourceIndexFromResourceSnapshot(masterSnapshot)
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "resourceSnapshot", klog.KObj(masterSnapshot))
			return nil, -1, controller.NewUnexpectedBehaviorError(err)
		}
		return masterSnapshot, resourceIndex, nil
	}
	// When there are no active snapshots, find the first snapshot who has the largest resource index.
	// It should be rare only when placement is crashed before creating the new active snapshot.
	sortedList, err := r.listSortedResourceSnapshots(ctx, placement)
	if err != nil {
		return nil, -1, err
	}
	if len(sortedList.GetResourceSnapshotObjs()) == 0 {
		// The resource index of the first snapshot will start from 0.
		return nil, -1, nil
	}
	latestSnapshot := sortedList.GetResourceSnapshotObjs()[len(sortedList.GetResourceSnapshotObjs())-1]
	resourceIndex, err := labels.ExtractResourceIndexFromResourceSnapshot(latestSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the resource index label", "placement", placementKObj, "resourceSnapshot", klog.KObj(latestSnapshot))
		return nil, -1, controller.NewUnexpectedBehaviorError(err)
	}
	return latestSnapshot, resourceIndex, nil
}

// listSortedResourceSnapshots returns the resource snapshots sorted by its index and its subindex.
// Now works with both cluster-scoped and namespaced resource snapshots using interface types.
// The resourceSnapshot is less than the other one when resourceIndex is less.
// When the resourceIndex is equal, then order by the subindex.
// Note: the snapshot does not have subindex is the largest of a group and there should be only one in a group.
func (r *Reconciler) listSortedResourceSnapshots(ctx context.Context, placementObj fleetv1beta1.PlacementObj) (fleetv1beta1.ResourceSnapshotObjList, error) {
	placementKey := types.NamespacedName{
		Namespace: placementObj.GetNamespace(),
		Name:      placementObj.GetName(),
	}

	snapshotList, err := controller.ListAllResourceSnapshots(ctx, r.Client, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list all resourceSnapshots", "placement", klog.KObj(placementObj))
		return nil, controller.NewAPIServerError(true, err)
	}

	items := snapshotList.GetResourceSnapshotObjs()
	var errs []error
	sort.Slice(items, func(i, j int) bool {
		iKObj := klog.KObj(items[i])
		jKObj := klog.KObj(items[j])
		ii, err := labels.ExtractResourceIndexFromResourceSnapshot(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "placement", klog.KObj(placementObj), "resourceSnapshot", iKObj)
			errs = append(errs, err)
		}
		ji, err := labels.ExtractResourceIndexFromResourceSnapshot(items[j])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the resource index label", "placement", klog.KObj(placementObj), "resourceSnapshot", jKObj)
			errs = append(errs, err)
		}
		if ii != ji {
			return ii < ji
		}

		iDoesExist, iSubindex, err := annotations.ExtractSubindexFromResourceSnapshot(items[i])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the subindex index", "placement", klog.KObj(placementObj), "resourceSnapshot", iKObj)
			errs = append(errs, err)
		}
		jDoesExist, jSubindex, err := annotations.ExtractSubindexFromResourceSnapshot(items[j])
		if err != nil {
			klog.ErrorS(err, "Failed to parse the subindex index", "placement", klog.KObj(placementObj), "resourceSnapshot", jKObj)
			errs = append(errs, err)
		}

		// Both of the snapshots do not have subindex, which should not happen.
		if !iDoesExist && !jDoesExist {
			klog.ErrorS(err, "There are more than one resource snapshot which do not have subindex in a group", "placement", klog.KObj(placementObj), "resourceSnapshot", iKObj, "resourceSnapshot", jKObj)
			errs = append(errs, err)
		}

		if !iDoesExist { // check if it's the first snapshot
			return false
		}
		if !jDoesExist { // check if it's the first snapshot
			return true
		}
		return iSubindex < jSubindex
	})

	if len(errs) > 0 {
		return nil, controller.NewUnexpectedBehaviorError(utilerrors.NewAggregate(errs))
	}

	return snapshotList, nil
}

// TODO: further streamline the logic of setPlacementStatus
// setPlacementStatus returns if there is a cluster scheduled by the scheduler.
// it returns true if the cluster schedule succeeded, false otherwise.
func (r *Reconciler) setPlacementStatus(
	ctx context.Context,
	placementObj fleetv1beta1.PlacementObj,
	selectedResourceIDs []fleetv1beta1.ResourceIdentifier,
	latestSchedulingPolicySnapshot fleetv1beta1.PolicySnapshotObj,
	latestResourceSnapshot fleetv1beta1.ResourceSnapshotObj,
) (bool, error) {
	placementStatus := placementObj.GetPlacementStatus()
	placementStatus.SelectedResources = selectedResourceIDs

	scheduledCondition := buildScheduledCondition(placementObj, latestSchedulingPolicySnapshot)
	placementObj.SetConditions(scheduledCondition)
	// set ObservedResourceIndex from the latest resource snapshot's resource index label, before we set Synchronized, Applied conditions.
	placementStatus.ObservedResourceIndex = latestResourceSnapshot.GetLabels()[fleetv1beta1.ResourceIndexLabel]

	// When scheduledCondition is unknown, appliedCondition should be unknown too.
	// Note: If the scheduledCondition is failed, it means the placement requirement cannot be satisfied fully. For example,
	// pickN deployment requires 5 clusters and scheduler schedules the resources on 3 clusters. And the appliedCondition
	// could be true when resources are applied successfully on these 3 clusters and the detailed the resourcePlacementStatuses
	// need to be populated.
	if scheduledCondition.Status == metav1.ConditionUnknown {
		// For the new conditions, we no longer populate the remaining otherwise it's too complicated and the default condition
		// will be unknown.
		// skip populating detailed resourcePlacementStatus & work related conditions
		// reset other status fields
		// TODO: need to track whether we have deleted the resources for the last decisions.
		// The undeleted resources on these old clusters could lead to failed synchronized or applied condition.
		// Today, we only track the resources progress if the same cluster is selected again.
		klog.V(2).InfoS("Resetting the resource placement status since scheduled condition is unknown", "placement", klog.KObj(placementObj))
		placementStatus.PerClusterPlacementStatuses = []fleetv1beta1.PerClusterPlacementStatus{}
		return false, nil
	}

	// Classify cluster decisions; find out clusters that have been selected and
	// have not been selected.
	selected, unselected := classifyClusterDecisions(latestSchedulingPolicySnapshot.GetPolicySnapshotStatus().ClusterDecisions)
	// Calculate the number of clusters that should have been selected yet cannot be, due to
	// scheduling constraints.
	failedToScheduleClusterCount, err := calculateFailedToScheduleClusterCount(placementObj, selected, unselected)
	if err != nil {
		return false, err
	}

	// For clusters that have been selected, set the resource placement status based on the
	// respective resource binding status for each of them.
	expectedCondTypes := determineExpectedPlacementAndResourcePlacementStatusCondType(placementObj)
	perClusterStatus, perClusterCondTypeCounter, err := r.buildSelectedPerClusterPlacementStatuses(
		ctx, selected, expectedCondTypes, placementObj, latestSchedulingPolicySnapshot, latestResourceSnapshot)
	if err != nil {
		return false, err
	}

	// For clusters that failed to get scheduled, set a resource placement status with the failed to schedule condition for each of them.
	perClusterStatus = append(perClusterStatus, buildFailedToSchedulePerClusterPlacementStatuses(unselected, failedToScheduleClusterCount, placementObj)...)
	placementStatus.PerClusterPlacementStatuses = perClusterStatus
	klog.V(2).InfoS("Updated placement status for each individual cluster", "selectedNoCluster", len(selected), "unselectedNoCluster", len(unselected), "failedToScheduleClusterCount", failedToScheduleClusterCount, "placement", klog.KObj(placementObj))

	// Prepare the conditions for the placement object itself.
	if len(selected) == 0 {
		// There is no selected cluster at all. It could be that there is no matching cluster
		// given the current scheduling policy; there remains a corner case as well where a cluster
		// has been selected before (with resources being possibly applied), but has now
		// left the fleet. To address this corner case, Fleet here will remove all lingering
		// conditions (any condition type other than Scheduled).

		// Note that the scheduled condition has been set earlier in this method.
		placementStatus.Conditions = []metav1.Condition{}
		placementObj.SetConditions(scheduledCondition)
		return isPolicySelectingNoClusters(placementObj.GetPlacementSpec().Policy), nil
	}

	if placementObj.GetPlacementSpec().Strategy.Type == fleetv1beta1.ExternalRolloutStrategyType {
		// For external rollout strategy, if clusters observe different resource snapshot versions,
		// we set RolloutStarted to Unknown without any other conditions since we do not know exactly which version is rolling out.
		// We also need to reset ObservedResourceIndex and selectedResources.
		rolloutStartedUnknown, err := r.determineRolloutStateForPlacementWithExternalRolloutStrategy(ctx, placementObj, selected, perClusterStatus, selectedResourceIDs)
		if err != nil || rolloutStartedUnknown {
			return true, err
		}
	}
	setPlacementConditions(placementObj, perClusterStatus, perClusterCondTypeCounter, expectedCondTypes)
	klog.V(2).InfoS("Updated placement status for the entire placement", "numResourcePlacementStatus", len(perClusterStatus), "placement", klog.KObj(placementObj))
	return true, nil
}

// isPolicySelectingNoClusters checks if the given placement policy is selecting no clusters by design.
func isPolicySelectingNoClusters(policy *fleetv1beta1.PlacementPolicy) bool {
	if policy == nil {
		return false
	}
	return (policy.PlacementType == fleetv1beta1.PickNPlacementType && *policy.NumberOfClusters == 0) || (policy.PlacementType == fleetv1beta1.PickFixedPlacementType && len(policy.ClusterNames) == 0)
}

// determineRolloutStateForPlacementWithExternalRolloutStrategy checks the rollout state for the placement with external rollout strategy.
func (r *Reconciler) determineRolloutStateForPlacementWithExternalRolloutStrategy(
	ctx context.Context,
	placementObj fleetv1beta1.PlacementObj,
	selected []*fleetv1beta1.ClusterDecision,
	allRPS []fleetv1beta1.PerClusterPlacementStatus,
	selectedResourceIDs []fleetv1beta1.ResourceIdentifier,
) (bool, error) {
	if len(selected) == 0 {
		// This should not happen as we already checked in setPlacementStatus.
		err := controller.NewUnexpectedBehaviorError(fmt.Errorf("selected cluster list is empty for placement %s/%s when checking per-cluster rollout state", placementObj.GetNamespace(), placementObj.GetName()))
		klog.ErrorS(err, "Should not happen: selected cluster list is empty in determineRolloutStateForPlacementWithExternalRolloutStrategy()")
		return false, err
	}

	differentResourceIndicesObserved := false
	observedResourceIndex := allRPS[0].ObservedResourceIndex
	for i := range len(selected) - 1 {
		if allRPS[i].ObservedResourceIndex != allRPS[i+1].ObservedResourceIndex {
			differentResourceIndicesObserved = true
			break
		}
	}

	if differentResourceIndicesObserved {
		// If clusters observe different resource snapshot versions, we set RolloutStarted condition to Unknown.
		// ObservedResourceIndex and selectedResources are reset, too.
		klog.V(2).InfoS("Placement has External rollout strategy and different resource snapshot versions are observed across clusters, set RolloutStarted condition to Unknown", "placement", klog.KObj(placementObj))
		placementStatus := placementObj.GetPlacementStatus()
		placementStatus.ObservedResourceIndex = ""
		placementStatus.SelectedResources = []fleetv1beta1.ResourceIdentifier{}
		placementObj.SetConditions(metav1.Condition{
			Type:               getPlacementRolloutStartedConditionType(placementObj),
			Status:             metav1.ConditionUnknown,
			Reason:             condition.RolloutControlledByExternalControllerReason,
			Message:            "Rollout is controlled by an external controller and different resource snapshot versions are observed across clusters",
			ObservedGeneration: placementObj.GetGeneration(),
		})
		// As placement status will refresh even if the spec has not changed, we reset any unused conditions to avoid confusion.
		for i := condition.RolloutStartedCondition + 1; i < condition.TotalCondition; i++ {
			meta.RemoveStatusCondition(&placementStatus.Conditions, getPlacementConditionType(placementObj, i))
		}
		return true, nil
	}
	// all bindings have the same observed resource snapshot.
	if observedResourceIndex == "" {
		// All bindings have empty resource snapshot name, we set the rollout condition to Unknown.
		// ObservedResourceIndex and selectedResources are reset, too.
		klog.V(2).InfoS("Placement has External rollout strategy and no resource snapshot name is observed across clusters, set RolloutStarted condition to Unknown", "placement", klog.KObj(placementObj))
		placementStatus := placementObj.GetPlacementStatus()
		placementStatus.ObservedResourceIndex = ""
		placementStatus.SelectedResources = []fleetv1beta1.ResourceIdentifier{}
		placementObj.SetConditions(metav1.Condition{
			Type:               getPlacementRolloutStartedConditionType(placementObj),
			Status:             metav1.ConditionUnknown,
			Reason:             condition.RolloutControlledByExternalControllerReason,
			Message:            "Rollout is controlled by an external controller and no resource snapshot name is observed across clusters, probably rollout has not started yet",
			ObservedGeneration: placementObj.GetGeneration(),
		})
		// As placement status will refresh even if the spec has not changed, we reset any unused conditions to avoid confusion.
		for i := condition.RolloutStartedCondition + 1; i < condition.TotalCondition; i++ {
			meta.RemoveStatusCondition(&placementStatus.Conditions, getPlacementConditionType(placementObj, i))
		}

		return true, nil
	}

	// All bindings have the same observed resource snapshot.
	// We only set the ObservedResourceIndex and selectedResources, as the conditions will be set with setPlacementConditions.
	// If all clusters observe the latest resource snapshot, we do not need to go through all the resource snapshots again to collect selected resources.
	placementStatus := placementObj.GetPlacementStatus()
	if observedResourceIndex == placementStatus.ObservedResourceIndex {
		placementStatus.SelectedResources = selectedResourceIDs
	} else {
		placementStatus.ObservedResourceIndex = observedResourceIndex
		// Construct placement key for the resource collection function
		placementKey := controller.GetObjectKeyFromNamespaceName(placementObj.GetNamespace(), placementObj.GetName())
		selectedResources, err := controller.CollectResourceIdentifiersFromResourceSnapshot(ctx, r.Client, placementKey, observedResourceIndex)
		if err != nil {
			klog.ErrorS(err, "Failed to collect resource identifiers from resourceSnapshot", "placement", klog.KObj(placementObj), "resourceSnapshotIndex", observedResourceIndex)
			return false, err
		}
		placementStatus.SelectedResources = selectedResources
	}

	for i := range len(selected) {
		rolloutStartedCond := meta.FindStatusCondition(allRPS[i].Conditions, string(fleetv1beta1.PerClusterRolloutStartedConditionType))
		if !condition.IsConditionStatusTrue(rolloutStartedCond, placementObj.GetGeneration()) &&
			!condition.IsConditionStatusFalse(rolloutStartedCond, placementObj.GetGeneration()) {
			klog.V(2).InfoS("Placement has External rollout strategy and some cluster is in RolloutStarted Unknown state, set RolloutStarted condition to Unknown",
				"clusterName", allRPS[i].ClusterName, "observedResourceIndex", observedResourceIndex, "placement", klog.KObj(placementObj))
			placementObj.SetConditions(metav1.Condition{
				Type:               getPlacementRolloutStartedConditionType(placementObj),
				Status:             metav1.ConditionUnknown,
				Reason:             condition.RolloutControlledByExternalControllerReason,
				Message:            fmt.Sprintf("Rollout is controlled by an external controller and cluster %s is in RolloutStarted Unknown state", allRPS[i].ClusterName),
				ObservedGeneration: placementObj.GetGeneration(),
			})
			// As placement status will refresh even if the spec has not changed, we reset any unused conditions to avoid confusion.
			for i := condition.RolloutStartedCondition + 1; i < condition.TotalCondition; i++ {
				meta.RemoveStatusCondition(&placementStatus.Conditions, string(i.ClusterResourcePlacementConditionType()))
			}
			return true, nil
		}
	}
	return false, nil
}

func buildScheduledCondition(placementObj fleetv1beta1.PlacementObj, latestSchedulingPolicySnapshot fleetv1beta1.PolicySnapshotObj) metav1.Condition {
	scheduledCondition := latestSchedulingPolicySnapshot.GetCondition(string(fleetv1beta1.PolicySnapshotScheduled))

	if scheduledCondition == nil ||
		// defensive check and not needed for now as the policySnapshot should be immutable.
		scheduledCondition.ObservedGeneration < latestSchedulingPolicySnapshot.GetGeneration() ||
		// We have numberOfCluster annotation added on the placement and it won't change the placement generation.
		// So that we need to compare the placement observedCRPGeneration reported by the scheduler.
		latestSchedulingPolicySnapshot.GetPolicySnapshotStatus().ObservedCRPGeneration < placementObj.GetGeneration() ||
		scheduledCondition.Status == metav1.ConditionUnknown {
		return metav1.Condition{
			Status:             metav1.ConditionUnknown,
			Type:               getPlacementScheduledConditionType(placementObj),
			Reason:             condition.SchedulingUnknownReason,
			Message:            "Scheduling has not completed",
			ObservedGeneration: placementObj.GetGeneration(),
		}
	}
	return metav1.Condition{
		Status:             scheduledCondition.Status,
		Type:               getPlacementScheduledConditionType(placementObj),
		Reason:             scheduledCondition.Reason,
		Message:            scheduledCondition.Message,
		ObservedGeneration: placementObj.GetGeneration(),
	}
}

func classifyClusterDecisions(decisions []fleetv1beta1.ClusterDecision) (selected []*fleetv1beta1.ClusterDecision, unselected []*fleetv1beta1.ClusterDecision) {
	selected = make([]*fleetv1beta1.ClusterDecision, 0, len(decisions))
	unselected = make([]*fleetv1beta1.ClusterDecision, 0, len(decisions))

	for i := range decisions {
		if decisions[i].Selected {
			selected = append(selected, &decisions[i])
		} else {
			unselected = append(unselected, &decisions[i])
		}
	}
	return selected, unselected
}

func buildPerClusterPlacementStatusMap(placementObj fleetv1beta1.PlacementObj) map[string][]metav1.Condition {
	perClusterStatuses := placementObj.GetPlacementStatus().PerClusterPlacementStatuses
	m := make(map[string][]metav1.Condition, len(perClusterStatuses))
	for i := range perClusterStatuses {
		if len(perClusterStatuses[i].ClusterName) == 0 || len(perClusterStatuses[i].Conditions) == 0 {
			continue
		}
		m[perClusterStatuses[i].ClusterName] = perClusterStatuses[i].Conditions
	}
	return m
}

// isRolloutCompleted checks if the placement rollout is completed for both CRP and RP which means:
// 1. Placement Scheduled condition is true.
// 2. All expected placement conditions are true depends on what type of policy placementObj has.
func isRolloutCompleted(placementObj fleetv1beta1.PlacementObj) bool {
	scheduledConditionType := getPlacementScheduledConditionType(placementObj)
	if !condition.IsConditionStatusTrue(placementObj.GetCondition(scheduledConditionType), placementObj.GetGeneration()) {
		return false
	}

	expectedCondTypes := determineExpectedPlacementAndResourcePlacementStatusCondType(placementObj)
	for _, i := range expectedCondTypes {
		conditionType := getPlacementConditionType(placementObj, i)
		if !condition.IsConditionStatusTrue(placementObj.GetCondition(conditionType), placementObj.GetGeneration()) {
			return false
		}
	}
	return true
}

func emitPlacementStatusMetric(placementObj fleetv1beta1.PlacementObj) {
	// Check Placement Scheduled condition.
	status := "nil"
	reason := "nil"
	scheduledConditionType := getPlacementScheduledConditionType(placementObj)
	cond := placementObj.GetCondition(scheduledConditionType)
	if !condition.IsConditionStatusTrue(cond, placementObj.GetGeneration()) {
		if cond != nil && cond.ObservedGeneration == placementObj.GetGeneration() {
			status = string(cond.Status)
			reason = cond.Reason
		}
		hubmetrics.FleetPlacementStatusLastTimeStampSeconds.WithLabelValues(placementObj.GetNamespace(), placementObj.GetName(), strconv.FormatInt(placementObj.GetGeneration(), 10), scheduledConditionType, status, reason).SetToCurrentTime()
		return
	}

	// Check placement expected conditions.
	expectedCondTypes := determineExpectedPlacementAndResourcePlacementStatusCondType(placementObj)
	for _, condType := range expectedCondTypes {
		conditionType := getPlacementConditionType(placementObj, condType)
		cond = placementObj.GetCondition(conditionType)
		if !condition.IsConditionStatusTrue(cond, placementObj.GetGeneration()) {
			if cond != nil && cond.ObservedGeneration == placementObj.GetGeneration() {
				status = string(cond.Status)
				reason = cond.Reason
			}
			hubmetrics.FleetPlacementStatusLastTimeStampSeconds.WithLabelValues(placementObj.GetNamespace(), placementObj.GetName(), strconv.FormatInt(placementObj.GetGeneration(), 10), conditionType, status, reason).SetToCurrentTime()
			return
		}
	}

	// Emit the "Completed" condition metric to indicate that the placement has completed.
	// This condition is used solely for metric reporting purposes.
	hubmetrics.FleetPlacementStatusLastTimeStampSeconds.WithLabelValues(placementObj.GetNamespace(), placementObj.GetName(), strconv.FormatInt(placementObj.GetGeneration(), 10), "Completed", string(metav1.ConditionTrue), "Completed").SetToCurrentTime()
}
