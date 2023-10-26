/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package v1alpha1

import (
	"context"
	"embed"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/test/e2e/framework"
	testutils "go.goms.io/fleet/test/e2e/v1alpha1/utils"
)

var (
	hubClusterName    = "kind-hub-testing"
	memberClusterName = "kind-member-testing"
	HubCluster        = framework.NewCluster(hubClusterName, scheme)
	MemberCluster     = framework.NewCluster(memberClusterName, scheme)
	hubURL            string
	scheme            = runtime.NewScheme()
	mc                *v1alpha1.MemberCluster
	imc               *v1alpha1.InternalMemberCluster
	ctx               context.Context

	// The fleet-system namespace.
	fleetSystemNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fleet-system",
		},
	}

	// The kube-system namespace
	kubeSystemNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}

	// This namespace will store Member cluster-related CRs, such as v1alpha1.MemberCluster.
	memberNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("fleet-member-%s", MemberCluster.ClusterName),
		},
	}

	// This namespace in HubCluster will store v1alpha1.Work to simulate Work-related features in Hub Cluster.
	workNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("fleet-member-%s", MemberCluster.ClusterName),
		},
	}

	sortOption          = cmpopts.SortSlices(func(ref1, ref2 metav1.Condition) bool { return ref1.Type < ref2.Type })
	imcStatusCmpOptions = []cmp.Option{
		cmpopts.IgnoreTypes(v1alpha1.ResourceUsage{}),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		cmpopts.IgnoreFields(v1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
		sortOption,
	}
	mcStatusCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		cmpopts.IgnoreFields(v1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
		cmpopts.IgnoreFields(v1alpha1.ResourceUsage{}, "ObservationTime"),
		sortOption,
	}
	crpStatusCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime", "Message"),
		sortOption,
	}

	imcJoinedAgentStatus = []v1alpha1.AgentStatus{
		{
			Type: v1alpha1.MemberAgent,
			Conditions: []metav1.Condition{
				{
					Reason: "InternalMemberClusterHealthy",
					Status: metav1.ConditionTrue,
					Type:   string(v1alpha1.AgentHealthy),
				},
				{
					Reason: "InternalMemberClusterJoined",
					Status: metav1.ConditionTrue,
					Type:   string(v1alpha1.AgentJoined),
				},
			},
		},
	}
	imcLeftAgentStatus = []v1alpha1.AgentStatus{
		{
			Type: v1alpha1.MemberAgent,
			Conditions: []metav1.Condition{
				{
					Reason: "InternalMemberClusterHealthy",
					Status: metav1.ConditionTrue,
					Type:   string(v1alpha1.AgentHealthy),
				},
				{
					Reason: "InternalMemberClusterLeft",
					Status: metav1.ConditionFalse,
					Type:   string(v1alpha1.AgentJoined),
				},
			},
		},
	}

	mcJoinedConditions = []metav1.Condition{
		{
			Reason: "MemberClusterReadyToJoin",
			Status: metav1.ConditionTrue,
			Type:   string(v1alpha1.ConditionTypeMemberClusterReadyToJoin),
		},
		{
			Reason: "MemberClusterJoined",
			Status: metav1.ConditionTrue,
			Type:   string(v1alpha1.ConditionTypeMemberClusterJoined),
		},
	}

	mcLeftConditions = []metav1.Condition{
		{
			Reason: "MemberClusterNotReadyToJoin",
			Status: metav1.ConditionFalse,
			Type:   string(v1alpha1.ConditionTypeMemberClusterReadyToJoin),
		},
		{
			Reason: "MemberClusterLeft",
			Status: metav1.ConditionFalse,
			Type:   string(v1alpha1.ConditionTypeMemberClusterJoined),
		},
	}

	genericCodecs = serializer.NewCodecFactory(scheme)
	genericCodec  = genericCodecs.UniversalDeserializer()

	//go:embed manifests
	TestManifestFiles embed.FS
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(workv1alpha1.AddToScheme(scheme))
	utilruntime.Must(fleetnetworkingv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet e2e suite")
}

var _ = BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	Expect(kubeconfig).ShouldNot(BeEmpty(), "Failure to retrieve kubeconfig")
	hubURL = os.Getenv("HUB_SERVER_URL")
	Expect(hubURL).ShouldNot(BeEmpty(), "Failure to retrieve Hub URL")

	// hub setup
	HubCluster.HubURL = hubURL
	framework.GetClusterClient(HubCluster)
	// member setup
	MemberCluster.HubURL = hubURL
	framework.GetClusterClient(MemberCluster)

	ctx = context.Background()

	By("deploy member cluster in the hub cluster")
	identity := rbacv1.Subject{
		Name:      "hub-agent-sa",
		Kind:      "ServiceAccount",
		Namespace: utils.FleetSystemNamespace,
	}
	mc = &v1alpha1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: MemberCluster.ClusterName,
		},
		Spec: v1alpha1.MemberClusterSpec{
			Identity:               identity,
			State:                  v1alpha1.ClusterStateJoin,
			HeartbeatPeriodSeconds: 60,
		},
	}
	Eventually(func() error {
		return HubCluster.KubeClient.Create(ctx, mc)
	}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for member cluster %s to be created in %s cluster", mc.Name, HubCluster.ClusterName)

	By("check if internal member cluster created in the hub cluster")
	imc = &v1alpha1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MemberCluster.ClusterName,
			Namespace: memberNamespace.Name,
		},
	}
	Eventually(func() error {
		return HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)
	}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed(), "Failed to wait for internal member cluster %s to be synced in %s cluster", imc.Name, HubCluster.ClusterName)

	By("check if internal member cluster status is updated to Joined")
	wantIMCStatus := v1alpha1.InternalMemberClusterStatus{AgentStatus: imcJoinedAgentStatus}
	testutils.CheckInternalMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, wantIMCStatus, imcStatusCmpOptions)

	By("check if member cluster status is updated to Joined")
	Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)).Should(Succeed(), "Failed to retrieve internal member cluster %s in %s cluster", imc.Name, HubCluster.ClusterName)
	wantMCStatus := v1alpha1.MemberClusterStatus{
		AgentStatus:   imc.Status.AgentStatus,
		Conditions:    mcJoinedConditions,
		ResourceUsage: imc.Status.ResourceUsage,
	}
	testutils.CheckMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: mc.Name}, wantMCStatus, mcStatusCmpOptions)

	By("create resources for webhook e2e")
	testutils.CreateResourcesForWebHookE2E(ctx, HubCluster)
})

var _ = AfterSuite(func() {
	By("delete resources created for webhook e2e")
	testutils.DeleteResourcesForWebHookE2E(ctx, HubCluster)

	By("update member cluster in the hub cluster")
	Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mc.Name}, mc)).Should(Succeed(), "Failed to retrieve member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)
	mc.Spec.State = v1alpha1.ClusterStateLeave
	Expect(HubCluster.KubeClient.Update(ctx, mc)).Should(Succeed(), "Failed to update member cluster %s in %s cluster", mc.Name, HubCluster.ClusterName)

	By("check if internal member cluster status is updated to Left")
	wantIMCStatus := v1alpha1.InternalMemberClusterStatus{AgentStatus: imcLeftAgentStatus}
	testutils.CheckInternalMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, wantIMCStatus, imcStatusCmpOptions)

	By("check if member cluster status is updated to Left")
	Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace}, imc)).Should(Succeed(), "Failed to retrieve internal member cluster %s in %s cluster", imc.Name, HubCluster.ClusterName)
	wantMCStatus := v1alpha1.MemberClusterStatus{
		AgentStatus:   imc.Status.AgentStatus,
		Conditions:    mcLeftConditions,
		ResourceUsage: imc.Status.ResourceUsage,
	}
	testutils.CheckMemberClusterStatus(ctx, *HubCluster, &types.NamespacedName{Name: mc.Name}, wantMCStatus, mcStatusCmpOptions)

	By("delete member cluster")
	testutils.DeleteMemberCluster(ctx, *HubCluster, mc)
})
