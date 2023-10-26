/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	// pre loaded test manifests
	testClonesetCRD, testNameSpace, testCloneset, testConfigMap, testEnvelopConfigMap, testEnvelopConfigMap2, testPdb []byte

	// the content of the enveloped resources
	testEnvelopeWebhook, testEnvelopeResourceQuota []byte
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Work generator Controller Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	var err error
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err = testEnv.Start()
	Expect(err).Should(Succeed())
	Expect(cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme
	By("Set all the customized scheme")
	Expect(fleetv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(workv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(kruisev1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	// load test manifests
	readTestManifests()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
		Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
	})
	Expect(err).Should(Succeed())
	// make sure the k8s client is same as the controller client or we can have cache delay
	By("set k8s client same as the controller manager")
	k8sClient = mgr.GetClient()
	// setup our main reconciler
	err = (&Reconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr)
	Expect(err).Should(Succeed())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).Should(Succeed(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).Should(Succeed())
})

func readTestManifests() {
	By("Read testCloneset CRD")
	rawByte, err := os.ReadFile("manifests/test_clonesets_crd.yaml")
	Expect(err).Should(Succeed())
	testClonesetCRD, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read namespace")
	rawByte, err = os.ReadFile("manifests/test_namespace.yaml")
	Expect(err).Should(Succeed())
	testNameSpace, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read clonesetCR")
	rawByte, err = os.ReadFile("manifests/test-cloneset.yaml")
	Expect(err).Should(Succeed())
	testCloneset, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testConfigMap resource")
	rawByte, err = os.ReadFile("manifests/test-configmap.yaml")
	Expect(err).Should(Succeed())
	testConfigMap, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testEnvelopConfigMap resource")
	rawByte, err = os.ReadFile("manifests/test-envelop-configmap.yaml")
	Expect(err).Should(Succeed())
	testEnvelopConfigMap, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testEnvelopConfigMap2 resource")
	rawByte, err = os.ReadFile("manifests/test-envelop-configmap2.yaml")
	Expect(err).Should(Succeed())
	testEnvelopConfigMap2, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read PodDisruptionBudget")
	rawByte, err = os.ReadFile("manifests/test_pdb.yaml")
	Expect(err).Should(Succeed())
	testPdb, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read EnvelopeWebhook")
	rawByte, err = os.ReadFile("manifests/webhook.yaml")
	Expect(err).Should(Succeed())
	testEnvelopeWebhook, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read ResourceQuota")
	rawByte, err = os.ReadFile("manifests/resourcequota.yaml")
	Expect(err).Should(Succeed())
	testEnvelopeResourceQuota, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())
}
