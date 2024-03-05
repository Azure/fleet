/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package labels provides utils related to object labels.
package labels

import (
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ExtractResourceIndexFromClusterResourceSnapshot extracts the resource index from the label of a clusterResourceSnapshot.
func ExtractResourceIndexFromClusterResourceSnapshot(snapshot client.Object) (int, error) {
	return ExtractIndex(snapshot, fleetv1beta1.ResourceIndexLabel)
}

// ExtractResourceSnapshotIndexFromWork extracts the resource snapshot index from the work.
func ExtractResourceSnapshotIndexFromWork(work client.Object) (int, error) {
	return ExtractIndex(work, fleetv1beta1.ParentResourceSnapshotIndexLabel)
}

// ExtractIndex extracts the numeric index from the a label with labelKey.
func ExtractIndex(object client.Object, labelKey string) (int, error) {
	indexStr := object.GetLabels()[labelKey]
	v, err := strconv.Atoi(indexStr)
	if err != nil || v < 0 {
		return -1, fmt.Errorf("invalid resource index %q, error: %w", indexStr, err)
	}
	return v, nil
}
