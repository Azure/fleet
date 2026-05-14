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

package workgenerator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

// createOrUpdateEnvelopeCRWorkObj creates or updates a work object for a given envelope CR.
func (r *Reconciler) createOrUpdateEnvelopeCRWorkObj(
	ctx context.Context,
	envelopeReader fleetv1beta1.EnvelopeReader,
	workNamePrefix string,
	binding fleetv1beta1.BindingObj,
	resourceSnapshot fleetv1beta1.ResourceSnapshotObj,
	resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string,
) (*fleetv1beta1.Work, error) {
	manifests, err := extractManifestsFromEnvelopeCR(envelopeReader)
	if err != nil {
		klog.ErrorS(err, "Failed to extract manifests from the envelope spec",
			"resourceBinding", klog.KObj(binding),
			"resourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		return nil, err
	}
	klog.V(2).InfoS("Successfully extracted wrapped manifests from the envelope",
		"numOfResources", len(manifests),
		"resourceBinding", klog.KObj(binding),
		"resourceSnapshot", klog.KObj(resourceSnapshot),
		"envelope", envelopeReader.GetEnvelopeObjRef())

	// Check to see if a corresponding work object has been created for the envelope.
	labelMatcher := client.MatchingLabels{
		fleetv1beta1.ParentBindingLabel:     binding.GetName(),
		fleetv1beta1.PlacementTrackingLabel: binding.GetLabels()[fleetv1beta1.PlacementTrackingLabel],
		fleetv1beta1.EnvelopeTypeLabel:      envelopeReader.GetEnvelopeType(),
		fleetv1beta1.EnvelopeNameLabel:      envelopeReader.GetName(),
	}
	// Add ParentNamespaceLabel if the binding is namespaced
	if binding.GetNamespace() != "" {
		labelMatcher[fleetv1beta1.ParentNamespaceLabel] = binding.GetNamespace()
	}

	// Add EnvelopeNamespaceLabel if the envelope type is ResourceEnvelope.
	if envelopeReader.GetEnvelopeType() == string(fleetv1beta1.ResourceEnvelopeType) {
		labelMatcher[fleetv1beta1.EnvelopeNamespaceLabel] = envelopeReader.GetNamespace()
	}

	namespaceMatcher := client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, binding.GetBindingSpec().TargetCluster))
	workList := &fleetv1beta1.WorkList{}
	if err = r.Client.List(ctx, workList, labelMatcher, namespaceMatcher); err != nil {
		klog.ErrorS(err, "Failed to list work objects when finding the work object for an envelope",
			"resourceBinding", klog.KObj(binding),
			"resourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		wrappedErr := fmt.Errorf("failed to list work objects when finding the work object for an envelope %v: %w", envelopeReader.GetEnvelopeObjRef(), err)
		return nil, controller.NewAPIServerError(true, wrappedErr)
	}

	var work *fleetv1beta1.Work
	switch {
	case len(workList.Items) > 1:
		// Multiple matching work objects found. This can only happen in environments that
		// were stuck on this state *before* deterministic Work naming was introduced in
		// buildNewWorkForEnvelopeCR; newly-raced reconciles are now prevented by the
		// API server's name-uniqueness check. We deliberately do NOT auto-delete the
		// extras: both Works may have been applied by the member agent, and deleting one
		// would trigger member-side resource cleanup and user-visible fluctuation on
		// resources the survivor still owns. Surface the state for operator cleanup
		// (manually delete all but the oldest Work in the affected namespace).
		workNames := make([]string, len(workList.Items))
		for i := range workList.Items {
			workNames[i] = workList.Items[i].Name
		}
		wrappedErr := fmt.Errorf("%d work objects found for the same envelope %v, only one expected", len(workList.Items), envelopeReader.GetEnvelopeObjRef())
		klog.ErrorS(wrappedErr, "Duplicate envelope Work objects found; manual cleanup required — delete all but the oldest Work in the namespace",
			"works", workNames,
			"resourceBinding", klog.KObj(binding),
			"resourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		r.recorder.Eventf(binding, corev1.EventTypeWarning, "DuplicateEnvelopeWorks",
			"Multiple Work objects (%v) found for envelope %v in namespace %s; delete all but the oldest to recover",
			workNames, envelopeReader.GetEnvelopeObjRef(), fmt.Sprintf(utils.NamespaceNameFormat, binding.GetBindingSpec().TargetCluster))
		return nil, controller.NewUnexpectedBehaviorError(wrappedErr)
	case len(workList.Items) == 1:
		klog.V(2).InfoS("Found existing work object for the envelope; updating it",
			"work", klog.KObj(&workList.Items[0]),
			"resourceBinding", klog.KObj(binding),
			"resourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		work = &workList.Items[0]
		refreshWorkForEnvelopeCR(work, binding, resourceSnapshot, manifests, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash)
	case len(workList.Items) == 0:
		// No matching work object found; create a new one.
		klog.V(2).InfoS("No existing work object found for the envelope; creating a new one",
			"resourceBinding", klog.KObj(binding),
			"resourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		work = buildNewWorkForEnvelopeCR(workNamePrefix, binding, resourceSnapshot, envelopeReader, manifests, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash)
	}

	return work, nil
}

func extractManifestsFromEnvelopeCR(envelopeReader fleetv1beta1.EnvelopeReader) ([]fleetv1beta1.Manifest, error) {
	manifests := make([]fleetv1beta1.Manifest, 0)
	for k, v := range envelopeReader.GetData() {
		// Verify if the wrapped manifests in the envelope are valid.
		var uObj unstructured.Unstructured
		if unMarshallErr := uObj.UnmarshalJSON(v.Raw); unMarshallErr != nil {
			klog.ErrorS(unMarshallErr, "Failed to parse the wrapped manifest data to a Kubernetes runtime object",
				"manifestKey", k, "envelope", envelopeReader.GetEnvelopeObjRef())
			wrappedErr := fmt.Errorf("failed to parse the wrapped manifest data to a Kubernetes runtime object (manifestKey=%s,envelopeObjRef=%v): %w", k, envelopeReader.GetEnvelopeObjRef(), unMarshallErr)
			return nil, controller.NewUnexpectedBehaviorError(wrappedErr)
		}
		resRef := klog.KRef(uObj.GetNamespace(), uObj.GetName())
		// Perform some basic validation to make sure that the envelope is used correctly.
		switch {
		// Check if a namespaced manifest has been wrapped in a cluster resource envelope.
		case envelopeReader.GetEnvelopeType() == string(fleetv1beta1.ClusterResourceEnvelopeType) && uObj.GetNamespace() != "":
			wrappedErr := fmt.Errorf("a namespaced object %s (%v) has been wrapped in a cluster resource envelope %s", k, resRef, envelopeReader.GetEnvelopeObjRef())
			klog.ErrorS(wrappedErr, "Found an invalid manifest", "manifestKey", k, "envelope", envelopeReader.GetEnvelopeObjRef())
			return nil, controller.NewUserError(wrappedErr)

		// Check if a cluster scoped manifest has been wrapped in a cluster resource envelope.
		case envelopeReader.GetEnvelopeType() == string(fleetv1beta1.ResourceEnvelopeType) && uObj.GetNamespace() == "":
			wrappedErr := fmt.Errorf("a cluster scope object %s (%v) has been wrapped in a resource envelope %s", k, resRef, envelopeReader.GetEnvelopeObjRef())
			klog.ErrorS(wrappedErr, "Found an invalid manifest", "manifestKey", k, "envelope", envelopeReader.GetEnvelopeObjRef())
			return nil, controller.NewUserError(wrappedErr)

		// Check if the namespace of the wrapped manifest matches the envelope's namespace.
		case envelopeReader.GetNamespace() != uObj.GetNamespace():
			wrappedErr := fmt.Errorf("a namespaced object %s (%v) in has been wrapped in a resource envelope from another namespace (%v)", k, resRef, envelopeReader.GetEnvelopeObjRef())
			klog.ErrorS(wrappedErr, "Found an invalid manifest", "manifestKey", k, "envelope", envelopeReader.GetEnvelopeObjRef())
			return nil, controller.NewUserError(wrappedErr)
		}

		manifests = append(manifests, fleetv1beta1.Manifest{
			RawExtension: v,
		})
	}

	// Do a stable sort of the extracted manifests to ensure consistent, deterministic ordering.
	//
	// Note (chenyu1): the sort order here does not affect the order in which resources
	// are applied on a selected member cluster (the work applier will handle the resources
	// in batch with its own grouping logic). KubeFleet sorts resources here solely
	// for consistency (deterministic processing) reasons (i.e., if the set of the
	// resources remain the same, work objects will not be updated).
	//
	// Important (chenyu1): changing the sort order here may induce side effects in
	// existing KubeFleet deployments, as it might trigger update ops on work objects.
	// Do not update the sort order unless absolutely necessary.
	sort.Slice(manifests, func(i, j int) bool {
		obj1 := manifests[i].Raw
		obj2 := manifests[j].Raw
		// order by its json formatted string
		return strings.Compare(string(obj1), string(obj2)) > 0
	})
	return manifests, nil
}

func refreshWorkForEnvelopeCR(
	work *fleetv1beta1.Work,
	resourceBinding fleetv1beta1.BindingObj,
	resourceSnapshot fleetv1beta1.ResourceSnapshotObj,
	manifests []fleetv1beta1.Manifest,
	resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string,
) {
	// Update the parent resource snapshot index label.
	work.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = resourceSnapshot.GetLabels()[fleetv1beta1.ResourceIndexLabel]

	// Update the annotations.
	if work.Annotations == nil {
		work.Annotations = make(map[string]string)
	}
	work.Annotations[fleetv1beta1.ParentResourceSnapshotNameAnnotation] = resourceBinding.GetBindingSpec().ResourceSnapshotName
	work.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] = resourceOverrideSnapshotHash
	work.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation] = clusterResourceOverrideSnapshotHash
	// Update the work spec (the manifests and the apply strategy).
	work.Spec.Workload.Manifests = manifests
	work.Spec.ApplyStrategy = resourceBinding.GetBindingSpec().ApplyStrategy
}

// envelopeWorkNameSuffix returns a deterministic suffix for the envelope Work's name,
// derived from the envelope's identity. Using a deterministic suffix (instead of a random
// UUID) means two concurrent Create attempts for the same envelope collide on name at the
// API server, which enforces the "one Work per (binding, envelope)" invariant server-side.
// The hash is truncated to 8 bytes of SHA-256 (rendered as 16 hex characters), keeping
// the overall Work name well under the DNS-1123 subdomain limit; collision probability
// at the scale of a single binding's envelope set is ~2^-64.
func envelopeWorkNameSuffix(envelopeReader fleetv1beta1.EnvelopeReader) string {
	// NOTE: ClusterResourceEnvelope has an empty namespace; ResourceEnvelope has a namespace.
	// Including the type disambiguates an unlikely name collision across kinds.
	identity := fmt.Sprintf("%s/%s/%s",
		envelopeReader.GetEnvelopeType(),
		envelopeReader.GetNamespace(),
		envelopeReader.GetName())
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:8])
}

func buildNewWorkForEnvelopeCR(
	workNamePrefix string,
	resourceBinding fleetv1beta1.BindingObj,
	resourceSnapshot fleetv1beta1.ResourceSnapshotObj,
	envelopeReader fleetv1beta1.EnvelopeReader,
	manifests []fleetv1beta1.Manifest,
	resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string,
) *fleetv1beta1.Work {
	workName := fmt.Sprintf(fleetv1beta1.WorkNameWithEnvelopeCRFmt, workNamePrefix, envelopeWorkNameSuffix(envelopeReader))
	workNamespace := fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.GetBindingSpec().TargetCluster)

	// Create the labels map
	labels := map[string]string{
		fleetv1beta1.ParentBindingLabel:               resourceBinding.GetName(),
		fleetv1beta1.PlacementTrackingLabel:           resourceBinding.GetLabels()[fleetv1beta1.PlacementTrackingLabel],
		fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.GetLabels()[fleetv1beta1.ResourceIndexLabel],
		fleetv1beta1.EnvelopeTypeLabel:                envelopeReader.GetEnvelopeType(),
		fleetv1beta1.EnvelopeNameLabel:                envelopeReader.GetName(),
	}
	// Add ParentNamespaceLabel if the binding is namespaced
	if resourceBinding.GetNamespace() != "" {
		labels[fleetv1beta1.ParentNamespaceLabel] = resourceBinding.GetNamespace()
	}

	// Add EnvelopeNamespaceLabel if the envelope type is ResourceEnvelope.
	if envelopeReader.GetEnvelopeType() == string(fleetv1beta1.ResourceEnvelopeType) {
		labels[fleetv1beta1.EnvelopeNamespaceLabel] = envelopeReader.GetNamespace()
	}

	return &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
			Labels:    labels,
			Annotations: map[string]string{
				fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.GetBindingSpec().ResourceSnapshotName,
				fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        resourceOverrideSnapshotHash,
				fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: clusterResourceOverrideSnapshotHash,
			},
			// OwnerReferences cannot be added, as the namespaces of work and resourceBinding are different.
			// Garbage collector will assume the resourceBinding is invalid as it cannot be found in the same namespace.
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: manifests,
			},
			ApplyStrategy: resourceBinding.GetBindingSpec().ApplyStrategy,
		},
	}
}
