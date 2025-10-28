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

package workgenerator

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	// pre loaded test manifests
	testResourceCRD, testNameSpace, testResource, testConfigMap, testResourceEnvelope, testResourceEnvelope2, testClusterScopedEnvelope, testPdb []byte

	// want overridden manifest which is overridden by cro-1 and ro-1
	wantOverriddenTestResource []byte

	// the content of the enveloped resources
	testResourceQuotaContent, testResourceQuota2Content, testWebhookContent, testClusterRoleContent []byte
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
	Expect(placementv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	// load test manifests
	readTestManifests()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).Should(Succeed())
	// make sure the k8s client is same as the controller client or we can have cache delay
	By("set k8s client same as the controller manager")
	k8sClient = mgr.GetClient()
	// setup our main reconciler
	fakeInformer := informer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			{
				Group:   "apiextensions.k8s.io",
				Version: "v1",
				Kind:    "CustomResourceDefinition",
			}: true,
			{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			}: true,
			{
				Group:   "admissionregistration.k8s.io",
				Version: "v1",
				Kind:    "MutatingWebhookConfiguration",
			}: true,
		},
		IsClusterScopedResource: true,
	}
	err = (&Reconciler{
		Client:          mgr.GetClient(),
		InformerManager: &fakeInformer,
	}).SetupWithManagerForClusterResourceBinding(mgr)
	Expect(err).Should(Succeed())

	err = (&Reconciler{
		Client:          mgr.GetClient(),
		InformerManager: &fakeInformer,
	}).SetupWithManagerForResourceBinding(mgr)
	Expect(err).Should(Succeed())

	createOverrides()

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).Should(Succeed(), "failed to run manager")
	}()
})

func createOverrides() {
	appNamespace = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: appNamespaceName,
		},
	}
	Expect(k8sClient.Create(ctx, &appNamespace)).Should(Succeed(), "Failed to create the application namespace")
	By(fmt.Sprintf("Application namespace %s created", appNamespaceName))

	validClusterResourceOverrideSnapshot = placementv1beta1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: validClusterResourceOverrideSnapshotName,
			Labels: map[string]string{
				placementv1beta1.IsLatestSnapshotLabel: "true",
			},
		},
		Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
			OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   utils.NamespaceGVK.Group,
						Version: utils.NamespaceGVK.Version,
						Kind:    utils.NamespaceGVK.Kind,
						Name:    appNamespaceName,
					},
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key1": "value1", // invalid label selector
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/metadata/labels/new-label",
									Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
								},
							},
						},
					},
				},
			},
			OverrideHash: []byte("123"),
		},
	}
	Expect(k8sClient.Create(ctx, &validClusterResourceOverrideSnapshot)).Should(Succeed(), "Failed to create the cro-1")
	By(fmt.Sprintf("Cluster resource override snapshot %s created", validClusterResourceOverrideSnapshotName))

	validResourceOverrideSnapshot = placementv1beta1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      validResourceOverrideSnapshotName,
			Namespace: appNamespaceName,
			Labels: map[string]string{
				placementv1beta1.IsLatestSnapshotLabel: "true",
			},
		},
		Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
			OverrideSpec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelector{
					{
						Group:   "test.kubernetes-fleet.io",
						Version: "v1alpha1",
						Kind:    "TestResource",
						Name:    "random-test-resource",
					},
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{}, // select all members
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpReplace,
									Path:     "/spec/foo",
									Value:    apiextensionsv1.JSON{Raw: []byte(`"foo2"`)},
								},
							},
						},
					},
				},
			},
			OverrideHash: []byte("123"),
		},
	}
	Expect(k8sClient.Create(ctx, &validResourceOverrideSnapshot)).Should(Succeed(), "Failed to create the ro-1")
	By(fmt.Sprintf("Resource override snapshot %s created", validResourceOverrideSnapshotName))

	invalidClusterResourceOverrideSnapshot = placementv1beta1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: invalidClusterResourceOverrideSnapshotName,
			Labels: map[string]string{
				placementv1beta1.IsLatestSnapshotLabel: "true",
			},
		},
		Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
			OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					{
						Group:   utils.NamespaceGVK.Group,
						Version: utils.NamespaceGVK.Version,
						Kind:    utils.NamespaceGVK.Kind,
						Name:    appNamespaceName,
					},
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"override": "true",
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpAdd,
									Path:     "/invalid/path",
									Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
								},
							},
						},
					},
				},
			},
			OverrideHash: []byte("123"),
		},
	}
	Expect(k8sClient.Create(ctx, &invalidClusterResourceOverrideSnapshot)).Should(Succeed(), "Failed to create the cro-2")
	By(fmt.Sprintf("Invalid cluster resource override snapshot %s created", invalidClusterResourceOverrideSnapshotName))
}

var _ = AfterSuite(func() {
	defer klog.Flush()

	Expect(k8sClient.Delete(ctx, &validClusterResourceOverrideSnapshot)).Should(Succeed(), "Failed to delete the cro-1")
	Expect(k8sClient.Delete(ctx, &validResourceOverrideSnapshot)).Should(Succeed(), "Failed to delete the ro-1")
	Expect(k8sClient.Delete(ctx, &invalidClusterResourceOverrideSnapshot)).Should(Succeed(), "Failed to delete the cro-2")
	Expect(k8sClient.Delete(ctx, &appNamespace)).Should(Succeed(), "Failed to delete app namespace")

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

	By("Read TestResource CR")
	rawByte, err = os.ReadFile("../../../test/manifests/test-resource.yaml")
	Expect(err).Should(Succeed())
	testResource, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read want overridden TestResource CR")
	rawByte, err = os.ReadFile("manifests/test-resource-overriden.yaml")
	Expect(err).Should(Succeed())
	wantOverriddenTestResource, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testConfigMap resource")
	rawByte, err = os.ReadFile("manifests/test-configmap.yaml")
	Expect(err).Should(Succeed())
	testConfigMap, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testResourceEnvelope resource")
	rawByte, err = os.ReadFile("manifests/test-resource-envelope.yaml")
	Expect(err).Should(Succeed())
	testResourceEnvelope, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testResourceEnvelope2 resource")
	rawByte, err = os.ReadFile("manifests/test-resource-envelope2.yaml")
	Expect(err).Should(Succeed())
	testResourceEnvelope2, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testClusterScopedEnvelope resource")
	rawByte, err = os.ReadFile("manifests/test-clusterscoped-envelope.yaml")
	Expect(err).Should(Succeed())
	testClusterScopedEnvelope, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read PodDisruptionBudget")
	rawByte, err = os.ReadFile("manifests/test_pdb.yaml")
	Expect(err).Should(Succeed())
	testPdb, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read ResourceQuota")
	rawByte, err = os.ReadFile("manifests/resourcequota.yaml")
	Expect(err).Should(Succeed())
	testResourceQuotaContent, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read ResourceQuota2")
	rawByte, err = os.ReadFile("manifests/resourcequota2.yaml")
	Expect(err).Should(Succeed())
	testResourceQuota2Content, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testWebhookContent")
	rawByte, err = os.ReadFile("manifests/webhook.yaml")
	Expect(err).Should(Succeed())
	testWebhookContent, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())

	By("Read testClusterRoleContent")
	rawByte, err = os.ReadFile("manifests/clusterrole.yaml")
	Expect(err).Should(Succeed())
	testClusterRoleContent, err = yaml.ToJSON(rawByte)
	Expect(err).Should(Succeed())
}
