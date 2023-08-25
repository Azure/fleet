/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package tests features a number of test suites that verify the behavior of the scheduler
// and its related components.
package tests

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	sched "go.goms.io/fleet/pkg/scheduler"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	fw "go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clusteraffinity"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clustereligibility"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/sameplacementaffinity"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/topologyspreadconstraints"
	"go.goms.io/fleet/pkg/scheduler/queue"
	crpwatcher "go.goms.io/fleet/pkg/scheduler/watchers/clusterresourceplacement"
	pswatcher "go.goms.io/fleet/pkg/scheduler/watchers/clusterschedulingpolicysnapshot"
	mcwatcher "go.goms.io/fleet/pkg/scheduler/watchers/membercluster"
)

const (
	defaultProfileName   = "defaultProfile"
	defaultSchedulerName = "defaultScheduler"

	customDeletionBlockerFinalizer = "custom-deletion-blocker"

	dummyReason = "dummyReason"

	eventuallyDuration   = time.Second * 5
	eventuallyInterval   = time.Millisecond * 500
	consistentlyDuration = time.Second * 1
	consistentlyInterval = time.Millisecond * 200

	memberCluster1  = "bravelion"
	memberCluster2  = "singingbutterfly"
	memberCluster3  = "smartfish"
	memberCluster4  = "dancingelephant"
	memberCluster5  = "jumpingfox"
	memberCluster6  = "vigilantpenguin"
	memberCluster7  = "sleepingbear"
	memberCluster8  = "runningwolf"
	memberCluster9  = "walkingeagle"
	memberCluster10 = "blueflamingo"

	regionLabel = "region"
	envLabel    = "env"
)

var (
	hubTestEnv *envtest.Environment
	hubClient  client.Client
	ctx        context.Context
	cancel     context.CancelFunc

	// The fleet environment simulated for testing features the following cluster topology:
	// * 10 member clusters in total
	//
	// * 7 clusters are in the healthy state (i.e., eligible for resource placement), incl.
	//     * bravelion, singingbutterfly, smartfish, dancingelephant, jumpingfox, vigilantpenguin, sleepingbear
	// * 3 clusters are not eligible for resource placement, incl.
	//     * runningwolf (unhealthy)
	//     * walkingeagle (left)
	//     * blueflamingo (not joined yet, i.e., non-existent)
	//
	// * 4 clusters are in the east region (with the label "region=east"), incl.
	//     * bravelion, singingbutterfly, smartfish, runningwolf
	// * 3 clusters are in the central region (with the label "region=central"), incl.
	//     * dancingelephant, jumpingfox, walkingeagle
	// * 2 clusters are in the west region (with the label "region=west"), incl.
	//     * vigilantpenguin, sleepingbear
	//
	// * 7 clusters are in the production environment (with the label "environment=prod"), incl.
	//     * bravelion, singingbutterfly, dancingelephant, jumpingfox, vigilantpenguin, runningwolf, walkingeagle
	// * 2 clusters are in the canary environment (with the label "environment=canary"), incl.
	//     * smartfish, sleepingbear
	allClusters = []string{
		memberCluster1, memberCluster2, memberCluster3, memberCluster4, memberCluster5,
		memberCluster6, memberCluster7, memberCluster8, memberCluster9,
	}

	labelsByCluster = map[string]map[string]string{
		memberCluster1: {
			regionLabel: "east",
			envLabel:    "prod",
		},
		memberCluster2: {
			regionLabel: "east",
			envLabel:    "prod",
		},
		memberCluster3: {
			regionLabel: "east",
			envLabel:    "canary",
		},
		memberCluster4: {
			regionLabel: "central",
			envLabel:    "prod",
		},
		memberCluster5: {
			regionLabel: "central",
			envLabel:    "prod",
		},
		memberCluster6: {
			regionLabel: "west",
			envLabel:    "prod",
		},
		memberCluster7: {
			regionLabel: "west",
			envLabel:    "canary",
		},
		memberCluster8: {
			regionLabel: "east",
			envLabel:    "prod",
		},
		memberCluster9: {
			regionLabel: "central",
			envLabel:    "prod",
		},
	}
)

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := placementv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Scheduler Test Suite")
}

func setupResources() {
	// Create all member cluster objects.
	for _, clusterName := range allClusters {
		memberCluster := clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterName,
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: clusterv1beta1.MemberClusterSpec{
				Identity: rbacv1.Subject{
					Kind:     "ServiceAccount",
					APIGroup: "",
					Name:     "admin",
				},
			},
		}
		Expect(hubClient.Create(ctx, &memberCluster)).To(Succeed(), "Failed to create member cluster")
	}

	// Mark all clusters as healthy.
	for _, clusterName := range allClusters {
		// Retrieve the cluster object.
		memberCluster := &clusterv1beta1.MemberCluster{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, memberCluster)).To(Succeed(), "Failed to get member cluster")

		// Add health check related information.
		memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
			{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.AgentJoined),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now()),
						Reason:             dummyReason,
					},
					{
						Type:               string(clusterv1beta1.AgentHealthy),
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now()),
						Reason:             dummyReason,
					},
				},
				LastReceivedHeartbeat: metav1.NewTime(time.Now()),
			},
		}
		Expect(hubClient.Status().Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")
	}

	// Mark cluster runningwolf as unhealthy (no recent heartbeats).
	memberCluster := &clusterv1beta1.MemberCluster{}
	Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberCluster8}, memberCluster)).To(Succeed(), "Failed to get member cluster")
	memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
		{
			Type: clusterv1beta1.MemberAgent,
			Conditions: []metav1.Condition{
				{
					Type:               string(clusterv1beta1.AgentJoined),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour * 25)),
					Reason:             dummyReason,
				},
				{
					Type:               string(clusterv1beta1.AgentHealthy),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour * 25)),
					Reason:             dummyReason,
				},
			},
			LastReceivedHeartbeat: metav1.NewTime(time.Now().Add(-time.Hour * 25)),
		},
	}
	Expect(hubClient.Status().Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")

	// Add labels to clusters.
	for clusterName, labels := range labelsByCluster {
		// Retrieve the cluster object.
		memberCluster := &clusterv1beta1.MemberCluster{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName}, memberCluster)).To(Succeed(), "Failed to get member cluster")

		// Add the region label.
		memberCluster.Labels = labels
		Expect(hubClient.Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster")
	}

	// Set cluster walkingeagle to leave by deleting the member cluster object.
	memberCluster = &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberCluster9,
		},
	}
	Expect(hubClient.Delete(ctx, memberCluster)).To(Succeed(), "Failed to delete member cluster")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping the test environment")

	// Start the hub cluster.
	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	Expect(err).ToNot(HaveOccurred(), "Failed to start test environment")
	Expect(hubCfg).ToNot(BeNil(), "Hub cluster configuration is nil")

	// Set up a client for the hub cluster..
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred(), "Failed to create hub cluster client")
	Expect(hubClient).ToNot(BeNil(), "Hub cluster client is nil")

	// Set up resources needed for the test suites.
	setupResources()

	// Set up a controller manager and let it manage the member cluster controller.
	ctrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

	// Spin up a scheduler work queue.
	schedulerWorkQueue := queue.NewSimpleClusterResourcePlacementSchedulingQueue()

	// Build a custom cluster eligibility checker.
	eligibilityChecker := clustereligibilitychecker.New(
		// Use a (much) larger health check and heartbeat check timeouts as in the test
		// environment there is no actual agent to report health check status and send heartbeat
		// signals.
		clustereligibilitychecker.WithClusterHealthCheckTimeout(time.Hour*24),
		clustereligibilitychecker.WithClusterHeartbeatCheckTimeout(time.Hour*24),
	)

	// Register the watchers.
	crpReconciler := crpwatcher.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = crpReconciler.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up CRP watcher with controller manager")

	policySnapshotWatcher := pswatcher.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = policySnapshotWatcher.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up policy snapshot watcher with controller manager")

	memberClusterWatcher := mcwatcher.Reconciler{
		Client:                    hubClient,
		SchedulerWorkQueue:        schedulerWorkQueue,
		ClusterEligibilityChecker: eligibilityChecker,
	}
	err = memberClusterWatcher.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up member cluster watcher with controller manager")

	// Set up the scheduler.

	// Create a new profile.
	profile := fw.NewProfile(defaultProfileName)

	// Register the plugins.
	clusterAffinityPlugin := clusteraffinity.New()
	clustereligibilityPlugin := clustereligibility.New()
	samePlacementAffinityPlugin := sameplacementaffinity.New()
	topologyspreadconstraintsPlugin := topologyspreadconstraints.New()
	profile.
		// Register cluster affinity plugin.
		WithPreFilterPlugin(&clusterAffinityPlugin).
		WithFilterPlugin(&clusterAffinityPlugin).
		WithPreScorePlugin(&clusterAffinityPlugin).
		WithScorePlugin(&clusterAffinityPlugin).
		// Register cluster eligibility plugin.
		WithFilterPlugin(&clustereligibilityPlugin).
		// Register same placement affinity plugin.
		WithFilterPlugin(&samePlacementAffinityPlugin).
		WithScorePlugin(&samePlacementAffinityPlugin).
		// Register topology spread constraints plugin.
		WithPostBatchPlugin(&topologyspreadconstraintsPlugin).
		WithPreFilterPlugin(&topologyspreadconstraintsPlugin).
		WithFilterPlugin(&topologyspreadconstraintsPlugin).
		WithPreScorePlugin(&topologyspreadconstraintsPlugin).
		WithScorePlugin(&topologyspreadconstraintsPlugin)

	// Create a scheduler framework.
	framework := fw.NewFramework(profile, ctrlMgr, fw.WithClusterEligibilityChecker(eligibilityChecker))

	// Create a scheduler.
	scheduler := sched.NewScheduler(defaultSchedulerName, framework, schedulerWorkQueue, ctrlMgr)

	// Run the controller manager.
	go func() {
		defer GinkgoRecover()
		err := ctrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "Failed to start controller manager")
	}()

	// Run the scheduler.
	go func() {
		scheduler.Run(ctx)
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(hubTestEnv.Stop()).Should(Succeed(), "Failed to stop test environment")
})
