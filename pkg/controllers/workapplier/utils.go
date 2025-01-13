/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// formatWRIString returns a string representation of a work resource identifier.
func formatWRIString(wri *fleetv1beta1.WorkResourceIdentifier) (string, error) {
	switch {
	case wri.Group == "" && wri.Version == "":
		// The manifest object cannot be decoded, i.e., it can only be identified by its ordinal.
		//
		// This branch is added solely for completeness reasons; normally such objects would not
		// be included in any cases that would require a WRI string formatting.
		return "", fmt.Errorf("the manifest object can only be identified by its ordinal")
	default:
		// For a regular object, the string representation includes the actual name.
		return fmt.Sprintf("GV=%s/%s, Kind=%s, Namespace=%s, Name=%s",
			wri.Group, wri.Version, wri.Kind, wri.Namespace, wri.Name), nil
	}
}

// isManifestObjectApplied returns if an applied result type indicates that a manifest
// object in a bundle has been successfully applied.
func isManifestObjectApplied(appliedResTyp manifestProcessingAppliedResultType) bool {
	return appliedResTyp == ManifestProcessingApplyResultTypeApplied ||
		appliedResTyp == ManifestProcessingApplyResultTypeAppliedWithFailedDriftDetection
}

// isWorkObjectAvailable checks if a Work object is available.
func isWorkObjectAvailable(work *fleetv1beta1.Work) bool {
	availableCond := meta.FindStatusCondition(work.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
	return condition.IsConditionStatusTrue(availableCond, work.Generation)
}

// isPlacedByFleetInDuplicate checks if the object has already been placed by Fleet via another
// CRP.
func isPlacedByFleetInDuplicate(ownerRefs []metav1.OwnerReference, expectedAppliedWorkOwnerRef *metav1.OwnerReference) bool {
	for idx := range ownerRefs {
		ownerRef := ownerRefs[idx]
		if ownerRef.APIVersion == fleetv1beta1.GroupVersion.String() && ownerRef.Kind == fleetv1beta1.AppliedWorkKind && string(ownerRef.UID) != string(expectedAppliedWorkOwnerRef.UID) {
			return true
		}
	}
	return false
}
