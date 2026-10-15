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

package placementpolicy

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

var _ = Describe("cluster claim lifecycle", Ordered, func() {
	var counter int

	nextName := func(prefix string) string {
		counter++
		return fmt.Sprintf("%s-claim-%d", prefix, counter)
	}

	AfterEach(func() {
		policies := &kfplacementv1alpha1.PlacementPolicyList{}
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).Should(Succeed())
		for i := range policies.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &policies.Items[i]))).Should(Succeed())
		}
		// Policy deletion is finalizer-gated on claim cleanup; wait for both to drain.
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).Should(Succeed())
			g.Expect(policies.Items).Should(BeEmpty())
			claims := &kfplacementv1alpha1.ClusterClaimList{}
			g.Expect(k8sClient.List(ctx, claims)).Should(Succeed())
			g.Expect(claims.Items).Should(BeEmpty())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		memberClusters := &clusterv1beta1.MemberClusterList{}
		Expect(k8sClient.List(ctx, memberClusters)).Should(Succeed())
		for i := range memberClusters.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &memberClusters.Items[i]))).Should(Succeed())
		}
		Eventually(func() (int, error) {
			if err := k8sClient.List(ctx, memberClusters); err != nil {
				return -1, err
			}
			return len(memberClusters.Items), nil
		}, eventuallyTimeout, pollInterval).Should(Equal(0))
	})

	It("adds a claim for an unfulfilled selector and cleans it up with the policy", func() {
		policy := newPolicy(nextName("pp"), regionSelector("brazilsouth", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		By("the claim appears with the policy reference, terms, and ownership labels")
		Eventually(func(g Gomega) {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)).Should(Succeed())
			g.Expect(claim.Labels).Should(HaveKeyWithValue(kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel, policy.Name))
			g.Expect(claim.Labels).Should(HaveKeyWithValue(kfplacementv1alpha1.ClusterClaimPlacementPolicyNamespaceLabel, testNamespace))
			g.Expect(claim.Spec.PlacementPolicyRef).NotTo(BeNil())
			g.Expect(claim.Spec.PlacementPolicyRef.Name).Should(Equal(policy.Name))
			g.Expect(claim.Spec.PlacementPolicyRef.Kind).Should(Equal(kfplacementv1alpha1.PlacementPolicyKind))
			g.Expect(apiequality.Semantic.DeepEqual(claim.Spec.ClusterSelectorTerms, policy.Spec.ClusterSelectors[0].Terms)).Should(BeTrue())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the policy carries the cleanup finalizer and counts the claim")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			g.Expect(controllerutil.ContainsFinalizer(fetched, claimCleanupFinalizer)).Should(BeTrue())
			g.Expect(fetched.Status.ActiveClusterClaims).NotTo(BeNil())
			g.Expect(*fetched.Status.ActiveClusterClaims).Should(Equal(int32(1)))
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("deleting the policy withdraws the claim and releases the policy")
		Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
			g.Expect(client.IgnoreNotFound(err)).Should(Succeed())
			g.Expect(err).Should(HaveOccurred(), "claim should be withdrawn")
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), &kfplacementv1alpha1.PlacementPolicy{})
			g.Expect(client.IgnoreNotFound(err)).Should(Succeed())
			g.Expect(err).Should(HaveOccurred(), "policy should be gone once the finalizer is released")
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("withdraws the claim only when an eligible cluster fulfills the selector", func() {
		policy := newPolicy(nextName("pp"), regionSelector("koreacentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a matching but not-yet-joined cluster does not withdraw the claim (join window)")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "koreacentral"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		Consistently(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
		}, consistentlyDuration, pollInterval).Should(Succeed())

		By("once the cluster joins, the claim is withdrawn and the count drops")
		markJoined(mc)
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
			g.Expect(client.IgnoreNotFound(err)).Should(Succeed())
			g.Expect(err).Should(HaveOccurred())
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			g.Expect(fetched.Status.ActiveClusterClaims).NotTo(BeNil())
			g.Expect(*fetched.Status.ActiveClusterClaims).Should(Equal(int32(0)))
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("still withdraws a claim whose ownership labels were stripped, tracked by the immutable reference", func() {
		policy := newPolicy(nextName("pp"), regionSelector("chilecentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a provisioner strips the ownership labels, leaving only spec.placementPolicyRef")
		Eventually(func(g Gomega) {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)).Should(Succeed())
			claim.Labels = nil
			g.Expect(k8sClient.Update(ctx, claim)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("fulfilling the selector still withdraws the claim (a label-selected list would have lost it)")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "chilecentral"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
			g.Expect(client.IgnoreNotFound(err)).Should(Succeed())
			g.Expect(err).Should(HaveOccurred(), "the label-stripped claim should still be withdrawn")
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("restores the ownership labels on a still-wanted claim after they are stripped", func() {
		policy := newPolicy(nextName("pp"), regionSelector("canadacentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a provisioner strips the ownership labels while the selector is still unfulfilled")
		Eventually(func(g Gomega) {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)).Should(Succeed())
			claim.Labels = nil
			g.Expect(k8sClient.Update(ctx, claim)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the controller re-asserts the labels so label-watching consumers find the claim again")
		Eventually(func(g Gomega) {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)).Should(Succeed())
			g.Expect(claim.Labels).Should(HaveKeyWithValue(kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel, policy.Name))
			g.Expect(claim.Labels).Should(HaveKeyWithValue(kfplacementv1alpha1.ClusterClaimPlacementPolicyNamespaceLabel, testNamespace))
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("releases the cleanup finalizer once the policy's last claim is withdrawn", func() {
		policy := newPolicy(nextName("pp"), regionSelector("mexicocentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		By("the policy carries the cleanup finalizer while its claim is outstanding")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			g.Expect(controllerutil.ContainsFinalizer(fetched, claimCleanupFinalizer)).Should(BeTrue())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("fulfilling the selector withdraws the claim and releases the finalizer")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "mexicocentral"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			g.Expect(controllerutil.ContainsFinalizer(fetched, claimCleanupFinalizer)).Should(BeFalse(), "a claim-free policy should not retain the cleanup finalizer")
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the claim-free policy deletes without waiting on a finalizer")
		Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), &kfplacementv1alpha1.PlacementPolicy{})
			g.Expect(client.IgnoreNotFound(err)).Should(Succeed())
			g.Expect(err).Should(HaveOccurred())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("issues one claim at a time until a count>1 selector is fully provisioned", func() {
		policy := newPolicy(nextName("pp"), regionSelector("australiaeast", ptr.To(intstr.FromInt32(3)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())
		claimN := claimName(policy, 0)

		// waitFreshUID returns the UID of the claim at the selector's name once it exists and has not
		// been marked completed -- a claim the provisioner has not acted on yet.
		waitFreshUID := func() types.UID {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claimN}, claim)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(claim.Status.Conditions, kfplacementv1alpha1.ClusterClaimCondTypeCompleted)).Should(BeFalse())
			}, eventuallyTimeout, pollInterval).Should(Succeed())
			return claim.UID
		}
		// provision stands in for a provisioner acting on exactly one claim: it joins one cluster and
		// marks that specific claim (identified by UID) completed for it, exactly once. Guarding on the
		// UID keeps a conflict retry from marking the next, already-rotated claim.
		provision := func(clusterName string, claimUID types.UID) {
			mc := newMemberCluster(clusterName, map[string]string{testRegionLabel: "australiaeast"})
			Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
			markJoined(mc)
			Eventually(func(g Gomega) {
				claim := &kfplacementv1alpha1.ClusterClaim{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: claimN}, claim)
				if apierrors.IsNotFound(err) || (err == nil && claim.UID != claimUID) {
					return // the targeted claim is gone or already rotated: our completion took effect
				}
				g.Expect(err).ShouldNot(HaveOccurred()) // any other read error fails loudly rather than passing as a skip
				claim.Status.ProvisionedClusterName = ptr.To(clusterName)
				meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
					Type: kfplacementv1alpha1.ClusterClaimCondTypeCompleted, Status: metav1.ConditionTrue, Reason: "Fulfilled",
				})
				g.Expect(k8sClient.Status().Update(ctx, claim)).Should(Succeed())
			}, eventuallyTimeout, pollInterval).Should(Succeed())
		}
		// expectRotation waits for the completed claim (previous UID) to be withdrawn and a fresh one
		// issued for the next cluster, and returns the fresh claim's UID.
		expectRotation := func(previousUID types.UID) types.UID {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claimN}, claim)).Should(Succeed())
				g.Expect(claim.UID).ShouldNot(Equal(previousUID), "the completed claim should be rotated to a fresh one")
				g.Expect(meta.IsStatusConditionTrue(claim.Status.Conditions, kfplacementv1alpha1.ClusterClaimCondTypeCompleted)).Should(BeFalse())
			}, eventuallyTimeout, pollInterval).Should(Succeed())
			return claim.UID
		}

		By("clusters 1 and 2 of 3: each completed claim is rotated to a fresh one for the next cluster")
		uid := waitFreshUID()
		provision(nextName("mc-aue"), uid)
		uid = expectRotation(uid)
		provision(nextName("mc-aue"), uid)
		uid = expectRotation(uid)

		By("cluster 3 of 3: the selector is satisfied, so the claim is withdrawn and not reissued")
		provision(nextName("mc-aue"), uid)
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: claimN}, &kfplacementv1alpha1.ClusterClaim{})
			g.Expect(client.IgnoreNotFound(err)).Should(Succeed())
			g.Expect(err).Should(HaveOccurred(), "the claim should be withdrawn once three clusters are provisioned")
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			g.Expect(fetched.Status.ActiveClusterClaims).NotTo(BeNil())
			g.Expect(*fetched.Status.ActiveClusterClaims).Should(Equal(int32(0)))
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("keeps at most one claim outstanding across multiple unfulfilled selectors", func() {
		policy := newPolicy(nextName("pp"),
			regionSelector("swedencentral", ptr.To(intstr.FromInt32(1)), nil),
			regionSelector("norwayeast", ptr.To(intstr.FromInt32(1)), nil),
		)
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		firstClaim := claimName(policy, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: firstClaim}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		Consistently(func(g Gomega) {
			claims := &kfplacementv1alpha1.ClusterClaimList{}
			g.Expect(k8sClient.List(ctx, claims, client.MatchingLabels{kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel: policy.Name})).Should(Succeed())
			g.Expect(claims.Items).Should(HaveLen(1))
			g.Expect(claims.Items[0].Name).Should(Equal(firstClaim))
		}, consistentlyDuration, pollInterval).Should(Succeed())
	})

	It("does not add claims for KeepSearching selectors", func() {
		selector := regionSelector("qatarcentral", ptr.To(intstr.FromInt32(1)), nil)
		selector.WhenUnfulfilled = kfplacementv1alpha1.WhenUnfulfilledOptionKeepSearching
		policy := newPolicy(nextName("pp"), selector)
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		By("the policy reports unfulfilled scheduling yet no claim appears")
		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 1, 0),
			eventuallyTimeout, pollInterval).Should(Succeed())
		Consistently(func(g Gomega) {
			claims := &kfplacementv1alpha1.ClusterClaimList{}
			g.Expect(k8sClient.List(ctx, claims, client.MatchingLabels{kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel: policy.Name})).Should(Succeed())
			g.Expect(claims.Items).Should(BeEmpty())
		}, consistentlyDuration, pollInterval).Should(Succeed())
	})

	It("refreshes the claim freshness marker when a non-matching cluster joins", func() {
		policy := newPolicy(nextName("pp"), regionSelector("polandcentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a cluster joins that does not satisfy the selector")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "italynorth"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)

		By("the claim survives and its freshness marker is advanced")
		Eventually(func(g Gomega) {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)).Should(Succeed())
			g.Expect(claim.Status.LastObservedMostRecentClusterCreationTimestamp).NotTo(BeNil())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("leaves a Terminating claim alone when its selector becomes unfulfilled again", func() {
		policy := newPolicy(nextName("pp"), regionSelector("uaenorth", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		claim := &kfplacementv1alpha1.ClusterClaim{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a provisioner holds the claim with a finalizer")
		claim.Finalizers = append(claim.Finalizers, "test.kubefleet.dev/provisioner")
		Expect(k8sClient.Update(ctx, claim)).Should(Succeed())

		By("a matching cluster joins and fulfills the selector, so the claim is withdrawn")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "uaenorth"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeFalse(), "claim should be Terminating")
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the cluster leaves, so the identical selector is unfulfilled again while the claim is still Terminating")
		Expect(k8sClient.Delete(ctx, mc)).Should(Succeed())

		By("the Terminating claim keeps its slot and no replacement is issued")
		Consistently(func(g Gomega) {
			claims := &kfplacementv1alpha1.ClusterClaimList{}
			g.Expect(k8sClient.List(ctx, claims, client.MatchingLabels{kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel: policy.Name})).Should(Succeed())
			g.Expect(claims.Items).Should(HaveLen(1))
			g.Expect(claims.Items[0].DeletionTimestamp.IsZero()).Should(BeFalse())
		}, consistentlyDuration, pollInterval).Should(Succeed())

		By("releasing the finalizer lets a fresh claim be issued for the still-unfulfilled selector")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			fetched.Finalizers = nil
			g.Expect(k8sClient.Update(ctx, fetched)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeTrue(), "a live replacement claim should exist")
		}, eventuallyTimeout*2, pollInterval).Should(Succeed())
	})

	It("waits out a provisioner finalizer when replacing a claim after a selector change", func() {
		policy := newPolicy(nextName("pp"), regionSelector("israelcentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		claim := &kfplacementv1alpha1.ClusterClaim{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a provisioner protects its in-flight work with a finalizer")
		claim.Finalizers = append(claim.Finalizers, "test.kubefleet.dev/provisioner")
		Expect(k8sClient.Update(ctx, claim)).Should(Succeed())

		By("the selector is retargeted; the old claim lingers Terminating and blocks the slot")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			fetched.Spec.ClusterSelectors[0].Terms = []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{testRegionLabel: "newzealandnorth"}},
			}
			g.Expect(k8sClient.Update(ctx, fetched)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeFalse(), "old claim should be Terminating")
			g.Expect(apiequality.Semantic.DeepEqual(fetched.Spec.ClusterSelectorTerms, claim.Spec.ClusterSelectorTerms)).Should(BeTrue(), "the Terminating claim still carries the old terms")
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("releasing the finalizer lets the replacement claim through with the new terms")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			fetched.Finalizers = nil
			g.Expect(k8sClient.Update(ctx, fetched)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeTrue(), "replacement claim should be live")
			g.Expect(fetched.Spec.ClusterSelectorTerms).Should(HaveLen(1))
			g.Expect(fetched.Spec.ClusterSelectorTerms[0].MatchLabels).Should(HaveKeyWithValue(testRegionLabel, "newzealandnorth"))
			policy := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fetched.Labels[kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel], Namespace: testNamespace}, policy)).Should(Succeed())
			g.Expect(policy.Status.ActiveClusterClaims).NotTo(BeNil())
			g.Expect(*policy.Status.ActiveClusterClaims).Should(Equal(int32(1)))
		}, eventuallyTimeout*2, pollInterval).Should(Succeed())
	})

	It("holds the budget while a withdrawn claim of another selector lingers Terminating", func() {
		// Two unfulfilled selectors; the budget admits only selector 0's claim. When selector 0
		// is fulfilled, its claim is withdrawn -- but a provisioner finalizer keeps it
		// Terminating, and selector 1's claim, which has a different name, must wait for that
		// slot rather than stand beside it.
		policy := newPolicy(nextName("pp"),
			regionSelector("swedencentral", ptr.To(intstr.FromInt32(1)), nil),
			regionSelector("italynorth", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		claim0Name := claimName(policy, 0)
		claim1Name := claimName(policy, 1)
		claim0 := &kfplacementv1alpha1.ClusterClaim{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: claim0Name}, claim0)
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("a provisioner protects the first claim with a finalizer")
		claim0.Finalizers = append(claim0.Finalizers, "test.kubefleet.dev/provisioner")
		Expect(k8sClient.Update(ctx, claim0)).Should(Succeed())

		By("fulfilling selector 0 withdraws its claim into Terminating")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "swedencentral"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim0Name}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeFalse(), "the fulfilled selector's claim should be Terminating")
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the second selector's claim waits for the slot rather than exceeding the budget")
		Consistently(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: claim1Name}, &kfplacementv1alpha1.ClusterClaim{})
		}, consistentlyDuration, pollInterval).ShouldNot(Succeed(), "the budget must hold while the withdrawn claim lingers")

		By("releasing the finalizer frees the slot for the second selector's claim")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim0Name}, fetched)).Should(Succeed())
			fetched.Finalizers = nil
			g.Expect(k8sClient.Update(ctx, fetched)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: claim1Name}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout*2, pollInterval).Should(Succeed())
	})

	It("does not release the cleanup finalizer over a claim whose labels were stripped", func() {
		// The ownership labels are mutable; the claim's back-reference is not. Cleanup must go
		// by the reference, or a label-stripped claim would vanish from the cleanup list and be
		// orphaned the moment the policy's finalizer is released.
		policy := newPolicy(nextName("pp"), regionSelector("qatarcentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		claim := &kfplacementv1alpha1.ClusterClaim{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the claim's ownership labels are stripped and a provisioner finalizer holds it")
		claim.Labels = nil
		claim.Finalizers = append(claim.Finalizers, "test.kubefleet.dev/provisioner")
		Expect(k8sClient.Update(ctx, claim)).Should(Succeed())

		By("deleting the policy withdraws the claim by its reference and keeps the finalizer")
		Expect(k8sClient.Delete(ctx, policy)).Should(Succeed())
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeFalse(), "cleanup must find the label-stripped claim through its reference")
		}, eventuallyTimeout, pollInterval).Should(Succeed())
		Consistently(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), &kfplacementv1alpha1.PlacementPolicy{})
		}, consistentlyDuration, pollInterval).Should(Succeed(), "the policy must outlive its lingering claim")

		By("once the claim is released, the policy goes too")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			fetched.Finalizers = nil
			g.Expect(k8sClient.Update(ctx, fetched)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), &kfplacementv1alpha1.PlacementPolicy{})
		}, eventuallyTimeout*2, pollInterval).ShouldNot(Succeed(), "the policy should be gone once its claim is")
	})

	It("leaves a claim of another policy alone when it holds the claim's name", func() {
		policy := newPolicy(nextName("pp"), regionSelector("norwayeast", ptr.To(intstr.FromInt32(1)), nil))
		wantClaimName := claimName(policy, 0)

		By("a claim of another policy already holds the deterministic name")
		squatterRef := policyReference(newPolicy("squatter"))
		squatter := &kfplacementv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{Name: wantClaimName},
			Spec:       kfplacementv1alpha1.ClusterClaimSpec{PlacementPolicyRef: squatterRef},
		}
		Expect(k8sClient.Create(ctx, squatter)).Should(Succeed())
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		By("the policy neither adopts, rewrites, nor withdraws it, and counts no claim")
		Consistently(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(fetched.DeletionTimestamp.IsZero()).Should(BeTrue(), "the other policy's claim must not be withdrawn")
			g.Expect(fetched.Spec.PlacementPolicyRef).Should(Equal(squatterRef))
			g.Expect(fetched.Labels).Should(BeEmpty(), "the other policy's claim must not be relabeled")
			pp := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), pp)).Should(Succeed())
			g.Expect(pp.Status.ActiveClusterClaims).Should(BeNil(), "a round that could not issue its claim publishes no count")
		}, consistentlyDuration, pollInterval).Should(Succeed())

		By("once the name frees up, the policy issues its own claim")
		Expect(k8sClient.Delete(ctx, squatter)).Should(Succeed())
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, fetched)).Should(Succeed())
			g.Expect(claimBelongsTo(fetched, policyReference(policy))).Should(BeTrue())
		}, eventuallyTimeout*2, pollInterval).Should(Succeed())
	})

	It("replaces the outstanding claim when the selector terms change", func() {
		policy := newPolicy(nextName("pp"), regionSelector("spaincentral", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		wantClaimName := claimName(policy, 0)
		oldTerms := policy.Spec.ClusterSelectors[0].Terms
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, &kfplacementv1alpha1.ClusterClaim{})
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the selector is retargeted to another region")
		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			fetched.Spec.ClusterSelectors[0].Terms = []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
				{MatchLabels: map[string]string{testRegionLabel: "mexicocentral"}},
			}
			g.Expect(k8sClient.Update(ctx, fetched)).Should(Succeed())
		}, eventuallyTimeout, pollInterval).Should(Succeed())

		By("the claim is re-issued with the new terms under the same deterministic name")
		Eventually(func(g Gomega) {
			claim := &kfplacementv1alpha1.ClusterClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wantClaimName}, claim)).Should(Succeed())
			g.Expect(claim.Spec.ClusterSelectorTerms).Should(HaveLen(1))
			g.Expect(claim.Spec.ClusterSelectorTerms[0].MatchLabels).Should(HaveKeyWithValue(testRegionLabel, "mexicocentral"))
			g.Expect(apiequality.Semantic.DeepEqual(claim.Spec.ClusterSelectorTerms, oldTerms)).Should(BeFalse())
		}, eventuallyTimeout*2, pollInterval).Should(Succeed())
	})
})
