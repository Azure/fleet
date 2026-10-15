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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	eventuallyTimeout    = time.Second * 15
	consistentlyDuration = time.Second * 2
	pollInterval         = time.Millisecond * 250

	testRegionLabel = "topology.kubernetes.io/region"
)

// newMemberCluster builds a MemberCluster object; the caller decides whether to mark it joined.
func newMemberCluster(name string, labels map[string]string, taints ...clusterv1beta1.Taint) *clusterv1beta1.MemberCluster {
	return &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: "fleet-system",
			},
			Taints: taints,
		},
	}
}

// markJoined stamps a member agent status that passes the scheduler's eligibility gate: joined,
// healthy, and heartbeating.
func markJoined(mc *clusterv1beta1.MemberCluster) {
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed())
	now := metav1.Now()
	mc.Status.AgentStatus = []clusterv1beta1.AgentStatus{{
		Type: clusterv1beta1.MemberAgent,
		Conditions: []metav1.Condition{
			{Type: string(clusterv1beta1.AgentJoined), Status: metav1.ConditionTrue, Reason: "AgentJoined", Message: "integration test: simulated join", LastTransitionTime: now},
			{Type: string(clusterv1beta1.AgentHealthy), Status: metav1.ConditionTrue, Reason: "AgentHealthy", Message: "integration test: simulated health", LastTransitionTime: now},
		},
		LastReceivedHeartbeat: now,
	}}
	Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed())
}

func newPolicy(name string, selectors ...kfplacementv1alpha1.ClusterSelector) *kfplacementv1alpha1.PlacementPolicy {
	return &kfplacementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: kfplacementv1alpha1.PlacementPolicySpec{
			ClusterSelectors: selectors,
			ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "demo"},
			},
		},
	}
}

func regionSelector(region string, count *intstr.IntOrString, minCount *int32) kfplacementv1alpha1.ClusterSelector {
	return kfplacementv1alpha1.ClusterSelector{
		Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{
			{MatchLabels: map[string]string{testRegionLabel: region}},
		},
		Count:    count,
		MinCount: minCount,
	}
}

// scheduledConditionOf fetches the policy and returns its Scheduled condition together with the
// desired/scheduled counts; nil pieces denote absence.
func scheduledConditionOf(key types.NamespacedName) func() (*metav1.Condition, *int32, *int32, error) {
	return func() (*metav1.Condition, *int32, *int32, error) {
		policy := &kfplacementv1alpha1.PlacementPolicy{}
		if err := k8sClient.Get(ctx, key, policy); err != nil {
			return nil, nil, nil, err
		}
		cond := meta.FindStatusCondition(policy.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled)
		return cond, policy.Status.DesiredClusters, policy.Status.ScheduledClusters, nil
	}
}

func wantScheduledState(key types.NamespacedName, status metav1.ConditionStatus, reason string, desired, scheduled int32) func(Gomega) {
	return func(g Gomega) {
		cond, gotDesired, gotScheduled, err := scheduledConditionOf(key)()
		g.Expect(err).Should(Succeed())
		g.Expect(cond).NotTo(BeNil(), "Scheduled condition is not set")
		g.Expect(cond.Status).Should(Equal(status))
		g.Expect(cond.Reason).Should(Equal(reason))
		g.Expect(gotDesired).NotTo(BeNil(), "desiredClusters is not set")
		g.Expect(*gotDesired).Should(Equal(desired))
		g.Expect(gotScheduled).NotTo(BeNil(), "scheduledClusters is not set")
		g.Expect(*gotScheduled).Should(Equal(scheduled))
	}
}

var _ = Describe("placement policy scheduling status", Ordered, func() {
	var counter int

	nextName := func(prefix string) string {
		counter++
		return fmt.Sprintf("%s-%d", prefix, counter)
	}

	AfterEach(func() {
		policies := &kfplacementv1alpha1.PlacementPolicyList{}
		Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).Should(Succeed())
		for i := range policies.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &policies.Items[i]))).Should(Succeed())
		}
		clusterPolicies := &kfplacementv1alpha1.ClusterPlacementPolicyList{}
		Expect(k8sClient.List(ctx, clusterPolicies)).Should(Succeed())
		for i := range clusterPolicies.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &clusterPolicies.Items[i]))).Should(Succeed())
		}
		// Policy deletion is finalizer-gated on claim cleanup (unfulfilled selectors in these
		// specs spawn claims via the AddClusterClaim default); wait for policies and their
		// claims to fully drain so no state leaks into other suites in this package.
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.List(ctx, policies, client.InNamespace(testNamespace))).Should(Succeed())
			g.Expect(policies.Items).Should(BeEmpty())
			g.Expect(k8sClient.List(ctx, clusterPolicies)).Should(Succeed())
			g.Expect(clusterPolicies.Items).Should(BeEmpty())
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

	It("reports unfulfilled scheduling when no clusters match", func() {
		policy := newPolicy(nextName("pp"), regionSelector("eastus", ptr.To(intstr.FromInt32(2)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 2, 0),
			eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("fulfills scheduling once enough eligible clusters match", func() {
		policy := newPolicy(nextName("pp"), regionSelector("eastus", ptr.To(intstr.FromInt32(2)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		for _, name := range []string{nextName("mc"), nextName("mc")} {
			mc := newMemberCluster(name, map[string]string{testRegionLabel: "eastus"})
			Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
			markJoined(mc)
		}

		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 2, 2),
			eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("does not count clusters that have not passed the eligibility gate", func() {
		policy := newPolicy(nextName("pp"), regionSelector("westus", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		By("a matching cluster registers but its member agent has not reported")
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "westus"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())

		By("the policy stays unfulfilled through the join window")
		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 1, 0),
			eventuallyTimeout, pollInterval).Should(Succeed())
		Consistently(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 1, 0),
			consistentlyDuration, pollInterval).Should(Succeed())

		By("once the member agent reports in, the policy is fulfilled")
		markJoined(mc)
		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 1, 1),
			eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("distinguishes minimum fulfillment from full fulfillment", func() {
		policy := newPolicy(nextName("pp"), regionSelector("centralus", ptr.To(intstr.FromInt32(3)), ptr.To(int32(1))))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "centralus"})
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)

		By("the floor is met but the desired count is not; the binary contract keeps Scheduled False")
		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 3, 1),
			eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("excludes tainted clusters unless the policy tolerates them", func() {
		taint := clusterv1beta1.Taint{Key: "workload", Value: "restricted", Effect: "NoSchedule"}
		mc := newMemberCluster(nextName("mc"), map[string]string{testRegionLabel: "northeurope"}, taint)
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
		markJoined(mc)

		policy := newPolicy(nextName("pp"), regionSelector("northeurope", ptr.To(intstr.FromInt32(1)), nil))
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		By("the tainted cluster does not count without a toleration")
		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 1, 0),
			eventuallyTimeout, pollInterval).Should(Succeed())
		Consistently(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionFalse, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFailedToFindSomeClusters, 1, 0),
			consistentlyDuration, pollInterval).Should(Succeed())

		By("adding a toleration makes the cluster count")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), policy)).Should(Succeed())
		policy.Spec.Tolerations = []kfplacementv1alpha1.Toleration{
			{Key: "workload", Operator: "Equal", Value: "restricted", Effect: "NoSchedule"},
		}
		Expect(k8sClient.Update(ctx, policy)).Should(Succeed())

		Eventually(wantScheduledState(client.ObjectKeyFromObject(policy), metav1.ConditionTrue, kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters, 1, 1),
			eventuallyTimeout, pollInterval).Should(Succeed())
	})

	It("treats an empty selector list as matching all eligible clusters (cluster-scoped policy)", func() {
		names := []string{nextName("mc"), nextName("mc")}
		for _, name := range names {
			mc := newMemberCluster(name, map[string]string{testRegionLabel: "eastasia"})
			Expect(k8sClient.Create(ctx, mc)).Should(Succeed())
			markJoined(mc)
		}

		policy := &kfplacementv1alpha1.ClusterPlacementPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: nextName("cpp")},
			Spec: kfplacementv1alpha1.PlacementPolicySpec{
				ResourceSelectors: []kfplacementv1alpha1.ResourceSelector{
					{APIVersion: "v1", Kind: "ConfigMap", Name: "demo"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed())

		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.ClusterPlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policy.Name}, fetched)).Should(Succeed())
			cond := meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).Should(Equal(metav1.ConditionTrue))
			g.Expect(cond.Reason).Should(Equal(kfplacementv1alpha1.PlacementPolicyScheduledCondReasonFoundAllClusters))
			g.Expect(fetched.Status.DesiredClusters).NotTo(BeNil())
			g.Expect(*fetched.Status.DesiredClusters).Should(Equal(int32(len(names))))
			g.Expect(fetched.Status.ScheduledClusters).NotTo(BeNil())
			g.Expect(*fetched.Status.ScheduledClusters).Should(Equal(int32(len(names))))
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})

	// The gap this used to pin is closed: #829 added the CEL rule bounding the integer form of
	// count, so zero is now rejected at admission and never reaches the controller.
	It("rejects an integer count of zero at admission", func() {
		policy := newPolicy(nextName("pp"), regionSelector("eastus2", ptr.To(intstr.FromInt32(0)), nil))
		Expect(k8sClient.Create(ctx, policy)).ShouldNot(Succeed(), "count: 0 must be rejected by the CEL guard on the integer form")
	})

	// KNOWN GAP (API, pinned): numeric operators in matchLabelExpressions pass admission; the
	// doc comment defers misuse to the scheduling phase. Admission could reject with a CEL rule
	// on the field instead.
	It("KNOWN GAP: numeric operator in matchLabelExpressions passes admission and is rejected by the controller instead", func() {
		policy := newPolicy(nextName("pp"), kfplacementv1alpha1.ClusterSelector{
			Terms: []kfplacementv1alpha1.ClusterLabelAndPropertySelectorTerm{{
				MatchLabelExpressions: []kfplacementv1alpha1.LabelClusterPropertyExpression{
					{Key: "env", Operator: kfplacementv1alpha1.LabelClusterPropertyExpressionOperatorGt, Values: []string{"1"}},
				},
			}},
		})
		Expect(k8sClient.Create(ctx, policy)).Should(Succeed(), "numeric label expression is currently accepted at admission — flip this once the CEL guard exists")

		Eventually(func(g Gomega) {
			fetched := &kfplacementv1alpha1.PlacementPolicy{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), fetched)).Should(Succeed())
			cond := meta.FindStatusCondition(fetched.Status.Conditions, kfplacementv1alpha1.PlacementPolicyCondTypeScheduled)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).Should(Equal(metav1.ConditionFalse))
			g.Expect(cond.Reason).Should(Equal(reasonInvalidClusterSelectors))
		}, eventuallyTimeout, pollInterval).Should(Succeed())
	})
})
