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
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
	testutilseviction "github.com/kubefleet-dev/kubefleet/test/utils/eviction"
	toolsutils "github.com/kubefleet-dev/kubefleet/tools/utils"
)

var _ = Describe("Drain cluster successfully", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var drainEvictions []placementv1beta1.ClusterResourcePlacementEviction
	var drainClusters, noDrainClusters []*framework.Cluster
	var noDrainClusterNames []string
	var testStartTime time.Time

	BeforeAll(func() {
		testStartTime = time.Now()
		drainClusters = []*framework.Cluster{memberCluster1EastProd}
		noDrainClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
		noDrainClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		// remove drain evictions.
		for _, eviction := range drainEvictions {
			ensureCRPEvictionDeleted(eviction.Name)
		}
		// remove taints from member cluster 1 again to guarantee clean up of cordon taint on test failure.
		removeTaintsFromMemberClusters([]string{memberCluster1EastProdName})
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("drain cluster using binary, should succeed", func() { runDrainClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should update member cluster with cordon taint", func() {
		taintAddedActual := memberClusterCordonTaintAddedActual(memberCluster1EastProdName)
		Eventually(taintAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cordon taint to member cluster")
	})

	It("should update drain cluster resource placement evictions status as expected", func() {
		var fetchError error
		drainEvictions, fetchError = fetchDrainEvictions(crpName, memberCluster1EastProdName, testStartTime)
		Expect(fetchError).Should(Succeed(), "Failed to fetch drain evictions")
		for _, eviction := range drainEvictions {
			crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, hubClient, eviction.Name,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: condition.EvictionAllowedNoPDBMessage})
			Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
		}
	})

	It("should ensure no resources exist on drained clusters", func() {
		for _, cluster := range drainClusters {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if resources doesn't exist on member cluster")
		}
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noDrainClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the selected clusters which were not drained", func() {
		for _, cluster := range noDrainClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should remove cordon taint from member cluster", func() {
		taintRemovedActual := memberClusterCordonTaintRemovedActual(memberCluster1EastProdName)
		Eventually(taintRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove cordon taint from member cluster")
	})
})

var _ = Describe("Drain cluster blocked - ClusterResourcePlacementDisruptionBudget blocks evictions on all clusters", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var drainEvictions []placementv1beta1.ClusterResourcePlacementEviction
	var testStartTime time.Time

	BeforeAll(func() {
		testStartTime = time.Now()
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		// remove drain evictions.
		for _, eviction := range drainEvictions {
			ensureCRPEvictionDeleted(eviction.Name)
		}
		// remove taints from member cluster 1 again to guarantee clean up of cordon taint on test failure.
		removeTaintsFromMemberClusters([]string{memberCluster1EastProdName})
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block draining", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: int32(len(allMemberClusterNames)),
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

	It("drain cluster using binary, should fail due to CRPDB", func() { runDrainClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should update member cluster with cordon taint", func() {
		taintAddedActual := memberClusterCordonTaintAddedActual(memberCluster1EastProdName)
		Eventually(taintAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cordon taint to member cluster")
	})

	It("should update drain cluster resource placement evictions status as expected", func() {
		var fetchError error
		drainEvictions, fetchError = fetchDrainEvictions(crpName, memberCluster1EastProdName, testStartTime)
		Expect(fetchError).Should(Succeed(), "Failed to fetch drain evictions")
		for _, eviction := range drainEvictions {
			crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, hubClient, eviction.Name,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 3, 3)})
			Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
		}
	})

	It("should ensure cluster resource placement status remains unchanged", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should remove cordon taint from member cluster", func() {
		taintRemovedActual := memberClusterCordonTaintRemovedActual(memberCluster1EastProdName)
		Eventually(taintRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove cordon taint from member cluster")
	})
})

var _ = Describe("Drain is allowed on one cluster, blocked on others - ClusterResourcePlacementDisruptionBudget blocks evictions on some clusters", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var drainEvictions []placementv1beta1.ClusterResourcePlacementEviction
	var drainClusters, noDrainClusters []*framework.Cluster
	var noDrainClusterNames []string
	var testStartTime time.Time

	BeforeAll(func() {
		testStartTime = time.Now()
		drainClusters = []*framework.Cluster{memberCluster1EastProd}
		noDrainClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
		noDrainClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		createCRP(crpName)
	})

	AfterAll(func() {
		// remove remaining drain evictions.
		for _, eviction := range drainEvictions {
			ensureCRPEvictionDeleted(eviction.Name)
		}
		// remove taints from member clusters 1,2 again to guarantee clean up of cordon taint on test failure.
		removeTaintsFromMemberClusters([]string{memberCluster1EastProdName, memberCluster2EastCanaryName})
		ensureCRPDisruptionBudgetDeleted(crpName)
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("create cluster resource placement disruption budget to block draining on all but one cluster", func() {
		crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: int32(len(allMemberClusterNames)) - 1,
				},
			},
		}
		Expect(hubClient.Create(ctx, &crpdb)).To(Succeed(), "Failed to create CRP Disruption Budget %s", crpName)
	})

	It("drain cluster using binary, should succeed", func() { runDrainClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should update member cluster with cordon taint", func() {
		taintAddedActual := memberClusterCordonTaintAddedActual(memberCluster1EastProdName)
		Eventually(taintAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cordon taint to member cluster")
	})

	It("should update drain cluster resource placement evictions status as expected", func() {
		var fetchError error
		drainEvictions, fetchError = fetchDrainEvictions(crpName, memberCluster1EastProdName, testStartTime)
		Expect(fetchError).Should(Succeed(), "Failed to fetch drain evictions")
		for _, eviction := range drainEvictions {
			crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, hubClient, eviction.Name,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 3, 3)})
			Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
		}
	})

	It("remove drain evictions for member cluster 1", func() {
		for _, eviction := range drainEvictions {
			ensureCRPEvictionDeleted(eviction.Name)
		}
	})

	It("drain cluster using binary, should fail due to CRPDB", func() { runDrainClusterBinary(hubClusterName, memberCluster2EastCanaryName) })

	It("should update member cluster with cordon taint", func() {
		taintAddedActual := memberClusterCordonTaintAddedActual(memberCluster2EastCanaryName)
		Eventually(taintAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cordon taint to member cluster")
	})

	It("should update drain cluster resource placement evictions status as expected", func() {
		var fetchError error
		drainEvictions, fetchError = fetchDrainEvictions(crpName, memberCluster2EastCanaryName, testStartTime)
		Expect(fetchError).Should(Succeed(), "Failed to fetch drain evictions")
		for _, eviction := range drainEvictions {
			crpEvictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, hubClient, eviction.Name,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 2, 2)})
			Eventually(crpEvictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement eviction status as expected")
		}
	})

	It("should ensure no resources exist on drained clusters", func() {
		for _, cluster := range drainClusters {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if resources doesn't exist on member cluster")
		}
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noDrainClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
		Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should still place resources on the selected clusters which were not drained", func() {
		for _, cluster := range noDrainClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster1EastProdName) })

	It("should remove cordon taint from member cluster", func() {
		taintRemovedActual := memberClusterCordonTaintRemovedActual(memberCluster1EastProdName)
		Eventually(taintRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove cordon taint from member cluster")
	})

	It("uncordon cluster using binary", func() { runUncordonClusterBinary(hubClusterName, memberCluster2EastCanaryName) })

	It("should remove cordon taint from member cluster", func() {
		taintRemovedActual := memberClusterCordonTaintRemovedActual(memberCluster2EastCanaryName)
		Eventually(taintRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove cordon taint from member cluster")
	})
})

func runDrainClusterBinary(hubClusterName, memberClusterName string) {
	By(fmt.Sprintf("draining cluster %s", memberClusterName))
	cmd := exec.Command(drainBinaryPath,
		"--hubClusterContext", hubClusterName,
		"--clusterName", memberClusterName)
	_, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "Drain command failed with error: %v", err)
}

func runUncordonClusterBinary(hubClusterName, memberClusterName string) {
	By(fmt.Sprintf("uncordoning cluster %s", memberClusterName))
	cmd := exec.Command(uncordonBinaryPath,
		"--hubClusterContext", hubClusterName,
		"--clusterName", memberClusterName)
	_, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "Uncordon command failed with error: %v", err)
}

func fetchDrainEvictions(crpName, clusterName string, testStartTime time.Time) ([]placementv1beta1.ClusterResourcePlacementEviction, error) {
	var evictionList placementv1beta1.ClusterResourcePlacementEvictionList
	if err := hubClient.List(ctx, &evictionList); err != nil {
		return nil, err
	}
	var filteredDrainEvictions []placementv1beta1.ClusterResourcePlacementEviction
	for _, eviction := range evictionList.Items {
		if eviction.CreationTimestamp.Time.After(testStartTime) &&
			eviction.Spec.PlacementName == crpName &&
			eviction.Spec.ClusterName == clusterName {
			filteredDrainEvictions = append(filteredDrainEvictions, eviction)
		}
	}
	return filteredDrainEvictions, nil
}

func memberClusterCordonTaintAddedActual(mcName string) func() error {
	return func() error {
		var mc clusterv1beta1.MemberCluster
		if err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
			return fmt.Errorf("failed to get member cluster %s: %w", mcName, err)
		}

		for _, taint := range mc.Spec.Taints {
			if taint == toolsutils.CordonTaint {
				return nil
			}
		}
		return fmt.Errorf("cordon taint not found on member cluster %s", mcName)
	}
}

func memberClusterCordonTaintRemovedActual(mcName string) func() error {
	return func() error {
		var mc clusterv1beta1.MemberCluster
		if err := hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
			return fmt.Errorf("failed to get member cluster %s: %w", mcName, err)
		}

		for _, taint := range mc.Spec.Taints {
			if taint == toolsutils.CordonTaint {
				return fmt.Errorf("cordon taint found on member cluster %s", mcName)
			}
		}
		return nil
	}
}
