/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package v1alpha1

import (
	"flag"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
)

func TestInternalMemberCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internal Member Cluster Controller Integration Test Suite")
}

var _ = BeforeSuite(func() {
	done := make(chan interface{})
	go func() {
		// GinkgoRecover should be deferred at the top of any spawned goroutine that (may) call `Fail` Since Gomega
		// assertions call fail, you should throw a `defer GinkgoRecover()` at the top of any goroutine that calls out
		// to Gomega.
		// Source: https://pkg.go.dev/github.com/onsi/ginkgo#GinkgoRecover
		defer GinkgoRecover()

		klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		By("bootstrapping test environment")
		testEnv = &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
			ErrorIfCRDPathMissing: true,
		}
		var err error
		cfg, err = testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		err = fleetv1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		err = workv1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		//+kubebuilder:scaffold:scheme
		By("construct the k8s client")
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		By("Starting the controller manager")
		klog.InitFlags(flag.CommandLine)
		mgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme:             scheme.Scheme,
			MetricsBindAddress: "0",
			Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
			Port:               4848,
		})
		Expect(err).ToNot(HaveOccurred())

		close(done)
	}()
	Eventually(done, 60).Should(BeClosed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
