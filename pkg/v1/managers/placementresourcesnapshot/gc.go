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

package placementresourcesnapshot

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	errors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

type snapshotGarbageCollectionRequest struct {
	snapshotNamespacedName types.NamespacedName
	snapshotUID            types.UID

	placementPolicyNamespacedName types.NamespacedName
}

// enqueueOldResourceSnapshotsForGC enqueues old resource snapshots for garbage collection based on the
// revision history limit of the placement policy.
func (m *Manager) enqueueOldResourceSnapshotsForGC(
	sortedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor,
	placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor,
) {
	// Exclude the most recent snapshot from the history.
	history := sortedSnapshots[:len(sortedSnapshots)-1]
	revLimit := defaultRevisionHistoryLimit
	if placementPolicyAccessor.GetSpec().ResourceRevisionHistoryLimit != nil {
		revLimit = *placementPolicyAccessor.GetSpec().ResourceRevisionHistoryLimit
	}
	if revLimit <= 0 {
		// Do a sanity check.
		revLimit = 1
	}

	if len(history)+1 <= int(revLimit) {
		// No old snapshots to enqueue for GC.
		return
	}
	history = history[:len(history)+1-int(revLimit)]
	for _, snapshot := range history {
		if !snapshot.GetDeletionTimestamp().IsZero() {
			// Skip snapshots that are already marked for deletion.
			continue
		}
		m.gcwq.Add(snapshotGarbageCollectionRequest{
			snapshotNamespacedName: types.NamespacedName{
				Namespace: snapshot.GetNamespace(),
				Name:      snapshot.GetName(),
			},
			snapshotUID: snapshot.GetUID(),
			placementPolicyNamespacedName: types.NamespacedName{
				Namespace: placementPolicyAccessor.GetNamespace(),
				Name:      placementPolicyAccessor.GetName(),
			},
		})
	}
}

// checkForInconsistentResourceSnapshots scans the given (sorted) primary resource snapshots of a placement policy for
// snapshots that are not owned by the placement policy, e.g., those left over from a previous placement policy of
// the same name. It returns the leftover (ownerless) primary resource snapshots found, which are enqueued for
// garbage collection (unless already marked for deletion).
//
// Note that only primary resource snapshots are checked here. A secondary resource snapshot without a primary, i.e.,
// one created by a previous placement policy of the same name in an interrupted snapshot creation attempt and then
// orphaned (e.g., the placement policy is deleted with the orphan propagation policy), will not be detected; nor will
// the Kubernetes GC remove it, as it has no owners. This is considered acceptable, as such a snapshot is harmless:
// it is not referenced by any object, and it never gets picked up when listing snapshots by index, as any secondary
// snapshots left over at an index are always cleaned up (see cleanUpOrphanedSecondarySnapshots) before the placement
// policy creates new snapshots at that index. The snapshot will linger until then (or until it is removed manually).
func (m *Manager) checkForInconsistentResourceSnapshots(
	sortedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor,
	placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor,
) (leftovers []klog.ObjectRef, err error) {
	for idx := range sortedSnapshots {
		snapshot := sortedSnapshots[idx]

		// Check if the snapshot has an owner reference to the same named placement policy, but with a different UID.
		//
		// This signals that we might be observing an inconsistent state where the Kubernetes garbage collector (run by
		// kube-controller-manager) has yet to finish; return a transient error and wait for the situation to resolve.
		ownerRef := placementPolicyOwnerRef(snapshot, placementPolicyAccessor.GetName())
		if ownerRef != nil && ownerRef.UID != placementPolicyAccessor.GetUID() {
			// Err out right away; no need to keep scanning.
			return leftovers, errors.NewTransientError(nil,
				"found a placement resource snapshot owned by a different placement policy of the same name (e.g., one that has been deleted); waiting for garbage collection to complete",
				"placementResourceSnapshot", klog.KObj(snapshot), "observedUID", ownerRef.UID, "expectedUID", placementPolicyAccessor.GetUID())
		}

		// Check if the snapshot has no owners. If so, we might have caught a situation where the snapshot is orphaned.
		// Enqueue the snapshot for garbage collection, unless it is already marked for deletion; either way,
		// snapshotting stays blocked until the snapshot is gone.
		if len(snapshot.GetOwnerReferences()) == 0 {
			leftovers = append(leftovers, klog.KObj(snapshot))
			if snapshot.GetDeletionTimestamp().IsZero() {
				m.gcwq.Add(snapshotGarbageCollectionRequest{
					snapshotNamespacedName: types.NamespacedName{
						Namespace: snapshot.GetNamespace(),
						Name:      snapshot.GetName(),
					},
					snapshotUID: snapshot.GetUID(),
					placementPolicyNamespacedName: types.NamespacedName{
						Namespace: placementPolicyAccessor.GetNamespace(),
						Name:      placementPolicyAccessor.GetName(),
					},
				})
			}
		}

		// Handle the unlikely case where the snapshot has other owners (differently named placement policies, etc.).
		// This is considered a user error.
		if ownerRefs := snapshot.GetOwnerReferences(); len(ownerRefs) > 0 && (ownerRef == nil || len(ownerRefs) > 1) {
			return leftovers, errors.NewUserError(nil,
				fmt.Sprintf("found a placement resource snapshot (%s) that has unexpected owner references", snapshot.GetName()),
				"placementResourceSnapshot", klog.KObj(snapshot), "ownerReferences", ownerRefs,
				"placementPolicy", klog.KObj(placementPolicyAccessor))
		}
	}
	return leftovers, nil
}

// garbageCollect processes a garbage collection request.
func (m *Manager) garbageCollect(ctx context.Context, snapshotGCRequest snapshotGarbageCollectionRequest) error {
	// GC the resource snapshots (the primary and its secondaries).
	//
	// A GC request is issued for either:
	//
	// * an outdated primary resource snapshot (one beyond the revision history limit), which is only enqueued after
	//   the resource snapshot manager has seen at least one newer snapshot; or
	// * a primary resource snapshot left over from a previous placement policy of the same name (one without any
	//   owner references), which might be the latest snapshot that the previous placement policy has.
	//
	// In both cases, no more snapshots will be created at the index of the primary resource snapshot: KubeFleet
	// always creates the snapshots of an index in a specific order (secondaries first, primary last) and only moves
	// forward to newer indices afterwards; and the resource snapshot manager blocks any snapshot creation as long as
	// there is a primary resource snapshot not owned by the placement policy. This guarantees that the cache can be
	// reliably used to list a primary resource snapshot and its secondaries - no snapshots will be missed.
	//
	// The same mechanisms also ensure that, in normal operations, all the snapshots listed at the index come from
	// the same placement policy as the primary resource snapshot: snapshots of a re-created placement policy can only
	// be created after all the primary snapshots left over from previous placement policies are gone, and any
	// secondary snapshots left over at an index are cleaned up before new snapshots are created at that index (see
	// cleanUpOrphanedSecondarySnapshots).
	//
	// Note, however, that these mechanisms rely on the cache; as snapshot names factor in the UID of the placement
	// policy, the API server itself does not prevent snapshots of different placement policies of the same name from
	// co-existing at the same index. Under heavy cache lag, the snapshots listed at the index might include those of
	// a re-created placement policy. As a sanity check, the GC process skips any listed snapshot that is another
	// primary snapshot, or that does not share the same owner (or lack of one) as the primary resource snapshot.

	// Retrieve the primary resource snapshot.
	var primarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
	if snapshotGCRequest.snapshotNamespacedName.Namespace == "" {
		primarySnapshot = &placementv1alpha1.ClusterPlacementResourceSnapshot{}
	} else {
		primarySnapshot = &placementv1alpha1.PlacementResourceSnapshot{}
	}
	if err := m.hubClient.Get(ctx, snapshotGCRequest.snapshotNamespacedName, primarySnapshot); err != nil {
		if apierrors.IsNotFound(err) {
			// The primary snapshot has been garbage collected already.
			klog.V(2).InfoS("The primary placement resource snapshot is not found; skipping garbage collection",
				"snapshotGarbageCollectionRequest", snapshotGCRequest)
			return nil
		}
		return errors.NewAPIServerError(err, "failed to retrieve the primary placement resource snapshot", true)
	}

	// Do a sanity check; verify that the UID of the primary snapshot matches the expected UID from the garbage collection request.
	if primarySnapshot.GetUID() != snapshotGCRequest.snapshotUID {
		// The snapshot has been re-created; consider the GC attempt completed.
		klog.V(2).InfoS("The UID of the primary placement resource snapshot does not match the expected UID; skipping garbage collection",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "expectedUID", snapshotGCRequest.snapshotUID, "observedUID", primarySnapshot.GetUID())
		return nil
	}

	if !primarySnapshot.GetDeletionTimestamp().IsZero() {
		// The snapshot is already marked for deletion; no further action is needed.
		//
		// This can happen either because a) a GC attempt has been made, which guarantees that all secondary snapshots
		// are also marked for deletion already, or b) the placement policy itself is marked for deletion, which
		// would trigger the deletion of all associated snapshots, primary or secondary.
		klog.V(2).InfoS("The primary placement resource snapshot is already marked for deletion; skipping garbage collection",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
		return nil
	}

	// Verify that the snapshot is no longer in use by any placement bindings.
	//
	// This is a best-effort check; if there is an on-going rollout that uses a to-be-GC'd resource snapshot, the
	// GC process does not guarantee that the snapshot will be saved. Note that a binding, once synchronized,
	// no longer depends on its target resource snapshot; errors will only occur when the binding has not gotten
	// a chance to read the GC'd resource snapshot yet, and users will be notified that a newer rollout is needed to
	// fix the situation.
	//
	// TO-DO (chenyu1): evaluate if it is needed to add additional safeguards against such
	// premature garbage collection; the chances are quite low in normal ops.
	inUse, err := m.isSnapshotInUseByAnyPlacementBinding(ctx, primarySnapshot, snapshotGCRequest.placementPolicyNamespacedName)
	if err != nil {
		return errors.Wraps(err, "failed to check if the placement resource snapshot is in use by any placement bindings")
	}
	if inUse {
		// The snapshot will be left alone for now; the manager will check again at the next GC attempt.
		klog.V(2).InfoS("The placement resource snapshot is still in use by some placement bindings; skipping garbage collection",
			"snapshotGarbageCollectionRequest", snapshotGCRequest)
		return nil
	}

	// Retrieve the index of the primary resource snapshot.
	primarySnapshotIdxStr := primarySnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
	primarySnapshotIdx, err := strconv.Atoi(primarySnapshotIdxStr)
	if err != nil {
		return errors.NewUnexpectedError(err, "failed to parse the index label of the primary placement resource snapshot",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "observedIdx", primarySnapshotIdxStr)
	}

	// List all the resource snapshots with the same index.
	ownedBy := primarySnapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey]
	if ownedBy == "" {
		return errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing the owned-by annotation",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	fieldMatchers := client.MatchingFields{
		fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName: fmt.Sprintf(
			fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt,
			ownedBy, strconv.Itoa(primarySnapshotIdx)),
	}
	var snapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
	var listErr error
	if snapshotGCRequest.snapshotNamespacedName.Namespace == "" {
		// The snapshots are cluster-scoped; list cluster placement resource snapshots.
		snapshots, listErr = m.listPlacementResourceSnapshots(ctx, true, []client.ListOption{fieldMatchers})
	} else {
		// The snapshots are namespace-scoped; list placement resource snapshots in the same namespace.
		snapshots, listErr = m.listPlacementResourceSnapshots(ctx, false, []client.ListOption{
			client.InNamespace(snapshotGCRequest.snapshotNamespacedName.Namespace),
			fieldMatchers,
		})
	}
	if listErr != nil {
		return listErr
	}

	// Retrieve the owner reference of the primary resource snapshot that points to its placement policy (nil if the
	// primary resource snapshot has been orphaned); it is used to verify the ownership of the listed snapshots.
	primarySnapshotOwnerRef := placementPolicyOwnerRef(primarySnapshot, ownedBy)

	// Delete all the resource snapshots that are not the primary first.
	for _, snapshot := range snapshots {
		if snapshot.GetName() == primarySnapshot.GetName() {
			continue
		}

		// As a sanity check, skip any snapshot that is another primary snapshot (sub-index 0), or that does not
		// share the same owner (or lack of one) as the primary resource snapshot, e.g., a snapshot of a re-created
		// placement policy of the same name. See the notes at the beginning of this method; this can only happen
		// under heavy cache lag.
		//
		// A skipped snapshot is left to its owner (or the Kubernetes GC); should it end up as an ownerless secondary
		// snapshot, it is handled the same way as other leftover secondary snapshots (see the notes on
		// checkForInconsistentResourceSnapshots).
		subIdxStr := snapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
		if subIdxStr == "0" || !isSamePlacementPolicyOwner(placementPolicyOwnerRef(snapshot, ownedBy), primarySnapshotOwnerRef) {
			klog.V(2).InfoS("Found a placement resource snapshot at the same index that is another primary snapshot or has a different owner (the cache might be lagging); skipping its deletion",
				"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot),
				"skippedPlacementResourceSnapshot", klog.KObj(snapshot), "observedSubIndex", subIdxStr)
			continue
		}

		if err := m.hubClient.Delete(ctx, snapshot, client.Preconditions{UID: ptr.To(snapshot.GetUID())}); err != nil && !apierrors.IsNotFound(err) {
			return errors.NewAPIServerError(err, "failed to delete a secondary placement resource snapshot", false,
				"secondaryPlacementResourceSnapshot", klog.KObj(snapshot))
		}
	}

	// Delete the primary placement resource snapshot last.
	if err := m.hubClient.Delete(ctx, primarySnapshot, client.Preconditions{UID: ptr.To(primarySnapshot.GetUID())}); err != nil && !apierrors.IsNotFound(err) {
		return errors.NewAPIServerError(err, "failed to delete the primary placement resource snapshot", false,
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	klog.V(2).InfoS("Garbage collected the placement resource snapshots",
		"snapshotGarbageCollectionRequest", snapshotGCRequest)

	return nil
}

// isSnapshotInUseByAnyPlacementBinding checks if any placement binding of the placement policy (i.e., one whose spec
// names the placement policy; binding owners are not checked) is using the given (primary) placement resource snapshot.
//
// Note that this method reads from the cache, as the custom field index is only available there.
func (m *Manager) isSnapshotInUseByAnyPlacementBinding(ctx context.Context,
	primarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
	placementPolicyNamespacedName types.NamespacedName,
) (bool, error) {
	placementPolicyName := placementPolicyNamespacedName.Name
	snapshotOwnerRef := placementPolicyOwnerRef(primarySnapshot, placementPolicyName)
	if snapshotOwnerRef == nil {
		// The primary resource snapshot is not owned by the placement policy; always consider such snapshots to
		// be safe for GC.
		return false, nil
	}

	placementPolicyNamespace := placementPolicyNamespacedName.Namespace
	fieldMatchers := client.MatchingFields{
		fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: fmt.Sprintf(
			fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt,
			placementPolicyName,
			primarySnapshot.GetName(),
		),
	}
	// Only one matching binding is needed to tell if the snapshot is in use.
	limit := client.Limit(1)

	if placementPolicyNamespace == "" {
		// The placement policy is cluster-scoped; list cluster placement bindings.
		placementBindingList := &placementv1alpha1.ClusterPlacementBindingList{}
		if err := m.hubClient.List(ctx, placementBindingList, fieldMatchers, limit); err != nil {
			return false, errors.NewAPIServerError(err, "failed to list cluster placement bindings", true)
		}
		if len(placementBindingList.Items) == 0 {
			return false, nil
		}
	} else {
		// The placement policy is namespace-scoped; list placement bindings in the same namespace.
		placementBindingList := &placementv1alpha1.PlacementBindingList{}
		if err := m.hubClient.List(ctx, placementBindingList,
			client.InNamespace(placementPolicyNamespace), fieldMatchers, limit); err != nil {
			return false, errors.NewAPIServerError(err, "failed to list placement bindings", true)
		}
		if len(placementBindingList.Items) == 0 {
			return false, nil
		}
	}
	// A binding has been found to reference the primary resource snapshot; it is considered in use.
	return true, nil
}

// placementPolicyOwnerRef returns the owner reference on the given object that points to the placement policy
// of the given name; it returns nil if no such owner reference is found.
//
// Note that the placement policy is always of the same scope as the given object.
func placementPolicyOwnerRef(obj client.Object, placementPolicyName string) *metav1.OwnerReference {
	wantKind := placementv1alpha1.PlacementPolicyKind
	if obj.GetNamespace() == "" {
		wantKind = placementv1alpha1.ClusterPlacementPolicyKind
	}

	ownerRefs := obj.GetOwnerReferences()
	for idx := range ownerRefs {
		ownerRef := &ownerRefs[idx]
		gv, err := schema.ParseGroupVersion(ownerRef.APIVersion)
		if err != nil {
			// Skip owner references with malformed API versions.
			continue
		}
		if gv.Group == placementv1alpha1.GroupVersion.Group && ownerRef.Kind == wantKind && ownerRef.Name == placementPolicyName {
			return ownerRef
		}
	}
	return nil
}

// isSamePlacementPolicyOwner returns true if the two given owner references (as returned by placementPolicyOwnerRef)
// point to the same placement policy, or if both are nil (i.e., neither object is owned by the placement policy).
func isSamePlacementPolicyOwner(a, b *metav1.OwnerReference) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UID == b.UID
}
