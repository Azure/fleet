/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package updaterun

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/utils/informer"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	// pre loaded test manifests
	testResourceCRD, testNameSpace, testResource, testConfigMap, testDeployment, testService []byte
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

	// load test manifests
	readTestManifests()

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
	Expect(placementv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(placementv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).Should(Succeed())

	// make sure the k8s client is same as the controller client, or we can have cache delay
	By("set k8s client same as the controller manager")
	k8sClient = mgr.GetClient()

	// setup our main reconciler
	fakeInformer := &informer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			}: true,
			{
				Group:   "",
				Version: "v1",
				Kind:    "Deployment",
			}: true,
			{
				Group:   "",
				Version: "v1",
				Kind:    "Secret",
			}: true,
		},
		IsClusterScopedResource: false,
	}
	err = (&Reconciler{
		Client:          k8sClient,
		InformerManager: fakeInformer,
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
	By("Read testResource CRD")
	rawByte, err := os.ReadFile("../../../test/manifests/test_testresources_crd.yaml")
	Expect(err).Should(Succeed())
	testResourceCRD, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testResource CR")
	rawByte, err = os.ReadFile("../../../test/manifests/test-resource.yaml")
	Expect(err).Should(Succeed())
	testResource, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testConfigMap resource")
	rawByte, err = os.ReadFile("../../../test/e2e/resources/test-configmap.yaml")
	Expect(err).Should(Succeed())
	testConfigMap, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testDeployment")
	rawByte, err = os.ReadFile("../../../test/e2e/resources/test-deployment.yaml")
	Expect(err).Should(Succeed())
	testDeployment, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testService")
	rawByte, err = os.ReadFile("../../../test/e2e/resources/test-service.yaml")
	Expect(err).Should(Succeed())
	testService, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Create testNameSpace")
	testNameSpace, err = json.Marshal(corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "app",
			Labels: map[string]string{
				"fleet.azure.com/name": "test",
			},
		},
	})
	Expect(err).Should(Succeed())
}
