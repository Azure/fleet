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

package placementresourcesnapshot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot/uniquename"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

// snapshotErrTestCase describes a placement policy for which snapshotting resources is expected to fail.
type snapshotErrTestCase struct {
	desc              string
	placementPolicy   placementv1alpha1.PlacementPolicyAccessor
	wantErrCategory   kferrors.ErrCategory
	wantErrMsgSubstrs []string
	// The index of the resource selector that should be reported in the error attributes (if set).
	wantResourceSelectorIdx *int
}

// newPlacementPolicy returns a placement policy (with a random name) in the given namespace that selects
// resources using the given resource selectors.
func newPlacementPolicy(namespace string, resourceSelectors ...placementv1alpha1.ResourceSelector) *placementv1alpha1.PlacementPolicy {
	return &placementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr()),
			Namespace: namespace,
		},
		Spec: placementv1alpha1.PlacementPolicySpec{
			ResourceSelectors: resourceSelectors,
		},
	}
}

// newClusterPlacementPolicy returns a cluster placement policy (with a random name) that selects resources
// using the given resource selectors.
func newClusterPlacementPolicy(resourceSelectors ...placementv1alpha1.ResourceSelector) *placementv1alpha1.ClusterPlacementPolicy {
	return &placementv1alpha1.ClusterPlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr()),
		},
		Spec: placementv1alpha1.PlacementPolicySpec{
			ResourceSelectors: resourceSelectors,
		},
	}
}

// newDeployment returns a minimal Deployment with the given name in the given namespace.
func newDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "demo"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(2)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "demo"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "demo"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.27",
						},
					},
				},
			},
		},
	}
}

// waitForDeploymentsInCache waits until the informer cache, from which the manager under test reads Deployments,
// has observed each of the given Deployments at its current resource version (i.e., the version returned by the
// last create or update call on the object).
//
// As the informer applies watch events in order, once the cache has observed a write, it has also observed all
// earlier writes to Deployments, including those made during the clean-up of other test cases.
func waitForDeploymentsInCache(deployments ...*appsv1.Deployment) {
	GinkgoHelper()

	lister := resourceSnapshotManager.hubDynamicInformerManager.Lister(appsv1.SchemeGroupVersion.WithResource("deployments"))
	for _, deployment := range deployments {
		Expect(deployment.ResourceVersion).ToNot(BeEmpty(), "Deployment %s has no resource version", client.ObjectKeyFromObject(deployment))
		Eventually(func() (string, error) {
			obj, err := lister.ByNamespace(deployment.Namespace).Get(deployment.Name)
			if err != nil {
				return "", err
			}
			cachedDeployment, err := meta.Accessor(obj)
			if err != nil {
				return "", err
			}
			return cachedDeployment.GetResourceVersion(), nil
		}, eventuallyDuration, eventuallyInterval).Should(Equal(deployment.ResourceVersion),
			"The cache has not observed Deployment %s at resource version %s", client.ObjectKeyFromObject(deployment), deployment.ResourceVersion)
	}
}

// waitForDeploymentsRemovedFromCache waits until the informer cache, from which the manager under test reads
// Deployments, no longer holds any of the given Deployments.
func waitForDeploymentsRemovedFromCache(deployments ...*appsv1.Deployment) {
	GinkgoHelper()

	lister := resourceSnapshotManager.hubDynamicInformerManager.Lister(appsv1.SchemeGroupVersion.WithResource("deployments"))
	for _, deployment := range deployments {
		Eventually(func() bool {
			_, err := lister.ByNamespace(deployment.Namespace).Get(deployment.Name)
			return errors.IsNotFound(err)
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The cache still holds Deployment %s", client.ObjectKeyFromObject(deployment))
	}
}

// placementResourceSnapshotObj returns an (empty) placement resource snapshot object of the matching scope with
// the given name; it is used for retrievals and clean-ups.
func placementResourceSnapshotObj(namespace, name string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	if namespace == "" {
		return &placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}
	return &placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

// primarySnapshotObjs returns the primary placement resource snapshot objects at indices [0, cnt) of the given
// placement policy; it is used for clean-ups.
func primarySnapshotObjs(placementPolicy placementv1alpha1.PlacementPolicyAccessor, cnt int) []client.Object {
	objs := make([]client.Object, 0, cnt)
	for idx := 0; idx < cnt; idx++ {
		objs = append(objs, placementResourceSnapshotObj(placementPolicy.GetNamespace(),
			uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), idx)))
	}
	return objs
}

// createNamespace creates a namespace with the given name.
//
// Note that the environment prepared by the envtest package does not support namespace deletion; the namespaces
// created by the test cases are left behind.
func createNamespace(name string) {
	GinkgoHelper()

	Expect(hubClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed(),
		"Failed to create namespace %s", name)
}

// createDeletingConfigMap creates a config map that has been marked for deletion, but cannot be deleted yet
// due to a finalizer.
func createDeletingConfigMap(name, namespace string, labels map[string]string) {
	GinkgoHelper()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Labels:     labels,
			Finalizers: []string{testFinalizer},
		},
		Data: map[string]string{"key": "value"},
	}
	Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
	Expect(hubClient.Delete(ctx, configMap)).To(Succeed(), "Failed to delete the config map")
	Eventually(func() error {
		gotConfigMap := &corev1.ConfigMap{}
		if err := hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(configMap), gotConfigMap); err != nil {
			return err
		}
		if gotConfigMap.DeletionTimestamp == nil {
			return fmt.Errorf("the config map has not been marked for deletion yet")
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the config map for deletion")
}

// deleteObjectsAndWait deletes the given objects and waits until they are gone.
//
// The envtest environment does not run the garbage collector; objects must be removed explicitly.
func deleteObjectsAndWait(objs []client.Object) {
	GinkgoHelper()

	for _, obj := range objs {
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T %s", obj, client.ObjectKeyFromObject(obj))
		Eventually(func() bool {
			return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T %s", obj, client.ObjectKeyFromObject(obj))
	}
}

// removeFinalizersAndDelete removes all finalizers from the given object and deletes it.
func removeFinalizersAndDelete(obj client.Object) {
	GinkgoHelper()

	Eventually(func() error {
		if err := hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return client.IgnoreNotFound(err)
		}
		obj.SetFinalizers(nil)
		return hubClient.Update(ctx, obj)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the finalizers from object %T %s", obj, client.ObjectKeyFromObject(obj))
	deleteObjectsAndWait([]client.Object{obj})
}

// verifyErrCategoryAndMsg verifies that the given error is a KubeFleet error of the wanted category, and that
// its message contains all the wanted substrings.
func verifyErrCategoryAndMsg(err error, wantErrCategory kferrors.ErrCategory, wantErrMsgSubstrs ...string) {
	GinkgoHelper()

	Expect(err).To(HaveOccurred(), "No error is returned")
	// The error category is the first key-value pair in the error attributes.
	gotErrAttrs := kferrors.Args(err)
	Expect(gotErrAttrs).ToNot(BeEmpty(), "The error is not a KubeFleet error: %v", err)
	wantErrCategoryAttrs := []interface{}{"errCategory", wantErrCategory}
	if diff := cmp.Diff(gotErrAttrs[:2], wantErrCategoryAttrs); diff != "" {
		Fail(fmt.Sprintf("Error category attributes diff (-got +want):\n%s\nerror: %v", diff, err))
	}
	for _, substr := range wantErrMsgSubstrs {
		Expect(err.Error()).To(ContainSubstring(substr), "Unexpected error message")
	}
}

// errAttrValue returns the value of the given key in the attributes of the given (KubeFleet) error.
func errAttrValue(err error, key string) (interface{}, bool) {
	attrs := kferrors.Args(err)
	for idx := 0; idx+1 < len(attrs); idx += 2 {
		if k, ok := attrs[idx].(string); ok && k == key {
			return attrs[idx+1], true
		}
	}
	return nil, false
}

// verifyNoPlacementResourceSnapshots verifies that no placement resource snapshot has been created for the
// given placement policy.
func verifyNoPlacementResourceSnapshots(placementPolicy placementv1alpha1.PlacementPolicyAccessor) {
	GinkgoHelper()

	matchingLabels := client.MatchingLabels{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey: placementPolicyOwnerLabelVal(placementPolicy),
	}
	if placementPolicy.GetNamespace() == "" {
		snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		Expect(hubUncachedReader.List(ctx, snapshotList, matchingLabels)).To(Succeed(), "Failed to list cluster placement resource snapshots")
		Expect(snapshotList.Items).To(BeEmpty(), "Cluster placement resource snapshots are created unexpectedly")
		return
	}
	snapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
	Expect(hubUncachedReader.List(ctx, snapshotList, client.InNamespace(placementPolicy.GetNamespace()), matchingLabels)).To(Succeed(),
		"Failed to list placement resource snapshots")
	Expect(snapshotList.Items).To(BeEmpty(), "Placement resource snapshots are created unexpectedly")
}

// snapshotResourcesAndExpectErr snapshots resources for the given placement policy, expecting a failure; it
// verifies that no placement resource snapshot is returned or created, and returns the error for further checks.
func snapshotResourcesAndExpectErr(placementPolicy placementv1alpha1.PlacementPolicyAccessor) error {
	GinkgoHelper()

	gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
	Expect(err).To(HaveOccurred(), "Snapshotting resources succeeded unexpectedly")
	Expect(gotSnapshots).To(BeEmpty(), "Placement resource snapshots are returned unexpectedly")
	Expect(isUpToDate).To(BeFalse(), "The up-to-date flag is set unexpectedly")
	verifyNoPlacementResourceSnapshots(placementPolicy)
	return err
}

// verifySnapshotErrTestCase runs a snapshot error test case.
func verifySnapshotErrTestCase(tc snapshotErrTestCase) {
	GinkgoHelper()

	err := snapshotResourcesAndExpectErr(tc.placementPolicy)
	verifyErrCategoryAndMsg(err, tc.wantErrCategory, tc.wantErrMsgSubstrs...)
	if tc.wantResourceSelectorIdx != nil {
		gotIdx, found := errAttrValue(err, "resourceSelectorIndex")
		Expect(found).To(BeTrue(), "The error does not report the index of the resource selector: %v", err)
		Expect(gotIdx).To(Equal(*tc.wantResourceSelectorIdx), "Unexpected resource selector index in the error")
	}
}

// verifyReturnedPrimarySnapshot verifies that snapshotting resources has succeeded, and has returned exactly
// one up-to-date placement resource snapshot, i.e., the primary snapshot at the wanted index.
func verifyReturnedPrimarySnapshot(
	gotSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor, isUpToDate bool, err error,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor, wantIdx int,
) {
	GinkgoHelper()

	Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
	Expect(isUpToDate).To(BeTrue(), "The placement resource snapshot is not up-to-date")
	Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")
	wantName := uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), wantIdx)
	Expect(gotSnapshots[0].GetName()).To(Equal(wantName), "Unexpected name of the placement resource snapshot")
}

// getSnapshotFromCache retrieves a placement resource snapshot using the cached client; the cache might take
// a moment to catch up.
func getSnapshotFromCache(namespace, name string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	GinkgoHelper()

	snapshot := placementResourceSnapshotObj(namespace, name)
	Eventually(func() error {
		return hubClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, snapshot)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get placement resource snapshot %s", name)
	return snapshot
}

// getPrimarySnapshotFromCache retrieves the primary placement resource snapshot at the given index using the
// cached client.
func getPrimarySnapshotFromCache(placementPolicy placementv1alpha1.PlacementPolicyAccessor, idx int) placementv1alpha1.PlacementResourceSnapshotAccessor {
	GinkgoHelper()

	return getSnapshotFromCache(placementPolicy.GetNamespace(), uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), idx))
}

// verifyNoPrimarySnapshotAtIdx verifies that no primary placement resource snapshot exists at the given index.
func verifyNoPrimarySnapshotAtIdx(placementPolicy placementv1alpha1.PlacementPolicyAccessor, idx int) {
	GinkgoHelper()

	name := uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), idx)
	err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: placementPolicy.GetNamespace(), Name: name},
		placementResourceSnapshotObj(placementPolicy.GetNamespace(), name))
	Expect(errors.IsNotFound(err)).To(BeTrue(), "Placement resource snapshot %s was created unexpectedly: %v", name, err)
}

// wantPlacementPolicyOwnerRefs returns the owner references that a placement resource snapshot should carry.
func wantPlacementPolicyOwnerRefs(placementPolicy placementv1alpha1.PlacementPolicyAccessor) []metav1.OwnerReference {
	kind := placementv1alpha1.PlacementPolicyKind
	if placementPolicy.GetNamespace() == "" {
		kind = placementv1alpha1.ClusterPlacementPolicyKind
	}
	return []metav1.OwnerReference{
		{
			APIVersion:         placementv1alpha1.GroupVersion.String(),
			Kind:               kind,
			Name:               placementPolicy.GetName(),
			UID:                placementPolicy.GetUID(),
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

// verifySnapshotMetadata verifies the labels, annotations, and owner references of a placement resource snapshot.
func verifySnapshotMetadata(
	snapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	idx, subIdx, snapshotCnt int,
	wantHash string,
) {
	GinkgoHelper()

	wantLabels := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyOwnerLabelVal(placementPolicy),
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    strconv.Itoa(idx),
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: strconv.Itoa(subIdx),
	}
	if subIdx == 0 {
		// Only the primary snapshot carries the count label.
		wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = strconv.Itoa(snapshotCnt)
	}
	if diff := cmp.Diff(snapshot.GetLabels(), wantLabels); diff != "" {
		Fail(fmt.Sprintf("Placement resource snapshot %s labels diff (-got +want):\n%s", snapshot.GetName(), diff))
	}
	wantAnnotations := map[string]string{
		placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
		placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicy.GetName(),
	}
	if diff := cmp.Diff(snapshot.GetAnnotations(), wantAnnotations); diff != "" {
		Fail(fmt.Sprintf("Placement resource snapshot %s annotations diff (-got +want):\n%s", snapshot.GetName(), diff))
	}
	if diff := cmp.Diff(snapshot.GetOwnerReferences(), wantPlacementPolicyOwnerRefs(placementPolicy)); diff != "" {
		Fail(fmt.Sprintf("Placement resource snapshot %s owner references diff (-got +want):\n%s", snapshot.GetName(), diff))
	}
}

// verifySnapshottedResourceIdentifiers verifies the identifiers of the given snapshotted resources.
func verifySnapshottedResourceIdentifiers(resources []placementv1alpha1.SnapshottedResource, wantIdentifiers []placementv1alpha1.ObjectReference) {
	GinkgoHelper()

	gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(resources))
	for idx := range resources {
		gotIdentifiers = append(gotIdentifiers, resources[idx].Identifier)
	}
	if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
		Fail(fmt.Sprintf("Snapshotted resource identifiers diff (-got +want):\n%s", diff))
	}
}

// manifestAsMap unmarshals a snapshotted manifest into a generic map.
//
// Comparing generic maps (rather than typed objects) verifies the absence of fields, which zero values in typed
// objects would otherwise hide.
func manifestAsMap(raw []byte) map[string]interface{} {
	GinkgoHelper()

	m := map[string]interface{}{}
	Expect(json.Unmarshal(raw, &m)).To(Succeed(), "Failed to unmarshal the manifest")
	return m
}

// snapshotCallResult is the outcome of a call to the placement resource snapshot manager.
type snapshotCallResult struct {
	names      []string
	isUpToDate bool
	err        error
}

// createDecoySnapshot creates a hand-crafted placement resource snapshot (a decoy) with an empty spec.
//
// A decoy without the owned-by annotation (or without the index/sub-index labels) is never indexed by the cache;
// it stays invisible to the field index-based lookups of the manager, yet is visible to quorum reads (uncached
// reads by name) and causes conflicts on creation. This deterministically simulates the situation where the
// API server has an object that the cache has yet to observe.
func createDecoySnapshot(namespace, name string, labels, annotations map[string]string, finalizers []string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	GinkgoHelper()

	decoy := placementResourceSnapshotObj(namespace, name)
	decoy.SetLabels(labels)
	decoy.SetAnnotations(annotations)
	decoy.SetFinalizers(finalizers)
	Expect(hubClient.Create(ctx, decoy)).To(Succeed(), "Failed to create the decoy placement resource snapshot %s", name)
	return decoy
}

// createOwnedDecoySnapshot creates a decoy (see createDecoySnapshot) in the namespace of the given placement policy,
// with a controller owner reference to the placement policy, the same way as the snapshots that the manager creates.
//
// The manager treats an indexed primary snapshot without owner references as a leftover; decoys that mimic the
// snapshots of a placement policy must be owned by the placement policy.
func createOwnedDecoySnapshot(placementPolicy placementv1alpha1.PlacementPolicyAccessor, name string, labels, annotations map[string]string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	GinkgoHelper()

	decoy := placementResourceSnapshotObj(placementPolicy.GetNamespace(), name)
	decoy.SetLabels(labels)
	decoy.SetAnnotations(annotations)
	decoy.SetOwnerReferences(decoyOwnerRefs(placementPolicy))
	Expect(hubClient.Create(ctx, decoy)).To(Succeed(), "Failed to create the decoy placement resource snapshot %s", name)
	return decoy
}

// decoyOwnerRefs returns the owner references of a placement resource snapshot that the given placement policy
// controls.
func decoyOwnerRefs(placementPolicy placementv1alpha1.PlacementPolicyAccessor) []metav1.OwnerReference {
	kind := placementv1alpha1.PlacementPolicyKind
	if placementPolicy.GetNamespace() == "" {
		kind = placementv1alpha1.ClusterPlacementPolicyKind
	}
	return []metav1.OwnerReference{*metav1.NewControllerRef(placementPolicy, placementv1alpha1.GroupVersion.WithKind(kind))}
}

// decoyPrimaryLabels returns the labels of a primary placement resource snapshot; the values are strings so that
// malformed values can be injected.
func decoyPrimaryLabels(placementPolicy placementv1alpha1.PlacementPolicyAccessor, idx, cnt string) map[string]string {
	labels := decoySecondaryLabels(placementPolicy, idx, "0")
	labels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = cnt
	return labels
}

// decoySecondaryLabels returns the labels of a secondary placement resource snapshot; the values are strings so
// that malformed values can be injected.
func decoySecondaryLabels(placementPolicy placementv1alpha1.PlacementPolicyAccessor, idx, subIdx string) map[string]string {
	return map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyOwnerLabelVal(placementPolicy),
		placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    idx,
		placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: subIdx,
	}
}

// decoyOwnedByAnnotations returns the owned-by annotation, which a decoy must carry to be indexed by the cache.
func decoyOwnedByAnnotations(placementPolicy placementv1alpha1.PlacementPolicyAccessor) map[string]string {
	return map[string]string{
		placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey: placementPolicy.GetName(),
	}
}

// waitForSnapshotGoneFromCache waits until the cache has observed the removal of a placement resource snapshot.
func waitForSnapshotGoneFromCache(namespace, name string) {
	GinkgoHelper()

	Eventually(func() bool {
		return errors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, placementResourceSnapshotObj(namespace, name)))
	}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The cache still holds placement resource snapshot %s", name)
}

// verifySnapshotExists verifies (via an uncached read) that a placement resource snapshot exists, and returns it.
func verifySnapshotExists(namespace, name string) placementv1alpha1.PlacementResourceSnapshotAccessor {
	GinkgoHelper()

	snapshot := placementResourceSnapshotObj(namespace, name)
	Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, snapshot)).To(Succeed(),
		"Failed to get placement resource snapshot %s", name)
	return snapshot
}

// verifySnapshotNotFound verifies (via an uncached read) that a placement resource snapshot does not exist.
func verifySnapshotNotFound(namespace, name string) {
	GinkgoHelper()

	err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, placementResourceSnapshotObj(namespace, name))
	Expect(errors.IsNotFound(err)).To(BeTrue(), "Placement resource snapshot %s exists unexpectedly: %v", name, err)
}

// snapshotNamesOf returns the names of the given placement resource snapshots.
func snapshotNamesOf(snapshots []placementv1alpha1.PlacementResourceSnapshotAccessor) []string {
	names := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		names = append(names, snapshot.GetName())
	}
	return names
}

// snapshotContentsHash returns the contents hash annotation of a placement resource snapshot.
func snapshotContentsHash(snapshot placementv1alpha1.PlacementResourceSnapshotAccessor) string {
	return snapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
}

// verifyReturnedSnapshots verifies that a call to the manager has succeeded, and has returned the wanted
// up-to-date placement resource snapshots.
func verifyReturnedSnapshots(
	gotSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor, isUpToDate bool, err error,
	wantNames []string,
) {
	GinkgoHelper()

	Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
	Expect(isUpToDate).To(BeTrue(), "The placement resource snapshots are not up-to-date")
	if diff := cmp.Diff(snapshotNamesOf(gotSnapshots), wantNames); diff != "" {
		Fail(fmt.Sprintf("Returned placement resource snapshot names diff (-got +want):\n%s", diff))
	}
}

// checkSnapshotCallFailed checks if a call to the manager has failed with an error of the wanted category and
// message; it returns an error describing the mismatch (if any), for use in Eventually and Consistently blocks.
func checkSnapshotCallFailed(
	gotSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor, isUpToDate bool, err error,
	wantErrCategory kferrors.ErrCategory, wantErrMsgSubstr string,
) error {
	switch {
	case err == nil:
		return fmt.Errorf("snapshotting resources succeeded unexpectedly (snapshots %v, up-to-date %t)", snapshotNamesOf(gotSnapshots), isUpToDate)
	case len(gotSnapshots) != 0 || isUpToDate:
		return fmt.Errorf("unexpected return values alongside the error (snapshots %v, up-to-date %t)", snapshotNamesOf(gotSnapshots), isUpToDate)
	case kferrors.Category(err) != wantErrCategory:
		return fmt.Errorf("error category = %s, want %s: %w", kferrors.Category(err), wantErrCategory, err)
	case !strings.Contains(err.Error(), wantErrMsgSubstr):
		return fmt.Errorf("error message does not contain %q: %w", wantErrMsgSubstr, err)
	}
	return nil
}

// verifySnapshotCallFailed verifies that a call to the manager has failed with an error of the wanted category and
// message.
//
// Unlike snapshotResourcesAndExpectErr, it does not require that no placement resource snapshots exist.
func verifySnapshotCallFailed(
	gotSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor, isUpToDate bool, err error,
	wantErrCategory kferrors.ErrCategory, wantErrMsgSubstrs ...string,
) {
	GinkgoHelper()

	Expect(gotSnapshots).To(BeEmpty(), "Placement resource snapshots are returned unexpectedly")
	Expect(isUpToDate).To(BeFalse(), "The up-to-date flag is set unexpectedly")
	verifyErrCategoryAndMsg(err, wantErrCategory, wantErrMsgSubstrs...)
}

// verifyOwnedSnapshots verifies (via an uncached read) the names of all the placement resource snapshots owned by
// a placement policy.
//
// Ownership is determined by the owned-by annotation, which always carries the full name of the placement policy;
// the owned-by label might be truncated, and two placement policies might share the same label value.
func verifyOwnedSnapshots(placementPolicy placementv1alpha1.PlacementPolicyAccessor, wantNames []string) {
	GinkgoHelper()

	var candidates []client.Object
	if placementPolicy.GetNamespace() == "" {
		snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		Expect(hubUncachedReader.List(ctx, snapshotList)).To(Succeed(), "Failed to list cluster placement resource snapshots")
		for idx := range snapshotList.Items {
			candidates = append(candidates, &snapshotList.Items[idx])
		}
	} else {
		snapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
		Expect(hubUncachedReader.List(ctx, snapshotList, client.InNamespace(placementPolicy.GetNamespace()))).To(Succeed(),
			"Failed to list placement resource snapshots")
		for idx := range snapshotList.Items {
			candidates = append(candidates, &snapshotList.Items[idx])
		}
	}

	var gotNames []string
	for _, candidate := range candidates {
		if candidate.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey] == placementPolicy.GetName() {
			gotNames = append(gotNames, candidate.GetName())
		}
	}
	if diff := cmp.Diff(gotNames, wantNames, cmpopts.SortSlices(func(a, b string) bool { return a < b }), cmpopts.EquateEmpty()); diff != "" {
		Fail(fmt.Sprintf("Owned placement resource snapshot names of %s diff (-got +want):\n%s", placementPolicy.GetName(), diff))
	}
}

// configMapNameSelector returns a resource selector that selects a config map by name.
func configMapNameSelector(name string) placementv1alpha1.ResourceSelector {
	return placementv1alpha1.ResourceSelector{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       name,
	}
}

// configMapLabelSelector returns a resource selector that selects config maps by labels.
func configMapLabelSelector(labels map[string]string) placementv1alpha1.ResourceSelector {
	return placementv1alpha1.ResourceSelector{
		APIVersion:    "v1",
		Kind:          "ConfigMap",
		LabelSelector: &metav1.LabelSelector{MatchLabels: labels},
	}
}

// createLabeledConfigMaps creates config maps, named `[PREFIX]-[INDEX]`, with the given labels in the app namespace,
// and returns their names.
func createLabeledConfigMaps(prefix string, cnt int, labels map[string]string) []string {
	GinkgoHelper()

	names := make([]string, 0, cnt)
	for idx := 0; idx < cnt; idx++ {
		name := fmt.Sprintf("%s-%d", prefix, idx)
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: appNamespaceName,
				Labels:    labels,
			},
			Data: map[string]string{"key": fmt.Sprintf("value-%d", idx)},
		}
		Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create config map %s", name)
		names = append(names, name)
	}
	return names
}

// configMapObjs returns (empty) config map objects with the given names in the app namespace; it is used for
// clean-ups.
func configMapObjs(names []string) []client.Object {
	objs := make([]client.Object, 0, len(names))
	for _, name := range names {
		objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
	}
	return objs
}

// snapshotObjs returns (empty) placement resource snapshot objects of the matching scope with the given names; it
// is used for clean-ups.
func snapshotObjs(namespace string, names ...string) []client.Object {
	objs := make([]client.Object, 0, len(names))
	for _, name := range names {
		objs = append(objs, placementResourceSnapshotObj(namespace, name))
	}
	return objs
}

// updateConfigMapData updates the data of a config map in the given namespace.
func updateConfigMapData(namespace, name, value string) {
	GinkgoHelper()

	configMap := &corev1.ConfigMap{}
	Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, configMap)).To(Succeed(),
		"Failed to get config map %s", name)
	configMap.Data = map[string]string{"key": value}
	Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update config map %s", name)
}

// placementPolicyNameOfLen returns a valid placement policy name of the given length.
func placementPolicyNameOfLen(l int) string {
	suffix := utils.RandStr()
	return strings.Repeat(placementPolicyNameFillerLetter, l-len(suffix)-1) + "-" + suffix
}

// verifySnapshotName verifies that a generated placement resource snapshot name is valid and ends with a hash, and
// that the placement policy name segment in it is sanitized (i.e., with dots dropped and/or truncated) only when
// wanted. unsanitizedNamePrefix is the wanted snapshot name minus the hash suffix, should no sanitization occur.
func verifySnapshotName(gotName, unsanitizedNamePrefix, hashedNamePattern string, wantSanitized bool) {
	GinkgoHelper()

	Expect(validation.IsDNS1123Subdomain(gotName)).To(BeEmpty(), "Snapshot name %s is not a valid DNS subdomain", gotName)
	Expect(len(gotName)).To(BeNumerically("<=", wantMaxSnapshotNameLen), "Snapshot name %s is too long", gotName)
	Expect(gotName).ToNot(ContainSubstring("."), "Snapshot name %s contains dots", gotName)
	Expect(gotName).To(MatchRegexp(hashedNamePattern), "Snapshot name %s does not end with a hash", gotName)
	if !wantSanitized {
		Expect(gotName).To(HavePrefix(unsanitizedNamePrefix), "Snapshot name %s is sanitized unexpectedly", gotName)
		Expect(gotName[len(unsanitizedNamePrefix):]).To(MatchRegexp(wantHashSuffixPattern), "Unexpected hash suffix of snapshot name %s", gotName)
		return
	}
	Expect(gotName).ToNot(HavePrefix(unsanitizedNamePrefix), "Snapshot name %s is not sanitized", gotName)
}

// verifyOwnedByLabelVal verifies the value of the owned-by label for a placement policy.
func verifyOwnedByLabelVal(placementPolicy placementv1alpha1.PlacementPolicyAccessor, wantTruncated bool) {
	GinkgoHelper()

	name := placementPolicy.GetName()
	gotLabelVal := placementPolicyOwnerLabelVal(placementPolicy)
	if !wantTruncated {
		Expect(gotLabelVal).To(Equal(name), "The owned-by label value is truncated unexpectedly")
		return
	}
	Expect(gotLabelVal).To(HaveLen(wantOwnedByLabelValLen), "Unexpected length of the truncated owned-by label value")
	Expect(gotLabelVal).To(HavePrefix(name[:wantOwnedByLabelValPrefixLen]+"-"), "Unexpected prefix of the truncated owned-by label value")
	Expect(gotLabelVal[wantOwnedByLabelValPrefixLen+1:]).To(MatchRegexp(wantHashSuffixPattern), "Unexpected hash suffix of the truncated owned-by label value")
}

// snapshotConcurrently calls the manager to snapshot resources for a placement policy from multiple goroutines at
// the same time, and returns the results.
func snapshotConcurrently(n int, placementPolicy placementv1alpha1.PlacementPolicyAccessor) []snapshotCallResult {
	results := make([]snapshotCallResult, n)
	var wg sync.WaitGroup
	for idx := 0; idx < n; idx++ {
		wg.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wg.Done()

			snapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			results[idx] = snapshotCallResult{names: snapshotNamesOf(snapshots), isUpToDate: isUpToDate, err: err}
		}()
	}
	wg.Wait()
	return results
}

// verifyConcurrentSnapshotCalls verifies the results of concurrent calls to the manager: at least one call should
// succeed, every successful call should return the wanted up-to-date snapshots, and every failed call should be
// retryable.
func verifyConcurrentSnapshotCalls(results []snapshotCallResult, wantNames []string) {
	GinkgoHelper()

	succeeded := 0
	for idx, res := range results {
		if res.err == nil {
			succeeded++
			Expect(res.isUpToDate).To(BeTrue(), "Call %d returned placement resource snapshots that are not up-to-date", idx)
			if diff := cmp.Diff(res.names, wantNames); diff != "" {
				Fail(fmt.Sprintf("Call %d returned placement resource snapshot names diff (-got +want):\n%s", idx, diff))
			}
			continue
		}

		// The cache might lag behind; the resulting errors must be retryable.
		category := kferrors.Category(res.err)
		isRetryable := category == kferrors.ErrCategoryTransient || (category == kferrors.ErrCategoryAPIServer && errors.IsAlreadyExists(res.err))
		Expect(isRetryable).To(BeTrue(), "Call %d returned a non-retryable error (category %s): %v", idx, category, res.err)
	}
	Expect(succeeded).To(BeNumerically(">=", 1), "No call has succeeded")
}

// newPausedManager returns a placement resource snapshot manager that is never started; as no worker processes its
// GC workqueue, the test cases can inspect (and drain) the GC requests it enqueues.
//
// The paused manager shares the cache (and its field indexes) with the manager under test; a placement policy
// should only be handled by one of the two managers.
func newPausedManager() *Manager {
	GinkgoHelper()

	pausedMgr, err := New(hubMgr,
		resourceSnapshotManager.hubDynamicClient,
		resourceSnapshotManager.hubDynamicInformerManager,
		resourceSnapshotManager.resourceEligibilityChecker,
		resourceSnapshotManager.restMapper,
		testMuSlotCnt,
		resourceSnapshotManager.maxPerSnapshotResourceDataSizeBytes,
		resourceSnapshotManager.maxPerSnapshotResourceCnt,
	)
	Expect(err).ToNot(HaveOccurred(), "Failed to create a paused placement resource snapshot manager")
	return pausedMgr
}

// drainGCRequests drains the GC workqueue of a paused manager, and returns the GC requests in the order they
// were enqueued.
func drainGCRequests(m *Manager) []snapshotGarbageCollectionRequest {
	var reqs []snapshotGarbageCollectionRequest
	for m.gcwq.Len() > 0 {
		req, _ := m.gcwq.Get()
		m.gcwq.Done(req)
		m.gcwq.Forget(req)
		reqs = append(reqs, req)
	}
	return reqs
}

// gcRequestFor returns the GC request that the manager should enqueue for a primary placement resource snapshot.
func gcRequestFor(
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	primarySnapshot placementv1alpha1.PlacementResourceSnapshotAccessor,
) snapshotGarbageCollectionRequest {
	return snapshotGarbageCollectionRequest{
		snapshotNamespacedName: types.NamespacedName{
			Namespace: primarySnapshot.GetNamespace(),
			Name:      primarySnapshot.GetName(),
		},
		snapshotUID: primarySnapshot.GetUID(),
		placementPolicyNamespacedName: types.NamespacedName{
			Namespace: placementPolicy.GetNamespace(),
			Name:      placementPolicy.GetName(),
		},
	}
}

// verifyGCRequests verifies that the GC requests (in order) are as wanted.
func verifyGCRequests(gotReqs, wantReqs []snapshotGarbageCollectionRequest) {
	GinkgoHelper()

	if diff := cmp.Diff(gotReqs, wantReqs, cmp.AllowUnexported(snapshotGarbageCollectionRequest{}), cmpopts.EquateEmpty()); diff != "" {
		Fail(fmt.Sprintf("GC requests diff (-got +want):\n%s", diff))
	}
}

// snapshotSetNames returns the names of the primary placement resource snapshot at the given index and its
// secondary snapshots, given the total number of snapshots at the index.
func snapshotSetNames(placementPolicy placementv1alpha1.PlacementPolicyAccessor, idx, cnt int) []string {
	names := []string{uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), idx)}
	for subIdx := 1; subIdx < cnt; subIdx++ {
		names = append(names, uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), idx, subIdx))
	}
	return names
}

// snapshotAndVerify calls the given manager to snapshot the selected resources if the latest snapshots are stale,
// and verifies that the wanted up-to-date snapshots are returned; it then waits until the cache has observed them,
// so that the next call can see them.
func snapshotAndVerify(m *Manager, placementPolicy placementv1alpha1.PlacementPolicyAccessor, wantNames []string) {
	GinkgoHelper()

	gotSnapshots, isUpToDate, err := m.SnapshotResourcesIfStale(ctx, placementPolicy)
	verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantNames)
	for _, name := range wantNames {
		getSnapshotFromCache(placementPolicy.GetNamespace(), name)
	}
}

// checkOwnedSnapshots checks (via an uncached read) the names of all the placement resource snapshots owned by a
// placement policy, as determined by the owned-by annotation; it returns an error describing the mismatch (if any),
// for use in Eventually blocks.
func checkOwnedSnapshots(placementPolicy placementv1alpha1.PlacementPolicyAccessor, wantNames []string) error {
	var gotNames []string
	if placementPolicy.GetNamespace() == "" {
		snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
		if err := hubUncachedReader.List(ctx, snapshotList); err != nil {
			return fmt.Errorf("failed to list cluster placement resource snapshots: %w", err)
		}
		for idx := range snapshotList.Items {
			snapshot := &snapshotList.Items[idx]
			if snapshot.Annotations[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey] == placementPolicy.GetName() {
				gotNames = append(gotNames, snapshot.Name)
			}
		}
	} else {
		snapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
		if err := hubUncachedReader.List(ctx, snapshotList, client.InNamespace(placementPolicy.GetNamespace())); err != nil {
			return fmt.Errorf("failed to list placement resource snapshots: %w", err)
		}
		for idx := range snapshotList.Items {
			snapshot := &snapshotList.Items[idx]
			if snapshot.Annotations[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey] == placementPolicy.GetName() {
				gotNames = append(gotNames, snapshot.Name)
			}
		}
	}

	if diff := cmp.Diff(gotNames, wantNames, cmpopts.SortSlices(func(a, b string) bool { return a < b }), cmpopts.EquateEmpty()); diff != "" {
		return fmt.Errorf("owned placement resource snapshot names of %s diff (-got +want):\n%s", placementPolicy.GetName(), diff)
	}
	return nil
}

// addSnapshotFinalizer adds a finalizer to a placement resource snapshot, which blocks its deletion.
func addSnapshotFinalizer(namespace, name string) {
	GinkgoHelper()

	Eventually(func() error {
		snapshot := placementResourceSnapshotObj(namespace, name)
		if err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, snapshot); err != nil {
			return err
		}
		snapshot.SetFinalizers(append(snapshot.GetFinalizers(), testFinalizer))
		return hubClient.Update(ctx, snapshot)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add a finalizer to placement resource snapshot %s", name)
}

// markSnapshotForDeletion deletes a placement resource snapshot (whose deletion is blocked by a finalizer), and waits
// until the cache has observed its deletion timestamp.
func markSnapshotForDeletion(namespace, name string) {
	GinkgoHelper()

	Expect(hubClient.Delete(ctx, placementResourceSnapshotObj(namespace, name))).To(Succeed(), "Failed to delete placement resource snapshot %s", name)
	Eventually(func() error {
		snapshot := placementResourceSnapshotObj(namespace, name)
		if err := hubClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, snapshot); err != nil {
			return err
		}
		if snapshot.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("the cache has not observed the deletion timestamp yet")
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark placement resource snapshot %s for deletion", name)
}

// setConfigMapLabels sets the labels of the given config maps in the app namespace; nil labels remove all labels.
func setConfigMapLabels(names []string, labels map[string]string) {
	GinkgoHelper()

	for _, name := range names {
		configMap := &corev1.ConfigMap{}
		Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: name}, configMap)).To(Succeed(),
			"Failed to get config map %s", name)
		configMap.Labels = labels
		Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update config map %s", name)
	}
}

// placementBindingObj returns an (empty) placement binding object of the matching scope with the given name.
func placementBindingObj(namespace, name string) placementv1alpha1.PlacementBindingAccessor {
	if namespace == "" {
		return &placementv1alpha1.ClusterPlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}
	return &placementv1alpha1.PlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

// createPlacementBinding creates a placement binding (of the same scope as the placement policy) that uses the given
// primary placement resource snapshot; the binding has an owner reference to the placement policy (with the
// given UID) only if the UID is not nil.
func createPlacementBinding(placementPolicy placementv1alpha1.PlacementPolicyAccessor, name, snapshotName string, ownerUID *types.UID) {
	GinkgoHelper()

	placementBinding := placementBindingObj(placementPolicy.GetNamespace(), name)
	if ownerUID != nil {
		kind := placementv1alpha1.PlacementPolicyKind
		if placementPolicy.GetNamespace() == "" {
			kind = placementv1alpha1.ClusterPlacementPolicyKind
		}
		placementBinding.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion: placementv1alpha1.GroupVersion.String(),
				Kind:       kind,
				Name:       placementPolicy.GetName(),
				UID:        *ownerUID,
			},
		})
	}
	placementBinding.SetSpec(placementv1alpha1.PlacementBindingSpec{
		PlacementPolicyName:  placementPolicy.GetName(),
		ClusterName:          "member-1",
		ResourceSnapshotName: snapshotName,
	})
	Expect(hubClient.Create(ctx, placementBinding)).To(Succeed(), "Failed to create placement binding %s", name)
}

// waitForPlacementBindingCntInCache waits until the cache (and its field index) reports the wanted number of
// placement bindings that are owned by the placement policy and use the given primary placement resource snapshot;
// the GC process relies on the field index to tell if a snapshot is still in use.
func waitForPlacementBindingCntInCache(placementPolicy placementv1alpha1.PlacementPolicyAccessor, snapshotName string, wantCnt int) {
	GinkgoHelper()

	fieldMatchers := client.MatchingFields{
		fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: fmt.Sprintf(
			fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, placementPolicy.GetName(), snapshotName),
	}
	Eventually(func() (int, error) {
		if placementPolicy.GetNamespace() == "" {
			placementBindingList := &placementv1alpha1.ClusterPlacementBindingList{}
			err := hubClient.List(ctx, placementBindingList, fieldMatchers)
			return len(placementBindingList.Items), err
		}
		placementBindingList := &placementv1alpha1.PlacementBindingList{}
		err := hubClient.List(ctx, placementBindingList, client.InNamespace(placementPolicy.GetNamespace()), fieldMatchers)
		return len(placementBindingList.Items), err
	}, eventuallyDuration, eventuallyInterval).Should(Equal(wantCnt), "Unexpected number of placement bindings in the cache that use snapshot %s", snapshotName)
}

// withInMemoryRevisionHistoryLimit returns a copy of the placement policy with the given revision history limit;
// the change is not persisted, which allows the use of values that the API server would reject.
func withInMemoryRevisionHistoryLimit(placementPolicy placementv1alpha1.PlacementPolicyAccessor, limit *int32) placementv1alpha1.PlacementPolicyAccessor {
	placementPolicyCopy := placementPolicy.DeepCopyObject().(placementv1alpha1.PlacementPolicyAccessor)
	placementPolicyCopy.GetSpec().ResourceRevisionHistoryLimit = limit
	return placementPolicyCopy
}

// updateRevisionHistoryLimit updates the revision history limit of the placement policy; the object is refreshed
// in place.
func updateRevisionHistoryLimit(placementPolicy placementv1alpha1.PlacementPolicyAccessor, limit int32) {
	GinkgoHelper()

	placementPolicy.GetSpec().ResourceRevisionHistoryLimit = ptr.To(limit)
	Expect(hubClient.Update(ctx, placementPolicy)).To(Succeed(), "Failed to update the revision history limit of the placement policy")
}

// recreatePlacementPolicy deletes the given placement policy, and creates a new one with the same name and spec; it
// returns the new placement policy, which has a different UID.
//
// The envtest environment does not run the Kubernetes GC; the placement resource snapshots of the deleted placement
// policy stay, along with their owner references (if any).
func recreatePlacementPolicy(placementPolicy placementv1alpha1.PlacementPolicyAccessor) placementv1alpha1.PlacementPolicyAccessor {
	GinkgoHelper()

	previousUID := placementPolicy.GetUID()
	deleteObjectsAndWait([]client.Object{placementPolicy})

	objectMeta := metav1.ObjectMeta{Name: placementPolicy.GetName(), Namespace: placementPolicy.GetNamespace()}
	var recreatedPlacementPolicy placementv1alpha1.PlacementPolicyAccessor
	if placementPolicy.GetNamespace() == "" {
		recreatedPlacementPolicy = &placementv1alpha1.ClusterPlacementPolicy{ObjectMeta: objectMeta, Spec: *placementPolicy.GetSpec().DeepCopy()}
	} else {
		recreatedPlacementPolicy = &placementv1alpha1.PlacementPolicy{ObjectMeta: objectMeta, Spec: *placementPolicy.GetSpec().DeepCopy()}
	}
	Expect(hubClient.Create(ctx, recreatedPlacementPolicy)).To(Succeed(), "Failed to re-create the placement policy")
	Expect(recreatedPlacementPolicy.GetUID()).ToNot(Equal(previousUID), "The re-created placement policy has the same UID")
	return recreatedPlacementPolicy
}

// setSnapshotOwnerRefs sets the owner references of a placement resource snapshot (nil removes all of them), and
// waits until the cache has observed the change, as the manager reads placement resource snapshots from the cache.
func setSnapshotOwnerRefs(namespace, name string, ownerRefs []metav1.OwnerReference) {
	GinkgoHelper()

	key := types.NamespacedName{Namespace: namespace, Name: name}
	Eventually(func() error {
		snapshot := placementResourceSnapshotObj(namespace, name)
		if err := hubUncachedReader.Get(ctx, key, snapshot); err != nil {
			return err
		}
		snapshot.SetOwnerReferences(ownerRefs)
		return hubClient.Update(ctx, snapshot)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to set the owner references of placement resource snapshot %s", name)
	Eventually(func() error {
		snapshot := placementResourceSnapshotObj(namespace, name)
		if err := hubClient.Get(ctx, key, snapshot); err != nil {
			return err
		}
		if diff := cmp.Diff(snapshot.GetOwnerReferences(), ownerRefs, cmpopts.EquateEmpty()); diff != "" {
			return fmt.Errorf("owner references in the cache diff (-got +want):\n%s", diff)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "The cache has not observed the owner references of placement resource snapshot %s", name)
}

// updatePlacementBindingSnapshot points a placement binding (of the same scope as the placement policy) to the given
// primary placement resource snapshot.
func updatePlacementBindingSnapshot(placementPolicy placementv1alpha1.PlacementPolicyAccessor, name, snapshotName string) {
	GinkgoHelper()

	Eventually(func() error {
		placementBinding := placementBindingObj(placementPolicy.GetNamespace(), name)
		if err := hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(placementBinding), placementBinding); err != nil {
			return err
		}
		placementBinding.GetSpec().ResourceSnapshotName = snapshotName
		return hubClient.Update(ctx, placementBinding)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update placement binding %s", name)
}
