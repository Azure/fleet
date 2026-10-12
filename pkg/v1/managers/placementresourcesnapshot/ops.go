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
	"sort"
	"strconv"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	errors "go.goms.io/fleet/pkg/utils/errors"
	"go.goms.io/fleet/pkg/v1/utils/fieldindexers"
)

// SnapshotResourcesIfNoSnapshotExists creates a new placement resource snapshot only if no snapshots exist.
func (m *Manager) SnapshotResourcesIfNoSnapshotExists(ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
) ([]placementv1alpha1.PlacementResourceSnapshotAccessor, bool, error) {
	return m.snapshotResources(ctx, placementPolicy, true)
}

// SnapshotResourcesIfStale creates a new placement resource snapshot if the current latest snapshot has become stale.
func (m *Manager) SnapshotResourcesIfStale(ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
) ([]placementv1alpha1.PlacementResourceSnapshotAccessor, bool, error) {
	return m.snapshotResources(ctx, placementPolicy, false)
}

// snapshotResources checks for the latest placement resource snapshot(s) associated
// with a placement policy; it will then:
//
// a) create new placement resource snapshot(s) if none exists, or
// b) return the latest placement resource snapshot(s) if they exist and are up-to-date;
// c) create new placement resource snapshot(s) if the latest ones exist but have become stale.
//
// Set the createOnlyWhenMissing flag to true if one only needs to create a new snapshot when none exists.
func (m *Manager) snapshotResources(ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	createOnlyWhenMissing bool,
) ([]placementv1alpha1.PlacementResourceSnapshotAccessor, bool, error) {
	// Do a sanity check.
	if placementPolicy == nil {
		return nil, false, errors.NewUnexpectedError(nil, "placement policy accessor is nil", "manager", managerName)
	}

	// Acquire the mutex for the placement policy.
	m.acquireLock(placementPolicy)
	defer m.releaseLock(placementPolicy)

	// Retrieve the latest placement resource snapshot(s) associated with the placement policy.
	snapshots, err := m.retrieveLatestSnapshot(ctx, placementPolicy)
	if err != nil {
		return nil, false, errors.Wraps(err, "failed to retrieve the latest placement resource snapshot(s)")
	}

	// Retrieve the currently selected resources and their hash based on the placement policy.
	currentResources, currentHash, err := m.retrieveAndHashSelectedResources(ctx, placementPolicy)
	if err != nil {
		return nil, false, errors.Wraps(err, "failed to retrieve and hash the selected resources")
	}

	var latestPrimarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
	var isUpToDate bool
	if len(snapshots) > 0 {
		// A latest placement resource snapshot exists; check if it is up-to-date.
		latestPrimarySnapshot = snapshots[0]

		isUpToDate, err = m.isSnapshotUpToDate(ctx, placementPolicy, latestPrimarySnapshot, currentHash)
		if err != nil {
			return nil, false, errors.Wraps(err, "failed to check if the latest placement resource snapshot is up-to-date",
				"primaryPlacementResourceSnapshot", klog.KObj(latestPrimarySnapshot))
		}
	}

	switch {
	case createOnlyWhenMissing && len(snapshots) > 0:
		// A placement resource snapshot already exists, and the requester dictates that a new snapshot can only be created if none exists.
		// Return the retrieved snapshots and their freshness state.
		return snapshots, isUpToDate, nil
	case len(snapshots) == 0:
		// No placement resource snapshot exists; create one.
		createdSnapshots, err := m.createResourceSnapshotAnyway(ctx, placementPolicy, nil, currentResources, currentHash)
		if err != nil {
			return nil, false, errors.Wraps(err, "failed to create a placement resource snapshot", "manager", managerName)
		}
		return createdSnapshots, true, nil
	case isUpToDate:
		// The latest placement resource snapshot is up-to-date; return it as it is.
		return snapshots, isUpToDate, nil
	default:
		// The latest placement resource snapshot exists but has become stale; create a new one.
		createdSnapshots, err := m.createResourceSnapshotAnyway(ctx, placementPolicy, latestPrimarySnapshot, currentResources, currentHash)
		if err != nil {
			return nil, false, errors.Wraps(err, "failed to create a placement resource snapshot", "manager", managerName)
		}
		return createdSnapshots, true, nil
	}
}

// retrieveLatestSnapshot retrieves the latest placement resource snapshot(s) associated with a placement policy.
//
// If there are multiple placement resource snapshots with the same index, they will be returned in the ascending order
// of their sub-indices.
//
// Note that this method assumes that the corresponding mutex for the placement policy has been acquired before
// calling this method.
func (m *Manager) retrieveLatestSnapshot(ctx context.Context, placementPolicy placementv1alpha1.PlacementPolicyAccessor) (
	placementResourceSnapshot []placementv1alpha1.PlacementResourceSnapshotAccessor, err error) {
	// Retrieve the primary resource snapshots associated with the placement policy.
	var snapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
	fieldMatchers := client.MatchingFields{
		fieldindexers.PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldName: fmt.Sprintf(fieldindexers.PlacementResourceSnapshotOwnedByAndSubIndexedCustomFieldValFmt, placementPolicyOwnerLabelVal(placementPolicy), "0"),
	}
	if placementPolicy.GetNamespace() == "" {
		// The placement policy is cluster-scoped; list cluster placement resource snapshots.
		placementResourceSnapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := m.hubClient.List(ctx, placementResourceSnapshotList, fieldMatchers); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list cluster placement resource snapshots", true)
		}
		snapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(placementResourceSnapshotList.Items))
		for i := range placementResourceSnapshotList.Items {
			snapshots[i] = &placementResourceSnapshotList.Items[i]
		}
	} else {
		// The placement policy is namespace-scoped; list placement resource snapshots in the same namespace.
		placementResourceSnapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
		if err := m.hubClient.List(ctx, placementResourceSnapshotList,
			client.InNamespace(placementPolicy.GetNamespace()), fieldMatchers); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list placement resource snapshots", true)
		}
		snapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(placementResourceSnapshotList.Items))
		for i := range placementResourceSnapshotList.Items {
			snapshots[i] = &placementResourceSnapshotList.Items[i]
		}
	}

	if len(snapshots) == 0 {
		// No placement resource snapshot exists for the placement policy.
		return nil, nil
	}

	// Sort the primary snapshots by their indices.
	var sortErrs []error
	sort.Slice(snapshots, func(i, j int) bool {
		indexIStr := snapshots[i].GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
		indexJStr := snapshots[j].GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
		indexI, iErr := strconv.Atoi(indexIStr)
		indexJ, jErr := strconv.Atoi(indexJStr)
		if iErr != nil {
			sortErrs = append(sortErrs, fmt.Errorf("failed to convert index label to integer: %w (placementResourceSnapshot: %v)",
				iErr, klog.KObj(snapshots[i])))
			return false
		}
		if jErr != nil {
			sortErrs = append(sortErrs, fmt.Errorf("failed to convert index label to integer: %w (placementResourceSnapshot: %v)",
				jErr, klog.KObj(snapshots[j])))
			return false
		}
		return indexI < indexJ
	})
	if len(sortErrs) > 0 {
		return nil, errors.NewUnexpectedError(nil, "failed to sort primary placement resource snapshots", "errs", sortErrs)
	}

	latestPrimarySnapshot := snapshots[len(snapshots)-1]
	// Check if there are snapshots with the same index.
	subIndexedSnapshotCntStr := latestPrimarySnapshot.GetLabels()[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey]
	if subIndexedSnapshotCntStr == "1" {
		// The primary placement resource snapshot is the only snapshot with the latest index; return it.
		return []placementv1alpha1.PlacementResourceSnapshotAccessor{latestPrimarySnapshot}, nil
	}
	subIndexedSnapshotCnt, err := strconv.Atoi(subIndexedSnapshotCntStr)
	if err != nil {
		return nil, errors.NewUnexpectedError(err, "failed to convert sub-indexed placement resource snapshot count label to integer",
			"placementResourceSnapshot", klog.KObj(latestPrimarySnapshot))
	}

	if subIndexedSnapshotCnt < 1 {
		// Do a sanity check.
		return nil, errors.NewUnexpectedError(nil, "sub-indexed placement resource snapshot count label is less than 1",
			"placementResourceSnapshot", klog.KObj(latestPrimarySnapshot), "subIndexedSnapshotCount", subIndexedSnapshotCnt)
	}

	// There are sub-indexed placement resource snapshots with the same index; retrieve them.
	latestIndex := latestPrimarySnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
	fieldMatchers = client.MatchingFields{
		fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName: fmt.Sprintf(fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt, placementPolicyOwnerLabelVal(placementPolicy), latestIndex),
	}
	var subIndexedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
	if placementPolicy.GetNamespace() == "" {
		// The placement policy is cluster-scoped; list cluster placement resource snapshots.
		placementResourceSnapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := m.hubClient.List(ctx, placementResourceSnapshotList, fieldMatchers); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list cluster placement resource snapshots", true)
		}
		subIndexedSnapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(placementResourceSnapshotList.Items))
		for i := range placementResourceSnapshotList.Items {
			subIndexedSnapshots[i] = &placementResourceSnapshotList.Items[i]
		}
	} else {
		// The placement policy is namespace-scoped; list placement resource snapshots in the same namespace.
		placementResourceSnapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
		if err := m.hubClient.List(ctx, placementResourceSnapshotList,
			client.InNamespace(placementPolicy.GetNamespace()), fieldMatchers); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list placement resource snapshots", true)
		}
		subIndexedSnapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(placementResourceSnapshotList.Items))
		for i := range placementResourceSnapshotList.Items {
			subIndexedSnapshots[i] = &placementResourceSnapshotList.Items[i]
		}
	}
	// Sort the sub-indexed snapshots by their sub-indices.
	sortErrs = nil
	sort.Slice(subIndexedSnapshots, func(i, j int) bool {
		subIndexIStr := subIndexedSnapshots[i].GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
		subIndexJStr := subIndexedSnapshots[j].GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
		subIndexI, iErr := strconv.Atoi(subIndexIStr)
		subIndexJ, jErr := strconv.Atoi(subIndexJStr)
		if iErr != nil {
			sortErrs = append(sortErrs, fmt.Errorf("failed to convert sub-index label to integer: %w (placementResourceSnapshot: %v)",
				iErr, klog.KObj(subIndexedSnapshots[i])))
			return false
		}
		if jErr != nil {
			sortErrs = append(sortErrs, fmt.Errorf("failed to convert sub-index label to integer: %w (placementResourceSnapshot: %v)",
				jErr, klog.KObj(subIndexedSnapshots[j])))
			return false
		}
		return subIndexI < subIndexJ
	})
	if len(sortErrs) > 0 {
		return nil, errors.NewUnexpectedError(nil, "failed to sort sub-indexed placement resource snapshots", "errs", sortErrs)
	}

	// Verify that there are enough sub-indexed placement resource snapshots as dictated by the count label.
	if len(subIndexedSnapshots) < subIndexedSnapshotCnt {
		// Normally this would never happen, as the manager creates secondary placement resource snapshots first
		// before creating the primary placement resource snapshot with the count label.
		return nil, errors.NewUnexpectedError(nil, "there are fewer sub-indexed placement resource snapshots than the count label indicates",
			"expectedCount", subIndexedSnapshotCnt, "actualCount", len(subIndexedSnapshots))
	}

	// As there is no way to create multiple placement resource snapshots with the same index in a transactional
	// manner, there exists a corner case where the manager, when going through several snapshot creation passes,
	// created more placement resource snapshots than the count label indicates. The extra snapshots are orphans from
	// resource changes that have been overwritten.
	//
	// This is not registered as an error, and here the manager returns only the number of placement resource snapshots
	// dictated by the count label, which is guaranteed to be consistent. The orphaned snapshots will eventually
	// be cleaned up.
	if len(subIndexedSnapshots) > subIndexedSnapshotCnt {
		// There are more snapshots than expected; log a warning and only return the ones dictated by the count.
		klog.Warningf("found more sub-indexed placement resource snapshots (%d) than the count label indicates (%d) for placement policy %v; only returning the first %d",
			len(subIndexedSnapshots), subIndexedSnapshotCnt, klog.KObj(placementPolicy), subIndexedSnapshotCnt)
	}

	return subIndexedSnapshots[:subIndexedSnapshotCnt], nil
}

// isSnapshotUpToDate checks if the given placement resource snapshot is up-to-date, i.e., the snapshot is
// consistent with the current state of the resources as selected by the placement policy.
//
// Note that this method assumes that the corresponding mutex for the placement policy has been acquired before
// calling this method.
func (m *Manager) isSnapshotUpToDate(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	primaryPlacementResourceSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
	currentHash string,
) (bool, error) {
	// Get the contents hash annotation from the given primary placement resource snapshot.
	snapshotHash := primaryPlacementResourceSnapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]

	if snapshotHash != currentHash {
		// The hashes do not match; the placement resource snapshot is not up-to-date.
		//
		// Note that due to the check being carried out using a cached client, false negatives are possible, i.e.,
		// a newer snapshot with matching hash might have been created, yet the cache has not been updated yet.
		// However, this is considered OK as any attempt to create a new snapshot based on the false negative
		// will lead to a failure (`AlreadyExists` error). Eventually the cache will catch up, and consistency
		// will be restored.
		return false, nil
	}

	// The hashes do match.
	//
	// Note that this check is being carried out using a cached client, and false positives can occur in the
	// situation where the user does an A -> B -> A type of resource change; in this scenario the false positive
	// might lead to side effects, e.g., empty rollouts and inconsistent status reporting. Here KubeFleet does a
	// quorum read to verify that the snapshot is indeed up-to-date.

	// Compute the index of the snapshot that would be created next.
	currentIdxStr := primaryPlacementResourceSnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
	currentIdx, err := strconv.Atoi(currentIdxStr)
	if err != nil {
		return false, errors.NewUnexpectedError(err, "failed to convert index label to integer")
	}
	nextIdx := currentIdx + 1

	found, err := m.primaryPlacementResourceSnapshotExistsAtIdx(ctx, placementPolicy, nextIdx)
	if err != nil {
		return false, err
	}
	if found {
		// A primary placement resource snapshot already exists at the given index; the currently observed primary
		// resource snapshot is not up-to-date.
		return false, errors.NewTransientError(nil, "a newer snapshot already exists (found via quorum reads); the client cache might be stale",
			"primaryPlacementResourceSnapshotName", uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), nextIdx),
			"snapshotIndex", nextIdx)
	}

	// No newer snapshot exists at the next index.
	return true, nil
}

// createResourceSnapshotAnyway creates a new placement resource snapshot for the given placement policy.
//
// Snapshot creation spans multiple objects (secondaries then the primary) and is not transactional; it relies on
// the mutex plus the hub controller manager's leader election for serialization. Because the listing/cleanup steps
// read from a cached client, a stale cache can lead to `AlreadyExists` errors on create, or to a quorum read finding
// a primary snapshot that the cache has yet to observe. These are expected and surfaced to the caller so that it
// requeues; each retry re-runs the orphan cleanup from a clean slate, and the operation converges once the cache
// catches up.
//
// Note that this method assumes that the corresponding mutex for the placement policy has been acquired before
// calling this method.
func (m *Manager) createResourceSnapshotAnyway(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	latestPrimaryPlacementResourceSnapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
	currentResources []placementv1alpha1.SnapshottedResource,
	currentHash string,
) ([]placementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	// Compute the index of the snapshot that would be created next.
	nextSnapshotIdx := 0
	if latestPrimaryPlacementResourceSnapshot != nil {
		lastSeenSnapshotIdxStr := latestPrimaryPlacementResourceSnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
		lastSeenSnapshotIdx, err := strconv.Atoi(lastSeenSnapshotIdxStr)
		if err != nil {
			return nil, errors.NewUnexpectedError(err,
				"failed to convert last seen primary placement resource snapshot index label to integer")
		}
		nextSnapshotIdx = lastSeenSnapshotIdx + 1
	}

	// Clean up orphaned secondary placement resource snapshots (if any).
	//
	// Due to the inability to create multiple placement resource snapshots with the same index in a
	// transactional manner, it is possible that the manager has already created a few secondary placement
	// resource snapshot in a previous pass. In this case, the manager should delete the existing snapshots
	// (its content might be outdated, and the object spec is immutable anyway) before creating new ones.
	acted, err := m.cleanUpOrphanedSecondarySnapshots(ctx, placementPolicy, nextSnapshotIdx)
	if err != nil {
		return nil, errors.Wraps(err, "failed to clean up orphaned secondary placement resource snapshots")
	}
	if acted {
		// Ask the caller to requeue when there are orphaned secondary placement resource snapshots to be cleaned up.
		// This helps avoid oscillation issues where a not fully deleted snapshot blocks later creation.
		return nil, errors.NewTransientError(nil, "cleaned up orphaned secondary placement resource snapshots; requeue before creating new snapshots", "snapshotIndex", nextSnapshotIdx)
	}

	// Split the resources into size-controlled groups. Each group corresponds to a placement resource snapshot
	// that will be created.
	resGroups, err := splitResourcesIntoSizeControlledGroups(currentResources)
	if err != nil {
		return nil, errors.Wraps(err, "failed to split the selected resources into size-controlled groups")
	}

	// createdSnapshots holds the created snapshots for the new index, keyed by their sub-indices.
	createdSnapshots := make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(resGroups))

	// Note (chenyu1): evaluate if parallelization is needed here. In most cases the number of secondary
	// placement resource snapshots is small, so the overhead of parallelization might not be worth it.
	if len(resGroups) > 1 {
		// Create the secondary placement resource snapshots first. Start with the last resource group and work
		// backwards, so that the primary snapshot (which carries the count label) is created last.
		for subIdx := len(resGroups) - 1; subIdx >= 1; subIdx-- {
			secondaryName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.GetName(), nextSnapshotIdx, subIdx)
			secondarySnapshot, err := secondaryPlacementResourceSnapshot(
				placementPolicy.GetNamespace(), secondaryName, placementPolicy, nextSnapshotIdx, subIdx, resGroups[subIdx], currentHash, m.hubClient.Scheme())
			if err != nil {
				return nil, errors.Wraps(err, "failed to build a secondary placement resource snapshot",
					"secondaryPlacementResourceSnapshotName", secondaryName,
					"snapshotIndex", nextSnapshotIdx, "snapshotSubIndex", subIdx)
			}

			if err := m.hubClient.Create(ctx, secondarySnapshot); err != nil {
				return nil, errors.NewAPIServerError(err, "failed to create a secondary placement resource snapshot", false,
					"secondaryPlacementResourceSnapshot", klog.KObj(secondarySnapshot),
					"snapshotIndex", nextSnapshotIdx, "snapshotSubIndex", subIdx)
			}

			createdSnapshots[subIdx] = secondarySnapshot
		}
	}

	// Create the primary placement resource snapshot last, with the count label.
	primaryName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), nextSnapshotIdx)
	primarySnapshot, err := primaryPlacementResourceSnapshot(
		placementPolicy.GetNamespace(), primaryName, placementPolicy, nextSnapshotIdx, resGroups[0], currentHash, len(resGroups), m.hubClient.Scheme())
	if err != nil {
		return nil, errors.Wraps(err, "failed to build the primary placement resource snapshot",
			"primaryPlacementResourceSnapshotName", primaryName, "snapshotIndex", nextSnapshotIdx)
	}

	if err := m.hubClient.Create(ctx, primarySnapshot); err != nil {
		// Note that if the primary placement resource snapshot already exists, no deletion will be attempted. The
		// caller must retry and create the next placement resource snapshot with a new index.
		return nil, errors.NewAPIServerError(err, "failed to create the primary placement resource snapshot", false,
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "snapshotIndex", nextSnapshotIdx)
	}

	createdSnapshots[0] = primarySnapshot
	return createdSnapshots, nil
}

// cleanUpOrphanedSecondarySnapshots deletes all secondary placement resource snapshots at the given index.
//
// Note that this method assumes that the corresponding mutex for the placement policy has been acquired before
// calling this method.
func (m *Manager) cleanUpOrphanedSecondarySnapshots(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	nextSnapshotIdx int,
) (bool, error) {
	// List all placement resource snapshots at the given index.
	fieldMatchers := client.MatchingFields{
		fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldName: fmt.Sprintf(fieldindexers.PlacementResourceSnapshotOwnedByAndIndexedCustomFieldValFmt, placementPolicyOwnerLabelVal(placementPolicy), strconv.Itoa(nextSnapshotIdx)),
	}

	var snapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
	if placementPolicy.GetNamespace() == "" {
		// The placement policy is cluster-scoped; list cluster placement resource snapshots.
		placementResourceSnapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := m.hubClient.List(ctx, placementResourceSnapshotList, fieldMatchers); err != nil {
			return false, errors.NewAPIServerError(err, "failed to list cluster placement resource snapshots", true)
		}
		snapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(placementResourceSnapshotList.Items))
		for i := range placementResourceSnapshotList.Items {
			snapshots[i] = &placementResourceSnapshotList.Items[i]
		}
	} else {
		// The placement policy is namespace-scoped; list placement resource snapshots in the same namespace.
		placementResourceSnapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
		if err := m.hubClient.List(ctx, placementResourceSnapshotList,
			client.InNamespace(placementPolicy.GetNamespace()), fieldMatchers); err != nil {
			return false, errors.NewAPIServerError(err, "failed to list placement resource snapshots", true)
		}
		snapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(placementResourceSnapshotList.Items))
		for i := range placementResourceSnapshotList.Items {
			snapshots[i] = &placementResourceSnapshotList.Items[i]
		}
	}

	if len(snapshots) == 0 {
		// No placement resource snapshots are found at the index.
		return false, nil
	}

	// Do a sanity check; verify that there is no primary placement resource snapshot at the given index.
	for idx := range snapshots {
		snapshot := snapshots[idx]
		subIdxStr := snapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey]
		if subIdxStr == "0" {
			// This normally should never occur.
			return false, errors.NewUnexpectedError(nil,
				"found a primary placement resource snapshot at the given index while cleaning up orphaned secondary snapshots",
				"primaryPlacementResourceSnapshot", klog.KObj(snapshot))
		}
	}

	// There exists a corner case, where, due to the staleness of cache, a primary placement resource snapshot has been created
	// at the given (next) index yet has not been registered in the cache. Do a quorum read to confirm this.
	found, err := m.primaryPlacementResourceSnapshotExistsAtIdx(ctx, placementPolicy, nextSnapshotIdx)
	if err != nil {
		return false, errors.Wraps(err, "failed to perform a quorum read for the primary placement resource snapshot at the given index",
			"snapshotIdx", nextSnapshotIdx)
	}
	if found {
		// A primary placement resource snapshot already exists at the given index; the secondary snapshots found
		// here are not orphans. Report this as an error; the caller should requeue and wait for the cache to catch up.
		return false, errors.NewTransientError(nil, "a primary placement resource snapshot already exists at the given index (found via quorum read); the client cache might be stale",
			"primaryPlacementResourceSnapshotName", uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), nextSnapshotIdx),
			"snapshotIndex", nextSnapshotIdx)
	}

	// Delete all the secondary placement resource snapshots at the given index.
	for idx := range snapshots {
		snapshot := snapshots[idx]
		if !snapshot.GetDeletionTimestamp().IsZero() {
			// The secondary placement resource snapshot has been marked for deletion; wait for it to complete.
			continue
		}

		if err := m.hubClient.Delete(ctx, snapshot); err != nil {
			return false, errors.NewAPIServerError(err, "failed to delete an orphaned secondary placement resource snapshot",
				false, "secondaryPlacementResourceSnapshot", klog.KObj(snapshot))
		}
	}
	return true, nil
}
