/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package tests

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("scheduling CRPs on member clusters with taints & tolerations", func() {
	// This is a serial test as adding taints can affect other tests
	Context("pickFixed, valid target clusters with taints", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		targetClusters := []string{memberCluster1EastProd, memberCluster4CentralProd, memberCluster6WestProd}
		taintClusters := targetClusters

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Add taints to some member clusters 1, 4, 6 from all regions.
			addTaintsToMemberClusters(taintClusters, buildTaints(taintClusters))

			// Create the CRP and its associated policy snapshot.
			createPickFixedCRPWithPolicySnapshot(crpName, targetClusters, policySnapshotName)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for valid target clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(targetClusters, nilScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickFixedPolicySnapshotStatusUpdatedActual(targetClusters, []string{}, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report correct policy snapshot status")
		})

		AfterAll(func() {
			// Remove taints
			removeTaintsFromMemberClusters(taintClusters)
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	// This is a serial test as adding taints can affect other tests.
	Context("pick all valid cluster with no taints, ignore valid cluster with taints, CRP with no matching toleration", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		taintClusters := []string{memberCluster1EastProd, memberCluster4CentralProd, memberCluster7WestCanary}
		selectedClusters := []string{memberCluster2EastProd, memberCluster3EastCanary, memberCluster5CentralProd, memberCluster6WestProd}
		unSelectedClusters := []string{memberCluster1EastProd, memberCluster4CentralProd, memberCluster7WestCanary, memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Add taints to some member clusters 1, 4, 7 from all regions.
			addTaintsToMemberClusters(taintClusters, buildTaints(taintClusters))

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot, with no tolerations specified.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for all healthy clusters with no taints", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(selectedClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for unhealthy clusters, healthy cluster with taints", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(unSelectedClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(selectedClusters, unSelectedClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Remove taints
			removeTaintsFromMemberClusters(taintClusters)
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	// This is a serial test as adding taints, tolerations can affect other tests.
	Context("pick all valid cluster with tolerated taints, ignore valid clusters with taints, CRP has some matching tolerations on creation", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		taintClusters := []string{memberCluster1EastProd, memberCluster2EastProd, memberCluster6WestProd}
		tolerateClusters := []string{memberCluster1EastProd, memberCluster2EastProd}
		selectedClusters := tolerateClusters
		unSelectedClusters := []string{memberCluster3EastCanary, memberCluster4CentralProd, memberCluster5CentralProd, memberCluster6WestProd, memberCluster7WestCanary, memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Add taints to some member clusters 1, 2, 6 from all regions.
			addTaintsToMemberClusters(taintClusters, buildTaints(taintClusters))

			// Create a CRP with affinity, tolerations for clusters 1,2.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											envLabel: "prod",
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      regionLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"east", "west"},
											},
										},
									},
								},
							},
						},
					},
				},
				Tolerations: buildTolerations(tolerateClusters),
			}
			// Create CRP .
			createPickAllCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create scheduled bindings for clusters with tolerated taints", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(selectedClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		It("should not create any binding for clusters with untolerated taints", func() {
			noBindingsCreatedActual := noBindingsCreatedForClustersActual(unSelectedClusters, crpName)
			Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		})

		It("should report status correctly", func() {
			statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(selectedClusters, unSelectedClusters, policySnapshotName)
			Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
			Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
		})

		AfterAll(func() {
			// Remove taints
			removeTaintsFromMemberClusters(taintClusters)
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	// This is a serial test as adding taints, tolerations can affect other tests.
	Context("pick N clusters with affinity specified, ignore valid clusters with taints, CRP has some matching tolerations after update", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		policySnapshotNameAfter := fmt.Sprintf(policySnapshotNameTemplate, crpName, 2)
		numOfClusters := int32(2) // Less than the number of clusters available (7) in the fleet.
		taintClusters := []string{memberCluster1EastProd, memberCluster2EastProd}
		tolerateClusters := taintClusters
		// The scheduler is designed to produce only deterministic decisions; if there are no
		// comparable scores available for selected clusters, the scheduler will rank the clusters
		// by their names.
		wantFilteredClusters := []string{memberCluster1EastProd, memberCluster2EastProd, memberCluster3EastCanary, memberCluster4CentralProd, memberCluster5CentralProd, memberCluster6WestProd, memberCluster7WestCanary, memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd}
		wantPickedClustersAfter := taintClusters
		wantFilteredClustersAfter := []string{memberCluster3EastCanary, memberCluster4CentralProd, memberCluster5CentralProd, memberCluster6WestProd, memberCluster7WestCanary, memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Add taints to some member clusters 1, 2.
			addTaintsToMemberClusters(taintClusters, buildTaints(taintClusters))

			policy := &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &numOfClusters,
				Affinity: &placementv1beta1.Affinity{
					ClusterAffinity: &placementv1beta1.ClusterAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											regionLabel: "east",
										},
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      envLabel,
												Operator: metav1.LabelSelectorOpIn,
												Values: []string{
													"prod",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			// Create a CRP of the PickN placement type, along with its associated policy snapshot, no tolerations specified.
			createPickNCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, 0)
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual([]string{}, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(2, []string{}, []string{}, wantFilteredClusters, zeroScoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		It("update CRP with new tolerations", func() {
			// Update CRP with tolerations for clusters 1,2.
			updatePickNCRPWithTolerations(crpName, buildTolerations(tolerateClusters), policySnapshotName, policySnapshotNameAfter)
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, 2)
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual([]string{}, zeroScoreByCluster, crpName, policySnapshotNameAfter)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(2, wantPickedClustersAfter, []string{}, wantFilteredClustersAfter, zeroScoreByCluster, policySnapshotNameAfter)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Remove taints
			removeTaintsFromMemberClusters(taintClusters)
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("pickAll, add a new healthy cluster with taint", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		// Prepare a new cluster to avoid interrupting other concurrently running test cases.
		newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot.
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, nil)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, buildTaints([]string{newUnhealthyMemberClusterName}))

			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)
		})

		It("should create scheduled bindings for existing clusters, and exclude new cluster with taint", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(healthyClusters, zeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("pickAll, add a new healthy cluster with taint and matching toleration", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		// Prepare a new cluster to avoid interrupting other concurrently running test cases.
		newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())
		updatedHealthyClusters := healthyClusters
		updatedHealthyClusters = append(updatedHealthyClusters, newUnhealthyMemberClusterName)
		updatedZeroScoreByCluster := make(map[string]*placementv1beta1.ClusterScore)
		for k, v := range zeroScoreByCluster {
			updatedZeroScoreByCluster[k] = v
		}
		updatedZeroScoreByCluster[newUnhealthyMemberClusterName] = &zeroScore

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP with no scheduling policy specified, along with its associated policy snapshot, and toleration for new cluster.
			policy := &placementv1beta1.PlacementPolicy{
				Tolerations: buildTolerations([]string{newUnhealthyMemberClusterName}),
			}
			createNilSchedulingPolicyCRPWithPolicySnapshot(crpName, policySnapshotName, policy)

			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, buildTaints([]string{newUnhealthyMemberClusterName}))

			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)
		})

		It("should create scheduled bindings for the newly recovered cluster with tolerated taint", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("pickN with required topology spread constraints, add new cluster with taint, upscaling doesn't pick new cluster", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		// Prepare a new cluster to avoid interrupting other concurrently running test cases.
		newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())
		numOfClusters := int32(2)
		numOfClustersAfter := int32(3)
		wantPickedClusters := []string{memberCluster7WestCanary, memberCluster5CentralProd}
		wantNotPickedClusters := []string{memberCluster1EastProd, memberCluster2EastProd, memberCluster3EastCanary, memberCluster4CentralProd, memberCluster6WestProd}
		wantFilteredClusters := []string{memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd}
		wantPickedClustersAfter := []string{memberCluster7WestCanary, memberCluster5CentralProd, memberCluster3EastCanary}
		wantNotPickedClustersAfter := []string{memberCluster1EastProd, memberCluster2EastProd, memberCluster4CentralProd, memberCluster6WestProd, newUnhealthyMemberClusterName}
		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd:    &zeroScore,
			memberCluster2EastProd:    &zeroScore,
			memberCluster3EastCanary:  &zeroScore,
			memberCluster4CentralProd: &zeroScore,
			// Cluster 5 is picked in the second iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			// Cluster 6 is considered to be unschedulable in the second iteration as placing
			// resources on it would violate the topology spread constraint (skew becomes 2,
			// the limit is 1); unschedulable clusters do not have scores assigned.
			memberCluster6WestProd: nil,
			// Cluster 7 is picked in the first iteration, as placing resources on it does not
			// violate any topology spread constraints + increases the skew only by one (so do other
			// clusters), and its name is the largest in alphanumeric order. It is assigned
			// a topology spread score of -1 as placing resources on it increases the skew.
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
		}
		scoreByClusterAfter := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd: &zeroScore,
			memberCluster2EastProd: &zeroScore,
			// Cluster 3 is picked in the third iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged.
			memberCluster3EastCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			memberCluster4CentralProd: nil,
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			memberCluster6WestProd: nil,
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			newUnhealthyMemberClusterName: nil,
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: &numOfClusters,
				TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:           ptr.To(int32(1)),
						TopologyKey:       regionLabel,
						WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
					},
				},
			}
			// Create CRP with no tolerations specified.
			createPickNCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, scoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, wantNotPickedClusters, wantFilteredClusters, scoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		It("create a new member cluster", func() {
			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, buildTaints([]string{newUnhealthyMemberClusterName}))
			// Retrieve the cluster object.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: newUnhealthyMemberClusterName}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Add the region label.
			memberCluster.Labels = map[string]string{
				regionLabel: "north",
				envLabel:    "prod",
			}
			Expect(hubClient.Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster")
			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)
		})

		It("upscale policy to pick 3 clusters", func() {
			// Update the policy snapshot.
			//
			// Normally upscaling is done by increasing the number of clusters field in the CRP;
			// however, since in the integration test environment, CRP controller is not available,
			// we directly manipulate the number of clusters annoation on the policy snapshot
			// to trigger upscaling.
			Eventually(func() error {
				policySnapshot := &placementv1beta1.ClusterSchedulingPolicySnapshot{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: policySnapshotName}, policySnapshot); err != nil {
					return err
				}

				policySnapshot.Annotations[placementv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(int(numOfClustersAfter))
				return hubClient.Update(ctx, policySnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClustersAfter))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersAfter, scoreByClusterAfter, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClustersAfter), wantPickedClustersAfter, wantNotPickedClustersAfter, wantFilteredClusters, scoreByClusterAfter, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})

	// This is a serial test as adding a new member cluster may interrupt other test cases.
	Context("pickN with required topology spread constraints, add new cluster with taint, upscaling picks new cluster with tolerated taint", Serial, Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, 1)
		// Prepare a new cluster to avoid interrupting other concurrently running test cases.
		newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())
		numOfClusters := int32(2)
		numOfClustersAfter := int32(3)
		wantPickedClusters := []string{memberCluster7WestCanary, memberCluster5CentralProd}
		wantNotPickedClusters := []string{memberCluster1EastProd, memberCluster2EastProd, memberCluster3EastCanary, memberCluster4CentralProd, memberCluster6WestProd}
		wantFilteredClusters := []string{memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd}
		wantPickedClustersAfter := []string{memberCluster7WestCanary, memberCluster5CentralProd, newUnhealthyMemberClusterName}
		wantNotPickedClustersAfter := wantNotPickedClusters
		scoreByCluster := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd:    &zeroScore,
			memberCluster2EastProd:    &zeroScore,
			memberCluster3EastCanary:  &zeroScore,
			memberCluster4CentralProd: &zeroScore,
			// Cluster 5 is picked in the second iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged.
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			// Cluster 6 is considered to be unschedulable in the second iteration as placing
			// resources on it would violate the topology spread constraint (skew becomes 2,
			// the limit is 1); unschedulable clusters do not have scores assigned.
			memberCluster6WestProd: nil,
			// Cluster 7 is picked in the first iteration, as placing resources on it does not
			// violate any topology spread constraints + increases the skew only by one (so do other
			// clusters), and its name is the largest in alphanumeric order. It is assigned
			// a topology spread score of -1 as placing resources on it increases the skew.
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
		}
		scoreByClusterAfter := map[string]*placementv1beta1.ClusterScore{
			memberCluster1EastProd:    &zeroScore,
			memberCluster2EastProd:    &zeroScore,
			memberCluster3EastCanary:  &zeroScore,
			memberCluster4CentralProd: nil,
			memberCluster5CentralProd: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
			memberCluster6WestProd: nil,
			memberCluster7WestCanary: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(-1)),
			},
			// new cluster is picked in the third iteration, as placing resources on it does
			// not violate any topology spread constraints + does not increase the skew. It
			// is assigned a topology spread score of 0 as the skew is unchanged.
			newUnhealthyMemberClusterName: {
				AffinityScore:       ptr.To(int32(0)),
				TopologySpreadScore: ptr.To(int32(0)),
			},
		}

		BeforeAll(func() {
			// Ensure that no bindings have been created so far.
			noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
			Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

			// Create a CRP of the PickN placement type, along with its associated policy snapshot.
			topologySpreadConstraints := []placementv1beta1.TopologySpreadConstraint{
				{
					MaxSkew:           ptr.To(int32(1)),
					TopologyKey:       regionLabel,
					WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
				},
			}
			policy := &placementv1beta1.PlacementPolicy{
				PlacementType:             placementv1beta1.PickNPlacementType,
				NumberOfClusters:          &numOfClusters,
				TopologySpreadConstraints: topologySpreadConstraints,
				Tolerations:               buildTolerations([]string{newUnhealthyMemberClusterName}),
			}
			// Create CRP with toleration for new cluster.
			createPickNCRPWithPolicySnapshot(crpName, policySnapshotName, policy)
		})

		It("should add scheduler cleanup finalizer to the CRP", func() {
			finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, len(wantPickedClusters))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClusters, scoreByCluster, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClusters), wantPickedClusters, wantNotPickedClusters, wantFilteredClusters, scoreByCluster, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		It("create a new member cluster", func() {
			// Create a new member cluster.
			createMemberCluster(newUnhealthyMemberClusterName, buildTaints([]string{newUnhealthyMemberClusterName}))
			// Retrieve the cluster object.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: newUnhealthyMemberClusterName}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Add the region label.
			memberCluster.Labels = map[string]string{
				regionLabel: "north",
				envLabel:    "prod",
			}
			Expect(hubClient.Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster")
			// Mark this cluster as healthy.
			markClusterAsHealthy(newUnhealthyMemberClusterName)
		})

		It("upscale policy to pick 3 clusters", func() {
			// Update the policy snapshot.
			//
			// Normally upscaling is done by increasing the number of clusters field in the CRP;
			// however, since in the integration test environment, CRP controller is not available,
			// we directly manipulate the number of clusters annoation on the policy snapshot
			// to trigger upscaling.
			Eventually(func() error {
				policySnapshot := &placementv1beta1.ClusterSchedulingPolicySnapshot{}
				if err := hubClient.Get(ctx, types.NamespacedName{Name: policySnapshotName}, policySnapshot); err != nil {
					return err
				}

				policySnapshot.Annotations[placementv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(int(numOfClustersAfter))
				return hubClient.Update(ctx, policySnapshot)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update policy snapshot")
		})

		It("should create N bindings", func() {
			hasNScheduledOrBoundBindingsActual := hasNScheduledOrBoundBindingsPresentActual(crpName, int(numOfClustersAfter))
			Eventually(hasNScheduledOrBoundBindingsActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create N bindings")
			Consistently(hasNScheduledOrBoundBindingsActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create N bindings")
		})

		It("should create scheduled bindings for selected clusters", func() {
			scheduledBindingsCreatedActual := scheduledBindingsCreatedOrUpdatedForClustersActual(wantPickedClustersAfter, scoreByClusterAfter, crpName, policySnapshotName)
			Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
			Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create scheduled bindings for selected clusters")
		})

		It("should report status correctly", func() {
			crpStatusUpdatedActual := pickNPolicySnapshotStatusUpdatedActual(int(numOfClustersAfter), wantPickedClustersAfter, wantNotPickedClustersAfter, wantFilteredClusters, scoreByClusterAfter, policySnapshotName)
			Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report status correctly")
			Consistently(crpStatusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to report status correctly")
		})

		AfterAll(func() {
			// Delete the CRP.
			ensureCRPAndAllRelatedResourcesDeletion(crpName)
			// Delete the provisional cluster.
			ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
		})
	})
})
