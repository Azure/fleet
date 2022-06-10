package membercluster

import (
	"flag"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"go.goms.io/fleet/apis/v1alpha1"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
)

func TestMemberCluster(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "MemberCluster Controller Integration Test Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	done := make(chan interface{})
	go func() {
		klog.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

		ginkgo.By("bootstrapping test environment")
		testEnv = &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join("../../../", "config", "crd", "bases")},
			ErrorIfCRDPathMissing: true,
		}
		var err error
		cfg, err = testEnv.Start()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cfg).NotTo(gomega.BeNil())

		err = v1alpha1.AddToScheme(scheme.Scheme)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		//+kubebuilder:scaffold:scheme
		ginkgo.By("construct the k8s client")
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(k8sClient).NotTo(gomega.BeNil())

		ginkgo.By("Starting the controller manager")
		klog.InitFlags(flag.CommandLine)
		mgr, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme:             scheme.Scheme,
			MetricsBindAddress: "0",
			Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
			Port:               4848,
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		close(done)
	}()
	gomega.Eventually(done, 60).Should(gomega.BeClosed())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")
	err := testEnv.Stop()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})
