/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package internalmembercluster

import (
	"context"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

var _ = Describe("Test Internal Member Cluster Controller", func() {
	var (
		ctx                              context.Context
		HBPeriod                         int
		internalMemberClusterChan        chan v1alpha1.ClusterState
		membershipChan                   chan v1alpha1.ClusterState
		memberClusterName                string
		memberClusterNamespace           string
		memberClusterNamespacedName      types.NamespacedName
		nodes                            corev1.NodeList
		chanMsgLock                      sync.RWMutex
		internalMemberClusterChanLastMsg v1alpha1.ClusterState
		r                                *Reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		HBPeriod = int(utils.RandSecureInt(1000))
		memberClusterName = "rand-" + strings.ToLower(utils.RandStr()) + "-mc"
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

		By("creating member cluster nodes")
		nodes = corev1.NodeList{Items: utils.NewTestNodes(memberClusterNamespace)}
		for _, node := range nodes.Items {
			node := node // prevent Implicit memory aliasing in for loop
			Expect(k8sClient.Create(ctx, &node)).Should(Succeed())
		}

		By("create the internalMemberCluster reconciler")
		r = NewReconciler(k8sClient, k8sClient, internalMemberClusterChan, membershipChan)
		err := r.SetupWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		go func() {
			for state := range internalMemberClusterChan {
				chanMsgLock.Lock()
				internalMemberClusterChanLastMsg = state
				chanMsgLock.Unlock()
			}
		}()
	})

	AfterEach(func() {
		close(internalMemberClusterChan)
		close(membershipChan)

		By("delete member cluster namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, &ns)).Should(Succeed())

		By("delete member cluster nodes")
		for _, node := range nodes.Items {
			node := node
			Expect(k8sClient.Delete(ctx, &node)).Should(Succeed())
		}

		By("delete member cluster")
		internalMemberCluster := v1alpha1.InternalMemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      memberClusterName,
				Namespace: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, &internalMemberCluster)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
	})

	Context("join", func() {
		BeforeEach(func() {
			internalMemberCluster := v1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: v1alpha1.InternalMemberClusterSpec{
					State:                  v1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(HBPeriod),
				},
			}
			Expect(k8sClient.Create(ctx, &internalMemberCluster)).Should(Succeed())
		})

		It("should update internalMemberCluster to joined when start after membership with Join State was created", func() {
			membershipChan <- v1alpha1.ClusterStateJoin
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{RequeueAfter: time.Second * time.Duration(HBPeriod)}))
			Expect(err).Should(Not(HaveOccurred()))

			var imc v1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &imc)).Should(Succeed())

			By("checking updated join condition")
			updatedJoinedCond := imc.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterJoin)
			Expect(updatedJoinedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(updatedJoinedCond.Reason).To(Equal(eventReasonInternalMemberClusterJoined))

			By("checking updated heartbeat condition")
			updatedHBCond := imc.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterHeartbeat)
			Expect(updatedHBCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(updatedHBCond.Reason).To(Equal(eventReasonInternalMemberClusterHBReceived))

			By("checking updated health condition")
			updatedHealthCond := imc.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterHealth)
			Expect(updatedHealthCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(updatedHealthCond.Reason).To(Equal(eventReasonInternalMemberClusterHealthy))

			By("checking updated member cluster usage")
			Expect(imc.Status.Allocatable).ShouldNot(BeNil())
			Expect(imc.Status.Capacity).ShouldNot(BeNil())
		})

		It("should return error joining when start before membership with Join State was created", func() {
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{RequeueAfter: time.Second * time.Duration(HBPeriod)}))
			Expect(err).ShouldNot(HaveOccurred())

			// We need to write to a chan that has a goroutine reading from it before closing the chan. Otherwise, there
			// will be data race on the chan var in the next test.
			internalMemberClusterChan <- v1alpha1.ClusterStateLeave
		})
	})

	Context("leave", func() {
		BeforeEach(func() {
			By("create internalMemberCluster CR")
			internalMemberCluster := v1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: v1alpha1.InternalMemberClusterSpec{
					State:                  v1alpha1.ClusterStateLeave,
					HeartbeatPeriodSeconds: int32(HBPeriod),
				},
			}
			Expect(k8sClient.Create(ctx, &internalMemberCluster)).Should(Succeed())

			By("update internalMemberCluster CR with random usage status")
			internalMemberCluster.Status = v1alpha1.InternalMemberClusterStatus{
				Conditions:  []metav1.Condition{},
				Allocatable: utils.NewResourceList(),
				Capacity:    utils.NewResourceList(),
			}
			Expect(k8sClient.Status().Update(ctx, &internalMemberCluster)).Should(Succeed())
		})

		It("should update internalMemberCluster to left when start after membership controller finished leaving", func() {
			membershipChan <- v1alpha1.ClusterStateLeave
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Not(HaveOccurred()))

			var internalMemberCluster v1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &internalMemberCluster)).Should(Succeed())

			By("checking updated join condition")
			updatedJoinedCond := internalMemberCluster.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterJoin)
			Expect(updatedJoinedCond.Status).Should(Equal(metav1.ConditionFalse))
			Expect(updatedJoinedCond.Reason).Should(Equal(eventReasonInternalMemberClusterLeft))
		})

		It("should update internalMemberCluster to unknown when start before membership controller finished leaving", func() {
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{RequeueAfter: requeueAfterPeriod}))
			Expect(err).Should(Not(HaveOccurred()))

			var internalMemberCluster v1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &internalMemberCluster)).Should(Succeed())

			By("checking updated join condition")
			updatedJoinedCond := internalMemberCluster.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterJoin)
			Expect(updatedJoinedCond.Status).Should(Equal(metav1.ConditionUnknown))
			Expect(updatedJoinedCond.Reason).Should(Equal(eventReasonInternalMemberClusterUnknown))

			// We need to write to a chan that has a goroutine reading from it before closing the chan. Otherwise, there
			// will be data race on the chan var in the next test.
			internalMemberClusterChan <- v1alpha1.ClusterStateLeave
		})
	})

	It("should send Leave message to internalMemberCluster chan when start after internalMemberClusterCR was deleted", func() {
		result, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: memberClusterNamespacedName,
		})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Not(HaveOccurred()))

		By("checking last message in internalMemberCluster chan")
		chanMsgLock.Lock()
		defer chanMsgLock.Unlock()
		Expect(internalMemberClusterChanLastMsg).Should(Equal(v1alpha1.ClusterStateLeave))
	})
})
