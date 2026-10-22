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
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
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
	configMapDataKey              = "foo"
	singleWorkLinkedWorkCountAnno = "1"
	// A test-only label that nudgeWork sets on manifests to bump the generations of Work objects.
	nudgeLabelKey = "nudge"
	// The UID used in the owner references that link secondary work objects to their primary work objects. The API
	// server only requires the UID to be non-empty, and there is no GC controller in the test environment to check
	// it against the actual primary work object.
	dummyPrimaryWorkUID types.UID = "dummy-primary-work-uid"
	// The message the work applier reports when it finds an object that it does not own and may not take over.
	notTakenOverErrMsg = "no ownership of the object in the member cluster; takeover is needed"
	// The message the work applier reports when it may not take over an object as the object is owned by other
	// sources and co-ownership is disallowed.
	ownedByOthersErrMsg = "the object is already owned by some other source(s) and co-ownership is disallowed"
	// The message the work applier reports when it may not take over an object as the object has been placed by
	// another KubeFleet AppliedWork object.
	placedInDuplicateErrMsg = "the object is already owned by another KubeFleet AppliedWork object"
	// The message the work applier reports when it may not apply to an object it co-owns, as co-ownership has been
	// disallowed since the takeover.
	coOwnershipDisallowedErrMsg = "object is co-owned by multiple objects but co-ownership has been disallowed"

	// A label that the test cases add to objects in the member cluster, to mimic changes made by users or other
	// controllers on fields that KubeFleet does not manage.
	memberOnlyLabelKey   = "member-only"
	memberOnlyLabelValue = "true"

	// The field manager that the test cases use when they write to objects in the member cluster, to mimic
	// changes made by users or other controllers.
	thirdPartyFieldManagerName = "integration-test-manager"
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
			configMapDataKey: "bar",
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

	// An owner reference that points to an object managed by a non-KubeFleet controller. The API server does not
	// verify that owners exist, and there is no GC controller in the test environment to act on the reference.
	externalOwnerRef = metav1.OwnerReference{
		APIVersion: "example.com/v1",
		Kind:       "ExternalOwner",
		Name:       "external-owner",
		UID:        "external-owner-uid",
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

// setPrimaryWorkAsOwner sets the primary work object as the controller owner of a secondary work object, which
// mimics how KubeFleet links secondary work objects to their primary work objects; the work applier relies on
// this owner reference to map events on secondary work objects to their primary work objects.
func setPrimaryWorkAsOwner(secondaryWork *placementv1alpha1.Work, primaryWorkName string) {
	secondaryWork.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         placementv1alpha1.GroupVersion.String(),
			Kind:               placementv1alpha1.WorkKind,
			Name:               primaryWorkName,
			UID:                dummyPrimaryWorkUID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
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
	wantDeploy := deploy.DeepCopy()
	wantDeploy.Namespace = nsName
	wantDeploy.Name = deployName
	wantDeploy.OwnerReferences = []metav1.OwnerReference{
		*appliedWorkOwnerRef,
	}
	return deploymentObjectActual(memberClient, wantDeploy)
}

// deploymentObjectActual checks that the Deployment object in the member cluster has the namespace, name, owner
// references, replicas, selector, and pod template (labels, container names, images, and ports) of the given
// Deployment object, which is expected to feature exactly one container with exactly one port.
func deploymentObjectActual(memberClient client.Client, wantDeploy *appsv1.Deployment) func() error {
	return func() error {
		// Retrieve the Deployment object.
		gotDeploy := &appsv1.Deployment{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: wantDeploy.Namespace, Name: wantDeploy.Name}, gotDeploy); err != nil {
			return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
		}

		// Check that the Deployment object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		rebuiltWantDeploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       wantDeploy.Namespace,
				Name:            wantDeploy.Name,
				OwnerReferences: wantDeploy.OwnerReferences,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: wantDeploy.Spec.Replicas,
				Selector: wantDeploy.Spec.Selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: wantDeploy.Spec.Template.Labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  wantDeploy.Spec.Template.Spec.Containers[0].Name,
								Image: wantDeploy.Spec.Template.Spec.Containers[0].Image,
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: wantDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
									},
								},
							},
						},
					},
				},
			},
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
		if diff := cmp.Diff(rebuiltGotDeploy, rebuiltWantDeploy); diff != "" {
			return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// updateDeploymentInMemberCluster updates a Deployment object in the member cluster with the given mutation, as if
// a user or another controller has made the change, and returns the updated object.
func updateDeploymentInMemberCluster(memberClient client.Client, nsName, deployName string, mutate func(deploy *appsv1.Deployment)) *appsv1.Deployment {
	updatedDeploy := &appsv1.Deployment{}
	Eventually(func() error {
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, updatedDeploy); err != nil {
			return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
		}
		mutate(updatedDeploy)
		// The work applier might update the object at the same time; retry on conflicts.
		return memberClient.Update(ctx, updatedDeploy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the Deployment object in the member cluster")
	return updatedDeploy
}

// updateConfigMapInMemberCluster updates a ConfigMap object in the member cluster with the given mutation, as if a
// user or another controller has made the change, and returns the updated object.
func updateConfigMapInMemberCluster(memberClient client.Client, nsName, configMapName string, mutate func(configMap *corev1.ConfigMap)) *corev1.ConfigMap {
	updatedConfigMap := &corev1.ConfigMap{}
	Eventually(func() error {
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, updatedConfigMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap object: %w", err)
		}
		mutate(updatedConfigMap)
		// The work applier might update the object at the same time; retry on conflicts.
		return memberClient.Update(ctx, updatedConfigMap)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the ConfigMap object in the member cluster")
	return updatedConfigMap
}

// objectLabelsActual checks that an object in the member cluster has exactly the given labels.
func objectLabelsActual(memberClient client.Client, obj client.Object, wantLabels map[string]string) func() error {
	return func() error {
		gotObj, ok := obj.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("failed to copy the object %s", client.ObjectKeyFromObject(obj))
		}
		if err := memberClient.Get(ctx, client.ObjectKeyFromObject(obj), gotObj); err != nil {
			return fmt.Errorf("failed to retrieve the object %s: %w", client.ObjectKeyFromObject(obj), err)
		}

		if diff := cmp.Diff(gotObj.GetLabels(), wantLabels); diff != "" {
			return fmt.Errorf("object %s labels diff (-got +want):\n%s", client.ObjectKeyFromObject(obj), diff)
		}
		return nil
	}
}

func regularConfigMapObjectAppliedActual(memberClient client.Client, nsName, configMapName string, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	wantConfigMap := configMap.DeepCopy()
	wantConfigMap.Namespace = nsName
	wantConfigMap.Name = configMapName
	return configMapObjectAppliedActual(memberClient, wantConfigMap, appliedWorkOwnerRef)
}

// configMapObjectAppliedActual checks that the ConfigMap object has been applied as expected, i.e., it has the
// same namespace, name, and data as the given ConfigMap object, and is owned by the AppliedWork object.
func configMapObjectAppliedActual(memberClient client.Client, configMapToApply *corev1.ConfigMap, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		// Retrieve the ConfigMap object.
		gotConfigMap := &corev1.ConfigMap{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: configMapToApply.Namespace, Name: configMapToApply.Name}, gotConfigMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap object: %w", err)
		}

		// Check that the ConfigMap object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: configMapToApply.Namespace,
				Name:      configMapToApply.Name,
				OwnerReferences: []metav1.OwnerReference{
					*appliedWorkOwnerRef,
				},
			},
			Data: configMapToApply.Data,
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

// configMapObjectActual checks that the ConfigMap object in the member cluster has exactly the namespace, name,
// labels, annotations (sans those reserved for KubeFleet), owner references, and data of the given ConfigMap object.
//
// Unlike configMapObjectAppliedActual, this function compares labels and annotations, and allows any set of owners
// (e.g., when KubeFleet co-owns the object with other sources).
func configMapObjectActual(memberClient client.Client, wantConfigMap *corev1.ConfigMap) func() error {
	return func() error {
		gotConfigMap := &corev1.ConfigMap{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: wantConfigMap.Namespace, Name: wantConfigMap.Name}, gotConfigMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap object: %w", err)
		}

		// To ignore default values and fields managed by KubeFleet (e.g., the KubeFleet annotations) automatically,
		// here the test suite rebuilds the objects.
		rebuiltWantConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       wantConfigMap.Namespace,
				Name:            wantConfigMap.Name,
				Labels:          wantConfigMap.Labels,
				Annotations:     wantConfigMap.Annotations,
				OwnerReferences: wantConfigMap.OwnerReferences,
			},
			Data: wantConfigMap.Data,
		}
		rebuiltGotConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       gotConfigMap.Namespace,
				Name:            gotConfigMap.Name,
				Labels:          gotConfigMap.Labels,
				Annotations:     nonKubeFleetAnnotations(gotConfigMap.Annotations),
				OwnerReferences: gotConfigMap.OwnerReferences,
			},
			Data: gotConfigMap.Data,
		}
		if diff := cmp.Diff(rebuiltGotConfigMap, rebuiltWantConfigMap); diff != "" {
			return fmt.Errorf("configMap diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// nonKubeFleetAnnotations returns the annotations that are not reserved for KubeFleet, or nil if there are none.
func nonKubeFleetAnnotations(annotations map[string]string) map[string]string {
	var filtered map[string]string
	for key, val := range annotations {
		if strings.Contains(key, placementv1alpha1.KubeFleetPrefix) {
			continue
		}
		if filtered == nil {
			filtered = map[string]string{}
		}
		filtered[key] = val
	}
	return filtered
}

// configMapObjectWithDataDigestActual checks that the ConfigMap object in the member cluster has the namespace,
// name, owner references, and data of the given ConfigMap object.
//
// Unlike configMapObjectActual, this function compares the lengths and digests of the data values instead of the
// values themselves, so that the outputs stay readable when the data is large.
func configMapObjectWithDataDigestActual(memberClient client.Client, wantConfigMap *corev1.ConfigMap) func() error {
	digestsOf := func(data map[string]string) map[string]string {
		digests := make(map[string]string, len(data))
		for key, val := range data {
			digests[key] = fmt.Sprintf("len=%d, sha256=%x", len(val), sha256.Sum256([]byte(val)))
		}
		return digests
	}

	return func() error {
		gotConfigMap := &corev1.ConfigMap{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: wantConfigMap.Namespace, Name: wantConfigMap.Name}, gotConfigMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap object: %w", err)
		}

		if diff := cmp.Diff(gotConfigMap.OwnerReferences, wantConfigMap.OwnerReferences); diff != "" {
			return fmt.Errorf("configMap owner references diff (-got +want):\n%s", diff)
		}
		if diff := cmp.Diff(digestsOf(gotConfigMap.Data), digestsOf(wantConfigMap.Data)); diff != "" {
			return fmt.Errorf("configMap data digests diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// objectAnnotationActual checks that an object in the member cluster has the given annotation with the given value
// (which might be empty).
func objectAnnotationActual(memberClient client.Client, obj client.Object, key, wantValue string) func() error {
	return func() error {
		gotObj, ok := obj.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("failed to copy the object %s", client.ObjectKeyFromObject(obj))
		}
		if err := memberClient.Get(ctx, client.ObjectKeyFromObject(obj), gotObj); err != nil {
			return fmt.Errorf("failed to retrieve the object %s: %w", client.ObjectKeyFromObject(obj), err)
		}

		gotValue, found := gotObj.GetAnnotations()[key]
		if !found {
			return fmt.Errorf("object %s does not have the annotation %s", client.ObjectKeyFromObject(obj), key)
		}
		if gotValue != wantValue {
			return fmt.Errorf("object %s annotation %s = %q, want %q", client.ObjectKeyFromObject(obj), key, gotValue, wantValue)
		}
		return nil
	}
}

// fieldManagersActual checks that a field of an object in the member cluster is managed exactly by the given field
// managers, each formatted as `[MANAGER]/[OPERATION]` (e.g., `kubefleet-member-agent/Apply`).
//
// The field is specified as a path in the managed fields format, e.g., `f:data`, `f:foo` for the `foo` key in the
// data of a ConfigMap object.
func fieldManagersActual(memberClient client.Client, obj client.Object, fieldPath []string, wantManagers []string) func() error {
	return func() error {
		gotObj, ok := obj.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("failed to copy the object %s", client.ObjectKeyFromObject(obj))
		}
		if err := memberClient.Get(ctx, client.ObjectKeyFromObject(obj), gotObj); err != nil {
			return fmt.Errorf("failed to retrieve the object %s: %w", client.ObjectKeyFromObject(obj), err)
		}

		gotManagers := []string{}
		for _, managedFieldsEntry := range gotObj.GetManagedFields() {
			if managedFieldsEntry.FieldsV1 == nil {
				continue
			}
			fields := map[string]interface{}{}
			if err := json.Unmarshal(managedFieldsEntry.FieldsV1.Raw, &fields); err != nil {
				return fmt.Errorf("failed to unmarshal the managed fields of manager %s: %w", managedFieldsEntry.Manager, err)
			}

			cur := fields
			found := true
			for _, segment := range fieldPath {
				next, ok := cur[segment].(map[string]interface{})
				if !ok {
					found = false
					break
				}
				cur = next
			}
			if found {
				gotManagers = append(gotManagers, fmt.Sprintf("%s/%s", managedFieldsEntry.Manager, managedFieldsEntry.Operation))
			}
		}

		if diff := cmp.Diff(gotManagers, wantManagers, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
			return fmt.Errorf("object %s field %v managers diff (-got +want):\n%s", client.ObjectKeyFromObject(obj), fieldPath, diff)
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

// workStatusStaleActual checks that the status of a Work object has not been refreshed since the Work object was last
// processed at the given (now outdated) generation, i.e., the Work object has been updated since then, and all of its
// conditions have become stale.
func workStatusStaleActual(
	workNS, workName string,
	staleWorkGeneration int64,
	workConds []metav1.Condition,
	manifestStatuses []placementv1alpha1.PerManifestStatus,
) func() error {
	return func() error {
		// Retrieve the Work object.
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		if work.Generation <= staleWorkGeneration {
			return fmt.Errorf("work generation = %d, want a generation newer than %d", work.Generation, staleWorkGeneration)
		}

		// Prepare the expected Work object status, with all the conditions observed at the outdated generation.
		//
		// Note that the observed generation of a manifest condition is that of an applied
		// resource, not that of the Work object.
		wantWorkConds := make([]metav1.Condition, len(workConds))
		for idx := range workConds {
			wantWorkConds[idx] = workConds[idx]
			wantWorkConds[idx].ObservedGeneration = staleWorkGeneration
		}
		wantWorkStatus := placementv1alpha1.WorkStatus{
			Conditions: wantWorkConds,
			Manifests:  manifestStatuses,
		}

		// Check that the Work object status has stayed the same.
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

// updatePrimaryLinkedWorkObject replaces the manifests and the sync strategy of an existing primary work object,
// links it to the given primary placement resource snapshot, and sets its linked work count; this mimics how
// KubeFleet refreshes primary work objects during a rollout.
func updatePrimaryLinkedWorkObject(workName, memberClusterReservedNSName, primarySnapshotName, linkedWorkCount string, syncStrategy *placementv1alpha1.SyncStrategy, rawManifestJSON ...[]byte) {
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
		work.Annotations[placementv1alpha1.LinkedWorkCountAnnotationKey] = linkedWorkCount
		work.Spec.Manifests = manifests
		work.Spec.SyncStrategy = syncStrategy
		// The work applier might update the work object (e.g., its status) at the same time; retry on conflicts.
		return hubClient.Update(ctx, work)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update the primary Work object")
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

// workNotProcessedActual checks that the work applier has never processed a work object, i.e., the work object exists
// with no cleanup finalizer and no status, and no appliedWork object has been created for it.
func workNotProcessedActual(memberClient client.Client, workNS, workName string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}
		if controllerutil.ContainsFinalizer(work, workApplierCleanupFinalizer) {
			return fmt.Errorf("work object %s has the cleanup finalizer", workName)
		}
		if diff := cmp.Diff(work.Status, placementv1alpha1.WorkStatus{}, cmpopts.EquateEmpty()); diff != "" {
			return fmt.Errorf("work object %s status diff (-got +want):\n%s", workName, diff)
		}

		appliedWork := &placementv1alpha1.AppliedWork{}
		err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork)
		switch {
		case err == nil:
			return fmt.Errorf("an appliedWork object has been created for work object %s", workName)
		case !apierrors.IsNotFound(err):
			return fmt.Errorf("failed to retrieve the appliedWork object: %w", err)
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
		if !appliedWork.DeletionTimestamp.IsZero() &&
			(controllerutil.ContainsFinalizer(appliedWork, metav1.FinalizerDeleteDependents) || controllerutil.ContainsFinalizer(appliedWork, metav1.FinalizerOrphanDependents)) {
			// The AppliedWork object is being deleted (in the foreground, or with its dependents orphaned), but the
			// finalizer is still present. Remove the finalizer as there are no real built-in controllers in this
			// test environment to handle garbage collection.
			controllerutil.RemoveFinalizer(appliedWork, metav1.FinalizerDeleteDependents)
			controllerutil.RemoveFinalizer(appliedWork, metav1.FinalizerOrphanDependents)
			Expect(memberClient.Update(ctx, appliedWork)).To(Succeed(), "Failed to remove the finalizer from the AppliedWork object")
		}
		return fmt.Errorf("appliedWork object still exists")
	}
}

// appliedWorkBeingDeletedWithOrphanActual checks that the AppliedWork object has been marked for deletion with the
// orphan propagation policy, i.e., its dependents are to be left behind.
func appliedWorkBeingDeletedWithOrphanActual(memberClient client.Client, workName string) func() error {
	return func() error {
		appliedWork := &placementv1alpha1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		// Note that as there are no real built-in controllers (the GC controller) in this test environment, the
		// AppliedWork object will stay in the pending deletion state with the orphan finalizer.
		if appliedWork.DeletionTimestamp.IsZero() {
			return fmt.Errorf("appliedWork object has not been marked for deletion")
		}
		if !controllerutil.ContainsFinalizer(appliedWork, metav1.FinalizerOrphanDependents) {
			return fmt.Errorf("appliedWork object is not being deleted with the orphan propagation policy (finalizers: %v)", appliedWork.Finalizers)
		}
		return nil
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

func regularHPARemovedActual(memberClient client.Client, nsName, hpaName string) func() error {
	return func() error {
		// Delete the HorizontalPodAutoscaler object manually, as there are no real built-in controllers in this
		// test environment to handle garbage collection.
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      hpaName,
			},
		}
		if err := memberClient.Delete(ctx, hpa); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete the HorizontalPodAutoscaler object: %w", err)
		}

		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: hpaName}, hpa); !apierrors.IsNotFound(err) {
			return fmt.Errorf("horizontalPodAutoscaler object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

// configMapStableActual checks that the ConfigMap object exists with the given UID, i.e., it has never been deleted
// or re-created, and that it has the given data. If wantData is nil, the data is not checked.
func configMapStableActual(memberClient client.Client, nsName, configMapName string, wantUID types.UID, wantData map[string]string) func() error {
	return func() error {
		gotConfigMap := &corev1.ConfigMap{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, gotConfigMap); err != nil {
			return fmt.Errorf("failed to retrieve the ConfigMap object: %w", err)
		}

		if gotConfigMap.UID != wantUID {
			return fmt.Errorf("configMap UID = %s, want %s (the object has been re-created)", gotConfigMap.UID, wantUID)
		}
		if wantData == nil {
			return nil
		}
		if diff := cmp.Diff(gotConfigMap.Data, wantData); diff != "" {
			return fmt.Errorf("configMap data diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

// objectUntouchedActual checks that an object in the member cluster still exists and has not been written to since
// it was last retrieved, i.e., it keeps the resource version of the given object. Any write to the object, including
// changes to its owner references, bumps the resource version.
func objectUntouchedActual(memberClient client.Client, wantObj client.Object) func() error {
	return func() error {
		gotObj, ok := wantObj.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("failed to copy the object %s", client.ObjectKeyFromObject(wantObj))
		}
		if err := memberClient.Get(ctx, client.ObjectKeyFromObject(wantObj), gotObj); err != nil {
			return fmt.Errorf("failed to retrieve the object %s: %w", client.ObjectKeyFromObject(wantObj), err)
		}

		if gotObj.GetResourceVersion() != wantObj.GetResourceVersion() {
			return fmt.Errorf("object %s resource version = %s, want %s (the object has been modified; owner references: %v)",
				client.ObjectKeyFromObject(wantObj), gotObj.GetResourceVersion(), wantObj.GetResourceVersion(), gotObj.GetOwnerReferences())
		}
		return nil
	}
}

// objectNotRecreatedActual checks that an object in the member cluster still exists with the UID of the given
// object, i.e., it has never been deleted or re-created since it was last retrieved.
func objectNotRecreatedActual(memberClient client.Client, wantObj client.Object) func() error {
	return func() error {
		gotObj, ok := wantObj.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("failed to copy the object %s", client.ObjectKeyFromObject(wantObj))
		}
		if err := memberClient.Get(ctx, client.ObjectKeyFromObject(wantObj), gotObj); err != nil {
			return fmt.Errorf("failed to retrieve the object %s: %w", client.ObjectKeyFromObject(wantObj), err)
		}

		if gotObj.GetUID() != wantObj.GetUID() {
			return fmt.Errorf("object %s UID = %s, want %s (the object has been re-created)",
				client.ObjectKeyFromObject(wantObj), gotObj.GetUID(), wantObj.GetUID())
		}
		return nil
	}
}

// configMapsWithPrefixAbsentActual checks that no ConfigMap object in the given namespace has a name that starts
// with the given prefix.
func configMapsWithPrefixAbsentActual(memberClient client.Client, nsName, prefix string) func() error {
	return func() error {
		configMapList := &corev1.ConfigMapList{}
		if err := memberClient.List(ctx, configMapList, client.InNamespace(nsName)); err != nil {
			return fmt.Errorf("failed to list ConfigMap objects: %w", err)
		}

		for idx := range configMapList.Items {
			if strings.HasPrefix(configMapList.Items[idx].Name, prefix) {
				return fmt.Errorf("found a ConfigMap object (%s) with the name prefix %s", configMapList.Items[idx].Name, prefix)
			}
		}
		return nil
	}
}

// namespaceAbsentActual checks that the namespace does not exist in the member cluster.
func namespaceAbsentActual(memberClient client.Client, nsName string) func() error {
	return func() error {
		gotNS := &corev1.Namespace{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, gotNS); !apierrors.IsNotFound(err) {
			return fmt.Errorf("namespace object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

// namespaceNotManagedActual checks that the namespace exists in the member cluster and is not being deleted, but has
// not been adopted by KubeFleet, i.e., it has no owner references and no KubeFleet annotations.
func namespaceNotManagedActual(memberClient client.Client, nsName string) func() error {
	return func() error {
		gotNS := &corev1.Namespace{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, gotNS); err != nil {
			return fmt.Errorf("failed to retrieve the namespace object: %w", err)
		}

		if !gotNS.DeletionTimestamp.IsZero() {
			return fmt.Errorf("namespace is being deleted (deletion timestamp: %v)", gotNS.DeletionTimestamp)
		}
		if len(gotNS.OwnerReferences) > 0 {
			return fmt.Errorf("namespace has owner references: %v", gotNS.OwnerReferences)
		}
		for key := range gotNS.Annotations {
			if strings.Contains(key, placementv1alpha1.KubeFleetPrefix) {
				return fmt.Errorf("namespace has a KubeFleet annotation: %s", key)
			}
		}
		return nil
	}
}

// findManifestStatusByOrdinal returns the status of the manifest with the given ordinal in a Work object.
func findManifestStatusByOrdinal(work *placementv1alpha1.Work, ordinal int) *placementv1alpha1.PerManifestStatus {
	for idx := range work.Status.Manifests {
		if work.Status.Manifests[idx].Identifier.Ordinal == ordinal {
			return &work.Status.Manifests[idx]
		}
	}
	return nil
}

// manifestAppliedCondMessageActual checks that the Applied condition of the manifest with the given ordinal in a
// Work object has a message that contains the given substring.
func manifestAppliedCondMessageActual(workNS, workName string, ordinal int, wantSubstr string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		manifestStatus := findManifestStatusByOrdinal(work, ordinal)
		if manifestStatus == nil {
			return fmt.Errorf("no status found for the manifest with ordinal %d", ordinal)
		}
		appliedCond := meta.FindStatusCondition(manifestStatus.Conditions, placementv1alpha1.ManifestCondTypeApplied)
		if appliedCond == nil {
			return fmt.Errorf("no Applied condition found for the manifest with ordinal %d", ordinal)
		}
		if !strings.Contains(appliedCond.Message, wantSubstr) {
			return fmt.Errorf("applied condition message = %q, want a message that contains %q", appliedCond.Message, wantSubstr)
		}
		return nil
	}
}

// retrieveManifestCondLTT returns the last transition time of a condition of the manifest with the given ordinal
// in a Work object.
func retrieveManifestCondLTT(workNS, workName string, ordinal int, condType string) metav1.Time {
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work)).To(Succeed(), "Failed to retrieve the Work object")

	manifestStatus := findManifestStatusByOrdinal(work, ordinal)
	Expect(manifestStatus).NotTo(BeNil(), "No status found for the manifest with ordinal %d", ordinal)
	cond := meta.FindStatusCondition(manifestStatus.Conditions, condType)
	Expect(cond).NotTo(BeNil(), "No %s condition found for the manifest with ordinal %d", condType, ordinal)
	return cond.LastTransitionTime
}

// retrieveManifestFirstDiffedTimestamp returns the timestamp when diffs were first observed on the manifest with the
// given ordinal in a Work object.
func retrieveManifestFirstDiffedTimestamp(workNS, workName string, ordinal int) metav1.Time {
	work := &placementv1alpha1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work)).To(Succeed(), "Failed to retrieve the Work object")

	manifestStatus := findManifestStatusByOrdinal(work, ordinal)
	Expect(manifestStatus).NotTo(BeNil(), "No status found for the manifest with ordinal %d", ordinal)
	Expect(manifestStatus.DiffDetails).NotTo(BeNil(), "No diff details found for the manifest with ordinal %d", ordinal)
	return manifestStatus.DiffDetails.FirstDiffedObservedTimestamp
}

// manifestCondLTTActual checks that a condition of the manifest with the given ordinal in a Work object has the
// given last transition time, i.e., the condition has not transitioned since the time was captured.
func manifestCondLTTActual(workNS, workName string, ordinal int, condType string, wantLTT metav1.Time) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		manifestStatus := findManifestStatusByOrdinal(work, ordinal)
		if manifestStatus == nil {
			return fmt.Errorf("no status found for the manifest with ordinal %d", ordinal)
		}
		cond := meta.FindStatusCondition(manifestStatus.Conditions, condType)
		if cond == nil {
			return fmt.Errorf("no %s condition found for the manifest with ordinal %d", condType, ordinal)
		}
		if !cond.LastTransitionTime.Equal(&wantLTT) {
			return fmt.Errorf("%s condition last transition time = %v, want %v", condType, cond.LastTransitionTime, wantLTT)
		}
		return nil
	}
}

// nudgeWork bumps the generation of a Work object by setting a random value on a test-only label of its first
// manifest. For a primary Work object, this triggers an immediate reconciliation.
func nudgeWork(workName, memberClusterReservedNSName string) {
	Eventually(func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberClusterReservedNSName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}
		if len(work.Spec.Manifests) == 0 {
			return fmt.Errorf("the Work object has no manifest to nudge")
		}

		manifestObj := &unstructured.Unstructured{}
		if err := manifestObj.UnmarshalJSON(work.Spec.Manifests[0].Raw); err != nil {
			return fmt.Errorf("failed to unmarshal the first manifest: %w", err)
		}
		labels := manifestObj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[nudgeLabelKey] = utils.RandStr()
		manifestObj.SetLabels(labels)
		nudgedManifestJSON, err := manifestObj.MarshalJSON()
		if err != nil {
			return fmt.Errorf("failed to marshal the first manifest: %w", err)
		}

		work.Spec.Manifests[0].Raw = nudgedManifestJSON
		// The work applier might update the work object (e.g., its status) at the same time; retry on conflicts.
		return hubClient.Update(ctx, work)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to nudge the Work object")
}

// workProcessedAtLatestGenerationActual checks that the work applier has processed the latest generation of a
// Work object, i.e., the Applied condition of the Work object reports the current generation.
func workProcessedAtLatestGenerationActual(workNS, workName string) func() error {
	return func() error {
		work := &placementv1alpha1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: workNS}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		appliedCond := meta.FindStatusCondition(work.Status.Conditions, placementv1alpha1.WorkCondTypeApplied)
		if appliedCond == nil {
			return fmt.Errorf("no Applied condition found on the Work object")
		}
		if appliedCond.ObservedGeneration != work.Generation {
			return fmt.Errorf("applied condition observed generation = %d, want %d", appliedCond.ObservedGeneration, work.Generation)
		}
		return nil
	}
}

const (
	// The tolerances for comparing the observed gap between two consecutive reconciliations with the requeue delay
	// that the requeue rate limiter returns. The observed gap also includes the duration of the reconciliation itself
	// (and the scheduling latency), which might take a while when the test suite runs with the race detector and in
	// parallel.
	requeueGapLowerTolerance = time.Millisecond * 250
	requeueGapUpperTolerance = time.Millisecond * 1500
)

// parallelizerWithFixedDelay is a parallelizer that pauses for a fixed delay after the work applier processes each
// wave of manifests, so that the order in which the waves are processed shows in the creation timestamps (which are
// of the second granularity) of the applied objects.
type parallelizerWithFixedDelay struct {
	parallelizer.Parallelizer
	delay time.Duration
}

// ParallelizeUntil runs the tasks in parallel, and pauses for the fixed delay afterwards if the tasks process a wave
// of manifests.
func (p *parallelizerWithFixedDelay) ParallelizeUntil(ctx context.Context, pieces int, doWork workqueue.DoWorkPieceFunc, operation string) {
	p.Parallelizer.ParallelizeUntil(ctx, pieces, doWork, operation)
	if !strings.HasPrefix(operation, "processingManifestsInWave") {
		return
	}

	select {
	case <-ctx.Done():
	case <-time.After(p.delay):
	}
}

// recordingClient is a client that records the timestamps of the Get calls on work objects (the work applier starts
// each reconciliation of a primary work object with such a call) and counts the successful status updates on each
// object.
type recordingClient struct {
	client.Client

	mu                 sync.Mutex
	workGetTimestamps  map[types.NamespacedName][]time.Time
	statusUpdateCounts map[types.NamespacedName]int
}

// newRecordingClient wraps the given client with a recordingClient.
func newRecordingClient(c client.Client) *recordingClient {
	return &recordingClient{
		Client:             c,
		workGetTimestamps:  make(map[types.NamespacedName][]time.Time),
		statusUpdateCounts: make(map[types.NamespacedName]int),
	}
}

// Get retrieves an object, and records the timestamp if the object is a work object.
func (c *recordingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, isWork := obj.(*placementv1alpha1.Work); isWork {
		c.mu.Lock()
		c.workGetTimestamps[key] = append(c.workGetTimestamps[key], time.Now())
		c.mu.Unlock()
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// Status returns a status writer that counts the successful status updates.
func (c *recordingClient) Status() client.SubResourceWriter {
	return &recordingStatusWriter{
		SubResourceWriter: c.Client.Status(),
		recorder:          c,
	}
}

// reconcileStartTimestamps returns the timestamps of the Get calls on the given work object, i.e., the timestamps
// when the reconciliations of the (primary) work object started.
func (c *recordingClient) reconcileStartTimestamps(key types.NamespacedName) []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.workGetTimestamps[key])
}

// statusUpdateCount returns the count of the successful status updates on the given object.
func (c *recordingClient) statusUpdateCount(key types.NamespacedName) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusUpdateCounts[key]
}

// recordingStatusWriter is a status writer that counts the successful status updates on each object.
type recordingStatusWriter struct {
	client.SubResourceWriter
	recorder *recordingClient
}

// Update updates the status of an object, and counts the update if it succeeds.
func (w *recordingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if err := w.SubResourceWriter.Update(ctx, obj, opts...); err != nil {
		return err
	}
	w.recorder.mu.Lock()
	w.recorder.statusUpdateCounts[client.ObjectKeyFromObject(obj)]++
	w.recorder.mu.Unlock()
	return nil
}

// requeueGapsActual checks that the gaps between consecutive reconciliations of a work object, as recorded by the
// given recording client, match the given requeue delays (within tolerance). The gaps are counted from the first
// reconciliation at or after the given time, after skipping the given number of reconciliations.
func requeueGapsActual(recorder *recordingClient, workKey types.NamespacedName, since time.Time, skip int, wantGaps []time.Duration) func() error {
	return func() error {
		timestamps := recorder.reconcileStartTimestamps(workKey)
		startIdx := slices.IndexFunc(timestamps, func(t time.Time) bool { return !t.Before(since) })
		if startIdx != -1 {
			startIdx += skip
		}
		if startIdx == -1 || len(timestamps)-startIdx < len(wantGaps)+1 {
			return fmt.Errorf("not enough reconciliations have been observed since %v (observed: %v)", since, timestamps)
		}

		gotGaps := make([]time.Duration, 0, len(wantGaps))
		for idx := startIdx + 1; idx <= startIdx+len(wantGaps); idx++ {
			gotGaps = append(gotGaps, timestamps[idx].Sub(timestamps[idx-1]))
		}
		for idx := range wantGaps {
			if gotGaps[idx] < wantGaps[idx]-requeueGapLowerTolerance || gotGaps[idx] > wantGaps[idx]+requeueGapUpperTolerance {
				return fmt.Errorf("requeue gaps = %v, want %v (with tolerance -%v/+%v)", gotGaps, wantGaps, requeueGapLowerTolerance, requeueGapUpperTolerance)
			}
		}
		return nil
	}
}

// retrieveCreationTimestamp returns the creation timestamp of an object in the member cluster.
func retrieveCreationTimestamp(memberClient client.Client, obj client.Object) time.Time {
	gotObj, ok := obj.DeepCopyObject().(client.Object)
	Expect(ok).To(BeTrue(), "Failed to copy the object %s", client.ObjectKeyFromObject(obj))
	Expect(memberClient.Get(ctx, client.ObjectKeyFromObject(obj), gotObj)).To(Succeed(), "Failed to retrieve the object %s", client.ObjectKeyFromObject(obj))
	return gotObj.GetCreationTimestamp().Time
}
