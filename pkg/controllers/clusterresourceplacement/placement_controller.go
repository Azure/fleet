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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

// TODO: add a sanity check loop to go through all CRPS and check all the cluster namespace that there are no orphaned work

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

	// Handle placement deleting
	if placementOld.GetDeletionTimestamp() != nil {
		return r.garbageCollect(ctx, placementOld)
	}

	// Makes sure that the finalizer is added before we do anything else
	if !controllerutil.ContainsFinalizer(placementOld, utils.PlacementFinalizer) {
		controllerutil.AddFinalizer(placementOld, utils.PlacementFinalizer)
		if err := r.Client.Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName)); err != nil {
			klog.ErrorS(err, "Failed to add the cluster resource placement finalizer", "placement", placeRef)
			return ctrl.Result{}, err
		}
	}

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
		scheduleErr = fmt.Errorf("no matching cluster")
		klog.ErrorS(scheduleErr, "Failed to select the clusters", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
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
	klog.V(3).InfoS("Successfully selected resources", "placement", placementOld.Name, "number of resources", len(manifests))

	// schedule works for each cluster by placing them in the cluster scoped namespace
	scheduleErr = r.createOrUpdateWorkSpec(ctx, placementNew, manifests, selectedClusters)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to apply work resources ", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully applied work resources", "placement", placementOld.Name, "number of clusters", len(selectedClusters))

	// go through the existing selected resource list and release the claim from those no longer scheduled cluster scoped objects
	// by removing the finalizer and placementOld name in the annotation.
	released, scheduleErr := r.removeResourcesClaims(ctx, placementNew, placementOld.Status.SelectedResources, placementNew.Status.SelectedResources)
	if scheduleErr != nil {
		//  if we fail here, the newly selected resources' claim may stick to them if they are not picked by the next reconcile loop
		//  as they are not recorded in the old placement status.
		// TODO: add them to the old placement selected resources since the work has been created although the update can still fail
		klog.ErrorS(scheduleErr, "failed to release claims on no longer selected cluster scoped resources", "placement", placeRef)
		updatePlacementScheduledCondition(placementOld, scheduleErr)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully released claims on no longer selected cluster scoped resources", "placement", placementOld.Name, "released resources", released)

	// go through the existing cluster list and remove work from no longer scheduled clusters.
	removed, scheduleErr := r.removeWorkResources(ctx, placementNew, placementOld.Status.TargetClusters, placementNew.Status.TargetClusters)
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

	// we stop reconcile here if the update succeeds as any update on the work will trigger a new reconcile
	return ctrl.Result{}, r.Client.Status().Update(ctx, placementNew, client.FieldOwner(utils.PlacementFieldManagerName))
}

// garbageCollect makes sure that we garbage-collects all the side effects of the placement resources before it's deleted
func (r *Reconciler) garbageCollect(ctx context.Context, placementOld *fleetv1alpha1.ClusterResourcePlacement) (ctrl.Result, error) {
	placeRef := klog.KObj(placementOld)
	klog.V(2).InfoS("Placement is being deleted", "placement", placeRef)
	// remove all the placement annotation on the still selected resource
	released, err := r.removeResourcesClaims(ctx, placementOld, placementOld.Status.SelectedResources, make([]fleetv1alpha1.ResourceIdentifier, 0))
	if err != nil {
		klog.ErrorS(err, "failed to release claims on the no longer selected cluster scoped resources on placementOld deletion",
			"placement", placeRef, "total resources", len(placementOld.Status.SelectedResources), "number of released resources", released)
		_ = r.Client.Status().Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, err
	}
	klog.V(3).InfoS("Successfully released claims on no longer selected cluster scoped resources on placement deletion",
		"placement", placeRef, "number of released resources", released)
	controllerutil.RemoveFinalizer(placementOld, utils.PlacementFinalizer)
	return ctrl.Result{}, r.Client.Update(ctx, placementOld, client.FieldOwner(utils.PlacementFieldManagerName))
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
			Status:  metav1.ConditionTrue,
			Type:    string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
			Reason:  "scheduleSucceeded",
			Message: "Successfully scheduled resources for placement",
		})
	} else {
		placement.SetConditions(metav1.Condition{
			Status:  metav1.ConditionFalse,
			Type:    string(fleetv1alpha1.ResourcePlacementConditionTypeScheduled),
			Reason:  "scheduleFailed",
			Message: scheduleErr.Error(),
		})
	}
}

func updatePlacementAppliedCondition(placement *fleetv1alpha1.ClusterResourcePlacement, applyErr error) {
	if applyErr == nil {
		placement.SetConditions(metav1.Condition{
			Status:  metav1.ConditionTrue,
			Type:    string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:  "applySucceeded",
			Message: "Successfully applied resources to member clusters",
		})
	} else {
		placement.SetConditions(metav1.Condition{
			Status:  metav1.ConditionFalse,
			Type:    string(fleetv1alpha1.ResourcePlacementStatusConditionTypeApplied),
			Reason:  "applyFailed",
			Message: applyErr.Error(),
		})
	}
}
