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
package e2e

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// Helper functions for NamespaceWithResourceSelectors tests

// deleteCRP deletes a ClusterResourcePlacement.
func deleteCRP(crpName string) {
	crp := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
	}
	Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP %s", crpName)
}

// checkCRPStatusWithResources verifies the CRP status matches expected resources.
func checkCRPStatusWithResources(crpName string, expectedResources []placementv1beta1.ResourceIdentifier) {
	crpStatusUpdatedActual := crpStatusUpdatedActual(expectedResources, allMemberClusterNames, nil, "0")
	Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update CRP %s status as expected", crpName)
}

// checkFinalizersRemoved verifies controller finalizers are removed from CRP.
func checkFinalizersRemoved(crpName string) {
	finalizerRemovedActual := allFinalizersExceptForCustomDeletionBlockerRemovedFromPlacementActual(types.NamespacedName{Name: crpName})
	Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove controller finalizers from CRP %s", crpName)
}

// Tests for NamespaceWithResourceSelectors SelectionScope mode
var _ = Describe("CRP with NamespaceWithResourceSelectors selecting single namespace and specific resources", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	testNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	configMapName := fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating test namespace and resources")
		createWorkResources()
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting CRP and related resources %s", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should create CRP with NamespaceWithResourceSelectors mode", func() {
		createCRPWithSelectors(crpName, []placementv1beta1.ResourceSelectorTerm{
			{
				Group:          "",
				Kind:           "Namespace",
				Version:        "v1",
				Name:           testNamespace,
				SelectionScope: placementv1beta1.NamespaceWithResourceSelectors,
			},
			{
				Group:   "",
				Kind:    "ConfigMap",
				Version: "v1",
				Name:    configMapName,
			},
		})
	})

	It("should update CRP status as expected", func() {
		checkCRPStatusWithResources(crpName, []placementv1beta1.ResourceIdentifier{
			{
				Group:   "",
				Kind:    "Namespace",
				Version: "v1",
				Name:    testNamespace,
			},
			{
				Group:     "",
				Kind:      "ConfigMap",
				Version:   "v1",
				Name:      configMapName,
				Namespace: testNamespace,
			},
		})
	})

	It("should place the namespace and configmap on member clusters", func() {
		checkNamespacePlacedOnClusters(testNamespace)
		checkConfigMapPlacedOnClusters(testNamespace, configMapName)
	})

	It("can delete the CRP", func() {
		deleteCRP(crpName)
	})

	It("should remove placed resources from all member clusters", checkIfRemovedWorkResourcesFromAllMemberClusters)

	It("should remove controller finalizers from CRP", func() {
		checkFinalizersRemoved(crpName)
	})
})

var _ = Describe("CRP with NamespaceWithResourceSelectors selecting only namespace", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	testNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating test namespace and resources")
		createWorkResources()
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting CRP and related resources %s", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should create CRP with NamespaceWithResourceSelectors selecting only namespace", func() {
		createCRPWithSelectors(crpName, []placementv1beta1.ResourceSelectorTerm{
			{
				Group:          "",
				Kind:           "Namespace",
				Version:        "v1",
				Name:           testNamespace,
				SelectionScope: placementv1beta1.NamespaceWithResourceSelectors,
			},
		})
	})

	It("should update CRP status with only namespace", func() {
		checkCRPStatusWithResources(crpName, []placementv1beta1.ResourceIdentifier{
			{
				Group:   "",
				Kind:    "Namespace",
				Version: "v1",
				Name:    testNamespace,
			},
		})
	})

	It("should place only the namespace on member clusters", func() {
		checkNamespacePlacedOnClusters(testNamespace)
	})

	It("can delete the CRP", func() {
		deleteCRP(crpName)
	})

	It("should remove placed namespace from all member clusters", func() {
		checkNamespaceRemovedFromClusters(testNamespace)
	})

	It("should remove controller finalizers from CRP", func() {
		checkFinalizersRemoved(crpName)
	})
})

var _ = Describe("CRP with NamespaceWithResourceSelectors with cluster-scoped resources", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	testNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	clusterRoleName := fmt.Sprintf("test-clusterrole-%d", GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating test namespace")
		createWorkResources()
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting CRP and related resources %s", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should create CRP with NamespaceWithResourceSelectors and cluster-scoped resource", func() {
		createCRPWithSelectors(crpName, []placementv1beta1.ResourceSelectorTerm{
			{
				Group:          "",
				Kind:           "Namespace",
				Version:        "v1",
				Name:           testNamespace,
				SelectionScope: placementv1beta1.NamespaceWithResourceSelectors,
			},
			{
				Group:   "rbac.authorization.k8s.io",
				Kind:    "ClusterRole",
				Version: "v1",
				Name:    clusterRoleName,
			},
		})
	})

	It("should update CRP status with namespace only (ClusterRole doesn't exist)", func() {
		checkCRPStatusWithResources(crpName, []placementv1beta1.ResourceIdentifier{
			{
				Group:   "",
				Kind:    "Namespace",
				Version: "v1",
				Name:    testNamespace,
			},
		})
	})

	It("can delete the CRP", func() {
		deleteCRP(crpName)
	})

	It("should remove placed resources from all member clusters", func() {
		checkNamespaceRemovedFromClusters(testNamespace)
	})

	It("should remove controller finalizers from CRP", func() {
		checkFinalizersRemoved(crpName)
	})
})

// Negative test case: selecting a non-existent namespace should show error in status
var _ = Describe("CRP with NamespaceWithResourceSelectors selecting non-existent namespace", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	nonExistentNamespace := fmt.Sprintf("non-existent-ns-%d", GinkgoParallelProcess())

	AfterAll(func() {
		By("cleaning up CRP")
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should create CRP that selects a non-existent namespace by name", func() {
		createCRPWithSelectors(crpName, []placementv1beta1.ResourceSelectorTerm{
			{
				Group:          "",
				Kind:           "Namespace",
				Version:        "v1",
				Name:           nonExistentNamespace,
				SelectionScope: placementv1beta1.NamespaceWithResourceSelectors,
			},
		})
	})

	It("should show error in CRP status for non-existent namespace", func() {
		Eventually(func() bool {
			var crp placementv1beta1.ClusterResourcePlacement
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp); err != nil {
				return false
			}
			// Check if there's a condition indicating the error about namespace not being selected
			for _, cond := range crp.Status.Conditions {
				if strings.Contains(cond.Message, "NamespaceWithResourceSelectors mode requires exactly one namespace, but no namespaces were selected") {
					return true
				}
			}
			return false
		}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "CRP %s should have error condition for non-existent namespace", crpName)
	})

	It("can delete the CRP", func() {
		deleteCRP(crpName)
	})

	It("should remove controller finalizers from CRP", func() {
		checkFinalizersRemoved(crpName)
	})
})
