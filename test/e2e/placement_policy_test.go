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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// The referenced ConfigMap is the one the shared work resources already provide.
//
// The namespace is set for the cluster-scoped API, which selects namespaced resources by
// qualifying them; the namespaced API ignores the field beyond requiring it to match its own
// namespace.
func placementPolicyResourceSelectors() []kfplacementv1alpha1.ResourceSelector {
	return []kfplacementv1alpha1.ResourceSelector{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       fmt.Sprintf(appConfigMapNameTemplate, GinkgoParallelProcess()),
			Namespace:  appNamespace().Name,
		},
	}
}

func placementPolicyRegionSelector(region string, count int32) kfplacementv1alpha1.ClusterSelector {
	return kfplacementv1alpha1.ClusterSelector{
		Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{regionLabelName: region}},
		},
		Count: ptr.To(intstr.FromInt32(count)),
	}
}

// claimsOfPolicy lists the cluster claims added for a policy through the ownership labels; the
// namespace label is empty for claims of a cluster-scoped policy.
func claimsOfPolicy(name, namespace string) (*kfplacementv1alpha1.ClusterClaimList, error) {
	claims := &kfplacementv1alpha1.ClusterClaimList{}
	err := hubClient.List(ctx, claims, client.MatchingLabels{
		kfplacementv1alpha1.ClusterClaimPlacementPolicyNameLabel:      name,
		kfplacementv1alpha1.ClusterClaimPlacementPolicyNamespaceLabel: namespace,
	})
	return claims, err
}

// The FEP-0001 placement experience is alpha and runs behind the enable-placement-policy-apis
// hub agent flag, which test/e2e/setup.sh turns on.
var _ = Describe("placement policy scheduling and cluster claims", Ordered, Serial, func() {
	policyName := fmt.Sprintf("pp-e2e-%d", GinkgoParallelProcess())
	var policyNS string

	BeforeAll(func() {
		By("creating the work resources")
		createWorkResources()
		policyNS = appNamespace().Name
	})

	AfterAll(func() {
		cleanupWorkResources()
	})

	scheduledConditionActual := func(wantStatus metav1.ConditionStatus, wantReason string, wantScheduled int32) func() error {
		return func() error {
			policy := &kfplacementv1alpha1.PlacementPolicy{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: policyName, Namespace: policyNS}, policy); err != nil {
				return err
			}
			cond := meta.FindStatusCondition(policy.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled)
			if cond == nil {
				return fmt.Errorf("the Scheduled condition is not set yet")
			}
			if cond.Status != wantStatus || cond.Reason != wantReason {
				return fmt.Errorf("Scheduled condition = %s/%s, want %s/%s", cond.Status, cond.Reason, wantStatus, wantReason)
			}
			if policy.Status.ScheduledClusters == nil || *policy.Status.ScheduledClusters != wantScheduled {
				return fmt.Errorf("scheduledClusters = %v, want %d", policy.Status.ScheduledClusters, wantScheduled)
			}
			return nil
		}
	}

	newPolicy := func(selectors ...kfplacementv1alpha1.ClusterSelector) *kfplacementv1alpha1.PlacementPolicy {
		return &kfplacementv1alpha1.PlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: policyNS},
			Spec: kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors:  selectors,
				ResourceSelectors: placementPolicyResourceSelectors(),
			},
		}
	}

	AfterEach(func() {
		policy := &kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: policyNS}}
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, policy))).Should(Succeed(), "Failed to delete the placement policy")
		// Policy deletion is finalizer-gated on cluster claim withdrawal, so waiting for the
		// policy to disappear also confirms its claims are gone.
		Eventually(func() error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: policyName, Namespace: policyNS}, &kfplacementv1alpha1.PlacementPolicy{})
			if err == nil {
				return fmt.Errorf("the placement policy still exists")
			}
			return client.IgnoreNotFound(err)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to clean up the placement policy")
	})

	It("should report fulfilled scheduling for a selector the fleet satisfies", func() {
		// Two of the three E2E member clusters carry region=east.
		Expect(hubClient.Create(ctx, newPolicy(placementPolicyRegionSelector(regionEast, 2)))).Should(Succeed(), "Failed to create the placement policy")

		Eventually(scheduledConditionActual(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2),
			eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report fulfilled scheduling")

		By("checking that each selected cluster gets a binding naming the resource snapshot")
		Eventually(func() error {
			bindings := &kfplacementv1alpha1.PlacementBindingList{}
			if err := hubClient.List(ctx, bindings, client.InNamespace(policyNS)); err != nil {
				return err
			}
			got := 0
			for _, b := range bindings.Items {
				if b.Spec.PlacementPolicyName != policyName {
					continue
				}
				if b.Spec.ResourceSnapshotName == "" {
					return fmt.Errorf("binding %s names no resource snapshot", b.Name)
				}
				got++
			}
			if got != 2 {
				return fmt.Errorf("got %d bindings, want 2", got)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the selected clusters")
	})

	It("should add a cluster claim for an unfulfillable selector and withdraw it with the policy", func() {
		Expect(hubClient.Create(ctx, newPolicy(placementPolicyRegionSelector("nowhere", 1)))).Should(Succeed(), "Failed to create the placement policy")

		By("checking that the policy reports unfulfilled scheduling")
		Eventually(scheduledConditionActual(metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 0),
			eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report unfulfilled scheduling")

		By("checking that a cluster claim carrying the selector terms is added")
		var claimName string
		Eventually(func() error {
			claims, err := claimsOfPolicy(policyName, policyNS)
			if err != nil {
				return err
			}
			if len(claims.Items) != 1 {
				return fmt.Errorf("got %d cluster claims, want 1", len(claims.Items))
			}
			claim := claims.Items[0]
			if claim.Spec.PlacementPolicyRef == nil ||
				claim.Spec.PlacementPolicyRef.Name != policyName ||
				claim.Spec.PlacementPolicyRef.Kind != kfplacementv1alpha1.PlacementPolicyKind {
				return fmt.Errorf("the cluster claim does not reference the policy")
			}
			if len(claim.Spec.ClusterSelectorTerms) != 1 || claim.Spec.ClusterSelectorTerms[0].MatchLabels[regionLabelName] != "nowhere" {
				return fmt.Errorf("the cluster claim does not carry the selector terms")
			}
			claimName = claim.Name
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cluster claim")

		By("checking that deleting the policy withdraws the claim")
		Expect(hubClient.Delete(ctx, &kfplacementv1alpha1.PlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: policyNS},
		})).Should(Succeed(), "Failed to delete the placement policy")
		Eventually(func() error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: claimName}, &kfplacementv1alpha1.ClusterClaim{})
			if err == nil {
				return fmt.Errorf("the cluster claim still exists")
			}
			return client.IgnoreNotFound(err)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to withdraw the cluster claim")
	})
})

// The cluster-scoped policy has its own watch wiring and claim reference path, so it gets its
// own end-to-end coverage.
var _ = Describe("cluster placement policy scheduling and cluster claims", Ordered, Serial, func() {
	policyName := fmt.Sprintf("cpp-e2e-%d", GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating the work resources")
		createWorkResources()
	})

	AfterAll(func() {
		cleanupWorkResources()
	})

	newClusterPolicy := func(selectors ...kfplacementv1alpha1.ClusterSelector) *kfplacementv1alpha1.ClusterPlacementPolicy {
		return &kfplacementv1alpha1.ClusterPlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: policyName},
			Spec: kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors:  selectors,
				ResourceSelectors: placementPolicyResourceSelectors(),
			},
		}
	}

	AfterEach(func() {
		policy := &kfplacementv1alpha1.ClusterPlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName}}
		Expect(client.IgnoreNotFound(hubClient.Delete(ctx, policy))).Should(Succeed(), "Failed to delete the cluster placement policy")
		Eventually(func() error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: policyName}, &kfplacementv1alpha1.ClusterPlacementPolicy{})
			if err == nil {
				return fmt.Errorf("the cluster placement policy still exists")
			}
			return client.IgnoreNotFound(err)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to clean up the cluster placement policy")
	})

	It("should schedule against the fleet and claim for an unfulfillable selector", func() {
		Expect(hubClient.Create(ctx, newClusterPolicy(
			placementPolicyRegionSelector(regionWest, 1),
			placementPolicyRegionSelector("nowhere", 1),
		))).Should(Succeed(), "Failed to create the cluster placement policy")

		By("checking that the fulfillable selector is scheduled while the policy stays unfulfilled overall")
		Eventually(func() error {
			policy := &kfplacementv1alpha1.ClusterPlacementPolicy{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: policyName}, policy); err != nil {
				return err
			}
			cond := meta.FindStatusCondition(policy.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled)
			if cond == nil {
				return fmt.Errorf("the Scheduled condition is not set yet")
			}
			if cond.Status != metav1.ConditionFalse || cond.Reason != kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters {
				return fmt.Errorf("Scheduled condition = %s/%s, want False/%s", cond.Status, cond.Reason, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters)
			}
			// One cluster carries region=west; the second selector matches nothing.
			if policy.Status.ScheduledClusters == nil || *policy.Status.ScheduledClusters != 1 {
				return fmt.Errorf("scheduledClusters = %v, want 1", policy.Status.ScheduledClusters)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to report the expected scheduling status")

		By("checking that a cluster claim referencing the cluster-scoped policy is added")
		Eventually(func() error {
			claims, err := claimsOfPolicy(policyName, "")
			if err != nil {
				return err
			}
			if len(claims.Items) != 1 {
				return fmt.Errorf("got %d cluster claims, want 1", len(claims.Items))
			}
			ref := claims.Items[0].Spec.PlacementPolicyRef
			if ref == nil || ref.Kind != kfplacementv1alpha1.ClusterPlacementPolicyKind || ref.Namespace != "" {
				return fmt.Errorf("the cluster claim does not reference the cluster placement policy")
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add the cluster claim")

		By("checking that the west cluster gets a cluster binding naming the resource snapshot")
		Eventually(func() error {
			bindings := &kfplacementv1alpha1.ClusterPlacementBindingList{}
			if err := hubClient.List(ctx, bindings); err != nil {
				return err
			}
			var owned []kfplacementv1alpha1.ClusterPlacementBinding
			for _, b := range bindings.Items {
				if b.Spec.PlacementPolicyName == policyName {
					owned = append(owned, b)
				}
			}
			if len(owned) != 1 {
				return fmt.Errorf("got %d cluster bindings, want 1", len(owned))
			}
			if owned[0].Spec.ClusterName != memberCluster3WestProdName {
				return fmt.Errorf("cluster binding %s targets %s, want %s", owned[0].Name, owned[0].Spec.ClusterName, memberCluster3WestProdName)
			}
			if owned[0].Spec.ResourceSnapshotName == "" {
				return fmt.Errorf("cluster binding %s names no resource snapshot", owned[0].Name)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to bind the west cluster")
	})
})
