/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
