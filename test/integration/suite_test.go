/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package integration

import (
	"context"
	"flag"
	"path/filepath"
	"testing"

	"go.goms.io/fleet/cmd/hubagent/options"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	// +kubebuilder:scaffold:imports

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/cmd/hubagent/workload"
	"go.goms.io/fleet/pkg/controllers/membercluster"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	mgr manager.Manager

	// pre loaded test manifests
	namespacedResource = []string{"Namespace", "PodDisruptionBudget", "CloneSet", "ConfigMap", "Secret", "Service"}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Workload Orchestration Controller Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	var err error
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	By("Set all the customized scheme")
	Expect(fleetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(workv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(kruisev1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// get the codec with the all the scheme
	genericCodecs := serializer.NewCodecFactory(scheme.Scheme)
	genericCodec = genericCodecs.UniversalDeserializer()

	By("Bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme.Scheme,
		UseExistingCluster:    pointer.Bool(false),
	}

	cfg, err = testEnv.Start()
	Expect(err).Should(Succeed())
	Expect(cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme
	By("Construct the controller manager")
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme.Scheme,
		LeaderElection: false,
	})
	Expect(err).Should(Succeed())

	By("Get the k8s client from manager")
	k8sClient = mgr.GetClient()

	By("Construct the controller manager")
	// Set up  the memberCluster reconciler with the manager
	err = (&membercluster.Reconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr)
	Expect(err).Should(Succeed())

	By("Create all the test manifest resources")
	applyTestManifests()

	By("Setup custom controllers")
	opts := options.NewOptions()
	opts.LeaderElection.LeaderElect = false
	err = workload.SetupControllers(ctx, mgr, cfg, opts)
	Expect(err).Should(Succeed())

	By("Start the controller manager")
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).Should(Succeed(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	deleteTestManifests()
	defer klog.Flush()
	cancel()
	By("Tearing down the test environment")
	Expect(testEnv.Stop()).Should(Succeed())
})
