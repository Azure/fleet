/*
Copyright 2026 The KubeFleet Authors.

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

package placementpolicy

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/naming"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/bindingmanager"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	// controllerName identifies this controller, to the manager and in the binding manager
	// role it holds on a policy while it changes the policy's bindings.
	controllerName = "placement-policy-controller"

	// bindingNameSegmentMaxLength bounds each of the two name segments of a generated binding
	// name; two segments, two separators, and the hash stay well within the 253-character
	// object name limit.
	bindingNameSegmentMaxLength = 100
)

// snapshotter is the slice of the placement resource snapshot manager this controller depends
// on. A binding names the resource snapshot it rolls out, and the FEP has the first snapshot
// taken when the placement is created, so the controller asks every round for the current
// snapshot -- creating it if there is none yet -- and whether it still matches the resources.
type snapshotter interface {
	SnapshotResourcesIfNoSnapshotExists(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor) ([]kfplacementv1alpha1.PlacementResourceSnapshotAccessor, bool, error)
}

// bindingName derives the deterministic name of the binding between a policy and a cluster. A
// binding lives in its policy's scope, so the policy name alone tells bindings of different
// policies apart; the hash covers the full identity so that two long names shortened to the
// same prefixes still get distinct bindings. Determinism makes creation get-or-create: a
// reconciler restarted between a create and the next list converges on the same object.
func bindingName(policy kfplacementv1alpha1.PlacementPolicyAccessor, clusterName string) string {
	return fmt.Sprintf("%s-%s-%s",
		naming.Truncate(policy.GetName(), bindingNameSegmentMaxLength),
		naming.Truncate(clusterName, bindingNameSegmentMaxLength),
		naming.Hash(policy.GetNamespace()+"/"+policy.GetName()+"/"+clusterName))
}

// chooseClusters decides which clusters each selector selects, records the decision on the
// outcomes, and returns, for every cluster that should hold a binding, the indices of the
// selectors it satisfies, in ascending order. bound maps each currently bound cluster to the
// time it was bound.
//
// Preferences are sticky, per the FEP: a bound cluster keeps its place ahead of any better
// ranked newcomer, and ranking is never re-applied to bound clusters, since re-ranking them on
// live metrics would move workloads around as the metrics drift. When a selector's count is
// lowered, the bindings of schedulable clusters that no longer satisfy its terms go first, then
// the newest ones, oldest and name breaking ties, so a scale-down undoes the latest picks and
// nothing else.
//
// Stickiness survives an outage: a bound cluster that still matches a selector's terms keeps
// its binding even while it is not schedulable (its agent has gone dark, or it carries a taint
// the policy stopped tolerating), because unbinding on an outage would tear resources down on
// a cluster that may be back in minutes. Such a cluster does not count toward the selector,
// so the gap is filled from the schedulable candidates, best-ranked first; once the cluster is
// back it is the older binding, and the replacement is what the count trims. A cluster no
// selector selects anymore -- evaluateSelectors keeps a bound cluster selected until the
// selector's terms change -- or that has left the fleet, is unbound.
//
// Selectors are processed in order, and a cluster chosen for an earlier selector counts toward
// every later selector it also matches, which is how one cluster may satisfy several selectors.
func chooseClusters(outcomes []selectorOutcome, bound map[string]metav1.Time, clusters map[string]*clusterv1beta1.MemberCluster) map[string][]int {
	desired := make(map[string][]int)
	// seniority orders the sticky phase, oldest binding first; a cluster picked earlier this
	// round has no binding yet and is the newest of all, so that a scale-down drops it first.
	seniority := func(a, b string) int {
		boundA, isBoundA := bound[a]
		boundB, isBoundB := bound[b]
		switch {
		case isBoundA != isBoundB:
			return cmp.Compare(btoi(isBoundB), btoi(isBoundA))
		case isBoundA:
			return cmp.Or(boundA.Time.Compare(boundB.Time), cmp.Compare(a, b))
		default:
			return cmp.Compare(a, b)
		}
	}

	for i := range outcomes {
		o := &outcomes[i]
		schedulable := sets.New(o.matched...)
		stale := sets.New(o.stale...)

		var kept []string
		for _, name := range slices.Concat(o.matched, o.sticky) {
			if _, isBound := bound[name]; isBound || desired[name] != nil {
				kept = append(kept, name)
			}
		}
		slices.SortFunc(kept, func(a, b string) int {
			return cmp.Or(cmp.Compare(btoi(stale.Has(a)), btoi(stale.Has(b))), seniority(a, b))
		})

		chosen := sets.New[string]()
		for _, name := range kept {
			if !schedulable.Has(name) {
				desired[name] = append(desired[name], i)
				continue
			}
			if !o.counts.selectAll && chosen.Len() >= int(o.counts.desired) {
				// Beyond the count: this is the scale-down. A later selector may still want it.
				continue
			}
			chosen.Insert(name)
			desired[name] = append(desired[name], i)
		}

		candidates := make([]*clusterv1beta1.MemberCluster, 0, schedulable.Len())
		for name := range schedulable.Difference(chosen) {
			candidates = append(candidates, clusters[name])
		}
		ranked := rankClusters(candidates)
		if !o.counts.selectAll {
			ranked = ranked[:max(0, min(len(ranked), int(o.counts.desired)-chosen.Len()))]
		}
		for _, name := range ranked {
			chosen.Insert(name)
			desired[name] = append(desired[name], i)
		}
		o.chosen = sets.List(chosen)
	}
	return desired
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// listBindings returns the bindings the policy controls. The list is served from the informer
// cache through the binding index on spec.placementPolicyName (registered by
// fieldindexers.SetupWithHubAgentControllerManager), within the policy's own scope: a
// ClusterPlacementPolicy only ever lists cluster-scoped bindings and a PlacementPolicy only the
// bindings in its namespace. Ownership is still decided by the controller reference, because the
// name does not tell a policy apart from an earlier, deleted one of the same name whose bindings
// were orphaned rather than collected. A binding the cache has yet to observe is caught by the
// AlreadyExists tolerance on create and by the re-queue its own create event fires.
func (r *Reconciler) listBindings(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor) ([]kfplacementv1alpha1.PlacementBindingAccessor, error) {
	byPolicy := client.MatchingFields{fieldindexers.PlacementBindingOwnedByCustomFieldName: policy.GetName()}
	var all []kfplacementv1alpha1.PlacementBindingAccessor
	if policy.GetNamespace() == "" {
		list := &kfplacementv1alpha1.ClusterPlacementBindingList{}
		if err := r.List(ctx, list, byPolicy); err != nil {
			return nil, kferrors.NewAPIServerError(err, "failed to list the cluster placement bindings of the policy", true, "placementPolicy", klog.KObj(policy))
		}
		for i := range list.Items {
			all = append(all, &list.Items[i])
		}
	} else {
		list := &kfplacementv1alpha1.PlacementBindingList{}
		if err := r.List(ctx, list, client.InNamespace(policy.GetNamespace()), byPolicy); err != nil {
			return nil, kferrors.NewAPIServerError(err, "failed to list the placement bindings of the policy", true, "placementPolicy", klog.KObj(policy))
		}
		for i := range list.Items {
			all = append(all, &list.Items[i])
		}
	}
	owned := make([]kfplacementv1alpha1.PlacementBindingAccessor, 0, len(all))
	for _, binding := range all {
		if metav1.IsControlledBy(binding, policy) {
			owned = append(owned, binding)
		}
	}
	return owned, nil
}

// holdsBindingManagerRole reports whether this controller currently holds the policy's binding
// manager role.
func holdsBindingManagerRole(policy kfplacementv1alpha1.PlacementPolicyAccessor) bool {
	manager := policy.GetStatus().BindingManager
	return manager != nil && manager.ControllerName == controllerName
}

// reconcileBindings creates, updates, and deletes the policy's bindings to match the decided
// cluster set. It reports roleHeld when the policy's binding manager role is held by another
// process, in which case nothing was changed and the caller should retry later.
//
// Binding changes run under the policy's binding manager role, the FEP's mutual exclusion between
// scheduling decision changes and rollouts. The role is released only once every change has gone
// through: a failed write returns with the role still held, so the retry resumes the same
// manipulation under the role it already holds, as the FEP prescribes for a process restarted
// mid-flow. A round that finds nothing left to do still releases a role a previous round left
// held, so a change that became moot before its retry cannot keep the role forever.
//
// A new binding rolls out snapshotName, the policy's latest resource snapshot; with no snapshot at
// hand (snapshotName empty), the clusters still to bind wait for a later round while every other
// change goes through.
func (r *Reconciler) reconcileBindings(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor, outcomes []selectorOutcome, desired map[string][]int, existing []kfplacementv1alpha1.PlacementBindingAccessor, snapshotName string) (roleHeld bool, err error) {
	selectorsFor := func(clusterName string) []kfplacementv1alpha1.ClusterSelectorWithTermsOnly {
		selectors := make([]kfplacementv1alpha1.ClusterSelectorWithTermsOnly, 0, len(desired[clusterName]))
		for _, i := range desired[clusterName] {
			selectors = append(selectors, kfplacementv1alpha1.ClusterSelectorWithTermsOnly{Terms: outcomes[i].terms})
		}
		return selectors
	}

	existingByCluster := make(map[string]kfplacementv1alpha1.PlacementBindingAccessor, len(existing))
	for _, binding := range existing {
		existingByCluster[binding.GetSpec().ClusterName] = binding
	}
	var toCreate []string
	var toUpdate, toDelete []kfplacementv1alpha1.PlacementBindingAccessor
	for clusterName := range desired {
		binding, bound := existingByCluster[clusterName]
		switch {
		case !bound:
			toCreate = append(toCreate, clusterName)
		case !apiequality.Semantic.DeepEqual(binding.GetSpec().ClusterSelectors, selectorsFor(clusterName)),
			!apiequality.Semantic.DeepEqual(binding.GetSpec().SyncStrategy, policy.GetSpec().SyncStrategy):
			toUpdate = append(toUpdate, binding)
		}
	}
	if snapshotName == "" {
		// Without a snapshot to roll out the clusters still to bind wait; dropping them before the
		// role is claimed keeps a round with nothing else to do from claiming it at all.
		toCreate = nil
	}
	slices.Sort(toCreate)
	for clusterName, binding := range existingByCluster {
		if _, wanted := desired[clusterName]; !wanted && binding.GetDeletionTimestamp().IsZero() {
			toDelete = append(toDelete, binding)
		}
	}

	ref := *policyReference(policy)
	if len(toCreate)+len(toUpdate)+len(toDelete) == 0 {
		if !holdsBindingManagerRole(policy) {
			return false, nil
		}
		return false, bindingmanager.RelinquishRoleFor(ctx, r.Client, policy, controllerName, ref)
	}

	claimed, err := bindingmanager.ClaimRoleAs(ctx, r.Client, policy, controllerName, ref)
	if err != nil {
		return false, err
	}
	if !claimed {
		return true, nil
	}

	// A taken binding name blocks only its own cluster: the occupant cannot be dealt with here, so
	// the rest of the changes go through, the role is released -- only a person or the garbage
	// collector can clear the way, and holding the role meanwhile would block the policy's rollouts
	// -- and the first such error is returned for a retry.
	var takenName error
	for _, clusterName := range toCreate {
		binding, err := r.newBinding(policy, clusterName, snapshotName, selectorsFor(clusterName))
		if err != nil {
			return false, err
		}
		klog.V(2).InfoS("Binding a cluster", "placementPolicy", klog.KObj(policy), "memberCluster", clusterName, "placementBinding", binding.GetName())
		if err := r.Create(ctx, binding); err != nil {
			if !errors.IsAlreadyExists(err) {
				return false, kferrors.NewAPIServerError(err, "failed to create the placement binding", false, "placementBinding", klog.KObj(binding))
			}
			if err := r.adoptOrReportBindingOccupant(ctx, policy, binding); err != nil {
				if category := kferrors.Category(err); category != kferrors.ErrCategoryUser && category != kferrors.ErrCategoryTransient {
					return false, err
				}
				klog.ErrorS(err, "Cannot bind a cluster: the binding name is taken", kferrors.Args(err)...)
				if takenName == nil {
					takenName = err
				}
			}
		}
	}
	for _, binding := range toUpdate {
		binding.GetSpec().ClusterSelectors = selectorsFor(binding.GetSpec().ClusterName)
		binding.GetSpec().SyncStrategy = policy.GetSpec().SyncStrategy.DeepCopy()
		if err := r.Update(ctx, binding); err != nil {
			return false, kferrors.NewAPIServerError(err, "failed to update the placement binding", false, "placementBinding", klog.KObj(binding))
		}
	}
	for _, binding := range toDelete {
		klog.V(2).InfoS("Unbinding a cluster", "placementPolicy", klog.KObj(policy), "memberCluster", binding.GetSpec().ClusterName, "placementBinding", binding.GetName())
		uid := binding.GetUID()
		if err := r.Delete(ctx, binding, client.Preconditions{UID: &uid}); err != nil && !errors.IsNotFound(err) {
			return false, kferrors.NewAPIServerError(err, "failed to delete the placement binding", false, "placementBinding", klog.KObj(binding))
		}
	}

	if err := bindingmanager.RelinquishRoleFor(ctx, r.Client, policy, controllerName, ref); err != nil {
		return false, err
	}
	return false, takenName
}

// adoptOrReportBindingOccupant settles a binding name the policy failed to create because it is
// taken. The occupant is read uncached, since the cache is what failed to show it:
//   - one the policy already controls is a create the cache has yet to show, and needs nothing;
//   - one being deleted frees the name shortly, and a later round creates the binding;
//   - an orphan of the policy's name for the same cluster -- left by an earlier policy of the same
//     name deleted without cascading -- is adopted and brought up to date, the way a ReplicaSet
//     adopts an orphaned pod, rather than leaving the cluster unbound behind a name the policy cannot
//     take; deleting it instead would tear down the workloads the orphaning kept running. Adoption
//     makes the policy's desired state win, so a suspension the orphan carried is lifted;
//   - one still controlled by an earlier policy of the same name is a transient error: the garbage
//     collector removes it shortly;
//   - one any other object controls is a user error: without it the cluster would stay unbound with
//     nothing saying why.
func (r *Reconciler) adoptOrReportBindingOccupant(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor, want kfplacementv1alpha1.PlacementBindingAccessor) error {
	var occupant kfplacementv1alpha1.PlacementBindingAccessor = &kfplacementv1alpha1.PlacementBinding{}
	if policy.GetNamespace() == "" {
		occupant = &kfplacementv1alpha1.ClusterPlacementBinding{}
	}
	if err := r.uncachedReader.Get(ctx, client.ObjectKeyFromObject(want), occupant); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return kferrors.NewAPIServerError(err, "failed to get the placement binding occupying the name", false, "placementBinding", klog.KObj(want))
	}
	switch {
	case metav1.IsControlledBy(occupant, policy), !occupant.GetDeletionTimestamp().IsZero():
		return nil
	case metav1.GetControllerOf(occupant) == nil && occupant.GetSpec().PlacementPolicyName == policy.GetName() &&
		occupant.GetSpec().ClusterName == want.GetSpec().ClusterName:
		klog.V(2).InfoS("Adopting an orphaned placement binding", "placementPolicy", klog.KObj(policy), "placementBinding", klog.KObj(occupant))
		if err := controllerutil.SetControllerReference(policy, occupant, r.Scheme()); err != nil {
			return kferrors.NewUnexpectedError(err, "failed to set the controller reference on the placement binding", "placementBinding", klog.KObj(occupant))
		}
		occupant.SetSpec(*want.GetSpec())
		if err := r.Update(ctx, occupant); err != nil {
			return kferrors.NewAPIServerError(err, "failed to adopt the orphaned placement binding", false, "placementBinding", klog.KObj(occupant))
		}
		return nil
	case isEarlierPolicyOfSameName(metav1.GetControllerOf(occupant), policy):
		return kferrors.NewTransientError(nil, "the placement binding name is taken by a binding of an earlier policy of the same name, awaiting garbage collection",
			"placementBinding", klog.KObj(occupant), "placementPolicy", klog.KObj(policy))
	default:
		return kferrors.NewUserError(nil, "the placement binding name is taken by a binding the policy does not control",
			"placementBinding", klog.KObj(occupant), "placementPolicy", klog.KObj(policy), "controller", metav1.GetControllerOf(occupant))
	}
}

// isEarlierPolicyOfSameName reports whether a controller reference names a policy of the same kind
// and name as the given one, but another incarnation of it.
func isEarlierPolicyOfSameName(ref *metav1.OwnerReference, policy kfplacementv1alpha1.PlacementPolicyAccessor) bool {
	kind := kfplacementv1alpha1.PlacementPolicyKind
	if policy.GetNamespace() == "" {
		kind = kfplacementv1alpha1.ClusterPlacementPolicyKind
	}
	return ref != nil && ref.Kind == kind && ref.Name == policy.GetName() && ref.UID != policy.GetUID()
}

// newBinding builds the binding between a policy and a cluster, owned by the policy so that
// deleting the policy takes its bindings with it.
func (r *Reconciler) newBinding(policy kfplacementv1alpha1.PlacementPolicyAccessor, clusterName, snapshotName string, selectors []kfplacementv1alpha1.ClusterSelectorWithTermsOnly) (kfplacementv1alpha1.PlacementBindingAccessor, error) {
	spec := kfplacementv1alpha1.PlacementBindingSpec{
		PlacementPolicyName:  policy.GetName(),
		ClusterSelectors:     selectors,
		ClusterName:          clusterName,
		ResourceSnapshotName: snapshotName,
		SyncStrategy:         policy.GetSpec().SyncStrategy.DeepCopy(),
	}
	objectMeta := metav1.ObjectMeta{Name: bindingName(policy, clusterName), Namespace: policy.GetNamespace()}
	var binding kfplacementv1alpha1.PlacementBindingAccessor
	if policy.GetNamespace() == "" {
		binding = &kfplacementv1alpha1.ClusterPlacementBinding{ObjectMeta: objectMeta, Spec: spec}
	} else {
		binding = &kfplacementv1alpha1.PlacementBinding{ObjectMeta: objectMeta, Spec: spec}
	}
	if err := controllerutil.SetControllerReference(policy, binding, r.Scheme()); err != nil {
		return nil, kferrors.NewUnexpectedError(err, "failed to set the controller reference on the placement binding", "placementBinding", klog.KObj(binding))
	}
	return binding, nil
}
