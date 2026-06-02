/*
Copyright 2026 The KubeFleet Authors.

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

package admissionpolicymanager

import (
	"context"
	"flag"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
var (
	cfg               *rest.Config
	testEnv           *envtest.Environment
	hubUncachedClient client.Client
	hubMgr            ctrl.Manager

	ctx    context.Context
	cancel context.CancelFunc
)

var (
	eventuallyDuration = time.Second * 10
	eventuallyInterval = time.Millisecond * 500
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Admission Policy Manager Integration Test Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)

	By("Bootstrapping test environment")
	testEnv = &envtest.Environment{}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	By("Setting up the controller manager")
	hubMgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0", // disable the metrics server.
		},
		Logger: logger,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(hubMgr).ToNot(BeNil())

	By("Building the K8s client")
	hubUncachedClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(hubUncachedClient).ToNot(BeNil())

	By("Setting up the policy manager")
	policyManager, err := New(hubUncachedClient, DefaultPolicyGeneratorConfigs)
	Expect(err).ToNot(HaveOccurred())
	Expect(policyManager).ToNot(BeNil())
	Expect(policyManager.Start(ctx)).To(Succeed())

	By("Starting the controller manager")
	go func() {
		defer GinkgoRecover()
		Expect(hubMgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("Tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
