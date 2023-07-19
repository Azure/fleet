/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"fmt"
	"reflect"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/framework/uniquename"
	"go.goms.io/fleet/pkg/utils/controller"
)

// classifyBindings categorizes bindings into the following groups:
//   - bound bindings, i.e., bindings that are associated with a normally operating cluster and
//     have been cleared for processing by the dispatcher; and
//   - scheduled bindings, i.e., bindings that have been associated with a normally operating cluster,
//     but have not yet been cleared for processing by the dispatcher; and
//   - dangling bindings, i.e., bindings that are associated with a cluster that is no longer in
//     a normally operating state (the cluster has left the fleet, or is in the state of leaving),
//     yet has not been marked as deleting by the scheduler; and
//   - obsolete bindings, i.e., bindings that are no longer associated with the latest scheduling
//     policy.
func classifyBindings(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, bindings []fleetv1beta1.ClusterResourceBinding, clusters []fleetv1beta1.MemberCluster) (bound, scheduled, obsolete, dangling []*fleetv1beta1.ClusterResourceBinding) {
	// Pre-allocate arrays.
	bound = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	scheduled = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	obsolete = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))
	dangling = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(bindings))

	// Build a map for clusters for quick loopup.
	clusterMap := make(map[string]fleetv1beta1.MemberCluster)
	for _, cluster := range clusters {
		clusterMap[cluster.Name] = cluster
	}

	for idx := range bindings {
		binding := bindings[idx]
		targetCluster, isTargetClusterPresent := clusterMap[binding.Spec.TargetCluster]

		switch {
		case binding.DeletionTimestamp != nil:
			// Ignore any binding that has been deleted.
			//
			// Note that the scheduler will not add any cleanup scheduler to a binding, as
			// in normal operations bound and scheduled bindings will not be deleted, and
			// unscheduled bindings are disregarded by the scheduler.
		case binding.Spec.State == fleetv1beta1.BindingStateUnscheduled:
			// Ignore any binding that is of the unscheduled state.
		case !isTargetClusterPresent || targetCluster.Spec.State == fleetv1beta1.ClusterStateLeave:
			// Check if the binding is now dangling, i.e., it is associated with a cluster that
			// is no longer in normal operations, but is still of a scheduled or bound state.
			//
			// Note that this check is solely for the purpose of detecting a situation where
			// bindings are stranded on a leaving/left cluster; it does not perform any binding
			// association eligibility check for the cluster.
			dangling = append(dangling, &binding)
		case binding.Spec.SchedulingPolicySnapshotName != policy.Name:
			// The binding is in the scheduled or bound state, but is no longer associated
			// with the latest scheduling policy snapshot.
			obsolete = append(obsolete, &binding)
		case binding.Spec.State == fleetv1beta1.BindingStateScheduled:
			// Check if the binding is of the scheduled state.
			scheduled = append(scheduled, &binding)
		case binding.Spec.State == fleetv1beta1.BindingStateBound:
			// Check if the binding is of the bound state.
			bound = append(bound, &binding)
			// At this stage all states are already accounted for, so there is no need for a default
			// clause.
		}
	}

	return bound, scheduled, obsolete, dangling
}

// bindingWithPatch is a helper struct that includes a binding that needs to be patched and the
// patch itself.
type bindingWithPatch struct {
	// updated is the modified binding.
	updated *fleetv1beta1.ClusterResourceBinding
	// patch is the patch that will be applied to the binding object.
	patch client.Patch
}

// crossReferencePickedCustersAndBindings cross references picked clusters in the current scheduling
// run and existing bindings to find out:
//
//   - bindings that should be created, i.e., create a binding in the state of Scheduled for every
//     cluster that is newly picked and does not have a binding associated with;
//   - bindings that should be patched, i.e., associate a binding, whose target cluster is picked again
//     in the current run, with the latest score and the latest scheduling policy snapshot (if applicable);
//   - bindings that should be deleted, i.e., mark a binding as unschedulable if its target cluster is no
//     longer picked in the current run.
//
// Note that this function will return bindings with all fields fulfilled/refreshed, as applicable.
func crossReferencePickedCustersAndObsoleteBindings(
	crpName string,
	policy *fleetv1beta1.ClusterSchedulingPolicySnapshot,
	picked ScoredClusters,
	obsolete []*fleetv1beta1.ClusterResourceBinding,
) (toCreate, toDelete []*fleetv1beta1.ClusterResourceBinding, toPatch []*bindingWithPatch, err error) {
	// Pre-allocate with a reasonable capacity.
	toCreate = make([]*fleetv1beta1.ClusterResourceBinding, 0, len(picked))
	toPatch = make([]*bindingWithPatch, 0, 20)
	toDelete = make([]*fleetv1beta1.ClusterResourceBinding, 0, 20)

	// Build a map of picked scored clusters for quick lookup.
	pickedMap := make(map[string]*ScoredCluster)
	for _, scored := range picked {
		pickedMap[scored.Cluster.Name] = scored
	}

	// Build a map of all clusters that have been cross-referenced.
	checked := make(map[string]bool)

	for _, binding := range obsolete {
		scored, ok := pickedMap[binding.Spec.TargetCluster]
		checked[binding.Spec.TargetCluster] = true

		if !ok {
			// The binding's target cluster is no longer picked in the current run; mark the
			// binding as unscheduled.
			toDelete = append(toDelete, binding)
			continue
		}

		// The binding's target cluster is picked again in the current run; yet the binding
		// is originally created/updated in accordance with an out-of-date scheduling policy.

		// Update the binding so that it is associated with the latest score.
		updated := binding.DeepCopy()
		affinityScore := int32(scored.Score.AffinityScore)
		topologySpreadScore := int32(scored.Score.TopologySpreadScore)
		updated.Spec.ClusterDecision = fleetv1beta1.ClusterDecision{
			ClusterName: scored.Cluster.Name,
			Selected:    true,
			ClusterScore: &fleetv1beta1.ClusterScore{
				AffinityScore:       &affinityScore,
				TopologySpreadScore: &topologySpreadScore,
			},
			Reason: pickedByPolicyReason,
		}

		// Update the binding so that it is associated with the lastest scheduling policy.
		updated.Spec.SchedulingPolicySnapshotName = policy.Name

		// Add the binding to the toUpdate list.
		toPatch = append(toPatch, &bindingWithPatch{
			updated: updated,
			// Prepare the patch.
			patch: client.MergeFrom(binding),
		})
	}

	for _, scored := range picked {
		if _, ok := checked[scored.Cluster.Name]; !ok {
			// The cluster is newly picked in the current run; it does not have an associated binding in presence.
			name, err := uniquename.NewClusterResourceBindingName(crpName, scored.Cluster.Name)
			if err != nil {
				// Cannot get a unique name for the binding; normally this should never happen.
				return nil, nil, nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to cross reference picked clusters and existing bindings: %w", err))
			}
			affinityScore := int32(scored.Score.AffinityScore)
			topologySpreadScore := int32(scored.Score.TopologySpreadScore)
			binding := &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State: fleetv1beta1.BindingStateScheduled,
					// Leave the associated resource snapshot name empty; it is up to another controller
					// to fulfill this field.
					SchedulingPolicySnapshotName: policy.Name,
					TargetCluster:                scored.Cluster.Name,
					ClusterDecision: fleetv1beta1.ClusterDecision{
						ClusterName: scored.Cluster.Name,
						Selected:    true,
						ClusterScore: &fleetv1beta1.ClusterScore{
							AffinityScore:       &affinityScore,
							TopologySpreadScore: &topologySpreadScore,
						},
						Reason: pickedByPolicyReason,
					},
				},
			}

			toCreate = append(toCreate, binding)
		}
	}

	return toCreate, toDelete, toPatch, nil
}

// newSchedulingDecisionsFrom returns a list of scheduling decisions, based on the newly manipulated list of
// bindings and (if applicable) a list of filtered clusters.
func newSchedulingDecisionsFrom(maxUnselectedClusterDecisionCount int, filtered []*filteredClusterWithStatus, existing ...[]*fleetv1beta1.ClusterResourceBinding) []fleetv1beta1.ClusterDecision {
	// Pre-allocate with a reasonable capacity.
	newDecisions := make([]fleetv1beta1.ClusterDecision, 0, maxUnselectedClusterDecisionCount)

	// Build new scheduling decisions.
	slotsLeft := clustersDecisionArrayLengthLimitInAPI
	for _, bindingSet := range existing {
		setLength := len(bindingSet)
		for i := 0; i < setLength && i < slotsLeft; i++ {
			newDecisions = append(newDecisions, bindingSet[i].Spec.ClusterDecision)
		}

		slotsLeft -= setLength
		if slotsLeft <= 0 {
			klog.V(2).InfoS("Reached API limit of cluster decision count; decisions off the limit will be discarded")
			break
		}
	}

	// Move some decisions from unbound clusters, if there are still enough room.
	for i := 0; i < maxUnselectedClusterDecisionCount && i < len(filtered) && i < slotsLeft; i++ {
		clusterWithStatus := filtered[i]
		newDecisions = append(newDecisions, fleetv1beta1.ClusterDecision{
			ClusterName: clusterWithStatus.cluster.Name,
			Selected:    false,
			Reason:      clusterWithStatus.status.String(),
		})
	}

	return newDecisions
}

// fullySchedulingCondition returns a condition for fully scheduled policy snapshot.
func fullyScheduledCondition(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetv1beta1.PolicySnapshotScheduled),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: policy.Generation,
		Reason:             fullyScheduledReason,
		Message:            fullyScheduledMessage,
	}
}

// equalDecisions returns if two arrays of ClusterDecisions are equal; it returns true if
// every decision in one array is also present in the other array regardless of their indexes,
// and vice versa.
func equalDecisions(current, desired []fleetv1beta1.ClusterDecision) bool {
	// As a shortcut, decisions are not equal if the two arrays are not of the same length.
	if len(current) != len(desired) {
		return false
	}

	desiredDecisionByCluster := make(map[string]fleetv1beta1.ClusterDecision, len(desired))
	for _, decision := range desired {
		desiredDecisionByCluster[decision.ClusterName] = decision
	}

	for _, decision := range current {
		matched, ok := desiredDecisionByCluster[decision.ClusterName]
		if !ok {
			// No matching decision can be found.
			return false
		}
		if !reflect.DeepEqual(decision, matched) {
			// A matched decision is found but the two decisions are not equal.
			return false
		}
	}

	// The two arrays have the same length and the same content.
	return true
}

// shouldDownscale checks if the scheduler needs to perform some downscaling, and (if so) how
// many active or creating bindings it should remove.
func shouldDownscale(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot, desired, present, obsolete int) (act bool, count int) {
	if policy.Spec.Policy.PlacementType == fleetv1beta1.PickNPlacementType && desired <= present {
		// Downscale only applies to CRPs of the Pick N placement type; and it only applies when the number of
		// clusters requested by the user is less than the number of currently active + creating bindings combined;
		// or there are the right number of active + creating bindings, yet some obsolete bindings still linger
		// in the system.
		if count := present - desired + obsolete; count > 0 {
			// Note that in the case of downscaling, obsolete bindings are always removed; they
			// are counted towards the returned downscale count value.
			return true, present - desired
		}
	}
	return false, 0
}

// sortByClusterScoreAndName sorts a list of ClusterResourceBindings by their cluster scores and
// target cluster names.
func sortByClusterScoreAndName(bindings []*fleetv1beta1.ClusterResourceBinding) (sorted []*fleetv1beta1.ClusterResourceBinding) {
	lessFunc := func(i, j int) bool {
		bindingA := bindings[i]
		bindingB := bindings[j]

		scoreA := bindingA.Spec.ClusterDecision.ClusterScore
		scoreB := bindingB.Spec.ClusterDecision.ClusterScore

		switch {
		case scoreA == nil && scoreB == nil:
			// Both bindings have no assigned cluster scores; normally this will never happen,
			// as for CRPs of the PickN type, the scheduler will always assign cluster scores
			// to bindings.
			//
			// In this case, compare their target cluster names instead.
			return bindingA.Spec.TargetCluster < bindingB.Spec.TargetCluster
		case scoreA == nil:
			// If only one binding has no assigned cluster score, prefer trimming it first.
			return true
		case scoreB == nil:
			// If only one binding has no assigned cluster score, prefer trimming it first.
			return false
		default:
			// Both clusters have assigned cluster scores; compare their scores first.
			clusterScoreA := ClusterScore{
				AffinityScore:       int(*scoreA.AffinityScore),
				TopologySpreadScore: int(*scoreA.TopologySpreadScore),
			}
			clusterScoreB := ClusterScore{
				AffinityScore:       int(*scoreB.AffinityScore),
				TopologySpreadScore: int(*scoreB.TopologySpreadScore),
			}

			if clusterScoreA.Equal(&clusterScoreB) {
				// Two clusters have the same scores; compare their names instead.
				return bindingA.Spec.TargetCluster < bindingB.Spec.TargetCluster
			}

			return clusterScoreA.Less(&clusterScoreB)
		}
	}
	sort.Slice(bindings, lessFunc)

	return bindings
}

// shouldSchedule checks if the scheduler needs to perform some scheduling.
func shouldSchedule(desiredCount, existingCount int) bool {
	return desiredCount > existingCount
}
