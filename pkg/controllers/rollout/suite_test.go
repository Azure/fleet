/*
Copyright 2025 The KubeFleet Authors.

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

package rollout

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1alpha1"
	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	// pre loaded test manifests
	testResourceCRD, testNameSpace, testResource, testConfigMap, testPdb []byte
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
	Expect(fleetv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(fleetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

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
	err = (&Reconciler{
		Client:         k8sClient,
		UncachedReader: mgr.GetAPIReader(),
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

	By("Read namespace")
	rawByte, err = os.ReadFile("manifests/test_namespace.yaml")
	Expect(err).Should(Succeed())
	testNameSpace, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testResource CR")
	rawByte, err = os.ReadFile("../../../test/manifests/test-resource.yaml")
	Expect(err).Should(Succeed())
	testResource, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testConfigMap resource")
	rawByte, err = os.ReadFile("manifests/test-configmap.yaml")
	Expect(err).Should(Succeed())
	testConfigMap, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read PodDisruptionBudget")
	rawByte, err = os.ReadFile("manifests/test_pdb.yaml")
	Expect(err).Should(Succeed())
	testPdb, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())
}
