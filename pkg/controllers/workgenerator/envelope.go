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
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// createOrUpdateEnvelopeCRWorkObj creates or updates a work object for a given envelope CR.
func (r *Reconciler) createOrUpdateEnvelopeCRWorkObj(
	ctx context.Context,
	envelopeReader fleetv1beta1.EnvelopeReader,
	workNamePrefix string,
	resourceBinding *fleetv1beta1.ClusterResourceBinding,
	resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string,
) (*fleetv1beta1.Work, error) {
	manifests, err := extractManifestsFromEnvelopeCR(envelopeReader)
	if err != nil {
		klog.ErrorS(err, "Failed to extract manifests from the envelope spec",
			"clusterResourceBinding", klog.KObj(resourceBinding),
			"clusterResourceSnapshot", klog.KObj(resourceBinding),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		return nil, err
	}
	klog.V(2).InfoS("Successfully extracted wrapped manifests from the envelope",
		"numOfResources", len(manifests),
		"clusterResourceBinding", klog.KObj(resourceBinding),
		"clusterResourceSnapshot", klog.KObj(resourceSnapshot),
		"envelope", envelopeReader.GetEnvelopeObjRef())

	// Check to see if a corresponding work object has been created for the envelope.
	labelMatcher := client.MatchingLabels{
		fleetv1beta1.ParentBindingLabel:     resourceBinding.Name,
		fleetv1beta1.CRPTrackingLabel:       resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
		fleetv1beta1.EnvelopeTypeLabel:      envelopeReader.GetEnvelopeType(),
		fleetv1beta1.EnvelopeNameLabel:      envelopeReader.GetName(),
		fleetv1beta1.EnvelopeNamespaceLabel: envelopeReader.GetNamespace(),
	}
	workList := &fleetv1beta1.WorkList{}
	if err = r.Client.List(ctx, workList, labelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list work objects when finding the work object for an envelope",
			"clusterResourceBinding", klog.KObj(resourceBinding),
			"clusterResourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		wrappedErr := fmt.Errorf("failed to list work objects when finding the work object for an envelope %v: %w", envelopeReader.GetEnvelopeObjRef(), err)
		return nil, controller.NewAPIServerError(true, wrappedErr)
	}

	var work *fleetv1beta1.Work
	switch {
	case len(workList.Items) > 1:
		// Multiple matching work objects found; this should never occur under normal conditions.
		wrappedErr := fmt.Errorf("%d work objects found for the same envelope %v, only one expected", len(workList.Items), envelopeReader.GetEnvelopeObjRef())
		klog.ErrorS(wrappedErr, "Failed to create or update work object for envelope",
			"clusterResourceBinding", klog.KObj(resourceBinding),
			"clusterResourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		return nil, controller.NewUnexpectedBehaviorError(wrappedErr)
	case len(workList.Items) == 1:
		klog.V(2).InfoS("Found existing work object for the envelope; updating it",
			"work", klog.KObj(&workList.Items[0]),
			"clusterResourceBinding", klog.KObj(resourceBinding),
			"clusterResourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		work = &workList.Items[0]
		refreshWorkForEnvelopeCR(work, resourceBinding, resourceSnapshot, manifests, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash)
	case len(workList.Items) == 0:
		// No matching work object found; create a new one.
		klog.V(2).InfoS("No existing work object found for the envelope; creating a new one",
			"clusterResourceBinding", klog.KObj(resourceBinding),
			"clusterResourceSnapshot", klog.KObj(resourceSnapshot),
			"envelope", envelopeReader.GetEnvelopeObjRef())
		work = buildNewWorkForEnvelopeCR(workNamePrefix, resourceBinding, resourceSnapshot, envelopeReader, manifests, resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash)
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
	resourceBinding *fleetv1beta1.ClusterResourceBinding,
	resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	manifests []fleetv1beta1.Manifest,
	resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string,
) {
	// Update the parent resource snapshot index label.
	work.Labels[fleetv1beta1.ParentResourceSnapshotIndexLabel] = resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel]

	// Update the annotations.
	if work.Annotations == nil {
		work.Annotations = make(map[string]string)
	}
	work.Annotations[fleetv1beta1.ParentResourceSnapshotNameAnnotation] = resourceBinding.Spec.ResourceSnapshotName
	work.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] = resourceOverrideSnapshotHash
	work.Annotations[fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation] = clusterResourceOverrideSnapshotHash
	// Update the work spec (the manifests and the apply strategy).
	work.Spec.Workload.Manifests = manifests
	work.Spec.ApplyStrategy = resourceBinding.Spec.ApplyStrategy
}

func buildNewWorkForEnvelopeCR(
	workNamePrefix string,
	resourceBinding *fleetv1beta1.ClusterResourceBinding,
	resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot,
	envelopeReader fleetv1beta1.EnvelopeReader,
	manifests []fleetv1beta1.Manifest,
	resourceOverrideSnapshotHash, clusterResourceOverrideSnapshotHash string,
) *fleetv1beta1.Work {
	workName := fmt.Sprintf(fleetv1beta1.WorkNameWithEnvelopeCRFmt, workNamePrefix, uuid.NewUUID())
	workNamespace := fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster)

	return &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
			Labels: map[string]string{
				fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
				fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
				fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
				fleetv1beta1.EnvelopeTypeLabel:                envelopeReader.GetEnvelopeType(),
				fleetv1beta1.EnvelopeNameLabel:                envelopeReader.GetName(),
				fleetv1beta1.EnvelopeNamespaceLabel:           envelopeReader.GetNamespace(),
			},
			Annotations: map[string]string{
				fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.Spec.ResourceSnapshotName,
				fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        resourceOverrideSnapshotHash,
				fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: clusterResourceOverrideSnapshotHash,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       resourceBinding.Kind,
					Name:       resourceBinding.Name,
					UID:        resourceBinding.UID,
					// Make sure that the resource binding can only be deleted after
					// all of its managed work objects have been deleted.
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: manifests,
			},
			ApplyStrategy: resourceBinding.Spec.ApplyStrategy,
		},
	}
}
