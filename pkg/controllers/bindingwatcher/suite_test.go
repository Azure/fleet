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
package bindingwatcher

import (
	"context"
	"flag"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/test/utils/controller"
)

var (
	cfg                     *rest.Config
	mgr                     manager.Manager
	k8sClient               client.Client
	testEnv                 *envtest.Environment
	ctx                     context.Context
	cancel                  context.CancelFunc
	fakePlacementController *controller.FakeController
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Binding Watcher Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).Should(Succeed())
	Expect(cfg).NotTo(BeNil())

	err = fleetv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("construct the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).Should(Succeed())
	Expect(k8sClient).NotTo(BeNil())

	By("creating a test namespace")
	var ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, &ns)).Should(Succeed(), "failed to create namespace")

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).Should(Succeed())

	fakePlacementController = &controller.FakeController{}

	err = (&Reconciler{
		Client:              mgr.GetClient(),
		PlacementController: fakePlacementController,
	}).SetupWithManagerForClusterResourceBinding(mgr)
	Expect(err).Should(Succeed())

	err = (&Reconciler{
		Client:              mgr.GetClient(),
		PlacementController: fakePlacementController,
	}).SetupWithManagerForResourceBinding(mgr)
	Expect(err).Should(Succeed())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).Should(Succeed(), "failed to run manager")
	}()

	// Note (chenyu1): for the binding watcher integration tests, must wait for the cache to sync
	// before moving onto the test stage, otherwise some Update events might not be caught properly.
	//
	// This is wrapped in an Eventually block as the manager might not have started yet.
	Eventually(func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}, time.Second*10, time.Second*2).To(BeTrue(), "failed to wait for cache to sync")
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).Should(Succeed())
})
