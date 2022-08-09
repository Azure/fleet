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

var (
	ErrStillPendingManifest = fmt.Errorf("there are still manifest pending to be processed by the member cluster")
	ErrFailedManifest       = fmt.Errorf("there are still failed to apply manifests")
)

const (
	eventReasonResourceSelected  = "ResourceSelected"
	eventReasonClusterSelected   = "ClusterSelected"
	eventReasonResourceScheduled = "ResourceScheduled"
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

	placement, scheduleErr := r.getPlacement(name)
	if scheduleErr != nil {
		if !apierrors.IsNotFound(scheduleErr) {
			klog.ErrorS(scheduleErr, "Failed to get the cluster resource placement in hub agent", "placement", name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(scheduleErr)
	}
	placeRef := klog.KObj(placement)
	placementCopy := placement.DeepCopy()

	if placement.GetDeletionTimestamp() != nil {
		klog.V(2).InfoS("Placement is being deleted", "placement", placeRef)
		// remove all the placement annotation on the still selected resource
		released, applyErr := r.removeResourcesClaims(ctx, placement, placementCopy.Status.SelectedResources, make([]fleetv1alpha1.ResourceIdentifier, 0))
		if applyErr != nil {
			klog.ErrorS(applyErr, "failed to release claims on the no longer selected cluster scoped resources on placement deletion",
				"placement", placeRef, "total resources", len(placement.Status.SelectedResources), "released resources", released)
			_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
			return ctrl.Result{}, applyErr
		}
		klog.V(3).InfoS("Successfully released claims on no longer selected cluster scoped resources on placement deletion",
			"placement", placement.Name, "released resources", released)
		controllerutil.RemoveFinalizer(placement, utils.PlacementFinalizer)
		return ctrl.Result{}, r.Client.Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
	}

	if !controllerutil.ContainsFinalizer(placement, utils.PlacementFinalizer) {
		controllerutil.AddFinalizer(placement, utils.PlacementFinalizer)
		updateErr := r.Client.Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		if updateErr != nil {
			klog.ErrorS(updateErr, "Failed to add the cluster resource placement finalizer", "placement", name)
			return ctrl.Result{}, updateErr
		}
	}

	// TODO: move this to webhook
	if err := validator.ValidateClusterResourcePlacement(placement); err != nil {
		invalidSpec := "the spec is invalid"
		klog.ErrorS(err, invalidSpec, "placement", placeRef)
		r.Recorder.Event(placement, corev1.EventTypeWarning, invalidSpec, err.Error())
		return ctrl.Result{}, nil
	}

	klog.V(2).InfoS("Start to reconcile a ClusterResourcePlacement", "placement", placeRef)
	// select the new clusters and record that in the placementCopy status
	selectedClusters, scheduleErr := r.selectClusters(placementCopy)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "Failed to select the clusters", "placement", placeRef)
		updatePlacementScheduledCondition(placement, scheduleErr)
		_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	if len(selectedClusters) == 0 {
		scheduleErr = fmt.Errorf("no matching cluster")
		klog.ErrorS(scheduleErr, "Failed to select the clusters", "placement", placeRef)
		updatePlacementScheduledCondition(placement, scheduleErr)
		_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully selected clusters", "placement", placement.Name, "number of clusters", len(selectedClusters))

	// select the new resources and record the result in the placementCopy status
	manifests, scheduleErr := r.selectResources(ctx, placementCopy)
	if scheduleErr != nil {
		klog.ErrorS(scheduleErr, "failed to generate the work resource for this placement", "placement", placeRef)
		updatePlacementScheduledCondition(placement, scheduleErr)
		_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, scheduleErr
	}
	klog.V(3).InfoS("Successfully selected resources", "placement", placement.Name, "number of resources", len(manifests))

	// schedule works for each cluster by placing them in the cluster scoped namespace
	applyErr := r.createOrUpdateWorkSpec(ctx, placementCopy, manifests, selectedClusters)
	if applyErr != nil {
		klog.ErrorS(applyErr, "failed to apply work resources ", "placement", placeRef)
		updatePlacementScheduledCondition(placement, applyErr)
		_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, applyErr
	}
	klog.V(3).InfoS("Successfully applied work resources", "placement", placement.Name, "number of clusters", len(selectedClusters))

	// go through the existing selected resource list and release the claim from those no longer scheduled cluster scoped objects
	// by removing the finalizer and placement name in the annotation.
	released, applyErr := r.removeResourcesClaims(ctx, placementCopy, placement.Status.SelectedResources, placementCopy.Status.SelectedResources)
	if applyErr != nil {
		klog.ErrorS(applyErr, "failed to release claims on no longer selected cluster scoped resources", "placement", placeRef)
		updatePlacementScheduledCondition(placement, scheduleErr)
		_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, applyErr
	}
	klog.V(3).InfoS("Successfully released claims on no longer selected cluster scoped resources", "placement", placement.Name, "released resources", released)

	// go through the existing cluster list and remove work from no longer scheduled clusters.
	removed, applyErr := r.removeWorkResources(ctx, placementCopy, placement.Status.TargetClusters, placementCopy.Status.TargetClusters)
	if applyErr != nil {
		klog.ErrorS(applyErr, "failed to remove work resources from previously selected clusters", "placement", placeRef)
		updatePlacementScheduledCondition(placement, scheduleErr)
		updatePlacementScheduledCondition(placement, applyErr)
		_ = r.Client.Status().Update(ctx, placement, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, applyErr
	}
	klog.V(3).InfoS("Successfully removed work resources from previously selected clusters", "placement", placement.Name, "removed clusters", removed)

	// the schedule has succeeded so we now can use the new placement status
	updatePlacementScheduledCondition(placementCopy, nil)
	r.Recorder.Event(placement, corev1.EventTypeNormal, eventReasonResourceScheduled, "successfully scheduled all selected resources to their clusters")

	// go through all the valid works, get the failed and pending manifests
	hasPending, applyErr := r.collectAllManifestsStatus(placementCopy)
	if applyErr != nil {
		klog.ErrorS(applyErr, "failed to collect work resources status from all selected clusters", "placement", placeRef)
		updatePlacementAppliedCondition(placementCopy, applyErr)
		_ = r.Client.Status().Update(ctx, placementCopy, client.FieldOwner(utils.PlacementFieldManagerName))
		return ctrl.Result{}, applyErr
	}
	klog.V(3).InfoS("Successfully collected work resources status from all selected clusters", "placement", placement.Name, "number of clusters", len(selectedClusters))

	if !hasPending && len(placementCopy.Status.FailedResourcePlacements) == 0 {
		updatePlacementAppliedCondition(placementCopy, nil)
	} else {
		updatePlacementAppliedCondition(placementCopy, ErrFailedManifest)
	}

	return ctrl.Result{}, r.Client.Status().Update(ctx, placementCopy, client.FieldOwner(utils.PlacementFieldManagerName))
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
