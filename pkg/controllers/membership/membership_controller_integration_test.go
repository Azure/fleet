/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package membership

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
		joinSucceedCounter          = promauto.NewCounter(prometheus.CounterOpts{
			Name: "member_agent_join_succeed_cnt",
			Help: "counts the number of successful Join operations for member agent",
		})
		joinFailCounter = promauto.NewCounter(prometheus.CounterOpts{
			Name: "member_agent_join_fail_cnt",
			Help: "counts the number of failed Join operations for member agent",
		})
		leaveSucceedCounter = promauto.NewCounter(prometheus.CounterOpts{
			Name: "member_agent_leave_succeed_cnt",
			Help: "counts the number of successful Leave operations for member agent",
		})
		leaveFailCounter = promauto.NewCounter(prometheus.CounterOpts{
			Name: "member_agent_leave_fail_cnt",
			Help: "counts the number of failed Leave operations for member agent",
		})
	)

	BeforeEach(func() {
		ctx = context.Background()
		memberClusterName = strings.ToLower(utils.RandStr()) + "-mc"
		memberClusterNamespace = "fleet-" + memberClusterName
		memberClusterNamespacedName = types.NamespacedName{
			Name:      memberClusterName,
			Namespace: memberClusterNamespace,
		}
		internalMemberClusterChan = make(chan v1alpha1.ClusterState)
		membershipChan = make(chan v1alpha1.ClusterState)

		By("create the member cluster namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

		By("create the membership reconciler")
		r = NewReconciler(k8sClient, internalMemberClusterChan, membershipChan,
			MemberAgentJoinLeaveMetrics{
				JoinSucceedCounter:  joinSucceedCounter,
				JoinFailCounter:     joinFailCounter,
				LeaveSucceedCounter: leaveSucceedCounter,
				LeaveFailCounter:    leaveFailCounter,
			})
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

		By("delete the member cluster membership CR")
		membership := v1alpha1.Membership{
			ObjectMeta: metav1.ObjectMeta{
				Name:      memberClusterName,
				Namespace: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, &membership)).Should(Succeed())

		Expect(internalMemberClusterChan).Should(BeClosed())
		Expect(membershipChan).Should(BeClosed())
	})

	Context("join", func() {
		BeforeEach(func() {
			membership := v1alpha1.Membership{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: v1alpha1.MembershipSpec{State: v1alpha1.ClusterStateJoin},
			}
			Expect(k8sClient.Create(ctx, &membership)).Should(Succeed())
		})

		Context("after internalMemberCluster controller finished joining", func() {
			BeforeEach(func() {
				internalMemberClusterChan <- v1alpha1.ClusterStateJoin
			})
			It("should update membership CR to joined", func() {
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: memberClusterNamespacedName,
				})
				Expect(result).Should(Equal(ctrl.Result{}))
				Expect(err).Should(Not(HaveOccurred()))

				By("checking membership CR updated condition")
				var updatedMembership v1alpha1.Membership
				Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &updatedMembership)).Should(Succeed())

				membershipJoinCond := updatedMembership.GetCondition(v1alpha1.ConditionTypeMembershipJoin)
				Expect(membershipJoinCond.Status).Should(Equal(metav1.ConditionTrue))
				Expect(membershipJoinCond.Reason).Should(Equal(eventReasonMembershipJoined))
			})
		})

		Context("before internalMemberCluster controller finished joining", func() {
			It("should update membership CR to unknown", func() {
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      memberClusterName,
						Namespace: memberClusterNamespace,
					},
				})
				Expect(result).Should(Equal(ctrl.Result{RequeueAfter: time.Minute}))
				Expect(err).Should(Not(HaveOccurred()))

				By("checking membership CR updated condition")
				var updatedMembership v1alpha1.Membership
				Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &updatedMembership)).Should(Succeed())

				membershipJoinCond := updatedMembership.GetCondition(v1alpha1.ConditionTypeMembershipJoin)
				Expect(membershipJoinCond.Status).Should(Equal(metav1.ConditionUnknown))
				Expect(membershipJoinCond.Reason).Should(Equal(eventReasonMembershipUnknown))
			})
		})
	})

	Context("leave", func() {
		BeforeEach(func() {
			membership := v1alpha1.Membership{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: v1alpha1.MembershipSpec{State: v1alpha1.ClusterStateLeave},
			}
			Expect(k8sClient.Create(ctx, &membership)).Should(Succeed())
		})

		Context("after internalMemberCluster controller finished leaving", func() {
			BeforeEach(func() {
				internalMemberClusterChan <- v1alpha1.ClusterStateLeave
			})
			It("should update membership CR to left", func() {
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      memberClusterName,
						Namespace: memberClusterNamespace,
					},
				})
				Expect(result).Should(Equal(ctrl.Result{}))
				Expect(err).Should(Not(HaveOccurred()))

				By("checking membership CR updated condition")
				var updatedMembership v1alpha1.Membership
				Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &updatedMembership)).Should(Succeed())

				membershipJoinCond := updatedMembership.GetCondition(v1alpha1.ConditionTypeMembershipJoin)
				Expect(membershipJoinCond.Status).Should(Equal(metav1.ConditionFalse))
				Expect(membershipJoinCond.Reason).Should(Equal(eventReasonMembershipLeft))
			})
		})

		Context("before leave flow on internalMemberCluster controller finished", func() {
			It("should update membership CR to unknown", func() {
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      memberClusterName,
						Namespace: memberClusterNamespace,
					},
				})
				Expect(result).Should(Equal(ctrl.Result{RequeueAfter: time.Minute}))
				Expect(err).Should(Not(HaveOccurred()))

				By("checking membership CR updated condition")
				var updatedMembership v1alpha1.Membership
				Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &updatedMembership)).Should(Succeed())

				membershipJoinCond := updatedMembership.GetCondition(v1alpha1.ConditionTypeMembershipJoin)
				Expect(membershipJoinCond.Status).Should(Equal(metav1.ConditionUnknown))
				Expect(membershipJoinCond.Reason).Should(Equal(eventReasonMembershipUnknown))
			})
		})
	})
})
