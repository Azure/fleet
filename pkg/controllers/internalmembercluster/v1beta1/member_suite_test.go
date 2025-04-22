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
package v1beta1

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/pkg/propertyprovider"
)

const (
	member1Name           = "cluster-1"
	member2Name           = "cluster-2"
	member1ReservedNSName = "fleet-member-cluster-1"
	member2ReservedNSName = "fleet-member-cluster-2"

	imageName = "nginx"
)

var (
	// This test suite features three clusters:
	//
	// * Hub cluster
	// * Member cluster 1: the member cluster that has a property provider set up
	// * Member cluster 2: the member cluster that does not have a property provider set up
	hubCfg            *rest.Config
	member1Cfg        *rest.Config
	member2Cfg        *rest.Config
	hubEnv            *envtest.Environment
	member1Env        *envtest.Environment
	member2Env        *envtest.Environment
	member1Mgr        manager.Manager
	member2Mgr        manager.Manager
	hubClient         client.Client
	member1Client     client.Client
	member2Client     client.Client
	workApplier1      *workapplier.Reconciler
	workApplier2      *workapplier.Reconciler
	propertyProvider1 *manuallyUpdatedProvider

	ctx    context.Context
	cancel context.CancelFunc
)

func TestMain(m *testing.M) {
	// Normally the GinkgoWriter is configured as part of the BeforeSuite routine; however,
	// since the unit tests in this package is not using the Ginkgo framework, and some of the
	// tests might emit error logs, here the configuration happens at the test entrypoint to
	// avoid R/W data races on the logger.
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	ctrllog.SetLogger(logger)

	os.Exit(m.Run())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Internal Member Cluster Controller Integration Test Suite")
}

type manuallyUpdatedProvider struct {
	mu             sync.Mutex
	lastUpdatedRes *propertyprovider.PropertyCollectionResponse
}

var _ propertyprovider.PropertyProvider = &manuallyUpdatedProvider{}

func (m *manuallyUpdatedProvider) Start(_ context.Context, _ *rest.Config) error {
	return nil
}

func (m *manuallyUpdatedProvider) Collect(_ context.Context) propertyprovider.PropertyCollectionResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.lastUpdatedRes != nil {
		return *m.lastUpdatedRes
	}
	return propertyprovider.PropertyCollectionResponse{}
}

func (m *manuallyUpdatedProvider) Update(res *propertyprovider.PropertyCollectionResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastUpdatedRes = res
}

func setupResources() {
	// Create the namespaces.
	member1ReservedNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: member1ReservedNSName,
		},
	}
	Expect(hubClient.Create(ctx, &member1ReservedNS)).To(Succeed())

	member2ReservedNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: member2ReservedNSName,
		},
	}
	Expect(hubClient.Create(ctx, &member2ReservedNS)).To(Succeed())

	memberClients := []client.Client{member1Client, member2Client}

	// Create the nodes in the clusters.
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName2,
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("9"),
					corev1.ResourceMemory: resource.MustParse("30Gi"),
				},
			},
		},
	}

	for nidx := range nodes {
		node := nodes[nidx]

		for cidx := range memberClients {
			nodeCopy := node.DeepCopy()
			client := memberClients[cidx]
			Expect(client.Create(ctx, nodeCopy)).To(Succeed())

			nodeCopy.Status = *node.Status.DeepCopy()
			Expect(client.Status().Update(ctx, nodeCopy)).To(Succeed())
		}
	}

	// Create the pods in the clusters.
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName1,
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName1,
				Containers: []corev1.Container{
					{
						Name:  containerName1,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
					{
						Name:  containerName2,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName2,
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName2,
				Containers: []corev1.Container{
					{
						Name:  containerName1,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodSucceeded,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName3,
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName2,
				Containers: []corev1.Container{
					{
						Name:  containerName1,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName4,
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  containerName1,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}

	for pidx := range pods {
		pod := pods[pidx]

		for cidx := range memberClients {
			podCopy := pod.DeepCopy()
			client := memberClients[cidx]
			Expect(client.Create(ctx, podCopy)).To(Succeed())

			podCopy.Status = *pod.Status.DeepCopy()
			Expect(client.Status().Update(ctx, podCopy)).To(Succeed())
		}
	}
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environments")
	hubEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	member1Env = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	member2Env = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	hubCfg, err = hubEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubCfg).NotTo(BeNil())

	member1Cfg, err = member1Env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(member1Cfg).NotTo(BeNil())

	member2Cfg, err = member2Env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(member2Cfg).NotTo(BeNil())

	err = clusterv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("building the K8s clients")
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(hubClient).NotTo(BeNil())

	member1Client, err = client.New(member1Cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(member1Client).NotTo(BeNil())

	member2Client, err = client.New(member2Cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(member2Client).NotTo(BeNil())

	By("setting up the resources")
	setupResources()

	By("starting the controller managers and setting up the controllers")
	member1Mgr, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				member1ReservedNSName: {},
			},
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).NotTo(HaveOccurred())

	// This controller is created for testing purposes only; no reconciliation loop is actually
	// run.
	workApplier1 = workapplier.NewReconciler(hubClient, member1ReservedNSName, nil, nil, nil, nil, 0, 1, time.Second*5, time.Second*5)

	propertyProvider1 = &manuallyUpdatedProvider{}
	member1Reconciler, err := NewReconciler(ctx, hubClient, member1Cfg, member1Client, workApplier1, propertyProvider1)
	Expect(err).NotTo(HaveOccurred())
	Expect(member1Reconciler.SetupWithManager(member1Mgr, member1Name+"-controller")).To(Succeed())

	member2Mgr, err = ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				member2ReservedNSName: {},
			},
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).NotTo(HaveOccurred())

	// This controller is created for testing purposes only; no reconciliation loop is actually
	// run.
	workApplier2 = workapplier.NewReconciler(hubClient, member2ReservedNSName, nil, nil, nil, nil, 0, 1, time.Second*5, time.Second*5)

	member2Reconciler, err := NewReconciler(ctx, hubClient, member2Cfg, member2Client, workApplier2, nil)
	Expect(err).NotTo(HaveOccurred())
	Expect(member2Reconciler.SetupWithManager(member2Mgr, member2Name+"-controller")).To(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(member1Mgr.Start(ctx)).To(Succeed())
	}()

	go func() {
		defer GinkgoRecover()
		Expect(member2Mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("tearing down the test environments")
	Expect(hubEnv.Stop()).To(Succeed())
	Expect(member1Env.Stop()).To(Succeed())
	Expect(member2Env.Stop()).To(Succeed())
})
