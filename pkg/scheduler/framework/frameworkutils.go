/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"fmt"
	"reflect"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

// extractNumOfClustersFromPolicySnapshot extracts the numOfClusters from the policy snapshot.
func extractNumOfClustersFromPolicySnapshot(policy *fleetv1beta1.ClusterPolicySnapshot) (int, error) {
	numOfClustersStr, ok := policy.Annotations[fleetv1beta1.NumOfClustersAnnotation]
	if !ok {
		return 0, controller.NewUnexpectedBehaviorError(fmt.Errorf("cannot find annotation %s", fleetv1beta1.NumOfClustersAnnotation))
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	numOfClusters, err := strconv.Atoi(numOfClustersStr)
	if err != nil || numOfClusters < 0 {
		return 0, controller.NewUnexpectedBehaviorError(fmt.Errorf("invalid annotation %s: %s is not a valid count: %w", fleetv1beta1.NumOfClustersAnnotation, numOfClustersStr, err))
	}

	return numOfClusters, nil
}

// extractOwnerCRPNameFromPolicySnapshot extracts the name of the owner CRP from the policy snapshot.
func extractOwnerCRPNameFromPolicySnapshot(policy *fleetv1beta1.ClusterPolicySnapshot) (string, error) {
	var owner string
	for _, ownerRef := range policy.OwnerReferences {
		if ownerRef.Kind == utils.CRPV1Beta1GVK.Kind {
			owner = ownerRef.Name
			break
		}
	}
	if len(owner) == 0 {
		return "", fmt.Errorf("cannot find owner reference for policy snapshot %v", policy.Name)
	}
	return owner, nil
}

// classifyBindings categorizes bindings into three groups:
// * active: active bindings, that is, bindings that are not marked for deletion; and
// * deletedWithDispatcherFinalizer: bindings that are marked for deletion, but still has the dispatcher finalizer present; and
// * deletedWithoutDispatcherFinalizer: bindings that are marked for deletion, and the dispatcher finalizer is already removed.
func classifyBindings(bindings []fleetv1beta1.ClusterResourceBinding) (active, deletedWithDispatcherFinalizer, deletedWithoutDispatcherFinalizer []*fleetv1beta1.ClusterResourceBinding) {
	// Pre-allocate arrays.
	active = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	deletedWithDispatcherFinalizer = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	deletedWithoutDispatcherFinalizer = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))

	for idx := range bindings {
		binding := bindings[idx]
		if binding.DeletionTimestamp != nil {
			if controllerutil.ContainsFinalizer(&binding, utils.DispatcherFinalizer) {
				deletedWithDispatcherFinalizer = append(deletedWithDispatcherFinalizer, &binding)
			} else {
				deletedWithoutDispatcherFinalizer = append(deletedWithoutDispatcherFinalizer, &binding)
			}
		} else {
			active = append(active, &binding)
		}
	}

	return active, deletedWithDispatcherFinalizer, deletedWithoutDispatcherFinalizer
}

// shouldDownscale checks if the scheduler needs to perform some downscaling, and (if so) how many bindings
// it should remove.
func shouldDownscale(policy *fleetv1beta1.ClusterPolicySnapshot, numOfClusters int, active []*fleetv1beta1.ClusterResourceBinding) (act bool, count int) {
	if policy.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType && numOfClusters < len(active) {
		return true, len(active) - numOfClusters
	}
	return false, 0
}

// prepareNewSchedulingDecisions returns a list of new scheduling decisions, in accordance with the list
// of existing bindings.
func prepareNewSchedulingDecisions(policy *fleetv1beta1.ClusterPolicySnapshot, existing ...[]*fleetv1beta1.ClusterResourceBinding) []fleetv1beta1.ClusterDecision {
	// Pre-allocate arrays.
	current := policy.Status.ClusterDecisions
	desired := make([]fleetv1beta1.ClusterDecision, 0, len(existing))

	// Build new scheduling decisions.
	for _, bindings := range existing {
		for _, binding := range bindings {
			desired = append(desired, binding.Spec.ClusterDecision)
		}
	}

	// Move some decisions from unbound clusters, if there are still enough room.
	if diff := maxClusterDecisionCount - len(current); diff > 0 {
		for _, decision := range current {
			if !decision.Selected {
				desired = append(desired, decision)
				diff--
				if diff == 0 {
					break
				}
			}
		}
	}

	return desired
}

// fullySchedulingCondition returns a condition for fully scheduled policy snapshot.
func fullyScheduledCondition(policy *fleetv1beta1.ClusterPolicySnapshot) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetv1beta1.PolicySnapshotScheduled),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             fullyScheduledReason,
		Message:            fullyScheduledMessage,
	}
}

// shouldSchedule checks if the scheduler needs to perform some scheduling, and (if so) how many bindings.
//
// A scheduling cycle is only needed if
// * the policy is of the PickAll type; or
// * the policy is of the PickN type, and currently there are not enough number of bindings.
func shouldSchedule(policy *fleetv1beta1.ClusterPolicySnapshot, numOfClusters, existingBindingsCount int) bool {
	if policy.Spec.Policy.PlacementType == fleetv1beta1.PickAllPlacementType {
		return true
	}

	return numOfClusters > existingBindingsCount
}

// equalDecisions returns if two arrays of ClusterDecisions are equal; it returns true if
// every decision in one array is also present in the other array regardless of their indexes,
// and vice versa.
func equalDecisions(current, desired []fleetv1beta1.ClusterDecision) bool {
	desiredDecisionByCluster := make(map[string]fleetv1beta1.ClusterDecision, len(desired))
	for _, decision := range desired {
		desiredDecisionByCluster[decision.ClusterName] = decision
	}

	for _, decision := range current {
		// Note that it will return false if no matching decision can be found.
		if !reflect.DeepEqual(decision, desiredDecisionByCluster[decision.ClusterName]) {
			return false
		}
	}

	return len(current) == len(desired)
}

// notFullyScheduledCondition returns a condition for not fully scheduled policy snapshot.
func notFullyScheduledCondition(policy *fleetv1beta1.ClusterPolicySnapshot, desiredCount int) metav1.Condition {
	message := notFullyScheduledMessage
	if policy.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType {
		message = fmt.Sprintf("%s: expected count %d, current count %d", message, policy.Spec.Policy.NumberOfClusters, desiredCount)
	}
	return metav1.Condition{
		Type:               string(fleetv1beta1.PolicySnapshotScheduled),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             notFullyScheduledReason,
		Message:            message,
	}
}
