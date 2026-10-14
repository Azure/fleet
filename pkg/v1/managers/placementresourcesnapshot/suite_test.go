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

package placementresourcesnapshot

import (
	"context"
	"flag"
	"path/filepath"
	"testing"

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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	// The number of mutex slots used by the manager under test.
	testMuSlotCnt = 256

	appNamespaceName = "app"
)

var (
	hubCfg *rest.Config
	hubEnv *envtest.Environment
	hubMgr ctrl.Manager
	// hubClient is the client from the controller manager; reads are served from its cache, which
	// matches how the manager under test reads objects (and is what makes the field indexes usable).
	hubClient client.Client
	// hubUncachedReader reads straight from the API server.
	hubUncachedReader client.Reader

	resourceSnapshotManager *Manager

	ctx    context.Context
	cancel context.CancelFunc
	// mgrStopped is closed once the controller manager has fully stopped.
	mgrStopped chan struct{}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Placement Resource Snapshot Manager Integration Test Suite")
}

// noOpReconciler is a no-op controller whose only purpose is to have the controller manager set up
// informers (and thus populate the cache) for the objects the manager under test reads from the cache.
type noOpReconciler struct{}

// Reconcile does nothing.
func (r *noOpReconciler) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (r *noOpReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("placement-resource-snapshot-manager-test-noop").
		For(&placementv1alpha1.PlacementPolicy{}).
		Watches(&placementv1alpha1.ClusterPlacementPolicy{}, &handler.EnqueueRequestForObject{}).
		Watches(&placementv1alpha1.PlacementResourceSnapshot{}, &handler.EnqueueRequestForObject{}).
		Watches(&placementv1alpha1.ClusterPlacementResourceSnapshot{}, &handler.EnqueueRequestForObject{}).
		Watches(&placementv1alpha1.PlacementBinding{}, &handler.EnqueueRequestForObject{}).
		Watches(&placementv1alpha1.ClusterPlacementBinding{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	logger := textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4)))
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)

	By("Bootstrapping the test environment")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(hubCfg).ToNot(BeNil())

	By("Setting up the scheme")
	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("Setting up the controller manager")
	hubMgr, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())
	hubClient = hubMgr.GetClient()
	hubUncachedReader = hubMgr.GetAPIReader()

	By("Creating the test namespace")
	Expect(hubClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: appNamespaceName},
	})).To(Succeed())

	By("Setting up the field indexes")
	// The indexes must be set up before the manager starts.
	Expect(fieldindexers.SetupWithHubAgentControllerManager(ctx, hubMgr)).To(Succeed())

	By("Setting up the placement resource snapshot manager")
	hubDynamicClient, err := dynamic.NewForConfig(hubCfg)
	Expect(err).ToNot(HaveOccurred())
	hubDynamicInformerManager := informer.NewInformerManager(hubDynamicClient, 0, ctx.Done())

	resourceSnapshotManager, err = New(hubMgr,
		hubDynamicClient,
		hubDynamicInformerManager,
		hubMgr.GetRESTMapper(),
		testMuSlotCnt,
		// Set maxPerSnapshotResourceDataSizeBytes and maxPerSnapshotResourceCnt to 2000 and 3 respectively to allow
		// simpler test setup.
		2000,
		3,
	)
	Expect(err).ToNot(HaveOccurred())
	Expect(hubMgr.Add(resourceSnapshotManager)).To(Succeed())

	By("Setting up the no-op controller")
	Expect((&noOpReconciler{}).SetupWithManager(hubMgr)).To(Succeed())

	By("Starting the controller manager")
	mgrStopped = make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(mgrStopped)
		Expect(hubMgr.Start(ctx)).To(Succeed(), "Failed to run the controller manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	// Wait for the manager to stop first; otherwise the API server may not shut down in time as
	// the manager's watches are still open.
	Eventually(mgrStopped, "30s").Should(BeClosed())

	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).To(Succeed())
})
