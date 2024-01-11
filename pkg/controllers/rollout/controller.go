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
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/validator"
)

// Reconciler recomputes the cluster resource binding.
type Reconciler struct {
	client.Client
	UncachedReader client.Reader
	recorder       record.EventRecorder
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
	if err := r.UncachedReader.List(ctx, bindingList, crpLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the bindings associated with the clusterResourcePlacement",
			"clusterResourcePlacement", crpName)
		return ctrl.Result{}, controller.NewAPIServerError(false, err)
	}
	// take a deep copy of the bindings so that we can safely modify them
	allBindings := make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindingList.Items))
	for _, binding := range bindingList.Items {
		allBindings = append(allBindings, binding.DeepCopy())
	}

	// handle the case that a cluster was unselected by the scheduler and then selected again but the unselected binding is not completely deleted yet
	wait, err := waitForResourcesToCleanUp(allBindings, &crp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if wait {
		// wait for the deletion to finish
		klog.V(2).InfoS("Found multiple bindings pointing to the same cluster, wait for the deletion to finish", "clusterResourcePlacement", crpName)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// find the latest resource resourceBinding
	latestResourceSnapshotName, err := r.fetchLatestResourceSnapshot(ctx, crpName)
	if err != nil {
		klog.ErrorS(err, "Failed to find the latest resource resourceBinding for the clusterResourcePlacement",
			"clusterResourcePlacement", crpName)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Found the latest resourceSnapshot for the clusterResourcePlacement", "clusterResourcePlacement", crpName, "latestResourceSnapshotName", latestResourceSnapshotName)

	// fill out all the default values for CRP just in case the mutation webhook is not enabled.
	fleetv1beta1.SetDefaultsClusterResourcePlacement(&crp)
	// validate the clusterResourcePlacement just in case the validation webhook is not enabled
	if err = validator.ValidateClusterResourcePlacement(&crp); err != nil {
		klog.ErrorS(err, "Encountered an invalid clusterResourcePlacement", "clusterResourcePlacement", crpName)
		return ctrl.Result{}, controller.NewUnexpectedBehaviorError(err)
	}

	// pick the bindings to be updated according to the rollout plan
	toBeUpdatedBindings, needRoll := pickBindingsToRoll(allBindings, latestResourceSnapshotName, &crp)
	if !needRoll {
		klog.V(2).InfoS("No bindings are out of date, stop rolling", "clusterResourcePlacement", crpName)
		return ctrl.Result{}, nil
	}
	klog.V(2).InfoS("Picked the bindings to be updated", "clusterResourcePlacement", crpName, "numberOfBindings", len(toBeUpdatedBindings))

	// Update all the bindings in parallel according to the rollout plan.
	// We need to requeue the request regardless if the binding updates succeed or not
	// to avoid the case that the rollout process stalling because the time based binding readiness does not trigger any event.
	// We wait for 1/5 of the UnavailablePeriodSeconds so we can catch the next ready one early.
	// TODO: only wait the time we need to wait for the first applied but not ready binding to be ready
	return ctrl.Result{RequeueAfter: time.Duration(*crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds) * time.Second / 5},
		r.updateBindings(ctx, latestResourceSnapshotName, toBeUpdatedBindings)
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
		return "", controller.NewExpectedBehaviorError(fmt.Errorf("crp `%s` has no latest resourceSnapshot", crpName))
	}
	klog.V(2).InfoS("Find the latest associated resource resourceBinding", "clusterResourcePlacement", crpName,
		"latestResourceSnapshotName", latestResourceSnapshotName)
	return latestResourceSnapshotName, nil
}

// waitForResourcesToCleanUp checks if there are any cluster that has a binding that is both being deleted and another one that needs rollout.
// We currently just wait for those cluster to be cleanup so that we can have a clean slate to start compute the rollout plan.
// TODO (rzhang): group all bindings pointing to the same cluster together when we calculate the rollout plan so that we can avoid this.
func waitForResourcesToCleanUp(allBindings []*fleetv1beta1.ClusterResourceBinding, crp *fleetv1beta1.ClusterResourcePlacement) (bool, error) {
	crpObj := klog.KObj(crp)
	deletingBinding := make(map[string]bool)
	bindingMap := make(map[string]*fleetv1beta1.ClusterResourceBinding)
	// separate deleting bindings from the rest of the bindings
	for _, binding := range allBindings {
		if !binding.DeletionTimestamp.IsZero() {
			deletingBinding[binding.Spec.TargetCluster] = true
			klog.V(2).InfoS("Found a binding that is being deleted", "clusterResourcePlacement", crpObj, "binding", klog.KObj(binding))
		} else {
			if _, exist := bindingMap[binding.Spec.TargetCluster]; !exist {
				bindingMap[binding.Spec.TargetCluster] = binding
			} else {
				return false, controller.NewUnexpectedBehaviorError(fmt.Errorf("the same cluster `%s` has bindings `%s` and `%s` pointing to it",
					binding.Spec.TargetCluster, bindingMap[binding.Spec.TargetCluster].Name, binding.Name))
			}
		}
	}
	// check if there are any cluster that has a binding that is both being deleted and scheduled
	for cluster, binding := range bindingMap {
		// check if there is a  deleting binding on the same cluster
		if deletingBinding[cluster] {
			klog.V(2).InfoS("Find a binding assigned to a cluster with another deleting binding", "clusterResourcePlacement", crpObj, "binding", binding)
			if binding.Spec.State == fleetv1beta1.BindingStateBound {
				// the rollout controller won't move a binding from scheduled state to bound if there is a deleting binding on the same cluster.
				return false, controller.NewUnexpectedBehaviorError(fmt.Errorf(
					"find a cluster `%s` that has a bound binding `%s` and a deleting binding point to it", binding.Spec.TargetCluster, binding.Name))
			}
			if binding.Spec.State == fleetv1beta1.BindingStateUnscheduled {
				// this is a very rare case that the resource was in the middle of being removed from a member cluster after it is unselected.
				// then the cluster get selected and unselected in two scheduling before the member agent is able to clean up all the resources.
				if binding.GetAnnotations()[fleetv1beta1.PreviousBindingStateAnnotation] == string(fleetv1beta1.BindingStateBound) {
					// its previous state can not be bound as rollout won't roll a binding with a deleting binding pointing to the same cluster.
					return false, controller.NewUnexpectedBehaviorError(fmt.Errorf(
						"find a cluster `%s` that has a unscheduled binding `%+s` with previous state is `bound` and a deleting binding point to it", binding.Spec.TargetCluster, binding.Name))
				}
				return true, nil
			}
			// there is a scheduled binding on the same cluster, we need to wait for the deletion to finish
			return true, nil
		}
	}
	return false, nil
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

	// Those are the bindings that are a sub-set of the candidates to be updated to latest resources but also are failed to apply.
	// We can safely update those bindings to latest resources even if we can't update the rest of the bindings when we don't meet the
	// minimum AvailableNumber of copies as we won't reduce the total unavailable number of bindings.
	applyFailedUpdateCandidates := make([]*fleetv1beta1.ClusterResourceBinding, 0)

	// calculate the cutoff time for a binding to be applied before so that it can be considered ready
	readyTimeCutOff := time.Now().Add(-time.Duration(*crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds) * time.Second)

	// classify the bindings into different categories
	// TODO: calculate the time we need to wait for the first applied but not ready binding to be ready.
	for idx := range allBindings {
		binding := allBindings[idx]
		switch binding.Spec.State {
		case fleetv1beta1.BindingStateUnscheduled:
			appliedCondition := binding.GetCondition(string(fleetv1beta1.ResourceBindingApplied))
			if appliedCondition != nil && appliedCondition.Status == metav1.ConditionFalse && appliedCondition.ObservedGeneration == binding.Generation {
				klog.V(3).InfoS("Found an failed to apply unscheduled binding", "clusterResourcePlacement", klog.KObj(crp), "binding", klog.KObj(binding))
			} else {
				canBeReadyBindings = append(canBeReadyBindings, binding)
			}
			_, bindingReady := isBindingReady(binding, readyTimeCutOff)
			if bindingReady {
				klog.V(3).InfoS("Found a ready unscheduled binding", "clusterResourcePlacement", klog.KObj(crp), "binding", klog.KObj(binding))
				readyBindings = append(readyBindings, binding)
			}
			if binding.DeletionTimestamp.IsZero() {
				// it's not been deleted yet, so it is a removal candidate
				klog.V(3).InfoS("Found a not yet deleted unscheduled binding", "clusterResourcePlacement", klog.KObj(crp), "binding", klog.KObj(binding))
				removeCandidates = append(removeCandidates, binding)
			} else if bindingReady {
				// it is being deleted, it can be removed from the cluster at any time, so it can be unavailable at any time
				canBeUnavailableBindings = append(canBeUnavailableBindings, binding)
			}

		case fleetv1beta1.BindingStateScheduled:
			// the scheduler has picked a cluster for this binding
			schedulerTargetedBinds = append(schedulerTargetedBinds, binding)
			// this binding has not been bound yet, so it is an update candidate
			boundingCandidates = append(boundingCandidates, binding)

		case fleetv1beta1.BindingStateBound:
			bindingFailed := false
			schedulerTargetedBinds = append(schedulerTargetedBinds, binding)
			if _, bindingReady := isBindingReady(binding, readyTimeCutOff); bindingReady {
				klog.V(3).InfoS("Found a ready bound binding", "clusterResourcePlacement", klog.KObj(crp), "binding", klog.KObj(binding))
				readyBindings = append(readyBindings, binding)
			}
			appliedCondition := binding.GetCondition(string(fleetv1beta1.ResourceBindingApplied))
			if appliedCondition != nil && appliedCondition.Status == metav1.ConditionFalse && appliedCondition.ObservedGeneration == binding.Generation {
				klog.V(3).InfoS("Found a failed to apply bound binding", "clusterResourcePlacement", klog.KObj(crp), "binding", klog.KObj(binding))
				bindingFailed = true
			} else {
				canBeReadyBindings = append(canBeReadyBindings, binding)
			}
			// The binding needs update if it's not pointing to the latest resource resourceBinding
			if binding.Spec.ResourceSnapshotName != latestResourceSnapshotName {
				updateCandidates = append(updateCandidates, binding)
				if bindingFailed {
					// the binding has been applied but failed to apply, we can safely update it to latest resources without affecting max unavailable count
					applyFailedUpdateCandidates = append(applyFailedUpdateCandidates, binding)
				}
			}
		}
	}

	// calculate the target number of bindings
	targetNumber := 0

	// note that if the policy will be overwritten if it is nil in this controller.
	switch {
	case crp.Spec.Policy.PlacementType == fleetv1beta1.PickAllPlacementType:
		// we use the scheduler picked bindings as the target number since there is no target in the CRP
		targetNumber = len(schedulerTargetedBinds)
	case crp.Spec.Policy.PlacementType == fleetv1beta1.PickFixedPlacementType:
		// we use the length of the given cluster names are targets
		targetNumber = len(crp.Spec.Policy.ClusterNames)
	case crp.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType:
		// we use the given number as the target
		targetNumber = int(*crp.Spec.Policy.NumberOfClusters)
	default:
		// should never happen
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("unknown placement type")),
			"Encountered an invalid placementType", "clusterResourcePlacement", klog.KObj(crp))
		targetNumber = 0
	}
	klog.V(2).InfoS("Calculated the targetNumber", "clusterResourcePlacement", klog.KObj(crp),
		"targetNumber", targetNumber, "readyBindingNumber", len(readyBindings), "canBeUnavailableBindingNumber", len(canBeUnavailableBindings),
		"canBeReadyBindingNumber", len(canBeReadyBindings), "boundingCandidateNumber", len(boundingCandidates),
		"removeCandidateNumber", len(removeCandidates), "updateCandidateNumber", len(updateCandidates), "applyFailedUpdateCandidateNumber", len(applyFailedUpdateCandidates))

	// the list of bindings that are to be updated by this rolling phase
	toBeUpdatedBinding := make([]*fleetv1beta1.ClusterResourceBinding, 0)
	if len(removeCandidates)+len(updateCandidates)+len(boundingCandidates) == 0 {
		return toBeUpdatedBinding, false
	}

	// calculate the max number of bindings that can be unavailable according to user specified maxUnavailable
	maxUnavailableNumber, _ := intstr.GetScaledValueFromIntOrPercent(crp.Spec.Strategy.RollingUpdate.MaxUnavailable, targetNumber, true)
	minAvailableNumber := targetNumber - maxUnavailableNumber
	// This is the lower bound of the number of bindings that can be available during the rolling update
	// Since we can't predict the number of bindings that can be unavailable after they are applied, we don't take them into account
	lowerBoundAvailableNumber := len(readyBindings) - len(canBeUnavailableBindings)
	maxNumberToRemove := lowerBoundAvailableNumber - minAvailableNumber
	klog.V(2).InfoS("Calculated the max number of bindings to remove", "clusterResourcePlacement", klog.KObj(crp),
		"maxUnavailableNumber", maxUnavailableNumber, "minAvailableNumber", minAvailableNumber,
		"lowerBoundAvailableBindings", lowerBoundAvailableNumber, "maxNumberOfBindingsToRemove", maxNumberToRemove)

	// we can still update the bindings that are failed to apply already regardless of the maxNumberToRemove
	for i := 0; i < len(applyFailedUpdateCandidates); i++ {
		toBeUpdatedBinding = append(toBeUpdatedBinding, applyFailedUpdateCandidates[i])
	}
	if maxNumberToRemove > 0 {
		i := 0
		// we first remove the bindings that are not selected by the scheduler anymore
		for ; i < maxNumberToRemove && i < len(removeCandidates); i++ {
			toBeUpdatedBinding = append(toBeUpdatedBinding, removeCandidates[i])
		}
		// we then update the bound bindings to the latest resource resourceBinding which will lead them to be unavailable for a short period of time
		j := 0
		for ; i < maxNumberToRemove && j < len(updateCandidates); i++ {
			toBeUpdatedBinding = append(toBeUpdatedBinding, updateCandidates[j])
			j++
		}
	}

	// calculate the max number of bindings that can be added according to user specified MaxSurge
	maxSurgeNumber, _ := intstr.GetScaledValueFromIntOrPercent(crp.Spec.Strategy.RollingUpdate.MaxSurge, targetNumber, true)
	maxReadyNumber := targetNumber + maxSurgeNumber
	// This is the upper bound of the number of bindings that can be ready during the rolling update
	// We count anything that still has work object on the hub cluster as can be ready since the member agent may have connection issue with the hub cluster
	upperBoundReadyNumber := len(canBeReadyBindings)
	maxNumberToAdd := maxReadyNumber - upperBoundReadyNumber

	klog.V(2).InfoS("Calculated the max number of bindings to add", "clusterResourcePlacement", klog.KObj(crp),
		"maxSurgeNumber", maxSurgeNumber, "maxReadyNumber", maxReadyNumber, "upperBoundReadyBindings",
		upperBoundReadyNumber, "maxNumberOfBindingsToAdd", maxNumberToAdd)
	for i := 0; i < maxNumberToAdd && i < len(boundingCandidates); i++ {
		toBeUpdatedBinding = append(toBeUpdatedBinding, boundingCandidates[i])
	}

	return toBeUpdatedBinding, true
}

// isBindingReady checks if a binding is considered ready.
// A binding is considered ready if the binding's current spec has been applied before the ready cutoff time.
func isBindingReady(binding *fleetv1beta1.ClusterResourceBinding, readyTimeCutOff time.Time) (time.Duration, bool) {
	// find the latest applied condition that has the same generation as the binding
	appliedCondition := binding.GetCondition(string(fleetv1beta1.ResourceBindingApplied))
	if condition.IsConditionStatusTrue(appliedCondition, binding.GetGeneration()) {
		waitTime := appliedCondition.LastTransitionTime.Time.Sub(readyTimeCutOff)
		if waitTime < 0 {
			return 0, true
		}
		// return the time we need to wait for it to be ready in this case
		return waitTime, false
	}
	// we don't know when the current spec is applied yet, return a negative wait time
	return -1, false
}

// updateBindings updates the bindings according to its state.
func (r *Reconciler) updateBindings(ctx context.Context, latestResourceSnapshotName string, toBeUpgradedBinding []*fleetv1beta1.ClusterResourceBinding) error {
	// issue all the update requests in parallel
	errs, cctx := errgroup.WithContext(ctx)
	// handle the bindings depends on its state
	for i := 0; i < len(toBeUpgradedBinding); i++ {
		binding := toBeUpgradedBinding[i]
		bindObj := klog.KObj(binding)
		switch binding.Spec.State {
		// The only thing we can do on a bound binding is to update its resource resourceBinding
		case fleetv1beta1.BindingStateBound:
			binding.Spec.ResourceSnapshotName = latestResourceSnapshotName
			errs.Go(func() error {
				if err := r.Client.Update(cctx, binding); err != nil {
					klog.ErrorS(err, "Failed to update a binding to the latest resource", "resourceBinding", bindObj)
					return controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Updated a binding to the latest resource", "resourceBinding", bindObj, "latestResourceSnapshotName", latestResourceSnapshotName)
				return nil
			})
		// We need to bound the scheduled binding to the latest resource snapshot, scheduler doesn't set the resource snapshot name
		case fleetv1beta1.BindingStateScheduled:
			binding.Spec.State = fleetv1beta1.BindingStateBound
			binding.Spec.ResourceSnapshotName = latestResourceSnapshotName
			errs.Go(func() error {
				if err := r.Client.Update(cctx, binding); err != nil {
					klog.ErrorS(err, "Failed to mark a binding bound", "resourceBinding", bindObj)
					return controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Mark a binding bound", "resourceBinding", bindObj)
				return nil
			})
		// The only thing we can do on an unscheduled binding is to delete it
		case fleetv1beta1.BindingStateUnscheduled:
			errs.Go(func() error {
				if err := r.Client.Delete(cctx, binding); err != nil {
					if !errors.IsNotFound(err) {
						klog.ErrorS(err, "Failed to delete an unselected binding", "resourceBinding", bindObj)
						return controller.NewAPIServerError(false, err)
					}
				}
				klog.V(2).InfoS("Deleted an unselected binding", "resourceBinding", bindObj)
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
	return ctrl.NewControllerManagedBy(mgr).Named("rollout_controller").
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
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid annotation value %s : %w", fleetv1beta1.IsLatestSnapshotLabel, err)),
			"Resource resourceBinding has does not have a valid islatest annotation", "clusterResourceSnapshot", snapshotKRef)
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
