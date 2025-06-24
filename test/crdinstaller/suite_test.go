/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package crdinstaller

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	scheme    *runtime.Scheme
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "CRD installer Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).Should(Succeed())
	Expect(cfg).NotTo(BeNil())

	scheme = runtime.NewScheme()
	Expect(apiextensionsv1.AddToScheme(scheme)).Should(Succeed())

	//+kubebuilder:scaffold:scheme
	By("construct the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).Should(Succeed())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).Should(Succeed())
})
