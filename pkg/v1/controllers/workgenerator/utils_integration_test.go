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
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	testutilsresource "github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

const (
	placementBindingNameTemplate          = "binding-%s"
	placementPolicyNameTemplate           = "policy-%s"
	clusterPlacementBindingNameTemplate   = "cluster-binding-%s"
	clusterPlacementPolicyNameTemplate    = "cluster-policy-%s"
	placementResourceSnapshotNameTemplate = "%s-snapshot-%d"
	// The name template for secondary placement resource snapshots (those with a non-zero sub-index).
	subIndexedPlacementResourceSnapshotNameTemplate = "%s-snapshot-%d-%d"
	// The name template for stray placement resource snapshots (see bindingFixture.createStraySnapshot).
	straySnapshotNameTemplate = "%s-snapshot-%d-%d-stray-%s"
	nsNameTemplate            = "ns-%s"

	deployName    = "deploy-1"
	configMapName = "configmap-1"

	indexedNSNameTemplate = "ns-%d"
	deployNameTemplate    = "deploy-%d"
	configMapNameTemplate = "configmap-%d"
	// A zero-padded ConfigMap name template, so that the names sort in the order of their numbers.
	paddedConfigMapNameTemplate = "configmap-%02d"

	workKind      = "Work"
	nsKind        = "Namespace"
	deployKind    = "Deployment"
	configMapKind = "ConfigMap"
	nginxName     = "nginx"

	appsAPIGroup = "apps"

	configMapDataKey   = "foo"
	configMapDataValue = "bar"
	// Alternative ConfigMap data values, used to simulate changes or drifts.
	altConfigMapDataValue1 = "baz"
	altConfigMapDataValue2 = "qux"

	dummyContentsHash = "dummy-hash"

	nsResource        = "namespaces"
	deployResource    = "deployments"
	configMapResource = "configmaps"

	markedAsAppliedReason       = "MarkedAsApplied"
	markedAsAvailableReason     = "MarkedAsAvailable"
	markedAsUnavailableReason   = "MarkedAsUnavailable"
	markedAsFailedToApplyReason = "MarkedAsFailedToApply"

	markedAsAppliedManifestMsg = "Manifest has been marked as applied"
	markedAsAppliedWorkMsg     = "Work has been marked as applied"

	// Substrings of the messages that the work generator reports (in the Synchronized condition of a placement binding)
	// when the placement resource snapshots that the placement binding points to are invalid.
	invalidSnapshotsMsgPrefix = "Failed to synchronize resources as the given placement resource snapshots are invalid"
	fewerSnapshotsErrMsg      = "there are fewer placement resource snapshots than the count label indicates"
	duplicateSubIdxErrMsg     = "found placement resource snapshots with duplicate sub-indices"
	inconsistentSubIdxErrMsg  = "found placement resource snapshots with inconsistent sub-indices"
	// The format of the message substring reporting that a placement resource snapshot (by name) is not found.
	snapshotNotFoundErrMsgFmt = "%q not found"
)

// The namespace of cluster-scoped objects (i.e., none); the test helpers use it to tell cluster-scoped placement
// bindings and placement resource snapshots apart from their namespace-scoped counterparts.
const clusterScopedNS = ""

const (
	eventuallyDuration   = time.Second * 10
	eventuallyInterval   = time.Second * 1
	consistentlyDuration = time.Second * 5
	consistentlyInterval = time.Millisecond * 500
)

var (
	ignoreFieldObjectMetaAutoGenFields = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "CreationTimestamp", "Generation", "ResourceVersion", "SelfLink", "UID", "ManagedFields")
	ignoreFieldWorkTypeMetaAndStatus   = cmpopts.IgnoreFields(placementv1alpha1.Work{}, "TypeMeta", "Status")
	ignoreFieldConditionLTTMsg         = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")

	lessFuncWork = func(a, b placementv1alpha1.Work) bool {
		return a.Name < b.Name
	}
	// The work generator collects failed resources work object by work object, in the order the work objects are
	// listed from the cache, which is not deterministic; failed resources are sorted before comparison.
	lessFuncFailedResource = func(a, b placementv1alpha1.FailedResource) bool {
		keyA := fmt.Sprintf("%s/%s/%s/%s", a.ObjectRef.APIGroup, a.ObjectRef.Kind, a.ObjectRef.Namespace, a.ObjectRef.Name)
		keyB := fmt.Sprintf("%s/%s/%s/%s", b.ObjectRef.APIGroup, b.ObjectRef.Kind, b.ObjectRef.Namespace, b.ObjectRef.Name)
		return keyA < keyB
	}
)

var (
	ns = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       nsKind,
			APIVersion: "v1",
		},
	}

	deploy = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       deployKind,
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: deployName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": nginxName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": nginxName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  nginxName,
							Image: nginxName,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	configMap = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       configMapKind,
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: configMapName,
		},
		Data: map[string]string{
			configMapDataKey: configMapDataValue,
		},
	}
)

func marshalK8sObjJSON(obj runtime.Object) []byte {
	json, err := testutilsresource.MarshalRuntimeObjToJSONForTest(obj)
	Expect(err).To(BeNil(), "Failed to marshal the k8s object to JSON")
	return json
}

// nsSnapshottedResource returns a snapshotted resource for a NS object of the given name.
func nsSnapshottedResource(nsName string) placementv1alpha1.SnapshottedResource {
	regularNS := ns.DeepCopy()
	regularNS.Name = nsName
	return placementv1alpha1.SnapshottedResource{
		Identifier: placementv1alpha1.ObjectReference{
			Name:       nsName,
			APIVersion: "v1",
			Kind:       nsKind,
		},
		Manifest: runtime.RawExtension{Raw: marshalK8sObjJSON(regularNS)},
	}
}

// deploySnapshottedResource returns a snapshotted resource for a Deployment object of the given namespace and name.
func deploySnapshottedResource(nsName, deployName string) placementv1alpha1.SnapshottedResource {
	regularDeploy := deploy.DeepCopy()
	regularDeploy.Namespace = nsName
	regularDeploy.Name = deployName
	return placementv1alpha1.SnapshottedResource{
		Identifier: placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       deployName,
			APIGroup:   appsAPIGroup,
			APIVersion: "v1",
			Kind:       deployKind,
		},
		Manifest: runtime.RawExtension{Raw: marshalK8sObjJSON(regularDeploy)},
	}
}

// configMapSnapshottedResource returns a snapshotted resource for a ConfigMap object of the given namespace and name.
func configMapSnapshottedResource(nsName, configMapName string) placementv1alpha1.SnapshottedResource {
	return configMapSnapshottedResourceWithData(nsName, configMapName, nil)
}

// configMapSnapshottedResourceWithData returns a snapshotted resource for a ConfigMap object of the given namespace
// and name, with the given data (or the data from the template if the given data is nil).
func configMapSnapshottedResourceWithData(nsName, configMapName string, data map[string]string) placementv1alpha1.SnapshottedResource {
	regularConfigMap := configMap.DeepCopy()
	regularConfigMap.Namespace = nsName
	regularConfigMap.Name = configMapName
	if data != nil {
		regularConfigMap.Data = data
	}
	return placementv1alpha1.SnapshottedResource{
		Identifier: placementv1alpha1.ObjectReference{
			Namespace:  nsName,
			Name:       configMapName,
			APIVersion: "v1",
			Kind:       configMapKind,
		},
		Manifest: runtime.RawExtension{Raw: marshalK8sObjJSON(regularConfigMap)},
	}
}

// nsManifestIdentifier returns the manifest identifier for a NS object of the given name at the given ordinal.
func nsManifestIdentifier(ordinal int, nsName string) placementv1alpha1.ManifestIdentifier {
	return placementv1alpha1.ManifestIdentifier{
		Ordinal:    ordinal,
		Name:       nsName,
		APIVersion: "v1",
		Kind:       nsKind,
		Resource:   nsResource,
	}
}

// deployManifestIdentifier returns the manifest identifier for a Deployment object of the given namespace and name
// at the given ordinal.
func deployManifestIdentifier(ordinal int, nsName, deployName string) placementv1alpha1.ManifestIdentifier {
	return placementv1alpha1.ManifestIdentifier{
		Ordinal:    ordinal,
		Namespace:  nsName,
		Name:       deployName,
		APIGroup:   appsAPIGroup,
		APIVersion: "v1",
		Kind:       deployKind,
		Resource:   deployResource,
	}
}

// configMapManifestIdentifier returns the manifest identifier for a ConfigMap object of the given namespace and name
// at the given ordinal.
func configMapManifestIdentifier(ordinal int, nsName, configMapName string) placementv1alpha1.ManifestIdentifier {
	return placementv1alpha1.ManifestIdentifier{
		Ordinal:    ordinal,
		Namespace:  nsName,
		Name:       configMapName,
		APIVersion: "v1",
		Kind:       configMapKind,
		Resource:   configMapResource,
	}
}

// objectRefFromManifestIdentifier returns the object reference (as used in the failed resources of placement binding
// status) for the resource identified by the given manifest identifier.
func objectRefFromManifestIdentifier(identifier placementv1alpha1.ManifestIdentifier) placementv1alpha1.ObjectReference {
	return placementv1alpha1.ObjectReference{
		Namespace:  identifier.Namespace,
		Name:       identifier.Name,
		APIGroup:   identifier.APIGroup,
		APIVersion: identifier.APIVersion,
		Kind:       identifier.Kind,
	}
}

// newPlacementBindingObj returns a placement binding object of the appropriate scope with only the namespace and name
// set: a ClusterPlacementBinding object if the namespace is empty, or a PlacementBinding object otherwise.
func newPlacementBindingObj(bindingNS, bindingName string) placementv1alpha1.PlacementBindingAccessor {
	if bindingNS == "" {
		return &placementv1alpha1.ClusterPlacementBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
			},
		}
	}
	return &placementv1alpha1.PlacementBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: bindingNS,
			Name:      bindingName,
		},
	}
}

// newPlacementResourceSnapshotObj returns a placement resource snapshot object of the appropriate scope with only the
// namespace and name set: a ClusterPlacementResourceSnapshot object if the namespace is empty, or a
// PlacementResourceSnapshot object otherwise.
func newPlacementResourceSnapshotObj(snapshotNS, snapshotName string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	if snapshotNS == "" {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: snapshotName,
			},
		}
	}
	return &placementv1alpha1.PlacementResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: snapshotNS,
			Name:      snapshotName,
		},
	}
}

// getPlacementBinding retrieves a placement binding (cluster-scoped if the namespace is empty).
func getPlacementBinding(bindingNS, bindingName string) (placementv1alpha1.PlacementBindingAccessor, error) {
	binding := newPlacementBindingObj(bindingNS, bindingName)
	if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); err != nil {
		return nil, fmt.Errorf("failed to retrieve the placement binding: %w", err)
	}
	return binding, nil
}

// createPlacementResourceSnapshot creates a primary placement resource snapshot (sub-index 0) of the given index
// with the given resources, as the only snapshot of its index; it returns the snapshot as written to the API server.
//
// If the namespace is empty, a ClusterPlacementResourceSnapshot object is created; otherwise, a
// PlacementResourceSnapshot object is created.
func createPlacementResourceSnapshot(
	snapshotNS, snapshotName, ownerPlacementPolicyName string,
	index int,
	resources ...placementv1alpha1.SnapshottedResource,
) placementv1alpha1.PlacementResourceSnapshotAccessor {
	return createSubIndexedPlacementResourceSnapshot(snapshotNS, snapshotName, ownerPlacementPolicyName, index, 0, 1, resources...)
}

// createSubIndexedPlacementResourceSnapshot creates a placement resource snapshot of the given index and sub-index
// with the given resources; it returns the snapshot as written to the API server.
//
// The count of snapshots sharing the same index is set (as a label) only on the primary snapshot (sub-index 0);
// all snapshots of the same index share the same contents hash. If the namespace is empty, a
// ClusterPlacementResourceSnapshot object is created; otherwise, a PlacementResourceSnapshot object is created.
func createSubIndexedPlacementResourceSnapshot(
	snapshotNS, snapshotName, ownerPlacementPolicyName string,
	index, subIndex, subIndexedSnapshotCount int,
	resources ...placementv1alpha1.SnapshottedResource,
) placementv1alpha1.PlacementResourceSnapshotAccessor {
	return createSubIndexedPlacementResourceSnapshotWithHash(snapshotNS, snapshotName, ownerPlacementPolicyName,
		index, subIndex, subIndexedSnapshotCount, dummyContentsHash, resources...)
}

// createSubIndexedPlacementResourceSnapshotWithHash is the same as createSubIndexedPlacementResourceSnapshot, except
// that it allows specifying the contents hash annotation of the snapshot.
func createSubIndexedPlacementResourceSnapshotWithHash(
	snapshotNS, snapshotName, ownerPlacementPolicyName string,
	index, subIndex, subIndexedSnapshotCount int,
	contentsHash string,
	resources ...placementv1alpha1.SnapshottedResource,
) placementv1alpha1.PlacementResourceSnapshotAccessor {
	snapshot := newPlacementResourceSnapshotObj(snapshotNS, snapshotName)
	labels := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  snapshotOwnerLabelValue(ownerPlacementPolicyName),
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    strconv.Itoa(index),
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIndex),
	}
	if subIndex == 0 {
		labels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = strconv.Itoa(subIndexedSnapshotCount)
	}
	snapshot.SetLabels(labels)
	snapshot.SetAnnotations(map[string]string{
		placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: contentsHash,
		placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      ownerPlacementPolicyName,
	})
	snapshot.SetSpec(placementv1alpha1.PlacementResourceSnapshotSpec{
		Resources: resources,
	})
	Expect(hubClient.Create(ctx, snapshot)).To(Succeed(), "Failed to create the placement resource snapshot")
	return snapshot
}

// snapshotOwnerLabelValue returns the value of the owner label on placement resource snapshots for the given placement
// policy name, mirroring the placement resource snapshot manager: names over 61 characters are truncated and appended
// with a hash suffix, so that the value stays within the label value length limit. The work generator does not read
// this label (it looks up snapshots by the owner annotation, which always carries the full name).
func snapshotOwnerLabelValue(placementPolicyName string) string {
	const lenLimit, hashLen = 61, 12
	if len(placementPolicyName) <= lenLimit {
		return placementPolicyName
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(placementPolicyName)))[:hashLen]
	return fmt.Sprintf("%s-%s", placementPolicyName[:lenLimit-hashLen-1], hash)
}

// longDottedName returns a random name of exactly the given length that starts with the given prefix, and contains
// dots every 50 characters or so; it is a valid DNS subdomain name as long as the length does not exceed 253.
func longDottedName(prefix string, length int) string {
	name := fmt.Sprintf("%s-%s", prefix, utils.RandStr())
	for len(name) < length {
		remaining := length - len(name)
		if remaining == 1 {
			// Avoid a trailing dot.
			name += "a"
			break
		}
		name += "." + strings.Repeat("a", min(remaining-1, 50))
	}
	return name
}

// createPlacementBinding creates a placement binding that binds a placement resource snapshot to a member cluster;
// it returns the binding as written to the API server.
//
// If the namespace is empty, a ClusterPlacementBinding object is created; otherwise, a PlacementBinding object
// is created.
func createPlacementBinding(bindingNS, bindingName, placementPolicyName, clusterName, snapshotName string) placementv1alpha1.PlacementBindingAccessor {
	binding := newPlacementBindingObj(bindingNS, bindingName)
	binding.SetSpec(placementv1alpha1.PlacementBindingSpec{
		PlacementPolicyName:  placementPolicyName,
		ClusterName:          clusterName,
		ResourceSnapshotName: snapshotName,
	})
	Expect(hubClient.Create(ctx, binding)).To(Succeed(), "Failed to create the placement binding")
	return binding
}

// updatePlacementBindingResourceSnapshot points a placement binding to a different placement resource snapshot,
// as if a rollout has been made.
func updatePlacementBindingResourceSnapshot(bindingNS, bindingName, snapshotName string) {
	Eventually(func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		binding.GetSpec().ResourceSnapshotName = snapshotName
		if err := hubClient.Update(ctx, binding); err != nil {
			return fmt.Errorf("failed to update the placement binding: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the placement binding with a new placement resource snapshot")
}

// wantWorkForPrimaryPlacementResourceSnapshot builds the work object that the work generator is expected to create
// for a placement binding (cluster-scoped if the namespace is empty) from its primary placement resource snapshot,
// when the snapshot is the only one of its index.
func wantWorkForPrimaryPlacementResourceSnapshot(
	workNS, workName, bindingNS, bindingName, placementPolicyName string,
	snapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
) placementv1alpha1.Work {
	return wantWorkForPlacementResourceSnapshot(workNS, workName, bindingNS, bindingName, placementPolicyName,
		snapshot.GetName(), 0, 1, nil, snapshot)
}

// wantWorkForPlacementResourceSnapshot builds the work object that the work generator is expected to create for a
// placement binding (cluster-scoped if the namespace is empty) from a placement resource snapshot of the given
// sub-index.
//
// The linked work count applies only to the primary work object (sub-index 0); the owner reference (which should
// point to the primary work object) applies only to secondary work objects.
func wantWorkForPlacementResourceSnapshot(
	workNS, workName, bindingNS, bindingName, placementPolicyName, primarySnapshotName string,
	subIndex, linkedWorkCount int,
	ownerRef *metav1.OwnerReference,
	snapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
) placementv1alpha1.Work {
	// The manifests in the work object should match those in the snapshot as written to the API server.
	resources := snapshot.GetSpec().Resources
	manifests := make([]placementv1alpha1.Manifest, len(resources))
	for idx := range resources {
		manifests[idx] = placementv1alpha1.Manifest{RawExtension: resources[idx].Manifest}
	}

	work := placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: workNS,
			Name:      workName,
			Labels: map[string]string{
				placementv1alpha1.WorkOwnerNamespaceLabelKey:          bindingNS,
				placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  workOwnerLabelValue(placementPolicyName),
				placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: workOwnerLabelValue(bindingName),
			},
			Annotations: map[string]string{
				placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: primarySnapshotName,
				placementv1alpha1.WorkDerivedFromSourceAnnotationKey:                        fmt.Sprintf("placement-resource-snapshot/%d", subIndex),
				placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   placementPolicyName,
				placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  bindingName,
			},
		},
		Spec: placementv1alpha1.WorkSpec{
			Manifests: manifests,
		},
	}
	if subIndex == 0 {
		work.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = strconv.Itoa(linkedWorkCount)
	} else if ownerRef != nil {
		work.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	}
	return work
}

// wantPlacementBindingStatusWaitingForSync builds the placement binding status that the work generator is expected
// to report after it has (re-)generated the work objects, but before the work objects have been processed.
func wantPlacementBindingStatusWaitingForSync(selectedResources int32, snapshotName string) *placementv1alpha1.PlacementBindingStatus {
	return &placementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			{
				Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
				Status: metav1.ConditionFalse,
				Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonWaitingForSynchronization,
			},
			{
				Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
				Status: metav1.ConditionUnknown,
				Reason: placementv1alpha1.PlacementBindingAvailableCondReasonWaitingForAvailabilityCheck,
			},
		},
		SelectedResources:                 ptr.To(selectedResources),
		LastProcessedResourceSnapshotName: ptr.To(snapshotName),
	}
}

// wantPlacementBindingStatusSyncedAndAvailable builds the placement binding status that the work generator is
// expected to report after all the work objects have been applied and become available.
func wantPlacementBindingStatusSyncedAndAvailable(selectedResources int32, snapshotName string) *placementv1alpha1.PlacementBindingStatus {
	return &placementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			{
				Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
				Status: metav1.ConditionTrue,
				Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonAllResourcesSynchronized,
			},
			{
				Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: placementv1alpha1.PlacementBindingAvailableCondReasonAllResourcesAvailable,
			},
		},
		SelectedResources:                 ptr.To(selectedResources),
		SynchronizedResources:             ptr.To(selectedResources),
		AvailableResources:                ptr.To(selectedResources),
		LastProcessedResourceSnapshotName: ptr.To(snapshotName),
	}
}

// wantPlacementBindingStatusInvalidSnapshots builds the placement binding status that the work generator is expected
// to report when the placement resource snapshots that a placement binding has been rolled out to are invalid. The
// resource counters are tallied from the work objects of the last processed placement resource snapshot, all of
// which are expected to have been applied and become available. For a rollout to a different snapshot, previousStatus
// supplies the Available condition to preserve; nil expects an incomplete set of resources instead.
func wantPlacementBindingStatusInvalidSnapshots(placedResources int32, lastProcessedSnapshotName string, previousStatus *placementv1alpha1.PlacementBindingStatus) *placementv1alpha1.PlacementBindingStatus {
	wantStatus := &placementv1alpha1.PlacementBindingStatus{
		Conditions: []metav1.Condition{
			{
				Type:   placementv1alpha1.PlacementBindingCondTypeSynchronized,
				Status: metav1.ConditionFalse,
				Reason: placementv1alpha1.PlacementBindingSynchronizedCondReasonInvalidSnapshots,
			},
			{
				Type:   placementv1alpha1.PlacementBindingCondTypeAvailable,
				Status: metav1.ConditionFalse,
				Reason: placementv1alpha1.PlacementBindingAvailableCondReasonIncompleteSetOfResources,
			},
		},
		SelectedResources:                 ptr.To(placedResources),
		SynchronizedResources:             ptr.To(placedResources),
		AvailableResources:                ptr.To(placedResources),
		LastProcessedResourceSnapshotName: ptr.To(lastProcessedSnapshotName),
	}
	if previousStatus != nil {
		wantStatus.Conditions = wantStatus.Conditions[:1]
		if availableCond := meta.FindStatusCondition(previousStatus.Conditions, placementv1alpha1.PlacementBindingCondTypeAvailable); availableCond != nil {
			wantStatus.Conditions = append(wantStatus.Conditions, *availableCond)
		}
	}
	return wantStatus
}

func placementBindingFinalizerAddedActual(bindingNS, bindingName string) func() error {
	return func() error {
		// Retrieve the placement binding.
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		// Check that the cleanup finalizer has been added.
		if !controllerutil.ContainsFinalizer(binding, workGeneratorCleanupFinalizer) {
			return fmt.Errorf("cleanup finalizer has not been added")
		}
		return nil
	}
}

func worksGeneratedActual(clusterName, bindingNS, bindingName string, wantWorks []placementv1alpha1.Work) func() error {
	return func() error {
		// List the work objects owned by the placement binding (the owner label carries a hashed value for long or
		// dotted placement binding names).
		workList := &placementv1alpha1.WorkList{}
		listOptions := []client.ListOption{
			client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, clusterName)),
			client.MatchingLabels{
				placementv1alpha1.WorkOwnerNamespaceLabelKey:          bindingNS,
				placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: workOwnerLabelValue(bindingName),
			},
		}
		if err := hubClient.List(ctx, workList, listOptions...); err != nil {
			return fmt.Errorf("failed to list work objects: %w", err)
		}

		// Check that the work objects have been generated as expected.
		if diff := cmp.Diff(
			workList.Items, wantWorks,
			ignoreFieldObjectMetaAutoGenFields,
			ignoreFieldWorkTypeMetaAndStatus,
			cmpopts.EquateEmpty(),
			cmpopts.SortSlices(lessFuncWork),
		); diff != "" {
			return fmt.Errorf("works diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func placementBindingStatusUpdatedActual(bindingNS, bindingName string, wantStatus *placementv1alpha1.PlacementBindingStatus) func() error {
	return func() error {
		// Retrieve the placement binding.
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		// Prepare the expected placement binding status; update the conditions with the observed generation.
		wantStatusWithObsGen := wantStatus.DeepCopy()
		for idx := range wantStatusWithObsGen.Conditions {
			wantStatusWithObsGen.Conditions[idx].ObservedGeneration = binding.GetGeneration()
		}

		// Check that the placement binding status has been updated as expected.
		if diff := cmp.Diff(
			*binding.GetStatus(), *wantStatusWithObsGen,
			ignoreFieldConditionLTTMsg,
			cmpopts.SortSlices(lessFuncFailedResource),
		); diff != "" {
			return fmt.Errorf("placement binding status diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// placementBindingStatusMatchesActual verifies that the status of a placement binding matches the given one as is,
// i.e., unlike placementBindingStatusUpdatedActual, the observed generations of the conditions are not updated with
// the current generation of the placement binding. This is useful for verifying that a placement binding status has
// not been refreshed after the placement binding spec changes.
func placementBindingStatusMatchesActual(bindingNS, bindingName string, wantStatus *placementv1alpha1.PlacementBindingStatus) func() error {
	return func() error {
		// Retrieve the placement binding.
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		// Check that the placement binding status matches the expected one.
		if diff := cmp.Diff(
			*binding.GetStatus(), *wantStatus,
			ignoreFieldConditionLTTMsg,
			cmpopts.SortSlices(lessFuncFailedResource),
		); diff != "" {
			return fmt.Errorf("placement binding status diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// placementBindingSyncCondMessageActual verifies that the message of the Synchronized condition on a placement binding
// contains the given substring; the status comparison helpers ignore condition messages.
func placementBindingSyncCondMessageActual(bindingNS, bindingName, wantMsgSubstr string) func() error {
	return func() error {
		// Retrieve the placement binding.
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		syncCond := meta.FindStatusCondition(binding.GetStatus().Conditions, placementv1alpha1.PlacementBindingCondTypeSynchronized)
		if syncCond == nil {
			return fmt.Errorf("the Synchronized condition is not found on the placement binding")
		}
		if !strings.Contains(syncCond.Message, wantMsgSubstr) {
			return fmt.Errorf("the Synchronized condition message = %q, want it to contain %q", syncCond.Message, wantMsgSubstr)
		}
		return nil
	}
}

// markWorkAsAppliedAndAvailable updates the status of a work object to mark it (and all of its manifests) as
// applied and available, as the KubeFleet member agent would do.
func markWorkAsAppliedAndAvailable(workNS, workName string, manifestIdentifiers []placementv1alpha1.ManifestIdentifier) {
	// Retrieve the work object.
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work)).To(Succeed(), "Failed to retrieve the work object")

	// Mark the work object as applied and available.
	now := metav1.Now()
	manifestStatuses := make([]placementv1alpha1.PerManifestStatus, len(manifestIdentifiers))
	for idx := range manifestIdentifiers {
		manifestStatuses[idx] = placementv1alpha1.PerManifestStatus{
			Identifier: manifestIdentifiers[idx],
			Conditions: []metav1.Condition{
				{
					Type:               placementv1alpha1.ManifestCondTypeApplied,
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					Reason:             markedAsAppliedReason,
					Message:            markedAsAppliedManifestMsg,
					LastTransitionTime: now,
				},
				{
					Type:               placementv1alpha1.ManifestCondTypeAvailable,
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					Reason:             markedAsAvailableReason,
					Message:            "Manifest has been marked as available",
					LastTransitionTime: now,
				},
			},
		}
	}
	work.Status = placementv1alpha1.WorkStatus{
		Conditions: []metav1.Condition{
			{
				Type:               placementv1alpha1.WorkCondTypeApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: work.Generation,
				Reason:             markedAsAppliedReason,
				Message:            markedAsAppliedWorkMsg,
				LastTransitionTime: now,
			},
			{
				Type:               placementv1alpha1.WorkCondTypeAvailable,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: work.Generation,
				Reason:             markedAsAvailableReason,
				Message:            "Work has been marked as available",
				LastTransitionTime: now,
			},
		},
		Manifests: manifestStatuses,
	}
	Expect(hubClient.Status().Update(ctx, work)).To(Succeed(), "Failed to mark the work object as applied and available")
}

// markWorkManifestAsUnavailable updates the status of a work object to mark one of its manifests (identified by
// its ordinal), and consequently the work object itself, as unavailable, as the KubeFleet member agent would do when
// an applied resource fails its availability check.
func markWorkManifestAsUnavailable(workNS, workName string, ordinal int) {
	// Retrieve the work object.
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work)).To(Succeed(), "Failed to retrieve the work object")

	// Mark the manifest as unavailable.
	found := false
	for idx := range work.Status.Manifests {
		manifestStatus := &work.Status.Manifests[idx]
		if manifestStatus.Identifier.Ordinal != ordinal {
			continue
		}
		meta.SetStatusCondition(&manifestStatus.Conditions, metav1.Condition{
			Type:               placementv1alpha1.ManifestCondTypeAvailable,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 1,
			Reason:             markedAsUnavailableReason,
			Message:            "Manifest has been marked as unavailable",
		})
		found = true
	}
	Expect(found).To(BeTrue(), "Failed to find the status of the manifest with ordinal %d", ordinal)

	// Mark the work object as unavailable.
	meta.SetStatusCondition(&work.Status.Conditions, metav1.Condition{
		Type:               placementv1alpha1.WorkCondTypeAvailable,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: work.Generation,
		Reason:             markedAsUnavailableReason,
		Message:            "Work has been marked as unavailable",
	})
	Expect(hubClient.Status().Update(ctx, work)).To(Succeed(), "Failed to mark the work object as unavailable")
}

// markWorkManifestsAsFailed updates the status of a work object to mark some of its manifests (identified by their
// ordinals) as failed to be applied (with the given diff details, if any), and some as applied but failed to become
// available; all the other manifests are marked as applied and available. This simulates what the KubeFleet member
// agent would do when it cannot apply some resources (e.g., when it fails to take over existing resources with diffs)
// and some applied resources fail their availability checks.
func markWorkManifestsAsFailed(
	workNS, workName string,
	manifestIdentifiers []placementv1alpha1.ManifestIdentifier,
	failedToApplyOrdinals map[int]*placementv1alpha1.DiffDetails,
	failedToBecomeAvailableOrdinals sets.Set[int],
) {
	// Retrieve the work object.
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work)).To(Succeed(), "Failed to retrieve the work object")

	now := metav1.Now()
	appliedCond := metav1.Condition{
		Type:               placementv1alpha1.ManifestCondTypeApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 1,
		Reason:             markedAsAppliedReason,
		Message:            markedAsAppliedManifestMsg,
		LastTransitionTime: now,
	}
	availableCond := metav1.Condition{
		Type:               placementv1alpha1.ManifestCondTypeAvailable,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 1,
		Reason:             markedAsAvailableReason,
		Message:            "Manifest has been marked as available",
		LastTransitionTime: now,
	}
	failedToApplyCond := metav1.Condition{
		Type:               placementv1alpha1.ManifestCondTypeApplied,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: 1,
		Reason:             markedAsFailedToApplyReason,
		Message:            "Manifest has been marked as failed to be applied",
		LastTransitionTime: now,
	}
	unavailableCond := metav1.Condition{
		Type:               placementv1alpha1.ManifestCondTypeAvailable,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: 1,
		Reason:             markedAsUnavailableReason,
		Message:            "Manifest has been marked as unavailable",
		LastTransitionTime: now,
	}

	manifestStatuses := make([]placementv1alpha1.PerManifestStatus, len(manifestIdentifiers))
	for idx := range manifestIdentifiers {
		ordinal := manifestIdentifiers[idx].Ordinal
		manifestStatuses[idx] = placementv1alpha1.PerManifestStatus{
			Identifier: manifestIdentifiers[idx],
		}

		if diffDetails, failedToApply := failedToApplyOrdinals[ordinal]; failedToApply {
			// Mark the manifest as failed to be applied; the availability check is skipped.
			manifestStatuses[idx].Conditions = []metav1.Condition{failedToApplyCond}
			manifestStatuses[idx].DiffDetails = diffDetails
			continue
		}

		if failedToBecomeAvailableOrdinals.Has(ordinal) {
			// Mark the manifest as applied but unavailable.
			manifestStatuses[idx].Conditions = []metav1.Condition{appliedCond, unavailableCond}
			continue
		}

		// Mark the manifest as applied and available.
		manifestStatuses[idx].Conditions = []metav1.Condition{appliedCond, availableCond}
	}

	// Mark the work object as failed to be applied (if applicable) and unavailable.
	workAppliedCond := metav1.Condition{
		Type:               placementv1alpha1.WorkCondTypeApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: work.Generation,
		Reason:             markedAsAppliedReason,
		Message:            markedAsAppliedWorkMsg,
		LastTransitionTime: now,
	}
	if len(failedToApplyOrdinals) > 0 {
		workAppliedCond.Status = metav1.ConditionFalse
		workAppliedCond.Reason = markedAsFailedToApplyReason
		workAppliedCond.Message = "Work has been marked as failed to be applied"
	}
	work.Status = placementv1alpha1.WorkStatus{
		Conditions: []metav1.Condition{
			workAppliedCond,
			{
				Type:               placementv1alpha1.WorkCondTypeAvailable,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: work.Generation,
				Reason:             markedAsUnavailableReason,
				Message:            "Work has been marked as unavailable",
				LastTransitionTime: now,
			},
		},
		Manifests: manifestStatuses,
	}
	Expect(hubClient.Status().Update(ctx, work)).To(Succeed(), "Failed to mark some manifests in the work object as failed")
}

func removePlacementBinding(bindingNS, bindingName string) {
	binding := newPlacementBindingObj(bindingNS, bindingName)
	Expect(hubClient.Delete(ctx, binding)).To(Succeed(), "Failed to delete the placement binding")
}

// updatePlacementBindingSuspended suspends or resumes a placement binding.
func updatePlacementBindingSuspended(bindingNS, bindingName string, suspended bool) {
	Eventually(func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		binding.GetSpec().Suspended = suspended
		if err := hubClient.Update(ctx, binding); err != nil {
			return fmt.Errorf("failed to update the placement binding: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the suspended flag of the placement binding")
}

// updatePlacementBindingSyncStrategy sets (or, if nil, unsets) the sync strategy of a placement binding.
func updatePlacementBindingSyncStrategy(bindingNS, bindingName string, syncStrategy *placementv1alpha1.SyncStrategy) {
	Eventually(func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		binding.GetSpec().SyncStrategy = syncStrategy.DeepCopy()
		if err := hubClient.Update(ctx, binding); err != nil {
			return fmt.Errorf("failed to update the placement binding: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the sync strategy of the placement binding")
}

// updatePlacementBindingSpec updates the spec of a placement binding with the given mutation in one write.
func updatePlacementBindingSpec(bindingNS, bindingName string, mutate func(spec *placementv1alpha1.PlacementBindingSpec)) {
	Eventually(func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		mutate(binding.GetSpec())
		if err := hubClient.Update(ctx, binding); err != nil {
			return fmt.Errorf("failed to update the placement binding: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the spec of the placement binding")
}

// placementBindingFinalizerRemovedActual verifies that the cleanup finalizer is absent from a placement binding.
func placementBindingFinalizerRemovedActual(bindingNS, bindingName string) func() error {
	return func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		if controllerutil.ContainsFinalizer(binding, workGeneratorCleanupFinalizer) {
			return fmt.Errorf("cleanup finalizer is still present")
		}
		return nil
	}
}

// placementBindingLastProcessedSnapshotUnsetActual verifies that a placement binding has no last processed
// placement resource snapshot name in its status.
func placementBindingLastProcessedSnapshotUnsetActual(bindingNS, bindingName string) func() error {
	return func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		if lastProcessed := binding.GetStatus().LastProcessedResourceSnapshotName; lastProcessed != nil {
			return fmt.Errorf("last processed resource snapshot name is set to %q", *lastProcessed)
		}
		return nil
	}
}

// deleteWork deletes a work object; it is not an error if the work object is already gone.
func deleteWork(workNS, workName string) {
	work := &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: workNS,
			Name:      workName,
		},
	}
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, work))).To(Succeed(), "Failed to delete the work object")
}

// markWorkAsApplied updates the status of a work object to mark it (and all of its manifests) as applied, as the
// KubeFleet member agent would do before it completes the availability checks; no Available conditions are set.
func markWorkAsApplied(workNS, workName string, manifestIdentifiers []placementv1alpha1.ManifestIdentifier) {
	// Retrieve the work object.
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work)).To(Succeed(), "Failed to retrieve the work object")

	now := metav1.Now()
	manifestStatuses := make([]placementv1alpha1.PerManifestStatus, len(manifestIdentifiers))
	for idx := range manifestIdentifiers {
		manifestStatuses[idx] = placementv1alpha1.PerManifestStatus{
			Identifier: manifestIdentifiers[idx],
			Conditions: []metav1.Condition{
				{
					Type:               placementv1alpha1.ManifestCondTypeApplied,
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					Reason:             markedAsAppliedReason,
					Message:            markedAsAppliedManifestMsg,
					LastTransitionTime: now,
				},
			},
		}
	}
	work.Status = placementv1alpha1.WorkStatus{
		Conditions: []metav1.Condition{
			{
				Type:               placementv1alpha1.WorkCondTypeApplied,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: work.Generation,
				Reason:             markedAsAppliedReason,
				Message:            markedAsAppliedWorkMsg,
				LastTransitionTime: now,
			},
		},
		Manifests: manifestStatuses,
	}
	Expect(hubClient.Status().Update(ctx, work)).To(Succeed(), "Failed to mark the work object as applied")
}

// touchWorkStatus updates the status of a work object without changing any processing result, by giving the
// work-level Applied condition a new message; the update still counts as a status change (i.e., it is an event the
// work generator reacts to).
func touchWorkStatus(workNS, workName string) {
	Eventually(func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the work object: %w", err)
		}

		appliedCond := meta.FindStatusCondition(work.Status.Conditions, placementv1alpha1.WorkCondTypeApplied)
		if appliedCond == nil {
			return fmt.Errorf("the work object has no Applied condition")
		}
		appliedCond.Message = fmt.Sprintf("Work has been marked as applied (touched: %s)", utils.RandStr())
		if err := hubClient.Status().Update(ctx, work); err != nil {
			return fmt.Errorf("failed to update the work object status: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to touch the work object status")
}

// setWorkAnnotation sets an annotation on a work object (a metadata-only change).
func setWorkAnnotation(workNS, workName, key, value string) {
	Eventually(func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the work object: %w", err)
		}

		annotations := work.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[key] = value
		work.SetAnnotations(annotations)
		if err := hubClient.Update(ctx, work); err != nil {
			return fmt.Errorf("failed to update the work object: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set the annotation on the work object")
}

// workAnnotationActual verifies that a work object has the given annotation value.
func workAnnotationActual(workNS, workName, key, wantValue string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the work object: %w", err)
		}
		if got := work.GetAnnotations()[key]; got != wantValue {
			return fmt.Errorf("work object annotation %s = %q, want %q", key, got, wantValue)
		}
		return nil
	}
}

// createReservedNamespace creates the reserved namespace of a member cluster on the hub cluster.
func createReservedNamespace(clusterName string) {
	Expect(hubClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf(utils.NamespaceNameFormat, clusterName)},
	})).Should(Succeed(), "Failed to create the reserved namespace for member cluster %s", clusterName)
}

// placementBindingStatusInOrderActual is the same as placementBindingStatusUpdatedActual, except that the failed
// resources are compared in order (i.e., the order that the work generator reports them in is verified).
func placementBindingStatusInOrderActual(bindingNS, bindingName string, wantStatus *placementv1alpha1.PlacementBindingStatus) func() error {
	return func() error {
		binding, err := getPlacementBinding(bindingNS, bindingName)
		if err != nil {
			return err
		}

		wantStatusWithObsGen := wantStatus.DeepCopy()
		for idx := range wantStatusWithObsGen.Conditions {
			wantStatusWithObsGen.Conditions[idx].ObservedGeneration = binding.GetGeneration()
		}
		if diff := cmp.Diff(*binding.GetStatus(), *wantStatusWithObsGen, ignoreFieldConditionLTTMsg); diff != "" {
			return fmt.Errorf("placement binding status diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// createForeignWork creates a work object with arbitrary labels and annotations, i.e., one that is not
// generated by the work generator.
func createForeignWork(workNS, workName string, labels, annotations map[string]string) {
	work := &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   workNS,
			Name:        workName,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	Expect(hubClient.Create(ctx, work)).To(Succeed(), "Failed to create the work object")
}

// placementResourceSnapshotRemovedActual deletes a placement resource snapshot, and verifies that it is gone.
func placementResourceSnapshotRemovedActual(snapshotNS, snapshotName string) func() error {
	return func() error {
		snapshot := newPlacementResourceSnapshotObj(snapshotNS, snapshotName)
		if err := hubClient.Delete(ctx, snapshot); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete the placement resource snapshot: %w", err)
		}

		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: snapshotNS, Name: snapshotName}, snapshot); !apierrors.IsNotFound(err) {
			return fmt.Errorf("placement resource snapshot still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func placementBindingRemovedActual(bindingNS, bindingName string) func() error {
	return func() error {
		binding := newPlacementBindingObj(bindingNS, bindingName)
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: bindingNS, Name: bindingName}, binding); !apierrors.IsNotFound(err) {
			return fmt.Errorf("placement binding still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func workRemovedActual(workNS, workName string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: workNS, Name: workName}, work); !apierrors.IsNotFound(err) {
			return fmt.Errorf("work object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

// scope describes the scope (namespace-scoped or cluster-scoped) of the placement bindings (and their placement
// resource snapshots) that a test runs against.
type scope struct {
	name                string
	bindingNS           string
	bindingNameTemplate string
	policyNameTemplate  string
}

var scopes = []scope{
	{
		name:                "namespace-scoped",
		bindingNS:           appNamespaceName,
		bindingNameTemplate: placementBindingNameTemplate,
		policyNameTemplate:  placementPolicyNameTemplate,
	},
	{
		name:                "cluster-scoped",
		bindingNS:           clusterScopedNS,
		bindingNameTemplate: clusterPlacementBindingNameTemplate,
		policyNameTemplate:  clusterPlacementPolicyNameTemplate,
	},
}

// Every placement resource snapshot that the binding fixture creates carries a NS, a Deployment, and a ConfigMap.
const resourcesPerSnapshot = 3

// snapshotSet is a set of placement resource snapshots that share the same index; the first one is the
// primary snapshot (sub-index 0).
type snapshotSet struct {
	index       int
	names       []string
	resources   [][]placementv1alpha1.SnapshottedResource
	snapshots   []placementv1alpha1.PlacementResourceSnapshotAccessor
	identifiers [][]placementv1alpha1.ManifestIdentifier
}

func (s *snapshotSet) primaryName() string {
	return s.names[0]
}

// resourceCount returns the number of resources across all the snapshots in the set.
func (s *snapshotSet) resourceCount() int32 {
	total := 0
	for idx := range s.identifiers {
		total += len(s.identifiers[idx])
	}
	return int32(total) //nolint:gosec // safe: the counts are tiny.
}

// bindingFixture bundles the objects and the expectations of a placement binding bound to a member cluster.
// Its methods that touch the API server must be called at spec run time (not at tree construction time).
type bindingFixture struct {
	scope       scope
	clusterName string
	bindingName string
	policyName  string
	workNS      string

	// The snapshot sets created (or prepared) so far, keyed by snapshot index.
	sets map[int]*snapshotSet
	// The names of the stray snapshots created so far; see createStraySnapshot.
	strayNames []string
	// The UIDs of the work objects, keyed by work name; recorded by tests that check if work objects are
	// updated in place or re-created.
	workUIDs map[string]types.UID
	// The status and the resource version of the binding; recorded by tests that check if the binding is left
	// untouched.
	status          *placementv1alpha1.PlacementBindingStatus
	resourceVersion string
}

func newBindingFixture(sc scope, clusterName string) *bindingFixture {
	return newBindingFixtureWithNames(sc, clusterName,
		fmt.Sprintf(sc.bindingNameTemplate, utils.RandStr()), fmt.Sprintf(sc.policyNameTemplate, utils.RandStr()))
}

// newBindingFixtureWithNames is the same as newBindingFixture, except that it uses the given binding and placement
// policy names.
func newBindingFixtureWithNames(sc scope, clusterName, bindingName, policyName string) *bindingFixture {
	return &bindingFixture{
		scope:       sc,
		clusterName: clusterName,
		bindingName: bindingName,
		policyName:  policyName,
		workNS:      fmt.Sprintf(utils.NamespaceNameFormat, clusterName),
		sets:        map[int]*snapshotSet{},
	}
}

// newFixtureOfSamePolicy returns a fixture for another binding of the same placement policy, bound to the given member
// cluster. The two fixtures share their snapshot sets, i.e., snapshots created via one are visible to the other.
func (f *bindingFixture) newFixtureOfSamePolicy(clusterName string) *bindingFixture {
	other := newBindingFixtureWithNames(f.scope, clusterName, fmt.Sprintf(f.scope.bindingNameTemplate, utils.RandStr()), f.policyName)
	other.sets = f.sets
	return other
}

// createSnapshotWithResources creates a single placement resource snapshot of the given index with the given resources
// (and their expected manifest identifiers), as the only snapshot of its index.
func (f *bindingFixture) createSnapshotWithResources(
	index int,
	resources []placementv1alpha1.SnapshottedResource,
	identifiers []placementv1alpha1.ManifestIdentifier,
) *snapshotSet {
	set := &snapshotSet{
		index:       index,
		names:       []string{fmt.Sprintf(placementResourceSnapshotNameTemplate, f.policyName, index)},
		resources:   [][]placementv1alpha1.SnapshottedResource{resources},
		snapshots:   make([]placementv1alpha1.PlacementResourceSnapshotAccessor, 1),
		identifiers: [][]placementv1alpha1.ManifestIdentifier{identifiers},
	}
	f.sets[index] = set
	f.createSnapshotsInSet(set)
	return set
}

// createSnapshots creates `count` placement resource snapshots of the given index, each with its own NS, Deployment,
// and ConfigMap.
func (f *bindingFixture) createSnapshots(index, count int) *snapshotSet {
	set := f.newSnapshotSet(index, count)
	f.createSnapshotsInSet(set)
	return set
}

// newSnapshotSet prepares (but does not create) `count` placement resource snapshots of the given index, each with its
// own NS, Deployment, and ConfigMap; use createSnapshotsInSet to create them.
func (f *bindingFixture) newSnapshotSet(index, count int) *snapshotSet {
	set := &snapshotSet{
		index:       index,
		names:       make([]string, count),
		resources:   make([][]placementv1alpha1.SnapshottedResource, count),
		snapshots:   make([]placementv1alpha1.PlacementResourceSnapshotAccessor, count),
		identifiers: make([][]placementv1alpha1.ManifestIdentifier, count),
	}
	suffix := utils.RandStr()
	for subIdx := 0; subIdx < count; subIdx++ {
		if subIdx == 0 {
			set.names[subIdx] = fmt.Sprintf(placementResourceSnapshotNameTemplate, f.policyName, index)
		} else {
			set.names[subIdx] = fmt.Sprintf(subIndexedPlacementResourceSnapshotNameTemplate, f.policyName, index, subIdx)
		}

		nsName := fmt.Sprintf("ns-%s-%d", suffix, subIdx)
		deployName := fmt.Sprintf(deployNameTemplate, subIdx)
		configMapName := fmt.Sprintf(configMapNameTemplate, subIdx)
		set.resources[subIdx] = []placementv1alpha1.SnapshottedResource{
			nsSnapshottedResource(nsName),
			deploySnapshottedResource(nsName, deployName),
			configMapSnapshottedResource(nsName, configMapName),
		}
		set.identifiers[subIdx] = []placementv1alpha1.ManifestIdentifier{
			nsManifestIdentifier(0, nsName),
			deployManifestIdentifier(1, nsName, deployName),
			configMapManifestIdentifier(2, nsName, configMapName),
		}
	}
	f.sets[index] = set
	return set
}

// createSnapshotsInSet creates the placement resource snapshots of the given sub-indices (all of them if none is
// given) in a prepared snapshot set. The snapshots are created in descending order of their sub-indices, so that the
// primary snapshot comes last, as the placement resource snapshot manager would do. It can also re-create snapshots
// that have been deleted.
func (f *bindingFixture) createSnapshotsInSet(set *snapshotSet, subIdxs ...int) {
	if len(subIdxs) == 0 {
		for subIdx := range set.names {
			subIdxs = append(subIdxs, subIdx)
		}
	}
	slices.Sort(subIdxs)
	slices.Reverse(subIdxs)
	for _, subIdx := range subIdxs {
		set.snapshots[subIdx] = createSubIndexedPlacementResourceSnapshot(f.scope.bindingNS, set.names[subIdx], f.policyName,
			set.index, subIdx, len(set.names), set.resources[subIdx]...)
	}
}

// deleteSnapshot deletes a placement resource snapshot of a snapshot set, and waits until the deletion is observed
// in the cache.
func (f *bindingFixture) deleteSnapshot(setIdx, subIdx int) {
	snapshotName := f.sets[setIdx].names[subIdx]
	Eventually(placementResourceSnapshotRemovedActual(f.scope.bindingNS, snapshotName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
}

// createStraySnapshot creates a stray placement resource snapshot of the given index and sub-index under a name
// different from the ones the fixture uses, e.g., one left over from a previous placement policy of the same name.
// If it is a primary snapshot (sub-index 0), it carries the given count label. It returns the name of the snapshot.
func (f *bindingFixture) createStraySnapshot(index, subIdx, count int, contentsHash string) string {
	snapshotName := fmt.Sprintf(straySnapshotNameTemplate, f.policyName, index, subIdx, utils.RandStr())
	createSubIndexedPlacementResourceSnapshotWithHash(f.scope.bindingNS, snapshotName, f.policyName, index, subIdx, count, contentsHash,
		nsSnapshottedResource(fmt.Sprintf(nsNameTemplate, utils.RandStr())))
	f.strayNames = append(f.strayNames, snapshotName)
	return snapshotName
}

// deleteStraySnapshot deletes a stray placement resource snapshot, and waits until the deletion is observed in the
// cache.
func (f *bindingFixture) deleteStraySnapshot(snapshotName string) {
	Eventually(placementResourceSnapshotRemovedActual(f.scope.bindingNS, snapshotName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the stray placement resource snapshot")
}

// createBinding creates the placement binding, bound to the primary snapshot of the given snapshot set.
func (f *bindingFixture) createBinding(setIdx int) {
	createPlacementBinding(f.scope.bindingNS, f.bindingName, f.policyName, f.clusterName, f.sets[setIdx].primaryName())
}

// workName returns the name of the work object derived from the snapshot with the given sub-index.
func (f *bindingFixture) workName(subIdx int) string {
	binding := newPlacementBindingObj(f.scope.bindingNS, f.bindingName)
	binding.SetSpec(placementv1alpha1.PlacementBindingSpec{PlacementPolicyName: f.policyName})
	return uniqueNameForWorkDerivedFromPlacementResourceSnapshot(binding, subIdx == 0,
		&placementResourceSnapshotDerivedFromSourceFormatter{snapshotSubIdx: strconv.Itoa(subIdx)})
}

// worksMatchActual verifies that the work objects of the binding are exactly those derived from the given snapshot
// set, with the given sync strategy.
func (f *bindingFixture) worksMatchActual(setIdx int, syncStrategy *placementv1alpha1.SyncStrategy) func() error {
	return func() error {
		set := f.sets[setIdx]

		// The secondary work objects should be owned by the primary work object; retrieve its current UID as it
		// changes whenever the primary work object is re-created.
		primaryWork := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Namespace: f.workNS, Name: f.workName(0)}, primaryWork); err != nil {
			return fmt.Errorf("failed to retrieve the primary work object: %w", err)
		}
		ownerRef := &metav1.OwnerReference{
			APIVersion:         placementv1alpha1.GroupVersion.String(),
			Kind:               workKind,
			Name:               primaryWork.Name,
			UID:                primaryWork.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		}

		wantWorks := make([]placementv1alpha1.Work, len(set.names))
		for subIdx := range set.names {
			wantWorks[subIdx] = wantWorkForPlacementResourceSnapshot(f.workNS, f.workName(subIdx), f.scope.bindingNS, f.bindingName, f.policyName,
				set.primaryName(), subIdx, len(set.names), ownerRef, set.snapshots[subIdx])
			wantWorks[subIdx].Spec.SyncStrategy = syncStrategy.DeepCopy()
		}
		return worksGeneratedActual(f.clusterName, f.scope.bindingNS, f.bindingName, wantWorks)()
	}
}

// noWorksActual verifies that there is no work object for the binding.
func (f *bindingFixture) noWorksActual() func() error {
	return worksGeneratedActual(f.clusterName, f.scope.bindingNS, f.bindingName, nil)
}

func (f *bindingFixture) waitingStatusActual(setIdx int) func() error {
	set := f.sets[setIdx]
	return placementBindingStatusUpdatedActual(f.scope.bindingNS, f.bindingName,
		wantPlacementBindingStatusWaitingForSync(set.resourceCount(), set.primaryName()))
}

func (f *bindingFixture) syncedStatusActual(setIdx int) func() error {
	set := f.sets[setIdx]
	return placementBindingStatusUpdatedActual(f.scope.bindingNS, f.bindingName,
		wantPlacementBindingStatusSyncedAndAvailable(set.resourceCount(), set.primaryName()))
}

// markWorks marks the work objects with the given sub-indices (all of them if none is given) as applied and
// available.
func (f *bindingFixture) markWorks(setIdx int, subIdxs ...int) {
	set := f.sets[setIdx]
	if len(subIdxs) == 0 {
		for subIdx := range set.names {
			subIdxs = append(subIdxs, subIdx)
		}
	}
	for _, subIdx := range subIdxs {
		markWorkAsAppliedAndAvailable(f.workNS, f.workName(subIdx), set.identifiers[subIdx])
	}
}

// listWorkUIDs returns the UIDs of the work objects of the binding, keyed by work name.
func (f *bindingFixture) listWorkUIDs() (map[string]types.UID, error) {
	workList := &placementv1alpha1.WorkList{}
	if err := hubClient.List(ctx, workList,
		client.InNamespace(f.workNS),
		client.MatchingLabels{
			placementv1alpha1.WorkOwnerNamespaceLabelKey:          f.scope.bindingNS,
			placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: workOwnerLabelValue(f.bindingName),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to list work objects: %w", err)
	}
	uids := make(map[string]types.UID, len(workList.Items))
	for idx := range workList.Items {
		uids[workList.Items[idx].Name] = workList.Items[idx].UID
	}
	return uids, nil
}

// recordWorkUIDs records the current UIDs of the work objects of the binding.
func (f *bindingFixture) recordWorkUIDs() {
	uids, err := f.listWorkUIDs()
	Expect(err).To(Succeed(), "Failed to list the work objects")
	f.workUIDs = uids
}

// recordStatus records the current status and resource version of the binding.
func (f *bindingFixture) recordStatus() {
	binding, err := getPlacementBinding(f.scope.bindingNS, f.bindingName)
	Expect(err).To(Succeed(), "Failed to retrieve the placement binding")
	f.status = binding.GetStatus().DeepCopy()
	f.resourceVersion = binding.GetResourceVersion()
}

// statusUnchangedActual verifies that the status of the binding matches the one last recorded, including the
// observed generations of the conditions.
func (f *bindingFixture) statusUnchangedActual() func() error {
	return func() error {
		return placementBindingStatusMatchesActual(f.scope.bindingNS, f.bindingName, f.status)()
	}
}

// resourceVersionUnchangedActual verifies that the binding has the resource version last recorded, i.e., that it
// has not been written to (spec or status) since.
func (f *bindingFixture) resourceVersionUnchangedActual() func() error {
	return func() error {
		binding, err := getPlacementBinding(f.scope.bindingNS, f.bindingName)
		if err != nil {
			return err
		}
		if binding.GetResourceVersion() != f.resourceVersion {
			return fmt.Errorf("placement binding resource version = %s, want %s", binding.GetResourceVersion(), f.resourceVersion)
		}
		return nil
	}
}

// invalidSnapshotsStatusActual verifies the given status, preserving explicitly supplied observed generations, and
// checks that the Synchronized condition reports invalid placement resource snapshots with the given details.
func (f *bindingFixture) invalidSnapshotsStatusActual(wantStatus *placementv1alpha1.PlacementBindingStatus, wantMsgSubstr string) func() error {
	return func() error {
		binding, err := getPlacementBinding(f.scope.bindingNS, f.bindingName)
		if err != nil {
			return err
		}
		wantStatusWithObsGen := wantStatus.DeepCopy()
		for idx := range wantStatusWithObsGen.Conditions {
			if wantStatusWithObsGen.Conditions[idx].ObservedGeneration == 0 {
				wantStatusWithObsGen.Conditions[idx].ObservedGeneration = binding.GetGeneration()
			}
		}
		if err := placementBindingStatusMatchesActual(f.scope.bindingNS, f.bindingName, wantStatusWithObsGen)(); err != nil {
			return err
		}
		if err := placementBindingSyncCondMessageActual(f.scope.bindingNS, f.bindingName, invalidSnapshotsMsgPrefix)(); err != nil {
			return err
		}
		return placementBindingSyncCondMessageActual(f.scope.bindingNS, f.bindingName, wantMsgSubstr)()
	}
}

// worksMatchBindingStrategyActual is the same as worksMatchActual, except that the work objects are expected to carry
// the sync strategy of the binding as written to the API server (which might carry defaulted fields).
func (f *bindingFixture) worksMatchBindingStrategyActual(setIdx int) func() error {
	return func() error {
		binding, err := getPlacementBinding(f.scope.bindingNS, f.bindingName)
		if err != nil {
			return err
		}
		syncStrategy := binding.GetSpec().SyncStrategy
		if syncStrategy == nil {
			return fmt.Errorf("the sync strategy has not been set on the placement binding")
		}
		return f.worksMatchActual(setIdx, syncStrategy)()
	}
}

// expectWorksNotRecreated verifies that the work objects recorded earlier still exist with the same UIDs.
func (f *bindingFixture) expectWorksNotRecreated() {
	uids, err := f.listWorkUIDs()
	Expect(err).To(Succeed(), "Failed to list the work objects")
	for workName, wantUID := range f.workUIDs {
		if uids[workName] != wantUID {
			Fail(fmt.Sprintf("work object %s has been re-created or removed (UID %s, want %s)", workName, uids[workName], wantUID))
		}
	}
}

// updateSpec updates the spec of the binding with the given mutation in one write.
func (f *bindingFixture) updateSpec(mutate func(spec *placementv1alpha1.PlacementBindingSpec)) {
	updatePlacementBindingSpec(f.scope.bindingNS, f.bindingName, mutate)
}

// registerWorkGenerationSpecs registers the specs that generate the work objects from the given snapshot set and bring
// the binding to the waiting-for-synchronization state.
func (f *bindingFixture) registerWorkGenerationSpecs(setIdx int) {
	It("should add cleanup finalizer to the placement binding", func() {
		finalizerAddedActual := placementBindingFinalizerAddedActual(f.scope.bindingNS, f.bindingName)
		Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the placement binding")
	})

	It("should generate the work objects", func() {
		Eventually(f.worksMatchActual(setIdx, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
	})

	It("should refresh the placement binding status", func() {
		Eventually(f.waitingStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
	})
}

// registerBaselineSpecs registers the specs that generate the work objects from the given snapshot set and bring the
// binding to the synchronized and available state.
func (f *bindingFixture) registerBaselineSpecs(setIdx int) {
	f.registerWorkGenerationSpecs(setIdx)

	It("can mark the work objects as applied and available", func() {
		f.markWorks(setIdx)
	})

	It("should refresh the placement binding status", func() {
		Eventually(f.syncedStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
		f.recordStatus()
	})
}

// markWorksAsApplied marks all the work objects of the given snapshot set as applied (but not yet checked for
// availability).
func (f *bindingFixture) markWorksAsApplied(setIdx int) {
	set := f.sets[setIdx]
	for subIdx := range set.names {
		markWorkAsApplied(f.workNS, f.workName(subIdx), set.identifiers[subIdx])
	}
}

// workGeneration returns the generation of the work object derived from the snapshot with the given sub-index.
func (f *bindingFixture) workGeneration(subIdx int) int64 {
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Namespace: f.workNS, Name: f.workName(subIdx)}, work)).To(Succeed(), "Failed to retrieve the work object")
	return work.Generation
}

// syncCondTransitionedSinceRecordActual verifies that the Synchronized condition of the binding has transitioned
// (i.e., has a later last transition time) since the status was last recorded.
func (f *bindingFixture) syncCondTransitionedSinceRecordActual() func() error {
	return func() error {
		recordedCond := meta.FindStatusCondition(f.status.Conditions, placementv1alpha1.PlacementBindingCondTypeSynchronized)
		if recordedCond == nil {
			return fmt.Errorf("the recorded status has no Synchronized condition")
		}

		binding, err := getPlacementBinding(f.scope.bindingNS, f.bindingName)
		if err != nil {
			return err
		}
		syncCond := meta.FindStatusCondition(binding.GetStatus().Conditions, placementv1alpha1.PlacementBindingCondTypeSynchronized)
		if syncCond == nil {
			return fmt.Errorf("the Synchronized condition is not found on the placement binding")
		}
		if !syncCond.LastTransitionTime.After(recordedCond.LastTransitionTime.Time) {
			return fmt.Errorf("the Synchronized condition last transitioned at %v, want a time after %v",
				syncCond.LastTransitionTime, recordedCond.LastTransitionTime)
		}
		return nil
	}
}

// syncWorks generates the work objects from the given snapshot set and brings the binding to the synchronized and
// available state in one go; it is for bindings that are not the focus of a test.
func (f *bindingFixture) syncWorks(setIdx int) {
	Eventually(f.worksMatchActual(setIdx, nil), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to generate the work objects")
	Eventually(f.waitingStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
	f.markWorks(setIdx)
	Eventually(f.syncedStatusActual(setIdx), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to refresh the placement binding status")
}

// teardown removes the binding, its leftover work objects, and the snapshots.
func (f *bindingFixture) teardown() {
	Expect(client.IgnoreNotFound(hubClient.Delete(ctx, newPlacementBindingObj(f.scope.bindingNS, f.bindingName)))).To(Succeed(), "Failed to delete the placement binding")
	Eventually(placementBindingRemovedActual(f.scope.bindingNS, f.bindingName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement binding")

	// The work generator deletes only the primary work object upon cleanup, and the secondary work objects are left to
	// K8s' garbage collection, which does not run in the test environment; delete any leftover explicitly.
	uids, err := f.listWorkUIDs()
	Expect(err).To(Succeed(), "Failed to list the work objects")
	for workName := range uids {
		deleteWork(f.workNS, workName)
		Eventually(workRemovedActual(f.workNS, workName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the work object")
	}

	for _, set := range f.sets {
		for _, snapshotName := range set.names {
			Eventually(placementResourceSnapshotRemovedActual(f.scope.bindingNS, snapshotName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the placement resource snapshot")
		}
	}
	for _, snapshotName := range f.strayNames {
		Eventually(placementResourceSnapshotRemovedActual(f.scope.bindingNS, snapshotName), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the stray placement resource snapshot")
	}
}
