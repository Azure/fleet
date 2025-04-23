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
package v1alpha1

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/controllers/workv1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var _ = Describe("Test Internal Member Cluster Controller", func() {
	var (
		ctx                         context.Context
		HBPeriod                    int
		memberClusterName           string
		memberClusterNamespace      string
		memberClusterNamespacedName types.NamespacedName
		nodes                       corev1.NodeList
		r                           *Reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		HBPeriod = int(utils.RandSecureInt(600))
		memberClusterName = "rand-" + strings.ToLower(utils.RandStr()) + "-mc"
		memberClusterNamespace = "fleet-" + memberClusterName
		memberClusterNamespacedName = types.NamespacedName{
			Name:      memberClusterName,
			Namespace: memberClusterNamespace,
		}

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
		workController := workv1alpha1.NewApplyWorkReconciler(
			k8sClient, nil, k8sClient, nil, nil, 5, memberClusterNamespace)
		r = NewReconciler(k8sClient, k8sClient, workController)
		err := r.SetupWithManager(mgr, memberClusterName+"-controller")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
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
		internalMemberCluster := fleetv1alpha1.InternalMemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      memberClusterName,
				Namespace: memberClusterNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, &internalMemberCluster)).Should(SatisfyAny(Succeed(), &utils.NotFoundMatcher{}))
	})

	Context("join", func() {
		BeforeEach(func() {
			internalMemberCluster := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(HBPeriod),
				},
			}
			Expect(k8sClient.Create(ctx, &internalMemberCluster)).Should(Succeed())
		})

		It("should update internalMemberCluster to joined", func() {
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			// take into account the +- jitter
			Expect(result.RequeueAfter.Milliseconds() < (1000+1000*jitterPercent/2/100)*time.Millisecond.Milliseconds()*int64(HBPeriod)).Should(BeTrue())
			Expect(result.RequeueAfter.Milliseconds() > (1000-1000*jitterPercent/2/100)*time.Millisecond.Milliseconds()*int64(HBPeriod)).Should(BeTrue())
			Expect(err).Should(Not(HaveOccurred()))

			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &imc)).Should(Succeed())

			By("checking updated join condition")
			updatedJoinedCond := imc.GetConditionWithType(fleetv1alpha1.MemberAgent, string(fleetv1alpha1.AgentJoined))
			Expect(updatedJoinedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(updatedJoinedCond.Reason).To(Equal(eventReasonInternalMemberClusterJoined))

			By("checking updated heartbeat condition")
			agentStatus := imc.Status.AgentStatus[0]
			Expect(agentStatus.LastReceivedHeartbeat).ToNot(Equal(metav1.Now()))

			By("checking updated health condition")
			updatedHealthCond := imc.GetConditionWithType(fleetv1alpha1.MemberAgent, string(fleetv1alpha1.AgentHealthy))
			Expect(updatedHealthCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(updatedHealthCond.Reason).To(Equal(eventReasonInternalMemberClusterHealthy))

			By("checking updated member cluster usage")
			Expect(imc.Status.ResourceUsage.Allocatable).ShouldNot(BeNil())
			Expect(imc.Status.ResourceUsage.Capacity).ShouldNot(BeNil())
			Expect(imc.Status.ResourceUsage.ObservationTime).ToNot(Equal(metav1.Now()))
		})

		It("last received heart beat gets updated after heartbeat", func() {
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			// take into account the +- jitter
			Expect(result.RequeueAfter.Milliseconds() < (1000+1000*jitterPercent/2/100)*time.Millisecond.Milliseconds()*int64(HBPeriod)).Should(BeTrue())
			Expect(result.RequeueAfter.Milliseconds() > (1000-1000*jitterPercent/2/100)*time.Millisecond.Milliseconds()*int64(HBPeriod)).Should(BeTrue())
			Expect(err).Should(Not(HaveOccurred()))

			var imc fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &imc)).Should(Succeed())

			memberAgentStatus := imc.GetAgentStatus(fleetv1alpha1.MemberAgent)
			lastReceivedHeartbeat := memberAgentStatus.LastReceivedHeartbeat

			time.Sleep(time.Second)

			By("trigger reconcile which should update last received heart beat time")
			result, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			// take into account the +- jitter
			Expect(result.RequeueAfter.Milliseconds() < (1000+1000*jitterPercent/2/100)*time.Millisecond.Milliseconds()*int64(HBPeriod)).Should(BeTrue())
			Expect(result.RequeueAfter.Milliseconds() > (1000-1000*jitterPercent/2/100)*time.Millisecond.Milliseconds()*int64(HBPeriod)).Should(BeTrue())
			Expect(err).Should(Not(HaveOccurred()))
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &imc)).Should(Succeed())
			Expect(lastReceivedHeartbeat).ShouldNot(Equal(imc.Status.AgentStatus[0].LastReceivedHeartbeat))
		})
	})

	Context("leave", func() {
		BeforeEach(func() {
			By("create internalMemberCluster CR")
			internalMemberCluster := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateLeave,
					HeartbeatPeriodSeconds: int32(HBPeriod),
				},
			}
			Expect(k8sClient.Create(ctx, &internalMemberCluster)).Should(Succeed())

			By("update internalMemberCluster CR with random usage status")
			internalMemberCluster.Status = fleetv1alpha1.InternalMemberClusterStatus{
				ResourceUsage: fleetv1alpha1.ResourceUsage{
					Capacity:        utils.NewResourceList(),
					Allocatable:     utils.NewResourceList(),
					ObservationTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, &internalMemberCluster)).Should(Succeed())
		})

		It("should update internalMemberCluster to Left", func() {
			result, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: memberClusterNamespacedName,
			})
			Expect(result).Should(Equal(ctrl.Result{}))
			Expect(err).Should(Not(HaveOccurred()))

			var internalMemberCluster fleetv1alpha1.InternalMemberCluster
			Expect(k8sClient.Get(ctx, memberClusterNamespacedName, &internalMemberCluster)).Should(Succeed())

			By("checking updated join condition")
			updatedJoinedCond := internalMemberCluster.GetConditionWithType(fleetv1alpha1.MemberAgent, string(fleetv1alpha1.AgentJoined))
			Expect(updatedJoinedCond.Status).Should(Equal(metav1.ConditionFalse))
			Expect(updatedJoinedCond.Reason).Should(Equal(eventReasonInternalMemberClusterLeft))
		})
	})
})
