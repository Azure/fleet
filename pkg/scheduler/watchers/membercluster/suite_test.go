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

package membercluster

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/queue"
	"go.goms.io/fleet/test/utils/keycollector"
)

var (
	hubTestEnv   *envtest.Environment
	hubClient    client.Client
	ctx          context.Context
	cancel       context.CancelFunc
	keyCollector *keycollector.SchedulerWorkqueueKeyCollector
)

var (
	defaultResourceSelectors = []placementv1beta1.ClusterResourceSelector{
		{
			Group:   "core",
			Kind:    "Namespace",
			Version: "v1",
			Name:    "work",
		},
	}
)

var (
	newMemberCluster = func(name string) *clusterv1beta1.MemberCluster {
		return &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
	}

	newCRP = func(name string, policy *placementv1beta1.PlacementPolicy) *placementv1beta1.ClusterResourcePlacement {
		return &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: defaultResourceSelectors,
				Policy:            policy,
			},
		}
	}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Scheduler MemberCluster Source Controller Suite")
}

// setupResources adds resources required for this test suite to the hub cluster.
func setupResources() {
	// Create a member cluster that has just joined the fleet.
	Expect(hubClient.Create(ctx, newMemberCluster(clusterName1))).Should(Succeed(), "Failed to create member cluster")

	// Create a CRP that has no placement policy specified.
	Expect(hubClient.Create(ctx, newCRP(crpName1, nil))).Should(Succeed(), "Failed to create CRP")
	// Create a CRP that is of the PickAll placement type.
	Expect(hubClient.Create(ctx, newCRP(crpName2, &placementv1beta1.PlacementPolicy{
		PlacementType: placementv1beta1.PickAllPlacementType,
	}))).Should(Succeed(), "Failed to create CRP")
	// Create a CRP that is of the PickFixed placement type and has not been fully scheduled.
	Expect(hubClient.Create(ctx, newCRP(crpName3, &placementv1beta1.PlacementPolicy{
		PlacementType: placementv1beta1.PickFixedPlacementType,
		ClusterNames:  []string{clusterName1},
	}))).Should(Succeed(), "Failed to create CRP")

	// Create a CRP that is of the PickFixed placement type and has been fully scheduled.
	crp := newCRP(crpName4, &placementv1beta1.PlacementPolicy{
		PlacementType: placementv1beta1.PickFixedPlacementType,
		ClusterNames:  []string{clusterName1},
	})
	Expect(hubClient.Create(ctx, crp)).Should(Succeed(), "Failed to create CRP")
	// Update the status.
	meta.SetStatusCondition(&crp.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: crp.Generation,
		Reason:             dummyReason,
	})
	Expect(hubClient.Status().Update(ctx, crp)).Should(Succeed(), "Failed to update CRP status")

	// Create a CRP that is of the PickN placement type and has been fully scheduled.
	crp = newCRP(crpName5, &placementv1beta1.PlacementPolicy{
		PlacementType:    placementv1beta1.PickNPlacementType,
		NumberOfClusters: &numOfClusters,
	})
	Expect(hubClient.Create(ctx, crp)).Should(Succeed(), "Failed to create CRP")
	// Update the status.
	meta.SetStatusCondition(&crp.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: crp.Generation,
		Reason:             dummyReason,
	})
	Expect(hubClient.Status().Update(ctx, crp)).Should(Succeed(), "Failed to update CRP status")

	// Create a CRP that is of the PickN placement type and has not been fully scheduled.
	Expect(hubClient.Create(ctx, newCRP(crpName6, &placementv1beta1.PlacementPolicy{
		PlacementType:    placementv1beta1.PickNPlacementType,
		NumberOfClusters: &numOfClusters,
	}))).Should(Succeed(), "Failed to create CRP")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrap the test environment")

	// Start the hub cluster.
	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	Expect(err).ToNot(HaveOccurred(), "Failed to start test environment")
	Expect(hubCfg).ToNot(BeNil(), "Hub cluster configuration is nil")

	// Add custom APIs to the runtime scheme.
	Expect(placementv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// Set up a client for the hub cluster.
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred(), "Failed to create hub cluster client")
	Expect(hubClient).ToNot(BeNil(), "Hub cluster client is nil")

	// Set up resources.
	setupResources()

	// Set up a controller manager and let it manage the member cluster controller.
	ctrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

	schedulerWorkQueue := queue.NewSimpleClusterResourcePlacementSchedulingQueue()

	reconciler := Reconciler{
		Client:                    hubClient,
		SchedulerWorkQueue:        schedulerWorkQueue,
		ClusterEligibilityChecker: clustereligibilitychecker.New(),
	}
	err = reconciler.SetupWithManager(ctrlMgr)
	Expect(err).ToNot(HaveOccurred(), "Failed to set up controller with controller manager")

	// Start the key collector.
	keyCollector = keycollector.NewSchedulerWorkqueueKeyCollector(schedulerWorkQueue)
	go func() {
		keyCollector.Run(ctx)
	}()

	// Start the controller manager.
	go func() {
		defer GinkgoRecover()
		err := ctrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "Failed to start controller manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(hubTestEnv.Stop()).Should(Succeed(), "Failed to stop test environment")
})
