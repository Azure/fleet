/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package tests features a number of test suites that verify the behavior of the scheduler
// and its related components.
package tests

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
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
	"go.goms.io/fleet/pkg/scheduler"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/pkg/scheduler/watchers/clusterresourceplacement"
	"go.goms.io/fleet/pkg/scheduler/watchers/clusterschedulingpolicysnapshot"
	"go.goms.io/fleet/pkg/scheduler/watchers/membercluster"
)

const (
	defaultProfileName   = "defaultProfile"
	defaultSchedulerName = "defaultScheduler"

	customDeletionBlockerFinalizer = "custom-deletion-blocker"

	dummyReason = "dummyReason"

	eventuallyDuration   = time.Second * 30
	eventuallyInterval   = time.Second * 2
	consistentlyDuration = time.Second * 1
	consistentlyInterval = time.Millisecond * 200

	memberCluster1EastProd          = "cluster-1-east-prod"
	memberCluster2EastProd          = "cluster-2-east-prod"
	memberCluster3EastCanary        = "cluster-3-east-canary"
	memberCluster4CentralProd       = "cluster-4-central-prod"
	memberCluster5CentralProd       = "cluster-5-central-prod"
	memberCluster6WestProd          = "cluster-6-west-prod"
	memberCluster7WestCanary        = "cluster-7-west-canary"
	memberCluster8UnhealthyEastProd = "unhealthy-cluster-east-prod"
	memberCluster9LeftCentralProd   = "left-cluster-central-prod"
	memberCluster10NonExistent      = "non-existent-cluster"

	regionLabel = "region"
	envLabel    = "env"
)

var (
	// Note that the hubTestEnv variable is only available in the first Ginkgo process, when
	// the suite is running in parallel; in other processes, the variable remains nil.
	hubTestEnv *envtest.Environment
	hubClient  client.Client
	ctx        context.Context
	cancel     context.CancelFunc

	// The fleet environment simulated for testing features the following cluster topology:
	// * 10 member clusters in total
	//
	// * 7 clusters are in the healthy state (i.e., eligible for resource placement), incl.
	//     * cluster 1-7
	// * 3 clusters are ineligible for resource placement, incl.
	//     * cluster 8 (becomes unhealthy)
	//     * cluster 9 (has left the fleet)
	//     * cluster 10 (not joined yet, i.e., non-existent)
	//
	// * 4 clusters are in the east region (with the label "region=east"), incl.
	//     * cluster 1, 3, 8
	// * 3 clusters are in the central region (with the label "region=central"), incl.
	//     * member 4, 5, 9
	// * 2 clusters are in the west region (with the label "region=west"), incl.
	//     * member 6, 7
	//
	// * 7 clusters are in the production environment (with the label "environment=prod"), incl.
	//     * clusters other than cluster 3, 7, and 10
	// * 2 clusters are in the canary environment (with the label "environment=canary"), incl.
	//     * cluster 3 and 7
	allClusters = []string{
		memberCluster1EastProd, memberCluster2EastProd, memberCluster3EastCanary, memberCluster4CentralProd, memberCluster5CentralProd,
		memberCluster6WestProd, memberCluster7WestCanary, memberCluster8UnhealthyEastProd, memberCluster9LeftCentralProd,
	}
	healthyClusters = []string{
		memberCluster1EastProd, memberCluster2EastProd, memberCluster3EastCanary,
		memberCluster4CentralProd, memberCluster5CentralProd, memberCluster6WestProd,
		memberCluster7WestCanary,
	}
	unhealthyClusters = []string{
		memberCluster8UnhealthyEastProd,
		memberCluster9LeftCentralProd,
	}

	labelsByCluster = map[string]map[string]string{
		memberCluster1EastProd: {
			regionLabel: "east",
			envLabel:    "prod",
		},
		memberCluster2EastProd: {
			regionLabel: "east",
			envLabel:    "prod",
		},
		memberCluster3EastCanary: {
			regionLabel: "east",
			envLabel:    "canary",
		},
		memberCluster4CentralProd: {
			regionLabel: "central",
			envLabel:    "prod",
		},
		memberCluster5CentralProd: {
			regionLabel: "central",
			envLabel:    "prod",
		},
		memberCluster6WestProd: {
			regionLabel: "west",
			envLabel:    "prod",
		},
		memberCluster7WestCanary: {
			regionLabel: "west",
			envLabel:    "canary",
		},
		memberCluster8UnhealthyEastProd: {
			regionLabel: "east",
			envLabel:    "prod",
		},
		memberCluster9LeftCentralProd: {
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

	// Mark cluster 8 as unhealthy (no recent heartbeats).
	memberCluster := &clusterv1beta1.MemberCluster{}
	Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberCluster8UnhealthyEastProd}, memberCluster)).To(Succeed(), "Failed to get member cluster")
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

	// Set cluster 9 to leave by deleting the member cluster object.
	memberCluster = &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberCluster9LeftCentralProd,
		},
	}
	Expect(hubClient.Delete(ctx, memberCluster)).To(Succeed(), "Failed to delete member cluster")
}

func beforeSuiteForProcess1() []byte {
	ctx, cancel = context.WithCancel(context.TODO())

	klog.InitFlags(nil)
	Expect(flag.Set("v", "5")).To(Succeed(), "Failed to set verbosity flag")
	flag.Parse()
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5))))

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
	clusterEligibilityChecker := clustereligibilitychecker.New(
		// Use a (much) larger health check and heartbeat check timeouts as in the test
		// environment there is no actual agent to report health check status and send heartbeat
		// signals.
		clustereligibilitychecker.WithClusterHealthCheckTimeout(time.Hour*24),
		clustereligibilitychecker.WithClusterHeartbeatCheckTimeout(time.Hour*24),
	)

	// Register the watchers.
	crpReconciler := clusterresourceplacement.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = crpReconciler.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up CRP watcher with controller manager")

	policySnapshotWatcher := clusterschedulingpolicysnapshot.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = policySnapshotWatcher.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up policy snapshot watcher with controller manager")

	memberClusterWatcher := membercluster.Reconciler{
		Client:                    hubClient,
		SchedulerWorkQueue:        schedulerWorkQueue,
		ClusterEligibilityChecker: clusterEligibilityChecker,
	}
	err = memberClusterWatcher.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up member cluster watcher with controller manager")

	// Set up the scheduler.
	fw := buildSchedulerFramework(ctrlMgr, clusterEligibilityChecker)
	sched := scheduler.NewScheduler(defaultSchedulerName, fw, schedulerWorkQueue, ctrlMgr)

	// Run the controller manager.
	go func() {
		defer GinkgoRecover()
		err := ctrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "Failed to start controller manager")
	}()

	// Run the scheduler.
	go func() {
		sched.Run(ctx)
	}()

	// Pass the hub config to other processes.
	return buildK8sAPIConfigFrom(hubCfg)
}

func beforeSuiteForAllProcesses(hubCfgBytes []byte) {
	if ctx == nil {
		// Set the context only if it has not been set yet, i.e., the code runs in
		// Ginkgo processes other than the first one; otherwise the context set
		// before would be overwritten and cannot get cancelled at the end of the suite.
		ctx, cancel = context.WithCancel(context.Background())

		// Set the log verbosity level.
		//
		// Note that settings specified here do not apply to the controller logs, but to the
		// tests only.
		klog.InitFlags(nil)
		Expect(flag.Set("v", "5")).To(Succeed(), "Failed to set verbosity flag")
		flag.Parse()
		klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5))))
	}

	hubCfg := loadRestConfigFrom(hubCfgBytes)

	// Set up a client for the hub cluster.
	var err error
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred(), "Failed to create hub cluster client")
	Expect(hubClient).ToNot(BeNil(), "Hub cluster client is nil")
}

var _ = SynchronizedBeforeSuite(beforeSuiteForProcess1, beforeSuiteForAllProcesses)

func afterSuiteForProcess1() {
	By("tearing down the test environment")
	Expect(hubTestEnv.Stop()).Should(Succeed(), "Failed to stop test environment")
}

func afterSuiteForAllProcesses() {
	defer klog.Flush()
	cancel()
}

var _ = SynchronizedAfterSuite(afterSuiteForAllProcesses, afterSuiteForProcess1)
