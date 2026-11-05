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
	"fmt"
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
	utilcontroller "github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
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
		Expect(mgr.Start(ctx)).Should(Succeed(), "failed to run manager")
	}()

	// Waiting for the cache to sync is not enough: the watchers register their event handlers only when their
	// controllers start, which can be after the specs begin. A status update made before then reaches them as part
	// of a create event, which they ignore, so wait until both watchers act on a status update.
	By("waiting for the watchers to handle status updates")
	crb := clusterResourceBindingForTest()
	crb.Name = "probe-crb"
	waitForWatcher(crb, testPlacementName)
	rb := resourceBindingForTest()
	rb.Name = "probe-rb"
	waitForWatcher(rb, utilcontroller.GetObjectKeyFromNamespaceName(testNamespace, testPlacementName))
})

// waitForWatcher creates the binding and keeps updating its status until the placement controller receives
// wantKey, then deletes the binding and resets the queue.
func waitForWatcher(binding fleetv1beta1.BindingObj, wantKey string) {
	Expect(k8sClient.Create(ctx, binding)).Should(Succeed(), "failed to create the probe binding")
	attempt := 0
	Eventually(func() error {
		// Check before updating again, so that no update is left in flight once the key arrives.
		if fakePlacementController.Key() == wantKey {
			return nil
		}
		attempt++
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(binding), binding); err != nil {
			return err
		}
		binding.SetConditions(metav1.Condition{
			Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
			Status:             metav1.ConditionTrue,
			Reason:             fmt.Sprintf("Probe%d", attempt),
			ObservedGeneration: binding.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
		if err := k8sClient.Status().Update(ctx, binding); err != nil {
			return err
		}
		return fmt.Errorf("the watcher has not enqueued %s yet", wantKey)
	}, time.Second*30, time.Second).Should(Succeed(), "the watcher did not act on a status update for %s", wantKey)
	Expect(k8sClient.Delete(ctx, binding)).Should(Succeed(), "failed to delete the probe binding")
	fakePlacementController.ResetQueue()
}

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).Should(Succeed())
})
