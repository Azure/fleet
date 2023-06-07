/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1 "go.goms.io/fleet/apis/v1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/metrics"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

const (
	ApplyFailedReason    = "ApplyFailed"
	ApplyPendingReason   = "ApplyPending"
	ApplySucceededReason = "ApplySucceeded"
)

var (
	ErrStillPendingManifest = fmt.Errorf("there are still manifest pending to be processed by the member cluster")
	ErrFailedManifest       = fmt.Errorf("there are failed to apply manifests, please check the `failedResourcePlacements` status")
)

// Reconciler reconciles a cluster resource placement object
type Reconciler struct {
	// the informer contains the cache for all the resources we need.
	InformerManager informer.Manager

	// RestMapper is used to convert between gvk and gvr on known resources.
	RestMapper meta.RESTMapper

	// Client is used to update objects which goes to the api server directly.
	Client client.Client

	// DisabledResourceConfig contains all the api resources that we won't select.
	DisabledResourceConfig *utils.DisabledResourceConfig

	// SkippedNamespaces contains the namespaces that we should not propagate.
	SkippedNamespaces map[string]bool

	Recorder record.EventRecorder

	Scheme *runtime.Scheme
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	startTime := time.Now()
	name, ok := key.(string)
	if !ok {
		err := fmt.Errorf("get place key %+v not of type string", key)
		klog.ErrorS(err, "We have encountered a fatal error that can't be retried, requeue after a day")
		return ctrl.Result{RequeueAfter: time.Hour * 24}, nil
	}

	placementOld, err := r.getPlacement(name)
	if err != nil {
		klog.ErrorS(err, "Failed to get the cluster resource placement in hub", "placement", name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	placeRef := klog.KObj(placementOld)
	placementNew := placementOld.DeepCopy()
	// add latency log
	defer func() {
		klog.V(2).InfoS("ClusterResourcePlacement reconciliation loop ends", "placement", placeRef, "latency", time.Since(startTime).Milliseconds())
	}()

	// TODO: add finalizer logic if we need it in the future
	klog.V(2).InfoS("Start to reconcile a ClusterResourcePlacement", "placement", placeRef)
	// select the new clusters and record that in the placementNew status
	selectedClusters, scheduleErr := r.selectClusters(placementNew)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "Failed to select the clusters", "placement", placeRef)
		r.updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		// TODO: check on certain error (i.e. not cluster scoped) and do not retry
		return ctrl.Result{}, scheduleErr
	}
	if len(selectedClusters) == 0 {
		// no need to continue, we are not placing anything
		klog.V(2).InfoS("No clusters match the placement", "placement", placeRef)
		return r.removeAllWorks(ctx, placementOld)
	}

	klog.V(2).InfoS("Successfully selected clusters", "placement", placementOld.Name, "number of clusters", len(selectedClusters))

	// select the new resources and record the result in the placementNew status
	manifests, scheduleErr := r.selectResources(ctx, placementNew)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to select the resources for this placement", "placement", placeRef)
		r.updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	if len(manifests) == 0 {
		// no need to continue, we are not placing anything
		klog.V(2).InfoS("No resources match the placement", "placement", placeRef)
		return r.removeAllWorks(ctx, placementOld)
	}
	klog.V(2).InfoS("Successfully selected resources", "placement", placementOld.Name, "number of resources", len(manifests))

	// persist union of the all the selected resources and clusters between placementNew and placementOld so that we won't
	// get orphaned resource/cluster if the reconcile loops stops between work creation and the placement status persisted
	totalCluster, totalResources, scheduleErr := r.persistSelectedResourceUnion(ctx, placementOld, placementNew)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to record the  work resources ", "placement", placeRef)
		r.updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(2).InfoS("Successfully persisted the intermediate scheduling result", "placement", placementOld.Name,
		"totalClusters", totalCluster, "totalResources", totalResources)
	// pick up the newly updated schedule condition so that the last schedule time will change every time we run the reconcile loop
	meta.SetStatusCondition(&placementNew.Status.Conditions, *placementOld.GetCondition(string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled)))
	// pick up the new version so that we can update placementNew without getting it again
	placementNew.SetResourceVersion(placementOld.GetResourceVersion())

	// schedule works for each cluster by placing them in the cluster scoped namespace
	scheduleErr = r.scheduleWork(ctx, placementNew, manifests)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to apply work resources ", "placement", placeRef)
		r.updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(2).InfoS("Successfully scheduled work resources", "placement", placementOld.Name, "number of clusters", len(selectedClusters))

	// go through the existing cluster list and remove work from no longer scheduled clusters.
	removed, scheduleErr := r.removeStaleWorks(ctx, placementNew.GetName(), placementOld.Status.TargetClusters, placementNew.Status.TargetClusters)
	if scheduleErr != nil {
		//  if we fail here, the newly selected cluster's work are not removed if they are not picked by the next reconcile loop
		//  as they are not recorded in the old placement status.
		// TODO: add them to the old placement selected clusters since the work has been created although the update can still fail
		klog.ErrorS(scheduleErr, "failed to remove work resources from previously selected clusters", "placement", placeRef)
		r.updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(2).InfoS("Successfully removed work resources from previously selected clusters", "placement", placementOld.Name, "removed clusters", removed)

	// the schedule has succeeded, so we now can use the placementNew status that contains all the newly selected cluster and resources
	r.updatePlacementScheduledCondition(placementNew, nil)

	// go through all the valid works, get the failed and pending manifests
	hasPending, applyErr := r.collectAllManifestsStatus(placementNew)
	if applyErr != nil {
		klog.ErrorS(applyErr, "failed to collect work resources status from all selected clusters", "placement", placeRef)
		r.updatePlacementAppliedCondition(placementNew, applyErr)
		_ = r.Client.Status().Update(ctx, placementNew, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, applyErr
	}
	klog.V(2).InfoS("Successfully collected work resources status from all selected clusters",
		"placement", placementOld.Name, "number of clusters", len(selectedClusters), "hasPending", hasPending,
		"numberFailedPlacement", len(placementNew.Status.FailedResourcePlacements))

	if !hasPending && len(placementNew.Status.FailedResourcePlacements) == 0 {
		r.updatePlacementAppliedCondition(placementNew, nil)
	} else if len(placementNew.Status.FailedResourcePlacements) == 0 {
		r.updatePlacementAppliedCondition(placementNew, ErrStillPendingManifest)
	} else {
		r.updatePlacementAppliedCondition(placementNew, ErrFailedManifest)
	}

	// we keep a slow reconcile loop here as a backup.
	// Any update on the work will trigger a new reconcile immediately
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.Client.Status().Update(ctx, placementNew, client.FieldOwner(utils.PlacementFieldManagerName))
}

// removeAllWorks removes all the work objects from the previous placed clusters.
func (r *Reconciler) removeAllWorks(ctx context.Context, placement *fleetv1alpha1.ClusterResourcePlacement) (ctrl.Result, error) {
	placeRef := klog.KObj(placement)
	removed, removeErr := r.removeStaleWorks(ctx, placement.GetName(), placement.Status.TargetClusters, nil)
	if removeErr != nil {
		klog.ErrorS(removeErr, "failed to remove all the work resources from previously selected clusters", "placement", placeRef)
		return ctrl.Result{}, removeErr
	}
	klog.V(2).InfoS("Successfully removed work resources from previously selected clusters",
		"placement", placeRef, "number of removed clusters", removed)
	placement.Status.TargetClusters = nil
	placement.Status.SelectedResources = nil
	placement.Status.FailedResourcePlacements = nil
	r.updatePlacementScheduledCondition(placement, fmt.Errorf("the placement didn't select any resource or cluster"))
	return ctrl.Result{}, r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
}

// persistSelectedResourceUnion finds the union of the clusters and resource we selected between the old and new placement
func (r *Reconciler) persistSelectedResourceUnion(ctx context.Context, placementOld, placementNew *fleetv1alpha1.ClusterResourcePlacement) (int, int, error) {
	// find the union of target clusters
	clusterUnion := make(map[string]struct{})
	for _, res := range placementOld.Status.TargetClusters {
		clusterUnion[res] = struct{}{}
	}
	for _, res := range placementNew.Status.TargetClusters {
		clusterUnion[res] = struct{}{}
	}
	clusterNum := len(clusterUnion)
	placementOld.Status.TargetClusters = make([]string, clusterNum)
	i := 0
	for cluster := range clusterUnion {
		placementOld.Status.TargetClusters[i] = cluster
		i++
	}
	// find the union of selected resources
	resourceUnion := make(map[fleetv1alpha1.ResourceIdentifier]struct{})
	for _, res := range placementOld.Status.SelectedResources {
		resourceUnion[res] = struct{}{}
	}
	for _, res := range placementNew.Status.SelectedResources {
		resourceUnion[res] = struct{}{}
	}
	resourceNum := len(resourceUnion)
	placementOld.Status.SelectedResources = make([]fleetv1alpha1.ResourceIdentifier, resourceNum)
	i = 0
	for resource := range resourceUnion {
		placementOld.Status.SelectedResources[i] = resource
		i++
	}
	// Condition is a required field, so we have to put something here.
	// This also helps to keep the last schedule time up to date.
	placementOld.SetConditions(metav1.Condition{
		Status:             metav1.ConditionUnknown,
		Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
		Reason:             "Scheduling",
		Message:            "Record the intermediate status of the scheduling",
		ObservedGeneration: placementOld.Generation,
	})
	return clusterNum, resourceNum, r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
}

// getPlacement retrieves a ClusterResourcePlacement object by its name, this will hit the informer cache.
func (r *Reconciler) getPlacement(name string) (*fleetv1alpha1.ClusterResourcePlacement, error) {
	obj, err := r.InformerManager.Lister(utils.ClusterResourcePlacementGVR).Get(name)
	if err != nil {
		return nil, err
	}
	var placement fleetv1alpha1.ClusterResourcePlacement
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.DeepCopyObject().(*unstructured.Unstructured).Object, &placement)
	if err != nil {
		return nil, err
	}
	return &placement, nil
}

// updatePlacementScheduledCondition updates the placement's schedule condition according to the schedule error
func (r *Reconciler) updatePlacementScheduledCondition(placement *fleetv1alpha1.ClusterResourcePlacement, scheduleErr error) {
	placementRef := klog.KObj(placement)
	schedCond := placement.GetCondition(string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled))
	if scheduleErr == nil {
		if schedCond == nil || schedCond.Status != metav1.ConditionTrue {
			klog.V(2).InfoS("successfully scheduled all selected resources to their clusters", "placement", placementRef)
			r.Recorder.Event(placement, corev1.EventTypeNormal, "ResourceScheduled", "successfully scheduled all selected resources to their clusters")
		}
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
			Reason:             "ScheduleSucceeded",
			Message:            "Successfully scheduled resources for placement",
			ObservedGeneration: placement.Generation,
		})
	} else {
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
			Reason:             "ScheduleFailed",
			Message:            scheduleErr.Error(),
			ObservedGeneration: placement.Generation,
		})
	}
}

// updatePlacementAppliedCondition updates the placement's applied condition according to the apply error
func (r *Reconciler) updatePlacementAppliedCondition(placement *fleetv1alpha1.ClusterResourcePlacement, applyErr error) {
	placementRef := klog.KObj(placement)
	preAppliedCond := placement.GetCondition(string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied))
	if preAppliedCond != nil {
		// this pointer value will be modified by the setCondition, so we need to take a deep copy.
		preAppliedCond = preAppliedCond.DeepCopy()
	}
	switch {
	case applyErr == nil:
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:             ApplySucceededReason,
			Message:            "Successfully applied resources to member clusters",
			ObservedGeneration: placement.Generation,
		})
		if preAppliedCond == nil || preAppliedCond.Status != metav1.ConditionTrue {
			klog.V(2).InfoS("successfully applied all selected resources", "placement", placementRef)
			metrics.PlacementApplySucceedCount.WithLabelValues(placement.GetName()).Inc()
			r.Recorder.Event(placement, corev1.EventTypeNormal, "ResourceApplied", "successfully applied all selected resources")
		}
	case errors.Is(applyErr, ErrStillPendingManifest):
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:             ApplyPendingReason,
			Message:            applyErr.Error(),
			ObservedGeneration: placement.Generation,
		})
		if preAppliedCond == nil || preAppliedCond.Status == metav1.ConditionTrue {
			klog.V(2).InfoS("Some selected resources are still waiting to be applied", "placement", placementRef)
			r.Recorder.Event(placement, corev1.EventTypeWarning, "ResourceApplyPending", "Some applied resources are now waiting to be applied to the member cluster")
		}
	default:
		// this includes ErrFailedManifest and any other applyError
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:             ApplyFailedReason,
			Message:            applyErr.Error(),
			ObservedGeneration: placement.Generation,
		})
		if preAppliedCond == nil || preAppliedCond.Status != metav1.ConditionFalse {
			klog.V(2).InfoS("failed to apply some selected resources", "placement", placementRef)
			metrics.PlacementApplyFailedCount.WithLabelValues(placement.GetName()).Inc()
			r.Recorder.Event(placement, corev1.EventTypeWarning, "ResourceApplyFailed", "failed to apply some selected resources")
		}
	}
}

// handleUpdate handles the create/update clusterResourcePlacement event.
// It creates corresponding clusterPolicySnapshot and clusterResourceSnapshot if needed and updates the status based on
// clusterPolicySnapshot status and work status.
// If the error type is ErrUnexpectedBehavior, the controller will skip the reconciling.
func (r *Reconciler) handleUpdate(ctx context.Context, crp *fleetv1.ClusterResourcePlacement) (ctrl.Result, error) {
	crpKObj := klog.KObj(crp)
	policyHash, err := generatePolicyHash(crp.Spec.Policy)
	if err != nil {
		klog.ErrorS(err, "Failed to generate policy hash of crp", "clusterResourcePlacement", crpKObj)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	latestPolicySnapshot, latestPolicySnapshotIndex, err := r.lookupLatestClusterPolicySnapshot(ctx, crp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// mark the last policy snapshot as inactive if it is different from what we have now
	if latestPolicySnapshot != nil &&
		string(latestPolicySnapshot.Spec.PolicyHash) != policyHash &&
		latestPolicySnapshot.Labels[fleetv1.IsLatestSnapshotLabel] == strconv.FormatBool(true) {
		// set the latest label to false first to make sure there is only one or none active policy snapshot
		latestPolicySnapshot.Labels[fleetv1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
		if err := r.Client.Update(ctx, latestPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to set the isLatestSnapshot label to false", "clusterPolicySnapshot", klog.KObj(latestPolicySnapshot))
			return ctrl.Result{}, controller.NewAPIServerError(err)
		}
	}
	if latestPolicySnapshot == nil || string(latestPolicySnapshot.Spec.PolicyHash) != policyHash {
		// create a new policy snapshot
		latestPolicySnapshotIndex++
		latestPolicySnapshot = &fleetv1.ClusterPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf(fleetv1.PolicySnapshotNameFmt, crp.Name, latestPolicySnapshotIndex),
				Labels: map[string]string{
					fleetv1.CRPTrackingLabel:      crp.Name,
					fleetv1.IsLatestSnapshotLabel: strconv.FormatBool(true),
					fleetv1.PolicyIndexLabel:      strconv.Itoa(latestPolicySnapshotIndex),
				},
			},
			Spec: fleetv1.PolicySnapShotSpec{
				Policy:     crp.Spec.Policy,
				PolicyHash: []byte(policyHash),
			},
		}
		if err := controllerutil.SetControllerReference(crp, latestPolicySnapshot, r.Scheme); err != nil {
			klog.ErrorS(err, "Failed to create set owner reference", "clusterPolicySnapshot", klog.KObj(latestPolicySnapshot))
			// should never happen
			return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
		}
		if err := r.Client.Create(ctx, latestPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to create new clusterPolicySnapshot", "clusterPolicySnapshot", klog.KObj(latestPolicySnapshot))
			return ctrl.Result{}, controller.NewAPIServerError(err)
		}
	} else if latestPolicySnapshot.Labels[fleetv1.IsLatestSnapshotLabel] != strconv.FormatBool(true) {
		// When latestPolicySnapshot.Spec.PolicyHash == policyHash,
		// It could happen when the controller just sets the latest label to false for the old snapshot, and fails to
		// create a new policy snapshot.
		// And then the customers revert back their policy to the old one again.
		// In this case, the "latest" snapshot without isLatest label has the same policy hash as the current policy.

		latestPolicySnapshot.Labels[fleetv1.IsLatestSnapshotLabel] = strconv.FormatBool(true)
		if err := r.Client.Update(ctx, latestPolicySnapshot); err != nil {
			klog.ErrorS(err, "Failed to set the isLatestSnapshot label to true", "clusterPolicySnapshot", klog.KObj(latestPolicySnapshot))
			return ctrl.Result{}, controller.NewAPIServerError(err)
		}
	}
	// create clusterResourceSnapshot
	// update the status based on the latestPolicySnapshot status
	// update the status based on the work
	return ctrl.Result{}, nil
}

// lookupLatestClusterPolicySnapshot finds the latest snapshots and its policy index.
// There will be only one active policy snapshot if exists.
// It first checks whether there is an active policy snapshot.
// If not, it finds the one whose policyIndex label is the largest.
// The policy index will always start from 0.
// Return error when 1) cannot list the snapshots 2) there are more than one active policy snapshots 3) snapshot has the
// invalid label value.
// 2 & 3 should never happen.
func (r *Reconciler) lookupLatestClusterPolicySnapshot(ctx context.Context, crp *fleetv1.ClusterResourcePlacement) (*fleetv1.ClusterPolicySnapshot, int, error) {
	snapshotList := &fleetv1.ClusterPolicySnapShotList{}
	latestSnapshotLabelMatcher := client.MatchingLabels{
		fleetv1.CRPTrackingLabel:      crp.Name,
		fleetv1.IsLatestSnapshotLabel: strconv.FormatBool(true),
	}
	crpKObj := klog.KObj(crp)
	if err := r.Client.List(ctx, snapshotList, latestSnapshotLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list active clusterPolicySnapshots", "clusterResourcePlacement", crpKObj)
		return nil, -1, controller.NewAPIServerError(err)
	}
	if len(snapshotList.Items) == 1 {
		policyIndex, err := parsePolicyIndexFromLabel(&snapshotList.Items[0])
		if err != nil {
			return nil, -1, controller.NewUnexpectedBehaviorError(err)
		}
		return &snapshotList.Items[0], policyIndex, nil
	} else if len(snapshotList.Items) > 1 {
		// It means there are multiple active snapshots and should never happen.
		err := fmt.Errorf("there are %d active clusterPolicySnapshots owned by clusterResourcePlacement %v", len(snapshotList.Items), crp.Name)
		klog.ErrorS(err, "It should never happen", "clusterResourcePlacement", crpKObj)
		return nil, -1, controller.NewUnexpectedBehaviorError(err)
	}
	// When there are no active snapshots, find the one who has the largest policy index.
	if err := r.Client.List(ctx, snapshotList, client.MatchingLabels{fleetv1.CRPTrackingLabel: crp.Name}); err != nil {
		klog.ErrorS(err, "Failed to list all clusterPolicySnapshots", "clusterResourcePlacement", crpKObj)
		return nil, -1, controller.NewAPIServerError(err)
	}
	if len(snapshotList.Items) == 0 {
		// The policy index of the first snapshot will start from 0.
		return nil, -1, nil
	}
	index := -1           // the index of the cluster policy snapshot array
	lastPolicyIndex := -1 // the assigned policy index of the cluster policy snapshot
	for i := range snapshotList.Items {
		policyIndex, err := parsePolicyIndexFromLabel(&snapshotList.Items[i])
		if err != nil {
			return nil, -1, controller.NewUnexpectedBehaviorError(err)
		}
		if lastPolicyIndex < policyIndex {
			index = i
			lastPolicyIndex = policyIndex
		}
	}
	return &snapshotList.Items[index], lastPolicyIndex, nil
}

func parsePolicyIndexFromLabel(s *fleetv1.ClusterPolicySnapshot) (int, error) {
	indexLabel := s.Labels[fleetv1.PolicyIndexLabel]
	v, err := strconv.Atoi(indexLabel)
	if err != nil {
		klog.ErrorS(err, "Failed to parse the policy index label", "clusterPolicySnapshot", klog.KObj(s), "policyIndexLabel", indexLabel)
		// should never happen
		return -1, err
	}
	return v, nil
}

func generatePolicyHash(policy *fleetv1.PlacementPolicy) (string, error) {
	jsonBytes, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}
