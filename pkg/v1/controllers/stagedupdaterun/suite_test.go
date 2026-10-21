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

package stagedupdaterun

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	rolloutv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/rollout/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resourceeligibility"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	// The namespace in which namespace-scoped objects (e.g., PlacementPolicy, StagedUpdateStrategy,
	// and StagedUpdateRun objects) are created.
	appNamespaceName = "app"
	// Another namespace, for verifying that namespace-scoped objects are not looked up across namespaces.
	//
	// Note that the namespaces are created once per (Ginkgo) process, as the envtest environment does not
	// support namespace deletion.
	otherAppNamespaceName = "other-app"

	// The number of max. concurrent reconciliations for the staged update run controller.
	maxConcurrentReconciles = 4
	// The number of workers in the parallelizer that the staged update run controller uses (e.g., to retrieve the
	// member clusters referenced by placement bindings in parallel).
	workerCount = 4

	// The number of mutex slots used by the placement resource snapshot manager.
	placementResourceSnapshotMuSlotCnt = 256

	// The base and max. delays of the rate limiters (for binding manager claims and binding availability
	// checks) used by the staged update run controller.
	//
	// The defaults (5 seconds and 180 seconds respectively) are too long for the integration tests;
	// shorter delays are used here to keep the tests fast.
	rateLimiterBaseDelay = time.Millisecond * 250
	rateLimiterMaxDelay  = time.Second * 2

	// The refresh period (i.e., the processing time budget per reconciliation) of the staged update run controller.
	//
	// The controller only persists the status of a staged update run at the end of a reconciliation; with the
	// minimum refresh period New() accepts (30 seconds), an in-progress rollout would not report its progress for
	// that long. The suite overrides the refresh period with a shorter value, so that the tests can observe the
	// progress in a timely manner; note that this deviates from what production deployments may use.
	refreshPeriodForTesting = time.Second * 5
)

var (
	hubCfg *rest.Config
	hubEnv *envtest.Environment
	hubMgr manager.Manager
	// hubClient is the client from the controller manager; reads are served from its cache, which
	// matches how the staged update run controller reads objects.
	hubClient client.Client

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

const (
	// The environment variable that the CI pipeline sets to tell which runner invokes the tests.
	testRunnerNameEnvVarName = "KUBEFLEET_CI_TEST_RUNNER_NAME"
	// The runner (`go test`, via `make test`) with which the tests are skipped in the CI pipeline; the pipeline runs
	// this package in a separate step with the Ginkgo CLI (in parallel) instead.
	runnerNameToSkipTestsInCI = "default"
)

func TestMain(m *testing.M) {
	// Skip the tests if in the CI environment the tests are invoked with `go test` instead of the Ginkgo CLI.
	// This has no effect outside the CI environment.
	if v := os.Getenv(testRunnerNameEnvVarName); v == runnerNameToSkipTestsInCI {
		log.Println("Skipping the tests in CI as they are not run with the expected runner")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Staged Update Run Controller Integration Test Suite")
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
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(rolloutv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

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

	// The staged update run controller (and the placement resource snapshot manager) list objects via
	// custom field indexes; the indexes must be set up before the manager starts.
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
		placementresourcesnapshot.DefaultMaxPerSnapshotResourceDataSizeBytes,
		placementresourcesnapshot.DefaultMaxPerSnapshotResourceCnt,
	)
	Expect(err).Should(Succeed())
	// Add the placement resource snapshot manager to the controller manager so that its snapshot GC runs.
	Expect(hubMgr.Add(placementResourceSnapshotManager)).Should(Succeed())

	By("Setting up the staged update run controller")
	reconciler, err := New(
		hubMgr.GetClient(),
		hubMgr.GetAPIReader(),
		placementResourceSnapshotManager,
		parallelizer.NewParallelizer(workerCount),
		workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](rateLimiterBaseDelay, rateLimiterMaxDelay),
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](rateLimiterBaseDelay, rateLimiterMaxDelay),
		minRefreshPeriod,
	)
	Expect(err).Should(Succeed())
	// New() does not accept refresh periods shorter than the minimum; set the refresh period directly.
	reconciler.refreshPeriod = refreshPeriodForTesting
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
