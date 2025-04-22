/*
Copyright 2025 The KubeFleet Authors.

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

// Package annotations provides the utils related to object annotations.
package annotations

import (
	"fmt"
	"strconv"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// ExtractNumOfClustersFromPolicySnapshot extracts the numOfClusters value from the annotations
// on a policy snapshot.
func ExtractNumOfClustersFromPolicySnapshot(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (int, error) {
	numOfClustersStr, ok := policy.Annotations[fleetv1beta1.NumberOfClustersAnnotation]
	if !ok {
		return 0, fmt.Errorf("cannot find annotation %s", fleetv1beta1.NumberOfClustersAnnotation)
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	numOfClusters, err := strconv.Atoi(numOfClustersStr)
	if err != nil || numOfClusters < 0 {
		return 0, fmt.Errorf("invalid annotation %s: %s is not a valid count: %w", fleetv1beta1.NumberOfClustersAnnotation, numOfClustersStr, err)
	}

	return numOfClusters, nil
}

// ExtractSubindexFromClusterResourceSnapshot extracts the subindex value from the annotations of a clusterResourceSnapshot.
func ExtractSubindexFromClusterResourceSnapshot(snapshot *fleetv1beta1.ClusterResourceSnapshot) (doesExist bool, subindex int, err error) {
	subindexStr, ok := snapshot.Annotations[fleetv1beta1.SubindexOfResourceSnapshotAnnotation]
	if !ok {
		return false, -1, nil
	}
	subindex, err = strconv.Atoi(subindexStr)
	if err != nil || subindex < 0 {
		return true, -1, fmt.Errorf("invalid annotation %s: %s is invalid: %w", fleetv1beta1.SubindexOfResourceSnapshotAnnotation, subindexStr, err)
	}

	return true, subindex, nil
}

// ExtractObservedCRPGenerationFromPolicySnapshot extracts the observed CRP generation from policySnapshot.
func ExtractObservedCRPGenerationFromPolicySnapshot(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (int64, error) {
	crpGenerationStr, ok := policy.Annotations[fleetv1beta1.CRPGenerationAnnotation]
	if !ok {
		return 0, fmt.Errorf("cannot find annotation %s", fleetv1beta1.CRPGenerationAnnotation)
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	observedCRPGeneration, err := strconv.Atoi(crpGenerationStr)
	if err != nil || observedCRPGeneration < 0 {
		return 0, fmt.Errorf("invalid annotation %s: %s is not a valid value: %w", fleetv1beta1.CRPGenerationAnnotation, crpGenerationStr, err)
	}

	return int64(observedCRPGeneration), nil
}

// ExtractNumberOfResourceSnapshotsFromResourceSnapshot extracts the number of clusterResourceSnapshots in a group from the master clusterResourceSnapshot.
func ExtractNumberOfResourceSnapshotsFromResourceSnapshot(snapshot *fleetv1beta1.ClusterResourceSnapshot) (int, error) {
	countAnnotation := snapshot.Annotations[fleetv1beta1.NumberOfResourceSnapshotsAnnotation]
	snapshotCount, err := strconv.Atoi(countAnnotation)
	if err != nil || snapshotCount < 1 {
		return 0, fmt.Errorf(
			"resource snapshot %s has an invalid snapshot count %d or err %w", snapshot.Name, snapshotCount, err)
	}
	return snapshotCount, nil
}

// ExtractNumberOfEnvelopeObjFromResourceSnapshot extracts the number of envelope object in a group from the master clusterResourceSnapshot.
func ExtractNumberOfEnvelopeObjFromResourceSnapshot(snapshot *fleetv1beta1.ClusterResourceSnapshot) (int, error) {
	countAnnotation, exist := snapshot.Annotations[fleetv1beta1.NumberOfEnvelopedObjectsAnnotation]
	// doesn't exist means no enveloped object in the snapshot group
	if !exist {
		return 0, nil
	}
	envelopeObjCount, err := strconv.Atoi(countAnnotation)
	if err != nil || envelopeObjCount < 0 {
		return 0, fmt.Errorf(
			"resource snapshot %s has an invalid enveloped object count %d or err %w", snapshot.Name, envelopeObjCount, err)
	}
	return envelopeObjCount, nil
}
