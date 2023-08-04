/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package labels provides utils related to object labels.
package labels

import (
	"fmt"
	"strconv"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ExtractResourceIndexFromClusterResourceSnapshot extracts the resource index from the label of a clusterResourceSnapshot.
func ExtractResourceIndexFromClusterResourceSnapshot(snapshot *fleetv1beta1.ClusterResourceSnapshot) (int, error) {
	return validateResourceIndexStr(snapshot.Labels[fleetv1beta1.ResourceIndexLabel])
}

// ExtractResourceSnapshotIndexFromWork extracts the resource snapshot index from the work.
func ExtractResourceSnapshotIndexFromWork(work *workv1alpha1.Work) (int, error) {
	return validateResourceIndexStr(work.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel])
}

func validateResourceIndexStr(indexStr string) (int, error) {
	v, err := strconv.Atoi(indexStr)
	if err != nil || v < 0 {
		return -1, fmt.Errorf("invalid resource index %q, error: %w", indexStr, err)
	}
	return v, nil
}
