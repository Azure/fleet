/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package rollout features a controller to do rollout.
package rollout

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
)

// Reconciler recompute the cluster resource binding.
type Reconciler struct {
	client.Client
	APIReader client.Reader
	recorder  record.EventRecorder
}

// Reconcile triggers a single binding reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	crpName := req.NamespacedName.Name
	klog.V(2).InfoS("Start to rollout the bindings", "clusterResourcePlacement", crpName)

	// add latency log
	defer func() {
		klog.V(2).InfoS("Rollout reconciliation loop ends", "clusterResourcePlacement", crpName, "latency", time.Since(startTime).Milliseconds())
	}()

	// Get the cluster resource placement
	crp := fleetv1beta1.ClusterResourcePlacement{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: crpName}, &crp); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound clusterResourcePlacement", "clusterResourcePlacement", crpName)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get clusterResourcePlacement", "clusterResourcePlacement", crpName)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}
	// check that the crp is not being deleted
	if crp.DeletionTimestamp != nil {
		klog.V(2).InfoS("Ignoring clusterResourcePlacement that is being deleted", "clusterResourcePlacement", crpName)
		return ctrl.Result{}, nil
	}

	// check that it's actually rollingUpdate strategy
	// TODO: support the rollout all at once type of RolloutStrategy
	if crp.Spec.Strategy.Type != fleetv1beta1.RollingUpdateRolloutStrategyType {
		klog.V(2).InfoS("Ignoring clusterResourcePlacement with non-rolling-update strategy", "clusterResourcePlacement", crpName)
		return ctrl.Result{}, nil
	}

	// list all the bindings associated with the clusterResourcePlacement
	// we read from the API server directly to avoid the repeated reconcile loop due to cache inconsistency
	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	crpLabelMatcher := client.MatchingLabels{
		fleetv1beta1.CRPTrackingLabel: crp.Name,
	}
	if err := r.APIReader.List(ctx, bindingList, crpLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the bindings associated with the clusterResourcePlacement",
			"clusterResourcePlacement", crpName)
		return ctrl.Result{}, controller.NewAPIServerError(false, err)
	}
	// take a deep copy of the bindings so that we can safely modify them
	allBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindingList.Items))
	for _, binding := range bindingList.Items {
		allBindings = append(allBindings, binding.DeepCopy())
	}

	// find the latest resource resourceBinding
	latestResourceSnapshotName, err := r.fetchLatestResourceSnapshot(ctx, crpName)
	if err != nil {
		klog.ErrorS(err, "Failed to find the latest resource resourceBinding for the clusterResourcePlacement",
			"clusterResourcePlacement", crpName)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Found the latest resource resourceBinding for the clusterResourcePlacement", "clusterResourcePlacement", crpName, "latestResourceSnapshotName", latestResourceSnapshotName)

	//TODO: handle the case that a cluster was unselected by the scheduler and then selected again but the unselected binding is not deleted yet

	// pick the bindings to be updated
	tobeUpdatedBindings, needRoll := pickBindingsToRoll(allBindings, latestResourceSnapshotName, &crp)
	klog.V(2).InfoS("Picked the bindings to be updated", "clusterResourcePlacement", crpName, "number of bindings", len(tobeUpdatedBindings))
	if !needRoll {
		klog.V(2).InfoS("No bindings are out of date, stop rolling", "clusterResourcePlacement", crpName)
		return ctrl.Result{}, nil
	}

	// update all the bindings in parallel according to the rollout plan, requeue the request even if there are no errors
	return ctrl.Result{RequeueAfter: time.Minute}, r.updateBindings(ctx, latestResourceSnapshotName, tobeUpdatedBindings)
}

// fetchLatestResourceSnapshot lists all the latest resource resourceBinding associated with a CRP and returns the name of the master.
func (r *Reconciler) fetchLatestResourceSnapshot(ctx context.Context, crpName string) (string, error) {
	var latestResourceSnapshotName string
	latestResourceLabelMatcher := client.MatchingLabels{
		fleetv1beta1.IsLatestSnapshotLabel: "true",
		fleetv1beta1.CRPTrackingLabel:      crpName,
	}
	resourceSnapshotList := &fleetv1beta1.ClusterResourceSnapshotList{}
	if err := r.Client.List(ctx, resourceSnapshotList, latestResourceLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list the latest resource resourceBinding associated with the clusterResourcePlacement",
			"clusterResourcePlacement", crpName)
		return "", controller.NewAPIServerError(true, err)
	}
	// try to find the master resource resourceBinding
	for _, resourceSnapshot := range resourceSnapshotList.Items {
		// only master has this annotation
		if len(resourceSnapshot.Annotations[fleetv1beta1.ResourceGroupHashAnnotation]) != 0 {
			latestResourceSnapshotName = resourceSnapshot.Name
			break
		}
	}
	// no resource resourceBinding found, it's possible since we remove the label from the last one first before
	// creating a new resource resourceBinding.
	if len(latestResourceSnapshotName) == 0 {
		klog.V(2).InfoS("Cannot find the latest associated resource resourceBinding", "clusterResourcePlacement", crpName)
		return "", controller.NewExpectedBehaviorError(fmt.Errorf("cpr `%s` has no latest resourceSnapshot", crpName))
	}
	klog.V(4).InfoS("Find the latest associated resource resourceBinding", "clusterResourcePlacement", crpName,
		"latestResourceSnapshotName", latestResourceSnapshotName)
	return latestResourceSnapshotName, nil
}

// pickBindingsToRoll go through all bindings associated with a CRP and returns the bindings that are ready to be updated.
// There could be cases that no bindings are ready to be updated because of the maxSurge/maxUnavailable constraints even if there are out of sync bindings.
// Thus, it also returns a bool indicating whether there are out of sync bindings to be rolled to differentiate those two cases.
func pickBindingsToRoll(allBindings []*fleetv1beta1.ClusterResourceBinding, latestResourceSnapshotName string, crp *fleetv1beta1.ClusterResourcePlacement) ([]*fleetv1beta1.ClusterResourceBinding, bool) {
	// Those are the bindings that are chosen by the scheduler to be applied to selected clusters.
	// They include the bindings that are already applied to the clusters and the bindings that are newly selected by the scheduler.
	schedulerTargetedBinds := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// The content of those bindings that are considered to be already running on the targeted clusters.
	readyBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// Those are the bindings that have the potential to be ready during the rolling phase.
	// It includes all bindings that have been applied to the clusters and not deleted yet so that they can still be ready at any time.
	canBeReadyBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// Those are the bindings that have the potential to be unavailable during the rolling phase which
	// includes the bindings that are being deleted. It depends on work generator and member agent for the timing of the removal from the cluster.
	canBeUnavailableBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// Those are the bindings that are candidates to be updated to be bound during the rolling phase.
	boundingCandidates := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// Those are the bindings that are candidates to be removed during the rolling phase.
	removeCandidates := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// Those are the bindings that are candidates to be updated to latest resources during the rolling phase.
	updateCandidates := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// the list of bindings that are to be updated by this rolling phase
	tobeUpdatedBinding := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// calculate the cutoff time for a binding to be applied before so that it can be considered ready
	var unavailablePeriod time.Duration
	if crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds != nil {
		unavailablePeriod = time.Duration(*crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds)
	} else {
		unavailablePeriod = 60
	}
	readyTimeCutOff := time.Now().Add(-unavailablePeriod * time.Second)

	// classify the bindings into different categories
	for idx := range allBindings {
		binding := allBindings[idx]
		switch binding.Spec.State {
		case fleetv1beta1.BindingStateUnscheduled:
			canBeReadyBindings = append(canBeReadyBindings, binding)
			bindingReady := isBindingReady(binding, readyTimeCutOff)
			if bindingReady {
				readyBindings = append(readyBindings, binding)
			}
			if binding.DeletionTimestamp == nil {
				// it's not deleted yet, so it is a removal candidate
				removeCandidates = append(removeCandidates, binding)
			} else if bindingReady {
				// it can be deleted at any time, so it can be unavailable at any time
				canBeUnavailableBindings = append(canBeUnavailableBindings, binding)
			}

		case fleetv1beta1.BindingStateScheduled:
			// the scheduler has picked a cluster for this binding
			schedulerTargetedBinds = append(schedulerTargetedBinds, binding)
			// this binding has not been bound yet, so it is an update candidate
			boundingCandidates = append(boundingCandidates, binding)

		case fleetv1beta1.BindingStateBound:
			schedulerTargetedBinds = append(schedulerTargetedBinds, binding)
			canBeReadyBindings = append(canBeReadyBindings, binding)
			if isBindingReady(binding, readyTimeCutOff) {
				readyBindings = append(readyBindings, binding)
			}
			// The binding needs update if it's not pointing to the latest resource resourceBinding
			if binding.Spec.ResourceSnapshotName != latestResourceSnapshotName {
				updateCandidates = append(updateCandidates, binding)
			}
		}
	}

	// calculate the target number of bindings
	targetNumber := 0
	if crp.Spec.Policy.PlacementType == fleetv1beta1.PickAllPlacementType {
		// we use the scheduler picked bindings as the target number since there is no target in the CRP
		targetNumber = len(schedulerTargetedBinds)
	} else {
		// the CRP validation webhook should make sure this is not nil
		targetNumber = int(*crp.Spec.Policy.NumberOfClusters)
	}
	klog.V(2).InfoS("Calculated the targetNumber", "clusterResourcePlacement", klog.KObj(crp),
		"targetNumber", targetNumber, "readyBindingNumber", len(readyBindings), "canBeUnavailableBindingNumber", len(canBeUnavailableBindings),
		"canBeReadyBindingNumber", len(canBeReadyBindings), "boundingCandidateNumber", len(boundingCandidates),
		"removeCandidateNumber", len(removeCandidates), "updateCandidateNumber", len(updateCandidates))

	if len(removeCandidates)+len(updateCandidates)+len(boundingCandidates) == 0 {
		return tobeUpdatedBinding, false
	}

	// calculate the max number of bindings that can be unavailable according to user specified maxUnavailable
	maxUnavailableNumber, _ := intstr.GetScaledValueFromIntOrPercent(crp.Spec.Strategy.RollingUpdate.MaxUnavailable, targetNumber, true)
	minAvailableNumber := targetNumber - maxUnavailableNumber
	// This is the lower bound of the number of bindings that can be available during the rolling update
	// Since we can't predict the number of bindings that can be unavailable after they are applied, we don't take them into account
	lowerBoundAvailable := len(readyBindings) - len(canBeUnavailableBindings)
	numberToRemove := lowerBoundAvailable - minAvailableNumber
	klog.V(2).InfoS("Calculated the number to remove", "clusterResourcePlacement", klog.KObj(crp),
		"maxUnavailableNumber", maxUnavailableNumber, "lowerBoundAvailable", lowerBoundAvailable, "numberToRemove", numberToRemove)
	if numberToRemove > 0 {
		i := 0
		// we first remove the bindings that are not selected by the scheduler anymore
		for ; i < numberToRemove && i < len(removeCandidates); i++ {
			tobeUpdatedBinding = append(tobeUpdatedBinding, removeCandidates[i])
		}
		// we then update the bound bindings to the latest resource resourceBinding which will lead them to be unavailable for a short period of time
		j := 0
		for ; i < numberToRemove && j < len(updateCandidates); i++ {
			tobeUpdatedBinding = append(tobeUpdatedBinding, updateCandidates[j])
			j++
		}
	}

	// calculate the max number of bindings that can be added according to user specified MaxSurge
	maxSurgeNumber, _ := intstr.GetScaledValueFromIntOrPercent(crp.Spec.Strategy.RollingUpdate.MaxSurge, targetNumber, true)
	maxReadyNumber := targetNumber + maxSurgeNumber
	// This is the upper bound of the number of bindings that can be ready during the rolling update
	// We count anything that still has work object on the hub cluster as can be ready since the member agent may have connection issue with the hub cluster
	upperBoundReadyNumber := len(canBeReadyBindings)
	klog.V(2).InfoS("Calculated the number to add ", "clusterResourcePlacement", klog.KObj(crp),
		"maxSurgeNumber", maxSurgeNumber, "maxReadyNumber", maxReadyNumber, "upperBoundReadyNumber", upperBoundReadyNumber)
	for i := 0; i < maxReadyNumber-upperBoundReadyNumber && i < len(boundingCandidates); i++ {
		tobeUpdatedBinding = append(tobeUpdatedBinding, boundingCandidates[i])
	}

	return tobeUpdatedBinding, true
}

// isBindingReady checks if a binding is considered ready.
// A binding is considered ready if the binding's current spec has been applied before the ready cutoff time.
func isBindingReady(binding *fleetv1beta1.ClusterResourceBinding, readyTimeCutOff time.Time) bool {
	// find the latest applied condition that has the same generation as the binding
	appliedCondition := binding.GetCondition(string(fleetv1beta1.ResourceBindingApplied))
	if appliedCondition != nil && appliedCondition.Status == metav1.ConditionTrue &&
		appliedCondition.ObservedGeneration == binding.GetGeneration() {
		return appliedCondition.LastTransitionTime.Time.Before(readyTimeCutOff)
	}
	return false
}

// updateBindings updates the bindings according to its state
func (r *Reconciler) updateBindings(ctx context.Context, latestResourceSnapshotName string, tobeUpgradedBinding []*fleetv1beta1.ClusterResourceBinding) error {
	// issue all the update requests in parallel
	errs, cctx := errgroup.WithContext(ctx)
	// handle the bindings depends on its state
	for i := 0; i < len(tobeUpgradedBinding); i++ {
		binding := tobeUpgradedBinding[i]
		switch binding.Spec.State {
		// The only thing we can do on a bound binding is to update its resource resourceBinding
		case fleetv1beta1.BindingStateBound:
			binding.Spec.ResourceSnapshotName = latestResourceSnapshotName
			binding.Status.LastResourceUpdateTime = metav1.Now()
			errs.Go(func() error {
				if err := r.Client.Update(cctx, binding); err != nil {
					klog.ErrorS(err, "Failed to update a binding to the latest resource", "resourceBinding", klog.KObj(binding))
					return controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Updated a binding to the latest resource", "resourceBinding", klog.KObj(binding), "latestResourceSnapshotName", latestResourceSnapshotName)
				return nil
			})
		// The only thing we can do on a scheduled binding is to bind it
		case fleetv1beta1.BindingStateScheduled:
			binding.Spec.State = fleetv1beta1.BindingStateBound
			errs.Go(func() error {
				if err := r.Client.Update(cctx, binding); err != nil {
					klog.ErrorS(err, "Failed to mark a binding bound", "resourceBinding", klog.KObj(binding))
					return controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Mark a binding bound", "resourceBinding", klog.KObj(binding))
				return nil
			})
		// The only thing we can do on an unscheduled binding is to delete it
		case fleetv1beta1.BindingStateUnscheduled:
			errs.Go(func() error {
				if err := r.Client.Delete(cctx, binding); err != nil {
					klog.ErrorS(err, "Failed to delete an unselected binding", "resourceBinding", klog.KObj(binding))
					return controller.NewAPIServerError(false, err)
				}
				klog.V(2).InfoS("Deleted an unselected binding", "resourceBinding", klog.KObj(binding))
				return nil
			})
		}
	}
	return errs.Wait()
}

// SetupWithManager sets up the rollout controller with the Manager.
// The rollout controller watches resource snapshots and resource bindings.
// It reconciles on the CRP when a new resource resourceBinding is created or an existing resource binding is created/updated.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("rollout-controller")
	// TODO: consider converting this to a customized controller with a queue
	return ctrl.NewControllerManagedBy(mgr).
		// we don't actually watch the CRP but the controller runtime requires us to specify a for type
		For(&fleetv1beta1.ClusterResourcePlacement{}, builder.WithPredicates(
			&predicate.Funcs{
				CreateFunc: func(createEvent event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
					return false
				},
				GenericFunc: func(genericEvent event.GenericEvent) bool {
					return false
				},
			},
		)).
		Watches(&source.Kind{Type: &fleetv1beta1.ClusterResourceSnapshot{}}, handler.Funcs{
			CreateFunc: func(e event.CreateEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a resourceSnapshot create event", "resourceSnapshot", klog.KObj(e.Object))
				handleResourceSnapshot(e.Object, q)
			},
			GenericFunc: func(e event.GenericEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a resourceSnapshot generic event", "resourceSnapshot", klog.KObj(e.Object))
				handleResourceSnapshot(e.Object, q)
			},
		}).
		Watches(&source.Kind{Type: &fleetv1beta1.ClusterResourceBinding{}}, handler.Funcs{
			CreateFunc: func(e event.CreateEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a resourceBinding create event", "resourceBinding", klog.KObj(e.Object))
				handleResourceBinding(e.Object, q)
			},
			UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a resourceBinding update event", "resourceBinding", klog.KObj(e.ObjectNew))
				handleResourceBinding(e.ObjectNew, q)
			},
			GenericFunc: func(e event.GenericEvent, q workqueue.RateLimitingInterface) {
				klog.V(2).InfoS("Handling a resourceBinding generic event", "resourceBinding", klog.KObj(e.Object))
				handleResourceBinding(e.Object, q)
			},
		}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// handleResourceSnapshot parse the resourceBinding label and annotation and enqueue the CRP name associated with the resource resourceBinding
func handleResourceSnapshot(snapshot client.Object, q workqueue.RateLimitingInterface) {
	snapshotKRef := klog.KObj(snapshot)
	// check if it is the first resource resourceBinding which is supposed to have NumberOfResourceSnapshotsAnnotation
	_, exist := snapshot.GetAnnotations()[fleetv1beta1.ResourceGroupHashAnnotation]
	if !exist {
		// we only care about when a new resource resourceBinding index is created
		klog.V(2).InfoS("Ignore the subsequent sub resource snapshots", "clusterResourceSnapshot", snapshotKRef)
		return
	}
	// check if it is the latest resource resourceBinding
	isLatest, err := strconv.ParseBool(snapshot.GetLabels()[fleetv1beta1.IsLatestSnapshotLabel])
	if err != nil {
		err := fmt.Errorf("invalid annotation value %s : %w", fleetv1beta1.IsLatestSnapshotLabel, err)
		klog.ErrorS(err, "resource resourceBinding has does not have a valid islatest annotation", "clusterResourceSnapshot", snapshotKRef)
		return
	}
	if !isLatest {
		// All newly created resource snapshots should start with the latest label to be true.
		// However, this can happen if the label is removed fast by the time this reconcile loop is triggered.
		klog.V(2).InfoS("Newly created resource resourceBinding %s is not the latest", "clusterResourceSnapshot", snapshotKRef)
		return
	}
	// get the CRP name from the label
	crp := snapshot.GetLabels()[fleetv1beta1.CRPTrackingLabel]
	if len(crp) == 0 {
		// should never happen, we might be able to alert on this error
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find CRPTrackingLabel label value")),
			"Invalid clusterResourceSnapshot", "clusterResourceSnapshot", snapshotKRef)
		return
	}
	// enqueue the CRP to the rollout controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: crp},
	})
}

// handleResourceBinding parse the binding label and enqueue the CRP name associated with the resource binding
func handleResourceBinding(binding client.Object, q workqueue.RateLimitingInterface) {
	bindingRef := klog.KObj(binding)
	// get the CRP name from the label
	crp := binding.GetLabels()[fleetv1beta1.CRPTrackingLabel]
	if len(crp) == 0 {
		// should never happen
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find CRPTrackingLabel label value")),
			"Invalid clusterResourceBinding", "clusterResourceBinding", bindingRef)
		return
	}
	// enqueue the CRP to the rollout controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: crp},
	})
}
