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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
//     yet has not been marked as unscheduled by the scheduler; and
//   - unscheduled bindings, i.e., bindings that are marked to be removed by the scheduler.
//   - obsolete bindings, i.e., bindings that are no longer associated with the latest scheduling
//     policy.
func classifyBindings(policy *placementv1beta1.ClusterSchedulingPolicySnapshot, bindings []placementv1beta1.ClusterResourceBinding, clusters []clusterv1beta1.MemberCluster) (bound, scheduled, obsolete, unscheduled, dangling []*placementv1beta1.ClusterResourceBinding) {
	// Pre-allocate arrays.
	bound = make([]*placementv1beta1.ClusterResourceBinding, 0, len(bindings))
	scheduled = make([]*placementv1beta1.ClusterResourceBinding, 0, len(bindings))
	obsolete = make([]*placementv1beta1.ClusterResourceBinding, 0, len(bindings))
	unscheduled = make([]*placementv1beta1.ClusterResourceBinding, 0, len(bindings))
	dangling = make([]*placementv1beta1.ClusterResourceBinding, 0, len(bindings))

	// Build a map for clusters for quick loopup.
	clusterMap := make(map[string]clusterv1beta1.MemberCluster)
	for _, cluster := range clusters {
		clusterMap[cluster.Name] = cluster
	}

	for idx := range bindings {
		binding := bindings[idx]
		targetCluster, isTargetClusterPresent := clusterMap[binding.Spec.TargetCluster]

		switch {
		case !binding.DeletionTimestamp.IsZero():
			// Ignore any binding that has been deleted.
			//
			// Note that the scheduler will not add any cleanup scheduler to a binding, as
			// in normal operations bound and scheduled bindings will not be deleted, and
			// unscheduled bindings are disregarded by the scheduler.
		case binding.Spec.State == placementv1beta1.BindingStateUnscheduled:
			// we need to remember those bindings so that we will not create another one.
			unscheduled = append(unscheduled, &binding)
		case !isTargetClusterPresent || !targetCluster.GetDeletionTimestamp().IsZero():
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
		case binding.Spec.State == placementv1beta1.BindingStateScheduled:
			// Check if the binding is of the scheduled state.
			scheduled = append(scheduled, &binding)
		case binding.Spec.State == placementv1beta1.BindingStateBound:
			// Check if the binding is of the bound state.
			bound = append(bound, &binding)
			// At this stage all states are already accounted for, so there is no need for a default
			// clause.
		}
	}

	return bound, scheduled, obsolete, unscheduled, dangling
}

// bindingWithPatch is a helper struct that includes a binding that needs to be patched and the
// patch itself.
type bindingWithPatch struct {
	// updated is the modified binding.
	updated *placementv1beta1.ClusterResourceBinding
	// patch is the patch that will be applied to the binding object.
	patch client.Patch
}

// crossReferencePickedClustersAndDeDupBindings cross references picked clusters in the current scheduling
// run and existing bindings to find out:
//
//   - bindings that should be created, i.e., create a binding in the state of Scheduled for every
//     cluster that is newly picked and does not have a binding associated with;
//   - bindings that should be patched, i.e., associate a binding, whose target cluster is picked again
//     in the current run, with the latest score and the latest scheduling policy snapshot (if applicable);
//     This will deduplicate bindings that are originally created in accordance with a previous scheduling round.
//   - bindings that should be deleted, i.e., mark a binding as unschedulable if its target cluster is no
//     longer picked in the current run.
//
// Note that this function will return bindings with all fields fulfilled/refreshed, as applicable.
func crossReferencePickedClustersAndDeDupBindings(
	crpName string,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	picked ScoredClusters,
	unscheduled, obsolete []*placementv1beta1.ClusterResourceBinding,
) (toCreate, toDelete []*placementv1beta1.ClusterResourceBinding, toPatch []*bindingWithPatch, err error) {
	// Pre-allocate with a reasonable capacity.
	toCreate = make([]*placementv1beta1.ClusterResourceBinding, 0, len(picked))
	toPatch = make([]*bindingWithPatch, 0, 20)
	toDelete = make([]*placementv1beta1.ClusterResourceBinding, 0, 20)

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
		// Add the binding to the toPatch list. We will simply keep the binding's state as
		// it could be "scheduled" or "bound".
		toPatch = append(toPatch, patchBinding(binding, binding.Spec.State, scored, policy))
	}

	for _, binding := range unscheduled {
		scored, ok := pickedMap[binding.Spec.TargetCluster]
		checked[binding.Spec.TargetCluster] = true
		if !ok {
			// this cluster is not picked up again, so we can skip it
			continue
		}
		// The binding's target cluster is picked again in the current run; yet the binding
		// is originally de-selected by the previous scheduling round.
		// Add the binding to the toPatch list so that we won't create more and more bindings.
		// We need to recover the previous state before the binding is marked as unscheduled.
		var desiredState placementv1beta1.BindingState
		// we recorded the previous state in the binding's annotation
		if previousState, exist := binding.GetAnnotations()[placementv1beta1.PreviousBindingStateAnnotation]; exist {
			desiredState = placementv1beta1.BindingState(previousState)
			// remove the annotation just to avoid confusion.
			binding.SetAnnotations(nil)
		} else {
			return nil, nil, nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to find the previous state of an unscheduled binding: %+v", binding))
		}
		toPatch = append(toPatch, patchBinding(binding, desiredState, scored, policy))
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
			binding := &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Labels: map[string]string{
						placementv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateScheduled,
					// Leave the associated resource snapshot name empty; it is up to another controller
					// to fulfill this field.
					SchedulingPolicySnapshotName: policy.Name,
					TargetCluster:                scored.Cluster.Name,
					ClusterDecision: placementv1beta1.ClusterDecision{
						ClusterName: scored.Cluster.Name,
						Selected:    true,
						ClusterScore: &placementv1beta1.ClusterScore{
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

func patchBinding(binding *placementv1beta1.ClusterResourceBinding, desiredState placementv1beta1.BindingState,
	scored *ScoredCluster, policy *placementv1beta1.ClusterSchedulingPolicySnapshot) *bindingWithPatch {
	// Update the binding so that it is associated with the latest score.
	updated := binding.DeepCopy()
	affinityScore := int32(scored.Score.AffinityScore)
	topologySpreadScore := int32(scored.Score.TopologySpreadScore)
	// Update the binding so that it is associated with the lastest scheduling policy.
	updated.Spec.State = desiredState
	updated.Spec.SchedulingPolicySnapshotName = policy.Name
	// copy the scheduling decision
	updated.Spec.ClusterDecision = placementv1beta1.ClusterDecision{
		ClusterName: scored.Cluster.Name,
		Selected:    true,
		ClusterScore: &placementv1beta1.ClusterScore{
			AffinityScore:       &affinityScore,
			TopologySpreadScore: &topologySpreadScore,
		},
		Reason: pickedByPolicyReason,
	}

	return &bindingWithPatch{
		updated: updated,
		// Prepare the patch.
		patch: client.MergeFrom(binding),
	}
}

// newSchedulingDecisionsFromBindings returns a list of scheduling decisions, based on the newly manipulated list of
// bindings and (if applicable) a list of filtered clusters.
func newSchedulingDecisionsFromBindings(
	maxUnselectedClusterDecisionCount int,
	notPicked ScoredClusters,
	filtered []*filteredClusterWithStatus,
	existing ...[]*placementv1beta1.ClusterResourceBinding,
) []placementv1beta1.ClusterDecision {
	// Pre-allocate with a reasonable capacity.
	newDecisions := make([]placementv1beta1.ClusterDecision, 0, maxUnselectedClusterDecisionCount)

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

	// Add decisions for clusters that have been scored, but are not picked, if there are still
	// enough room.
	for _, sc := range notPicked {
		if slotsLeft == 0 || maxUnselectedClusterDecisionCount == 0 {
			break
		}

		newDecisions = append(newDecisions, placementv1beta1.ClusterDecision{
			ClusterName: sc.Cluster.Name,
			Selected:    false,
			ClusterScore: &placementv1beta1.ClusterScore{
				AffinityScore:       pointer.Int32(int32(sc.Score.AffinityScore)),
				TopologySpreadScore: pointer.Int32(int32(sc.Score.TopologySpreadScore)),
			},
			Reason: notPickedByScoreReason,
		})

		slotsLeft--
		maxUnselectedClusterDecisionCount--
	}

	// Move some decisions from unbound clusters, if there are still enough room.
	for i := 0; i < maxUnselectedClusterDecisionCount && i < len(filtered) && i < slotsLeft; i++ {
		clusterWithStatus := filtered[i]
		newDecisions = append(newDecisions, placementv1beta1.ClusterDecision{
			ClusterName: clusterWithStatus.cluster.Name,
			Selected:    false,
			Reason:      clusterWithStatus.status.String(),
		})
	}

	return newDecisions
}

// newSchedulingCondition returns a new scheduling condition.
func newScheduledCondition(policy *placementv1beta1.ClusterSchedulingPolicySnapshot, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               string(placementv1beta1.PolicySnapshotScheduled),
		Status:             status,
		ObservedGeneration: policy.Generation,
		Reason:             reason,
		Message:            message,
	}
}

// newScheduledConditionFromBindings prepares a scheduling condition by comparing the desired
// number of cluster and the count of existing bindings.
func newScheduledConditionFromBindings(policy *placementv1beta1.ClusterSchedulingPolicySnapshot, numOfClusters int, existing ...[]*placementv1beta1.ClusterResourceBinding) metav1.Condition {
	count := 0
	for _, bindingSet := range existing {
		count += len(bindingSet)
	}

	if count < numOfClusters {
		// The current count of scheduled + bound bindings is less than the desired number.
		return newScheduledCondition(policy, metav1.ConditionFalse, notFullyScheduledReason, notFullyScheduledMessage)
	}
	// The desired number has been achieved.
	return newScheduledCondition(policy, metav1.ConditionTrue, fullyScheduledReason, fullyScheduledMessage)
}

// newSchedulingDecisionsForPickFixedPlacementType returns a list of scheduling decisions, based on different
// types of target clusters.
func newSchedulingDecisionsForPickFixedPlacementType(valid []*clusterv1beta1.MemberCluster, invalid []*invalidClusterWithReason, notFound []string) []placementv1beta1.ClusterDecision {
	// Pre-allocate with a reasonable capacity.
	clusterDecisions := make([]placementv1beta1.ClusterDecision, 0, len(valid))

	// Add decisions from valid target clusters.
	for _, cluster := range valid {
		clusterDecisions = append(clusterDecisions, placementv1beta1.ClusterDecision{
			ClusterName: cluster.Name,
			Selected:    true,
			// Scoring does not apply in this placement type.
			Reason: pickedByPolicyReason,
		})
	}

	// Add decisions from invalid target clusters.
	for _, clusterWithReason := range invalid {
		clusterDecisions = append(clusterDecisions, placementv1beta1.ClusterDecision{
			ClusterName: clusterWithReason.cluster.Name,
			Selected:    false,
			// Scoring does not apply in this placement type.
			Reason: fmt.Sprintf(pickFixedInvalidClusterReasonTemplate, clusterWithReason.reason),
		})
	}

	// Add decisions from not found target clusters.
	for _, clusterName := range notFound {
		clusterDecisions = append(clusterDecisions, placementv1beta1.ClusterDecision{
			ClusterName: clusterName,
			Selected:    false,
			// Scoring does not apply in this placement type.
			Reason: pickFixedNotFoundClusterReason,
		})
	}

	return clusterDecisions
}

// equalDecisions returns if two arrays of ClusterDecisions are equal; it returns true if
// every decision in one array is also present in the other array regardless of their indexes,
// and vice versa.
func equalDecisions(current, desired []placementv1beta1.ClusterDecision) bool {
	// As a shortcut, decisions are not equal if the two arrays are not of the same length.
	if len(current) != len(desired) {
		return false
	}

	desiredDecisionByCluster := make(map[string]placementv1beta1.ClusterDecision, len(desired))
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
// many scheduled or bound bindings it should remove.
func shouldDownscale(policy *placementv1beta1.ClusterSchedulingPolicySnapshot, desired, present, obsolete int) (act bool, count int) {
	if policy.Spec.Policy.PlacementType == placementv1beta1.PickNPlacementType && desired <= present {
		// Downscale only applies to CRPs of the Pick N placement type; and it only applies when the number of
		// clusters requested by the user is less than the number of currently bound + scheduled bindings combined;
		// or there are the right number of bound + scheduled bindings, yet some obsolete bindings still linger
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
func sortByClusterScoreAndName(bindings []*placementv1beta1.ClusterResourceBinding) (sorted []*placementv1beta1.ClusterResourceBinding) {
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

// calcNumOfClustersToSelect calculates the number of clusters to select in a scheduling run; it
// essentially returns the minimum among the desired number of clusters, the batch size limit,
// and the number of scored clusters.
func calcNumOfClustersToSelect(desired, limit, scored int) int {
	num := desired
	if limit < num {
		num = limit
	}
	if scored < num {
		num = scored
	}
	return num
}

// Pick clusters with the top N highest scores from a sorted list of clusters.
//
// Note that this function assumes that the list of clusters have been sorted by their scores,
// and the N count is no greater than the length of the list.
func pickTopNScoredClusters(scoredClusters ScoredClusters, N int) (picked, notPicked ScoredClusters) {
	// Sort the clusters by their scores in reverse order.
	//
	// Note that when two clusters have the same score, they are sorted by their names in
	// lexicographical order instead; this is to achieve deterministic behavior when picking
	// clusters.
	sort.Sort(sort.Reverse(scoredClusters))

	// No need to pick if there is no scored cluster or the number to pick is zero.
	if len(scoredClusters) == 0 || N == 0 {
		return make(ScoredClusters, 0), scoredClusters
	}

	// No need to pick if the number of scored clusters is less than or equal to N.
	if len(scoredClusters) <= N {
		return scoredClusters, make(ScoredClusters, 0)
	}

	return scoredClusters[:N], scoredClusters[N:]
}

// shouldRequeue determines if the scheduler should start another scheduling cycle on the same
// policy snapshot.
//
// For each scheduling run, four different possibilities exist:
//
//   - the desired batch size is equal to the batch size limit, i.e., no plugin has imposed a limit
//     on the batch size; and the actual number of bindings created/updated is equal to the desired
//     batch size
//     -> in this case, no immediate requeue is necessary as all the work has been completed.
//   - the desired batch size is equal to the batch size limit, i.e., no plugin has imposed a limit
//     on the batch size; but the actual number of bindings created/updated is less than the desired
//     batch size
//     -> in this case, no immediate requeue is necessary as retries will not correct the situation;
//     the scheduler should wait for the next signal from scheduling triggers, e.g., new cluster
//     joined, or scheduling policy is updated.
//   - the desired batch size is greater than the batch size limit, i.e., a plugin has imposed a limit
//     on the batch size; and the actual number of bindings created/updated is equal to batch size
//     limit
//     -> in this case, immediate requeue is needed as there might be more fitting clusters to bind
//     resources to.
//   - the desired batch size is greater than the batch size limit, i.e., a plugin has imposed a limit
//     on the batch size; but the actual number of bindings created/updated is less than the batch
//     size limit
//     -> in this case, no immediate requeue is necessary as retries will not correct the situation;
//     the scheduler should wait for the next signal from scheduling triggers, e.g., new cluster
//     joined, or scheduling policy is updated.
func shouldRequeue(desiredBatchSize, batchSizeLimit, bindingCount int) bool {
	if desiredBatchSize > batchSizeLimit && bindingCount == batchSizeLimit {
		return true
	}
	return false
}

// crossReferenceValidTargetsWithBindings cross references valid target clusters
// with the list of existing bindings (scheduled, bound, and obsolete ones) to find out:
//
//   - bindings that should be created, i.e., create a binding in the state of Scheduled for every
//     cluster that is a valid target and does not have a binding associated with;
//   - bindings that should be patched, i.e., associate a binding, whose target cluster is a valid target cluster
//     in the current run, with the latest score and the latest scheduling policy snapshot (if applicable);
//   - bindings that should be deleted, i.e., mark a binding as unschedulable if its target cluster is no
//     longer a valid cluster in the current run.
//
// Note that this function will return bindings with all fields fulfilled/refreshed, as applicable.
func crossReferenceValidTargetsWithBindings(
	crpName string,
	policy *placementv1beta1.ClusterSchedulingPolicySnapshot,
	valid []*clusterv1beta1.MemberCluster,
	bound, scheduled, obsolete []*placementv1beta1.ClusterResourceBinding,
) (
	toCreate []*placementv1beta1.ClusterResourceBinding,
	toDelete []*placementv1beta1.ClusterResourceBinding,
	toPatch []*bindingWithPatch,
	err error,
) {
	// Pre-allocate with a reasonable capacity.
	toCreate = make([]*placementv1beta1.ClusterResourceBinding, 0, len(valid))
	toPatch = make([]*bindingWithPatch, 0, 20)
	toDelete = make([]*placementv1beta1.ClusterResourceBinding, 0, 20)

	// Build maps for quick lookup.
	scheduledOrBoundClusterMap := make(map[string]bool)
	for _, binding := range scheduled {
		scheduledOrBoundClusterMap[binding.Spec.TargetCluster] = true
	}
	for _, binding := range bound {
		scheduledOrBoundClusterMap[binding.Spec.TargetCluster] = true
	}

	obsoleteClusterMap := make(map[string]*placementv1beta1.ClusterResourceBinding)
	for _, binding := range obsolete {
		obsoleteClusterMap[binding.Spec.TargetCluster] = binding
	}

	validTargetMap := make(map[string]bool)
	for _, cluster := range valid {
		validTargetMap[cluster.Name] = true
	}

	// Perform the cross-reference to find out bindings that should be created or patched.
	for _, cluster := range valid {
		_, foundInScheduledOrBound := scheduledOrBoundClusterMap[cluster.Name]
		obsoleteBinding, foundInObsolete := obsoleteClusterMap[cluster.Name]

		switch {
		case foundInScheduledOrBound:
			// The cluster already has a binding of the scheduled or bound state associated;
			// do nothing.
		case foundInObsolete:
			// The cluster already has a binding associated, but it is selected in a previous
			// scheduling run; update the binding to refer to the latest scheduling policy
			// snapshot.
			updated := obsoleteBinding.DeepCopy()
			// Technically speaking, overwriting the cluster decision is not needed, as the same value
			// should have been set in the previous run. Here the scheduler writes the information
			// again just in case.
			updated.Spec.ClusterDecision = placementv1beta1.ClusterDecision{
				ClusterName: cluster.Name,
				Selected:    true,
				// Scoring does not apply in this placement type.
				Reason: pickedByPolicyReason,
			}
			updated.Spec.SchedulingPolicySnapshotName = policy.Name

			toPatch = append(toPatch, &bindingWithPatch{
				updated: updated,
				// Prepare the patch.
				patch: client.MergeFrom(obsoleteBinding),
			})
		default:
			// The cluster does not have an associated binding yet; create one.

			// Generate a unique name.
			name, err := uniquename.NewClusterResourceBindingName(crpName, cluster.Name)
			if err != nil {
				// Cannot get a unique name for the binding; normally this should never happen.
				return nil, nil, nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to cross reference picked clusters and existing bindings: %w", err))
			}

			newBinding := &placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
					Labels: map[string]string{
						placementv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State: placementv1beta1.BindingStateScheduled,
					// Leave the associated resource snapshot name empty; it is up to another controller
					// to fulfill this field.
					SchedulingPolicySnapshotName: policy.Name,
					TargetCluster:                cluster.Name,
					ClusterDecision: placementv1beta1.ClusterDecision{
						ClusterName: cluster.Name,
						Selected:    true,
						// Scoring does not apply in this placement type.
						Reason: pickedByPolicyReason,
					},
				},
			}
			toCreate = append(toCreate, newBinding)
		}
	}

	// Perform the cross-reference to find out bindings that should be deleted.
	for _, binding := range obsolete {
		if _, ok := validTargetMap[binding.Spec.TargetCluster]; !ok {
			// The cluster is no longer a valid target; mark the binding as unscheduled.
			toDelete = append(toDelete, binding)
		}
	}

	return toCreate, toDelete, toPatch, nil
}
