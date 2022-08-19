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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var _ = Describe("Join/leave member cluster testing", func() {
	var mc *v1alpha1.MemberCluster
	var sa *corev1.ServiceAccount
	var memberNS *corev1.Namespace
	var imc *v1alpha1.InternalMemberCluster

	BeforeEach(func() {
		memberNS = testutils.NewNamespace(fmt.Sprintf(utils.NamespaceNameFormat, MemberCluster.ClusterName))
		By("prepare resources in member cluster")
		// create testing NS in member cluster
		testutils.CreateNamespace(*MemberCluster, memberNS)
		sa = testutils.NewServiceAccount(MemberCluster.ClusterName, memberNS.Name)
		testutils.CreateServiceAccount(*MemberCluster, sa)

		By("deploy member cluster in the hub cluster")
		mc = testutils.NewMemberCluster(MemberCluster.ClusterName, 60, v1alpha1.ClusterStateJoin)
		testutils.CreateMemberCluster(*HubCluster, mc)

		By("check if internal member cluster created in the hub cluster")
		imc = testutils.NewInternalMemberCluster(MemberCluster.ClusterName, memberNS.Name)
		testutils.WaitInternalMemberCluster(*HubCluster, imc)

		By("check if member cluster is marked as readyToJoin")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterReadyToJoin, v1.ConditionTrue, 3*testutils.PollTimeout)
	})

	AfterEach(func() {
		testutils.DeleteNamespace(*MemberCluster, memberNS)
		Eventually(func() bool {
			err := MemberCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
			return apierrors.IsNotFound(err)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Equal(true))
		testutils.DeleteMemberCluster(*HubCluster, mc)
		Eventually(func() bool {
			err := HubCluster.KubeClient.Get(context.TODO(), types.NamespacedName{Name: memberNS.Name, Namespace: ""}, memberNS)
			return apierrors.IsNotFound(err)
		}, testutils.PollTimeout, testutils.PollInterval).Should(Equal(true))
	})

	It("Join & Leave flow is successful ", func() {
		By("check if internal member cluster condition is updated to Joined")
		testutils.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionTrue, 3*testutils.PollTimeout)

		By("check if member cluster condition is updated to Joined")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionTrue, 3*testutils.PollTimeout)

		By("update member cluster in the hub cluster")
		testutils.UpdateMemberClusterState(*HubCluster, mc, v1alpha1.ClusterStateLeave)

		By("check if internal member cluster condition is updated to Left")
		testutils.WaitConditionInternalMemberCluster(*HubCluster, imc, v1alpha1.AgentJoined, v1.ConditionFalse, 3*testutils.PollTimeout)

		By("check if member cluster is marked as notReadyToJoin")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterReadyToJoin, v1.ConditionFalse, 3*testutils.PollTimeout)

		By("check if member cluster condition is updated to Left")
		testutils.WaitConditionMemberCluster(*HubCluster, mc, v1alpha1.ConditionTypeMemberClusterJoin, v1.ConditionFalse, 3*testutils.PollTimeout)
	})
})
