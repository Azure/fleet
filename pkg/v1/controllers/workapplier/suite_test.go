/*
Copyright 2026 The KubeFleet Authors.

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

package workapplier

import (
	"context"
	"flag"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
	testv1alpha1 "github.com/kubefleet-dev/kubefleet/test/apis/v1alpha1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
var (
	hubCfg    *rest.Config
	hubEnv    *envtest.Environment
	hubClient client.Client

	memberCfg1           *rest.Config
	memberEnv1           *envtest.Environment
	hubMgr1              manager.Manager
	memberClient1        client.Client
	memberDynamicClient1 dynamic.Interface
	workApplier1         *Reconciler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

const (
	// The number of max. concurrent reconciliations for the work applier controller.
	maxConcurrentReconciles = 5
	// The count of workers for the work applier controller.
	workerCount = 4

	// The requeue delay when the work applier waits for the cleanup (appliedWork deletion) to complete.
	cleanupRequeueAfter = time.Second * 2
	// The requeue delay for the periodic re-processing of work objects.
	periodicRequeueAfter = time.Second * 3
	// The max. time the work applier waits for an appliedWork object to be deleted before it unblocks
	// the deletion manually.
	//
	// Note that envtest runs no GC controller; foreground deletions of appliedWork objects never complete
	// in the test environments, and the work applier will always wait for 2 * cleanupWaitTime before
	// it removes the cleanup finalizers. A short wait time is used here to keep the tests fast.
	cleanupWaitTime = time.Second * 5

	memberReservedNSName1 = "fleet-member-experimental-1"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Work Applier Integration Test Suite")
}

func setupResources() {
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberReservedNSName1,
		},
	}
	Expect(hubClient.Create(ctx, ns1)).To(Succeed())
}

// Note: each Ginkgo process must do the same setup; unlike our E2E tests, the integration
// tests uses in-memory testing environments, and as a result cannot be shared across processes.
var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)

	By("Bootstrapping test environments")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../../", "config", "crd", "bases"),
			filepath.Join("../../../../", "test", "manifests"),
		},
		ErrorIfCRDPathMissing: true,
	}
	// memberEnv1 is the test environment for verifying most work applier behaviors.
	memberEnv1 = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../../", "config", "crd", "bases"),
			filepath.Join("../../../../", "test", "manifests"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(hubCfg).ToNot(BeNil())

	memberCfg1, err = memberEnv1.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(memberCfg1).ToNot(BeNil())

	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(testv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("Building the K8s clients")
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(hubClient).ToNot(BeNil())

	memberClient1, err = client.New(memberCfg1, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(memberClient1).ToNot(BeNil())

	memberDynamicClient1, err = dynamic.NewForConfig(memberCfg1)
	Expect(err).ToNot(HaveOccurred())

	By("Setting up the resources")
	setupResources()

	By("Setting up the controller and the controller manager for member cluster 1")
	hubMgr1, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				memberReservedNSName1: {},
			},
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).ToNot(HaveOccurred())

	// The work applier lists work objects via custom field indexes; the indexes must be set up
	// (with the manager's cache) before the manager starts.
	Expect(fieldindexers.SetupWithMemberAgentControllerManager(ctx, hubMgr1)).To(Succeed())

	workApplier1 = New(
		memberReservedNSName1,
		// Use the cached client from the manager, as the work applier relies on the field indexes.
		hubMgr1.GetClient(),
		hubMgr1.GetAPIReader(),
		memberClient1,
		memberDynamicClient1,
		memberClient1.RESTMapper(),
		parallelizer.NewParallelizer(workerCount),
		maxConcurrentReconciles,
		cleanupRequeueAfter,
		periodicRequeueAfter,
		cleanupWaitTime,
	)
	Expect(workApplier1.SetupWithManager(hubMgr1)).To(Succeed())

	wg = sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer GinkgoRecover()
		defer wg.Done()
		Expect(workApplier1.Join()).To(Succeed())
		Expect(hubMgr1.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	wg.Wait()
	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).To(Succeed())
	Expect(memberEnv1.Stop()).To(Succeed())
})
