/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package membership

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

var _ = Describe("Test Membership Controller", func() {
	var (
		ctx                         context.Context
		internalMemberClusterChan   chan v1alpha1.ClusterState
		membershipChan              chan v1alpha1.ClusterState
		memberClusterName           string
		memberClusterNamespace      string
		memberClusterNamespacedName types.NamespacedName
		r                           *Reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		memberClusterName = "mc" + strings.ToLower(utils.RandStr())
		memberClusterNamespace = "fleet-" + memberClusterName
		memberClusterNamespacedName = types.NamespacedName{
			Name:      memberClusterName,
			Namespace: memberClusterNamespace,
		}
		internalMemberClusterChan = make(chan v1alpha1.ClusterState)
		membershipChan = make(chan v1alpha1.ClusterState)

		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

		r = NewReconciler(k8sClient, internalMemberClusterChan, membershipChan)
		err := r.SetupWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		go func() {
			for range membershipChan {
			}
		}()
	})

	AfterEach(func() {
		close(internalMemberClusterChan)
		close(membershipChan)

		By("delete the member cluster namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, &ns)).Should(Succeed())

		membership := v1alpha1.Membership{
			ObjectMeta: metav1.ObjectMeta{
				Name:      memberClusterName,
				Namespace: memberClusterNamespace,
			},
			Spec: v1alpha1.MembershipSpec{State: v1alpha1.ClusterStateJoin},
		}
		Expect(k8sClient.Delete(ctx, &membership)).Should(Succeed())

		Expect(internalMemberClusterChan).Should(BeClosed())
		Expect(membershipChan).Should(BeClosed())
	})

	Context("after join flow on InternalMemberCluster controller finished", func() {
		BeforeEach(func() {
			internalMemberClusterChan <- v1alpha1.ClusterStateJoin
		})
		It("should update Membership CR condition", func() {
			membership := v1alpha1.Membership{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: v1alpha1.MembershipSpec{State: v1alpha1.ClusterStateJoin},
			}
			Expect(k8sClient.Create(ctx, &membership)).Should(Succeed())

			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Not(HaveOccurred()))

			var updatedMembership v1alpha1.Membership
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &updatedMembership)).Should(Succeed())

			membershipJoinCond := updatedMembership.GetCondition(v1alpha1.ConditionTypeMembershipJoin)
			Expect(membershipJoinCond.Status).Should(Equal(metav1.ConditionTrue))
			Expect(membershipJoinCond.Reason).Should(Equal(eventReasonMembershipJoined))
		})
	})
})
