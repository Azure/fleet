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
	"time"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// ExtractNumOfClustersFromPolicySnapshot extracts the numOfClusters value from the annotations
// on a policy snapshot.
func ExtractNumOfClustersFromPolicySnapshot(policy fleetv1beta1.PolicySnapshotObj) (int, error) {
	numOfClustersStr, ok := policy.GetAnnotations()[fleetv1beta1.NumberOfClustersAnnotation]
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

// ExtractSubindexFromResourceSnapshot is a helper function to extract subindex from ResourceSnapshot objects.
func ExtractSubindexFromResourceSnapshot(snapshot fleetv1beta1.ResourceSnapshotObj) (doesExist bool, subindex int, err error) {
	annotations := snapshot.GetAnnotations()
	if annotations == nil {
		return false, 0, nil
	}

	subindexStr, exists := annotations[fleetv1beta1.SubindexOfResourceSnapshotAnnotation]
	if !exists || subindexStr == "" {
		return false, 0, nil
	}

	subindex, err = strconv.Atoi(subindexStr)
	if err != nil {
		return false, 0, fmt.Errorf("invalid subindex annotation value %q: %w", subindexStr, err)
	}

	if subindex < 0 {
		return false, 0, fmt.Errorf("subindex cannot be negative: %d", subindex)
	}

	return true, subindex, nil
}

// ExtractObservedPlacementGenerationFromPolicySnapshot extracts the observed placement generation from policySnapshot.
func ExtractObservedPlacementGenerationFromPolicySnapshot(policy fleetv1beta1.PolicySnapshotObj) (int64, error) {
	placementGenerationStr, ok := policy.GetAnnotations()[fleetv1beta1.CRPGenerationAnnotation]
	if !ok {
		return 0, fmt.Errorf("cannot find annotation %s", fleetv1beta1.CRPGenerationAnnotation)
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	observedplacementGeneration, err := strconv.Atoi(placementGenerationStr)
	if err != nil || observedplacementGeneration < 0 {
		return 0, fmt.Errorf("invalid annotation %s: %s is not a valid value: %w", fleetv1beta1.CRPGenerationAnnotation, placementGenerationStr, err)
	}

	return int64(observedplacementGeneration), nil
}

// ExtractNumberOfResourceSnapshotsFromResourceSnapshot extracts the number of resourceSnapshots in a group from the master resourceSnapshot.
func ExtractNumberOfResourceSnapshotsFromResourceSnapshot(snapshot fleetv1beta1.ResourceSnapshotObj) (int, error) {
	countAnnotation := snapshot.GetAnnotations()[fleetv1beta1.NumberOfResourceSnapshotsAnnotation]
	snapshotCount, err := strconv.Atoi(countAnnotation)
	if err != nil || snapshotCount < 1 {
		return 0, fmt.Errorf(
			"resource snapshot %s has an invalid snapshot count %d or err %w", snapshot.GetName(), snapshotCount, err)
	}
	return snapshotCount, nil
}

// ExtractNumberOfEnvelopeObjFromResourceSnapshot extracts the number of envelope object in a group from the master resourceSnapshot.
func ExtractNumberOfEnvelopeObjFromResourceSnapshot(snapshot fleetv1beta1.ResourceSnapshotObj) (int, error) {
	countAnnotation, exist := snapshot.GetAnnotations()[fleetv1beta1.NumberOfEnvelopedObjectsAnnotation]
	// doesn't exist means no enveloped object in the snapshot group
	if !exist {
		return 0, nil
	}
	envelopeObjCount, err := strconv.Atoi(countAnnotation)
	if err != nil || envelopeObjCount < 0 {
		return 0, fmt.Errorf(
			"resource snapshot %s has an invalid enveloped object count %d or err %w", snapshot.GetName(), envelopeObjCount, err)
	}
	return envelopeObjCount, nil
}

// ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot extracts the next resource snapshot candidate detection time from the annotations of a resourceSnapshot.
// If the annotation does not exist, it returns 0 duration.
func ExtractNextResourceSnapshotCandidateDetectionTimeFromResourceSnapshot(snapshot fleetv1beta1.ResourceSnapshotObj) (time.Time, error) {
	nextDetectionTimeStr, ok := snapshot.GetAnnotations()[fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation]
	if !ok {
		return time.Time{}, nil
	}
	nextDetectionTime, err := time.Parse(time.RFC3339, nextDetectionTimeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid annotation %s: %s is not a valid RFC3339 time: %w", fleetv1beta1.NextResourceSnapshotCandidateDetectionTimeAnnotation, nextDetectionTimeStr, err)
	}
	return nextDetectionTime, nil
}

// ParseResourceGroupHashFromAnnotation extracts the resource group hash from a ResourceSnapshot annotation.
func ParseResourceGroupHashFromAnnotation(resourceSnapshot fleetv1beta1.ResourceSnapshotObj) (string, error) {
	annotations := resourceSnapshot.GetAnnotations()
	if annotations == nil {
		return "", fmt.Errorf("ResourceGroupHashAnnotation is not set")
	}

	v, ok := annotations[fleetv1beta1.ResourceGroupHashAnnotation]
	if !ok {
		return "", fmt.Errorf("ResourceGroupHashAnnotation is not set")
	}
	return v, nil
}
