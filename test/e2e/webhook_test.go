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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testUtils "go.goms.io/fleet/test/e2e/utils"
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
	testUser                   = "test-user"
	testKey                    = "test-key"
	testValue                  = "test-value"
	testUserClusterRole        = "test-user-cluster-role"
	testUserClusterRoleBinding = "test-user-cluster-role-binding"

	crdStatusErrFormat = `failed to validate user: %s in groups: %v to modify fleet CRD: %s`
	mcStatusErrFormat  = `failed to validate user: %s in groups: %v to modify member cluster CR: %s`
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
				Eventually(func() error {
					var currentPod corev1.Pod
					Expect(HubCluster.KubeClient.Get(ctx, objKey, &currentPod)).Should(Succeed())
					podV2 = currentPod.DeepCopy()
					podV2.Labels = map[string]string{utils.RandStr(): utils.RandStr()}
					return HubCluster.KubeClient.Update(ctx, podV2)
				}, testUtils.PollTimeout, testUtils.PollInterval).Should(Succeed())

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
			}, testUtils.PollTimeout, testUtils.PollInterval).Should(Succeed())
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
					ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{},
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
				rs := &appsv1.ReplicaSet{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ReplicaSet",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      utils.RandStr(),
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
	BeforeEach(func() {
		By("create cluster role to modify CRDs")
		cr := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRole,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{v1.SchemeGroupVersion.Group},
					Verbs:     []string{"*"},
					Resources: []string{"*"},
				},
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, &cr)).Should(Succeed())

		By("create cluster role binding for test-user to modify CRD")
		crb := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRoleBinding,
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
				Kind:     "ClusterRole",
				Name:     testUserClusterRole,
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, &crb)).Should(Succeed())
	})

	AfterEach(func() {
		By("remove cluster role binding")
		crb := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRoleBinding,
			},
		}
		Expect(HubCluster.KubeClient.Delete(ctx, &crb)).Should(Succeed())

		By("remove cluster role")
		cr := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRole,
			},
		}
		Expect(HubCluster.KubeClient.Delete(ctx, &cr)).Should(Succeed())
	})

	Context("CRD validation webhook", func() {
		It("should deny CREATE operation on Fleet CRD for user not in system:masters group", func() {
			var crd v1.CustomResourceDefinition
			Expect(utils.GetObjectFromManifest("./charts/hub-agent/templates/crds/fleet.azure.com_clusterresourceplacements.yaml", &crd)).Should(Succeed())

			By("expecting denial of operation CREATE of CRD")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &crd)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(crdStatusErrFormat, testUser, testGroups, crd.Name)))
		})

		It("should deny UPDATE operation on Fleet CRD for user not in system:masters group", func() {
			var crd v1.CustomResourceDefinition
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).Should(Succeed())

			By("update labels in CRD")
			labels := crd.GetLabels()
			labels[testKey] = testValue
			crd.SetLabels(labels)

			By("expecting denial of operation UPDATE of CRD")
			err := HubCluster.ImpersonateKubeClient.Update(ctx, &crd)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(crdStatusErrFormat, testUser, testGroups, crd.Name)))
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
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(crdStatusErrFormat, testUser, testGroups, crd.Name)))
		})

		It("should allow UPDATE operation on Fleet CRDs if user in system:masters group", func() {
			var crd v1.CustomResourceDefinition
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).Should(Succeed())

			By("update labels in CRD")
			labels := crd.GetLabels()
			labels[testKey] = testValue
			crd.SetLabels(labels)

			By("expecting denial of operation UPDATE of CRD")
			// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
			Expect(HubCluster.KubeClient.Update(ctx, &crd)).To(Succeed())

			By("remove new label added for test")
			labels = crd.GetLabels()
			delete(labels, testKey)
			crd.SetLabels(labels)
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
	BeforeEach(func() {
		By("create cluster role to modify fleet CRs")
		cr := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRole,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{fleetv1alpha1.GroupVersion.Group},
					Verbs:     []string{"*"},
					Resources: []string{"*"},
				},
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, &cr)).Should(Succeed())

		By("create cluster role binding for test-user to modify fleet CRs")
		crb := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRoleBinding,
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
				Kind:     "ClusterRole",
				Name:     testUserClusterRole,
			},
		}
		Expect(HubCluster.KubeClient.Create(ctx, &crb)).Should(Succeed())
	})

	AfterEach(func() {
		By("remove cluster role binding")
		crb := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRoleBinding,
			},
		}
		Expect(HubCluster.KubeClient.Delete(ctx, &crb)).Should(Succeed())

		By("remove cluster role")
		cr := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: testUserClusterRole,
			},
		}
		Expect(HubCluster.KubeClient.Delete(ctx, &cr)).Should(Succeed())
	})

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
						Namespace: "fleet-system",
					},
				},
			}

			By("expecting denial of operation CREATE of member cluster")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &mc)
			fmt.Println(err)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(mcStatusErrFormat, testUser, testGroups, mc.Name)))
		})

		It("should deny UPDATE operation on member cluster CR for user not in system:masters group", func() {
			var mc fleetv1alpha1.MemberCluster
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())

			By("update member cluster spec")
			mc.Spec.State = fleetv1alpha1.ClusterStateLeave

			By("expecting denial of operation UPDATE of member cluster")
			err := HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			var statusErr *k8sErrors.StatusError
			fmt.Println(err.Error())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(mcStatusErrFormat, testUser, testGroups, mc.Name)))
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
			Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf(mcStatusErrFormat, testUser, testGroups, mc.Name)))
		})

		It("should allow update operation on member cluster CR for user in system:masters group", func() {
			var mc fleetv1alpha1.MemberCluster
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: MemberCluster.ClusterName}, &mc)).Should(Succeed())

			By("update labels in CRD")
			labels := make(map[string]string)
			labels[testKey] = testValue
			mc.SetLabels(labels)

			By("expecting denial of operation UPDATE of CRD")
			// The user associated with KubeClient is kubernetes-admin in groups: [system:masters, system:authenticated]
			Expect(HubCluster.KubeClient.Update(ctx, &mc)).To(Succeed())

			By("remove new label added for test")
			labels = mc.GetLabels()
			delete(labels, testKey)
			mc.SetLabels(labels)
		})
	})
})
