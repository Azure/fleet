/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This test suite features a number of test cases which cover the workflow of scheduling CRPs
// of the PickAll placement type.

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("scheduling CRPs with no scheduling policy specified", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	policySnapshotIdx := 1
	policySnapshotName := fmt.Sprintf(policySnapshotNameTemplate, crpName, policySnapshotIdx)

	// Prepare a new cluster to avoid interrupting other concurrently running test cases.
	newUnhealthyMemberClusterName := fmt.Sprintf(provisionalClusterNameTemplate, GinkgoParallelProcess())
	updatedHealthyClusters := healthyClusters
	updatedHealthyClusters = append(updatedHealthyClusters, newUnhealthyMemberClusterName)

	// Copy the map to avoid interrupting other concurrently running test cases.
	updatedZeroScoreByCluster := make(map[string]*placementv1beta1.ClusterScore)
	for k, v := range zeroScoreByCluster {
		updatedZeroScoreByCluster[k] = v
	}
	updatedZeroScoreByCluster[newUnhealthyMemberClusterName] = &zeroScore

	BeforeAll(func() {
		// Ensure that no bindings have been created so far.
		noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
		Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")

		// Create a CRP with no scheduling policy specified.
		crp := placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name:       crpName,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: defaultResourceSelectors,
				Policy:            nil,
			},
		}
		Expect(hubClient.Create(ctx, &crp)).Should(Succeed(), "Failed to create CRP")

		crpGeneration := crp.Generation

		// Create the associated policy snapshot.
		policySnapshot := &placementv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: policySnapshotName,
				Labels: map[string]string{
					placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
					placementv1beta1.CRPTrackingLabel:      crpName,
				},
				Annotations: map[string]string{
					placementv1beta1.CRPGenerationAnnotation: strconv.FormatInt(crpGeneration, 10),
				},
			},
			Spec: placementv1beta1.SchedulingPolicySnapshotSpec{
				Policy:     nil,
				PolicyHash: []byte(policyHash),
			},
		}
		Expect(hubClient.Create(ctx, policySnapshot)).Should(Succeed(), "Failed to create policy snapshot")
	})

	It("should add scheduler cleanup finalizer to the CRP", func() {
		finalizerAddedActual := crpSchedulerFinalizerAddedActual(crpName)
		Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add scheduler cleanup finalizer to CRP")
	})

	It("should create scheduled bindings for all healthy clusters", func() {
		scheduledBindingsCreatedActual := scheduledBindingsCreatedForClustersActual(healthyClusters, zeroScoreByCluster, crpName, policySnapshotName)
		Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
	})

	It("should not create any binding for unhealthy clusters", func() {
		noBindingsCreatedActual := noBindingsCreatedForClustersActual(unhealthyClusters, crpName)
		Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
		Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Some bindings have been created unexpectedly")
	})

	It("should report status correctly", func() {
		statusUpdatedActual := pickAllPolicySnapshotStatusUpdatedActual(healthyClusters, unhealthyClusters, policySnapshotName)
		Eventually(statusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update status")
		Consistently(statusUpdatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to update status")
	})

	It("can add a new healthy cluster", func() {
		// Create a new member cluster.
		memberCluster := clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: newUnhealthyMemberClusterName,
			},
			Spec: clusterv1beta1.MemberClusterSpec{
				Identity: rbacv1.Subject{
					Kind:     "ServiceAccount",
					APIGroup: "",
					Name:     "admin",
				},
			},
		}
		Expect(hubClient.Create(ctx, &memberCluster)).To(Succeed(), "Failed to create member cluster")

		// Mark this cluster as healthy.
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: newUnhealthyMemberClusterName}, &memberCluster)).To(Succeed(), "Failed to get member cluster")
		memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
			{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.AgentJoined),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now()),
						Reason:             dummyReason,
					},
					{
						Type:               string(clusterv1beta1.AgentHealthy),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now()),
						Reason:             dummyReason,
					},
				},
				LastReceivedHeartbeat: metav1.NewTime(time.Now()),
			},
		}
		Expect(hubClient.Status().Update(ctx, &memberCluster)).To(Succeed(), "Failed to update member cluster status")
	})

	It("should create scheduled bindings for the newly recovered cluster", func() {
		scheduledBindingsCreatedActual := scheduledBindingsCreatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpName, policySnapshotName)
		Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
	})

	It("can mark a healthy cluster as unhealthy", func() {
		memberCluster := &clusterv1beta1.MemberCluster{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: newUnhealthyMemberClusterName}, memberCluster)).To(Succeed(), "Failed to get member cluster")
		memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
			{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.AgentJoined),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now()),
						Reason:             dummyReason,
					},
					{
						Type:               string(clusterv1beta1.AgentHealthy),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now()),
						Reason:             dummyReason,
					},
				},
				LastReceivedHeartbeat: metav1.NewTime(time.Now()),
			},
		}
		Expect(hubClient.Status().Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")
	})

	It("should not remove binding for the cluster that just becomes unhealthy", func() {
		scheduledBindingsCreatedActual := scheduledBindingsCreatedForClustersActual(updatedHealthyClusters, updatedZeroScoreByCluster, crpName, policySnapshotName)
		Eventually(scheduledBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
		Consistently(scheduledBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to create the expected set of bindings")
	})

	It("can delete the CRP", func() {
		// Delete the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
		}
		Expect(hubClient.Delete(ctx, crp)).To(Succeed(), "Failed to delete CRP")
	})

	It("should clear all bindings", func() {
		noBindingsCreatedActual := noBindingsCreatedForCRPActual(crpName)
		Eventually(noBindingsCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to clear all bindings")
		Consistently(noBindingsCreatedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to clear all bindings")
	})

	It("should remove the scheduler cleanup finalizer from the CRP", func() {
		finalizerRemovedActual := crpSchedulerFinalizerRemovedActual(crpName)
		Eventually(finalizerRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove scheduler cleanup finalizer from CRP")
	})

	AfterAll(func() {
		// Delete the CRP.
		ensureCRPDeletion(crpName)

		// Remove all policy snapshots.
		clearPolicySnapshots(crpName)

		// Delete the provisional cluster.
		ensureProvisionalClusterDeletion(newUnhealthyMemberClusterName)
	})
})
