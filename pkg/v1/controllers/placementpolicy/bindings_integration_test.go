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
	"cmp"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

// setNodeCount reports a node count on a member cluster, which is what the ranking prefers
// fewer of.
func setNodeCount(name, count string) {
	mc := &clusterv1beta1.MemberCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, mc)).Should(Succeed())
	mc.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
		propertyprovider.NodeCountProperty: {Value: count, ObservationTime: metav1.Now()},
	}
	Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed())
}

// setJoined flips the member agent's Joined condition, which takes the cluster in and out of
// the scheduler's eligibility gate through a change the member cluster watch does not filter.
func setJoined(name string, joined metav1.ConditionStatus) {
	mc := &clusterv1beta1.MemberCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, mc)).Should(Succeed())
	meta.SetStatusCondition(&mc.Status.AgentStatus[0].Conditions, metav1.Condition{
		Type: string(clusterv1beta1.AgentJoined), Status: joined, Reason: "IntegrationTest",
	})
	Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed())
}

// setRegion relabels the region of a member cluster.
func setRegion(name, region string) {
	mc := &clusterv1beta1.MemberCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, mc)).Should(Succeed())
	mc.Labels[testRegionLabel] = region
	Expect(k8sClient.Update(ctx, mc)).Should(Succeed())
}

// setSelectorCount rewrites the count of a policy's only selector.
func setSelectorCount(key types.NamespacedName, count int32) {
	policy := &kfplacementv1alpha1.PlacementPolicy{}
	Expect(k8sClient.Get(ctx, key, policy)).Should(Succeed())
	policy.Spec.ClusterSelectors[0].Count = ptr.To(intstr.FromInt32(count))
	Expect(k8sClient.Update(ctx, policy)).Should(Succeed())
}

// drainMemberClusters deletes every member cluster and waits for them to be gone, since they are
// cluster-scoped and shared with the other containers of this suite.
func drainMemberClusters() {
	memberClusters := &clusterv1beta1.MemberClusterList{}
	Expect(k8sClient.List(ctx, memberClusters)).Should(Succeed())
	for i := range memberClusters.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &memberClusters.Items[i]))).Should(Succeed())
	}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.List(ctx, memberClusters)).Should(Succeed())
		g.Expect(memberClusters.Items).Should(BeEmpty())
	}, eventuallyTimeout, pollInterval).Should(Succeed())
}

var _ = Describe("placement binding lifecycle", Ordered, func() {
	const (
		region     = "binding-east"
		policyName = "binder"
	)
	policyKey := types.NamespacedName{Namespace: testNamespace, Name: policyName}
	regionTerms := []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
		{MatchLabels: map[string]string{testRegionLabel: region}},
	}

	// bindingsOf returns the policy's bindings, sorted by cluster name.
	bindingsOf := func(g Gomega) []kfplacementv1alpha1.PlacementBinding {
		list := &kfplacementv1alpha1.PlacementBindingList{}
		g.Expect(k8sClient.List(ctx, list, client.InNamespace(testNamespace))).Should(Succeed())
		owned := make([]kfplacementv1alpha1.PlacementBinding, 0, len(list.Items))
		for _, binding := range list.Items {
			if binding.Spec.PlacementPolicyName == policyName {
				owned = append(owned, binding)
			}
		}
		slices.SortFunc(owned, func(a, b kfplacementv1alpha1.PlacementBinding) int {
			return cmp.Compare(a.Spec.ClusterName, b.Spec.ClusterName)
		})
		return owned
	}
	boundClusters := func(g Gomega) []string {
		names := []string{}
		for _, binding := range bindingsOf(g) {
			names = append(names, binding.Spec.ClusterName)
		}
		return names
	}
	wantBound := func(clusters ...string) func(Gomega) {
		want := append([]string{}, clusters...)
		return func(g Gomega) {
			g.Expect(boundClusters(g)).Should(Equal(want))
		}
	}
	wantScheduled := func(status metav1.ConditionStatus, reason string, desired, scheduled int32) {
		Eventually(wantScheduledState(policyKey, status, reason, desired, scheduled), eventuallyTimeout, pollInterval).Should(Succeed())
	}

	BeforeAll(func() {
		// The node counts rank the clusters c1, c3, c2: the names deliberately disagree with
		// the ranking, so that picking by name cannot pass.
		for name, nodes := range map[string]string{"binder-c1": "1", "binder-c2": "5", "binder-c3": "3"} {
			Expect(k8sClient.Create(ctx, newMemberCluster(name, map[string]string{testRegionLabel: region}))).Should(Succeed())
			markJoined(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: name}})
			setNodeCount(name, nodes)
		}
		Expect(k8sClient.Create(ctx, newPolicy(policyName, regionSelector(region, ptr.To(intstr.FromInt32(2)), nil)))).Should(Succeed())
	})

	AfterAll(func() {
		policy := &kfplacementv1alpha1.PlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: testNamespace}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, policy))).Should(Succeed())
		// Policy deletion is finalizer-gated on claim cleanup; wait for both to drain.
		Eventually(func(g Gomega) {
			g.Expect(errors.IsNotFound(k8sClient.Get(ctx, policyKey, &kfplacementv1alpha1.PlacementPolicy{}))).Should(BeTrue())
			claims := &kfplacementv1alpha1.ClusterClaimList{}
			g.Expect(k8sClient.List(ctx, claims)).Should(Succeed())
			g.Expect(claims.Items).Should(BeEmpty())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
		// envtest runs no garbage collector, so the owned bindings are removed by hand.
		Expect(k8sClient.DeleteAllOf(ctx, &kfplacementv1alpha1.PlacementBinding{}, client.InNamespace(testNamespace))).Should(Succeed())
		drainMemberClusters()
	})

	It("binds the best-ranked clusters up to the count, then releases the binding manager role", func() {
		Eventually(wantBound("binder-c1", "binder-c3"), eventuallyTimeout, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2, 2)
		Eventually(func(g Gomega) {
			policy := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, policyKey, policy)).Should(Succeed())
			g.Expect(policy.Status.BindingManager).Should(BeNil())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("fills in each binding from the policy and owns it", func() {
		policy := &kfplacementv1alpha1.PlacementPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).Should(Succeed())
		for _, binding := range bindingsOf(Default) {
			Expect(metav1.IsControlledBy(&binding, policy)).Should(BeTrue(), "binding %s should be controlled by the policy", binding.Name)
			Expect(binding.Spec.ResourceSnapshotName).Should(Equal(stubSnapshotName(policy)))
			Expect(binding.Spec.ClusterSelectors).Should(Equal([]kfplacementv1alpha1.ClusterSelectorWithTermsOnly{{Terms: regionTerms}}))
		}
	})

	It("keeps a bound cluster that goes dark and fills its place", func() {
		// Seniority among bindings is their creation timestamp, which the API server records to
		// the second; the replacement must land in a later second than the originals for the
		// trim in the next spec to be about seniority rather than the name tie-break.
		var newest time.Time
		for _, binding := range bindingsOf(Default) {
			if binding.CreationTimestamp.After(newest) {
				newest = binding.CreationTimestamp.Time
			}
		}
		Eventually(func() bool { return time.Now().After(newest.Add(time.Second)) }, eventuallyTimeout, pollInterval).Should(BeTrue())

		setJoined("binder-c1", metav1.ConditionFalse)
		Eventually(wantBound("binder-c1", "binder-c2", "binder-c3"), eventuallyTimeout, pollInterval).Should(Succeed())
		// The dark cluster is still bound but no longer counted.
		wantScheduled(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2, 2)
	})

	It("trims the replacement once the cluster is back", func() {
		setJoined("binder-c1", metav1.ConditionTrue)
		Eventually(wantBound("binder-c1", "binder-c3"), eventuallyTimeout, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2, 2)
	})

	It("keeps a bound cluster that stops matching the selector, still counted", func() {
		setRegion("binder-c3", "elsewhere")

		// The relabel is a scheduling-relevant change, so the policy is reconciled; give that
		// round time to land before asserting nothing moved.
		Consistently(wantBound("binder-c1", "binder-c3"), consistentlyDuration, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2, 2)
	})

	It("scales down by unbinding the newest binding", func() {
		setSelectorCount(policyKey, 1)
		Eventually(wantBound("binder-c1"), eventuallyTimeout, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 1, 1)
	})

	It("scales back up", func() {
		setSelectorCount(policyKey, 2)
		Eventually(wantBound("binder-c1", "binder-c2"), eventuallyTimeout, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2, 2)
	})

	It("recreates a binding deleted out of band", func() {
		policy := &kfplacementv1alpha1.PlacementPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).Should(Succeed())
		key := types.NamespacedName{Namespace: testNamespace, Name: bindingName(policy, "binder-c2")}
		binding := &kfplacementv1alpha1.PlacementBinding{}
		Expect(k8sClient.Get(ctx, key, binding)).Should(Succeed())
		deleted := binding.UID
		Expect(k8sClient.Delete(ctx, binding)).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, key, binding)).Should(Succeed())
			g.Expect(binding.UID).ShouldNot(Equal(deleted), "the binding should have been recreated, not resurrected")
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("unbinds a cluster that leaves the fleet", func() {
		Expect(k8sClient.Delete(ctx, &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "binder-c1"}})).Should(Succeed())
		Eventually(wantBound("binder-c2"), eventuallyTimeout, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 2, 1)
	})

	It("re-evaluates the bound clusters from scratch when the selector terms change", func() {
		By("a bound cluster that stops matching stays bound")
		setRegion("binder-c2", "elsewhere")
		Consistently(wantBound("binder-c2"), consistentlyDuration, pollInterval).Should(Succeed())

		By("new terms that the cluster does not match release it")
		policy := &kfplacementv1alpha1.PlacementPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).Should(Succeed())
		policy.Spec.ClusterSelectors[0].Terms = []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{testRegionLabel: "nowhere"}},
		}
		Expect(k8sClient.Update(ctx, policy)).Should(Succeed())
		Eventually(wantBound(), eventuallyTimeout, pollInterval).Should(Succeed())
		wantScheduled(metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 2, 0)
	})
})

var _ = Describe("cluster placement binding lifecycle", Ordered, func() {
	const (
		region      = "binding-cluster-scope"
		policyName  = "cluster-binder"
		clusterName = "cluster-binder-c1"
	)
	policyKey := types.NamespacedName{Name: policyName}

	BeforeAll(func() {
		Expect(k8sClient.Create(ctx, newMemberCluster(clusterName, map[string]string{testRegionLabel: region}))).Should(Succeed())
		markJoined(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName}})
		policy := &kfplacementv1alpha1.ClusterPlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: policyName},
			Spec: kfplacementv1alpha1.PlacementPolicySpec{
				ClusterSelectors: []kfplacementv1alpha1.ClusterSelector{regionSelector(region, ptr.To(intstr.FromInt32(1)), nil)},
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "Namespace", Name: "demo"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())
	})

	AfterAll(func() {
		policy := &kfplacementv1alpha1.ClusterPlacementPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, policy))).Should(Succeed())
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, policyKey, &kfplacementv1alpha1.ClusterPlacementPolicy{}))
		}, eventuallyTimeout, pollInterval).Should(BeTrue(), "the policy should be gone")
		Expect(k8sClient.DeleteAllOf(ctx, &kfplacementv1alpha1.ClusterPlacementBinding{})).Should(Succeed())
		drainMemberClusters()
	})

	It("binds the cluster with a cluster-scoped binding owned by the policy", func() {
		policy := &kfplacementv1alpha1.ClusterPlacementPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).Should(Succeed())
		binding := &kfplacementv1alpha1.ClusterPlacementBinding{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bindingName(policy, clusterName)}, binding)).Should(Succeed())
			g.Expect(binding.Spec.ClusterName).Should(Equal(clusterName))
			g.Expect(binding.Spec.ResourceSnapshotName).Should(Equal(stubSnapshotName(policy)))
			g.Expect(metav1.IsControlledBy(binding, policy)).Should(BeTrue())
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})
})
