/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.goms.io/fleet/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = Describe("Join/leave member cluster testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster

	Context("member clusters don't share member identity", func() {
		BeforeEach(func() {
			memberNS = NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))
			By("prepare resources in member cluster")
			// create testing NS in member cluster
			framework.CreateNamespace(*MemberCluster, memberNS)
			framework.WaitNamespace(*MemberCluster, memberNS)
			sa = NewServiceAccount(MemberCluster.ClusterName, memberNS.Name)
			framework.CreateServiceAccount(*MemberCluster, sa)

			By("deploy member cluster in the hub cluster")
			mc = NewMemberCluster(MemberCluster.ClusterName, 60, v1alpha1.ClusterStateJoin)
			framework.CreateMemberCluster(*HubCluster, mc)
			framework.WaitMemberCluster(*HubCluster, mc)

			By("check if member cluster is marked as readyToJoin")
			framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterReadyToJoin, v1.ConditionTrue, 3*framework.PollTimeout)

			By("check if internal member cluster created in the hub cluster")
			imc = NewInternalMemberCluster(MemberCluster.ClusterName, memberNS.Name)
			framework.WaitInternalMemberCluster(*HubCluster, imc)
		})

		It("Join flow is successful ", func() {
			By("check if internal member cluster condition is updated to Joined")
			framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*framework.PollTimeout)

			By("check if member cluster condition is updated to Joined")
			framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)
		})

		It("leave flow is successful ", func() {
			By("check if member cluster condition is updated to Joined")
			framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*framework.PollTimeout)

			By("update member cluster in the hub cluster")
			framework.UpdateMemberClusterState(*HubCluster, mc, v1alpha1.ClusterStateLeave)
			framework.WaitMemberCluster(*HubCluster, mc)

			By("check if internal member cluster condition is updated to Left")
			framework.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionFalse, 3*framework.PollTimeout)

			By("check if member cluster is marked as notReadyToJoin")
			framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterReadyToJoin, v1.ConditionFalse, 3*framework.PollTimeout)

			By("check if member cluster condition is updated to Left")
			framework.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 3*framework.PollTimeout)
		})

		AfterEach(func() {
			framework.DeleteNamespace(*MemberCluster, memberNS)
			Eventually(func() bool {
				err := MemberCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
				return apierrors.IsNotFound(err)
			}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
			framework.DeleteMemberCluster(*HubCluster, mc)
			Eventually(func() bool {
				err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
				return apierrors.IsNotFound(err)
			}, framework.PollTimeout, framework.PollInterval).Should(Equal(true))
		})
	})
})
