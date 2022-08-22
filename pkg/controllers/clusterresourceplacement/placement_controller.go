/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/validator"
)

const (
	eventReasonResourceScheduled = "ResourceScheduled"
	eventReasonResourceApplied   = "ResourceApplied"
)

var (
	ErrStillPendingManifest = fmt.Errorf("there are still manifest pending to be processed by the member cluster")
	ErrFailedManifest       = fmt.Errorf("there are still failed to apply manifests")
)

// Reconciler reconciles a cluster resource placement object
type Reconciler struct {
	// the informer contains the cache for all the resources we need
	InformerManager utils.InformerManager

	RestMapper meta.RESTMapper

	// Client is used to update objects which goes to the api server directly
	Client client.Client

	// DisabledResourceConfig contains all the api resources that we won't select
	DisabledResourceConfig *utils.DisabledResourceConfig

	WorkPendingGracePeriod metav1.Duration
	Recorder               record.EventRecorder
}

func (r *Reconciler) Reconcile(ctx context.Context, key controller.QueueKey) (ctrl.Result, error) {
	name, ok := key.(string)
	if !ok {
		err := fmt.Errorf("get place key %+v not of type string", key)
		klog.ErrorS(err, "We have encountered a fatal error that can't be retried, requeue after a day")
		return ctrl.Result{RequeueAfter: time.Hour * 24}, nil
	}

	placementOld, err := r.getPlacement(name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get the cluster resource placementOld in hub agent", "placement", name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	placeRef := klog.KObj(placementOld)
	placementNew := placementOld.DeepCopy()

	// TODO: add finalizer logic if we need it in the future

	// TODO: move this to webhook
	if err := validator.ValidateClusterResourcePlacement(placementOld); err != nil {
		invalidSpec := "the spec is invalid"
		klog.ErrorS(err, invalidSpec, "placement", placeRef)
		r.Recorder.Event(placementOld, corev1.EventTypeWarning, invalidSpec, err.Error())
		return ctrl.Result{}, nil
	}

	klog.V(2).InfoS("Start to reconcile a ClusterResourcePlacement", "placement", placeRef)
	// select the new clusters and record that in the placementNew status
	selectedClusters, scheduleErr := r.selectClusters(placementNew)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "Failed to select the clusters", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	if len(selectedClusters) == 0 {
		// no need to continue, we are not placing anything
		klog.V(2).InfoS("No clusters match the placement", "placement", placeRef)
		return r.removeAllWorks(ctx, placementOld)
	}

	klog.V(3).InfoS("Successfully selected clusters", "placement", placementOld.Name, "number of clusters", len(selectedClusters))

	// select the new resources and record the result in the placementNew status
	manifests, scheduleErr := r.selectResources(ctx, placementNew)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to generate the work resource for this placementOld", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	if len(manifests) == 0 {
		// no need to continue, we are not placing anything
		klog.V(2).InfoS("No resources match the placement", "placement", placeRef)
		return r.removeAllWorks(ctx, placementOld)
	}
	klog.V(3).InfoS("Successfully selected resources", "placement", placementOld.Name, "number of resources", len(manifests))

	// persist union of the all the selected resources and clusters between placementNew and placementOld so that we won't
	// get orphaned resource/cluster if the reconcile loops stops between work creation and the placement status persisted
	totalCluster, totalResources, scheduleErr := r.persistSelectedResourceUnion(ctx, placementOld, placementNew)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to record the  work resources ", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully persisted the intermediate scheduling result", "placement", placementOld.Name,
		"totalClusters", totalCluster, "totalResources", totalResources)
	// pick up the new version so placementNew can continue to update
	placementNew.SetResourceVersion(placementOld.GetResourceVersion())

	// schedule works for each cluster by placing them in the cluster scoped namespace
	scheduleErr = r.scheduleWork(ctx, placementNew, manifests)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to apply work resources ", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully scheduled work resources", "placement", placementOld.Name, "number of clusters", len(selectedClusters))

	// go through the existing cluster list and remove work from no longer scheduled clusters.
	removed, scheduleErr := r.removeStaleWorks(ctx, placementNew.GetName(), placementOld.Status.TargetClusters, placementNew.Status.TargetClusters)
	if scheduleErr != nil {
		//  if we fail here, the newly selected cluster's work are not removed if they are not picked by the next reconcile loop
		//  as they are not recorded in the old placement status.
		// TODO: add them to the old placement selected clusters since the work has been created although the update can still fail
		klog.ErrorS(scheduleErr, "failed to remove work resources from previously selected clusters", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully removed work resources from previously selected clusters", "placement", placementOld.Name, "removed clusters", removed)

	// the schedule has succeeded, so we now can use the placementNew status that contains all the newly selected cluster and resources
	updatePlacementScheduledCondition(placementNew, nil)
	r.Recorder.Event(placementNew, corev1.EventTypeNormal, eventReasonResourceScheduled, "successfully scheduled all selected resources to their clusters")

	// go through all the valid works, get the failed and pending manifests
	hasPending, applyErr := r.collectAllManifestsStatus(placementNew)
	if applyErr != nil {
		klog.ErrorS(applyErr, "failed to collect work resources status from all selected clusters", "placement", placeRef)
		updatePlacementAppliedCondition(placementNew, applyErr)
		_ = r.Client.Status().Update(ctx, placementNew, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, applyErr
	}
	klog.V(3).InfoS("Successfully collected work resources status from all selected clusters", "placement", placementOld.Name, "number of clusters", len(selectedClusters))

	if !hasPending && len(placementNew.Status.FailedResourcePlacements) == 0 {
		updatePlacementAppliedCondition(placementNew, nil)
		r.Recorder.Event(placementNew, corev1.EventTypeNormal, eventReasonResourceApplied, "successfully applied all selected resources")
	} else {
		updatePlacementAppliedCondition(placementNew, ErrFailedManifest)
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
	klog.V(3).InfoS("Successfully removed work resources from previously selected clusters",
		"placement", placeRef, "number of removed clusters", removed)
	placement.Status.TargetClusters = nil
	placement.Status.SelectedResources = nil
	placement.Status.FailedResourcePlacements = nil
	updatePlacementScheduledCondition(placement, fmt.Errorf("the placement didn't select any resource or cluster"))
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

func updatePlacementScheduledCondition(placement *fleetv1alpha1.ClusterResourcePlacement, scheduleErr error) {
	if scheduleErr == nil {
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

func updatePlacementAppliedCondition(placement *fleetv1alpha1.ClusterResourcePlacement, applyErr error) {
	if applyErr == nil {
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:             "applySucceeded",
			Message:            "Successfully applied resources to member clusters",
			ObservedGeneration: placement.Generation,
		})
	} else {
		placement.SetConditions(metav1.Condition{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:             "applyFailed",
			Message:            applyErr.Error(),
			ObservedGeneration: placement.Generation,
		})
	}
}
