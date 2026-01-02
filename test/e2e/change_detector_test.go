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

// Test scenarios:
// 1. Basic change detection: Verify Create, Update, Delete config map detection works
// 2. CRD discovery: Verify InformerPopulator discovers new CRDs and ChangeDetector detects CR updates

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// Test 1: Basic change detection - verifies Create/Update/Delete config map detection
var _ = Describe("validating ChangeDetector detects resource changes", Label("resourceplacement"), Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		// Only create the namespace (not the config map yet)
		createNamespace()

		// Create the CRP that selects the namespace
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crpName,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						Name:    appNamespace().Name,
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	AfterAll(func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should select namespace only before config map creation", func() {
		// Expect only the namespace to be selected (no config map yet)
		expectedIdentifiers := workNamespaceIdentifiers()
		crpStatusUpdatedActual := crpStatusUpdatedActual(expectedIdentifiers, allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(),
			"CRP should have initial snapshot with namespace selected")
	})

	It("should create config map", func() {
		// Now create the config map
		configMap := appConfigMap()
		Expect(hubClient.Create(ctx, &configMap)).To(Succeed(), "Failed to create config map")

		klog.InfoS("Config map created", "configMap", configMap.Name)
	})

	It("should select namespace and config map", func() {
		// After creating the config map, expect both namespace and config map to be selected
		expectedIdentifiers := workResourceIdentifiers()
		crpStatusUpdatedActual := crpStatusUpdatedActual(expectedIdentifiers, allMemberClusterNames, nil, "1")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(),
			"CRP should have new snapshot with namespace and config map selected")
	})

	It("should propagate config map to all member clusters", func() {
		configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		name := types.NamespacedName{Name: configMapName, Namespace: appNamespace().Name}

		for _, cluster := range allMemberClusters {
			Eventually(func() error {
				return validateConfigMapOnCluster(cluster, name)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(),
				"ConfigMap should be propagated to member cluster %s", cluster.ClusterName)
		}
	})

	It("should update config map data", func() {
		configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		// Update ConfigMap data
		configMap := &corev1.ConfigMap{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: appNamespace().Name}, configMap)).Should(Succeed())
		configMap.Data["data"] = "updated-value"
		Expect(hubClient.Update(ctx, configMap)).Should(Succeed(), "Failed to update config map data")

		klog.InfoS("Config map data updated", "configMap", configMapName)
	})

	It("should propagate config map updates to member clusters", func() {
		configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())
		name := types.NamespacedName{Name: configMapName, Namespace: appNamespace().Name}

		for _, cluster := range allMemberClusters {
			Eventually(func() error {
				return validateConfigMapOnCluster(cluster, name)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(),
				"ConfigMap updates should be propagated to member cluster %s", cluster.ClusterName)
		}
	})

	It("should delete config map", func() {
		configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

		// Delete the ConfigMap
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: appNamespace().Name,
			},
		}
		Expect(hubClient.Delete(ctx, configMap)).To(Succeed(), "Failed to delete config map")

		klog.InfoS("Config map deleted", "configMap", configMapName)
	})

	It("should update CRP status after config map deletion", func() {
		// Verify CRP status updated to show only namespace selected (config map removed)
		expectedIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Group:     "",
				Version:   "v1",
				Kind:      "Namespace",
				Name:      appNamespace().Name,
				Namespace: "",
			},
		}
		// Snapshot progression:
		// Index 0: namespace only (initial)
		// Index 1: namespace + config map (after creation)
		// Index 2: namespace + config map (after update)
		// Index 3: namespace only (after deletion) <- current state
		crpStatusUpdatedActual := crpStatusUpdatedActual(expectedIdentifiers, allMemberClusterNames, nil, "3")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(),
			"CRP should detect config map deletion and update snapshot")
	})

	It("should remove config map from all member clusters", func() {
		checkIfRemovedConfigMapFromMemberClusters(allMemberClusters)
	})
})

// Test 2: CRD discovery - verifies InformerPopulator discovers new CRDs and ChangeDetector detects CR updates
var _ = Describe("validating InformerPopulator discovers new CRDs", Label("resourceplacement"), Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	crdName := fmt.Sprintf("testcrds-%d.kubefleet.test", GinkgoParallelProcess())
	crName := fmt.Sprintf("test-cr-%d", GinkgoParallelProcess())

	BeforeAll(func() {
		// Create namespace
		createNamespace()
	})

	AfterAll(func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)

		// Clean up CRD
		crd := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: crdName,
			},
		}
		Expect(hubClient.Delete(ctx, crd)).To(Succeed(), "Failed to delete CRD")
	})

	It("should create CRD", func() {
		crd := testCRD()
		Expect(hubClient.Create(ctx, &crd)).To(Succeed(), "Failed to create CRD")

		klog.InfoS("CRD created", "crd", crdName)
	})

	It("should establish CRD", func() {
		waitForCRDToBeReady(crdName)
		klog.InfoS("CRD established", "crd", crdName)
	})

	It("should create custom resource", func() {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "kubefleet.test",
			Version: "v1",
			Kind:    testCRD().Spec.Names.Kind,
		})
		cr.SetName(crName)
		cr.SetNamespace(appNamespace().Name)
		cr.Object["spec"] = map[string]interface{}{
			"field": "initial-value",
		}
		Expect(hubClient.Create(ctx, cr)).To(Succeed(), "Failed to create custom resource")

		klog.InfoS("Custom resource created", "cr", crName)
	})

	It("should create CRP", func() {
		// Create CRP to select the CRD and namespace
		// The custom resource instance will be automatically included because it's in the selected namespace
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crpName,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   "apiextensions.k8s.io",
						Kind:    "CustomResourceDefinition",
						Version: "v1",
						Name:    crdName,
					},
					{
						Group:   "",
						Kind:    "Namespace",
						Version: "v1",
						Name:    appNamespace().Name,
					},
				},
				Strategy: placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						UnavailablePeriodSeconds: ptr.To(2),
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP")
	})

	It("should update CRP status with selected resources", func() {
		expectedIdentifiers := []placementv1beta1.ResourceIdentifier{
			{
				Group:     "",
				Version:   "v1",
				Kind:      "Namespace",
				Name:      appNamespace().Name,
				Namespace: "",
			},
			{
				Group:     "apiextensions.k8s.io",
				Version:   "v1",
				Kind:      "CustomResourceDefinition",
				Name:      crdName,
				Namespace: "",
			},
			{
				Group:     "kubefleet.test",
				Version:   "v1",
				Kind:      testCRD().Spec.Names.Kind,
				Name:      crName,
				Namespace: appNamespace().Name,
			},
		}
		// Use customizedPlacementStatusUpdatedActual with resourceIsTrackable=false
		// because CRDs and custom resources don't have availability tracking
		crpKey := types.NamespacedName{Name: crpName}
		crpStatusUpdatedActual := customizedPlacementStatusUpdatedActual(crpKey, expectedIdentifiers, allMemberClusterNames, nil, "0", false)
		// Use workloadEventuallyDuration (45s) to account for InformerPopulator's 30s discovery cycle
		// The InformerPopulator needs up to 30s to discover the new CRD, then ChangeDetector can watch it
		Eventually(crpStatusUpdatedActual, workloadEventuallyDuration, eventuallyInterval).Should(Succeed(),
			"CRP should have selected namespace, CRD, and custom resource")
	})

	It("should propagate custom resource to all member clusters", func() {
		name := types.NamespacedName{Name: crName, Namespace: appNamespace().Name}
		gvk := schema.GroupVersionKind{
			Group:   "kubefleet.test",
			Version: "v1",
			Kind:    testCRD().Spec.Names.Kind,
		}

		for _, cluster := range allMemberClusters {
			Eventually(func() error {
				return validateCustomResourceOnCluster(cluster, name, gvk)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(),
				"Custom resource should be propagated to member cluster %s", cluster.ClusterName)
		}
	})

	It("should update custom resource", func() {
		// Update the custom resource
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "kubefleet.test",
			Version: "v1",
			Kind:    testCRD().Spec.Names.Kind,
		})
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: crName, Namespace: appNamespace().Name}, cr)).To(Succeed())
		cr.Object["spec"] = map[string]interface{}{
			"field": "updated-value",
		}
		Expect(hubClient.Update(ctx, cr)).To(Succeed(), "Failed to update custom resource")

		klog.InfoS("Custom resource updated", "cr", crName)
	})

	It("should propagate custom resource updates to all member clusters", func() {
		name := types.NamespacedName{Name: crName, Namespace: appNamespace().Name}
		gvk := schema.GroupVersionKind{
			Group:   "kubefleet.test",
			Version: "v1",
			Kind:    testCRD().Spec.Names.Kind,
		}

		for _, cluster := range allMemberClusters {
			Eventually(func() error {
				return validateCustomResourceOnCluster(cluster, name, gvk)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(),
				"Custom resource updates should be propagated to member cluster %s", cluster.ClusterName)
		}
	})
})
