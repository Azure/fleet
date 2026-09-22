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

package workgenerator

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/errors"
)

func (r *Reconciler) retrievePlacementBinding(ctx context.Context, namespacedName types.NamespacedName) (placementv1alpha1.PlacementBindingAccessor, error) {
	var placementBinding placementv1alpha1.PlacementBindingAccessor
	if namespacedName.Namespace == "" {
		// The placement binding is cluster-scoped.
		placementBinding = &placementv1alpha1.ClusterPlacementBinding{}
	} else {
		// The placement binding is namespace-scoped.
		placementBinding = &placementv1alpha1.PlacementBinding{}
	}

	if err := r.hubClient.Get(ctx, namespacedName, placementBinding); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to retrieve placement binding", true)
	}
	return placementBinding, nil
}

// listWorksByOwnerBinding lists the Work objects owned by a placement binding within a Fleet member cluster reserved
// namespace.
func (r *Reconciler) listWorksByOwnerBinding(ctx context.Context, clusterName, ownerBindingNSName, ownerBindingName string) ([]placementv1alpha1.Work, error) {
	memberClusterNamespace := fmt.Sprintf(utils.NamespaceNameFormat, clusterName)

	workList := &placementv1alpha1.WorkList{}
	listOptions := []client.ListOption{
		client.InNamespace(memberClusterNamespace),
		client.MatchingLabels{
			placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: workOwnerLabelValue(ownerBindingName),
			placementv1alpha1.WorkOwnerNamespaceLabelKey:          ownerBindingNSName,
		},
	}
	if err := r.hubClient.List(ctx, workList, listOptions...); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to list work objects", true)
	}
	return workList.Items, nil
}

// retrievePrimaryAndSecondaryPlacementResourceSnapshots retrieves the primary placement resource snapshot referenced
// by the placement binding, along with any secondary snapshots that share the same index. The returned snapshots are
// sorted in ascending order of their sub-indices (the primary, sub-index 0, comes first).
func (r *Reconciler) retrievePrimaryAndSecondaryPlacementResourceSnapshots(
	ctx context.Context,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
) ([]placementv1alpha1.PlacementResourceSnapshotAccessor, error) {
	namespace := placementBinding.GetNamespace()
	primarySnapshotName := placementBinding.GetSpec().ResourceSnapshotName

	// Retrieve the primary placement resource snapshot referenced by the binding.
	var primarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor
	if namespace == "" {
		// The placement binding is cluster-scoped.
		primarySnapshot = &placementv1alpha1.ClusterPlacementResourceSnapshot{}
	} else {
		// The placement binding is namespace-scoped.
		primarySnapshot = &placementv1alpha1.PlacementResourceSnapshot{}
	}
	if err := r.hubClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: primarySnapshotName}, primarySnapshot); err != nil {
		return nil, errors.NewAPIServerError(err, "failed to retrieve the primary placement resource snapshot", true,
			"primaryPlacementResourceSnapshotName", primarySnapshotName)
	}

	// Determine how many snapshots share the same index via the count label on the primary snapshot.
	countStr := primarySnapshot.GetLabels()[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey]
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 {
		return nil, errors.NewUnexpectedError(err, "invalid sub-indexed placement resource snapshot count label on the primary placement resource snapshot",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "countVal", countStr)
	}
	if count == 1 {
		// The primary snapshot is the only snapshot associated with this index.
		return []placementv1alpha1.PlacementResourceSnapshotAccessor{primarySnapshot}, nil
	}

	// There are secondary snapshots; list all snapshots that share the same owner and index.
	ownedBy := primarySnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey]
	index := primarySnapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotIndexLabelKey]
	if ownedBy == "" || index == "" {
		return nil, errors.NewUnexpectedError(nil, "the primary placement resource snapshot is missing required labels",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot))
	}
	labelMatchers := client.MatchingLabels{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey: ownedBy,
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:   index,
	}

	var snapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
	if namespace == "" {
		snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := r.hubClient.List(ctx, snapshotList, labelMatchers); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list cluster placement resource snapshots", true)
		}
		snapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotList.Items))
		for i := range snapshotList.Items {
			snapshots[i] = &snapshotList.Items[i]
		}
	} else {
		snapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
		if err := r.hubClient.List(ctx, snapshotList, client.InNamespace(namespace), labelMatchers); err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list placement resource snapshots", true)
		}
		snapshots = make([]placementv1alpha1.PlacementResourceSnapshotAccessor, len(snapshotList.Items))
		for i := range snapshotList.Items {
			snapshots[i] = &snapshotList.Items[i]
		}
	}

	// Sort the snapshots by their sub-indices in ascending order.
	var sortErrs []error
	sort.Slice(snapshots, func(i, j int) bool {
		subIdxI, iErr := strconv.Atoi(snapshots[i].GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey])
		subIdxJ, jErr := strconv.Atoi(snapshots[j].GetLabels()[placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey])
		if iErr != nil {
			sortErrs = append(sortErrs, fmt.Errorf("failed to convert sub-index label to integer: %w (placementResourceSnapshot: %s)", iErr, snapshots[i].GetName()))
			return false
		}
		if jErr != nil {
			sortErrs = append(sortErrs, fmt.Errorf("failed to convert sub-index label to integer: %w (placementResourceSnapshot: %s)", jErr, snapshots[j].GetName()))
			return false
		}
		return subIdxI < subIdxJ
	})
	if len(sortErrs) > 0 {
		return nil, errors.NewUnexpectedError(nil, "failed to sort placement resource snapshots by sub-index", "errs", sortErrs)
	}

	// Do some sanity checks; verify that all snapshots dictated by the count label are present and they have
	// the same snapshotted resource hash.

	if len(snapshots) < count {
		// Normally this branch will never run, as the placement resource snapshot manager creates secondary
		// snapshots first, then the primary snapshot with the count label.
		return nil, errors.NewUnexpectedError(nil, "there are fewer placement resource snapshots than the count label indicates",
			"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot), "expectedCount", count, "actualCount", len(snapshots))
	}

	primarySnapshottedResHash := primarySnapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
	for i := range snapshots[:count] {
		snapshottedResHash := snapshots[i].GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
		if snapshottedResHash != primarySnapshottedResHash {
			// Normally this branch will never run, as the placement resource snapshot manager uses ordered creation
			// to make sure that hashes are consistent across all snapshots with the same index.
			return nil, errors.NewUnexpectedError(nil, "the contents hash of a placement resource snapshot does not match the primary snapshot",
				"primaryPlacementResourceSnapshot", klog.KObj(primarySnapshot),
				"hashMismatchedPlacementResourceSnapshot", klog.KObj(snapshots[i]),
				"hashOnPrimaryPlacementResourceSnapshot", primarySnapshottedResHash,
				"mismatchedHash", snapshottedResHash)
		}
	}

	// Any snapshots beyond the count are orphans from an overwritten resource change; return only the ones
	// dictated by the count label, which are guaranteed to be consistent.
	return snapshots[:count], nil
}
