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

package workapplier

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	testutilsresource "github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

const (
	workNameTemplate              = "work-%s"
	nsNameTemplate                = "ns-%s"
	placementPolicyNameTemplate   = "policy-%s"
	placementBindingNameTemplate  = "binding-%s"
	primarySnapshotNameTemplate   = "snapshot-%s-0"
	deployName                    = "deploy-1"
	configMapName                 = "configmap-1"
	singleWorkLinkedWorkCountAnno = "1"
)

const (
	eventuallyDuration   = time.Second * 10
	eventuallyInterval   = time.Second * 1
	consistentlyDuration = time.Second * 5
	consistentlyInterval = time.Millisecond * 500
)

var (
	ignoreFieldObjectMetaAutoGenFields = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "CreationTimestamp", "Generation", "ResourceVersion", "SelfLink", "UID", "ManagedFields")
	ignoreFieldAppliedWorkStatus       = cmpopts.IgnoreFields(placementv1alpha1.AppliedWork{}, "Status")
	ignoreFieldConditionLTTMsg         = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
	ignoreFieldDiffDetailsObsTime      = cmpopts.IgnoreFields(placementv1alpha1.DiffDetails{}, "FirstDiffedObservedTimestamp")
)

var (
	ns = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns-1",
		},
	}

	configMap = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: "ns-1",
		},
		Data: map[string]string{
			"foo": "bar",
		},
	}

	deploy = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: "ns-1",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx",
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
)

// buildWorkObject builds a work object with the given work name/namespace, owner placement policy/binding,
// primary placement resource snapshot, sync strategy, and raw manifest JSONs.
//
// Note that the returned work object does not have the linked work count annotation set; the annotation
// is only present on primary work objects.
func buildWorkObject(
	workName, memberClusterReservedNSName string,
	placementPolicyName, placementBindingName, primarySnapshotName string,
	syncStrategy *placementv1alpha1.SyncStrategy,
	rawManifestJSON ...[]byte,
) *placementv1alpha1.Work {
	manifests := make([]placementv1alpha1.Manifest, len(rawManifestJSON))
	for idx := range rawManifestJSON {
		manifests[idx] = placementv1alpha1.Manifest{
			RawExtension: runtime.RawExtension{
				Raw: rawManifestJSON[idx],
			},
		}
	}

	return &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberClusterReservedNSName,
			Labels: map[string]string{
				// An empty owner namespace signals cluster-scoped owners.
				placementv1alpha1.WorkOwnerNamespaceLabelKey:          "",
				placementv1alpha1.WorkOwnedByPlacementPolicyLabelKey:  placementPolicyName,
				placementv1alpha1.WorkOwnedByPlacementBindingLabelKey: placementBindingName,
			},
			Annotations: map[string]string{
				placementv1alpha1.WorkOwnedByPlacementPolicyAnnotationKey:                   placementPolicyName,
				placementv1alpha1.WorkOwnedByPlacementBindingAnnotationKey:                  placementBindingName,
				placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey: primarySnapshotName,
			},
		},
		Spec: placementv1alpha1.WorkSpec{
			Manifests:    manifests,
			SyncStrategy: syncStrategy,
		},
	}
}

// createWorkObject creates a new (primary) work object with the given work name/namespace, sync strategy,
// and raw manifest JSONs.
//
// The work object is set up as the only work object linked to a dedicated (cluster-scoped) placement policy and
// placement binding, so that work objects created in different test cases never get linked with each other.
func createWorkObject(workName, memberClusterReservedNSName string, syncStrategy *placementv1alpha1.SyncStrategy, rawManifestJSON ...[]byte) {
	work := buildWorkObject(
		workName, memberClusterReservedNSName,
		fmt.Sprintf(placementPolicyNameTemplate, workName),
		fmt.Sprintf(placementBindingNameTemplate, workName),
		fmt.Sprintf(primarySnapshotNameTemplate, workName),
		syncStrategy,
		rawManifestJSON...,
	)
	work.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = singleWorkLinkedWorkCountAnno
	Expect(hubClient.Create(ctx, work)).To(Succeed(), "Failed to create the Work object")
}

func marshalK8sObjJSON(obj runtime.Object) []byte {
	json, err := testutilsresource.MarshalRuntimeObjToJSONForTest(obj)
	Expect(err).To(BeNil(), "Failed to marshal the k8s object to JSON")
	return json
}

func workFinalizerAddedActual(workNS, workName string) func() error {
	return func() error {
		// Retrieve the Work object.
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		// Check that the cleanup finalizer has been added.
		if !controllerutil.ContainsFinalizer(work, workApplierCleanupFinalizer) {
			return fmt.Errorf("cleanup finalizer has not been added")
		}
		return nil
	}
}

func appliedWorkCreatedActual(memberClient client.Client, workNS, workName string) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &placementv1alpha1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		wantAppliedWork := &placementv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: workName,
			},
			Spec: placementv1alpha1.AppliedWorkSpec{
				WorkName:      workName,
				WorkNamespace: workNS,
			},
		}
		if diff := cmp.Diff(
			appliedWork, wantAppliedWork,
			ignoreFieldObjectMetaAutoGenFields,
			ignoreFieldAppliedWorkStatus,
		); diff != "" {
			return fmt.Errorf("appliedWork diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func prepareAppliedWorkOwnerRef(memberClient client.Client, workName string) *metav1.OwnerReference {
	// Retrieve the AppliedWork object.
	appliedWork := &placementv1alpha1.AppliedWork{}
	Expect(memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork)).To(Succeed(), "Failed to retrieve the AppliedWork object")

	// Prepare the expected OwnerReference.
	return &metav1.OwnerReference{
		APIVersion:         placementv1alpha1.GroupVersion.String(),
		Kind:               "AppliedWork",
		Name:               appliedWork.Name,
		UID:                appliedWork.GetUID(),
		BlockOwnerDeletion: ptr.To(true),
	}
}

func regularNSObjectAppliedActual(memberClient client.Client, nsName string, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		// Retrieve the NS object.
		gotNS := &corev1.Namespace{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, gotNS); err != nil {
			return fmt.Errorf("failed to retrieve the NS object: %w", err)
		}

		// Check that the NS object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantNS := ns.DeepCopy()
		wantNS.TypeMeta = metav1.TypeMeta{}
		wantNS.Name = nsName
		wantNS.OwnerReferences = []metav1.OwnerReference{
			*appliedWorkOwnerRef,
		}

		rebuiltGotNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:            gotNS.Name,
				OwnerReferences: gotNS.OwnerReferences,
			},
		}

		if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
			return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func regularDeploymentObjectAppliedActual(memberClient client.Client, nsName, deployName string, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		// Retrieve the Deployment object.
		gotDeploy := &appsv1.Deployment{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, gotDeploy); err != nil {
			return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
		}

		// Check that the Deployment object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantDeploy := deploy.DeepCopy()
		wantDeploy.TypeMeta = metav1.TypeMeta{}
		wantDeploy.Namespace = nsName
		wantDeploy.Name = deployName
		wantDeploy.OwnerReferences = []metav1.OwnerReference{
			*appliedWorkOwnerRef,
		}

		if len(gotDeploy.Spec.Template.Spec.Containers) != 1 {
			return fmt.Errorf("number of containers in the Deployment object, got %d, want %d", len(gotDeploy.Spec.Template.Spec.Containers), 1)
		}
		if len(gotDeploy.Spec.Template.Spec.Containers[0].Ports) != 1 {
			return fmt.Errorf("number of ports in the first container, got %d, want %d", len(gotDeploy.Spec.Template.Spec.Containers[0].Ports), 1)
		}
		rebuiltGotDeploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       gotDeploy.Namespace,
				Name:            gotDeploy.Name,
				OwnerReferences: gotDeploy.OwnerReferences,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: gotDeploy.Spec.Replicas,
				Selector: gotDeploy.Spec.Selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": gotDeploy.Spec.Template.Labels["app"],
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  gotDeploy.Spec.Template.Spec.Containers[0].Name,
								Image: gotDeploy.Spec.Template.Spec.Containers[0].Image,
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: gotDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
									},
								},
							},
						},
					},
				},
			},
		}
		if diff := cmp.Diff(rebuiltGotDeploy, wantDeploy); diff != "" {
			return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func regularConfigMapObjectAppliedActual(memberClient client.Client, nsName, configMapName string, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		// Retrieve the ConfigMap object.
		gotConfigMap := &corev1.ConfigMap{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, gotConfigMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap object: %w", err)
		}

		// Check that the ConfigMap object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantConfigMap := configMap.DeepCopy()
		wantConfigMap.TypeMeta = metav1.TypeMeta{}
		wantConfigMap.Namespace = nsName
		wantConfigMap.Name = configMapName
		wantConfigMap.OwnerReferences = []metav1.OwnerReference{
			*appliedWorkOwnerRef,
		}

		rebuiltGotConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       gotConfigMap.Namespace,
				Name:            gotConfigMap.Name,
				OwnerReferences: gotConfigMap.OwnerReferences,
			},
			Data: gotConfigMap.Data,
		}
		if diff := cmp.Diff(rebuiltGotConfigMap, wantConfigMap); diff != "" {
			return fmt.Errorf("configMap diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func markDeploymentAsAvailable(memberClient client.Client, nsName, deployName string) {
	// Retrieve the Deployment object.
	gotDeploy := &appsv1.Deployment{}
	Expect(memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, gotDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")

	// Mark the Deployment object as available.
	now := metav1.Now()
	requiredReplicas := int32(1)
	if gotDeploy.Spec.Replicas != nil {
		requiredReplicas = *gotDeploy.Spec.Replicas
	}
	gotDeploy.Status = appsv1.DeploymentStatus{
		ObservedGeneration:  gotDeploy.Generation,
		Replicas:            requiredReplicas,
		UpdatedReplicas:     requiredReplicas,
		ReadyReplicas:       requiredReplicas,
		AvailableReplicas:   requiredReplicas,
		UnavailableReplicas: 0,
		Conditions: []appsv1.DeploymentCondition{
			{
				Type:               appsv1.DeploymentAvailable,
				Status:             corev1.ConditionTrue,
				Reason:             "MarkedAsAvailable",
				Message:            "Deployment has been marked as available",
				LastUpdateTime:     now,
				LastTransitionTime: now,
			},
		},
	}
	Expect(memberClient.Status().Update(ctx, gotDeploy)).To(Succeed(), "Failed to mark the Deployment object as available")
}

func workStatusUpdated(
	workNS, workName string,
	workConds []metav1.Condition,
	manifestStatuses []placementv1alpha1.PerManifestStatus,
) func() error {
	return func() error {
		// Retrieve the Work object.
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		// Prepare the expected Work object status.

		// Update the conditions with the observed generation.
		//
		// Note that the observed generation of a manifest condition is that of an applied
		// resource, not that of the Work object.
		for idx := range workConds {
			workConds[idx].ObservedGeneration = work.Generation
		}
		wantWorkStatus := placementv1alpha1.WorkStatus{
			Conditions: workConds,
			Manifests:  manifestStatuses,
		}

		// Check that the Work object status has been updated as expected.
		if diff := cmp.Diff(
			work.Status, wantWorkStatus,
			ignoreFieldConditionLTTMsg,
			ignoreFieldDiffDetailsObsTime,
		); diff != "" {
			return fmt.Errorf("work status diff (-got, +want):\n%s", diff)
		}
		return nil
	}
}

func appliedWorkStatusUpdated(memberClient client.Client, workName string, appliedResources []placementv1alpha1.AppliedResource) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &placementv1alpha1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		// Prepare the expected AppliedWork object status.
		wantAppliedWorkStatus := placementv1alpha1.AppliedWorkStatus{
			AppliedResources: appliedResources,
		}
		if diff := cmp.Diff(appliedWork.Status, wantAppliedWorkStatus); diff != "" {
			return fmt.Errorf("appliedWork status diff (-got, +want):\n%s", diff)
		}
		return nil
	}
}

func deleteWorkObject(workName, memberClusterReservedNSName string) {
	work := &placementv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberClusterReservedNSName,
		},
	}
	Expect(hubClient.Delete(ctx, work)).To(Succeed(), "Failed to delete the Work object")
}

// updateWorkObject replaces the manifests and the sync strategy of an existing work object.
func updateWorkObject(workName, memberClusterReservedNSName string, syncStrategy *placementv1alpha1.SyncStrategy, rawManifestJSON ...[]byte) {
	manifests := make([]placementv1alpha1.Manifest, len(rawManifestJSON))
	for idx := range rawManifestJSON {
		manifests[idx] = placementv1alpha1.Manifest{
			RawExtension: runtime.RawExtension{
				Raw: rawManifestJSON[idx],
			},
		}
	}

	Eventually(func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberClusterReservedNSName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		work.Spec.Manifests = manifests
		work.Spec.SyncStrategy = syncStrategy
		// The work applier might update the work object (e.g., its status) at the same time; retry on conflicts.
		return hubClient.Update(ctx, work)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the Work object")
}

// updateLinkedWorkObject replaces the manifests and the sync strategy of an existing work object, and links it
// to the given primary placement resource snapshot; this mimics how KubeFleet refreshes work objects during a
// rollout.
func updateLinkedWorkObject(workName, memberClusterReservedNSName, primarySnapshotName string, syncStrategy *placementv1alpha1.SyncStrategy, rawManifestJSON ...[]byte) {
	manifests := make([]placementv1alpha1.Manifest, len(rawManifestJSON))
	for idx := range rawManifestJSON {
		manifests[idx] = placementv1alpha1.Manifest{
			RawExtension: runtime.RawExtension{
				Raw: rawManifestJSON[idx],
			},
		}
	}

	Eventually(func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberClusterReservedNSName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		work.Annotations[placementv1alpha1.WorkLinkedToPrimaryPlacementResourceSnapshotAnnotationKey] = primarySnapshotName
		work.Spec.Manifests = manifests
		work.Spec.SyncStrategy = syncStrategy
		// The work applier might update the work object (e.g., its status) at the same time; retry on conflicts.
		return hubClient.Update(ctx, work)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the Work object")
}

func appliedWorkBeingDeletedInForegroundActual(memberClient client.Client, workName string) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &placementv1alpha1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		// Check that the AppliedWork object has been marked for deletion with the foreground propagation policy.
		//
		// Note that as there are no real built-in controllers (the GC controller) in this test environment, the
		// AppliedWork object will stay in the pending deletion state with the foreground deletion finalizer.
		if appliedWork.DeletionTimestamp.IsZero() {
			return fmt.Errorf("appliedWork object has not been marked for deletion")
		}
		if !controllerutil.ContainsFinalizer(appliedWork, metav1.FinalizerDeleteDependents) {
			return fmt.Errorf("appliedWork object is not being deleted in the foreground (finalizers: %v)", appliedWork.Finalizers)
		}
		return nil
	}
}

// appliedWorkOwnerRefUnblockedActual checks that the given object is still owned by the AppliedWork object,
// but the owner reference no longer blocks the deletion of the AppliedWork object.
func appliedWorkOwnerRefUnblockedActual(memberClient client.Client, obj client.Object, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		if err := memberClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return fmt.Errorf("failed to retrieve the object: %w", err)
		}

		wantOwnerRef := appliedWorkOwnerRef.DeepCopy()
		wantOwnerRef.BlockOwnerDeletion = ptr.To(false)
		wantOwnerRefs := []metav1.OwnerReference{*wantOwnerRef}
		if diff := cmp.Diff(obj.GetOwnerReferences(), wantOwnerRefs); diff != "" {
			return fmt.Errorf("owner references diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// workFinalizerRemovedActual checks that the work object still exists, but no longer has the cleanup finalizer.
func workFinalizerRemovedActual(workNS, workName string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}
		if controllerutil.ContainsFinalizer(work, workApplierCleanupFinalizer) {
			return fmt.Errorf("cleanup finalizer has not been removed")
		}
		return nil
	}
}

// appliedWorkAbsentActual checks that the AppliedWork object does not exist in the member cluster.
//
// Unlike appliedWorkRemovedActual, this function does not attempt to remove any finalizer from the object.
func appliedWorkAbsentActual(memberClient client.Client, workName string) func() error {
	return func() error {
		appliedWork := &placementv1alpha1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); !apierrors.IsNotFound(err) {
			return fmt.Errorf("appliedWork object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func appliedWorkRemovedActual(memberClient client.Client, workName string) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &placementv1alpha1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); err != nil {
			if apierrors.IsNotFound(err) {
				// The AppliedWork object has been deleted, which is expected.
				return nil
			}
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}
		if !appliedWork.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(appliedWork, metav1.FinalizerDeleteDependents) {
			// The AppliedWork object is being deleted, but the finalizer is still present. Remove the finalizer as there
			// are no real built-in controllers in this test environment to handle garbage collection.
			controllerutil.RemoveFinalizer(appliedWork, metav1.FinalizerDeleteDependents)
			Expect(memberClient.Update(ctx, appliedWork)).To(Succeed(), "Failed to remove the finalizer from the AppliedWork object")
		}
		return fmt.Errorf("appliedWork object still exists")
	}
}

func regularDeployRemovedActual(memberClient client.Client, nsName, deployName string) func() error {
	return func() error {
		// Delete the Deployment object manually, as there are no real built-in controllers in this
		// test environment to handle garbage collection.
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      deployName,
			},
		}
		if err := memberClient.Delete(ctx, deploy); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete the Deployment object: %w", err)
		}

		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, deploy); !apierrors.IsNotFound(err) {
			return fmt.Errorf("deployment object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func regularConfigMapRemovedActual(memberClient client.Client, nsName, configMapName string) func() error {
	return func() error {
		// Delete the ConfigMap object manually, as there are no real built-in controllers in this
		// test environment to handle garbage collection.
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      configMapName,
			},
		}
		if err := memberClient.Delete(ctx, configMap); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete the ConfigMap object: %w", err)
		}

		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, configMap); !apierrors.IsNotFound(err) {
			return fmt.Errorf("configMap object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

// regularConfigMapAbsentActual checks that the ConfigMap object does not exist in the member cluster.
//
// Unlike regularConfigMapRemovedActual, this function does not attempt to delete the object by itself.
func regularConfigMapAbsentActual(memberClient client.Client, nsName, configMapName string) func() error {
	return func() error {
		configMap := &corev1.ConfigMap{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, configMap); !apierrors.IsNotFound(err) {
			return fmt.Errorf("configMap object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

// regularDeployAbsentActual checks that the Deployment object does not exist in the member cluster.
//
// Unlike regularDeployRemovedActual, this function does not attempt to delete the object by itself.
func regularDeployAbsentActual(memberClient client.Client, nsName, deployName string) func() error {
	return func() error {
		deploy := &appsv1.Deployment{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, deploy); !apierrors.IsNotFound(err) {
			return fmt.Errorf("deployment object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func workRemovedActual(workNS, workName string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}
		return fmt.Errorf("work object still exists (finalizers: %v)", work.Finalizers)
	}
}
