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

package workapplier

import (
	"context"
	"flag"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrloption "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	testv1alpha1 "github.com/kubefleet-dev/kubefleet/test/apis/v1alpha1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
var (
	hubCfg    *rest.Config
	hubEnv    *envtest.Environment
	hubClient client.Client

	memberCfg1           *rest.Config
	memberEnv1           *envtest.Environment
	hubMgr1              manager.Manager
	memberClient1        client.Client
	memberDynamicClient1 dynamic.Interface
	workApplier1         *Reconciler

	memberCfg2           *rest.Config
	memberEnv2           *envtest.Environment
	hubMgr2              manager.Manager
	memberClient2        client.Client
	memberDynamicClient2 dynamic.Interface
	workApplier2         *Reconciler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

const (
	// The number of max. concurrent reconciliations for the work applier controller.
	maxConcurrentReconciles = 5
	// The count of workers for the work applier controller.
	workerCount = 4

	memberReservedNSName1 = "fleet-member-experimental-1"
	memberReservedNSName2 = "fleet-member-experimental-2"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Work Applier Integration Test Suite")
}

func setupResources() {
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberReservedNSName1,
		},
	}
	Expect(hubClient.Create(ctx, ns1)).To(Succeed())

	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberReservedNSName2,
		},
	}
	Expect(hubClient.Create(ctx, ns2)).To(Succeed())
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("Bootstrapping test environments")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../", "config", "crd", "bases"),
			filepath.Join("../../../", "test", "manifests"),
		},
	}
	// memberEnv1 is the test environment for verifying most work applier behaviors.
	memberEnv1 = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../", "config", "crd", "bases"),
			filepath.Join("../../../", "test", "manifests"),
		},
	}
	// memberEnv2 is the test environment for verifying the correctness of exponential backoff as
	// enabled in the work applier.
	//
	// Note (chenyu1): to avoid flakiness, a separate test environment with a work applier of special
	// exponential backoff setting is used so that the backoffs can be identified in a more
	// apparent and controllable manner.
	memberEnv2 = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../", "config", "crd", "bases"),
			filepath.Join("../../../", "test", "manifests"),
		},
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(hubCfg).ToNot(BeNil())

	memberCfg1, err = memberEnv1.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(memberCfg1).ToNot(BeNil())

	memberCfg2, err = memberEnv2.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(memberCfg2).ToNot(BeNil())

	err = batchv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = fleetv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = testv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("Building the K8s clients")
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(hubClient).ToNot(BeNil())

	memberClient1, err = client.New(memberCfg1, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(memberClient1).ToNot(BeNil())

	memberClient2, err = client.New(memberCfg2, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(memberClient2).ToNot(BeNil())

	// This setup also requires a client-go dynamic client for the member cluster.
	memberDynamicClient1, err = dynamic.NewForConfig(memberCfg1)
	Expect(err).ToNot(HaveOccurred())

	memberDynamicClient2, err = dynamic.NewForConfig(memberCfg2)
	Expect(err).ToNot(HaveOccurred())

	By("Setting up the resources")
	setupResources()

	By("Setting up the controller and the controller manager for member cluster 1")
	hubMgr1, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				memberReservedNSName1: {},
			},
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).ToNot(HaveOccurred())

	workApplier1 = NewReconciler(
		hubClient,
		memberReservedNSName1,
		memberDynamicClient1,
		memberClient1,
		memberClient1.RESTMapper(),
		hubMgr1.GetEventRecorderFor("work-applier"),
		maxConcurrentReconciles,
		workerCount,
		30*time.Second,
		true,
		60,
		nil, // Use the default backoff rate limiter.
	)
	Expect(workApplier1.SetupWithManager(hubMgr1)).To(Succeed())

	By("Setting up the controller and the controller manager for member cluster 2")
	hubMgr2, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				memberReservedNSName2: {},
			},
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).ToNot(HaveOccurred())

	superLongExponentialBackoffRateLimiter := NewRequeueMultiStageWithExponentialBackoffRateLimiter(
		// Allow one attempt of backoff with fixed delay.
		1,
		// Use a fixed delay of 10 seconds.
		10,
		// Set the exponential backoff base factor to 1.5 for the slow backoff stage.
		1.5,
		// Set the initial slow backoff delay to 20 seconds.
		20,
		// Set the maximum slow backoff delay to 30 seconds (allow 2 slow backoffs).
		30,
		// Set the exponential backoff base factor to 2 for the fast backoff stage.
		2,
		// Set the maximum fast backoff delay to 90 seconds (allow 1 fast backoff).
		90,
		// Allow skipping to fast backoff stage.
		true,
	)
	workApplier2 = NewReconciler(
		hubClient,
		memberReservedNSName2,
		memberDynamicClient2,
		memberClient2,
		memberClient2.RESTMapper(),
		hubMgr2.GetEventRecorderFor("work-applier"),
		maxConcurrentReconciles,
		workerCount,
		30*time.Second,
		true,
		60,
		superLongExponentialBackoffRateLimiter,
	)
	// Due to name conflicts, the second work applier must be set up manually.
	err = ctrl.NewControllerManagedBy(hubMgr2).Named("work-applier-controller-duplicate").
		WithOptions(ctrloption.Options{
			MaxConcurrentReconciles: workApplier2.concurrentReconciles,
		}).
		For(&fleetv1beta1.Work{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(workApplier2)
	Expect(err).NotTo(HaveOccurred())

	wg = sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer GinkgoRecover()
		defer wg.Done()
		Expect(workApplier1.Join(ctx)).To(Succeed())
		Expect(hubMgr1.Start(ctx)).To(Succeed())
	}()

	go func() {
		defer GinkgoRecover()
		defer wg.Done()
		Expect(workApplier2.Join(ctx)).To(Succeed())
		Expect(hubMgr2.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	wg.Wait()
	By("Tearing down the test environment")
	Expect(hubEnv.Stop()).To(Succeed())
	Expect(memberEnv1.Stop()).To(Succeed())
	Expect(memberEnv2.Stop()).To(Succeed())
})
