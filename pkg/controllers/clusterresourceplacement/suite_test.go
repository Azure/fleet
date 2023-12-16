/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package clusterresourceplacement

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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/cmd/hubagent/options"
	"go.goms.io/fleet/pkg/controllers/clusterresourceplacementwatcher"
	"go.goms.io/fleet/pkg/controllers/clusterschedulingpolicysnapshot"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

const (
	controllerName = "crp"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "ClusterResourcePlacement Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).Should(Succeed())
	Expect(cfg).NotTo(BeNil())

	err = placementv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).Should(Succeed())

	//+kubebuilder:scaffold:scheme
	By("construct the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).Should(Succeed())
	Expect(k8sClient).NotTo(BeNil())

	dynamicClient, err := dynamic.NewForConfig(cfg)
	Expect(err).Should(Succeed())
	Expect(dynamicClient).NotTo(BeNil())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
		Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
		Port:               4848,
	})
	Expect(err).Should(Succeed(), "failed to create manager")

	reconciler := &Reconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		UncachedReader:  mgr.GetAPIReader(),
		Recorder:        mgr.GetEventRecorderFor(controllerName),
		RestMapper:      mgr.GetRESTMapper(),
		InformerManager: informer.NewInformerManager(dynamicClient, 5*time.Minute, ctx.Done()),
		ResourceConfig:  utils.NewResourceConfig(false),
		SkippedNamespaces: map[string]bool{
			"default": true,
		},
	}
	opts := options.RateLimitOptions{
		RateLimiterBaseDelay:  5 * time.Millisecond,
		RateLimiterMaxDelay:   60 * time.Second,
		RateLimiterQPS:        10,
		RateLimiterBucketSize: 100,
	}
	rateLimiter := options.DefaultControllerRateLimiter(opts)
	crpController := controller.NewController(controllerName, controller.NamespaceKeyFunc, reconciler.Reconcile, rateLimiter)

	// Set up the watchers
	err = (&clusterschedulingpolicysnapshot.Reconciler{
		Client:              mgr.GetClient(),
		PlacementController: crpController,
	}).SetupWithManager(mgr)
	Expect(err).Should(Succeed(), "failed to create clusterSchedulingPolicySnapshot watcher")

	err = (&clusterresourceplacementwatcher.Reconciler{
		PlacementController: crpController,
	}).SetupWithManager(mgr)
	Expect(err).Should(Succeed(), "failed to create clusterResourcePlacement watcher")

	ctx, cancel = context.WithCancel(context.TODO())
	// Run the controller manager
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).Should(Succeed(), "failed to run manager")
	}()
	// Run the crp controller
	go func() {
		err := crpController.Run(ctx, 1)
		Expect(err).Should(Succeed(), "failed to run crp controller")
	}()

	By("By creating member clusters namespaces")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: member1Namespace,
		},
	}
	Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

	ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: member2Namespace,
		},
	}
	Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
