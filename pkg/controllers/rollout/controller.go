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

// Package rollout features a controller to do rollout.
package rollout

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	bindingutils "github.com/kubefleet-dev/kubefleet/pkg/utils/binding"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/overrider"
)

// Reconciler recomputes the cluster resource binding.
type Reconciler struct {
	client.Client
	UncachedReader client.Reader
	// the max number of concurrent reconciles per controller.
	MaxConcurrentReconciles int
	recorder                record.EventRecorder
	// the informer contains the cache for all the resources we need.
	// to check the resource scope
	InformerManager informer.Manager
}

// Reconcile triggers a single binding reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	placementKey := req.NamespacedName
	klog.V(2).InfoS("Start to rollout the bindings", "placementKey", placementKey)

	// add latency log
	defer func() {
		klog.V(2).InfoS("Rollout reconciliation loop ends", "placementKey", placementKey, "latency", time.Since(startTime).Milliseconds())
	}()

	// Get the placement object (either ClusterResourcePlacement or ResourcePlacement)
	placementObj, err := controller.FetchPlacementFromKey(ctx, r.Client, controller.GetObjectKeyFromRequest(req))
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound placement", "placementKey", placementKey)
			return runtime.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get placement", "placementKey", placementKey)
		return runtime.Result{}, controller.NewAPIServerError(true, err)
	}
	placementObjRef := klog.KObj(placementObj)

	// check that the placement is not being deleted
	if placementObj.GetDeletionTimestamp() != nil {
		klog.V(2).InfoS("Ignoring placement that is being deleted", "placement", placementObjRef)
		return runtime.Result{}, nil
	}

	// fill out all the default values for placement just in case the mutation webhook is not enabled.
	defaulter.SetPlacementDefaults(placementObj)
	placementSpec := placementObj.GetPlacementSpec()

	// check that it's actually rollingUpdate strategy
	if placementSpec.Strategy.Type != placementv1beta1.RollingUpdateRolloutStrategyType {
		klog.V(2).InfoS("Ignoring placement with non-rolling-update strategy", "placement", placementObjRef)
		return runtime.Result{}, nil
	}

	// list all the bindings associated with the placement
	// we read from the API server directly to avoid the repeated reconcile loop due to cache inconsistency
	allBindings, err := controller.ListBindingsFromKey(ctx, r.UncachedReader, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list all the bindings associated with the placement",
			"placement", placementObjRef)
		return runtime.Result{}, err
	}

	// Process apply strategy updates (if any). This runs independently of the rollout process.
	//
	// Apply strategy changes will be immediately applied to all bindings that have not been
	// marked for deletion yet. Note that even unscheduled bindings will receive this update;
	// as apply strategy changes might have an effect on its Applied and Available status, and
	// consequently on the rollout progress.
	applyStrategyUpdated, err := r.processApplyStrategyUpdates(ctx, placementObj, allBindings)
	switch {
	case err != nil:
		klog.ErrorS(err, "Failed to process apply strategy updates", "placement", placementObjRef)
		return runtime.Result{}, err
	case applyStrategyUpdated:
		// After the apply strategy is updated (a spec change), all status conditions on the
		// binding object will become stale. To simplify the workflow of
		// the rollout controller, Fleet will requeue the request now, and let the subsequent
		// reconciliation loop to handle the status condition refreshing.
		//
		// Note that work generator will skip processing bindings with stale
		// RolloutStarted conditions.
		klog.V(2).InfoS("Apply strategy has been updated; requeue the request", "placement", placementObjRef)
		return reconcile.Result{Requeue: true}, nil
	default:
		klog.V(2).InfoS("Apply strategy is up to date on all bindings; continue with the rollout process", "placement", placementObjRef)
	}

	// handle the case that a cluster was unselected by the scheduler and then selected again but the unselected binding is not completely deleted yet
	wait, err := waitForResourcesToCleanUp(allBindings, placementObj)
	if err != nil {
		return runtime.Result{}, err
	}
	if wait {
		// wait for the deletion to finish
		klog.V(2).InfoS("Found multiple bindings pointing to the same cluster, wait for the deletion to finish", "placement", placementObjRef)
		return runtime.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// find the master resourceSnapshot.
	masterResourceSnapshot, err := controller.FetchLatestMasterResourceSnapshot(ctx, r.UncachedReader, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to find the masterResourceSnapshot for the placement",
			"placement", placementObjRef)
		return runtime.Result{}, err
	}
	klog.V(2).InfoS("Found the masterResourceSnapshot for the placement", "placement", placementObjRef, "masterResourceSnapshot", klog.KObj(masterResourceSnapshot))

	// Note: there is a corner case that an override is in-between snapshots (the old one is marked as not the latest while the new one is not created yet)
	// This will result in one of the override is removed by the rollout controller so the first instance of the updated cluster can experience
	// a complete removal of the override effect following by applying the new override effect.
	// TODO: detect this situation in the FetchAllMatchingOverridesForResourceSnapshot and retry here
	matchedCRO, matchedRO, err := overrider.FetchAllMatchingOverridesForResourceSnapshot(ctx, r.Client, r.InformerManager, string(controller.GetObjectKeyFromRequest(req)), masterResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to find all matching overrides for the placement", "placement", placementObjRef)
		return runtime.Result{}, err
	}

	// pick the bindings to be updated according to the rollout plan
	// staleBoundBindings is a list of "Bound" bindings and are not selected in this round because of the rollout strategy.
	toBeUpdatedBindings, staleBoundBindings, upToDateBoundBindings, needRoll, waitTime, err := r.pickBindingsToRoll(ctx, allBindings, masterResourceSnapshot, placementObj, matchedCRO, matchedRO)
	if err != nil {
		klog.ErrorS(err, "Failed to pick the bindings to roll", "placement", placementObjRef)
		return runtime.Result{}, err
	}

	if !needRoll {
		klog.V(2).InfoS("No bindings are out of date, stop rolling", "placement", placementObjRef)
		// There is a corner case that rollout controller succeeds to update the binding spec to the latest one,
		// but fails to update the binding conditions when it reconciled it last time.
		// Here it will correct the binding status just in case this happens last time.
		return runtime.Result{}, r.checkAndUpdateStaleBindingsStatus(ctx, allBindings)
	}
	klog.V(2).InfoS("Picked the bindings to be updated",
		"placement", placementObjRef,
		"numberOfToBeUpdatedBindings", len(toBeUpdatedBindings),
		"numberOfStaleBindings", len(staleBoundBindings),
		"numberOfUpToDateBindings", len(upToDateBoundBindings))

	// StaleBindings is the list that contains bindings that need to be updated (binding to a
	// cluster, upgrading to a newer resource/override snapshot) but are blocked by
	// the rollout strategy.
	//
	// Note that Fleet does not consider unscheduled bindings as stale bindings, even if the
	// status conditions on them have become stale (the work generator will handle them as an
	// exception).
	//
	// TO-DO (chenyu1): evaluate how we could improve the flow to reduce coupling.
	//
	// Update the status first, so that if the rolling out (updateBindings func) fails in the
	// middle, the controller will recompute the list so the rollout can move forward.
	if err := r.updateStaleBindingsStatus(ctx, staleBoundBindings); err != nil {
		return runtime.Result{}, err
	}
	klog.V(2).InfoS("Successfully updated status of the stale bindings", "placement", placementObjRef, "numberOfStaleBindings", len(staleBoundBindings))

	// upToDateBoundBindings contains all the bindings that does not need to have
	// their resource/override snapshots updated, but might need to have their status updated.
	//
	// Bindings might have up to date resource/override snapshots but stale status information when
	// an apply strategy update has just been applied, or an error has occurred during the
	// previous rollout process (specifically after the spec update but before the status update).
	if err := r.refreshUpToDateBindingStatus(ctx, upToDateBoundBindings); err != nil {
		return runtime.Result{}, err
	}
	klog.V(2).InfoS("Successfully updated status of the up-to-date bindings", "placement", placementObjRef, "numberOfUpToDateBindings", len(upToDateBoundBindings))

	// Update all the bindings in parallel according to the rollout plan.
	// We need to requeue the request regardless if the binding updates succeed or not
	// to avoid the case that the rollout process stalling because the time based binding readiness does not trigger any event.
	// Wait the time we need to wait for the first applied but not ready binding to be ready
	return runtime.Result{Requeue: true, RequeueAfter: waitTime}, r.updateBindings(ctx, toBeUpdatedBindings)
}

func (r *Reconciler) checkAndUpdateStaleBindingsStatus(ctx context.Context, bindings []placementv1beta1.BindingObj) error {
	if len(bindings) == 0 {
		return nil
	}
	// issue all the update requests in parallel
	errs, cctx := errgroup.WithContext(ctx)
	for i := 0; i < len(bindings); i++ {
		binding := bindings[i]
		bindingSpec := binding.GetBindingSpec()
		if bindingSpec.State != placementv1beta1.BindingStateScheduled && bindingSpec.State != placementv1beta1.BindingStateBound {
			continue
		}

		rolloutStartedCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingRolloutStarted))
		if condition.IsConditionStatusTrue(rolloutStartedCondition, binding.GetGeneration()) {
			continue
		}
		klog.V(2).InfoS("Found a stale binding status and set rolloutStartedCondition to true", "binding", klog.KObj(binding))
		errs.Go(func() error {
			return r.updateBindingStatus(cctx, binding, true)
		})
	}
	return errs.Wait()
}

// waitForResourcesToCleanUp checks if there are any cluster that has a binding that is both being deleted and another one that needs rollout.
// We currently just wait for those cluster to be cleanup so that we can have a clean slate to start compute the rollout plan.
// TODO (rzhang): group all bindings pointing to the same cluster together when we calculate the rollout plan so that we can avoid this.
func waitForResourcesToCleanUp(allBindings []placementv1beta1.BindingObj, placementObj placementv1beta1.PlacementObj) (bool, error) {
	placementObjRef := klog.KObj(placementObj)
	deletingBinding := make(map[string]bool)
	bindingMap := make(map[string]placementv1beta1.BindingObj)
	// separate deleting bindings from the rest of the bindings
	for _, binding := range allBindings {
		bindingSpec := binding.GetBindingSpec()
		if !binding.GetDeletionTimestamp().IsZero() {
			deletingBinding[bindingSpec.TargetCluster] = true
			klog.V(2).InfoS("Found a binding that is being deleted", "placement", placementObjRef, "binding", klog.KObj(binding))
		} else {
			if _, exist := bindingMap[bindingSpec.TargetCluster]; !exist {
				bindingMap[bindingSpec.TargetCluster] = binding
			} else {
				return false, controller.NewUnexpectedBehaviorError(fmt.Errorf("the same cluster `%s` has bindings `%s` and `%s` pointing to it",
					bindingSpec.TargetCluster, bindingMap[bindingSpec.TargetCluster].GetName(), binding.GetName()))
			}
		}
	}
	// check if there are any cluster that has a binding that is both being deleted and scheduled
	for cluster, binding := range bindingMap {
		// check if there is a  deleting binding on the same cluster
		if deletingBinding[cluster] {
			klog.V(2).InfoS("Find a binding assigned to a cluster with another deleting binding", "placement", placementObjRef, "binding", binding)
			bindingSpec := binding.GetBindingSpec()
			if bindingSpec.State == placementv1beta1.BindingStateBound {
				// the rollout controller won't move a binding from scheduled state to bound if there is a deleting binding on the same cluster.
				return false, controller.NewUnexpectedBehaviorError(fmt.Errorf(
					"find a cluster `%s` that has a bound binding `%s` and a deleting binding point to it", bindingSpec.TargetCluster, binding.GetName()))
			}
			if bindingSpec.State == placementv1beta1.BindingStateUnscheduled {
				// this is a very rare case that the resource was in the middle of being removed from a member cluster after it is unselected.
				// then the cluster get selected and unselected in two scheduling before the member agent is able to clean up all the resources.
				if binding.GetAnnotations()[placementv1beta1.PreviousBindingStateAnnotation] == string(placementv1beta1.BindingStateBound) {
					// its previous state can not be bound as rollout won't roll a binding with a deleting binding pointing to the same cluster.
					return false, controller.NewUnexpectedBehaviorError(fmt.Errorf(
						"find a cluster `%s` that has a unscheduled binding `%s` with previous state is `bound` and a deleting binding point to it", bindingSpec.TargetCluster, binding.GetName()))
				}
				return true, nil
			}
			// there is a scheduled binding on the same cluster, we need to wait for the deletion to finish
			return true, nil
		}
	}
	return false, nil
}

// toBeUpdatedBinding is the stale binding which will be updated by the rollout controller based on the rollout strategy.
// If the binding is selected, it will be updated to the desired state.
// Otherwise, its status will be updated.
type toBeUpdatedBinding struct {
	currentBinding placementv1beta1.BindingObj
	desiredBinding placementv1beta1.BindingObj // only valid for scheduled or bound binding
}

func createUpdateInfo(binding placementv1beta1.BindingObj,
	masterResourceSnapshot placementv1beta1.ResourceSnapshotObj, cro []string, ro []placementv1beta1.NamespacedName) toBeUpdatedBinding {
	desiredBinding := binding.DeepCopyObject().(placementv1beta1.BindingObj)

	// Apply strategy is updated separately for all bindings.

	// Get current spec and update it
	desiredSpec := desiredBinding.GetBindingSpec()
	desiredSpec.State = placementv1beta1.BindingStateBound
	desiredSpec.ResourceSnapshotName = masterResourceSnapshot.GetName()
	// TODO: check the size of the cro and ro to not exceed the limit
	desiredSpec.ClusterResourceOverrideSnapshots = cro
	desiredSpec.ResourceOverrideSnapshots = ro

	return toBeUpdatedBinding{
		currentBinding: binding,
		desiredBinding: desiredBinding,
	}
}

// pickBindingsToRoll go through all bindings associated with a CRP and returns the bindings that are ready to be updated
// and the remaining bound/scheduled bindings whose resource spec is out of date and cannot be updated because of the rollout
// strategy.
// There could be cases that no bindings are ready to be updated because of the maxSurge/maxUnavailable constraints even
// if there are out of sync bindings.
// Thus, it also returns a bool indicating whether there are out of sync bindings to be rolled to differentiate those
// two cases.
func (r *Reconciler) pickBindingsToRoll(
	ctx context.Context,
	allBindings []placementv1beta1.BindingObj,
	masterResourceSnapshot placementv1beta1.ResourceSnapshotObj,
	placementObj placementv1beta1.PlacementObj,
	matchedCROs []*placementv1beta1.ClusterResourceOverrideSnapshot,
	matchedROs []*placementv1beta1.ResourceOverrideSnapshot,
) ([]toBeUpdatedBinding, []toBeUpdatedBinding, []toBeUpdatedBinding, bool, time.Duration, error) {
	// Those are the bindings that are chosen by the scheduler to be applied to selected clusters.
	// They include the bindings that are already applied to the clusters and the bindings that are newly selected by the scheduler.
	schedulerTargetedBinds := make([]placementv1beta1.BindingObj, 0)

	// The content of those bindings that are considered to be already running on the targeted clusters.
	readyBindings := make([]placementv1beta1.BindingObj, 0)

	// Those are the bindings that have the potential to be ready during the rolling phase.
	// It includes all bindings that have been applied to the clusters and not deleted yet so that they can still be ready at any time.
	canBeReadyBindings := make([]placementv1beta1.BindingObj, 0)

	// Those are the bindings that have the potential to be unavailable during the rolling phase which
	// includes the bindings that are being deleted. It depends on work generator and member agent for the timing of the removal from the cluster.
	canBeUnavailableBindings := make([]placementv1beta1.BindingObj, 0)

	// Those are the bindings that are candidates to be updated to be bound during the rolling phase.
	boundingCandidates := make([]toBeUpdatedBinding, 0)

	// Those are the bindings that are candidates to be removed during the rolling phase.
	removeCandidates := make([]toBeUpdatedBinding, 0)

	// Those are the bindings that are candidates to be updated to latest resources during the rolling phase.
	updateCandidates := make([]toBeUpdatedBinding, 0)

	// Those are the bindings that are a sub-set of the candidates to be updated to latest resources but also are failed to apply.
	// We can safely update those bindings to latest resources even if we can't update the rest of the bindings when we don't meet the
	// minimum AvailableNumber of copies as we won't reduce the total unavailable number of bindings.
	applyFailedUpdateCandidates := make([]toBeUpdatedBinding, 0)

	// Those are the bindings that have been bound to a cluster and have the latest
	// resource/override snapshots, but might or might not have the refresh status information.
	upToDateBoundBindings := make([]toBeUpdatedBinding, 0)

	// calculate the cutoff time for a binding to be applied before so that it can be considered ready
	placementSpec := placementObj.GetPlacementSpec()
	readyTimeCutOff := time.Now().Add(-time.Duration(*placementSpec.Strategy.RollingUpdate.UnavailablePeriodSeconds) * time.Second)

	// classify the bindings into different categories
	// Wait for the first applied but not ready binding to be ready.
	// return wait time longer if the rollout is stuck on failed apply/available bindings
	minWaitTime := time.Duration(*placementSpec.Strategy.RollingUpdate.UnavailablePeriodSeconds) * time.Second
	allReady := true
	placementKObj := klog.KObj(placementObj)
	for idx := range allBindings {
		binding := allBindings[idx]
		bindingKObj := klog.KObj(binding)
		bindingSpec := binding.GetBindingSpec()
		switch bindingSpec.State {
		case placementv1beta1.BindingStateUnscheduled:
			if bindingutils.HasBindingFailed(binding) {
				klog.V(2).InfoS("Found a failed to be ready unscheduled binding", "placement", placementKObj, "binding", bindingKObj)
			} else if !bindingutils.IsBindingDiffReported(binding) {
				canBeReadyBindings = append(canBeReadyBindings, binding)
			}
			waitTime, bindingReady := isBindingReady(binding, readyTimeCutOff)
			if bindingReady {
				klog.V(2).InfoS("Found a ready unscheduled binding", "placement", placementKObj, "binding", bindingKObj)
				readyBindings = append(readyBindings, binding)
			} else {
				allReady = false
				if waitTime >= 0 && waitTime < minWaitTime {
					minWaitTime = waitTime
				}
			}
			if binding.GetDeletionTimestamp().IsZero() {
				// it's not been deleted yet, so it is a removal candidate
				klog.V(2).InfoS("Found a not yet deleted unscheduled binding", "placement", placementKObj, "binding", bindingKObj)
				// The desired binding is nil for the removeCandidates.
				removeCandidates = append(removeCandidates, toBeUpdatedBinding{currentBinding: binding})
			} else if bindingReady {
				// it is being deleted, it can be removed from the cluster at any time, so it can be unavailable at any time
				canBeUnavailableBindings = append(canBeUnavailableBindings, binding)
			}
		case placementv1beta1.BindingStateScheduled:
			// the scheduler has picked a cluster for this binding
			schedulerTargetedBinds = append(schedulerTargetedBinds, binding)
			// this binding has not been bound yet, so it is an update candidate
			// PickFromResourceMatchedOverridesForTargetCluster always returns the ordered list of the overrides.
			cro, ro, err := overrider.PickFromResourceMatchedOverridesForTargetCluster(ctx, r.Client, bindingSpec.TargetCluster, matchedCROs, matchedROs)
			if err != nil {
				return nil, nil, nil, false, minWaitTime, err
			}
			boundingCandidates = append(boundingCandidates, createUpdateInfo(binding, masterResourceSnapshot, cro, ro))
		case placementv1beta1.BindingStateBound:
			bindingFailed := false
			schedulerTargetedBinds = append(schedulerTargetedBinds, binding)
			waitTime, bindingReady := isBindingReady(binding, readyTimeCutOff)
			if bindingReady {
				klog.V(2).InfoS("Found a ready bound binding", "placement", placementKObj, "binding", bindingKObj)
				readyBindings = append(readyBindings, binding)
			} else {
				allReady = false
				if waitTime >= 0 && waitTime < minWaitTime {
					minWaitTime = waitTime
				}
			}
			// check if the binding is failed or still on going
			if bindingutils.HasBindingFailed(binding) {
				klog.V(2).InfoS("Found a failed to be ready bound binding", "placement", placementKObj, "binding", bindingKObj)
				bindingFailed = true
			} else if !bindingutils.IsBindingDiffReported(binding) {
				canBeReadyBindings = append(canBeReadyBindings, binding)
			}

			// check to see if binding is not being deleted.
			if binding.GetDeletionTimestamp().IsZero() {
				// PickFromResourceMatchedOverridesForTargetCluster always returns the ordered list of the overrides.
				cro, ro, err := overrider.PickFromResourceMatchedOverridesForTargetCluster(ctx, r.Client, bindingSpec.TargetCluster, matchedCROs, matchedROs)
				if err != nil {
					return nil, nil, nil, false, 0, err
				}
				// The binding needs update if it's not pointing to the latest resource resourceBinding or the overrides.
				if bindingSpec.ResourceSnapshotName != masterResourceSnapshot.GetName() || !equality.Semantic.DeepEqual(bindingSpec.ClusterResourceOverrideSnapshots, cro) || !equality.Semantic.DeepEqual(bindingSpec.ResourceOverrideSnapshots, ro) {
					updateInfo := createUpdateInfo(binding, masterResourceSnapshot, cro, ro)
					if bindingFailed {
						// the binding has been applied but failed to apply, we can safely update it to latest resources without affecting max unavailable count
						applyFailedUpdateCandidates = append(applyFailedUpdateCandidates, updateInfo)
					} else {
						updateCandidates = append(updateCandidates, updateInfo)
					}
				} else {
					// The binding does not need update, but Fleet might need to refresh its status
					// information.
					upToDateBoundBindings = append(upToDateBoundBindings, toBeUpdatedBinding{currentBinding: binding})
				}
			} else if bindingReady {
				// it is being deleted, it can be removed from the cluster at any time, so it can be unavailable at any time
				canBeUnavailableBindings = append(canBeUnavailableBindings, binding)
			}
		}
	}
	if allReady {
		minWaitTime = 0
	}

	// Calculate target number
	targetNumber := r.calculateRealTarget(placementObj, schedulerTargetedBinds)
	klog.V(2).InfoS("Calculated the targetNumber", "placement", placementKObj,
		"targetNumber", targetNumber, "readyBindingNumber", len(readyBindings), "canBeUnavailableBindingNumber", len(canBeUnavailableBindings),
		"canBeReadyBindingNumber", len(canBeReadyBindings), "boundingCandidateNumber", len(boundingCandidates),
		"removeCandidateNumber", len(removeCandidates), "updateCandidateNumber", len(updateCandidates), "applyFailedUpdateCandidateNumber",
		len(applyFailedUpdateCandidates), "minWaitTime", minWaitTime)

	// the list of bindings that are to be updated by this rolling phase
	toBeUpdatedBindingList := make([]toBeUpdatedBinding, 0)
	if len(removeCandidates)+len(updateCandidates)+len(boundingCandidates)+len(applyFailedUpdateCandidates) == 0 {
		return toBeUpdatedBindingList, nil, upToDateBoundBindings, false, minWaitTime, nil
	}

	toBeUpdatedBindingList, staleUnselectedBinding := determineBindingsToUpdate(placementObj, removeCandidates, updateCandidates, boundingCandidates, applyFailedUpdateCandidates, targetNumber,
		readyBindings, canBeReadyBindings, canBeUnavailableBindings)

	return toBeUpdatedBindingList, staleUnselectedBinding, upToDateBoundBindings, true, minWaitTime, nil
}

// determineBindingsToUpdate determines which bindings to update
func determineBindingsToUpdate(
	placementObj placementv1beta1.PlacementObj,
	removeCandidates, updateCandidates, boundingCandidates, applyFailedUpdateCandidates []toBeUpdatedBinding,
	targetNumber int,
	readyBindings, canBeReadyBindings, canBeUnavailableBindings []placementv1beta1.BindingObj,
) ([]toBeUpdatedBinding, []toBeUpdatedBinding) {
	toBeUpdatedBindingList := make([]toBeUpdatedBinding, 0)
	// TODO: Fix the bug that we don't shrink to zero when there are bindings that are not ready yet.
	// calculate the max number of bindings that can be unavailable according to user specified maxUnavailable
	maxNumberToRemove := calculateMaxToRemove(placementObj, targetNumber, readyBindings, canBeUnavailableBindings)
	// we can still update the bindings that are failed to apply already regardless of the maxNumberToRemove
	toBeUpdatedBindingList = append(toBeUpdatedBindingList, applyFailedUpdateCandidates...)

	// updateCandidateUnselectedIndex stores the last index of the updateCandidate which are not selected to be updated.
	// The rolloutStarted condition of these elements from this index should be updated.
	updateCandidateUnselectedIndex := 0
	if maxNumberToRemove > 0 {
		i := 0
		// we first remove the bindings that are not selected by the scheduler anymore
		for ; i < maxNumberToRemove && i < len(removeCandidates); i++ {
			toBeUpdatedBindingList = append(toBeUpdatedBindingList, removeCandidates[i])
		}
		// we then update the bound bindings to the latest resource resourceBinding which will lead them to be unavailable for a short period of time
		for ; i < maxNumberToRemove && updateCandidateUnselectedIndex < len(updateCandidates); i++ {
			toBeUpdatedBindingList = append(toBeUpdatedBindingList, updateCandidates[updateCandidateUnselectedIndex])
			updateCandidateUnselectedIndex++
		}
	}

	// calculate the max number of bindings that can be added according to user specified MaxSurge
	maxNumberToAdd := calculateMaxToAdd(placementObj, targetNumber, canBeReadyBindings)

	// boundingCandidatesUnselectedIndex stores the last index of the boundingCandidates which are not selected to be updated.
	// The rolloutStarted condition of these elements from this index should be updated.
	boundingCandidatesUnselectedIndex := 0
	// TODO re-slice the array
	for ; boundingCandidatesUnselectedIndex < maxNumberToAdd && boundingCandidatesUnselectedIndex < len(boundingCandidates); boundingCandidatesUnselectedIndex++ {
		toBeUpdatedBindingList = append(toBeUpdatedBindingList, boundingCandidates[boundingCandidatesUnselectedIndex])
	}
	// Those are the bindings that are not up to date but not selected to be updated in this round because of the rollout constraints.
	staleUnselectedBinding := make([]toBeUpdatedBinding, 0)
	if updateCandidateUnselectedIndex < len(updateCandidates) {
		staleUnselectedBinding = append(staleUnselectedBinding, updateCandidates[updateCandidateUnselectedIndex:]...)
	}
	if boundingCandidatesUnselectedIndex < len(boundingCandidates) {
		staleUnselectedBinding = append(staleUnselectedBinding, boundingCandidates[boundingCandidatesUnselectedIndex:]...)
	}
	return toBeUpdatedBindingList, staleUnselectedBinding
}

func calculateMaxToRemove(placementObj placementv1beta1.PlacementObj, targetNumber int, readyBindings, canBeUnavailableBindings []placementv1beta1.BindingObj) int {
	placementSpec := placementObj.GetPlacementSpec()
	maxUnavailableNumber, _ := intstr.GetScaledValueFromIntOrPercent(placementSpec.Strategy.RollingUpdate.MaxUnavailable, targetNumber, true)
	minAvailableNumber := targetNumber - maxUnavailableNumber
	// This is the lower bound of the number of bindings that can be available during the rolling update
	// Since we can't predict the number of bindings that can be unavailable after they are applied, we don't take them into account
	lowerBoundAvailableNumber := len(readyBindings) - len(canBeUnavailableBindings)
	maxNumberToRemove := lowerBoundAvailableNumber - minAvailableNumber
	klog.V(2).InfoS("Calculated the max number of bindings to remove", "placement", klog.KObj(placementObj),
		"maxUnavailableNumber", maxUnavailableNumber, "minAvailableNumber", minAvailableNumber,
		"lowerBoundAvailableBindings", lowerBoundAvailableNumber, "maxNumberOfBindingsToRemove", maxNumberToRemove)
	return maxNumberToRemove
}

func calculateMaxToAdd(placementObj placementv1beta1.PlacementObj, targetNumber int, canBeReadyBindings []placementv1beta1.BindingObj) int {
	placementSpec := placementObj.GetPlacementSpec()
	maxSurgeNumber, _ := intstr.GetScaledValueFromIntOrPercent(placementSpec.Strategy.RollingUpdate.MaxSurge, targetNumber, true)
	maxReadyNumber := targetNumber + maxSurgeNumber
	// This is the upper bound of the number of bindings that can be ready during the rolling update
	// We count anything that still has work object on the hub cluster as can be ready since the member agent may have connection issue with the hub cluster
	upperBoundReadyNumber := len(canBeReadyBindings)
	maxNumberToAdd := maxReadyNumber - upperBoundReadyNumber

	klog.V(2).InfoS("Calculated the max number of bindings to add", "placement", klog.KObj(placementObj),
		"maxSurgeNumber", maxSurgeNumber, "maxReadyNumber", maxReadyNumber, "upperBoundReadyBindings",
		upperBoundReadyNumber, "maxNumberOfBindingsToAdd", maxNumberToAdd)
	return maxNumberToAdd
}

func (r *Reconciler) calculateRealTarget(placementObj placementv1beta1.PlacementObj, schedulerTargetedBinds []placementv1beta1.BindingObj) int {
	placementObjRef := klog.KObj(placementObj)
	// calculate the target number of bindings
	targetNumber := 0

	placementSpec := placementObj.GetPlacementSpec()
	// note that if the policy will be overwritten if it is nil in this controller.
	switch placementSpec.Policy.PlacementType {
	case placementv1beta1.PickAllPlacementType:
		// we use the scheduler picked bindings as the target number since there is no target in the CRP
		targetNumber = len(schedulerTargetedBinds)
	case placementv1beta1.PickFixedPlacementType:
		// we use the length of the given cluster names are targets
		targetNumber = len(placementSpec.Policy.ClusterNames)
	case placementv1beta1.PickNPlacementType:
		// we use the given number as the target
		targetNumber = int(*placementSpec.Policy.NumberOfClusters)
	default:
		// should never happen
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("unknown placement type")),
			"Encountered an invalid placementType", "placement", placementObjRef)
		targetNumber = 0
	}
	return targetNumber
}

// isBindingReady checks if a binding is considered ready.
// A binding with not trackable resources is considered ready if the binding's current spec has been available before
// the ready cutoff time.
func isBindingReady(binding placementv1beta1.BindingObj, readyTimeCutOff time.Time) (time.Duration, bool) {
	// the binding is ready if the diff report has been reported
	diffReportCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingDiffReported))
	if condition.IsConditionStatusTrue(diffReportCondition, binding.GetGeneration()) {
		// we can move to the next binding
		return 0, true
	}
	// find the latest applied condition that has the same generation as the binding
	availableCondition := binding.GetCondition(string(placementv1beta1.ResourceBindingAvailable))
	if condition.IsConditionStatusTrue(availableCondition, binding.GetGeneration()) {
		// TO-DO (chenyu1): currently it checks for both the new and the old reason
		// (as set previously by the work generator) to avoid compatibility issues.
		// the check for the old reason can be removed once the rollout completes successfully.
		if availableCondition.Reason != condition.WorkNotAvailabilityTrackableReason && availableCondition.Reason != condition.WorkNotAllManifestsTrackableReason {
			return 0, true
		}

		// For the not trackable work, the available condition should be set to true when the work has been applied.
		// So here we check the available condition transition time.
		waitTime := availableCondition.LastTransitionTime.Time.Sub(readyTimeCutOff)
		if waitTime < 0 {
			return 0, true
		}
		// return the time we need to wait for it to be ready in this case
		return waitTime, false
	}
	// we don't know when the current spec is available yet, return a negative wait time
	// since we will reconcile again after the binding status changes
	return -1, false
}

// updateBindings updates the bindings according to its state.
func (r *Reconciler) updateBindings(ctx context.Context, bindings []toBeUpdatedBinding) error {
	// issue all the update requests in parallel
	errs, cctx := errgroup.WithContext(ctx)
	// handle the bindings depends on its state
	for i := 0; i < len(bindings); i++ {
		binding := bindings[i]
		bindObj := klog.KObj(binding.currentBinding)
		switch binding.currentBinding.GetBindingSpec().State {
		// The only thing we can do on a bound binding is to update its resource resourceBinding
		case placementv1beta1.BindingStateBound:
			errs.Go(func() error {
				if err := r.Client.Update(cctx, binding.desiredBinding); err != nil {
					klog.ErrorS(err, "Failed to update a binding to the latest resource", "binding", bindObj)
					return controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Updated a binding to the latest resource", "binding", bindObj, "spec", binding.desiredBinding.GetBindingSpec())
				return r.updateBindingStatus(ctx, binding.desiredBinding, true)
			})
		// We need to bound the scheduled binding to the latest resource snapshot, scheduler doesn't set the resource snapshot name
		case placementv1beta1.BindingStateScheduled:
			errs.Go(func() error {
				if err := r.Client.Update(cctx, binding.desiredBinding); err != nil {
					klog.ErrorS(err, "Failed to mark a binding bound", "binding", bindObj)
					return controller.NewUpdateIgnoreConflictError(err)
				}
				klog.V(2).InfoS("Marked a binding bound", "binding", bindObj)
				return r.updateBindingStatus(ctx, binding.desiredBinding, true)
			})
		// The only thing we can do on an unscheduled binding is to delete it
		case placementv1beta1.BindingStateUnscheduled:
			errs.Go(func() error {
				if err := r.Client.Delete(cctx, binding.currentBinding); err != nil {
					if !errors.IsNotFound(err) {
						klog.ErrorS(err, "Failed to delete an unselected binding", "binding", bindObj)
						return controller.NewAPIServerError(false, err)
					}
				}
				klog.V(2).InfoS("Deleted an unselected binding", "binding", bindObj)
				return nil
			})
		}
	}
	return errs.Wait()
}

// SetupWithManager sets up the rollout controller with the Manager.
// The rollout controller watches resource snapshots and resource bindings.
// It reconciles on the CRP when a new resource resourceBinding is created or an existing resource binding is created/updated.
func (r *Reconciler) SetupWithManager(mgr runtime.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("rollout-controller")
	return runtime.NewControllerManagedBy(mgr).Named("rollout-controller").
		WithOptions(ctrl.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}). // set the max number of concurrent reconciles
		Watches(&placementv1beta1.ClusterResourceSnapshot{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a cluster resource snapshot create event", "resourceSnapshot", klog.KObj(e.Object))
				handleResourceSnapshot(e.Object, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a cluster resource snapshot generic event", "resourceSnapshot", klog.KObj(e.Object))
				handleResourceSnapshot(e.Object, q)
			},
		}).
		Watches(&placementv1beta1.ClusterResourceOverrideSnapshot{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a clusterResourceOverrideSnapshot create event", "clusterResourceOverrideSnapshot", klog.KObj(e.Object))
				handleClusterResourceOverrideSnapshot(e.Object, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a clusterResourceOverrideSnapshot generic event", "clusterResourceOverrideSnapshot", klog.KObj(e.Object))
				handleClusterResourceOverrideSnapshot(e.Object, q)
			},
		}).
		Watches(&placementv1beta1.ResourceOverrideSnapshot{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a resourceOverrideSnapshot create event", "resourceOverrideSnapshot", klog.KObj(e.Object))
				handleResourceOverrideSnapshot(e.Object, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a resourceOverrideSnapshot generic event", "resourceOverrideSnapshot", klog.KObj(e.Object))
				handleResourceOverrideSnapshot(e.Object, q)
			},
		}).
		Watches(&placementv1beta1.ClusterResourceOverride{}, handler.Funcs{
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				cro, ok := e.Object.(*placementv1beta1.ClusterResourceOverride)
				if !ok {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("non ClusterResourceOverride type resource: %+v", e.Object)),
						"Rollout controller received invalid ClusterResourceOverride event", "object", klog.KObj(e.Object))
					return
				}
				if cro.Spec.Placement == nil {
					return
				}
				klog.V(2).InfoS("Handling a clusterResourceOverride delete event", "clusterResourceOverride", klog.KObj(cro))
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{Name: cro.Spec.Placement.Name},
				})
			},
		}).
		Watches(&placementv1beta1.ResourceOverride{}, handler.Funcs{
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				ro, ok := e.Object.(*placementv1beta1.ResourceOverride)
				if !ok {
					klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("non ResourceOverride type resource: %+v", e.Object)),
						"Rollout controller received invalid ResourceOverride event", "object", klog.KObj(e.Object))
					return
				}
				if ro.Spec.Placement == nil {
					return
				}
				klog.V(2).InfoS("Handling a resourceOverride delete event", "resourceOverride", klog.KObj(ro))
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{Name: ro.Spec.Placement.Name},
				})
			},
		}).
		Watches(&placementv1beta1.ClusterResourceBinding{}, handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a cluster resourceBinding create event", "resourceBinding", klog.KObj(e.Object))
				enqueueResourceBinding(e.Object, q)
			},
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				handleResourceBindingUpdated(e.ObjectNew, e.ObjectOld, q)
			},
			GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a cluster resourceBinding generic event", "resourceBinding", klog.KObj(e.Object))
				enqueueResourceBinding(e.Object, q)
			},
		}).
		// Aside from resource snapshot and binding objects, the rollout
		// controller also watches placement objects (ClusterResourcePlacement and ResourcePlacement),
		// so that it can push apply strategy updates to all bindings right away.
		Watches(&placementv1beta1.ClusterResourcePlacement{}, handler.Funcs{
			// Ignore all Create, Delete, and Generic events; these do not concern the rollout controller.
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				handlePlacement(e.ObjectNew, e.ObjectOld, q)
			},
		}).
		Complete(r)
}

// handleClusterResourceOverrideSnapshot parse the clusterResourceOverrideSnapshot label and enqueue the CRP name associated
// with the clusterResourceOverrideSnapshot if set.
func handleClusterResourceOverrideSnapshot(o client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	snapshot, ok := o.(*placementv1beta1.ClusterResourceOverrideSnapshot)
	if !ok {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("non ClusterResourceOverrideSnapshot type resource: %+v", o)),
			"Rollout controller received invalid ClusterResourceOverrideSnapshot event", "object", klog.KObj(o))
		return
	}

	snapshotKRef := klog.KObj(snapshot)
	// check if it is the latest resource resourceBinding
	isLatest, err := strconv.ParseBool(snapshot.GetLabels()[placementv1beta1.IsLatestSnapshotLabel])
	if err != nil {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid label value %s : %w", placementv1beta1.IsLatestSnapshotLabel, err)),
			"Resource clusterResourceOverrideSnapshot has does not have a valid islatest label", "clusterResourceOverrideSnapshot", snapshotKRef)
		return
	}
	if !isLatest {
		// All newly created resource snapshots should start with the latest label to be true.
		// However, this can happen if the label is removed fast by the time this reconcile loop is triggered.
		klog.V(2).InfoS("Newly created resource clusterResourceOverrideSnapshot %s is not the latest", "clusterResourceOverrideSnapshot", snapshotKRef)
		return
	}
	if snapshot.Spec.OverrideSpec.Placement == nil {
		return
	}
	// enqueue the CRP to the rollout controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: snapshot.Spec.OverrideSpec.Placement.Name},
	})
}

// handleResourceOverrideSnapshot parse the resourceOverrideSnapshot label and enqueue the CRP name associated with the
// resourceOverrideSnapshot if set.
func handleResourceOverrideSnapshot(o client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	snapshot, ok := o.(*placementv1beta1.ResourceOverrideSnapshot)
	if !ok {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("non ResourceOverrideSnapshot type resource: %+v", o)),
			"Rollout controller received invalid ResourceOverrideSnapshot event", "object", klog.KObj(o))
		return
	}

	snapshotKRef := klog.KObj(snapshot)
	// check if it is the latest resource resourceBinding
	isLatest, err := strconv.ParseBool(snapshot.GetLabels()[placementv1beta1.IsLatestSnapshotLabel])
	if err != nil {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid label value %s : %w", placementv1beta1.IsLatestSnapshotLabel, err)),
			"Resource resourceOverrideSnapshot has does not have a valid islatest annotation", "resourceOverrideSnapshot", snapshotKRef)
		return
	}
	if !isLatest {
		// All newly created resource snapshots should start with the latest label to be true.
		// However, this can happen if the label is removed fast by the time this reconcile loop is triggered.
		klog.V(2).InfoS("Newly changed resource resourceOverrideSnapshot %s is not the latest", "resourceOverrideSnapshot", snapshotKRef)
		return
	}
	if snapshot.Spec.OverrideSpec.Placement == nil {
		return
	}
	// enqueue the CRP to the rollout controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: snapshot.Spec.OverrideSpec.Placement.Name},
	})
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: snapshot.GetNamespace(), Name: snapshot.Spec.OverrideSpec.Placement.Name},
	})
}

// handleResourceSnapshot parse the resourceBinding label and annotation and enqueue the CRP name associated with the resource resourceBinding
func handleResourceSnapshot(snapshot client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	snapshotKRef := klog.KObj(snapshot)
	// check if it is the first resource resourceBinding which is supposed to have NumberOfResourceSnapshotsAnnotation
	_, exist := snapshot.GetAnnotations()[placementv1beta1.ResourceGroupHashAnnotation]
	if !exist {
		// we only care about when a new resource resourceBinding index is created
		klog.V(2).InfoS("Ignore the subsequent sub resource snapshots", "clusterResourceSnapshot", snapshotKRef)
		return
	}
	// check if it is the latest resource resourceBinding
	isLatest, err := strconv.ParseBool(snapshot.GetLabels()[placementv1beta1.IsLatestSnapshotLabel])
	if err != nil {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid label value %s : %w", placementv1beta1.IsLatestSnapshotLabel, err)),
			"Resource snapshot has does not have a valid islatest annotation", "resourceSnapshot", snapshotKRef)
		return
	}
	if !isLatest {
		// All newly created resource snapshots should start with the latest label to be true.
		// However, this can happen if the label is removed fast by the time this reconcile loop is triggered.
		klog.V(2).InfoS("Newly changed resource snapshot %s is not the latest", "resourceSnapshot", snapshotKRef)
		return
	}
	// get the placement name from the label
	placementName := snapshot.GetLabels()[placementv1beta1.PlacementTrackingLabel]
	if len(placementName) == 0 {
		// should never happen, we might be able to alert on this error
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find CRPTrackingLabel label value")),
			"Invalid resource snapshot", "resourceSnapshot", snapshotKRef)
		return
	}
	// enqueue the placement to the rollout controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: placementName, Namespace: snapshot.GetNamespace()},
	})
}

// enqueueResourceBinding parse the binding label and enqueue the CRP name associated with the resource binding
func enqueueResourceBinding(binding client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	bindingRef := klog.KObj(binding)
	// get the placement name from the label
	placementName := binding.GetLabels()[placementv1beta1.PlacementTrackingLabel]
	if len(placementName) == 0 {
		// should never happen
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find CRPTrackingLabel label value")),
			"Invalid binding", "binding", bindingRef)
		return
	}
	// enqueue the CRP to the rollout controller queue
	q.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: placementName, Namespace: binding.GetNamespace()},
	})
}

// handleResourceBindingUpdated determines the action to take when a resource binding is updated.
// we only care about the Available and DiffReported condition change.
func handleResourceBindingUpdated(objectOld, objectNew client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Check if the update event is valid.
	if objectOld == nil || objectNew == nil {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("update event is nil")), "Failed to process update event")
		return
	}

	// Try to cast to BindingObj interface
	oldBinding, oldOk := objectOld.(placementv1beta1.BindingObj)
	newBinding, newOk := objectNew.(placementv1beta1.BindingObj)
	if !oldOk || !newOk {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to cast runtime objects in update event to binding objects")), "Failed to process update event")
		return
	}

	if oldBinding.GetGeneration() != newBinding.GetGeneration() {
		klog.V(2).InfoS("The binding spec have changed, need to notify rollout controller", "binding", klog.KObj(newBinding))
		enqueueResourceBinding(newBinding, q)
		return
	}

	// these are the conditions we care about
	conditionsToMonitor := []string{string(placementv1beta1.ResourceBindingDiffReported), string(placementv1beta1.ResourceBindingAvailable)}
	for _, conditionType := range conditionsToMonitor {
		oldCond := oldBinding.GetCondition(conditionType)
		newCond := newBinding.GetCondition(conditionType)
		if !condition.EqualCondition(oldCond, newCond) {
			klog.V(2).InfoS("The binding status have changed, need to notify rollout controller", "binding", klog.KObj(newBinding), "conditionType", conditionType)
			enqueueResourceBinding(newBinding, q)
			return
		}
	}
	klog.V(2).InfoS("A binding is updated but we don't need to handle it", "binding", klog.KObj(newBinding))
}

// updateStaleBindingsStatus updates the status of the stale bindings to indicate that they are blocked by the rollout strategy.
// Note: the binding state should be "Scheduled" or "Bound".
// The desired binding will be ignored.
func (r *Reconciler) updateStaleBindingsStatus(ctx context.Context, staleBindings []toBeUpdatedBinding) error {
	if len(staleBindings) == 0 {
		return nil
	}
	// issue all the update requests in parallel
	errs, cctx := errgroup.WithContext(ctx)
	for i := 0; i < len(staleBindings); i++ {
		binding := staleBindings[i]
		currentBindingSpec := binding.currentBinding.GetBindingSpec()
		if currentBindingSpec.State != placementv1beta1.BindingStateScheduled && currentBindingSpec.State != placementv1beta1.BindingStateBound {
			klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid stale binding state %s", currentBindingSpec.State)),
				"Found a stale binding with unexpected state", "binding", klog.KObj(binding.currentBinding))
			continue
		}
		errs.Go(func() error {
			return r.updateBindingStatus(cctx, binding.currentBinding, false)
		})
	}
	return errs.Wait()
}

// refreshUpToDateBindingStatus refreshes the status of all up-to-date bindings.
func (r *Reconciler) refreshUpToDateBindingStatus(ctx context.Context, upToDateBindings []toBeUpdatedBinding) error {
	if len(upToDateBindings) == 0 {
		return nil
	}
	// Issue all the requests in parallel.
	errs, cctx := errgroup.WithContext(ctx)
	for i := 0; i < len(upToDateBindings); i++ {
		binding := upToDateBindings[i]
		errs.Go(func() error {
			return r.updateBindingStatus(cctx, binding.currentBinding, true)
		})
	}
	return errs.Wait()
}

// updateBindingStatus updates the status of a BindingObj.
// This function operates purely on the interface without type conversions.
func (r *Reconciler) updateBindingStatus(ctx context.Context, binding placementv1beta1.BindingObj, rolloutStarted bool) error {
	cond := metav1.Condition{
		Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: binding.GetGeneration(),
		Reason:             condition.RolloutNotStartedYetReason,
		Message:            "The resources cannot be updated to the latest because of the rollout strategy",
	}
	if rolloutStarted {
		cond = metav1.Condition{
			Type:               string(placementv1beta1.ResourceBindingRolloutStarted),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: binding.GetGeneration(),
			Reason:             condition.RolloutStartedReason,
			Message:            "Detected the new changes on the resources and started the rollout process",
		}
	}
	binding.SetConditions(cond)
	if err := r.Client.Status().Update(ctx, binding); err != nil {
		klog.ErrorS(err, "Failed to update binding status", "binding", klog.KObj(binding), "condition", cond)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated the status of a binding", "binding", klog.KObj(binding), "condition", cond)
	return nil
}

// processApplyStrategyUpdates processes apply strategy updates on the CRP end; specifically
// it will push the update to all applicable bindings.
func (r *Reconciler) processApplyStrategyUpdates(
	ctx context.Context,
	placementObj placementv1beta1.PlacementObj,
	allBindings []placementv1beta1.BindingObj,
) (applyStrategyUpdated bool, err error) {
	applyStrategy := placementObj.GetPlacementSpec().Strategy.ApplyStrategy
	if applyStrategy == nil {
		// Initialize the apply strategy with default values; normally this would not happen
		// as default values have been set up in the definitions.
		//
		// Note (chenyu1): at this moment, due to the fact that Fleet offers both v1 and v1beta1
		// APIs at the same time with Kubernetes favoring the v1 API by default, should the
		// user chooses to use the v1 API, default values for v1beta1 exclusive fields
		// might not be handled correctly, hence the default value resetting logic added here.
		applyStrategy = &placementv1beta1.ApplyStrategy{}
		defaulter.SetDefaultsApplyStrategy(applyStrategy)
	}

	errs, childCtx := errgroup.WithContext(ctx)
	for idx := range allBindings {
		binding := allBindings[idx]
		if !binding.GetDeletionTimestamp().IsZero() {
			// The binding has been marked for deletion; no need to push the apply strategy
			// update there.
			continue
		}

		// Verify if the binding has the latest apply strategy set.
		if equality.Semantic.DeepEqual(binding.GetBindingSpec().ApplyStrategy, applyStrategy) {
			// The binding already has the latest apply strategy set; no need to push the update.
			klog.V(2).InfoS("The binding already has the latest apply strategy; skip the apply strategy update", "binding", klog.KObj(binding), "bindingGeneration", binding.GetGeneration())
			continue
		}

		// Push the new apply strategy to the binding.
		//
		// The ApplyStrategy field on binding objects are managed exclusively by the rollout
		// controller; to avoid unnecessary conflicts, Fleet will patch the field directly.
		updatedBinding := binding.DeepCopyObject().(placementv1beta1.BindingObj)
		updatedSpec := updatedBinding.GetBindingSpec()
		updatedSpec.ApplyStrategy = applyStrategy
		applyStrategyUpdated = true

		errs.Go(func() error {
			if err := r.Client.Patch(childCtx, updatedBinding, client.MergeFrom(binding)); err != nil {
				klog.ErrorS(err, "Failed to update binding with new apply strategy", "binding", klog.KObj(binding))
				return controller.NewAPIServerError(false, err)
			}
			klog.V(2).InfoS("Updated binding with new apply strategy", "binding", klog.KObj(binding), "beforeUpdateBindingGeneration", binding.GetGeneration(), "afterUpdateBindingGeneration", updatedBinding.GetGeneration())
			return nil
		})
	}

	// The patches are issued in parallel; wait for all of them to complete (or the first error
	// to return).
	return applyStrategyUpdated, errs.Wait()
}

// handlePlacement handles the update event of a placement object (ClusterResourcePlacement or ResourcePlacement),
// which the rollout controller watches.
func handlePlacement(newPlacementObj, oldPlacementObj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Do some sanity checks. Normally these checks would never fail.
	if newPlacementObj == nil || oldPlacementObj == nil {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("placement object is nil")), "Received an unexpected nil object in the placement Update event", "placement (new)", klog.KObj(newPlacementObj), "placement (old)", klog.KObj(oldPlacementObj))
		return
	}

	// Try to cast to PlacementObj interface
	newPlacement, newOK := newPlacementObj.(placementv1beta1.PlacementObj)
	oldPlacement, oldOK := oldPlacementObj.(placementv1beta1.PlacementObj)
	if !newOK || !oldOK {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("object is not a placement object")), "Failed to cast the object in the placement Update event to a placement object", "placement (new)", klog.KObj(newPlacementObj), "placement (old)", klog.KObj(oldPlacementObj), "canCastNewObj", newOK, "canCastOldObj", oldOK)
		return
	}

	// Check if the placement has been deleted.
	if newPlacement.GetDeletionTimestamp() != nil {
		// No need to process a placement that has been marked for deletion.
		return
	}

	// Get placement specs using interface methods
	newPlacementSpec := newPlacement.GetPlacementSpec()
	oldPlacementSpec := oldPlacement.GetPlacementSpec()

	// Check if the rollout strategy type has been updated.
	if newPlacementSpec.Strategy.Type != oldPlacementSpec.Strategy.Type {
		klog.V(2).InfoS("Detected an update to the rollout strategy type on the placement", "placement", klog.KObj(newPlacement), "newType", newPlacementSpec.Strategy.Type, "oldType", oldPlacementSpec.Strategy.Type)
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: newPlacement.GetName(), Namespace: newPlacement.GetNamespace()},
		})
		return
	}

	// Check if the apply strategy has been updated.
	newApplyStrategy := newPlacementSpec.Strategy.ApplyStrategy
	oldApplyStrategy := oldPlacementSpec.Strategy.ApplyStrategy
	if !equality.Semantic.DeepEqual(newApplyStrategy, oldApplyStrategy) {
		klog.V(2).InfoS("Detected an update to the apply strategy on the placement", "placement", klog.KObj(newPlacement))
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: newPlacement.GetName(), Namespace: newPlacement.GetNamespace()},
		})
		return
	}

	klog.V(2).InfoS("No update to apply strategy detected; ignore the placement Update event", "placement", klog.KObj(newPlacement))
}
