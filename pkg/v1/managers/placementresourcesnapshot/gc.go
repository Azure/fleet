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

// garbageCollect processes a garbage collection request.
func (m *Manager) garbageCollect(ctx context.Context, snapshotGCRequest snapshotGarbageCollectionRequest) error {
	// GC the resource snapshots (the primary and its secondaries).
	//
	// Note that KubeFleet always creates resource snapshots in a specific order, and GC only occurs when the
	// resource snapshot manager has already seen at least one newer snapshot; this guarantees that the cache
	// can be reliably used to list an outdated primary resource snapshot and its secondaries - no snapshots will
	// be missed.

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
	inUse, err := m.isSnapshotInUseByAnyPlacementBinding(ctx, primarySnapshot)
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
	ownedBy := primarySnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey]
	if ownedBy == "" {
		return errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing the owned-by label",
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

	// Retrieve the owner reference of the primary resource snapshot that points to its placement policy; it is
	// used to verify the ownership of the secondary resource snapshots.
	placementPolicyName := primarySnapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey]
	if placementPolicyName == "" {
		return errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing the owned-by annotation",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	primarySnapshotOwnerRef := placementPolicyOwnerRef(primarySnapshot, placementPolicyName)
	if primarySnapshotOwnerRef == nil {
		return errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing the owner reference to its placement policy",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "placementPolicyName", placementPolicyName)
	}

	// Delete all the resource snapshots that are not the primary first.
	for _, snapshot := range snapshots {
		if snapshot.GetName() == primarySnapshot.GetName() {
			continue
		}

		// As a sanity check, verify if the secondary snapshot is owned by the same placement policy as the
		// primary resource snapshot.
		//
		// The snapshots are listed by their owned-by label and index only; if a placement policy has been deleted
		// and re-created with the same name, the cache might hold snapshots owned by different generations of
		// the placement policy at the same time.
		secondarySnapshotOwnerRef := placementPolicyOwnerRef(snapshot, placementPolicyName)
		if secondarySnapshotOwnerRef == nil {
			return errors.NewUnexpectedError(nil, "the secondary placement resource snapshot is missing the owner reference to its placement policy",
				"secondaryPlacementResourceSnapshot", klog.KObj(snapshot), "placementPolicyName", placementPolicyName)
		}
		if secondarySnapshotOwnerRef.UID != primarySnapshotOwnerRef.UID {
			return errors.NewTransientError(nil, "observed an inconsistent state: the secondary and the primary placement resource snapshots are owned by different placement policies with the same name",
				"secondaryPlacementResourceSnapshot", klog.KObj(snapshot), "secondaryPlacementResourceSnapshotOwnerUID", secondarySnapshotOwnerRef.UID,
				"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "primaryPlacementResourceSnapshotOwnerUID", primarySnapshotOwnerRef.UID)
		}

		if err := m.hubClient.Delete(ctx, snapshot); err != nil && !apierrors.IsNotFound(err) {
			return errors.NewAPIServerError(err, "failed to delete a secondary placement resource snapshot", false,
				"secondaryPlacementResourceSnapshot", klog.KObj(snapshot))
		}
	}

	// Delete the primary placement resource snapshot last.
	if err := m.hubClient.Delete(ctx, primarySnapshot); err != nil && !apierrors.IsNotFound(err) {
		return errors.NewAPIServerError(err, "failed to delete the primary placement resource snapshot", false,
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	klog.V(2).InfoS("Garbage collected the placement resource snapshots",
		"snapshotGarbageCollectionRequest", snapshotGCRequest)

	return nil
}

// isSnapshotInUseByAnyPlacementBinding checks if any placement binding owned by the placement policy is using the
// given (primary) placement resource snapshot.
//
// Note that this method reads from the cache, as the custom field index is only available there.
func (m *Manager) isSnapshotInUseByAnyPlacementBinding(ctx context.Context, primarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor) (bool, error) {
	// Retrieve the name of the owner placement policy.
	//
	// Note that the owned-by label is not used here, as its value might have been truncated.
	placementPolicyName := primarySnapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey]
	if placementPolicyName == "" {
		return false, errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing the owned-by annotation",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	// The placement policy always lives in the same namespace as its placement resource snapshots.
	placementPolicyNamespace := primarySnapshot.GetNamespace()
	fieldMatchers := client.MatchingFields{
		fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: fmt.Sprintf(
			fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt,
			placementPolicyName,
			primarySnapshot.GetName(),
		),
	}
	// Only one matching binding is needed to tell if the snapshot is in use.
	limit := client.Limit(1)

	var placementBinding placementv1alpha1.PlacementBindingAccessor
	if placementPolicyNamespace == "" {
		// The placement policy is cluster-scoped; list cluster placement bindings.
		placementBindingList := &placementv1alpha1.ClusterPlacementBindingList{}
		if err := m.hubClient.List(ctx, placementBindingList, fieldMatchers, limit); err != nil {
			return false, errors.NewAPIServerError(err, "failed to list cluster placement bindings", true)
		}
		if len(placementBindingList.Items) == 0 {
			return false, nil
		}
		placementBinding = &placementBindingList.Items[0]
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
		placementBinding = &placementBindingList.Items[0]
	}

	// As a sanity check, verify that the placement binding is owned by the same placement policy as the
	// given primary placement resource snapshot.
	//
	// The field index matches placement bindings by the name of their owner placement policy only; if a placement
	// policy has been deleted and re-created with the same name, the cache might hold objects owned by
	// different generations of the placement policy at the same time.
	snapshotOwnerRef := placementPolicyOwnerRef(primarySnapshot, placementPolicyName)
	if snapshotOwnerRef == nil {
		return false, errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing the owner reference to its placement policy",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "placementPolicyName", placementPolicyName)
	}
	bindingOwnerRef := placementPolicyOwnerRef(placementBinding, placementPolicyName)
	if bindingOwnerRef == nil {
		return false, errors.NewUnexpectedError(nil, "the placement binding is missing the owner reference to its placement policy",
			"placementBinding", klog.KObj(placementBinding), "placementPolicyName", placementPolicyName)
	}
	if snapshotOwnerRef.UID != bindingOwnerRef.UID {
		return false, errors.NewTransientError(nil, "observed an inconsistent state: the placement binding and the primary placement resource snapshot are owned by different placement policies with the same name",
			"placementBinding", klog.KObj(placementBinding), "placementBindingOwnerUID", bindingOwnerRef.UID,
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "primaryPlacementResourceSnapshotOwnerUID", snapshotOwnerRef.UID)
	}
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
