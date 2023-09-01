/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testutils "go.goms.io/fleet/test/e2e/utils"
)

var (
	reservedNamespaces = []*corev1.Namespace{
		fleetSystemNamespace,
		kubeSystemNamespace,
		memberNamespace,
	}
	testGroups = []string{"system:authenticated"}
)

const (
	testUser        = "test-user"
	testKey         = "test-key"
	testValue       = "test-value"
	testRole        = "wh-test-role"
	testPod         = "test-pod"
	testService     = "test-service"
	testSecret      = "test-secret"
	testDaemonSet   = "test-daemon-set"
	testDeployment  = "test-deployment"
	testReplicaSet  = "test-replica-set"
	testConfigMap   = "test-config-map"
	testRoleBinding = "wh-test-role-binding"
	testCronJob     = "test-cron-job"
	testJob         = "test-job"
	fleetSystemNS   = "fleet-system"
	kubeSystemNS    = "kube-system"

	crdStatusErrFormat              = `user: %s in groups: %v is not allowed to modify fleet CRD: %+v`
	resourceStatusErrFormat         = `user: %s in groups: %v is not allowed to modify resource %s: %+v`
	imcStatusUpdateNotAllowedFormat = "user: %s in groups: %v is not allowed to update IMC status: %+v"
)

var _ = Describe("Fleet's Hub cluster webhook tests", func() {
	Context("Pod validation webhook", func() {
		It("should admit operations on Pods within reserved namespaces", func() {
			for _, reservedNamespace := range reservedNamespaces {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: reservedNamespace.Name}
				nginxPod := &corev1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Pod",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      objKey.Name,
						Namespace: objKey.Namespace,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:1.14.2",
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										Protocol:      corev1.ProtocolTCP,
										ContainerPort: 80,
									},
								},
							},
						},
					},
				}

				By(fmt.Sprintf("expecting admission of operation CREATE of Pod in reserved namespace %s", reservedNamespace.Name))
				Expect(HubCluster.KubeClient.Create(ctx, nginxPod)).Should(Succeed())

				By(fmt.Sprintf("expecting admission of operation UPDATE of Pod in reserved namespace %s", reservedNamespace.Name))
				var podV2 *corev1.Pod
				Eventually(func(g Gomega) error {
					var currentPod corev1.Pod
					g.Expect(HubCluster.KubeClient.Get(ctx, objKey, &currentPod)).Should(Succeed())
					podV2 = currentPod.DeepCopy()
					podV2.Labels = map[string]string{utils.RandStr(): utils.RandStr()}
					return HubCluster.KubeClient.Update(ctx, podV2)
				}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

				By(fmt.Sprintf("expecting admission of operation DELETE of Pod in reserved namespace %s", reservedNamespace.Name))
				Expect(HubCluster.KubeClient.Delete(ctx, nginxPod)).Should(Succeed())
			}
		})
		It("should deny create operation on Pods within any non-reserved namespace", func() {
			rndNs := corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind:       "namespace",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.RandStr(),
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &rndNs)).Should(Succeed())

			nginxPod := &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.RandStr(),
					Namespace: rndNs.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.14.2",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			}

			By(fmt.Sprintf("expecting denial of operation CREATE of Pod in non-reserved namespace %s", rndNs.Name))
			err := HubCluster.KubeClient.Create(ctx, nginxPod)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create Pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook.*denied the request.*`))
		})
	})
	Context("ClusterResourcePlacement validation webhook", func() {
		var createdCRP fleetv1alpha1.ClusterResourcePlacement
		BeforeEach(func() {
			validCRP := fleetv1alpha1.ClusterResourcePlacement{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterResourcePlacement",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.RandStr(),
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    utils.RandStr(),
						},
					},
				},
			}
			By("expecting admission of operation CREATE of a valid ClusterResourcePlacement")
			Expect(HubCluster.KubeClient.Create(ctx, &validCRP)).Should(Succeed())

			// Get the created CRP
			Eventually(func() error {
				if err := HubCluster.KubeClient.Get(ctx, client.ObjectKey{Name: validCRP.Name}, &createdCRP); err != nil {
					return err
				}
				// check conditions to infer we have latest
				if len(createdCRP.Status.Conditions) == 0 {
					return fmt.Errorf("failed to get crp condition, want not empty")
				}
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
		AfterEach(func() {
			By("expecting admission of operation DELETE of ClusterResourcePlacement")
			Expect(HubCluster.KubeClient.Delete(ctx, &createdCRP)).Should(Succeed())
		})
		It("should admit write operations for valid ClusterResourcePlacement resources", func() {
			// create & delete write operations are handled within the BeforeEach & AfterEach functions.
			By("expecting admission of operation UPDATE with a valid ClusterResourcePlacement")
			createdCRP.Spec.ResourceSelectors[0].Name = utils.RandStr()
			Expect(HubCluster.KubeClient.Update(ctx, &createdCRP)).Should(Succeed())
		})
		It("should deny write operations for ClusterResourcePlacements which specify both a label & name within a resource selector", func() {
			invalidCRP := fleetv1alpha1.ClusterResourcePlacement{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterResourcePlacement",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.RandStr(),
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "core",
							Version: "v1",
							Kind:    "Pod",
							Name:    utils.RandStr(),
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"SomeKey": "SomeValue"},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE with the invalid ClusterResourcePlacement")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))

			By("expecting denial of operation UPDATE with the invalid ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))
		})
		It("should deny write operations for ClusterResourcePlacements which specify an invalid cluster label selector", func() {
			invalidCRP := fleetv1alpha1.ClusterResourcePlacement{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterResourcePlacement",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.RandStr(),
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					Policy: &fleetv1alpha1.PlacementPolicy{
						ClusterNames: nil,
						Affinity: &fleetv1alpha1.Affinity{
							ClusterAffinity: &fleetv1alpha1.ClusterAffinity{
								ClusterSelectorTerms: []fleetv1alpha1.ClusterSelectorTerm{
									{
										LabelSelector: metav1.LabelSelector{
											MatchLabels: nil,
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "SomeKey",
													Operator: "invalid-operator",
													Values:   []string{"SomeValue"},
												},
											},
										},
									},
								},
							},
						},
					},
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    utils.RandStr(),
						},
					},
				},
			}

			By("expecting denial of operation CREATE of ClusterResourcePlacement")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", invalidCRP.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]))))

			By("expecting denial of operation UPDATE of ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", invalidCRP.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]))))
		})
		It("should deny write operations for ClusterResourcePlacements which specify an invalid resource label selector", func() {
			invalidCRP := fleetv1alpha1.ClusterResourcePlacement{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterResourcePlacement",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.RandStr(),
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "namespace",
							Name:    "",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: nil,
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "SomeKey",
										Operator: "invalid-operator",
										Values:   []string{"SomeValue"},
									},
								},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE with the invalid ClusterResourcePlacement")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in resource selector %+v is invalid:", invalidCRP.Spec.ResourceSelectors[0]))))

			By("expecting denial of operation UPDATE with the invalid ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in resource selector %+v is invalid:", invalidCRP.Spec.ResourceSelectors[0]))))
		})
		It("should deny WRITE operations for ClusterResourcePlacements which specify an invalid GVK within the resource selector", func() {
			invalidCRP := fleetv1alpha1.ClusterResourcePlacement{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ClusterResourcePlacement",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: utils.RandStr(),
				},
				Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
						{
							Group:         utils.RandStr(),
							Version:       utils.RandStr(),
							Kind:          utils.RandStr(),
							Name:          utils.RandStr(),
							LabelSelector: &metav1.LabelSelector{},
						},
					},
				},
			}

			invalidGVK := metav1.GroupVersionKind{
				Group:   invalidCRP.Spec.ResourceSelectors[0].Group,
				Version: invalidCRP.Spec.ResourceSelectors[0].Version,
				Kind:    invalidCRP.Spec.ResourceSelectors[0].Kind,
			}

			By("expecting denial of operation CREATE with the invalid ClusterResourcePlacement")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %s", invalidGVK))))

			By("expecting denial of operation UPDATE with the invalid ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %s", invalidGVK))))
		})
	})
	Context("ReplicaSet validation webhook", func() {
		It("should admit operation CREATE on ReplicaSets in reserved namespaces", func() {
			for _, ns := range reservedNamespaces {
				rsName := utils.RandStr()
				rs := &appsv1.ReplicaSet{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ReplicaSet",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      rsName,
						Namespace: ns.Name,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas:        pointer.Int32(1),
						MinReadySeconds: 1,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"web"},
								},
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "web"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx",
										Ports: []corev1.ContainerPort{
											{
												Name:          "http",
												Protocol:      corev1.ProtocolTCP,
												ContainerPort: 80,
											},
										},
									},
								},
							},
						},
					},
				}

				By(fmt.Sprintf("expecting admission of operation CREATE of ReplicaSet in reserved namespace %s", ns.Name))
				Expect(HubCluster.KubeClient.Create(ctx, rs)).Should(Succeed())

				By("clean up created replica set")
				Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rsName, Namespace: ns.Name}, rs)).Should(Succeed())
				Expect(HubCluster.KubeClient.Delete(ctx, rs))
				Eventually(func() bool {
					return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, rs))
				}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
			}
		})
		It("should deny CREATE operation on ReplicaSets in a non-reserved namespace", func() {
			rs := &appsv1.ReplicaSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ReplicaSet",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.RandStr(),
					Namespace: "default",
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas:        pointer.Int32(1),
					MinReadySeconds: 1,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{},
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "app",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"web"},
							},
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "web"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: 80,
										},
									},
								},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of ReplicaSet")
			err := HubCluster.KubeClient.Create(ctx, rs)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ReplicaSet call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.replicaset.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(fmt.Sprintf("ReplicaSet %s/%s creation is disallowed in the fleet hub cluster", rs.Namespace, rs.Name)))
		})
	})
})

var _ = Describe("Fleet's CRD Resource Handler webhook tests", func() {
	Context("CRD validation webhook", func() {
		It("should deny CREATE operation on Fleet CRD for user not in system:masters group", func() {
			var crd v1.CustomResourceDefinition
			Expect(utils.GetObjectFromManifest("./charts/hub-agent/templates/crds/fleet.azure.com_clusterresourceplacements.yaml", &crd)).Should(Succeed())

			By("expecting denial of operation CREATE of CRD")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &crd)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(crdStatusErrFormat, testUser, testGroups, types.NamespacedName{Name: crd.Name})))
		})

		It("should deny UPDATE operation on Fleet CRD for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var crd v1.CustomResourceDefinition
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).Should(Succeed())
				By("update labels in CRD")
				labels := crd.GetLabels()
				labels[testKey] = testValue
				crd.SetLabels(labels)
				By("expecting denial of operation UPDATE of CRD")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &crd)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(crdStatusErrFormat, testUser, testGroups, types.NamespacedName{Name: crd.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on Fleet CRD for user not in system:masters group", func() {
			crd := v1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "works.multicluster.x-k8s.io",
				},
			}
			By("expecting denial of operation Delete of CRD")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &crd)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(crdStatusErrFormat, testUser, testGroups, types.NamespacedName{Name: crd.Name})))
		})

		It("should allow UPDATE operation on Fleet CRDs even if user in system:masters group", func() {
			var crd v1.CustomResourceDefinition
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).Should(Succeed())

				By("update labels in CRD")
				labels := crd.GetLabels()
				labels[testKey] = testValue
				crd.SetLabels(labels)

				By("expecting denial of operation UPDATE of CRD")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &crd)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			Eventually(func(g Gomega) error {
				By("remove new label added for test")
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).To(Succeed())
				labels := crd.GetLabels()
				delete(labels, testKey)
				crd.SetLabels(labels)
				return HubCluster.KubeClient.Update(ctx, &crd)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow CREATE operation on Other CRDs", func() {
			var crd v1.CustomResourceDefinition
			Expect(utils.GetObjectFromManifest("./test/integration/manifests/resources/test_clonesets_crd.yaml", &crd)).Should(Succeed())

			By("expecting error to be nil")
			Expect(HubCluster.KubeClient.Create(ctx, &crd)).To(Succeed())

			By("delete clone set CRD")
			Expect(HubCluster.KubeClient.Delete(ctx, &crd)).To(Succeed())
		})
	})
})

var _ = Describe("Fleet's CR Resource Handler webhook tests", func() {
	Context("CR validation webhook", func() {
		It("should deny CREATE operation on member cluster CR for user not in system:masters group", func() {
			mc := fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-member-cluster",
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					State: fleetv1alpha1.ClusterStateJoin,
					Identity: rbacv1.Subject{
						Kind:      "User",
						APIGroup:  "",
						Name:      "test-subject",
						Namespace: fleetSystemNS,
					},
				},
			}

			By("expecting denial of operation CREATE of member cluster")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &mc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "MemberCluster", types.NamespacedName{Name: mc.Name})))
		})

		It("should deny UPDATE operation on member cluster CR for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var mc fleetv1alpha1.MemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())

				By("update member cluster spec")
				mc.Spec.State = fleetv1alpha1.ClusterStateLeave

				By("expecting denial of operation UPDATE of member cluster")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "MemberCluster", types.NamespacedName{Name: mc.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on member cluster CR for user not in system:masters group", func() {
			mc := fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: MemberCluster.ClusterName,
				},
			}

			By("expecting denial of operation DELETE of member cluster")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &mc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "MemberCluster", types.NamespacedName{Name: mc.Name})))
		})

		It("should allow update operation on member cluster CR labels for any user", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update labels in member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				labels := make(map[string]string)
				labels[testKey] = testValue
				mc.SetLabels(labels)
				return HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			By("remove new label added for test, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				labels := mc.GetLabels()
				delete(labels, testKey)
				mc.SetLabels(labels)
				return HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow update operation on member cluster CR annotations for any user", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update annotations in member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				annotations := make(map[string]string)
				annotations[testKey] = testValue
				mc.SetLabels(annotations)
				return HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			By("remove new annotation added for test, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				annotations := mc.GetLabels()
				delete(annotations, testKey)
				mc.SetLabels(annotations)
				return HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow update operation on member cluster CR spec for user in system:masters group", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update spec of member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				mc.Spec.HeartbeatPeriodSeconds = 31
				return HubCluster.KubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			By("revert spec change made for test, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				mc.Spec.HeartbeatPeriodSeconds = 30
				return HubCluster.KubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow update operation on member cluster CR status for user in system:masters group", func() {
			var mc fleetv1alpha1.MemberCluster
			var reason string
			By("update status of member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				g.Expect(mc.Status.Conditions).ToNot(BeEmpty())
				reason = mc.Status.Conditions[0].Reason
				mc.Status.Conditions[0].Reason = "update"
				return HubCluster.KubeClient.Status().Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())

			By("revert spec change made for test, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				mc.Status.Conditions[0].Reason = reason
				return HubCluster.KubeClient.Status().Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny member cluster CR status update for user not in system:master group or a whitelisted user", func() {
			Eventually(func(g Gomega) error {
				var mc fleetv1alpha1.MemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())
				By("update status of member cluster")
				g.Expect(mc.Status.Conditions).ToNot(BeEmpty())
				mc.Status.Conditions[0].Reason = "update"
				By("expecting denial UPDATE of member cluster status")
				err := HubCluster.ImpersonateKubeClient.Status().Update(ctx, &mc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update member cluster status call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "MemberCluster", types.NamespacedName{Name: mc.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny CREATE operation on internal member cluster CR for user not in system:masters group", func() {
			imc := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mc",
					Namespace: "fleet-member-test-mc",
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: 30,
				},
			}

			By("expecting denial of operation CREATE of Internal Member Cluster")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &imc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "InternalMemberCluster", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
		})

		It("should deny UPDATE operation on internal member cluster CR for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"}, &imc)).Should(Succeed())
				imc.Spec.HeartbeatPeriodSeconds = 25

				By("expecting denial of operation UPDATE of Internal Member Cluster")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &imc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "InternalMemberCluster", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on internal member cluster CR for user not in system:masters group", func() {
			var imc fleetv1alpha1.InternalMemberCluster
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"}, &imc)).Should(Succeed())

			By("expecting denial of operation UPDATE of Internal Member Cluster")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &imc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "InternalMemberCluster", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
		})

		It("should deny UPDATE operation on internal member cluster CR status for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"}, &imc)).Should(Succeed())
				imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU: {
								Format: "testFormat",
							},
						},
					},
					AgentStatus: nil,
				}
				By("expecting denial of operation UPDATE of internal member cluster CR status")
				err := HubCluster.KubeClient.Status().Update(ctx, &imc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(imcStatusUpdateNotAllowedFormat, "kubernetes-admin", []string{"system:masters", "system:authenticated"}, types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow UPDATE operation on internal member cluster CR spec for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"}, &imc)).Should(Succeed())
				imc.Spec.HeartbeatPeriodSeconds = 25

				By("expecting successful UPDATE of Internal Member Cluster Spec")
				return HubCluster.KubeClient.Update(ctx, &imc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow UPDATE operation on internal member cluster CR status for user in mc identity", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-mc", Namespace: "fleet-member-test-mc"}, &imc)).Should(Succeed())
				imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
					ResourceUsage: fleetv1alpha1.ResourceUsage{
						Capacity: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU: {
								Format: "testFormat",
							},
						},
					},
					AgentStatus: nil,
				}
				By("expecting successful UPDATE of Internal Member Cluster Status")
				return HubCluster.ImpersonateKubeClient.Status().Update(ctx, &imc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})

var _ = Describe("Fleet's Namespaced Resource Handler webhook tests", func() {
	Context("fleet guard rail e2e for role", func() {
		BeforeEach(func() {
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRole,
					Namespace: memberNamespace.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"*"},
						APIGroups: []string{"*"},
						Resources: []string{"*"},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &r)).Should(Succeed())
		})

		AfterEach(func() {
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRole,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &r)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: r.Name, Namespace: r.Namespace}, &r))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE operation on role for user not in system:masters group", func() {
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRole,
					Namespace: memberNamespace.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"*"},
						APIGroups: []string{rbacv1.SchemeGroupVersion.Group},
						Resources: []string{"*"},
					},
				},
			}

			By("expecting denial of operation CREATE of role")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &r)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create role call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Role", types.NamespacedName{Name: r.Name, Namespace: r.Namespace})))
		})

		It("should deny UPDATE operation on role for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var r rbacv1.Role
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testRole, Namespace: memberNamespace.Name}, &r)).Should(Succeed())
				By("update role")
				labels := make(map[string]string)
				labels[testKey] = testValue
				r.SetLabels(labels)
				By("expecting denial of operation UPDATE of role")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &r)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update role call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Role", types.NamespacedName{Name: r.Name, Namespace: r.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on role for user not in system:masters group", func() {
			r := rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRole,
					Namespace: memberNamespace.Name,
				},
			}

			By("expecting denial of operation DELETE of role")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &r)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete role call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Role", types.NamespacedName{Name: r.Name, Namespace: r.Namespace})))
		})

		It("should allow update operation on role for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var r rbacv1.Role
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testRole, Namespace: memberNamespace.Name}, &r)).Should(Succeed())
				By("update labels in Role")
				labels := make(map[string]string)
				labels[testKey] = testValue
				r.SetLabels(labels)

				By("expecting successful UPDATE of role")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &r)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for role binding", func() {
		BeforeEach(func() {
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRoleBinding,
					Namespace: kubeSystemNS,
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: rbacv1.GroupName,
						Kind:     "User",
						Name:     "test-user",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     testRole,
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &rb)).Should(Succeed())
		})

		AfterEach(func() {
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRoleBinding,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &rb)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}, &rb))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE operation on role binding for user not in system:masters group", func() {
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRoleBinding,
					Namespace: kubeSystemNS,
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: rbacv1.GroupName,
						Kind:     "User",
						Name:     testUser,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     testRole,
				},
			}

			By("expecting denial of operation CREATE of role binding")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &rb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create role binding call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "RoleBinding", types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace})))
		})

		It("should deny UPDATE operation on role binding for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rb rbacv1.RoleBinding
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testRoleBinding, Namespace: kubeSystemNS}, &rb)).Should(Succeed())
				By("update role")
				labels := make(map[string]string)
				labels[testKey] = testValue
				rb.SetLabels(labels)
				By("expecting denial of operation UPDATE of role binding")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &rb)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update role binding call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "RoleBinding", types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on role binding for user not in system:masters group", func() {
			rb := rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRoleBinding,
					Namespace: kubeSystemNS,
				},
			}

			By("expecting denial of operation DELETE of role binding")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &rb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete role binding call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "RoleBinding", types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace})))
		})

		It("should allow update operation on role binding for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rb rbacv1.RoleBinding
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testRoleBinding, Namespace: kubeSystemNS}, &rb)).Should(Succeed())
				By("update labels in role binding")
				labels := make(map[string]string)
				labels[testKey] = testValue
				rb.SetLabels(labels)

				By("expecting successful UPDATE of role binding")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &rb)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for pod", func() {
		BeforeEach(func() {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPod,
					Namespace: kubeSystemNS,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           "busybox",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Name:            "busybox",
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &pod)).Should(Succeed())
		})
		AfterEach(func() {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPod,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &pod)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &pod))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE pod operation for user not in system:masters group", func() {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPod,
					Namespace: kubeSystemNS,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           "busybox",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Name:            "busybox",
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			}

			By("expecting denial of operation CREATE of pod")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &pod)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Pod", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})))
		})

		It("should deny UPDATE pod operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var pod corev1.Pod
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testPod, Namespace: kubeSystemNS}, &pod)).Should(Succeed())
				pod.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of pod")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &pod)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Pod", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE pod operation for user not in system:masters group", func() {
			var pod corev1.Pod
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testPod, Namespace: kubeSystemNS}, &pod)).Should(Succeed())
			By("expecting denial of operation DELETE of pod")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &pod)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete pod call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Pod", types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})))
		})

		It("should allow update operation on pod for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var pod corev1.Pod
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testPod, Namespace: kubeSystemNS}, &pod)).Should(Succeed())
				By("update labels in pod")
				labels := make(map[string]string)
				labels[testKey] = testValue
				pod.SetLabels(labels)

				By("expecting successful UPDATE of pod")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &pod)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for service", func() {
		BeforeEach(func() {
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testService,
					Namespace: fleetSystemNS,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							TargetPort: intstr.IntOrString{
								IntVal: 8080,
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &service)).Should(Succeed())
		})
		AfterEach(func() {
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testService,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &service)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &service))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

		})

		It("should deny CREATE service operation for user not in system:masters group", func() {
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service1",
					Namespace: fleetSystemNS,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Protocol: corev1.ProtocolTCP,
							Port:     80,
							TargetPort: intstr.IntOrString{
								IntVal: 8080,
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of service")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &service)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create service call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Service", types.NamespacedName{Name: service.Name, Namespace: service.Namespace})))
		})

		It("should deny UPDATE service operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var service corev1.Service
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testService, Namespace: fleetSystemNS}, &service)).Should(Succeed())
				service.Spec.Ports[0].Port = 81
				By("expecting denial of operation UPDATE of service")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &service)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update service call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Service", types.NamespacedName{Name: service.Name, Namespace: service.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE service operation for user not in system:masters group", func() {
			var service corev1.Service
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testService, Namespace: fleetSystemNS}, &service)).Should(Succeed())
			By("expecting denial of operation DELETE of service")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &service)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete service call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Service", types.NamespacedName{Name: service.Name, Namespace: service.Namespace})))
		})

		It("should allow update operation on service for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var s corev1.Service
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testService, Namespace: fleetSystemNS}, &s)).Should(Succeed())
				By("update labels in service")
				labels := make(map[string]string)
				labels[testKey] = testValue
				s.SetLabels(labels)

				By("expecting successful UPDATE of service")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &s)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for config map", func() {
		BeforeEach(func() {
			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testConfigMap,
					Namespace: memberNamespace.Name,
				},
				Data: map[string]string{"test-key": "test-value"},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &cm)).Should(Succeed())
		})
		AfterEach(func() {
			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testConfigMap,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &cm)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, &cm))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE config map operation for user not in system:masters group", func() {
			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testConfigMap,
					Namespace: memberNamespace.Name,
				},
				Data: map[string]string{"test-key": "test-value"},
			}

			By("expecting denial of operation CREATE of config map")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &cm)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create config map call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ConfigMap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})))
		})

		It("should deny UPDATE config map operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cm corev1.ConfigMap
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testConfigMap, Namespace: memberNamespace.Name}, &cm)).Should(Succeed())
				cm.Data["test-key"] = "test-value1"
				By("expecting denial of operation UPDATE of config map")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &cm)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update config map call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ConfigMap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE config map operation for user not in system:masters group", func() {
			var cm corev1.ConfigMap
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testConfigMap, Namespace: memberNamespace.Name}, &cm)).Should(Succeed())
			By("expecting denial of operation DELETE of config map")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &cm)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete config map call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ConfigMap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})))
		})

		It("should allow update operation on config map for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cm corev1.ConfigMap
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testConfigMap, Namespace: memberNamespace.Name}, &cm)).Should(Succeed())
				By("update labels in config map")
				labels := make(map[string]string)
				labels[testKey] = testValue
				cm.SetLabels(labels)

				By("expecting successful UPDATE of config map")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &cm)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for secret", func() {
		BeforeEach(func() {
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecret,
					Namespace: memberNamespace.Name,
				},
				Data: map[string][]byte{"test-key": []byte("dGVzdA==")},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &s)).Should(Succeed())
		})
		AfterEach(func() {
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecret,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &s)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: s.Namespace}, &s))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE secret operation for user not in system:masters group", func() {
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecret,
					Namespace: memberNamespace.Name,
				},
				Data: map[string][]byte{"test-key": []byte("dGVzdA==")},
			}

			By("expecting denial of operation CREATE of secret")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &s)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create secret call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})))
		})

		It("should deny UPDATE secret operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var s corev1.Secret
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testSecret, Namespace: memberNamespace.Name}, &s)).Should(Succeed())
				s.Data["test-key"] = []byte("dmFsdWUtMg0KDQo=")
				By("expecting denial of operation UPDATE of secret")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &s)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update secret call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE secret operation for user not in system:masters group", func() {
			var s corev1.Secret
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testSecret, Namespace: memberNamespace.Name}, &s)).Should(Succeed())
			By("expecting denial of operation DELETE of secret")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &s)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete secret call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Secret", types.NamespacedName{Name: s.Name, Namespace: s.Namespace})))
		})

		It("should allow update operation on secret for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var s corev1.Secret
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testSecret, Namespace: memberNamespace.Name}, &s)).Should(Succeed())
				By("update labels in secret")
				labels := make(map[string]string)
				labels[testKey] = testValue
				s.SetLabels(labels)
				By("expecting successful UPDATE of secret")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &s)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for deployment", func() {
		BeforeEach(func() {
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDeployment,
					Namespace: kubeSystemNS,
					Labels:    map[string]string{"app": "busybox"},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "busybox"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "busybox"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &d)).Should(Succeed())
		})
		AfterEach(func() {
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDeployment,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &d)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, &d))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE deployment operation for user not in system:masters group", func() {
			d := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDeployment,
					Namespace: kubeSystemNS,
					Labels:    map[string]string{"app": "busybox"},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "busybox"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "busybox"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of deployment")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &d)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
		})

		It("should deny UPDATE deployment operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var d appsv1.Deployment
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testDeployment, Namespace: kubeSystemNS}, &d)).Should(Succeed())
				d.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of deployment")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &d)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE deployment operation for user not in system:masters group", func() {
			var d appsv1.Deployment
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testDeployment, Namespace: kubeSystemNS}, &d)).Should(Succeed())
			By("expecting denial of operation DELETE of deployment")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &d)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete deployment call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Deployment", types.NamespacedName{Name: d.Name, Namespace: d.Namespace})))
		})

		It("should allow update operation on deployment for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var d appsv1.Deployment
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testDeployment, Namespace: kubeSystemNS}, &d)).Should(Succeed())
				By("update labels in deployment")
				labels := make(map[string]string)
				labels[testKey] = testValue
				d.SetLabels(labels)

				By("expecting successful UPDATE of deployment")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &d)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for replica set", func() {
		BeforeEach(func() {
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testReplicaSet,
					Namespace: memberNamespace.Name,
					Labels:    map[string]string{"tier": "frontend"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "frontend"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"tier": "frontend"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &rs)).Should(Succeed())
		})
		AfterEach(func() {
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testReplicaSet,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &rs)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, &rs))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE replica set operation for user not in system:masters group", func() {
			rs := appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testReplicaSet,
					Namespace: memberNamespace.Name,
					Labels:    map[string]string{"tier": "frontend"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: pointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "frontend"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"tier": "frontend"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "busybox",
								},
							},
						},
					},
				},
			}

			By("expecting denial of operation CREATE of replica set")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &rs)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create replica set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ReplicaSet", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace})))
		})

		It("should deny UPDATE replica set operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rs appsv1.ReplicaSet
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testReplicaSet, Namespace: memberNamespace.Name}, &rs)).Should(Succeed())
				rs.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of replica set")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &rs)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update replica set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ReplicaSet", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE replica set operation for user not in system:masters group", func() {
			var rs appsv1.ReplicaSet
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testReplicaSet, Namespace: memberNamespace.Name}, &rs)).Should(Succeed())
			By("expecting denial of operation DELETE of replica set")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &rs)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete replica set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "ReplicaSet", types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace})))
		})

		It("should allow update operation on replica set for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var rs appsv1.ReplicaSet
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testReplicaSet, Namespace: memberNamespace.Name}, &rs)).Should(Succeed())
				By("update labels in replica set")
				labels := make(map[string]string)
				labels[testKey] = testValue
				rs.SetLabels(labels)

				By("expecting successful UPDATE of replica set")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &rs)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for daemon set", func() {
		BeforeEach(func() {
			hostPath := &corev1.HostPathVolumeSource{
				Path: "/var/log",
			}
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDaemonSet,
					Namespace: fleetSystemNS,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"name": "fluentd-elasticsearch"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"name": "fluentd-elasticsearch"},
						},
						Spec: corev1.PodSpec{
							Tolerations: []corev1.Toleration{
								{
									Key:      "node-role.kubernetes.io/control-plane",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
								{
									Key:      "node-role.kubernetes.io/master",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "fluentd-elasticsearch",
									Image: "quay.io/fluentd_elasticsearch/fluentd:v2.5.2",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "varlog",
									VolumeSource: corev1.VolumeSource{
										HostPath: hostPath,
									},
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &ds)).Should(Succeed())
		})
		AfterEach(func() {
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDaemonSet,
					Namespace: fleetSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &ds)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, &ds))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE daemon set operation for user not in system:masters group", func() {
			hostPath := &corev1.HostPathVolumeSource{
				Path: "/var/log",
			}
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testDaemonSet,
					Namespace: fleetSystemNS,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"name": "fluentd-elasticsearch"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"name": "fluentd-elasticsearch"},
						},
						Spec: corev1.PodSpec{
							Tolerations: []corev1.Toleration{
								{
									Key:      "node-role.kubernetes.io/control-plane",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
								{
									Key:      "node-role.kubernetes.io/master",
									Operator: corev1.TolerationOpExists,
									Effect:   corev1.TaintEffectNoSchedule,
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "fluentd-elasticsearch",
									Image: "quay.io/fluentd_elasticsearch/fluentd:v2.5.2",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("200Mi"),
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "varlog",
									VolumeSource: corev1.VolumeSource{
										HostPath: hostPath,
									},
								},
							},
						},
					},
				},
			}
			By("expecting denial of operation CREATE of daemon set")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &ds)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create daemon set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "DaemonSet", types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace})))
		})

		It("should deny UPDATE daemon set operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var ds appsv1.DaemonSet
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testDaemonSet, Namespace: fleetSystemNS}, &ds)).Should(Succeed())
				ds.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of daemon set")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &ds)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update dameon set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "DaemonSet", types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE daemon set operation for user not in system:masters group", func() {
			var ds appsv1.DaemonSet
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testDaemonSet, Namespace: fleetSystemNS}, &ds)).Should(Succeed())
			By("expecting denial of operation DELETE of daemon set")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &ds)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete daemon set call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "DaemonSet", types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace})))
		})

		It("should allow update operation on daemon set for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var ds appsv1.DaemonSet
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testDaemonSet, Namespace: fleetSystemNS}, &ds)).Should(Succeed())
				By("update labels in daemon set")
				labels := make(map[string]string)
				labels[testKey] = testValue
				ds.SetLabels(labels)

				By("expecting successful UPDATE of daemon set")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &ds)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for cronjob", func() {
		BeforeEach(func() {
			cj := batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCronJob,
					Namespace: kubeSystemNS,
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "* * * * *",
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Image:           "busybox",
											ImagePullPolicy: corev1.PullIfNotPresent,
											Name:            "cronjob-busybox",
										},
									},
									RestartPolicy: corev1.RestartPolicyOnFailure,
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &cj)).Should(Succeed())
		})
		AfterEach(func() {
			cj := batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCronJob,
					Namespace: kubeSystemNS,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &cj)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace}, &cj))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE cronjob operation for user not in system:masters group", func() {
			cj := batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCronJob,
					Namespace: kubeSystemNS,
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "* * * * *",
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Image:           "busybox",
											ImagePullPolicy: corev1.PullIfNotPresent,
											Name:            "cronjob-busybox",
										},
									},
									RestartPolicy: corev1.RestartPolicyOnFailure,
								},
							},
						},
					},
				},
			}
			By("expecting denial of operation CREATE of cronjob")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &cj)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create cronjob call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CronJob", types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace})))
		})

		It("should deny UPDATE cronjob operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cj batchv1.CronJob
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testCronJob, Namespace: kubeSystemNS}, &cj)).Should(Succeed())
				cj.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of cronjob")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &cj)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update cronjob call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CronJob", types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE cronjob operation for user not in system:masters group", func() {
			var cj batchv1.CronJob
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testCronJob, Namespace: kubeSystemNS}, &cj)).Should(Succeed())
			By("expecting denial of operation DELETE of cronjob")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &cj)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete cronjob call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "CronJob", types.NamespacedName{Name: cj.Name, Namespace: cj.Namespace})))
		})

		It("should allow update operation on cronjob for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var cj batchv1.CronJob
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testCronJob, Namespace: kubeSystemNS}, &cj)).Should(Succeed())
				By("update labels in cronjob")
				labels := make(map[string]string)
				labels[testKey] = testValue
				cj.SetLabels(labels)

				By("expecting successful UPDATE of cronjob")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &cj)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail e2e for job", func() {
		BeforeEach(func() {
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testJob,
					Namespace: memberNamespace.Name,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "job-busybox",
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &j)).Should(Succeed())
		})
		AfterEach(func() {
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testJob,
					Namespace: memberNamespace.Name,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &j)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: j.Name, Namespace: j.Namespace}, &j))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
		})

		It("should deny CREATE job operation for user not in system:masters group", func() {
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testJob,
					Namespace: memberNamespace.Name,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:           "busybox",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Name:            "job-busybox",
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}
			By("expecting denial of operation CREATE of job")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &j)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
		})

		It("should deny UPDATE job operation for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var j batchv1.Job
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testJob, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
				j.ObjectMeta.Labels = map[string]string{"test-key": "test-value"}
				By("expecting denial of operation UPDATE of job")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &j)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE job operation for user not in system:masters group", func() {
			var j batchv1.Job
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testJob, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
			By("expecting denial of operation DELETE of job")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &j)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete job call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Job", types.NamespacedName{Name: j.Name, Namespace: j.Namespace})))
		})

		It("should allow update operation on job for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var j batchv1.Job
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: testJob, Namespace: memberNamespace.Name}, &j)).Should(Succeed())
				By("update labels in job")
				labels := make(map[string]string)
				labels[testKey] = testValue
				j.SetLabels(labels)

				By("expecting successful UPDATE of job")
				// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
				return HubCluster.KubeClient.Update(ctx, &j)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})

var _ = Describe("Fleet's Reserved Namespace Handler webhook tests", func() {
	Context("deny requests to modify namespace with fleet/kube prefix", func() {
		It("should deny CREATE operation on namespace with fleet prefix for user not in system:masters group", func() {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fleet-namespace",
				},
			}
			By("expecting denial of operation CREATE of namespace")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &ns)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Namespace", types.NamespacedName{Name: ns.Name})))
		})

		It("should deny CREATE operation on namespace with kube prefix for user not in system:masters group", func() {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kube-namespace",
				},
			}
			By("expecting denial of operation CREATE of namespace")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &ns)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Namespace", types.NamespacedName{Name: ns.Name})))
		})

		It("should deny UPDATE operation on namespace with fleet prefix for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var ns corev1.Namespace
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: fleetSystemNS}, &ns)).Should(Succeed())
				ns.Spec.Finalizers[0] = "test-finalizer"
				By("expecting denial of operation UPDATE of namespace")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &ns)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Namespace", types.NamespacedName{Name: ns.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny UPDATE operation on namespace with kube prefix for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var ns corev1.Namespace
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: kubeSystemNS}, &ns)).Should(Succeed())
				ns.Spec.Finalizers[0] = "test-finalizer"
				By("expecting denial of operation UPDATE of namespace")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &ns)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Namespace", types.NamespacedName{Name: ns.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on namespace with fleet prefix for user not in system:masters group", func() {
			var ns corev1.Namespace
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: fleetSystemNS}, &ns)).Should(Succeed())
			By("expecting denial of operation DELETE of namespace")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &ns)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Namespace", types.NamespacedName{Name: ns.Name})))
		})

		It("should deny DELETE operation on namespace with kube prefix for user not in system:masters group", func() {
			var ns corev1.Namespace
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "kube-node-lease"}, &ns)).Should(Succeed())
			By("expecting denial of operation DELETE of namespace")
			// trying to delete kube-system/kube-public returns forbidden, looks like k8s intercepts the call before webhook. error returned namespaces "kube-system" is forbidden: this namespace may not be deleted
			// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiserver/pkg/admission/plugin/namespace/lifecycle/admission.go#L80
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &ns)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(resourceStatusErrFormat, testUser, testGroups, "Namespace", types.NamespacedName{Name: ns.Name})))
		})

		It("should allow create/update/delete operation on namespace without fleet/kube prefix for user not in system:masters group", func() {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}
			Expect(HubCluster.ImpersonateKubeClient.Create(ctx, &ns)).Should(Succeed())
			By("expecting successful UPDATE on namespace")
			Expect(HubCluster.ImpersonateKubeClient.Get(ctx, types.NamespacedName{Name: ns.Name}, &ns)).Should(Succeed())
			ns.Spec.Finalizers = []corev1.FinalizerName{"test-finalizer"}
			Expect(HubCluster.ImpersonateKubeClient.Update(ctx, &ns)).Should(Succeed())
			By("expecting successful DELETE of namespace")
			Expect(HubCluster.ImpersonateKubeClient.Delete(ctx, &ns)).Should(Succeed())
		})
	})
})
