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

package workapplier

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
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

// discardFieldsIrrelevantInComparisonFrom discards fields that are irrelevant when comparing
// the manifest and live objects (or two manifest objects).
//
// Note that this method will return an object copy; the original object will be left untouched.
func discardFieldsIrrelevantInComparisonFrom(obj *unstructured.Unstructured) *unstructured.Unstructured {
	// Create a deep copy of the object.
	objCopy := obj.DeepCopy()

	// Remove object meta fields that are irrelevant in comparison.

	// Clear out the object's name/namespace. For regular objects, the names/namespaces will
	// always be same between the manifest and the live objects, as guaranteed by the object
	// retrieval step earlier, so a comparison on these fields is unnecessary anyway.
	objCopy.SetName("")
	objCopy.SetNamespace("")

	// Clear out the object's generate name. This is a field that is irrelevant in comparison.
	objCopy.SetGenerateName("")

	// Remove certain labels and annotations.
	//
	// Fleet will remove labels/annotations that are reserved for Fleet own use cases, plus
	// well-known Kubernetes labels and annotations, as these cannot (should not) be set by users
	// directly.
	annotations := objCopy.GetAnnotations()
	cleanedAnnotations := map[string]string{}
	for k, v := range annotations {
		if strings.Contains(k, k8sReservedLabelAnnotationFullDomain) {
			// Skip Kubernetes reserved annotations.
			continue
		}

		if strings.Contains(k, k8sReservedLabelAnnotationAbbrDomain) {
			// Skip Kubernetes reserved annotations.
			continue
		}

		if strings.Contains(k, fleetReservedLabelAnnotationDomain) {
			// Skip Fleet reserved annotations.
			continue
		}
		cleanedAnnotations[k] = v
	}
	objCopy.SetAnnotations(cleanedAnnotations)

	labels := objCopy.GetLabels()
	cleanedLabels := map[string]string{}
	for k, v := range labels {
		if strings.Contains(k, k8sReservedLabelAnnotationFullDomain) {
			// Skip Kubernetes reserved labels.
			continue
		}

		if strings.Contains(k, k8sReservedLabelAnnotationAbbrDomain) {
			// Skip Kubernetes reserved labels.
			continue
		}

		if strings.Contains(k, fleetReservedLabelAnnotationDomain) {
			// Skip Fleet reserved labels.
			continue
		}
		cleanedLabels[k] = v
	}
	objCopy.SetLabels(cleanedLabels)

	// Fields below are system-reserved fields in object meta. Technically speaking they can be
	// set in the manifests, but this is a very uncommon practice, and currently Fleet will clear
	// these fields (except for the finalizers) before applying the manifests.
	// As a result, for now Fleet will ignore them in the comparison process as well.
	//
	// TO-DO (chenyu1): evaluate if this is a correct assumption for most (if not all) Fleet
	// users.
	objCopy.SetFinalizers([]string{})
	objCopy.SetManagedFields([]metav1.ManagedFieldsEntry{})
	objCopy.SetOwnerReferences([]metav1.OwnerReference{})

	// Fields below are read-only fields in object meta. Fleet will ignore them in the comparison
	// process.
	objCopy.SetCreationTimestamp(metav1.Time{})
	// Deleted objects are handled separately in the apply process; for comparison purposes,
	// Fleet will ignore the deletion timestamp and grace period seconds.
	objCopy.SetDeletionTimestamp(nil)
	objCopy.SetDeletionGracePeriodSeconds(nil)
	objCopy.SetGeneration(0)
	objCopy.SetResourceVersion("")
	objCopy.SetSelfLink("")
	objCopy.SetUID("")

	// Remove the status field.
	unstructured.RemoveNestedField(objCopy.Object, "status")

	return objCopy
}
