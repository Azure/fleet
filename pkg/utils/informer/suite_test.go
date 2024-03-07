/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package informer

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	testEnv *envtest.Environment
	//dynamicClient dynamic.DynamicClient
	impl        *informerManagerImpl
	informerMgr Manager
	ctx         context.Context
	cancel      context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Informer Manager Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")

	// Start the test environment.
	testEnv = &envtest.Environment{}
	restCfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(restCfg).NotTo(BeNil())

	// Setup dynamic client for informer manager
	dynamicClient, err := dynamic.NewForConfig(restCfg)
	Expect(dynamicClient).NotTo(BeNil())
	Expect(err).NotTo(HaveOccurred())

	impl = &informerManagerImpl{
		dynamicClient:   dynamicClient,
		ctx:             ctx,
		cancel:          cancel,
		informerFactory: dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, time.Minute*5),
		apiResources:    make(map[schema.GroupVersionKind]*APIResourceMeta),
	}

	informerMgr = impl
	defer func() {
		informerMgr.Start()
	}()

	informerMgr.WaitForCacheSync()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("Tearing down the test environment")
	Expect(testEnv.Stop()).Should(Succeed(), "Failed to stop test environment")
})
