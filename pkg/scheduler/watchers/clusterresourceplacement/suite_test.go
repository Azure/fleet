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

package clusterresourceplacement

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Scheduler Source Cluster Resource Placement Controller Suite")
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
	Expect(fleetv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// Set up a client for the hub cluster..
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred(), "Failed to create hub cluster client")
	Expect(hubClient).ToNot(BeNil(), "Hub cluster client is nil")

	// Set up a controller manager and let it manage the member cluster controller.
	ctrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create controller manager")

	schedulerWorkQueue := queue.NewSimpleClusterResourcePlacementSchedulingQueue()

	reconciler := &Reconciler{
		Client:             hubClient,
		SchedulerWorkQueue: schedulerWorkQueue,
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
