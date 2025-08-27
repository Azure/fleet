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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/clustereligibilitychecker"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/binding"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/membercluster"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/placement"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/watchers/schedulingpolicysnapshot"
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

	// energyEfficiencyRatingPropertyName is the name of a fictional property that is used for testing
	// purposes only.
	energyEfficiencyRatingPropertyName = "test.only/energy-efficiency-rating"
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

	// The resource and non-resource properties associated with each cluster. In the context
	// of this test suite, all properties are set manually.
	//
	// The properties for each cluster are:
	// * cluster 1:
	//   * non-resource properties:
	//	   2 nodes, 100 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 4/3/2, Memory: 4/3/2 Gi
	// * cluster 2:
	//   * non-resource properties:
	//     4 nodes, 60 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 8/7/4, Memory: 8/7/4 Gi
	// * cluster 3:
	//   * non-resource properties:
	//     6 nodes, 40 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 12/11/6, Memory: 12/11/6 Gi
	// * cluster 4:
	//   * non-resource properties:
	//     2 nodes, 80 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 8/6/2, Memory: 4/2/1 Gi
	// * cluster 5:
	//   * non-resource properties:
	//     8 nodes, 40 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 32/30/8, Memory: 16/14/4 Gi
	// * cluster 6:
	//   * non-resource properties:
	//     2 nodes, 60 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 4/2/1, Memory: 8/6/4 Gi
	// * cluster 7:
	//   * non-resource properties:
	//     8 nodes, 20 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 16/14/4, Memory: 32/30/16 Gi
	// * cluster 8 (unhealthy):
	//   * non-resource properties:
	//     1 node, 100 energy-efficiency rating
	//   * resource properties (total/allocatable/available capacity):
	//     CPU cores: 8/6/0, Memory: 8/6/0 Gi
	propertiesByCluster = map[string]clusterv1beta1.MemberClusterStatus{
		memberCluster1EastProd: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "2",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "100",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		},
		memberCluster2EastProd: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "4",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "60",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("7Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
		memberCluster3EastCanary: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "6",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "40",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("11"),
					corev1.ResourceMemory: resource.MustParse("11Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
				},
			},
		},
		memberCluster4CentralProd: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "2",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "80",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
		memberCluster5CentralProd: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "8",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "40",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("32"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30"),
					corev1.ResourceMemory: resource.MustParse("14Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
		memberCluster6WestProd: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "2",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "60",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		},
		memberCluster7WestCanary: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "8",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "20",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("14"),
					corev1.ResourceMemory: resource.MustParse("30Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
			},
		},
		memberCluster8UnhealthyEastProd: {
			Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: "1",
				},
				energyEfficiencyRatingPropertyName: {
					Value: "100",
				},
			},
			ResourceUsage: clusterv1beta1.ResourceUsage{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
			},
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

	// Add properties to the clusters.
	for clusterName := range propertiesByCluster {
		resetClusterPropertiesFor(clusterName)
	}

	// Create test namespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(hubClient.Create(ctx, namespace)).Should(Succeed())
}

func beforeSuiteForProcess1() []byte {
	ctx, cancel = context.WithCancel(context.TODO())

	klog.InitFlags(nil)
	Expect(flag.Set("v", "5")).To(Succeed(), "Failed to set verbosity flag")
	flag.Parse()
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.Level(-5)))
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)
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
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

	// Spin up a scheduler work queue.
	schedulerWorkQueue := queue.NewSimplePlacementSchedulingQueue()

	// Build a custom cluster eligibility checker.
	clusterEligibilityChecker := clustereligibilitychecker.New(
		// Use a (much) larger health check and heartbeat check timeouts as in the test
		// environment there is no actual agent to report health check status and send heartbeat
		// signals.
		clustereligibilitychecker.WithClusterHealthCheckTimeout(time.Hour*24),
		clustereligibilitychecker.WithClusterHeartbeatCheckTimeout(time.Hour*24),
	)

	// Register the watchers.
	crpReconciler := placement.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = crpReconciler.SetupWithManagerForClusterResourcePlacement(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up CRP watcher with controller manager")

	rpReconciler := placement.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = rpReconciler.SetupWithManagerForResourcePlacement(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up RP watcher with controller manager")

	clusterPolicySnapshotWatcher := schedulingpolicysnapshot.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = clusterPolicySnapshotWatcher.SetupWithManagerForClusterSchedulingPolicySnapshot(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up cluster policy snapshot watcher with controller manager")

	policySnapshotWatcher := schedulingpolicysnapshot.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = policySnapshotWatcher.SetupWithManagerForSchedulingPolicySnapshot(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up policy snapshot watcher with controller manager")

	memberClusterWatcher := membercluster.Reconciler{
		Client:                    hubClient,
		SchedulerWorkQueue:        schedulerWorkQueue,
		ClusterEligibilityChecker: clusterEligibilityChecker,
		EnableResourcePlacement:   true,
	}
	err = memberClusterWatcher.SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up member cluster watcher with controller manager")

	clusterResourceBindingWatcher := binding.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = clusterResourceBindingWatcher.SetupWithManagerForClusterResourceBinding(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up cluster resource binding watcher with controller manager")

	resourceBindingWatcher := binding.Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
	}
	err = resourceBindingWatcher.SetupWithManagerForResourceBinding(ctrlMgr)
	Expect(err).NotTo(HaveOccurred(), "Failed to set up resource binding watcher with controller manager")

	// Set up the scheduler.
	fw := buildSchedulerFramework(ctrlMgr, clusterEligibilityChecker)
	sched := scheduler.NewScheduler(defaultSchedulerName, fw, schedulerWorkQueue, ctrlMgr, 3)

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
