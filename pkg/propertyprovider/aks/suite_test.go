/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package aks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/propertyprovider/aks/trackers"
)

const (
	eventuallyDuration = time.Second * 20
	eventuallyInterval = time.Second * 5
)

const (
	region = "eastus"

	aksNodeSKU1 = "Standard_B4ms"
	aksNodeSKU2 = "Standard_A4_v2"
	aksNodeSKU3 = "Standard_DS2_v2"
)

var (
	memberTestEnv *envtest.Environment
	memberClient  client.Client
	ctx           context.Context
	cancel        context.CancelFunc
	p             propertyprovider.PropertyProvider
	pp            trackers.PricingProvider
)

// setUpResources help set up resources in the test environment.
func setUpResources() {
	// Add the namespaces.
	namespaceNames := []string{namespaceName1, namespaceName2, namespaceName3}

	for _, name := range namespaceNames {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		Expect(memberClient.Create(ctx, ns)).To(Succeed(), "Failed to create namespace")
	}
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "AKS Fleet Metric Provider Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("Bootstrap the test environment")

	// Start the test cluster.
	memberTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	memberCfg, err := memberTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(memberCfg).NotTo(BeNil())

	// Set up the K8s client for the test cluster.
	memberClient, err = client.New(memberCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(memberClient).NotTo(BeNil())

	// Set up resources.
	setUpResources()

	// Start the AKS property provider.
	pp = trackers.NewAKSKarpenterPricingClient(ctx, region)
	p = NewWithPricingProvider(pp)
	Expect(p.Start(ctx, memberCfg)).To(Succeed())
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
})
