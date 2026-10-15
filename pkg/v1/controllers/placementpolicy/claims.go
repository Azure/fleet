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
	"context"
	"fmt"
	"slices"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/naming"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	// claimCleanupFinalizer marks policies with outstanding cluster claims; deleting such a
	// policy first withdraws its claims.
	claimCleanupFinalizer = "placement.kubefleet.dev/claim-cleanup"

	// maxConcurrentClaimsPerPolicy is the number of cluster claims a single policy may have in
	// flight, per the FEP's default of one; the user-configurable per-fleet and per-placement
	// limits are a config surface that arrives separately.
	maxConcurrentClaimsPerPolicy = 1

	// claimNameBaseMaxLength bounds the policy-name prefix inside a generated claim name so
	// the full name stays well within the 253-character object name limit.
	claimNameBaseMaxLength = 200
)

// claimOwnershipLabels returns the labels that select the claims of a policy.
func claimOwnershipLabels(policy kfplacementv1alpha1.PlacementPolicyAccessor) client.MatchingLabels {
	return client.MatchingLabels{
		kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel:      naming.LabelValue(policy.GetName()),
		kfplacementv1alpha1.ClusterClaimPlacementPolicyNamespaceLabel: policy.GetNamespace(),
	}
}

// claimName derives the deterministic name for the claim serving a policy's selector. The name
// embeds a hash of the policy's namespaced name because claims are cluster-scoped while
// PlacementPolicy names are only unique per namespace; two same-named policies in different
// namespaces must not collide. Determinism matters: creation is get-or-create, so a restarted
// reconciler converges on the same claim instead of issuing a duplicate.
func claimName(policy kfplacementv1alpha1.PlacementPolicyAccessor, selectorIndex int) string {
	base := naming.Truncate(policy.GetName(), claimNameBaseMaxLength)
	return fmt.Sprintf("%s-%d-%s", base, selectorIndex, naming.Hash(policy.GetNamespace()+"/"+policy.GetName()))
}

// desiredClaim describes a claim the policy currently wants outstanding.
type desiredClaim struct {
	name  string
	terms []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm
	// outcome is the selector this claim serves. It is what lets the reconcile decide when a
	// completed claim has done its job and should be rotated to provision the next cluster.
	outcome *selectorOutcome
}

// desiredClaims returns the claims the policy should have outstanding given the selector
// outcomes: one claim per unfulfilled selector that opted into AddClusterClaim, in selector
// order, capped by the per-policy concurrency limit.
func desiredClaims(policy kfplacementv1alpha1.PlacementPolicyAccessor, outcomes []selectorOutcome) []desiredClaim {
	wanted := make([]desiredClaim, 0, maxConcurrentClaimsPerPolicy)
	for i := range outcomes {
		o := &outcomes[i]
		if o.satisfiedInFull() || o.whenUnfulfilled != kfplacementv1alpha1.WhenUnfulfilledOptionAddClusterClaim {
			continue
		}
		wanted = append(wanted, desiredClaim{name: claimName(policy, i), terms: o.terms, outcome: o})
		if len(wanted) >= maxConcurrentClaimsPerPolicy {
			break
		}
	}
	return wanted
}

// claimReadyToRotate reports whether a claim has done its job -- the provisioner marked it completed
// and the cluster it provisioned is now eligible for the selector -- while the selector still needs
// more clusters. One claim yields one cluster (the claim carries no count, and its status names a
// single provisioned cluster), so a selector wanting several is filled one claim at a time: such a
// claim is withdrawn and reissued so the provisioner provisions the next cluster.
//
// The eligibility check is what makes this safe. The next claim is issued only once the current
// claim's cluster is counted toward the selector, so a provisioner is never handed a second claim
// while the first cluster is still joining -- which would ask it to provision two clusters at once
// for one selector.
func claimReadyToRotate(claim *kfplacementv1alpha1.ClusterClaim, outcome *selectorOutcome) bool {
	if outcome == nil || outcome.satisfiedInFull() {
		return false
	}
	if !meta.IsStatusConditionTrue(claim.Status.Conditions, kfplacementv1alpha1.ClusterClaimCondTypeCompleted) {
		return false
	}
	provisioned := claim.Status.ProvisionedClusterName
	return provisioned != nil && slices.Contains(outcome.matched, *provisioned)
}

// reconcileClaims drives the policy's cluster claims toward the desired set: it withdraws
// claims whose selector is fulfilled or gone, refreshes the freshness marker on claims that
// are still wanted, and issues new claims within the concurrency budget. It returns the number
// of claims outstanding for the policy.
//
// A claim held in Terminating by a provisioner finalizer still counts toward the concurrency
// budget (its deterministic name also blocks re-creation), so a slow provisioner teardown can
// never cause double-provisioning for the same selector.
func (r *Reconciler) reconcileClaims(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor, outcomes []selectorOutcome, mostRecentClusterCreation metav1.Time) (int32, error) {
	existing, err := r.listClaims(ctx, policy)
	if err != nil {
		return 0, err
	}
	if len(existing) > 0 {
		// Re-assert the cleanup finalizer while any claim exists, so an out-of-band finalizer
		// removal self-heals instead of leaving claims orphanable.
		if err := r.ensureFinalizer(ctx, policy); err != nil {
			return 0, err
		}
	}

	wanted := desiredClaims(policy, outcomes)
	wantedByName := make(map[string]desiredClaim, len(wanted))
	for _, w := range wanted {
		wantedByName[w.name] = w
	}

	var outstanding int32
	// The names withdrawn this pass, whose claims are already counted as outstanding.
	withdrawn := make(map[string]bool)
	for i := range existing {
		claim := &existing[i]
		if !claim.DeletionTimestamp.IsZero() {
			// Already being withdrawn: the claim still occupies its name and budget slot
			// regardless of whether its terms happen to match the currently wanted set (a
			// fulfillment flap can re-want identical terms mid-teardown), and an object on
			// its way out receives no further status writes.
			outstanding++
			delete(wantedByName, claim.Name)
			continue
		}
		w, stillWanted := wantedByName[claim.Name]
		// A claim only serves its original terms; if the policy's selector changed, the
		// outstanding claim is withdrawn and a fresh one is issued on a later pass.
		keep := stillWanted && apiequality.Semantic.DeepEqual(claim.Spec.ClusterSelectorTerms, w.terms)
		if keep && claimReadyToRotate(claim, w.outcome) {
			// The provisioner has provisioned an eligible cluster for this claim, but the selector
			// still needs more. The completed claim is not kept: it falls through to the withdrawal
			// below and, its entry left in wantedByName, is reissued on a later pass so the provisioner
			// gets a fresh claim for the next cluster. Rotation is gated on the provisioned cluster
			// being eligible, so the next claim is never issued before this one's cluster is confirmed.
			keep = false
		}
		if keep {
			delete(wantedByName, claim.Name)
			outstanding++
			if err := r.reconcileClaimLabels(ctx, claim, policy); err != nil {
				return outstanding, err
			}
			if err := r.refreshClaimFreshness(ctx, claim, mostRecentClusterCreation); err != nil {
				return outstanding, err
			}
			continue
		}
		klog.V(2).InfoS("Withdrawing a cluster claim", "clusterClaim", claim.Name, "placementPolicy", klog.KObj(policy))
		// The delete is pinned to the UID the cache showed: claim names are deterministic, so a
		// cache that has yet to see an earlier withdrawal can still list the predecessor after its
		// successor was created under the same name, and an unpinned delete would withdraw the
		// successor. A UID mismatch fails with a conflict, and the requeue retries on a fresher view.
		if err := r.Delete(ctx, claim, client.Preconditions{UID: &claim.UID}); err != nil {
			if errors.IsNotFound(err) {
				// Already fully gone; it occupies nothing.
				continue
			}
			return outstanding, kferrors.NewAPIServerError(err, "failed to withdraw the cluster claim", false, "clusterClaim", klog.KObj(claim))
		}
		// A claim withdrawn this pass still occupies its budget slot: a provisioner finalizer
		// can hold it in Terminating past this reconcile, and a differently-named claim created
		// below would otherwise stand beside it, exceeding the concurrency budget. The claim
		// watch re-queues the policy once the object is truly gone, and the slot frees then.
		outstanding++
		withdrawn[claim.Name] = true
	}

	if len(wantedByName) == 0 {
		// Nothing more to create. If nothing is outstanding either, the policy is claim-free, so the
		// cleanup finalizer -- whose only job is to withdraw claims before the policy is deleted --
		// has nothing left to guard and is released. Without this a policy that once had a claim would
		// carry the finalizer forever, and disabling the feature and then deleting such a claim-free
		// policy would hang its deletion with no controller left to clear it, outside the documented
		// outstanding-claims caveat.
		if outstanding == 0 {
			if err := r.releaseFinalizerIfNoClaims(ctx, policy); err != nil {
				return outstanding, err
			}
		}
		return outstanding, nil
	}

	// The cleanup finalizer lands on the policy before any claim is created, so a crash
	// between the two writes cannot orphan a claim.
	if err := r.ensureFinalizer(ctx, policy); err != nil {
		return outstanding, err
	}
	// The budget so far was counted from the cache, which can lag a claim created moments ago:
	// one issued last round for a selector that has since been satisfied would be missing, and a
	// claim for the next selector, under another name, would stand beside it. Claims the cache
	// has yet to show are counted from the API server whenever the cached count would still let
	// one be issued. (A claim created after this read, by a replica that has just lost
	// leadership, can still slip past; leader election makes that window negligible.)
	//
	// ponytail: lists every claim in the fleet uncached, but only on rounds that could issue
	// one; the ownership labels cannot narrow it server-side, being mutable.
	if outstanding < maxConcurrentClaimsPerPolicy {
		unseen, err := r.countUnseenClaims(ctx, policy, existing)
		if err != nil {
			return outstanding, err
		}
		outstanding += unseen
	}
	for _, w := range wanted {
		if _, still := wantedByName[w.name]; !still {
			continue
		}
		claim := &kfplacementv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: w.name,
				// The labels select a policy's claims; spec.placementPolicyRef below carries
				// the policy's authoritative identity, which a label value cannot always hold.
				Labels: claimOwnershipLabels(policy),
			},
			Spec: kfplacementv1alpha1.ClusterClaimSpec{
				PlacementPolicyRef:   policyReference(policy),
				ClusterSelectorTerms: w.terms,
			},
		}
		if outstanding >= maxConcurrentClaimsPerPolicy {
			// Every slot is taken, by kept claims or by ones withdrawn moments ago that may
			// still be terminating; the create is retried when a watch frees a slot.
			break
		}
		klog.V(2).InfoS("Adding a cluster claim", "clusterClaim", claim.Name, "placementPolicy", klog.KObj(policy))
		if err := r.Create(ctx, claim); err != nil {
			if !errors.IsAlreadyExists(err) {
				return outstanding, kferrors.NewAPIServerError(err, "failed to add the cluster claim", false, "clusterClaim", klog.KObj(claim))
			}
			// The deterministic name is still occupied. Most commonly it is this round's own
			// withdraw of a same-named claim that a provisioner finalizer holds in Terminating;
			// otherwise it may be a claim of this policy the cache has yet to show. Both are
			// already counted above, and the claim watch re-queues the policy once the occupant
			// is gone, so the create is retried then. A claim of another policy is reported as an
			// error instead; the watch maps that claim to its own policy, so this one retries on
			// the error backoff.
			if withdrawn[claim.Name] {
				continue
			}
			if _, err := r.claimNameOccupiedByPolicy(ctx, claim.Name, policy); err != nil {
				return outstanding, err
			}
			continue
		}
		outstanding++
		// Stamp the freshness marker at creation, per the FEP: the claim carries the latest
		// observed cluster creation timestamp from the moment provisioners can see it.
		if err := r.refreshClaimFreshness(ctx, claim, mostRecentClusterCreation); err != nil {
			return outstanding, err
		}
	}
	return outstanding, nil
}

// countUnseenClaims counts, from the API server, the claims of the policy that the given cached list
// does not show.
func (r *Reconciler) countUnseenClaims(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor, seen []kfplacementv1alpha1.ClusterClaim) (int32, error) {
	claims := &kfplacementv1alpha1.ClusterClaimList{}
	if err := r.uncachedReader.List(ctx, claims); err != nil {
		return 0, kferrors.NewAPIServerError(err, "failed to list the cluster claims of the policy", false, "placementPolicy", klog.KObj(policy))
	}
	var unseen int32
	for _, claim := range claimsForPolicy(claims.Items, policy) {
		if !slices.ContainsFunc(seen, func(c kfplacementv1alpha1.ClusterClaim) bool { return c.UID == claim.UID }) {
			unseen++
		}
	}
	return unseen, nil
}

// claimNameOccupiedByPolicy reports whether the claim holding a name this policy failed to create
// belongs to the policy. The read is uncached, since the cache is what failed to show the occupant.
// A claim of another policy under the name is a user error, logged with the occupant's reference:
// the policy cannot issue the claim until the occupant is gone, and without the error its selector
// would stay unfulfilled with nothing saying why. The error ends the round, leaving any later
// selector's claim to the retry; the budget is a single claim per policy, so nothing is lost.
func (r *Reconciler) claimNameOccupiedByPolicy(ctx context.Context, name string, policy kfplacementv1alpha1.PlacementPolicyAccessor) (bool, error) {
	occupant := &kfplacementv1alpha1.ClusterClaim{}
	if err := r.uncachedReader.Get(ctx, client.ObjectKey{Name: name}, occupant); err != nil {
		if errors.IsNotFound(err) {
			// Gone since the create; its deletion event re-queues the policy, and that pass
			// issues the claim.
			return false, nil
		}
		return false, kferrors.NewAPIServerError(err, "failed to get the cluster claim occupying the name", false, "clusterClaim", name)
	}
	if !claimBelongsTo(occupant, policyReference(policy)) {
		return false, kferrors.NewUserError(nil, "the cluster claim name is taken by a claim that does not belong to the policy",
			"clusterClaim", name, "placementPolicy", klog.KObj(policy), "occupantPlacementPolicyRef", occupant.Spec.PlacementPolicyRef)
	}
	return true, nil
}

// reconcileClaimLabels restores the ownership labels on a kept claim. The controller itself finds
// a policy's claims through the immutable spec.placementPolicyRef, so a stripped or rewritten label
// never confuses the reconcile; the labels exist for external consumers -- a provisioner watching a
// policy's claims by label -- and a provisioner or user that mutated one would hide the claim from
// them. The labels are re-asserted here for the same reason the cleanup finalizer is re-asserted
// while claims exist: out-of-band drift on an object the controller owns should self-heal.
func (r *Reconciler) reconcileClaimLabels(ctx context.Context, claim *kfplacementv1alpha1.ClusterClaim, policy kfplacementv1alpha1.PlacementPolicyAccessor) error {
	want := claimOwnershipLabels(policy)
	drifted := false
	for k, v := range want {
		if claim.Labels[k] != v {
			drifted = true
			break
		}
	}
	if !drifted {
		return nil
	}
	if claim.Labels == nil {
		claim.Labels = make(map[string]string, len(want))
	}
	for k, v := range want {
		claim.Labels[k] = v
	}
	klog.V(2).InfoS("Restoring ownership labels on a cluster claim", "clusterClaim", claim.Name, "placementPolicy", klog.KObj(policy))
	// A conflict means another writer touched the claim; the claim watch re-queues the policy and
	// the repair retries then. A NotFound means the claim was withdrawn out from under us, which a
	// later pass reconciles.
	if err := r.Update(ctx, claim); err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
		return kferrors.NewAPIServerError(err, "failed to restore the ownership labels of the cluster claim", false, "clusterClaim", klog.KObj(claim))
	}
	return nil
}

// refreshClaimFreshness advances the claim's freshness marker when clusters joined after the
// last observation; provisioners use the marker to tell that the claim has been re-evaluated
// and is still wanted.
func (r *Reconciler) refreshClaimFreshness(ctx context.Context, claim *kfplacementv1alpha1.ClusterClaim, mostRecent metav1.Time) error {
	if mostRecent.IsZero() {
		return nil
	}
	observed := claim.Status.LastObservedMostRecentClusterCreationTimestamp
	if observed != nil && !mostRecent.After(observed.Time) {
		return nil
	}
	claim.Status.LastObservedMostRecentClusterCreationTimestamp = &mostRecent
	// Conflicts are expected steady-state once a provisioner co-writes the claim status; the
	// provisioner's own write re-enqueues the policy through the claim watch, so the refresh
	// simply retries then. This only-advance guard above is also what terminates the
	// claim-watch self-loop: a refresh fires one echo reconcile, which then no-ops here.
	if err := r.Status().Update(ctx, claim); err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
		return kferrors.NewAPIServerError(err, "failed to refresh the freshness marker of the cluster claim", false, "clusterClaim", klog.KObj(claim))
	}
	return nil
}

// cleanupClaims withdraws every claim belonging to a policy that is being deleted, releasing
// the cleanup finalizer once none remain. The claim count that gates the finalizer release is
// read from the API server directly, not the informer cache: a claim created moments before
// the policy deletion might not have reached the cache yet, and releasing the finalizer on a
// stale zero would orphan it permanently (nothing else ever looks at claims of a gone policy).
func (r *Reconciler) cleanupClaims(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor) error {
	if !controllerutil.ContainsFinalizer(policy, claimCleanupFinalizer) {
		return nil
	}

	// The release decision cannot rest on the ownership labels: labels are mutable, and a claim
	// whose labels were stripped would vanish from a label-selected list, releasing the
	// finalizer over a claim that still exists -- permanently orphaned, since nothing else ever
	// looks at claims of a gone policy. The claim's spec.placementPolicyRef is immutable, so
	// every claim is listed and ownership is decided by the reference.
	claims := &kfplacementv1alpha1.ClusterClaimList{}
	if err := r.uncachedReader.List(ctx, claims); err != nil {
		wrappedErr := kferrors.NewAPIServerError(err, "failed to list the cluster claims of the deleted policy", false, "placementPolicy", klog.KObj(policy))
		klog.ErrorS(wrappedErr, "Failed to list cluster claims for the deleted policy", kferrors.Args(wrappedErr)...)
		return wrappedErr
	}
	ownRef := policyReference(policy)
	remaining := 0
	for i := range claims.Items {
		claim := &claims.Items[i]
		if !claimBelongsTo(claim, ownRef) {
			continue
		}
		if !claim.DeletionTimestamp.IsZero() {
			remaining++
			continue
		}
		klog.V(2).InfoS("Withdrawing a cluster claim of a deleted policy", "clusterClaim", claim.Name, "placementPolicy", klog.KObj(policy))
		if err := r.Delete(ctx, claim, client.Preconditions{UID: &claim.UID}); err != nil {
			if errors.IsNotFound(err) {
				// Already fully removed between the list and the delete; nothing remains for
				// this claim.
				continue
			}
			return kferrors.NewAPIServerError(err, "failed to withdraw the cluster claim of the deleted policy", false, "clusterClaim", klog.KObj(claim))
		}
		remaining++
	}
	if remaining > 0 {
		// Claims may be held in Terminating by provisioner finalizers; the watch on claims
		// re-queues the policy as they go away.
		return nil
	}

	controllerutil.RemoveFinalizer(policy, claimCleanupFinalizer)
	// Once the finalizer is gone the policy is deleted, and the reconcile that observes its
	// absence drops the metric series.
	return r.updatePolicy(ctx, policy, "failed to release the claim cleanup finalizer")
}

// ensureFinalizer adds the claim cleanup finalizer to the policy if not present yet.
func (r *Reconciler) ensureFinalizer(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor) error {
	if controllerutil.ContainsFinalizer(policy, claimCleanupFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(policy, claimCleanupFinalizer)
	return r.updatePolicy(ctx, policy, "failed to add the claim cleanup finalizer")
}

// claimBelongsTo reports whether a claim's immutable back-reference names the given policy.
// The API enforces the reference's immutability, which is what makes it, unlike the ownership
// labels, fit to gate the cleanup finalizer's release.
//
// The comparison deliberately ignores the reference's version: an API promotion keeps the group,
// kind, namespace, and name, and a version-strict comparison would orphan every outstanding claim
// the moment the policy API moved to a new version. The group is part of the identity: a claim
// naming another group's PlacementPolicy is not this policy's.
func claimBelongsTo(claim *kfplacementv1alpha1.ClusterClaim, ref *kfplacementv1alpha1.ObjectReference) bool {
	got := claim.Spec.PlacementPolicyRef
	if got == nil || ref == nil {
		return false
	}
	return got.APIGroup == ref.APIGroup && got.Kind == ref.Kind && got.Name == ref.Name && got.Namespace == ref.Namespace
}

// listClaims returns the cluster claims belonging to a policy, matched on the immutable
// spec.placementPolicyRef rather than the ownership labels.
//
// The labels are mutable: a provisioner or a user that removes or rewrites one would make the claim
// vanish from a label-selected list while spec.placementPolicyRef still names the policy. The live
// reconcile would then stop tracking a claim it still owns -- never withdrawing it once the selector
// is fulfilled, changed, or switched to KeepSearching -- and a provisioner could keep acting on it.
// Matching on the immutable reference, as the cleanup path already does, keeps that from happening.
//
// The list is served from the informer cache through the index on that reference (registered by
// fieldindexers.SetupWithHubAgentControllerManager), so a reconcile reads only its own policy's
// claims rather than every claim in the fleet. A claim the cache has yet to observe is picked up
// on the re-queue the claim watch fires, which is why the cache is acceptable here while the
// finalizer release, whose mistake would be permanent, reads through the uncached reader instead.
func (r *Reconciler) listClaims(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor) ([]kfplacementv1alpha1.ClusterClaim, error) {
	claims := &kfplacementv1alpha1.ClusterClaimList{}
	if err := r.List(ctx, claims, client.MatchingFields{
		fieldindexers.ClusterClaimOwnedByPlacementPolicyCustomFieldName: fieldindexers.ClusterClaimOwnedByPlacementPolicyCustomFieldVal(policyReference(policy)),
	}); err != nil {
		wrappedErr := kferrors.NewAPIServerError(err, "failed to list the cluster claims of the policy", true, "placementPolicy", klog.KObj(policy))
		klog.ErrorS(wrappedErr, "Failed to list cluster claims for the policy", kferrors.Args(wrappedErr)...)
		return nil, wrappedErr
	}
	return claims.Items, nil
}

// claimsForPolicy returns the claims whose immutable reference names the policy.
func claimsForPolicy(all []kfplacementv1alpha1.ClusterClaim, policy kfplacementv1alpha1.PlacementPolicyAccessor) []kfplacementv1alpha1.ClusterClaim {
	ref := policyReference(policy)
	owned := make([]kfplacementv1alpha1.ClusterClaim, 0, len(all))
	for i := range all {
		if claimBelongsTo(&all[i], ref) {
			owned = append(owned, all[i])
		}
	}
	return owned
}

// releaseFinalizerIfNoClaims removes the cleanup finalizer from a live policy once no claim of it
// remains, confirmed against the API server. It mirrors the release in cleanupClaims: the finalizer
// exists only to withdraw claims before the policy is deleted, so once none remain it is safe to
// drop -- and dropping it is what keeps a claim-free policy deletable even after the feature is
// turned off. The count is read uncached because releasing the finalizer over a claim a stale cache
// has yet to show would orphan it, nothing ever looking at the claims of a gone policy.
func (r *Reconciler) releaseFinalizerIfNoClaims(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor) error {
	if !controllerutil.ContainsFinalizer(policy, claimCleanupFinalizer) {
		return nil
	}
	claims := &kfplacementv1alpha1.ClusterClaimList{}
	if err := r.uncachedReader.List(ctx, claims); err != nil {
		wrappedErr := kferrors.NewAPIServerError(err, "failed to list the cluster claims of the policy", false, "placementPolicy", klog.KObj(policy))
		klog.ErrorS(wrappedErr, "Failed to list cluster claims to release the cleanup finalizer", kferrors.Args(wrappedErr)...)
		return wrappedErr
	}
	if len(claimsForPolicy(claims.Items, policy)) > 0 {
		return nil
	}
	controllerutil.RemoveFinalizer(policy, claimCleanupFinalizer)
	return r.updatePolicy(ctx, policy, "failed to release the claim cleanup finalizer")
}

// updatePolicy writes the policy object, categorizing a failure as an API server error.
func (r *Reconciler) updatePolicy(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor, desc string) error {
	if err := r.Update(ctx, policy); err != nil {
		return kferrors.NewAPIServerError(err, desc, false, "placementPolicy", klog.KObj(policy))
	}
	return nil
}

// policyReference builds the claim's back-reference to its policy.
func policyReference(policy kfplacementv1alpha1.PlacementPolicyAccessor) *kfplacementv1alpha1.ObjectReference {
	kind := kfplacementv1alpha1.PlacementPolicyKind
	if policy.GetNamespace() == "" {
		kind = kfplacementv1alpha1.ClusterPlacementPolicyKind
	}
	return &kfplacementv1alpha1.ObjectReference{
		APIGroup:   kfplacementv1alpha1.GroupVersion.Group,
		APIVersion: kfplacementv1alpha1.GroupVersion.Version,
		Kind:       kind,
		Name:       policy.GetName(),
		Namespace:  policy.GetNamespace(),
	}
}
