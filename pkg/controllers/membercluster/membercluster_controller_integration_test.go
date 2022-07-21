/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package membercluster

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

var _ = Describe("Test MemberCluster Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	var (
		ctx                         context.Context
		memberClusterName           string
		namespaceName               string
		memberClusterNamespacedName types.NamespacedName
		r                           *Reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		memberClusterName = utils.RandStr()
		namespaceName = fmt.Sprintf(utils.NamespaceNameFormat, memberClusterName)
		memberClusterNamespacedName = types.NamespacedName{
			Name: memberClusterName,
		}

		By("create the member cluster reconciler")
		r = &Reconciler{
			Client: k8sClient,
		}
		err := r.SetupWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		By("create member cluster for join")
		mc := &fleetv1alpha1.MemberCluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       "MemberCluster",
				APIVersion: fleetv1alpha1.GroupVersion.Version,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterName,
			},
			Spec: fleetv1alpha1.MemberClusterSpec{
				State: fleetv1alpha1.ClusterStateJoin,
				Identity: rbacv1.Subject{
					Kind: rbacv1.ServiceAccountKind,
					Name: "hub-access",
				},
			},
		}
		Expect(k8sClient.Create(ctx, mc)).Should(Succeed())

		By("trigger reconcile to initiate the join workflow")
		result, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: memberClusterNamespacedName,
		})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Not(HaveOccurred()))

		var ns corev1.Namespace
		var role rbacv1.Role
		var roleBinding rbacv1.RoleBinding
		var imc fleetv1alpha1.InternalMemberCluster
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleNameFormat, memberClusterName), Namespace: namespaceName}, &role)).Should(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(utils.RoleBindingNameFormat, memberClusterName), Namespace: namespaceName}, &roleBinding)).Should(Succeed())

		By("simulate member agent updating internal member cluster status")
		imc.Status.Capacity = utils.NewResourceList()
		imc.Status.Allocatable = utils.NewResourceList()
		joinedCondition := metav1.Condition{
			Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
			Status:             metav1.ConditionTrue,
			Reason:             reasonMemberClusterJoined,
			ObservedGeneration: imc.GetGeneration(),
		}
		heartBeatReceivedCondition := metav1.Condition{
			Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
			Status:             metav1.ConditionTrue,
			Reason:             "InternalMemberClusterHeartbeatReceived",
			ObservedGeneration: imc.GetGeneration(),
		}
		imc.SetConditions(joinedCondition, heartBeatReceivedCondition)
		Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

		By("trigger reconcile again to update member cluster status to joined")
		result, err = r.Reconcile(ctx, ctrl.Request{
			NamespacedName: memberClusterNamespacedName,
		})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Not(HaveOccurred()))
	})

	AfterEach(func() {
		var ns corev1.Namespace
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)).Should(Succeed())

		By("Deleting the namespace")
		Eventually(func() error {
			return k8sClient.Delete(ctx, &ns)
		}, timeout, interval).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
	})

	It("should create namespace, role, role binding and internal member cluster & mark member cluster as joined", func() {
		var mc fleetv1alpha1.MemberCluster
		Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())

		readyToJoinCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterReadyToJoin)
		Expect(readyToJoinCondition).NotTo(BeNil())
		Expect(readyToJoinCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(readyToJoinCondition.Reason).To(Equal(reasonMemberClusterReadyToJoin))

		joinCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterJoin)
		Expect(joinCondition).NotTo(BeNil())
		Expect(joinCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(joinCondition.Reason).To(Equal(reasonMemberClusterJoined))

		heartBeatCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
		Expect(heartBeatCondition).NotTo(BeNil())
		Expect(heartBeatCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(heartBeatCondition.Reason).To(Equal("InternalMemberClusterHeartbeatReceived"))
	})

	It("member cluster is marked as left after leave workflow is completed", func() {
		By("Update member cluster's spec to leave")
		var mc fleetv1alpha1.MemberCluster
		Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())
		mc.Spec.State = fleetv1alpha1.ClusterStateLeave
		Expect(k8sClient.Update(ctx, &mc))

		By("trigger reconcile again to initiate leave workflow")
		result, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: memberClusterNamespacedName,
		})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Not(HaveOccurred()))

		var imc fleetv1alpha1.InternalMemberCluster
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: namespaceName}, &imc)).Should(Succeed())
		Expect(imc.Spec.State).To(Equal(fleetv1alpha1.ClusterStateLeave))

		By("mark Internal Member Cluster as left")
		imcLeftCondition := metav1.Condition{
			Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
			Status:             metav1.ConditionFalse,
			Reason:             "InternalMemberClusterLeft",
			ObservedGeneration: imc.GetGeneration(),
		}
		imc.SetConditions(imcLeftCondition)
		Expect(k8sClient.Status().Update(ctx, &imc)).Should(Succeed())

		By("trigger reconcile again to mark member cluster as left")
		result, err = r.Reconcile(ctx, ctrl.Request{
			NamespacedName: memberClusterNamespacedName,
		})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Not(HaveOccurred()))

		Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &mc)).Should(Succeed())
		mcLeftCondition := mc.GetCondition(fleetv1alpha1.ConditionTypeMemberClusterJoin)
		Expect(mcLeftCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(mcLeftCondition.Reason).To(Equal(reasonMemberClusterLeft))
	})
})
