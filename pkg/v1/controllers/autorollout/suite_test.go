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

package autorollout

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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resourceeligibility"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	// The namespace in which namespace-scoped objects (e.g., PlacementPolicy and PlacementBinding objects, and the
	// ConfigMap objects that placement policies select) are created.
	appNamespaceName = "app"
	// Another namespace, for verifying that namespace-scoped objects are not looked up across namespaces.
	//
	// Note that the namespaces are created once per (Ginkgo) process, as the envtest environment does not
	// support namespace deletion.
	otherAppNamespaceName = "other-app"

	// The number of max. concurrent reconciliations for the auto-rollout controller.
	maxConcurrentReconciles = 4
	// The number of workers in the parallelizer that the auto-rollout controller uses to refresh placement bindings
	// in parallel.
	workerCount = 4

	// The number of mutex slots used by the placement resource snapshot manager.
	placementResourceSnapshotMuSlotCnt = 256
	// The max. size (in bytes) of the resource data in a single placement resource snapshot.
	//
	// The limit is far lower than the default (~1.2 MiB), so that test cases can select a resource too large to
	// snapshot (a user error) without writing megabytes of data; the resources that test cases select otherwise are
	// a few hundred bytes each.
	maxPerSnapshotResourceDataSizeBytes = 16 * 1024

	// The base and max. delays of the rate limiter for binding manager claims used by the auto-rollout controller.
	//
	// The defaults (5 seconds and 180 seconds respectively) are too long for the integration tests;
	// shorter delays are used here to keep the tests fast.
	rateLimiterBaseDelay = time.Millisecond * 250
	rateLimiterMaxDelay  = time.Second * 2
)

var (
	hubCfg *rest.Config
	hubEnv *envtest.Environment
	hubMgr manager.Manager
	// hubClient is the client from the controller manager; reads are served from its cache, which
	// matches how the auto-rollout controller reads objects.
	hubClient client.Client
	// hubAPIReader reads objects from the API server directly (i.e., bypassing the cache); test cases use it only
	// when a read must reflect a write that the test case has just made (e.g., to tell that the controller has
	// undone the write, rather than that the cache has yet to observe it).
	hubAPIReader client.Reader

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Auto-Rollout Controller Integration Test Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)

	By("Bootstrapping the test environment")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).Should(Succeed())
	Expect(hubCfg).NotTo(BeNil())

	By("Setting up the scheme")
	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	By("Setting up the controller manager")
	hubMgr, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).Should(Succeed())

	// Use the same (cache-backed) client as the controller, so that test cases read objects the same way the
	// controller does; a test case that has waited for the cache to observe a change can be sure that the controller
	// observes it too.
	By("Setting the hub client to the one used by the controller manager")
	hubClient = hubMgr.GetClient()
	hubAPIReader = hubMgr.GetAPIReader()

	// The auto-rollout controller (and the placement resource snapshot manager) list objects via custom field
	// indexes; the indexes must be set up before the manager starts.
	By("Setting up the field indexes")
	Expect(fieldindexers.SetupWithHubAgentControllerManager(ctx, hubMgr)).Should(Succeed())

	By("Setting up the placement resource snapshot manager")
	hubDynamicClient, err := dynamic.NewForConfig(hubCfg)
	Expect(err).Should(Succeed())
	// No resource change detector runs in the test environment, so no informers are registered with the
	// dynamic informer manager; the placement resource snapshot manager reads selected resources directly
	// from the API server in this case.
	hubDynamicInformerManager := informer.NewInformerManager(hubDynamicClient, 0, ctx.Done())
	placementResourceSnapshotManager, err := placementresourcesnapshot.New(
		hubMgr,
		hubDynamicClient,
		hubDynamicInformerManager,
		resourceeligibility.Baseline(hubMgr.GetRESTMapper()),
		hubMgr.GetRESTMapper(),
		placementResourceSnapshotMuSlotCnt,
		maxPerSnapshotResourceDataSizeBytes,
		placementresourcesnapshot.DefaultMaxPerSnapshotResourceCnt,
	)
	Expect(err).Should(Succeed())
	// Add the placement resource snapshot manager to the controller manager so that its snapshot GC runs.
	Expect(hubMgr.Add(placementResourceSnapshotManager)).Should(Succeed())

	By("Setting up the auto-rollout controller")
	reconciler, err := New(
		hubMgr.GetClient(),
		placementResourceSnapshotManager,
		parallelizer.NewParallelizer(workerCount),
		workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](rateLimiterBaseDelay, rateLimiterMaxDelay),
		hubMgr.GetEventRecorder(controllerName),
	)
	Expect(err).Should(Succeed())
	Expect(reconciler.SetupWithManager(hubMgr, maxConcurrentReconciles)).Should(Succeed())

	By("Starting the controller manager")
	wg = sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer GinkgoRecover()
		defer wg.Done()
		Expect(hubMgr.Start(ctx)).Should(Succeed(), "Failed to run the controller manager")
	}()
	Expect(hubMgr.GetCache().WaitForCacheSync(ctx)).Should(BeTrue(), "Failed to sync the controller manager cache")

	By("Creating the application namespaces")
	for _, nsName := range []string{appNamespaceName, otherAppNamespaceName} {
		Expect(hubClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		})).Should(Succeed(), "Failed to create the application namespace %s", nsName)
	}
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	// Wait for the controller manager to stop first; otherwise the API server might not shut down in time
	// as the manager's watches are still open.
	wg.Wait()

	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).Should(Succeed())
})
