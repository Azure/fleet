/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Join/leave member cluster testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberNsName string
	var imc *v1alpha1.InternalMemberCluster

	BeforeEach(func() {
		memberNsName = fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName)
		sa = testutils.NewServiceAccount(MemberCluster.ClusterName, memberNsName)
		testutils.CreateServiceAccount(*MemberCluster, sa)

		By("deploy member cluster in the hub cluster")
		mc = testutils.NewMemberCluster(MemberCluster.ClusterName, 60, v1alpha1.ClusterStateJoin)
		testutils.CreateMemberCluster(*HubCluster, mc)

		By("check if internal member cluster created in the hub cluster")
		imc = testutils.NewInternalMemberCluster(MemberCluster.ClusterName, memberNsName)
		testutils.WaitInternalMemberCluster(*HubCluster, imc)

		By("check if member cluster is marked as readyToJoin")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterReadyToJoin, v1.ConditionTrue, 3*testutils.PollTimeout)
	})

	AfterEach(func() {
		testutils.DeleteServiceAccount(*MemberCluster, sa)
		testutils.DeleteMemberCluster(*HubCluster, mc)
	})

	It("Join & Leave flow is successful ", func() {
		By("check if internal member cluster condition is updated to Joined")
		testutils.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*testutils.PollTimeout)

		By("check if member cluster condition is updated to Joined")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoined, v1.ConditionTrue, 3*testutils.PollTimeout)

		By("update member cluster in the hub cluster")
		testutils.UpdateMemberClusterState(*HubCluster, mc, v1alpha1.ClusterStateLeave)

		By("check if internal member cluster condition is updated to Left")
		testutils.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionFalse, 3*testutils.PollTimeout)

		By("check if member cluster is marked as notReadyToJoin")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterReadyToJoin, v1.ConditionFalse, 3*testutils.PollTimeout)

		By("check if member cluster condition is updated to Left")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoined, v1.ConditionFalse, 3*testutils.PollTimeout)
	})
})
