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

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

const (
	clusterPlacementPolicyNeverJoinedNameTemplate = "cpp-never-joined-%d"
	neverJoinedMemberClusterNameTemplate          = "pp-never-joined-cluster-%d"

	clusterPlacementPolicyAgentStoppedNameTemplate = "cpp-agent-stopped-%d"

	// memberAgentDeploymentName is the Deployment that runs the member agent on a member cluster, in the fleet-system
	// namespace.
	memberAgentDeploymentName = "member-agent"
	// memberAgentStoppedEventuallyDuration bounds the wait for the placement policy controller to treat a cluster
	// whose member agent stopped as no longer eligible: the eligibility checker's heartbeat timeout is 5m, and the
	// controller re-evaluates a fulfilled policy at least every 150s, with a margin on top.
	memberAgentStoppedEventuallyDuration = 9 * time.Minute

	// neverJoinedLabelName labels the member cluster that the specs register without ever letting it join; no member
	// cluster of the fleet carries it.
	neverJoinedLabelName = "fleet-e2e/never-joined"
)

// setMemberAgentReplicas scales the member agent Deployment on the member cluster, retrying on conflicts.
func setMemberAgentReplicas(cluster *framework.Cluster, replicas int32) {
	GinkgoHelper()

	Eventually(func() error {
		deploy := &appsv1.Deployment{}
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: fleetSystemNS, Name: memberAgentDeploymentName}, deploy); err != nil {
			return err
		}
		deploy.Spec.Replicas = ptr.To(replicas)
		return cluster.KubeClient.Update(ctx, deploy)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to scale the member agent on cluster %s to %d replicas", cluster.ClusterName, replicas)
}

// stopMemberAgent scales the member agent on the member cluster down, and waits until none of its pods is left, so
// that the cluster sends no more heartbeats to the hub.
func stopMemberAgent(cluster *framework.Cluster) {
	GinkgoHelper()

	setMemberAgentReplicas(cluster, 0)
	Eventually(func() error {
		deploy := &appsv1.Deployment{}
		if err := cluster.KubeClient.Get(ctx, types.NamespacedName{Namespace: fleetSystemNS, Name: memberAgentDeploymentName}, deploy); err != nil {
			return err
		}
		selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
		if err != nil {
			return err
		}
		pods := &corev1.PodList{}
		if err := cluster.KubeClient.List(ctx, pods, client.InNamespace(fleetSystemNS), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return fmt.Errorf("cluster %s still runs %d member agent pods", cluster.ClusterName, len(pods.Items))
		}
		return nil
	}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to stop the member agent on cluster %s", cluster.ClusterName)
}

// startMemberAgent scales the member agent on the member cluster back up, and waits until the cluster has sent a
// heartbeat since and reports itself as joined and healthy again.
func startMemberAgent(cluster *framework.Cluster) {
	GinkgoHelper()

	// Heartbeat timestamps have a resolution of one second.
	startedAt := time.Now().Truncate(time.Second)
	setMemberAgentReplicas(cluster, 1)
	Eventually(func() error {
		mc := &clusterv1beta1.MemberCluster{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: cluster.ClusterName}, mc); err != nil {
			return err
		}
		agentStatus := mc.GetAgentStatus(clusterv1beta1.MemberAgent)
		if agentStatus == nil || agentStatus.LastReceivedHeartbeat.Time.Before(startedAt) {
			return fmt.Errorf("member cluster %s has received no heartbeat from its member agent since %v (agent status: %+v)", cluster.ClusterName, startedAt, agentStatus)
		}
		return nil
	}, longEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to restart the member agent on cluster %s", cluster.ClusterName)
	checkIfMemberClusterHasJoined(cluster)
}

// The specs below change the member clusters of the fleet, adding one or stopping the member agent of one, so they are
// Serial: they run only after the parallel specs finish, and undo every change they make, so that the fleet is whole
// for whatever runs next.
var _ = Describe("placement policy with member clusters joining and leaving the fleet", Label("placementpolicy"), func() {
	Context("cluster placement policy whose selector a registered member cluster matches without ever joining", Ordered, Serial, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyNeverJoinedNameTemplate, GinkgoParallelProcess()),
		}
		memberClusterName := fmt.Sprintf(neverJoinedMemberClusterNameTemplate, GinkgoParallelProcess())
		neverJoinedLabels := map[string]string{neverJoinedLabelName: "true"}
		neverJoinedTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: neverJoinedLabels},
		}

		var claimUID types.UID

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects one cluster carrying a label that no member cluster has")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(neverJoinedTerms, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources and the registered member cluster must go even if removing the policy fails; otherwise
			// they leak into later specs. The cleanup strips the member cluster's finalizers, as no agent ever reports
			// that it has left.
			defer cleanupWorkResourcesOnHubAndMembers()
			defer ensureMemberClusterAndRelatedResourcesDeletion(memberClusterName)
			// Deleting the policy also withdraws its cluster claim.
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should add a cluster claim, binding no cluster", func() {
			Eventually(placementPolicyClaimActual(policyKey, neverJoinedTerms), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to add the cluster claim")
			Eventually(placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1)), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
			Expect(noPlacementPolicyBindingsActual(policyKey)()).To(Succeed(), "A cluster was bound")

			claims, err := claimsOfPolicy(policyKey.Name, policyKey.Namespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to list the cluster claims of cluster placement policy %s", policyKey)
			Expect(claims.Items).To(HaveLen(1), "Cluster placement policy %s does not have exactly one cluster claim", policyKey)
			claimUID = claims.Items[0].UID
		})

		It("can register a member cluster that carries the label, but never let it join", func() {
			// No member agent runs for the cluster, so it never joins the fleet, and it is never marked as healthy.
			createMemberCluster(memberClusterName, hubClusterSAName, neverJoinedLabels, nil)
		})

		It("should observe the registered member cluster when reconciling the cluster claim", func() {
			// Every policy reconciles on a member cluster creation, and records the newest creation time it has seen on
			// its outstanding claims; this shows that the policy has evaluated its selector with the cluster in place.
			mc := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, mc)).To(Succeed(), "Failed to get member cluster %s", memberClusterName)
			Eventually(placementPolicyClaimObservedClusterCreationActual(policyKey, mc.CreationTimestamp), eventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to observe the registered member cluster on the cluster claim")
		})

		It("should keep the cluster claim outstanding, binding no cluster, as the registered member cluster is not eligible", func() {
			// A claim is withdrawn only once eligible clusters fulfill its selector, and a cluster that has never joined
			// is not eligible.
			claimKeptActual := placementPolicyClaimKeptActual(policyKey, claimUID)
			claimActual := placementPolicyClaimActual(policyKey, neverJoinedTerms)
			bindingsActual := noPlacementPolicyBindingsActual(policyKey)
			statusActual := placementPolicyStatusActual(policyKey, unfulfilledPlacementPolicyStatus(1, 0, 1))
			Consistently(func() error {
				for _, actual := range []func() error{claimKeptActual, claimActual, bindingsActual, statusActual} {
					if err := actual(); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The cluster claim was withdrawn or changed, or a cluster was bound")
		})
	})

	Context("cluster placement policy whose bound cluster loses its member agent for a while", Ordered, Serial, func() {
		policyKey := types.NamespacedName{
			Name: fmt.Sprintf(clusterPlacementPolicyAgentStoppedNameTemplate, GinkgoParallelProcess()),
		}
		prodTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{envLabelName: envProd}},
		}

		// Which prod cluster ranks first depends on live capacity, so the specs learn it rather than assume it.
		var stoppedClusterName, otherClusterName string
		var stoppedBindingUID types.UID
		var agentStopped bool

		BeforeAll(func() {
			By("creating the work resources")
			createWorkResources()

			By("creating a cluster placement policy that selects one prod cluster")
			policy := newPlacementPolicyObject(policyKey, kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{
					clusterSelectorWithTerms(prodTerms, intstr.FromInt32(1)),
				},
				ResourceSelectors: placementPolicyResourceSelectors(),
			})
			Expect(hubClient.Create(ctx, policy)).To(Succeed(), "Failed to create cluster placement policy %s", policyKey)
		})

		AfterAll(func() {
			// The work resources must go even if the cleanup below fails; otherwise they leak into later specs.
			defer cleanupWorkResourcesOnHubAndMembers()
			// The member agent must run again before the policy goes, so that it removes the ConfigMap it placed; the
			// specs that run next need the whole fleet as well.
			if agentStopped {
				startMemberAgent(memberClusterNamed(stoppedClusterName))
			}
			ensurePlacementPolicyRemoved(policyKey)
		})

		It("should bind one prod cluster", func() {
			binding := waitForSinglePlacementPolicyBinding(policyKey)
			stoppedClusterName, stoppedBindingUID = binding.GetSpec().ClusterName, binding.GetUID()
			otherClusterName = otherProdClusterName(stoppedClusterName)

			waitForPlacementPolicyPlacedOnlyOn(policyKey, prodTerms, stoppedClusterName)
		})

		It("can stop the member agent on the bound cluster", func() {
			agentStopped = true
			stopMemberAgent(memberClusterNamed(stoppedClusterName))
		})

		It("should keep the cluster bound, and bind the other prod cluster in its place, once the cluster's heartbeat goes stale", func() {
			// The cluster keeps its binding and its resources, so it is still a target of the resource status, and its
			// binding still reports what the member agent last reported; it no longer counts toward the selector,
			// though, so the other prod cluster fills the gap.
			Eventually(allSynchronizedPlacementPolicyBindingsActual(policyKey, sameSelectorsForClusters(prodTerms, stoppedClusterName, otherClusterName), 1),
				memberAgentStoppedEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the other prod cluster")
			Expect(placementPolicyBindingsKeptActual(policyKey, map[string]types.UID{stoppedClusterName: stoppedBindingUID})()).
				To(Succeed(), "Failed to keep the binding of the cluster whose member agent stopped")
			Eventually(appConfigMapPlacedOnClustersActual(memberClusterNamed(stoppedClusterName), memberClusterNamed(otherClusterName)),
				workloadEventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place the ConfigMap on both prod clusters")

			wantStatus := allSynchronizedPlacementPolicyStatus(2)
			wantStatus.DesiredClusters = ptr.To(int32(1))
			wantStatus.ScheduledClusters = ptr.To(int32(1))
			Eventually(placementPolicyStatusActual(policyKey, wantStatus), workloadEventuallyDuration, eventuallyInterval).
				Should(Succeed(), "Failed to report the expected cluster placement policy status")
		})

		It("can restart the member agent", func() {
			startMemberAgent(memberClusterNamed(stoppedClusterName))
			agentStopped = false
		})

		It("should unbind the other prod cluster, and keep the originally bound one", func() {
			// Back in the running, the originally bound cluster has the older binding, so the count trims the newer one.
			waitForPlacementPolicyPlacedOnlyOn(policyKey, prodTerms, stoppedClusterName)
			Expect(placementPolicyBindingsKeptActual(policyKey, map[string]types.UID{stoppedClusterName: stoppedBindingUID})()).
				To(Succeed(), "Failed to keep the binding of the originally bound cluster")
		})
	})
})
