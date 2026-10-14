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
	"slices"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	eventuallyDuration = time.Second * 10
	eventuallyInterval = time.Second * 1

	consistentlyDuration = time.Second * 5
	consistentlyInterval = time.Second * 1

	placementPolicyNameTemplate  = "placement-policy-%s"
	configMapNameTemplate        = "configmap-%s"
	deploymentNameTemplate       = "deployment-%s"
	placementBindingNameTemplate = "placement-binding-%s"
)

var _ = Describe("core ops (placement resource snapshots)", func() {
	Context("primary placement resource snapshot only (name-based selection)", Ordered, func() {
		// The environment prepared by the envtest package does not support namespace deletion; the
		// test case creates its objects in the shared app namespace, with unique names.
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		deploymentName := fmt.Sprintf(deploymentNameTemplate, utils.RandStr())

		var placementPolicy *placementv1alpha1.PlacementPolicy
		var wantHash string

		BeforeAll(func() {
			// Create a config map and a deployment in the app namespace.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"app": "demo"},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: appNamespaceName,
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
			Expect(hubClient.Create(ctx, deployment)).To(Succeed(), "Failed to create the Deployment")

			// Create a placement policy that selects the two resources.
			placementPolicy = &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementPolicyName,
					Namespace: appNamespaceName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Name:       configMapName,
						},
						{
							APIGroup:   "apps",
							APIVersion: "v1",
							Kind:       "Deployment",
							Name:       deploymentName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a primary placement resource snapshot", func() {
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshot")
			Expect(createdSnapshots).To(HaveLen(1), "Unexpected number of created placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshot is not up-to-date")
		})

		It("can compute the hash of selected resources", func() {
			_, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			wantHash = hash
		})

		It("should persist the placement resource snapshot as expected", func() {
			snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)

			// Read the snapshot using the cached client; the cache might take a moment to catch up.
			gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, gotSnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot")

			// Verify the metadata.
			wantLabels := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
				placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "0",
				placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
				placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
			}
			if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot labels diff (-got +want):\n%s", diff))
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot annotations diff (-got +want):\n%s", diff))
			}
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot owner references diff (-got +want):\n%s", diff))
			}

			// Verify the snapshotted resources; they are sorted by their unique IDs, i.e., the config map comes first.
			wantIdentifiers := []placementv1alpha1.ObjectReference{
				{
					Namespace:  appNamespaceName,
					Name:       configMapName,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				{
					Namespace:  appNamespaceName,
					Name:       deploymentName,
					APIGroup:   "apps",
					APIVersion: "v1",
					Kind:       "Deployment",
				},
			}
			gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
			for idx := range gotSnapshot.Spec.Resources {
				gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
			}
			if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
				Fail(fmt.Sprintf("Snapshotted resource identifiers diff (-got +want):\n%s", diff))
			}

			// Verify the config map manifest.
			gotConfigMap := &corev1.ConfigMap{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
			wantConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"app": "demo"},
				},
				Data: map[string]string{"key": "value"},
			}
			if diff := cmp.Diff(gotConfigMap, wantConfigMap); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map diff (-got +want):\n%s", diff))
			}

			// Verify the Deployment manifest; the spec should match that of the live object (which carries the
			// defaulted fields), while the read-only metadata and status fields should have been stripped.
			liveDeployment := &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, liveDeployment)).To(Succeed(),
				"Failed to get the Deployment")
			gotDeployment := &appsv1.Deployment{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[1].Manifest.Raw, gotDeployment)).To(Succeed(), "Failed to unmarshal the Deployment manifest")
			wantDeployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"app": "demo"},
				},
				Spec: liveDeployment.Spec,
			}
			if diff := cmp.Diff(gotDeployment, wantDeployment, cmpopts.EquateEmpty()); diff != "" {
				Fail(fmt.Sprintf("Snapshotted Deployment diff (-got +want):\n%s", diff))
			}
		})

		It("should return the existing snapshot when no snapshot is needed", func() {
			snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			wantSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			// The previous spec has waited for the snapshot to appear in the cache.
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, wantSnapshot)).To(Succeed(),
				"Failed to get the placement resource snapshot")

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The existing placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")

			gotSnapshot, ok := gotSnapshots[0].(*placementv1alpha1.PlacementResourceSnapshot)
			Expect(ok).To(BeTrue(), "Unexpected type of the returned placement resource snapshot: %T", gotSnapshots[0])
			if diff := cmp.Diff(gotSnapshot, wantSnapshot, cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot diff (-got +want):\n%s", diff))
			}

			// No new snapshot should have been created.
			nextSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			err = hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: nextSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "A new placement resource snapshot was created unexpectedly: %v", err)
		})

		It("should return the existing snapshot when it is not stale", func() {
			snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			wantSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, wantSnapshot)).To(Succeed(),
				"Failed to get the placement resource snapshot")

			// The selected resources have not changed since the snapshot was taken.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The existing placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")

			gotSnapshot, ok := gotSnapshots[0].(*placementv1alpha1.PlacementResourceSnapshot)
			Expect(ok).To(BeTrue(), "Unexpected type of the returned placement resource snapshot: %T", gotSnapshots[0])
			if diff := cmp.Diff(gotSnapshot, wantSnapshot, cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot diff (-got +want):\n%s", diff))
			}

			// No new snapshot should have been created.
			nextSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			err = hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: nextSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "A new placement resource snapshot was created unexpectedly: %v", err)
		})

		It("can update the selected resources", func() {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = "new-value"
			configMap.Data["extra-key"] = "extra-value"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			deployment := &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, deployment)).To(Succeed(),
				"Failed to get the Deployment")
			deployment.Spec.Replicas = ptr.To(int32(3))
			deployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.28"
			Expect(hubClient.Update(ctx, deployment)).To(Succeed(), "Failed to update the Deployment")
		})

		It("can create a new snapshot when the existing one is stale", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")

			newSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			Expect(gotSnapshots[0].GetName()).To(Equal(newSnapshotName), "Unexpected name of the new placement resource snapshot")
		})

		It("can create a new snapshot when the existing one is stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after the updates")

			newSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: newSnapshotName}, gotSnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the new placement resource snapshot")

			wantLabels := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
				placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "1",
				placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
				placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
			}
			if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
				Fail(fmt.Sprintf("New placement resource snapshot labels diff (-got +want):\n%s", diff))
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: newHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
				Fail(fmt.Sprintf("New placement resource snapshot annotations diff (-got +want):\n%s", diff))
			}

			// Verify that the new snapshot carries the updated resources.
			Expect(gotSnapshot.Spec.Resources).To(HaveLen(2), "Unexpected number of snapshotted resources")
			gotConfigMap := &corev1.ConfigMap{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
			wantConfigMapData := map[string]string{"key": "new-value", "extra-key": "extra-value"}
			if diff := cmp.Diff(gotConfigMap.Data, wantConfigMapData); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map data diff (-got +want):\n%s", diff))
			}
			gotDeployment := &appsv1.Deployment{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[1].Manifest.Raw, gotDeployment)).To(Succeed(), "Failed to unmarshal the Deployment manifest")
			Expect(gotDeployment.Spec.Replicas).To(Equal(ptr.To(int32(3))), "Unexpected replica count in the snapshotted Deployment")
			Expect(gotDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:1.28"), "Unexpected image in the snapshotted Deployment")

			// The previous snapshot should be left intact.
			oldSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{
				Namespace: appNamespaceName,
				Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
			}, oldSnapshot)).To(Succeed(), "Failed to get the previous placement resource snapshot")
			Expect(oldSnapshot.Annotations[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]).To(Equal(wantHash),
				"The previous placement resource snapshot has been modified")
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					Namespace: appNamespaceName,
				}},
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1),
					Namespace: appNamespaceName,
				}},
				placementPolicy,
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("primary and secondary placement resource snapshots (too many resources) (label-based selection)", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The names are in ascending order, which is also the order in which the selected resources are
		// snapshotted.
		configMapNames := make([]string, 5)
		for idx := range configMapNames {
			configMapNames[idx] = fmt.Sprintf("%s-%d", configMapNamePrefix, idx)
		}

		// The names of the config maps added later; they sort after the ones above.
		extraConfigMapNames := make([]string, 5)
		for idx := range extraConfigMapNames {
			extraConfigMapNames[idx] = fmt.Sprintf("%s-%d", configMapNamePrefix, idx+len(configMapNames))
		}

		allConfigMapNames := append(slices.Clone(configMapNames), extraConfigMapNames...)

		configMapIdentifiers := func(names []string) []placementv1alpha1.ObjectReference {
			identifiers := make([]placementv1alpha1.ObjectReference, 0, len(names))
			for _, name := range names {
				identifiers = append(identifiers, placementv1alpha1.ObjectReference{
					Namespace:  appNamespaceName,
					Name:       name,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				})
			}
			return identifiers
		}

		var placementPolicy *placementv1alpha1.PlacementPolicy
		var wantHash string

		BeforeAll(func() {
			// Create five config maps with the same label in the app namespace.
			for idx := range configMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapNames[idx],
						Namespace: appNamespaceName,
						Labels:    map[string]string{"placement-group": "foo"},
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx)},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}

			// Create a placement policy that selects the config maps by their label.
			placementPolicy = &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementPolicyName,
					Namespace: appNamespaceName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"placement-group": "foo"},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(HaveLen(len(configMapNames)), "Unexpected number of selected resources")
			wantHash = hash
		})

		It("can create a primary and a secondary placement resource snapshot", func() {
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshots are not up-to-date")
			Expect(createdSnapshots).To(HaveLen(2), "Unexpected number of created placement resource snapshots")

			// The snapshots are ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
			}
			gotNames := []string{createdSnapshots[0].GetName(), createdSnapshots[1].GetName()}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Created placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should persist the placement resource snapshots as expected", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			testCases := []struct {
				name            string
				snapshotName    string
				wantLabels      map[string]string
				wantIdentifiers []placementv1alpha1.ObjectReference
			}{
				{
					// Only the primary snapshot carries the count label.
					name:         "primary",
					snapshotName: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
						placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "2",
					},
					wantIdentifiers: configMapIdentifiers(configMapNames[:3]),
				},
				{
					name:         "secondary",
					snapshotName: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: "1",
					},
					wantIdentifiers: configMapIdentifiers(configMapNames[3:]),
				},
			}
			for _, tc := range testCases {
				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: tc.snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the %s placement resource snapshot", tc.name)

				if diff := cmp.Diff(gotSnapshot.Labels, tc.wantLabels); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot labels diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot annotations diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot owner references diff (-got +want):\n%s", tc.name, diff))
				}

				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, tc.wantIdentifiers); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot resource identifiers diff (-got +want):\n%s", tc.name, diff))
				}
			}
		})

		It("can create more config maps", func() {
			for idx, name := range extraConfigMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: appNamespaceName,
						Labels:    map[string]string{"placement-group": "foo"},
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx+len(configMapNames))},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}
		})

		It("should not create new snapshots if snapshots already exist", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			// The selected resources have changed since the snapshots were taken.
			Expect(isUpToDate).To(BeFalse(), "The existing placement resource snapshots are unexpectedly up-to-date")

			// The existing snapshots (at index 0) are returned, ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
			}
			gotNames := make([]string, 0, len(gotSnapshots))
			for _, snapshot := range gotSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot names diff (-got +want):\n%s", diff))
			}

			// No snapshots should have been created at the next index.
			nextSnapshotNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 1, 1),
			}
			for _, name := range nextSnapshotNames {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: name}, &placementv1alpha1.PlacementResourceSnapshot{})
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Placement resource snapshot %s was created unexpectedly: %v", name, err)
			}
		})

		// verifySnapshotsOfConfigMaps verifies that the given snapshots, created at the given index, are
		// the primary and secondary placement resource snapshots that cover the given config maps, and returns
		// the snapshots as persisted.
		//
		// The resources are spread across the snapshots in groups of three, in the order of their names.
		verifySnapshotsOfConfigMaps := func(
			snapshotIdx int,
			gotSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor,
			wantHash string,
			wantConfigMapNames []string,
		) []*placementv1alpha1.PlacementResourceSnapshot {
			wantSnapshotCnt := (len(wantConfigMapNames) + 2) / 3
			Expect(gotSnapshots).To(HaveLen(wantSnapshotCnt), "Unexpected number of placement resource snapshots")

			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			persistedSnapshots := make([]*placementv1alpha1.PlacementResourceSnapshot, len(gotSnapshots))
			for subIdx := range gotSnapshots {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx)
				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    fmt.Sprintf("%d", snapshotIdx),
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: fmt.Sprintf("%d", subIdx),
				}
				if subIdx == 0 {
					// Only the primary snapshot carries the count label.
					wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = fmt.Sprintf("%d", wantSnapshotCnt)
				} else {
					snapshotName = uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx, subIdx)
				}
				Expect(gotSnapshots[subIdx].GetName()).To(Equal(snapshotName), "Unexpected name of the placement resource snapshot at sub-index %d", subIdx)

				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at sub-index %d", subIdx)
				persistedSnapshots[subIdx] = gotSnapshot

				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) labels diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) annotations diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) owner references diff (-got +want):\n%s", subIdx, diff))
				}

				groupStart := subIdx * 3
				groupEnd := min(groupStart+3, len(wantConfigMapNames))
				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, configMapIdentifiers(wantConfigMapNames[groupStart:groupEnd])); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) resource identifiers diff (-got +want):\n%s", subIdx, diff))
				}
			}
			return persistedSnapshots
		}

		It("should create new snapshots if the existing ones are stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after adding config maps")
			wantHash = newHash

			// Ten resources, with at most three resources per snapshot: one primary and three secondary snapshots.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshots are not up-to-date")
			verifySnapshotsOfConfigMaps(1, gotSnapshots, wantHash, allConfigMapNames)
		})

		It("can update the last config map", func() {
			lastConfigMapName := extraConfigMapNames[len(extraConfigMapNames)-1]
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: lastConfigMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = "updated-value"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should create new snapshots again if the existing ones are stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after updating the config map")
			wantHash = newHash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshots are not up-to-date")
			persistedSnapshots := verifySnapshotsOfConfigMaps(2, gotSnapshots, wantHash, allConfigMapNames)

			// The last config map lives in the last secondary snapshot; it should carry the updated content.
			lastSnapshot := persistedSnapshots[len(persistedSnapshots)-1]
			Expect(lastSnapshot.Spec.Resources).To(HaveLen(1), "Unexpected number of resources in the last secondary snapshot")
			gotConfigMap := &corev1.ConfigMap{}
			Expect(json.Unmarshal(lastSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
			if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": "updated-value"}); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map data diff (-got +want):\n%s", diff))
			}
		})

		It("can delete some config maps", func() {
			for _, idx := range []int{2, 5, 9} {
				configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: allConfigMapNames[idx], Namespace: appNamespaceName}}
				Expect(hubClient.Delete(ctx, configMap)).To(Succeed(), "Failed to delete the config map")
			}
		})

		It("should create new snapshots if some selected resources have been removed", func() {
			// Seven resources are left, with at most three resources per snapshot: one primary and two secondary snapshots.
			var wantConfigMapNames []string
			for idx, name := range allConfigMapNames {
				if idx != 2 && idx != 5 && idx != 9 {
					wantConfigMapNames = append(wantConfigMapNames, name)
				}
			}

			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after deleting the config maps")
			wantHash = newHash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshots are not up-to-date")
			verifySnapshotsOfConfigMaps(3, gotSnapshots, wantHash, wantConfigMapNames)
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					Namespace: appNamespaceName,
				}},
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
					Namespace: appNamespaceName,
				}},
				placementPolicy,
			}
			var snapshotNames []string
			for snapshotIdx := 1; snapshotIdx <= 3; snapshotIdx++ {
				snapshotNames = append(snapshotNames, uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx))
				for subIdx := 1; subIdx <= 3; subIdx++ {
					snapshotNames = append(snapshotNames, uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx, subIdx))
				}
			}
			for _, name := range snapshotNames {
				objs = append(objs, &placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
			}
			for _, name := range allConfigMapNames {
				objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("primary and secondary placement resource snapshots (resources too large) (label-based selection)", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The name sorts after that of the first config map.
		secondConfigMapName := configMapName + "-2"

		var placementPolicy *placementv1alpha1.PlacementPolicy
		var wantHash string

		BeforeAll(func() {
			// Create a config map that is larger than the per-snapshot size limit of the manager under test.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"placement-group": "foo"},
				},
				Data: map[string]string{"key": strings.Repeat("a", 3000)},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			// Create a placement policy that selects the config map by its label.
			placementPolicy = &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementPolicyName,
					Namespace: appNamespaceName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"placement-group": "foo"},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("should return a user error when a selected resource is too large", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).To(HaveOccurred(), "Snapshotting resources succeeded unexpectedly")
			Expect(gotSnapshots).To(BeEmpty(), "Placement resource snapshots are returned unexpectedly")
			Expect(isUpToDate).To(BeFalse(), "The up-to-date flag is set unexpectedly")

			// The error category is the first key-value pair in the error attributes.
			gotErrAttrs := kferrors.Args(err)
			Expect(gotErrAttrs).ToNot(BeEmpty(), "The error is not a KubeFleet error: %v", err)
			wantErrCategoryAttrs := []interface{}{"errCategory", kferrors.ErrCategoryUser}
			if diff := cmp.Diff(gotErrAttrs[:2], wantErrCategoryAttrs); diff != "" {
				Fail(fmt.Sprintf("Error category attributes diff (-got +want):\n%s\nerror: %v", diff, err))
			}
		})

		It("should not create any placement resource snapshot", func() {
			snapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
			Expect(hubUncachedReader.List(ctx, snapshotList, client.InNamespace(appNamespaceName), client.MatchingLabels{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey: placementPolicyName,
			})).To(Succeed(), "Failed to list placement resource snapshots")
			Expect(snapshotList.Items).To(BeEmpty(), "Placement resource snapshots are created unexpectedly")
		})

		It("can resize the config map and create another config map", func() {
			// Each config map now fits in a snapshot, but the two of them together do not.
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = strings.Repeat("a", 1500)
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			secondConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secondConfigMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"placement-group": "foo"},
				},
				Data: map[string]string{"key": strings.Repeat("b", 1500)},
			}
			Expect(hubClient.Create(ctx, secondConfigMap)).To(Succeed(), "Failed to create the second config map")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(HaveLen(2), "Unexpected number of selected resources")
			wantHash = hash
		})

		It("can create a primary and a secondary placement resource snapshot", func() {
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshots are not up-to-date")
			Expect(createdSnapshots).To(HaveLen(2), "Unexpected number of created placement resource snapshots")

			// The snapshots are ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
			}
			gotNames := []string{createdSnapshots[0].GetName(), createdSnapshots[1].GetName()}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Created placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should persist the placement resource snapshots as expected", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			testCases := []struct {
				name           string
				snapshotName   string
				wantLabels     map[string]string
				wantConfigMap  string
				wantConfigData string
			}{
				{
					// Only the primary snapshot carries the count label.
					name:         "primary",
					snapshotName: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
						placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "2",
					},
					wantConfigMap:  configMapName,
					wantConfigData: strings.Repeat("a", 1500),
				},
				{
					name:         "secondary",
					snapshotName: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: "1",
					},
					wantConfigMap:  secondConfigMapName,
					wantConfigData: strings.Repeat("b", 1500),
				},
			}
			for _, tc := range testCases {
				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: tc.snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the %s placement resource snapshot", tc.name)

				if diff := cmp.Diff(gotSnapshot.Labels, tc.wantLabels); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot labels diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot annotations diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot owner references diff (-got +want):\n%s", tc.name, diff))
				}

				// Each snapshot holds exactly one of the two config maps.
				Expect(gotSnapshot.Spec.Resources).To(HaveLen(1), "Unexpected number of resources in the %s placement resource snapshot", tc.name)
				wantIdentifier := placementv1alpha1.ObjectReference{
					Namespace:  appNamespaceName,
					Name:       tc.wantConfigMap,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				}
				if diff := cmp.Diff(gotSnapshot.Spec.Resources[0].Identifier, wantIdentifier); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot resource identifier diff (-got +want):\n%s", tc.name, diff))
				}
				gotConfigMap := &corev1.ConfigMap{}
				Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(),
					"Failed to unmarshal the config map manifest in the %s placement resource snapshot", tc.name)
				if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": tc.wantConfigData}); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot config map data diff (-got +want):\n%s", tc.name, diff))
				}
			}
		})

		It("can shrink the config maps", func() {
			// The two config maps now fit in a single snapshot together.
			for _, name := range []string{configMapName, secondConfigMapName} {
				configMap := &corev1.ConfigMap{}
				Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: name}, configMap)).To(Succeed(),
					"Failed to get the config map")
				configMap.Data["key"] = strings.Repeat("c", 800)
				Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
			}
		})

		It("should create a primary placement resource snapshot only if the existing snapshots are stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after shrinking the config maps")
			wantHash = newHash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of created placement resource snapshots")
			wantName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			Expect(gotSnapshots[0].GetName()).To(Equal(wantName), "Unexpected name of the new placement resource snapshot")

			// Read the snapshot using the cached client; the cache might take a moment to catch up.
			gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: wantName}, gotSnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the new placement resource snapshot")

			wantLabels := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
				placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "1",
				placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
				placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
			}
			if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
				Fail(fmt.Sprintf("The new placement resource snapshot labels diff (-got +want):\n%s", diff))
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
				Fail(fmt.Sprintf("The new placement resource snapshot annotations diff (-got +want):\n%s", diff))
			}
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
				Fail(fmt.Sprintf("The new placement resource snapshot owner references diff (-got +want):\n%s", diff))
			}

			// Both config maps live in the primary snapshot, with the updated content.
			Expect(gotSnapshot.Spec.Resources).To(HaveLen(2), "Unexpected number of resources in the new placement resource snapshot")
			for idx, name := range []string{configMapName, secondConfigMapName} {
				wantIdentifier := placementv1alpha1.ObjectReference{
					Namespace:  appNamespaceName,
					Name:       name,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				}
				if diff := cmp.Diff(gotSnapshot.Spec.Resources[idx].Identifier, wantIdentifier); diff != "" {
					Fail(fmt.Sprintf("The new placement resource snapshot resource identifier (index %d) diff (-got +want):\n%s", idx, diff))
				}
				gotConfigMap := &corev1.ConfigMap{}
				Expect(json.Unmarshal(gotSnapshot.Spec.Resources[idx].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
				if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": strings.Repeat("c", 800)}); diff != "" {
					Fail(fmt.Sprintf("The new placement resource snapshot config map data (index %d) diff (-got +want):\n%s", idx, diff))
				}
			}

			// No secondary snapshot should have been created at the new index.
			secondaryName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 1, 1)
			err = hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: secondaryName}, &placementv1alpha1.PlacementResourceSnapshot{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "A secondary placement resource snapshot was created unexpectedly: %v", err)
		})

		AfterAll(func() {
			objs := []client.Object{
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					Namespace: appNamespaceName,
				}},
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
					Namespace: appNamespaceName,
				}},
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1),
					Namespace: appNamespaceName,
				}},
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: secondConfigMapName, Namespace: appNamespaceName}},
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("partial placement resource snapshot provisioning", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The names are in ascending order, which is also the order in which the selected resources are
		// snapshotted.
		configMapNames := make([]string, 10)
		for idx := range configMapNames {
			configMapNames[idx] = fmt.Sprintf("%s-%d", configMapNamePrefix, idx)
		}

		var placementPolicy *placementv1alpha1.PlacementPolicy
		var wantHash string
		// The UIDs of the secondary placement resource snapshots (by sub-index minus one), recorded for
		// later verification.
		var secondarySnapshotUIDs []types.UID

		// verifyOrphanedSecondariesCleanedUp verifies that the manager cleans up the orphaned secondary placement
		// resource snapshots (the primary snapshot is missing) and returns a transient error.
		//
		// The manager treats the secondary snapshots left without a primary snapshot as orphans: it deletes them
		// and returns a transient error, asking the caller to requeue before creating the snapshots anew.
		verifyOrphanedSecondariesCleanedUp := func() {
			// Other errors (e.g., those caused by a stale cache) might surface first; retry until the expected
			// transient error is observed.
			Eventually(func() error {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				if err == nil {
					return fmt.Errorf("snapshotting resources succeeded unexpectedly (snapshots %d, up-to-date %t)", len(gotSnapshots), isUpToDate)
				}
				if !strings.Contains(err.Error(), "cleaned up orphaned secondary placement resource snapshots") {
					return fmt.Errorf("unexpected error message: %w", err)
				}
				if len(gotSnapshots) != 0 || isUpToDate {
					return fmt.Errorf("unexpected return values alongside the error (snapshots %d, up-to-date %t)", len(gotSnapshots), isUpToDate)
				}

				// The error category is the first key-value pair in the error attributes.
				gotErrAttrs := kferrors.Args(err)
				if len(gotErrAttrs) < 2 {
					return fmt.Errorf("the error is not a KubeFleet error: %w", err)
				}
				wantErrCategoryAttrs := []interface{}{"errCategory", kferrors.ErrCategoryTransient}
				if diff := cmp.Diff(gotErrAttrs[:2], wantErrCategoryAttrs); diff != "" {
					return fmt.Errorf("error category attributes diff (-got +want):\n%s\nerror: %w", diff, err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the transient error from the clean-up of orphaned secondary placement resource snapshots")

			// The orphaned secondary snapshots should have been deleted.
			for subIdx := 1; subIdx <= 3; subIdx++ {
				secondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				Eventually(func() bool {
					err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: secondarySnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
					return errors.IsNotFound(err)
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the orphaned secondary placement resource snapshot at sub-index %d", subIdx)
			}
		}

		BeforeAll(func() {
			// Create ten config maps with the same label in the app namespace.
			for idx := range configMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapNames[idx],
						Namespace: appNamespaceName,
						Labels:    map[string]string{"placement-group": "foo"},
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx)},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}

			// Create a placement policy that selects the config maps by their label.
			placementPolicy = &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementPolicyName,
					Namespace: appNamespaceName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"placement-group": "foo"},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(HaveLen(len(configMapNames)), "Unexpected number of selected resources")
			wantHash = hash
		})

		It("can create a primary and three secondary placement resource snapshots", func() {
			// Ten resources, with at most three resources per snapshot: one primary and three secondary snapshots.
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshots are not up-to-date")
			Expect(createdSnapshots).To(HaveLen(4), "Unexpected number of created placement resource snapshots")

			// The snapshots are ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 2),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 3),
			}
			gotNames := make([]string, 0, len(createdSnapshots))
			for _, snapshot := range createdSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Created placement resource snapshot names diff (-got +want):\n%s", diff))
			}

			// Write down the UIDs of the secondary placement resource snapshots.
			secondarySnapshotUIDs = make([]types.UID, 0, len(createdSnapshots)-1)
			for _, snapshot := range createdSnapshots[1:] {
				Expect(snapshot.GetUID()).ToNot(BeEmpty(), "The secondary placement resource snapshot %s has no UID", snapshot.GetName())
				secondarySnapshotUIDs = append(secondarySnapshotUIDs, snapshot.GetUID())
			}
		})

		It("should persist the placement resource snapshots as expected", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			for subIdx := 0; subIdx < 4; subIdx++ {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: fmt.Sprintf("%d", subIdx),
				}
				if subIdx == 0 {
					// Only the primary snapshot carries the count label.
					wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = "4"
				} else {
					snapshotName = uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				}

				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at sub-index %d", subIdx)

				if subIdx > 0 {
					Expect(gotSnapshot.UID).To(Equal(secondarySnapshotUIDs[subIdx-1]), "Unexpected UID of the placement resource snapshot at sub-index %d", subIdx)
				}
				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) labels diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) annotations diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) owner references diff (-got +want):\n%s", subIdx, diff))
				}

				// The resources are spread across the snapshots in groups of three, in the order of their names.
				groupStart := subIdx * 3
				groupEnd := min(groupStart+3, len(configMapNames))
				wantIdentifiers := make([]placementv1alpha1.ObjectReference, 0, groupEnd-groupStart)
				for _, name := range configMapNames[groupStart:groupEnd] {
					wantIdentifiers = append(wantIdentifiers, placementv1alpha1.ObjectReference{
						Namespace:  appNamespaceName,
						Name:       name,
						APIVersion: "v1",
						Kind:       "ConfigMap",
					})
				}
				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) resource identifiers diff (-got +want):\n%s", subIdx, diff))
				}
			}
		})

		It("can delete the primary placement resource snapshot", func() {
			// Note that this is for simulation purposes only: it mimics a partial provisioning, where the primary
			// snapshot is missing while its secondary snapshots remain. The manager never deletes only the primary
			// snapshot in normal operations, so this situation will not occur in practice.
			primarySnapshot := &placementv1alpha1.PlacementResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					Namespace: appNamespaceName,
				},
			}
			Expect(hubClient.Delete(ctx, primarySnapshot)).To(Succeed(), "Failed to delete the primary placement resource snapshot")

			// Wait until the cache has observed the deletion.
			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, client.ObjectKeyFromObject(primarySnapshot), primarySnapshot))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the primary placement resource snapshot")
		})

		It("should clean up the orphaned secondary placement resource snapshots and return a transient error", func() {
			verifyOrphanedSecondariesCleanedUp()
		})

		It("can re-create the primary and secondary placement resource snapshots", func() {
			// The orphaned secondary snapshots have been cleaned up; the manager can now create the snapshots anew.
			// The call is retried as the cache might not have observed the clean-up yet.
			var recreatedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
			var isUpToDate bool
			Eventually(func() error {
				var err error
				recreatedSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				return err
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The re-created placement resource snapshots are not up-to-date")

			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 2),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 3),
			}
			gotNames := make([]string, 0, len(recreatedSnapshots))
			for _, snapshot := range recreatedSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Re-created placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should have re-created the primary snapshot and the secondary snapshots with new UIDs", func() {
			// The primary snapshot has been re-created (its UID is not checked).
			primarySnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
			primarySnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: primarySnapshotName}, primarySnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the primary placement resource snapshot")
			Expect(primarySnapshot.Annotations).To(HaveKeyWithValue(placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey, wantHash),
				"Unexpected contents hash of the re-created primary placement resource snapshot")
			Expect(primarySnapshot.Labels).To(HaveKeyWithValue(placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey, "4"),
				"Unexpected snapshot count of the re-created primary placement resource snapshot")

			// All the secondary snapshots have been re-created with new UIDs.
			for subIdx := 1; subIdx <= 3; subIdx++ {
				secondarySnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				secondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: secondarySnapshotName}, secondarySnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the secondary placement resource snapshot at sub-index %d", subIdx)
				Expect(secondarySnapshot.UID).ToNot(Equal(secondarySnapshotUIDs[subIdx-1]),
					"The secondary placement resource snapshot at sub-index %d has not been re-created (UID unchanged)", subIdx)
				Expect(secondarySnapshot.Annotations).To(HaveKeyWithValue(placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey, wantHash),
					"Unexpected contents hash of the re-created secondary placement resource snapshot at sub-index %d", subIdx)
			}
		})

		It("can delete the primary placement resource snapshot again", func() {
			// Record the UIDs of the secondary placement resource snapshots that were re-created previously.
			secondarySnapshotUIDs = make([]types.UID, 0, 3)
			for subIdx := 1; subIdx <= 3; subIdx++ {
				secondarySnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Expect(hubClient.Get(ctx, types.NamespacedName{
					Namespace: appNamespaceName,
					Name:      uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx),
				}, secondarySnapshot)).To(Succeed(), "Failed to get the secondary placement resource snapshot at sub-index %d", subIdx)
				secondarySnapshotUIDs = append(secondarySnapshotUIDs, secondarySnapshot.UID)
			}

			// Note that this is for simulation purposes only: it mimics a partial provisioning, where the primary
			// snapshot is missing while its secondary snapshots remain. The manager never deletes only the primary
			// snapshot in normal operations, so this situation will not occur in practice.
			primarySnapshot := &placementv1alpha1.PlacementResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					Namespace: appNamespaceName,
				},
			}
			Expect(hubClient.Delete(ctx, primarySnapshot)).To(Succeed(), "Failed to delete the primary placement resource snapshot")

			// Wait until the cache has observed the deletion.
			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, client.ObjectKeyFromObject(primarySnapshot), primarySnapshot))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the primary placement resource snapshot")
		})

		It("can update the last config map", func() {
			lastConfigMapName := configMapNames[len(configMapNames)-1]
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: lastConfigMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = "updated-value"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should clean up the orphaned secondary placement resource snapshots and return a transient error again", func() {
			verifyOrphanedSecondariesCleanedUp()
		})

		It("can re-create the primary and secondary placement resource snapshots with the updated content", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after updating the config map")
			wantHash = newHash

			// The orphaned secondary snapshots have been cleaned up; the manager can now create the snapshots anew.
			// The call is retried as the cache might not have observed the clean-up yet.
			var recreatedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
			var isUpToDate bool
			Eventually(func() error {
				var err error
				recreatedSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				return err
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The re-created placement resource snapshots are not up-to-date")
			Expect(recreatedSnapshots).To(HaveLen(4), "Unexpected number of re-created placement resource snapshots")
		})

		It("should have re-created all the snapshots with the expected content", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			for subIdx := 0; subIdx < 4; subIdx++ {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: fmt.Sprintf("%d", subIdx),
				}
				if subIdx == 0 {
					// Only the primary snapshot carries the count label.
					wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = "4"
				} else {
					snapshotName = uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				}

				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at sub-index %d", subIdx)

				if subIdx > 0 {
					Expect(gotSnapshot.UID).ToNot(Equal(secondarySnapshotUIDs[subIdx-1]),
						"The secondary placement resource snapshot at sub-index %d has not been re-created (UID unchanged)", subIdx)
				}
				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) labels diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) annotations diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) owner references diff (-got +want):\n%s", subIdx, diff))
				}

				// The resources are spread across the snapshots in groups of three, in the order of their names.
				groupStart := subIdx * 3
				groupEnd := min(groupStart+3, len(configMapNames))
				wantIdentifiers := make([]placementv1alpha1.ObjectReference, 0, groupEnd-groupStart)
				for _, name := range configMapNames[groupStart:groupEnd] {
					wantIdentifiers = append(wantIdentifiers, placementv1alpha1.ObjectReference{
						Namespace:  appNamespaceName,
						Name:       name,
						APIVersion: "v1",
						Kind:       "ConfigMap",
					})
				}
				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) resource identifiers diff (-got +want):\n%s", subIdx, diff))
				}

				// Verify the content of each config map; only the last one has been updated.
				for idx := range gotSnapshot.Spec.Resources {
					configMapIdx := groupStart + idx
					wantData := map[string]string{"key": fmt.Sprintf("value-%d", configMapIdx)}
					if configMapIdx == len(configMapNames)-1 {
						wantData["key"] = "updated-value"
					}
					gotConfigMap := &corev1.ConfigMap{}
					Expect(json.Unmarshal(gotSnapshot.Spec.Resources[idx].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
					if diff := cmp.Diff(gotConfigMap.Data, wantData); diff != "" {
						Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) config map data (index %d) diff (-got +want):\n%s", subIdx, idx, diff))
					}
				}
			}
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					Namespace: appNamespaceName,
				}},
				placementPolicy,
			}
			for subIdx := 1; subIdx <= 3; subIdx++ {
				objs = append(objs, &placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx),
					Namespace: appNamespaceName,
				}})
			}
			for _, name := range configMapNames {
				objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("GC", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())

		var placementPolicy *placementv1alpha1.PlacementPolicy

		// updateConfigMapAndSnapshot updates the config map with the given data and calls the manager to snapshot
		// the selected resources; it returns the snapshots created.
		updateConfigMapAndSnapshot := func(data string) []placementv1alpha1.PlacementResourceSnapshotAccessor {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = data
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of created placement resource snapshots")

			// Wait until the cache has observed the new snapshot; otherwise the next call might not see it.
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKeyFromObject(gotSnapshots[0]), &placementv1alpha1.PlacementResourceSnapshot{})
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the new placement resource snapshot")
			return gotSnapshots
		}

		BeforeAll(func() {
			// Create a config map in the app namespace.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
				},
				Data: map[string]string{"key": "initial-value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			// Create a placement policy that selects the config map.
			placementPolicy = &placementv1alpha1.PlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementPolicyName,
					Namespace: appNamespaceName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Name:       configMapName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create four primary placement resource snapshots", func() {
			for idx := 0; idx < 4; idx++ {
				gotSnapshots := updateConfigMapAndSnapshot(fmt.Sprintf("value-%d", idx))
				wantName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx)
				Expect(gotSnapshots[0].GetName()).To(Equal(wantName), "Unexpected name of the placement resource snapshot at index %d", idx)
			}
		})

		It("should have four primary placement resource snapshots at the expected indexes", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "PlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}

			seenHashes := map[string]bool{}
			for idx := 0; idx < 4; idx++ {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx)
				gotSnapshot := &placementv1alpha1.PlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at index %d", idx)

				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           fmt.Sprintf("%d", idx),
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
					placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
				}
				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (index %d) labels diff (-got +want):\n%s", idx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (index %d) owner references diff (-got +want):\n%s", idx, diff))
				}

				// Each snapshot tracks a different state of the config map.
				hash := gotSnapshot.Annotations[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
				Expect(hash).ToNot(BeEmpty(), "The placement resource snapshot (index %d) has no contents hash", idx)
				Expect(seenHashes).ToNot(HaveKey(hash), "The placement resource snapshot (index %d) has the same contents hash as another snapshot", idx)
				seenHashes[hash] = true

				Expect(gotSnapshot.Spec.Resources).To(HaveLen(1), "Unexpected number of resources in the placement resource snapshot (index %d)", idx)
				gotConfigMap := &corev1.ConfigMap{}
				Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
				if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": fmt.Sprintf("value-%d", idx)}); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (index %d) config map data diff (-got +want):\n%s", idx, diff))
				}
			}

			// There should be no other snapshots.
			snapshotList := &placementv1alpha1.PlacementResourceSnapshotList{}
			Expect(hubUncachedReader.List(ctx, snapshotList, client.InNamespace(appNamespaceName), client.MatchingLabels{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey: placementPolicyName,
			})).To(Succeed(), "Failed to list placement resource snapshots")
			Expect(snapshotList.Items).To(HaveLen(4), "Unexpected number of placement resource snapshots")
		})

		It("should garbage collect the first primary placement resource snapshot", func() {
			updateConfigMapAndSnapshot("value-4")

			firstSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			Eventually(func() bool {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: firstSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
				return errors.IsNotFound(err)
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The first primary placement resource snapshot has not been garbage collected")
		})

		It("should garbage collect the second primary placement resource snapshot", func() {
			updateConfigMapAndSnapshot("value-5")

			secondSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			Eventually(func() bool {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: secondSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
				return errors.IsNotFound(err)
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The second primary placement resource snapshot has not been garbage collected")
		})

		It("can create a placement binding that uses the third primary placement resource snapshot", func() {
			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			placementBinding := &placementv1alpha1.PlacementBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementBindingName,
					Namespace: appNamespaceName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: placementv1alpha1.GroupVersion.String(),
							Kind:       placementv1alpha1.PlacementPolicyKind,
							Name:       placementPolicyName,
							UID:        placementPolicy.UID,
						},
					},
				},
				Spec: placementv1alpha1.PlacementBindingSpec{
					PlacementPolicyName:  placementPolicyName,
					ClusterName:          "member-1",
					ResourceSnapshotName: thirdSnapshotName,
				},
			}
			Expect(hubClient.Create(ctx, placementBinding)).To(Succeed(), "Failed to create the placement binding")

			// Wait until the cache (and its field index) has observed the placement binding; the garbage collection
			// process relies on it to tell if a snapshot is still in use.
			wantFieldVal := fmt.Sprintf(
				fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, placementPolicyName, thirdSnapshotName)
			Eventually(func() (int, error) {
				placementBindingList := &placementv1alpha1.PlacementBindingList{}
				err := hubClient.List(ctx, placementBindingList, client.InNamespace(appNamespaceName), client.MatchingFields{
					fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: wantFieldVal,
				})
				return len(placementBindingList.Items), err
			}, eventuallyDuration, eventuallyInterval).Should(Equal(1), "Failed to find the placement binding in the cache")
		})

		It("should not garbage collect the third primary placement resource snapshot as it is in use", func() {
			// The third snapshot is now the oldest one that is past the revision history limit; the garbage
			// collection process should skip it as a placement binding is using it.
			updateConfigMapAndSnapshot("value-6")

			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			Consistently(func() error {
				return hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: thirdSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The third primary placement resource snapshot has been garbage collected unexpectedly")
		})

		It("should garbage collect the fourth primary placement resource snapshot but keep the third one", func() {
			// The third and the fourth snapshots are now both past the revision history limit; the third one is
			// still in use by a placement binding, so only the fourth one should be garbage collected.
			updateConfigMapAndSnapshot("value-7")

			fourthSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 3)
			Eventually(func() bool {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: fourthSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
				return errors.IsNotFound(err)
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The fourth primary placement resource snapshot has not been garbage collected")

			// The garbage collection requests are processed in order; the request for the third snapshot has been
			// handled by now.
			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			Consistently(func() error {
				return hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: thirdSnapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The third primary placement resource snapshot has been garbage collected unexpectedly")
		})

		It("can delete the placement binding", func() {
			placementBinding := &placementv1alpha1.PlacementBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementBindingName,
					Namespace: appNamespaceName,
				},
			}
			Expect(hubClient.Delete(ctx, placementBinding)).To(Succeed(), "Failed to delete the placement binding")

			// Wait until the cache (and its field index) has observed the deletion; the garbage collection process
			// relies on it to tell if a snapshot is still in use.
			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			Eventually(func() (int, error) {
				placementBindingList := &placementv1alpha1.PlacementBindingList{}
				err := hubClient.List(ctx, placementBindingList, client.InNamespace(appNamespaceName), client.MatchingFields{
					fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: fmt.Sprintf(
						fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, placementPolicyName, thirdSnapshotName),
				})
				return len(placementBindingList.Items), err
			}, eventuallyDuration, eventuallyInterval).Should(Equal(0), "The placement binding is still found in the cache")
		})

		It("should garbage collect the third and the fifth primary placement resource snapshots", func() {
			// The third snapshot is no longer in use; both the third and the fifth snapshots are now past the
			// revision history limit.
			updateConfigMapAndSnapshot("value-8")

			for _, idx := range []int{2, 4} {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx)
				Eventually(func() bool {
					err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: snapshotName}, &placementv1alpha1.PlacementResourceSnapshot{})
					return errors.IsNotFound(err)
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The primary placement resource snapshot at index %d has not been garbage collected", idx)
			}
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.PlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: placementBindingName, Namespace: appNamespaceName}},
				placementPolicy,
			}
			for idx := 0; idx < 9; idx++ {
				objs = append(objs, &placementv1alpha1.PlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx),
					Namespace: appNamespaceName,
				}})
			}
			objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})
})

var _ = Describe("core ops (cluster placement resource snapshots)", func() {
	Context("primary placement resource snapshot only (name-based selection)", Ordered, func() {
		// The environment prepared by the envtest package does not support namespace deletion; the
		// test case creates its objects in the shared app namespace, with unique names.
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		deploymentName := fmt.Sprintf(deploymentNameTemplate, utils.RandStr())

		var placementPolicy *placementv1alpha1.ClusterPlacementPolicy
		var wantHash string

		BeforeAll(func() {
			// Create a config map and a deployment in the app namespace.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"app": "demo"},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: appNamespaceName,
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
			Expect(hubClient.Create(ctx, deployment)).To(Succeed(), "Failed to create the Deployment")

			// Create a placement policy that selects the two resources.
			placementPolicy = &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementPolicyName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Namespace:  appNamespaceName,
							Name:       configMapName,
						},
						{
							APIGroup:   "apps",
							APIVersion: "v1",
							Kind:       "Deployment",
							Namespace:  appNamespaceName,
							Name:       deploymentName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a primary placement resource snapshot", func() {
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshot")
			Expect(createdSnapshots).To(HaveLen(1), "Unexpected number of created placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshot is not up-to-date")
		})

		It("can compute the hash of selected resources", func() {
			_, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			wantHash = hash
		})

		It("should persist the placement resource snapshot as expected", func() {
			snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)

			// Read the snapshot using the cached client; the cache might take a moment to catch up.
			gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, gotSnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot")

			// Verify the metadata.
			wantLabels := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
				placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "0",
				placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
				placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
			}
			if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot labels diff (-got +want):\n%s", diff))
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot annotations diff (-got +want):\n%s", diff))
			}
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot owner references diff (-got +want):\n%s", diff))
			}

			// Verify the snapshotted resources; they are sorted by their unique IDs, i.e., the config map comes first.
			wantIdentifiers := []placementv1alpha1.ObjectReference{
				{
					Namespace:  appNamespaceName,
					Name:       configMapName,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				{
					Namespace:  appNamespaceName,
					Name:       deploymentName,
					APIGroup:   "apps",
					APIVersion: "v1",
					Kind:       "Deployment",
				},
			}
			gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
			for idx := range gotSnapshot.Spec.Resources {
				gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
			}
			if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
				Fail(fmt.Sprintf("Snapshotted resource identifiers diff (-got +want):\n%s", diff))
			}

			// Verify the config map manifest.
			gotConfigMap := &corev1.ConfigMap{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
			wantConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"app": "demo"},
				},
				Data: map[string]string{"key": "value"},
			}
			if diff := cmp.Diff(gotConfigMap, wantConfigMap); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map diff (-got +want):\n%s", diff))
			}

			// Verify the Deployment manifest; the spec should match that of the live object (which carries the
			// defaulted fields), while the read-only metadata and status fields should have been stripped.
			liveDeployment := &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, liveDeployment)).To(Succeed(),
				"Failed to get the Deployment")
			gotDeployment := &appsv1.Deployment{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[1].Manifest.Raw, gotDeployment)).To(Succeed(), "Failed to unmarshal the Deployment manifest")
			wantDeployment := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"app": "demo"},
				},
				Spec: liveDeployment.Spec,
			}
			if diff := cmp.Diff(gotDeployment, wantDeployment, cmpopts.EquateEmpty()); diff != "" {
				Fail(fmt.Sprintf("Snapshotted Deployment diff (-got +want):\n%s", diff))
			}
		})

		It("should return the existing snapshot when no snapshot is needed", func() {
			snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			wantSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			// The previous spec has waited for the snapshot to appear in the cache.
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, wantSnapshot)).To(Succeed(),
				"Failed to get the placement resource snapshot")

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The existing placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")

			gotSnapshot, ok := gotSnapshots[0].(*placementv1alpha1.ClusterPlacementResourceSnapshot)
			Expect(ok).To(BeTrue(), "Unexpected type of the returned placement resource snapshot: %T", gotSnapshots[0])
			if diff := cmp.Diff(gotSnapshot, wantSnapshot, cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot diff (-got +want):\n%s", diff))
			}

			// No new snapshot should have been created.
			nextSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			err = hubUncachedReader.Get(ctx, types.NamespacedName{Name: nextSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "A new placement resource snapshot was created unexpectedly: %v", err)
		})

		It("should return the existing snapshot when it is not stale", func() {
			snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			wantSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, wantSnapshot)).To(Succeed(),
				"Failed to get the placement resource snapshot")

			// The selected resources have not changed since the snapshot was taken.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The existing placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")

			gotSnapshot, ok := gotSnapshots[0].(*placementv1alpha1.ClusterPlacementResourceSnapshot)
			Expect(ok).To(BeTrue(), "Unexpected type of the returned placement resource snapshot: %T", gotSnapshots[0])
			if diff := cmp.Diff(gotSnapshot, wantSnapshot, cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot diff (-got +want):\n%s", diff))
			}

			// No new snapshot should have been created.
			nextSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			err = hubUncachedReader.Get(ctx, types.NamespacedName{Name: nextSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "A new placement resource snapshot was created unexpectedly: %v", err)
		})

		It("can update the selected resources", func() {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = "new-value"
			configMap.Data["extra-key"] = "extra-value"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			deployment := &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, deployment)).To(Succeed(),
				"Failed to get the Deployment")
			deployment.Spec.Replicas = ptr.To(int32(3))
			deployment.Spec.Template.Spec.Containers[0].Image = "nginx:1.28"
			Expect(hubClient.Update(ctx, deployment)).To(Succeed(), "Failed to update the Deployment")
		})

		It("can create a new snapshot when the existing one is stale", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of placement resource snapshots")

			newSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			Expect(gotSnapshots[0].GetName()).To(Equal(newSnapshotName), "Unexpected name of the new placement resource snapshot")
		})

		It("can create a new snapshot when the existing one is stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after the updates")

			newSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Name: newSnapshotName}, gotSnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the new placement resource snapshot")

			wantLabels := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
				placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "1",
				placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
				placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
			}
			if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
				Fail(fmt.Sprintf("New placement resource snapshot labels diff (-got +want):\n%s", diff))
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: newHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
				Fail(fmt.Sprintf("New placement resource snapshot annotations diff (-got +want):\n%s", diff))
			}

			// Verify that the new snapshot carries the updated resources.
			Expect(gotSnapshot.Spec.Resources).To(HaveLen(2), "Unexpected number of snapshotted resources")
			gotConfigMap := &corev1.ConfigMap{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
			wantConfigMapData := map[string]string{"key": "new-value", "extra-key": "extra-value"}
			if diff := cmp.Diff(gotConfigMap.Data, wantConfigMapData); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map data diff (-got +want):\n%s", diff))
			}
			gotDeployment := &appsv1.Deployment{}
			Expect(json.Unmarshal(gotSnapshot.Spec.Resources[1].Manifest.Raw, gotDeployment)).To(Succeed(), "Failed to unmarshal the Deployment manifest")
			Expect(gotDeployment.Spec.Replicas).To(Equal(ptr.To(int32(3))), "Unexpected replica count in the snapshotted Deployment")
			Expect(gotDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:1.28"), "Unexpected image in the snapshotted Deployment")

			// The previous snapshot should be left intact.
			oldSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{
				Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
			}, oldSnapshot)).To(Succeed(), "Failed to get the previous placement resource snapshot")
			Expect(oldSnapshot.Annotations[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]).To(Equal(wantHash),
				"The previous placement resource snapshot has been modified")
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				}},
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1),
				}},
				placementPolicy,
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("primary and secondary placement resource snapshots (too many resources) (label-based selection)", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The names are in ascending order, which is also the order in which the selected resources are
		// snapshotted.
		configMapNames := make([]string, 5)
		for idx := range configMapNames {
			configMapNames[idx] = fmt.Sprintf("%s-%d", configMapNamePrefix, idx)
		}

		// The names of the config maps added later; they sort after the ones above.
		extraConfigMapNames := make([]string, 5)
		for idx := range extraConfigMapNames {
			extraConfigMapNames[idx] = fmt.Sprintf("%s-%d", configMapNamePrefix, idx+len(configMapNames))
		}

		allConfigMapNames := append(slices.Clone(configMapNames), extraConfigMapNames...)

		configMapIdentifiers := func(names []string) []placementv1alpha1.ObjectReference {
			identifiers := make([]placementv1alpha1.ObjectReference, 0, len(names))
			for _, name := range names {
				identifiers = append(identifiers, placementv1alpha1.ObjectReference{
					Namespace:  appNamespaceName,
					Name:       name,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				})
			}
			return identifiers
		}

		var placementPolicy *placementv1alpha1.ClusterPlacementPolicy
		var wantHash string

		BeforeAll(func() {
			// Create five config maps with the same label in the app namespace.
			for idx := range configMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapNames[idx],
						Namespace: appNamespaceName,
						Labels:    map[string]string{"placement-group": "foo"},
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx)},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}

			// Create a placement policy that selects the config maps by their label.
			placementPolicy = &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementPolicyName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Namespace:  appNamespaceName,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"placement-group": "foo"},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(HaveLen(len(configMapNames)), "Unexpected number of selected resources")
			wantHash = hash
		})

		It("can create a primary and a secondary placement resource snapshot", func() {
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshots are not up-to-date")
			Expect(createdSnapshots).To(HaveLen(2), "Unexpected number of created placement resource snapshots")

			// The snapshots are ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
			}
			gotNames := []string{createdSnapshots[0].GetName(), createdSnapshots[1].GetName()}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Created placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should persist the placement resource snapshots as expected", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			testCases := []struct {
				name            string
				snapshotName    string
				wantLabels      map[string]string
				wantIdentifiers []placementv1alpha1.ObjectReference
			}{
				{
					// Only the primary snapshot carries the count label.
					name:         "primary",
					snapshotName: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
						placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "2",
					},
					wantIdentifiers: configMapIdentifiers(configMapNames[:3]),
				},
				{
					name:         "secondary",
					snapshotName: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: "1",
					},
					wantIdentifiers: configMapIdentifiers(configMapNames[3:]),
				},
			}
			for _, tc := range testCases {
				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: tc.snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the %s placement resource snapshot", tc.name)

				if diff := cmp.Diff(gotSnapshot.Labels, tc.wantLabels); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot labels diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot annotations diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot owner references diff (-got +want):\n%s", tc.name, diff))
				}

				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, tc.wantIdentifiers); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot resource identifiers diff (-got +want):\n%s", tc.name, diff))
				}
			}
		})

		It("can create more config maps", func() {
			for idx, name := range extraConfigMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: appNamespaceName,
						Labels:    map[string]string{"placement-group": "foo"},
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx+len(configMapNames))},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}
		})

		It("should not create new snapshots if snapshots already exist", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			// The selected resources have changed since the snapshots were taken.
			Expect(isUpToDate).To(BeFalse(), "The existing placement resource snapshots are unexpectedly up-to-date")

			// The existing snapshots (at index 0) are returned, ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
			}
			gotNames := make([]string, 0, len(gotSnapshots))
			for _, snapshot := range gotSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot names diff (-got +want):\n%s", diff))
			}

			// No snapshots should have been created at the next index.
			nextSnapshotNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 1, 1),
			}
			for _, name := range nextSnapshotNames {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Name: name}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Placement resource snapshot %s was created unexpectedly: %v", name, err)
			}
		})

		// verifySnapshotsOfConfigMaps verifies that the given snapshots, created at the given index, are
		// the primary and secondary placement resource snapshots that cover the given config maps, and returns
		// the snapshots as persisted.
		//
		// The resources are spread across the snapshots in groups of three, in the order of their names.
		verifySnapshotsOfConfigMaps := func(
			snapshotIdx int,
			gotSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor,
			wantHash string,
			wantConfigMapNames []string,
		) []*placementv1alpha1.ClusterPlacementResourceSnapshot {
			wantSnapshotCnt := (len(wantConfigMapNames) + 2) / 3
			Expect(gotSnapshots).To(HaveLen(wantSnapshotCnt), "Unexpected number of placement resource snapshots")

			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			persistedSnapshots := make([]*placementv1alpha1.ClusterPlacementResourceSnapshot, len(gotSnapshots))
			for subIdx := range gotSnapshots {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx)
				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    fmt.Sprintf("%d", snapshotIdx),
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: fmt.Sprintf("%d", subIdx),
				}
				if subIdx == 0 {
					// Only the primary snapshot carries the count label.
					wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = fmt.Sprintf("%d", wantSnapshotCnt)
				} else {
					snapshotName = uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx, subIdx)
				}
				Expect(gotSnapshots[subIdx].GetName()).To(Equal(snapshotName), "Unexpected name of the placement resource snapshot at sub-index %d", subIdx)

				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at sub-index %d", subIdx)
				persistedSnapshots[subIdx] = gotSnapshot

				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) labels diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) annotations diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) owner references diff (-got +want):\n%s", subIdx, diff))
				}

				groupStart := subIdx * 3
				groupEnd := min(groupStart+3, len(wantConfigMapNames))
				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, configMapIdentifiers(wantConfigMapNames[groupStart:groupEnd])); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) resource identifiers diff (-got +want):\n%s", subIdx, diff))
				}
			}
			return persistedSnapshots
		}

		It("should create new snapshots if the existing ones are stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after adding config maps")
			wantHash = newHash

			// Ten resources, with at most three resources per snapshot: one primary and three secondary snapshots.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshots are not up-to-date")
			verifySnapshotsOfConfigMaps(1, gotSnapshots, wantHash, allConfigMapNames)
		})

		It("can update the last config map", func() {
			lastConfigMapName := extraConfigMapNames[len(extraConfigMapNames)-1]
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: lastConfigMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = "updated-value"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should create new snapshots again if the existing ones are stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after updating the config map")
			wantHash = newHash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshots are not up-to-date")
			persistedSnapshots := verifySnapshotsOfConfigMaps(2, gotSnapshots, wantHash, allConfigMapNames)

			// The last config map lives in the last secondary snapshot; it should carry the updated content.
			lastSnapshot := persistedSnapshots[len(persistedSnapshots)-1]
			Expect(lastSnapshot.Spec.Resources).To(HaveLen(1), "Unexpected number of resources in the last secondary snapshot")
			gotConfigMap := &corev1.ConfigMap{}
			Expect(json.Unmarshal(lastSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
			if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": "updated-value"}); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map data diff (-got +want):\n%s", diff))
			}
		})

		It("can delete some config maps", func() {
			for _, idx := range []int{2, 5, 9} {
				configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: allConfigMapNames[idx], Namespace: appNamespaceName}}
				Expect(hubClient.Delete(ctx, configMap)).To(Succeed(), "Failed to delete the config map")
			}
		})

		It("should create new snapshots if some selected resources have been removed", func() {
			// Seven resources are left, with at most three resources per snapshot: one primary and two secondary snapshots.
			var wantConfigMapNames []string
			for idx, name := range allConfigMapNames {
				if idx != 2 && idx != 5 && idx != 9 {
					wantConfigMapNames = append(wantConfigMapNames, name)
				}
			}

			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after deleting the config maps")
			wantHash = newHash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshots are not up-to-date")
			verifySnapshotsOfConfigMaps(3, gotSnapshots, wantHash, wantConfigMapNames)
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				}},
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
				}},
				placementPolicy,
			}
			var snapshotNames []string
			for snapshotIdx := 1; snapshotIdx <= 3; snapshotIdx++ {
				snapshotNames = append(snapshotNames, uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx))
				for subIdx := 1; subIdx <= 3; subIdx++ {
					snapshotNames = append(snapshotNames, uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, snapshotIdx, subIdx))
				}
			}
			for _, name := range snapshotNames {
				objs = append(objs, &placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name}})
			}
			for _, name := range allConfigMapNames {
				objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("primary and secondary placement resource snapshots (resources too large) (label-based selection)", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The name sorts after that of the first config map.
		secondConfigMapName := configMapName + "-2"

		var placementPolicy *placementv1alpha1.ClusterPlacementPolicy
		var wantHash string

		BeforeAll(func() {
			// Create a config map that is larger than the per-snapshot size limit of the manager under test.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"placement-group": "foo"},
				},
				Data: map[string]string{"key": strings.Repeat("a", 3000)},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			// Create a placement policy that selects the config map by its label.
			placementPolicy = &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementPolicyName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Namespace:  appNamespaceName,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"placement-group": "foo"},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("should return a user error when a selected resource is too large", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).To(HaveOccurred(), "Snapshotting resources succeeded unexpectedly")
			Expect(gotSnapshots).To(BeEmpty(), "Placement resource snapshots are returned unexpectedly")
			Expect(isUpToDate).To(BeFalse(), "The up-to-date flag is set unexpectedly")

			// The error category is the first key-value pair in the error attributes.
			gotErrAttrs := kferrors.Args(err)
			Expect(gotErrAttrs).ToNot(BeEmpty(), "The error is not a KubeFleet error: %v", err)
			wantErrCategoryAttrs := []interface{}{"errCategory", kferrors.ErrCategoryUser}
			if diff := cmp.Diff(gotErrAttrs[:2], wantErrCategoryAttrs); diff != "" {
				Fail(fmt.Sprintf("Error category attributes diff (-got +want):\n%s\nerror: %v", diff, err))
			}
		})

		It("should not create any placement resource snapshot", func() {
			snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
			Expect(hubUncachedReader.List(ctx, snapshotList, client.MatchingLabels{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey: placementPolicyName,
			})).To(Succeed(), "Failed to list placement resource snapshots")
			Expect(snapshotList.Items).To(BeEmpty(), "Placement resource snapshots are created unexpectedly")
		})

		It("can resize the config map and create another config map", func() {
			// Each config map now fits in a snapshot, but the two of them together do not.
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = strings.Repeat("a", 1500)
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			secondConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secondConfigMapName,
					Namespace: appNamespaceName,
					Labels:    map[string]string{"placement-group": "foo"},
				},
				Data: map[string]string{"key": strings.Repeat("b", 1500)},
			}
			Expect(hubClient.Create(ctx, secondConfigMap)).To(Succeed(), "Failed to create the second config map")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(HaveLen(2), "Unexpected number of selected resources")
			wantHash = hash
		})

		It("can create a primary and a secondary placement resource snapshot", func() {
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshots are not up-to-date")
			Expect(createdSnapshots).To(HaveLen(2), "Unexpected number of created placement resource snapshots")

			// The snapshots are ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
			}
			gotNames := []string{createdSnapshots[0].GetName(), createdSnapshots[1].GetName()}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Created placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should persist the placement resource snapshots as expected", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			testCases := []struct {
				name           string
				snapshotName   string
				wantLabels     map[string]string
				wantConfigMap  string
				wantConfigData string
			}{
				{
					// Only the primary snapshot carries the count label.
					name:         "primary",
					snapshotName: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
						placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "2",
					},
					wantConfigMap:  configMapName,
					wantConfigData: strings.Repeat("a", 1500),
				},
				{
					name:         "secondary",
					snapshotName: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
					wantLabels: map[string]string{
						placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
						placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
						placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: "1",
					},
					wantConfigMap:  secondConfigMapName,
					wantConfigData: strings.Repeat("b", 1500),
				},
			}
			for _, tc := range testCases {
				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: tc.snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the %s placement resource snapshot", tc.name)

				if diff := cmp.Diff(gotSnapshot.Labels, tc.wantLabels); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot labels diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot annotations diff (-got +want):\n%s", tc.name, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot owner references diff (-got +want):\n%s", tc.name, diff))
				}

				// Each snapshot holds exactly one of the two config maps.
				Expect(gotSnapshot.Spec.Resources).To(HaveLen(1), "Unexpected number of resources in the %s placement resource snapshot", tc.name)
				wantIdentifier := placementv1alpha1.ObjectReference{
					Namespace:  appNamespaceName,
					Name:       tc.wantConfigMap,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				}
				if diff := cmp.Diff(gotSnapshot.Spec.Resources[0].Identifier, wantIdentifier); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot resource identifier diff (-got +want):\n%s", tc.name, diff))
				}
				gotConfigMap := &corev1.ConfigMap{}
				Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(),
					"Failed to unmarshal the config map manifest in the %s placement resource snapshot", tc.name)
				if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": tc.wantConfigData}); diff != "" {
					Fail(fmt.Sprintf("The %s placement resource snapshot config map data diff (-got +want):\n%s", tc.name, diff))
				}
			}
		})

		It("can shrink the config maps", func() {
			// The two config maps now fit in a single snapshot together.
			for _, name := range []string{configMapName, secondConfigMapName} {
				configMap := &corev1.ConfigMap{}
				Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: name}, configMap)).To(Succeed(),
					"Failed to get the config map")
				configMap.Data["key"] = strings.Repeat("c", 800)
				Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
			}
		})

		It("should create a primary placement resource snapshot only if the existing snapshots are stale", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after shrinking the config maps")
			wantHash = newHash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of created placement resource snapshots")
			wantName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			Expect(gotSnapshots[0].GetName()).To(Equal(wantName), "Unexpected name of the new placement resource snapshot")

			// Read the snapshot using the cached client; the cache might take a moment to catch up.
			gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Name: wantName}, gotSnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the new placement resource snapshot")

			wantLabels := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
				placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           "1",
				placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
				placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
			}
			if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
				Fail(fmt.Sprintf("The new placement resource snapshot labels diff (-got +want):\n%s", diff))
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}
			if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
				Fail(fmt.Sprintf("The new placement resource snapshot annotations diff (-got +want):\n%s", diff))
			}
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
				Fail(fmt.Sprintf("The new placement resource snapshot owner references diff (-got +want):\n%s", diff))
			}

			// Both config maps live in the primary snapshot, with the updated content.
			Expect(gotSnapshot.Spec.Resources).To(HaveLen(2), "Unexpected number of resources in the new placement resource snapshot")
			for idx, name := range []string{configMapName, secondConfigMapName} {
				wantIdentifier := placementv1alpha1.ObjectReference{
					Namespace:  appNamespaceName,
					Name:       name,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				}
				if diff := cmp.Diff(gotSnapshot.Spec.Resources[idx].Identifier, wantIdentifier); diff != "" {
					Fail(fmt.Sprintf("The new placement resource snapshot resource identifier (index %d) diff (-got +want):\n%s", idx, diff))
				}
				gotConfigMap := &corev1.ConfigMap{}
				Expect(json.Unmarshal(gotSnapshot.Spec.Resources[idx].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
				if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": strings.Repeat("c", 800)}); diff != "" {
					Fail(fmt.Sprintf("The new placement resource snapshot config map data (index %d) diff (-got +want):\n%s", idx, diff))
				}
			}

			// No secondary snapshot should have been created at the new index.
			secondaryName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 1, 1)
			err = hubUncachedReader.Get(ctx, types.NamespacedName{Name: secondaryName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "A secondary placement resource snapshot was created unexpectedly: %v", err)
		})

		AfterAll(func() {
			objs := []client.Object{
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				}},
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
				}},
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1),
				}},
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: secondConfigMapName, Namespace: appNamespaceName}},
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("partial placement resource snapshot provisioning", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The names are in ascending order, which is also the order in which the selected resources are
		// snapshotted.
		configMapNames := make([]string, 10)
		for idx := range configMapNames {
			configMapNames[idx] = fmt.Sprintf("%s-%d", configMapNamePrefix, idx)
		}

		var placementPolicy *placementv1alpha1.ClusterPlacementPolicy
		var wantHash string
		// The UIDs of the secondary placement resource snapshots (by sub-index minus one), recorded for
		// later verification.
		var secondarySnapshotUIDs []types.UID

		// verifyOrphanedSecondariesCleanedUp verifies that the manager cleans up the orphaned secondary placement
		// resource snapshots (the primary snapshot is missing) and returns a transient error.
		//
		// The manager treats the secondary snapshots left without a primary snapshot as orphans: it deletes them
		// and returns a transient error, asking the caller to requeue before creating the snapshots anew.
		verifyOrphanedSecondariesCleanedUp := func() {
			// Other errors (e.g., those caused by a stale cache) might surface first; retry until the expected
			// transient error is observed.
			Eventually(func() error {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				if err == nil {
					return fmt.Errorf("snapshotting resources succeeded unexpectedly (snapshots %d, up-to-date %t)", len(gotSnapshots), isUpToDate)
				}
				if !strings.Contains(err.Error(), "cleaned up orphaned secondary placement resource snapshots") {
					return fmt.Errorf("unexpected error message: %w", err)
				}
				if len(gotSnapshots) != 0 || isUpToDate {
					return fmt.Errorf("unexpected return values alongside the error (snapshots %d, up-to-date %t)", len(gotSnapshots), isUpToDate)
				}

				// The error category is the first key-value pair in the error attributes.
				gotErrAttrs := kferrors.Args(err)
				if len(gotErrAttrs) < 2 {
					return fmt.Errorf("the error is not a KubeFleet error: %w", err)
				}
				wantErrCategoryAttrs := []interface{}{"errCategory", kferrors.ErrCategoryTransient}
				if diff := cmp.Diff(gotErrAttrs[:2], wantErrCategoryAttrs); diff != "" {
					return fmt.Errorf("error category attributes diff (-got +want):\n%s\nerror: %w", diff, err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the transient error from the clean-up of orphaned secondary placement resource snapshots")

			// The orphaned secondary snapshots should have been deleted.
			for subIdx := 1; subIdx <= 3; subIdx++ {
				secondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				Eventually(func() bool {
					err := hubUncachedReader.Get(ctx, types.NamespacedName{Name: secondarySnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
					return errors.IsNotFound(err)
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the orphaned secondary placement resource snapshot at sub-index %d", subIdx)
			}
		}

		BeforeAll(func() {
			// Create ten config maps with the same label in the app namespace.
			for idx := range configMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapNames[idx],
						Namespace: appNamespaceName,
						Labels:    map[string]string{"placement-group": "foo"},
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx)},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}

			// Create a placement policy that selects the config maps by their label.
			placementPolicy = &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementPolicyName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Namespace:  appNamespaceName,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"placement-group": "foo"},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(HaveLen(len(configMapNames)), "Unexpected number of selected resources")
			wantHash = hash
		})

		It("can create a primary and three secondary placement resource snapshots", func() {
			// Ten resources, with at most three resources per snapshot: one primary and three secondary snapshots.
			createdSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The created placement resource snapshots are not up-to-date")
			Expect(createdSnapshots).To(HaveLen(4), "Unexpected number of created placement resource snapshots")

			// The snapshots are ordered by their sub-indices.
			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 2),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 3),
			}
			gotNames := make([]string, 0, len(createdSnapshots))
			for _, snapshot := range createdSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Created placement resource snapshot names diff (-got +want):\n%s", diff))
			}

			// Write down the UIDs of the secondary placement resource snapshots.
			secondarySnapshotUIDs = make([]types.UID, 0, len(createdSnapshots)-1)
			for _, snapshot := range createdSnapshots[1:] {
				Expect(snapshot.GetUID()).ToNot(BeEmpty(), "The secondary placement resource snapshot %s has no UID", snapshot.GetName())
				secondarySnapshotUIDs = append(secondarySnapshotUIDs, snapshot.GetUID())
			}
		})

		It("should persist the placement resource snapshots as expected", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			for subIdx := 0; subIdx < 4; subIdx++ {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: fmt.Sprintf("%d", subIdx),
				}
				if subIdx == 0 {
					// Only the primary snapshot carries the count label.
					wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = "4"
				} else {
					snapshotName = uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				}

				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at sub-index %d", subIdx)

				if subIdx > 0 {
					Expect(gotSnapshot.UID).To(Equal(secondarySnapshotUIDs[subIdx-1]), "Unexpected UID of the placement resource snapshot at sub-index %d", subIdx)
				}
				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) labels diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) annotations diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) owner references diff (-got +want):\n%s", subIdx, diff))
				}

				// The resources are spread across the snapshots in groups of three, in the order of their names.
				groupStart := subIdx * 3
				groupEnd := min(groupStart+3, len(configMapNames))
				wantIdentifiers := make([]placementv1alpha1.ObjectReference, 0, groupEnd-groupStart)
				for _, name := range configMapNames[groupStart:groupEnd] {
					wantIdentifiers = append(wantIdentifiers, placementv1alpha1.ObjectReference{
						Namespace:  appNamespaceName,
						Name:       name,
						APIVersion: "v1",
						Kind:       "ConfigMap",
					})
				}
				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) resource identifiers diff (-got +want):\n%s", subIdx, diff))
				}
			}
		})

		It("can delete the primary placement resource snapshot", func() {
			// Note that this is for simulation purposes only: it mimics a partial provisioning, where the primary
			// snapshot is missing while its secondary snapshots remain. The manager never deletes only the primary
			// snapshot in normal operations, so this situation will not occur in practice.
			primarySnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				},
			}
			Expect(hubClient.Delete(ctx, primarySnapshot)).To(Succeed(), "Failed to delete the primary placement resource snapshot")

			// Wait until the cache has observed the deletion.
			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, client.ObjectKeyFromObject(primarySnapshot), primarySnapshot))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the primary placement resource snapshot")
		})

		It("should clean up the orphaned secondary placement resource snapshots and return a transient error", func() {
			verifyOrphanedSecondariesCleanedUp()
		})

		It("can re-create the primary and secondary placement resource snapshots", func() {
			// The orphaned secondary snapshots have been cleaned up; the manager can now create the snapshots anew.
			// The call is retried as the cache might not have observed the clean-up yet.
			var recreatedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
			var isUpToDate bool
			Eventually(func() error {
				var err error
				recreatedSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				return err
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The re-created placement resource snapshots are not up-to-date")

			wantNames := []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 1),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 2),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, 3),
			}
			gotNames := make([]string, 0, len(recreatedSnapshots))
			for _, snapshot := range recreatedSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Re-created placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should have re-created the primary snapshot and the secondary snapshots with new UIDs", func() {
			// The primary snapshot has been re-created (its UID is not checked).
			primarySnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
			primarySnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			Eventually(func() error {
				return hubClient.Get(ctx, types.NamespacedName{Name: primarySnapshotName}, primarySnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the primary placement resource snapshot")
			Expect(primarySnapshot.Annotations).To(HaveKeyWithValue(placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey, wantHash),
				"Unexpected contents hash of the re-created primary placement resource snapshot")
			Expect(primarySnapshot.Labels).To(HaveKeyWithValue(placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey, "4"),
				"Unexpected snapshot count of the re-created primary placement resource snapshot")

			// All the secondary snapshots have been re-created with new UIDs.
			for subIdx := 1; subIdx <= 3; subIdx++ {
				secondarySnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				secondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: secondarySnapshotName}, secondarySnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the secondary placement resource snapshot at sub-index %d", subIdx)
				Expect(secondarySnapshot.UID).ToNot(Equal(secondarySnapshotUIDs[subIdx-1]),
					"The secondary placement resource snapshot at sub-index %d has not been re-created (UID unchanged)", subIdx)
				Expect(secondarySnapshot.Annotations).To(HaveKeyWithValue(placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey, wantHash),
					"Unexpected contents hash of the re-created secondary placement resource snapshot at sub-index %d", subIdx)
			}
		})

		It("can delete the primary placement resource snapshot again", func() {
			// Record the UIDs of the secondary placement resource snapshots that were re-created previously.
			secondarySnapshotUIDs = make([]types.UID, 0, 3)
			for subIdx := 1; subIdx <= 3; subIdx++ {
				secondarySnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Expect(hubClient.Get(ctx, types.NamespacedName{
					Name: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx),
				}, secondarySnapshot)).To(Succeed(), "Failed to get the secondary placement resource snapshot at sub-index %d", subIdx)
				secondarySnapshotUIDs = append(secondarySnapshotUIDs, secondarySnapshot.UID)
			}

			// Note that this is for simulation purposes only: it mimics a partial provisioning, where the primary
			// snapshot is missing while its secondary snapshots remain. The manager never deletes only the primary
			// snapshot in normal operations, so this situation will not occur in practice.
			primarySnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				},
			}
			Expect(hubClient.Delete(ctx, primarySnapshot)).To(Succeed(), "Failed to delete the primary placement resource snapshot")

			// Wait until the cache has observed the deletion.
			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, client.ObjectKeyFromObject(primarySnapshot), primarySnapshot))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove the primary placement resource snapshot")
		})

		It("can update the last config map", func() {
			lastConfigMapName := configMapNames[len(configMapNames)-1]
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: lastConfigMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = "updated-value"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should clean up the orphaned secondary placement resource snapshots and return a transient error again", func() {
			verifyOrphanedSecondariesCleanedUp()
		})

		It("can re-create the primary and secondary placement resource snapshots with the updated content", func() {
			_, newHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(newHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed after updating the config map")
			wantHash = newHash

			// The orphaned secondary snapshots have been cleaned up; the manager can now create the snapshots anew.
			// The call is retried as the cache might not have observed the clean-up yet.
			var recreatedSnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor
			var isUpToDate bool
			Eventually(func() error {
				var err error
				recreatedSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				return err
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to re-create the placement resource snapshots")
			Expect(isUpToDate).To(BeTrue(), "The re-created placement resource snapshots are not up-to-date")
			Expect(recreatedSnapshots).To(HaveLen(4), "Unexpected number of re-created placement resource snapshots")
		})

		It("should have re-created all the snapshots with the expected content", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}
			wantAnnotations := map[string]string{
				placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: wantHash,
				placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey:      placementPolicyName,
			}

			for subIdx := 0; subIdx < 4; subIdx++ {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:  placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:    "0",
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey: fmt.Sprintf("%d", subIdx),
				}
				if subIdx == 0 {
					// Only the primary snapshot carries the count label.
					wantLabels[placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey] = "4"
				} else {
					snapshotName = uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx)
				}

				// Read the snapshot using the cached client; the cache might take a moment to catch up.
				gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at sub-index %d", subIdx)

				if subIdx > 0 {
					Expect(gotSnapshot.UID).ToNot(Equal(secondarySnapshotUIDs[subIdx-1]),
						"The secondary placement resource snapshot at sub-index %d has not been re-created (UID unchanged)", subIdx)
				}
				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) labels diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.Annotations, wantAnnotations); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) annotations diff (-got +want):\n%s", subIdx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) owner references diff (-got +want):\n%s", subIdx, diff))
				}

				// The resources are spread across the snapshots in groups of three, in the order of their names.
				groupStart := subIdx * 3
				groupEnd := min(groupStart+3, len(configMapNames))
				wantIdentifiers := make([]placementv1alpha1.ObjectReference, 0, groupEnd-groupStart)
				for _, name := range configMapNames[groupStart:groupEnd] {
					wantIdentifiers = append(wantIdentifiers, placementv1alpha1.ObjectReference{
						Namespace:  appNamespaceName,
						Name:       name,
						APIVersion: "v1",
						Kind:       "ConfigMap",
					})
				}
				gotIdentifiers := make([]placementv1alpha1.ObjectReference, 0, len(gotSnapshot.Spec.Resources))
				for idx := range gotSnapshot.Spec.Resources {
					gotIdentifiers = append(gotIdentifiers, gotSnapshot.Spec.Resources[idx].Identifier)
				}
				if diff := cmp.Diff(gotIdentifiers, wantIdentifiers); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) resource identifiers diff (-got +want):\n%s", subIdx, diff))
				}

				// Verify the content of each config map; only the last one has been updated.
				for idx := range gotSnapshot.Spec.Resources {
					configMapIdx := groupStart + idx
					wantData := map[string]string{"key": fmt.Sprintf("value-%d", configMapIdx)}
					if configMapIdx == len(configMapNames)-1 {
						wantData["key"] = "updated-value"
					}
					gotConfigMap := &corev1.ConfigMap{}
					Expect(json.Unmarshal(gotSnapshot.Spec.Resources[idx].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
					if diff := cmp.Diff(gotConfigMap.Data, wantData); diff != "" {
						Fail(fmt.Sprintf("The placement resource snapshot (sub-index %d) config map data (index %d) diff (-got +want):\n%s", subIdx, idx, diff))
					}
				}
			}
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0),
				}},
				placementPolicy,
			}
			for subIdx := 1; subIdx <= 3; subIdx++ {
				objs = append(objs, &placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyName, 0, subIdx),
				}})
			}
			for _, name := range configMapNames {
				objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
			}
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})

	Context("GC", Ordered, func() {
		placementPolicyName := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())

		var placementPolicy *placementv1alpha1.ClusterPlacementPolicy

		// updateConfigMapAndSnapshot updates the config map with the given data and calls the manager to snapshot
		// the selected resources; it returns the snapshots created.
		updateConfigMapAndSnapshot := func(data string) []placementv1alpha1.PlacementResourceSnapshotAccessor {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Data["key"] = data
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The new placement resource snapshot is not up-to-date")
			Expect(gotSnapshots).To(HaveLen(1), "Unexpected number of created placement resource snapshots")

			// Wait until the cache has observed the new snapshot; otherwise the next call might not see it.
			Eventually(func() error {
				return hubClient.Get(ctx, client.ObjectKeyFromObject(gotSnapshots[0]), &placementv1alpha1.ClusterPlacementResourceSnapshot{})
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the new placement resource snapshot")
			return gotSnapshots
		}

		BeforeAll(func() {
			// Create a config map in the app namespace.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
				},
				Data: map[string]string{"key": "initial-value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			// Create a placement policy that selects the config map.
			placementPolicy = &placementv1alpha1.ClusterPlacementPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementPolicyName,
				},
				Spec: placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Namespace:  appNamespaceName,
							Name:       configMapName,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create four primary placement resource snapshots", func() {
			for idx := 0; idx < 4; idx++ {
				gotSnapshots := updateConfigMapAndSnapshot(fmt.Sprintf("value-%d", idx))
				wantName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx)
				Expect(gotSnapshots[0].GetName()).To(Equal(wantName), "Unexpected name of the placement resource snapshot at index %d", idx)
			}
		})

		It("should have four primary placement resource snapshots at the expected indexes", func() {
			wantOwnerRefs := []metav1.OwnerReference{
				{
					APIVersion:         placementv1alpha1.GroupVersion.String(),
					Kind:               "ClusterPlacementPolicy",
					Name:               placementPolicyName,
					UID:                placementPolicy.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			}

			seenHashes := map[string]bool{}
			for idx := 0; idx < 4; idx++ {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx)
				gotSnapshot := &placementv1alpha1.ClusterPlacementResourceSnapshot{}
				Eventually(func() error {
					return hubClient.Get(ctx, types.NamespacedName{Name: snapshotName}, gotSnapshot)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placement resource snapshot at index %d", idx)

				wantLabels := map[string]string{
					placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey:         placementPolicyName,
					placementv1alpha1.PlacementResourceSnapshotIndexLabelKey:           fmt.Sprintf("%d", idx),
					placementv1alpha1.PlacementResourceSnapshotSubIndexLabelKey:        "0",
					placementv1alpha1.SubIndexedPlacementResourceSnapshotCountLabelKey: "1",
				}
				if diff := cmp.Diff(gotSnapshot.Labels, wantLabels); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (index %d) labels diff (-got +want):\n%s", idx, diff))
				}
				if diff := cmp.Diff(gotSnapshot.OwnerReferences, wantOwnerRefs); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (index %d) owner references diff (-got +want):\n%s", idx, diff))
				}

				// Each snapshot tracks a different state of the config map.
				hash := gotSnapshot.Annotations[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
				Expect(hash).ToNot(BeEmpty(), "The placement resource snapshot (index %d) has no contents hash", idx)
				Expect(seenHashes).ToNot(HaveKey(hash), "The placement resource snapshot (index %d) has the same contents hash as another snapshot", idx)
				seenHashes[hash] = true

				Expect(gotSnapshot.Spec.Resources).To(HaveLen(1), "Unexpected number of resources in the placement resource snapshot (index %d)", idx)
				gotConfigMap := &corev1.ConfigMap{}
				Expect(json.Unmarshal(gotSnapshot.Spec.Resources[0].Manifest.Raw, gotConfigMap)).To(Succeed(), "Failed to unmarshal the config map manifest")
				if diff := cmp.Diff(gotConfigMap.Data, map[string]string{"key": fmt.Sprintf("value-%d", idx)}); diff != "" {
					Fail(fmt.Sprintf("The placement resource snapshot (index %d) config map data diff (-got +want):\n%s", idx, diff))
				}
			}

			// There should be no other snapshots.
			snapshotList := &placementv1alpha1.ClusterPlacementResourceSnapshotList{}
			Expect(hubUncachedReader.List(ctx, snapshotList, client.MatchingLabels{
				placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey: placementPolicyName,
			})).To(Succeed(), "Failed to list placement resource snapshots")
			Expect(snapshotList.Items).To(HaveLen(4), "Unexpected number of placement resource snapshots")
		})

		It("should garbage collect the first primary placement resource snapshot", func() {
			updateConfigMapAndSnapshot("value-4")

			firstSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 0)
			Eventually(func() bool {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Name: firstSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
				return errors.IsNotFound(err)
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The first primary placement resource snapshot has not been garbage collected")
		})

		It("should garbage collect the second primary placement resource snapshot", func() {
			updateConfigMapAndSnapshot("value-5")

			secondSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 1)
			Eventually(func() bool {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Name: secondSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
				return errors.IsNotFound(err)
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The second primary placement resource snapshot has not been garbage collected")
		})

		It("can create a placement binding that uses the third primary placement resource snapshot", func() {
			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			placementBinding := &placementv1alpha1.ClusterPlacementBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementBindingName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: placementv1alpha1.GroupVersion.String(),
							Kind:       placementv1alpha1.ClusterPlacementPolicyKind,
							Name:       placementPolicyName,
							UID:        placementPolicy.UID,
						},
					},
				},
				Spec: placementv1alpha1.PlacementBindingSpec{
					PlacementPolicyName:  placementPolicyName,
					ClusterName:          "member-1",
					ResourceSnapshotName: thirdSnapshotName,
				},
			}
			Expect(hubClient.Create(ctx, placementBinding)).To(Succeed(), "Failed to create the placement binding")

			// Wait until the cache (and its field index) has observed the placement binding; the garbage collection
			// process relies on it to tell if a snapshot is still in use.
			wantFieldVal := fmt.Sprintf(
				fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, placementPolicyName, thirdSnapshotName)
			Eventually(func() (int, error) {
				placementBindingList := &placementv1alpha1.ClusterPlacementBindingList{}
				err := hubClient.List(ctx, placementBindingList, client.MatchingFields{
					fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: wantFieldVal,
				})
				return len(placementBindingList.Items), err
			}, eventuallyDuration, eventuallyInterval).Should(Equal(1), "Failed to find the placement binding in the cache")
		})

		It("should not garbage collect the third primary placement resource snapshot as it is in use", func() {
			// The third snapshot is now the oldest one that is past the revision history limit; the garbage
			// collection process should skip it as a placement binding is using it.
			updateConfigMapAndSnapshot("value-6")

			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			Consistently(func() error {
				return hubUncachedReader.Get(ctx, types.NamespacedName{Name: thirdSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The third primary placement resource snapshot has been garbage collected unexpectedly")
		})

		It("should garbage collect the fourth primary placement resource snapshot but keep the third one", func() {
			// The third and the fourth snapshots are now both past the revision history limit; the third one is
			// still in use by a placement binding, so only the fourth one should be garbage collected.
			updateConfigMapAndSnapshot("value-7")

			fourthSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 3)
			Eventually(func() bool {
				err := hubUncachedReader.Get(ctx, types.NamespacedName{Name: fourthSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
				return errors.IsNotFound(err)
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The fourth primary placement resource snapshot has not been garbage collected")

			// The garbage collection requests are processed in order; the request for the third snapshot has been
			// handled by now.
			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			Consistently(func() error {
				return hubUncachedReader.Get(ctx, types.NamespacedName{Name: thirdSnapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The third primary placement resource snapshot has been garbage collected unexpectedly")
		})

		It("can delete the placement binding", func() {
			placementBinding := &placementv1alpha1.ClusterPlacementBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: placementBindingName,
				},
			}
			Expect(hubClient.Delete(ctx, placementBinding)).To(Succeed(), "Failed to delete the placement binding")

			// Wait until the cache (and its field index) has observed the deletion; the garbage collection process
			// relies on it to tell if a snapshot is still in use.
			thirdSnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, 2)
			Eventually(func() (int, error) {
				placementBindingList := &placementv1alpha1.ClusterPlacementBindingList{}
				err := hubClient.List(ctx, placementBindingList, client.MatchingFields{
					fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: fmt.Sprintf(
						fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, placementPolicyName, thirdSnapshotName),
				})
				return len(placementBindingList.Items), err
			}, eventuallyDuration, eventuallyInterval).Should(Equal(0), "The placement binding is still found in the cache")
		})

		It("should garbage collect the third and the fifth primary placement resource snapshots", func() {
			// The third snapshot is no longer in use; both the third and the fifth snapshots are now past the
			// revision history limit.
			updateConfigMapAndSnapshot("value-8")

			for _, idx := range []int{2, 4} {
				snapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx)
				Eventually(func() bool {
					err := hubUncachedReader.Get(ctx, types.NamespacedName{Name: snapshotName}, &placementv1alpha1.ClusterPlacementResourceSnapshot{})
					return errors.IsNotFound(err)
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "The primary placement resource snapshot at index %d has not been garbage collected", idx)
			}
		})

		AfterAll(func() {
			// The envtest environment does not run the garbage collector; remove all the objects explicitly.
			objs := []client.Object{
				&placementv1alpha1.ClusterPlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: placementBindingName}},
				placementPolicy,
			}
			for idx := 0; idx < 9; idx++ {
				objs = append(objs, &placementv1alpha1.ClusterPlacementResourceSnapshot{ObjectMeta: metav1.ObjectMeta{
					Name: uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyName, idx),
				}})
			}
			objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			for _, obj := range objs {
				Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %T", obj)
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Failed to remove object %T", obj)
			}
		})
	})
})
