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
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	nsNameTemplate                                  = "ns-%s"

	deployName    = "deploy-1"
	configMapName = "configmap-1"

	indexedNSNameTemplate = "ns-%d"
	deployNameTemplate    = "deploy-%d"
	configMapNameTemplate = "configmap-%d"

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
)

// The namespace of cluster-scoped objects (i.e., none); the test helpers use it to tell cluster-scoped placement
// bindings and placement resource snapshots apart from their namespace-scoped counterparts.
const clusterScopedNS = ""

const (
	eventuallyDuration   = time.Second * 10
	eventuallyInterval   = time.Second * 1
	consistentlyDuration = time.Second * 5
	consistentlyInterval = time.Millisecond * 500

	// The work generator does not watch placement resource snapshots; when it fails to process a placement binding
	// due to inconsistent snapshots, it only picks up later snapshot changes when it retries the reconciliation,
	// which is subject to exponential backoff. Checks that depend on such retries use a longer timeout.
	eventuallyDurationForBackoffRetries = time.Minute
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
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  ownerPlacementPolicyName,
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    strconv.Itoa(index),
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIndex),
	}
	if subIndex == 0 {
		labels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = strconv.Itoa(subIndexedSnapshotCount)
	}
	snapshot.SetLabels(labels)
	snapshot.SetAnnotations(map[string]string{
		placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: contentsHash,
	})
	snapshot.SetSpec(placementv1alpha1.PlacementResourceSnapshotSpec{
		Resources: resources,
	})
	Expect(hubClient.Create(ctx, snapshot)).To(Succeed(), "Failed to create the placement resource snapshot")
	return snapshot
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
				placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  placementPolicyName,
				placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: bindingName,
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
		// List the work objects owned by the placement binding.
		workList := &placementv1alpha1.WorkList{}
		listOptions := []client.ListOption{
			client.InNamespace(fmt.Sprintf(utils.NamespaceNameFormat, clusterName)),
			client.MatchingLabels{
				placementv1alpha1.WorkOwnerNamespaceLabelKey:          bindingNS,
				placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: bindingName,
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
					Message:            "Manifest has been marked as applied",
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
				Message:            "Work has been marked as applied",
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
		Message:            "Manifest has been marked as applied",
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
		Message:            "Work has been marked as applied",
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
