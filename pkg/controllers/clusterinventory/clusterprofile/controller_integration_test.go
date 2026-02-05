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
package clusterprofile

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

const (
	testTargetCluster = "test-cluster"
	clusterProfileNS  = "default"

	eventuallyTimeout    = time.Second * 5
	consistentlyDuration = time.Second * 10
	interval             = time.Millisecond * 250
)

// This container cannot be run in parallel with other ITs because it uses a shared fakePlacementController.
var _ = Describe("Test ClusterProfile Controller", func() {
	var mc *clusterv1beta1.MemberCluster
	var clusterProfile clusterinventory.ClusterProfile
	var testMCName string
	BeforeEach(func() {
		testMCName = testTargetCluster + utils.RandStr()
		By("Creating a new MemberCluster")
		mc = memberClusterForTest(testMCName)
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed(), "failed to create MemberCluster")
	})

	AfterEach(func() {
		By("Deleting the MemberCluster")
		Expect(k8sClient.Delete(ctx, mc)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
		By("Deleting the ClusterProfile")
		Expect(k8sClient.Delete(ctx, &clusterProfile)).Should(SatisfyAny(Succeed(), utils.NotFoundMatcher{}))
	})

	It("Should create a clusterProfile when a member cluster is created", func() {
		By("Check the clusterProfile is not created")
		Consistently(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, consistentlyDuration, interval).ShouldNot(Succeed(), "clusterProfile is created before member cluster is marked as join")
		By("Mark the member cluster as joined")
		mc.Status.Conditions = []metav1.Condition{
			{
				Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
				Status:             metav1.ConditionTrue,
				Reason:             "Joined",
				Message:            "Member cluster has joined",
				LastTransitionTime: metav1.Time{Time: time.Now()},
				ObservedGeneration: mc.Generation,
			},
		}
		Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed(), "failed to update member cluster status")
		By("Check the clusterProfile is created")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, eventuallyTimeout, interval).Should(Succeed(), "clusterProfile is not created")
		By("Check the MemberCluster has the finalizer")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: testMCName}, mc)).Should(Succeed(), "failed to get MemberCluster")
		Expect(controllerutil.ContainsFinalizer(mc, clusterProfileCleanupFinalizer)).Should(BeTrue(), "failed to add the finalizer to MemberCluster")
		mc.Status.AgentStatus = []clusterv1beta1.AgentStatus{
			{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.AgentHealthy),
						Status:             metav1.ConditionTrue,
						Reason:             "Healthy",
						Message:            "Agent is healthy",
						LastTransitionTime: metav1.Time{Time: time.Now()},
						ObservedGeneration: mc.Generation,
					},
				},
				LastReceivedHeartbeat: metav1.Time{Time: time.Now()},
			},
		}
		Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed(), "failed to update member cluster status")
		Eventually(func() bool {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile); err != nil {
				return false
			}
			if clusterProfile.Spec.ClusterManager.Name != controller.ClusterManagerName {
				return false
			}
			if clusterProfile.Spec.DisplayName != testMCName {
				return false
			}
			cond := meta.FindStatusCondition(clusterProfile.Status.Conditions, clusterinventory.ClusterConditionControlPlaneHealthy)
			return condition.IsConditionStatusTrue(cond, clusterProfile.Generation)
		}, eventuallyTimeout, interval).Should(BeTrue(), "clusterProfile is not created")
	})

	It("Should recreate a clusterProfile when it is deleted by the user but properties should not show if MC property collection is not succeeded", func() {
		By("Mark the member cluster as joined")
		mc.Status.Conditions = []metav1.Condition{
			{
				Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
				Status:             metav1.ConditionTrue,
				Reason:             "Joined",
				Message:            "Member cluster has joined",
				LastTransitionTime: metav1.Time{Time: time.Now()},
				ObservedGeneration: mc.Generation,
			},
		}
		Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed(), "failed to update member cluster status")
		By("Check the clusterProfile is created")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, eventuallyTimeout, interval).Should(Succeed(), "clusterProfile is not created")
		By("Deleting the ClusterProfile")
		Expect(k8sClient.Delete(ctx, &clusterProfile)).Should(Succeed(), "failed to delete clusterProfile")
		By("Check the clusterProfile is created again")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, eventuallyTimeout, interval).Should(Succeed(), "clusterProfile is not created")
		By("Check the properties are not created")
		Consistently(func() bool {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile); err != nil {
				return false
			}
			return clusterProfile.Status.AccessProviders == nil || clusterProfile.Status.AccessProviders[0].Cluster.CertificateAuthorityData == nil
		}, consistentlyDuration, interval).Should(BeTrue(), "ClusterCertificateAuthority property is created before member cluster is marked as collection succeeded")
	})

	It("Should have property filled in clusterProfile created from MemberCluster and reconcile the clusterProfile if changed", func() {
		By("Mark the member cluster as joined")
		mc.Status.Conditions = []metav1.Condition{
			{
				Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
				Status:             metav1.ConditionTrue,
				Reason:             "CollectionSucceeded",
				Message:            "Cluster property collection succeeded",
				LastTransitionTime: metav1.Time{Time: time.Now()},
				ObservedGeneration: mc.Generation,
			},
			{
				Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
				Status:             metav1.ConditionTrue,
				Reason:             "Joined",
				Message:            "Member cluster has joined",
				LastTransitionTime: metav1.Time{Time: time.Now()},
				ObservedGeneration: mc.Generation,
			},
		}
		mc.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
			propertyprovider.ClusterCertificateAuthorityProperty: {
				Value:           "dummy-ca-data",
				ObservationTime: metav1.Time{Time: time.Now()},
			},
		}
		Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed(), "failed to update member cluster status")
		By("Check the clusterProfile is created")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, eventuallyTimeout, interval).Should(Succeed(), "clusterProfile is not created")
		By("Check the properties in clusterProfile")
		Eventually(func() bool {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile); err != nil {
				return false
			}
			return string(clusterProfile.Status.AccessProviders[0].Cluster.CertificateAuthorityData) == "dummy-ca-data"
		}, eventuallyTimeout, interval).Should(BeTrue(), "ClusterCertificateAuthority property is not created")
		By("Modifying the ClusterProfile")
		clusterProfile.Spec.DisplayName = "ModifiedMCName"
		Expect(k8sClient.Update(ctx, &clusterProfile)).Should(Succeed(), "failed to modify clusterProfile")
		By("Check the clusterProfile is updated back to original state")
		Eventually(func() bool {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile); err != nil {
				return false
			}
			return clusterProfile.Spec.DisplayName == testMCName
		}, eventuallyTimeout, interval).Should(BeTrue(), "clusterProfile is not updated back to original state")
	})

	It("Should delete the clusterProfile when the MemberCluster is deleted", func() {
		By("Mark the member cluster as joined")
		mc.Status.Conditions = []metav1.Condition{
			{
				Type:               string(clusterv1beta1.ConditionTypeMemberClusterJoined),
				Status:             metav1.ConditionTrue,
				Reason:             "Joined",
				Message:            "Member cluster has joined",
				LastTransitionTime: metav1.Time{Time: time.Now()},
				ObservedGeneration: mc.Generation,
			},
		}
		Expect(k8sClient.Status().Update(ctx, mc)).Should(Succeed(), "failed to update member cluster status")
		By("Check the clusterProfile is created")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, eventuallyTimeout, interval).Should(Succeed(), "clusterProfile is not created")
		By("Deleting the MemberCluster")
		Expect(k8sClient.Delete(ctx, mc)).Should(Succeed(), "failed to delete clusterProfile")
		By("Check the clusterProfile is deleted too")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, eventuallyTimeout, interval).Should(utils.NotFoundMatcher{}, "clusterProfile is not deleted")
		Consistently(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: clusterProfileNS, Name: testMCName}, &clusterProfile)
		}, consistentlyDuration, interval).Should(utils.NotFoundMatcher{}, "clusterProfile is not deleted")
	})
})

func memberClusterForTest(mcName string) *clusterv1beta1.MemberCluster {
	return &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: mcName,
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      "test-service-account",
				Namespace: "fleet-system",
			},
		},
	}
}
