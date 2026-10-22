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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
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

	// A work applier dedicated to the test cases that change its lifecycle (i.e., leave and re-join the fleet), so
	// that such changes never affect other test cases. It watches its own reserved namespace in the hub cluster, and
	// shares the member cluster (memberEnv1) with workApplier1.
	hubMgrLifecycle      manager.Manager
	workApplierLifecycle *Reconciler

	// A work applier dedicated to the wave test cases. Its parallelizer pauses for a fixed delay after processing
	// each wave of manifests, so that the order of the waves shows in the creation timestamps of the applied objects;
	// its hub cluster client records the status updates on work objects. It watches its own reserved namespace in the
	// hub cluster, and shares the member cluster (memberEnv1) with workApplier1.
	hubMgrWaves            manager.Manager
	workApplierWaves       *Reconciler
	hubClientRecorderWaves *recordingClient

	// A work applier dedicated to the requeue and status update test cases. Its hub/member cluster clients record the
	// start of each reconciliation and the status updates on work/appliedWork objects, and it uses a requeue rate
	// limiter with short delays (see instrumentedRequeueRateLimiter). It watches its own reserved namespace in the hub
	// cluster, and shares the member cluster (memberEnv1) with workApplier1.
	hubMgrInstrumented               manager.Manager
	workApplierInstrumented          *Reconciler
	hubClientRecorderInstrumented    *recordingClient
	memberClientRecorderInstrumented *recordingClient

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

const (
	// The number of max. concurrent reconciliations for the work applier controller.
	maxConcurrentReconciles = 5
	// The count of workers for the work applier controller.
	workerCount = 4

	// The max. time the work applier waits for an appliedWork object to be deleted before it unblocks
	// the deletion manually.
	//
	// Note that envtest runs no GC controller; foreground deletions of appliedWork objects never complete
	// in the test environments, and the work applier will always wait for 2 * cleanupWaitTime before
	// it removes the cleanup finalizers. A short wait time is used here to keep the tests fast.
	cleanupWaitTime = time.Second * 5

	memberReservedNSName1 = "fleet-member-experimental-1"
	// The reserved namespace for the work applier dedicated to the lifecycle test cases.
	memberReservedNSNameLifecycle = "fleet-member-experimental-lifecycle"
	// The reserved namespace for the work applier dedicated to the wave test cases.
	memberReservedNSNameWaves = "fleet-member-experimental-waves"
	// The reserved namespace for the work applier dedicated to the requeue and status update test cases.
	memberReservedNSNameInstrumented = "fleet-member-experimental-instrumented"

	// The delay that the parallelizer of the work applier for the wave test cases adds after processing each wave of
	// manifests. Creation timestamps are of the second granularity; the delay must be at least two seconds for
	// consecutive waves to be told apart reliably.
	parallelizerFixedDelay = time.Second * 3
)

// instrumentedRequeueRateLimiter returns the requeue rate limiter for the work applier dedicated to the requeue test
// cases. It requeues after 2 seconds once, then backs off slowly (by a factor of 1.5, from 2 seconds up to 4 seconds),
// then backs off fast (by a factor of 2, up to 8 seconds); it skips to the fast backoff stage when all the applied
// manifests are available. In other words, the requeue delays are 2s, 2s, 3s, 6s, 8s, 8s, ... if some manifests are
// not available, and 2s, 2s, 4s, 8s, 8s, ... if all the manifests are available.
func instrumentedRequeueRateLimiter() *MultiStageRequeueWithExponentialBackoffRateLimiter {
	rateLimiter, err := NewMultiStageRequeueWithExponentialBackoffRateLimiter(1, 2, 1.5, 2, 4, 2, 8, true)
	Expect(err).ToNot(HaveOccurred(), "Failed to create the requeue rate limiter for the instrumented work applier")
	return rateLimiter
}

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

	RunSpecs(t, "Work Applier Integration Test Suite")
}

func setupResources() {
	for _, nsName := range []string{memberReservedNSName1, memberReservedNSNameLifecycle, memberReservedNSNameWaves, memberReservedNSNameInstrumented} {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		Expect(hubClient.Create(ctx, ns)).To(Succeed(), "Failed to create the reserved namespace %s", nsName)
	}
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
	hubMgr1 = newHubManager(memberReservedNSName1, false)
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
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](DefaultCleanupBaseDelay, DefaultCleanupMaxDelay),
		defaultRequeueRateLimiter,
		cleanupWaitTime,
	)
	Expect(workApplier1.SetupWithManager(hubMgr1)).To(Succeed())

	// The work appliers below all share the member cluster with workApplier1, and watch their own reserved
	// namespaces in the hub cluster. The requeue rate limiter tracks work objects by their namespaces and names; it is
	// safe to share the default one, as the work appliers watch different namespaces.

	By("Setting up the controller and the controller manager for the lifecycle test cases")
	hubMgrLifecycle = newHubManager(memberReservedNSNameLifecycle, true)
	workApplierLifecycle = New(
		memberReservedNSNameLifecycle,
		hubMgrLifecycle.GetClient(),
		hubMgrLifecycle.GetAPIReader(),
		memberClient1,
		memberDynamicClient1,
		memberClient1.RESTMapper(),
		parallelizer.NewParallelizer(workerCount),
		maxConcurrentReconciles,
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](DefaultCleanupBaseDelay, DefaultCleanupMaxDelay),
		defaultRequeueRateLimiter,
		cleanupWaitTime,
	)
	Expect(workApplierLifecycle.SetupWithManager(hubMgrLifecycle)).To(Succeed())

	By("Setting up the controller and the controller manager for the wave test cases")
	hubMgrWaves = newHubManager(memberReservedNSNameWaves, true)
	hubClientRecorderWaves = newRecordingClient(hubMgrWaves.GetClient())
	workApplierWaves = New(
		memberReservedNSNameWaves,
		hubClientRecorderWaves,
		hubMgrWaves.GetAPIReader(),
		memberClient1,
		memberDynamicClient1,
		memberClient1.RESTMapper(),
		&parallelizerWithFixedDelay{
			Parallelizer: parallelizer.NewParallelizer(workerCount),
			delay:        parallelizerFixedDelay,
		},
		maxConcurrentReconciles,
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](DefaultCleanupBaseDelay, DefaultCleanupMaxDelay),
		defaultRequeueRateLimiter,
		cleanupWaitTime,
	)
	Expect(workApplierWaves.SetupWithManager(hubMgrWaves)).To(Succeed())

	By("Setting up the controller and the controller manager for the requeue and status update test cases")
	hubMgrInstrumented = newHubManager(memberReservedNSNameInstrumented, true)
	hubClientRecorderInstrumented = newRecordingClient(hubMgrInstrumented.GetClient())
	memberClientRecorderInstrumented = newRecordingClient(memberClient1)
	workApplierInstrumented = New(
		memberReservedNSNameInstrumented,
		hubClientRecorderInstrumented,
		hubMgrInstrumented.GetAPIReader(),
		memberClientRecorderInstrumented,
		memberDynamicClient1,
		memberClient1.RESTMapper(),
		parallelizer.NewParallelizer(workerCount),
		maxConcurrentReconciles,
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](DefaultCleanupBaseDelay, DefaultCleanupMaxDelay),
		instrumentedRequeueRateLimiter(),
		cleanupWaitTime,
	)
	Expect(workApplierInstrumented.SetupWithManager(hubMgrInstrumented)).To(Succeed())

	wg = sync.WaitGroup{}
	for _, pair := range []struct {
		mgr         manager.Manager
		workApplier *Reconciler
	}{
		{hubMgr1, workApplier1},
		{hubMgrLifecycle, workApplierLifecycle},
		{hubMgrWaves, workApplierWaves},
		{hubMgrInstrumented, workApplierInstrumented},
	} {
		wg.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			Expect(pair.workApplier.Join(ctx)).To(Succeed())
			Expect(pair.mgr.Start(ctx)).To(Succeed())
		}()
	}
})

// newHubManager creates a controller manager for the hub cluster, whose cache covers only the given reserved
// namespace, and sets up the field indexes that the work applier needs.
//
// The work applier always registers its controller under the same name; for all but the first work applier in the
// test environment, the controller name uniqueness check must be skipped.
func newHubManager(reservedNSName string, skipControllerNameValidation bool) manager.Manager {
	mgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				reservedNSName: {},
			},
		},
		Controller: config.Controller{
			SkipNameValidation: ptr.To(skipControllerNameValidation),
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).ToNot(HaveOccurred(), "Failed to create the controller manager for reserved namespace %s", reservedNSName)

	// The work applier lists work objects via custom field indexes; the indexes must be set up
	// (with the manager's cache) before the manager starts.
	Expect(fieldindexers.SetupWithMemberAgentControllerManager(ctx, mgr)).To(Succeed(), "Failed to set up the field indexes for reserved namespace %s", reservedNSName)
	return mgr
}

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	wg.Wait()
	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).To(Succeed())
	Expect(memberEnv1.Stop()).To(Succeed())
})
