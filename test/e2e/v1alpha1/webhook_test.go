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

package v1alpha1

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"
	testutils "go.goms.io/fleet/test/e2e/v1alpha1/utils"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
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
	testUser          = "test-user"
	randomUser        = "random-user"
	testKey           = "test-key"
	testValue         = "test-value"
	testWork          = "test-work"
	fleetSystemNS     = "fleet-system"
	kubeSystemNS      = "kube-system"
	testMemberCluster = "test-mc"
)

var (
	crdGVK       = metav1.GroupVersionKind{Group: apiextensionsv1.SchemeGroupVersion.Group, Version: apiextensionsv1.SchemeGroupVersion.Version, Kind: "CustomResourceDefinition"}
	mcGVK        = metav1.GroupVersionKind{Group: fleetv1alpha1.GroupVersion.Group, Version: fleetv1alpha1.GroupVersion.Version, Kind: "MemberCluster"}
	imcGVK       = metav1.GroupVersionKind{Group: fleetv1alpha1.GroupVersion.Group, Version: fleetv1alpha1.GroupVersion.Version, Kind: "InternalMemberCluster"}
	namespaceGVK = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Namespace"}
	workGVK      = metav1.GroupVersionKind{Group: workv1alpha1.GroupVersion.Group, Version: workv1alpha1.GroupVersion.Version, Kind: "Work"}
	iseGVK       = metav1.GroupVersionKind{Group: fleetnetworkingv1alpha1.GroupVersion.Group, Version: fleetnetworkingv1alpha1.GroupVersion.Version, Kind: "InternalServiceExport"}

	deployment = appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: utilrand.String(10),
					Kind:       utilrand.String(10),
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
			},
		},
	}
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))

			By("expecting denial of operation UPDATE with the invalid ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", invalidCRP.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]))))

			By("expecting denial of operation UPDATE of ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in resource selector %+v is invalid:", invalidCRP.Spec.ResourceSelectors[0]))))

			By("expecting denial of operation UPDATE with the invalid ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %s", invalidGVK))))

			By("expecting denial of operation UPDATE with the invalid ClusterResourcePlacement")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacementv1alpha1.validating" denied the request`))
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
						Replicas:        ptr.To(int32(1)),
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
					Replicas:        ptr.To(int32(1)),
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
			var crd apiextensionsv1.CustomResourceDefinition
			Expect(utils.GetObjectFromManifest("./config/crd/bases/fleet.azure.com_clusterresourceplacements.yaml", &crd)).Should(Succeed())

			By("expecting denial of operation CREATE of CRD")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &crd)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &crdGVK, "", types.NamespacedName{Name: crd.Name})))
		})

		It("should deny UPDATE operation on Fleet CRD for user not in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var crd apiextensionsv1.CustomResourceDefinition
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).Should(Succeed())
				By("update labels in CRD")
				labels := crd.GetLabels()
				if labels == nil {
					labels = make(map[string]string)
				}
				labels[testKey] = testValue
				crd.SetLabels(labels)
				By("expecting denial of operation UPDATE of CRD")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &crd)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &crdGVK, "", types.NamespacedName{Name: crd.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on Fleet CRD for user not in system:masters group", func() {
			crd := apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "works.multicluster.x-k8s.io",
				},
			}
			By("expecting denial of operation Delete of CRD")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &crd)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete CRD call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &crdGVK, "", types.NamespacedName{Name: crd.Name})))
		})

		It("should allow UPDATE operation on Fleet CRDs even if user in system:masters group", func() {
			var crd apiextensionsv1.CustomResourceDefinition
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "memberclusters.fleet.azure.com"}, &crd)).Should(Succeed())

				By("update labels in CRD")
				labels := crd.GetLabels()
				if labels == nil {
					labels = make(map[string]string)
				}
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
			var crd apiextensionsv1.CustomResourceDefinition
			Expect(utils.GetObjectFromManifest("./test/manifests/test_testresources_crd.yaml", &crd)).Should(Succeed())

			By("create test resource CRD")
			Expect(HubCluster.KubeClient.Create(ctx, &crd)).To(Succeed())

			By("delete test resource CRD")
			Expect(HubCluster.KubeClient.Delete(ctx, &crd)).To(Succeed())
		})
	})
})

var _ = Describe("Fleet's Custom Resource Handler webhook tests", func() {
	Context("fleet guard rail tests for MC", func() {
		var mcName, imcNamespace string
		BeforeEach(func() {
			// Creating this MC for IMC E2E, this MC will fail to join since it's name is not configured to be recognized by the member agent
			// which it uses to create the namespace to watch for IMC resource. But it serves its purpose for the tests.
			mcName = testMemberCluster + "-" + utils.RandStr()
			imcNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, randomUser)
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, imcNamespace)
		})
		AfterEach(func() {
			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should deny CREATE operation on member cluster CR for user not in system:masters group", func() {
			mc := &fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: testMemberCluster + utils.RandStr(),
				},
				Spec: fleetv1alpha1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "test-user",
						Kind:      "ServiceAccount",
						Namespace: utils.FleetSystemNamespace,
					},
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: 60,
				},
			}

			By("expecting denial of operation CREATE of member cluster")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, mc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
		})

		It("should deny UPDATE operation on member cluster CR for user not in MC identity", func() {
			Eventually(func(g Gomega) error {
				var mc fleetv1alpha1.MemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())

				By("update member cluster spec")
				mc.Spec.State = fleetv1alpha1.ClusterStateLeave

				By("expecting denial of operation UPDATE of member cluster")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on member cluster CR for user not in system:masters group", func() {
			mc := fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcName,
				},
			}

			By("expecting denial of operation DELETE of member cluster")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &mc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &mcGVK, "", types.NamespacedName{Name: mc.Name})))
		})

		It("should allow update operation on member cluster CR labels for any user", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update labels in member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
				labels := make(map[string]string)
				labels[testKey] = testValue
				mc.SetLabels(labels)
				return HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow update operation on member cluster CR annotations for any user", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update annotations in member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
				annotations := make(map[string]string)
				annotations[testKey] = testValue
				mc.SetLabels(annotations)
				return HubCluster.ImpersonateKubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow update operation on member cluster CR spec for user in system:masters group", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update spec of member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
				mc.Spec.HeartbeatPeriodSeconds = 31
				return HubCluster.KubeClient.Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow update operation on member cluster CR status for user in system:masters group", func() {
			var mc fleetv1alpha1.MemberCluster
			By("update status of member cluster, expecting successful UPDATE of member cluster")
			Eventually(func(g Gomega) error {
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
				g.Expect(mc.Status.Conditions).ToNot(BeEmpty())
				mc.Status.Conditions[0].Reason = "update"
				return HubCluster.KubeClient.Status().Update(ctx, &mc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny member cluster CR status update for user not in system:master group or a whitelisted user", func() {
			Eventually(func(g Gomega) error {
				var mc fleetv1alpha1.MemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
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
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &mcGVK, "status", types.NamespacedName{Name: mc.Name})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail tests for IMC, in fleet-member prefixed namespace with user not in MC identity", func() {
		var mcName, imcNamespace string
		BeforeEach(func() {
			// Creating this MC for IMC E2E, this MC will fail to join since it's name is not configured to be recognized by the member agent
			// which it uses to create the namespace to watch for IMC resource. But it serves its purpose for the tests.
			mcName = testMemberCluster + "-" + utils.RandStr()
			imcNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, randomUser)
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, imcNamespace)
		})
		AfterEach(func() {
			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should deny CREATE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
			imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			imc := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcName,
					Namespace: imcNamespace,
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
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
		})

		It("should deny UPDATE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
			Eventually(func(g Gomega) error {
				imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
				imc.Spec.HeartbeatPeriodSeconds = 25

				By("expecting denial of operation UPDATE of Internal Member Cluster")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &imc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE operation on internal member cluster CR for user not in MC identity in fleet member namespace", func() {
			var imc fleetv1alpha1.InternalMemberCluster
			imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())

			By("expecting denial of operation UPDATE of Internal Member Cluster")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &imc)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete internal member cluster call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &imcGVK, "", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
		})

		It("should deny UPDATE operation on internal member cluster CR status for user not in MC identity in fleet member namespace", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				imcNamespace := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
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
				err := HubCluster.ImpersonateKubeClient.Status().Update(ctx, &imc)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update internal member cluster status call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString(testGroups), admissionv1.Update, &imcGVK, "status", types.NamespacedName{Name: imc.Name, Namespace: imc.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("fleet guard rail tests for IMC, in fleet-member prefixed namespace with user in MC identity, system:master group users", func() {
		var mcName, imcNamespace string
		BeforeEach(func() {
			// Creating this MC for IMC E2E, this MC will fail to join since it's name is not configured to be recognized by the member agent
			// which it uses to create the namespace to watch for IMC resource. But it serves its purpose for the tests.
			mcName = testMemberCluster + "-" + utils.RandStr()
			imcNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, "test-user")
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, imcNamespace)
		})
		AfterEach(func() {
			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should allow UPDATE operation on internal member cluster CR status for user in MC identity", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
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

		It("should allow UPDATE operation on internal member cluster CR for user in MC identity", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
				imc.Labels = map[string]string{"test-key": "test-value"}
				By("expecting successful UPDATE of Internal Member Cluster Status")
				return HubCluster.ImpersonateKubeClient.Update(ctx, &imc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow UPDATE operation on internal member cluster CR spec for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var imc fleetv1alpha1.InternalMemberCluster
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: mcName, Namespace: imcNamespace}, &imc)).Should(Succeed())
				imc.Spec.HeartbeatPeriodSeconds = 25

				By("expecting successful UPDATE of Internal Member Cluster Spec")
				return HubCluster.KubeClient.Update(ctx, &imc)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})

var _ = Describe("Fleet's Work Resource Handler webhook tests", func() {
	Context("fleet guard rail for work resource in fleet prefixed namespace with user not in MC identity", func() {
		var mcName, workName, testMemberClusterNamespace string
		BeforeEach(func() {
			mcName = testMemberCluster + "-" + utils.RandStr()
			workName = testWork + "-" + utils.RandStr()
			testMemberClusterNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, randomUser)
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, testMemberClusterNamespace)
			deploymentBytes, err := json.Marshal(deployment)
			Expect(err).Should(Succeed())
			w := workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: testMemberClusterNamespace,
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{
									Raw: deploymentBytes,
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &w)).Should(Succeed())
		})

		AfterEach(func() {
			w := workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: testMemberClusterNamespace,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &w)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, &w))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())

			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should deny CREATE operation on work CR for user not in MC identity", func() {
			deploymentBytes, err := json.Marshal(deployment)
			Expect(err).Should(Succeed())
			w := workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testWork + "-" + utils.RandStr(),
					Namespace: testMemberClusterNamespace,
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{
									Raw: deploymentBytes,
								},
							},
						},
					},
				},
			}
			By("expecting denial of operation CREATE of work")
			err = HubCluster.ImpersonateKubeClient.Create(ctx, &w)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create work call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace})))
		})

		It("should deny UPDATE operation on work CR status for user not in MC identity", func() {
			Eventually(func(g Gomega) error {
				var w workv1alpha1.Work
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: testMemberClusterNamespace}, &w)).Should(Succeed())
				w.Status = workv1alpha1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Applied",
							Status:             metav1.ConditionTrue,
							Reason:             "appliedWorkComplete",
							Message:            "Apply work complete",
							LastTransitionTime: metav1.Now(),
						},
					},
				}
				By("expecting denial of operation UPDATE of work CR status")
				err := HubCluster.ImpersonateKubeClient.Status().Update(ctx, &w)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update work status call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, "test-user", utils.GenerateGroupString(testGroups), admissionv1.Update, &workGVK, "status", types.NamespacedName{Name: w.Name, Namespace: w.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should deny DELETE work operation for user not in MC identity", func() {
			var w workv1alpha1.Work
			Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: testMemberClusterNamespace}, &w)).Should(Succeed())
			By("expecting denial of operation DELETE of work")
			err := HubCluster.ImpersonateKubeClient.Delete(ctx, &w)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Delete work call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &workGVK, "", types.NamespacedName{Name: w.Name, Namespace: w.Namespace})))
		})
	})

	Context("fleet guard rail for work resource in fleet prefixed namespace with user in MC identity, system:masters group users", func() {
		var mcName, workName, testMemberClusterNamespace string
		BeforeEach(func() {
			mcName = testMemberCluster + "-" + utils.RandStr()
			workName = testWork + "-" + utils.RandStr()
			testMemberClusterNamespace = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, "test-user")
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, testMemberClusterNamespace)

			deploymentBytes, err := json.Marshal(deployment)
			Expect(err).Should(Succeed())
			w := workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: testMemberClusterNamespace,
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{
									Raw: deploymentBytes,
								},
							},
						},
					},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &w)).Should(Succeed())
		})

		AfterEach(func() {
			w := workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: testMemberClusterNamespace,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &w)).Should(Succeed())
			Eventually(func() bool {
				return k8sErrors.IsNotFound(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, &w))
			}, testutils.PollTimeout, testutils.PollInterval).Should(BeTrue())
			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should allow UPDATE operation on work CR for user in MC identity", func() {
			Eventually(func(g Gomega) error {
				var w workv1alpha1.Work
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: testMemberClusterNamespace}, &w)).Should(Succeed())
				w.SetLabels(map[string]string{"test-key": "test-value"})
				By("expecting successful UPDATE of work")
				return HubCluster.ImpersonateKubeClient.Update(ctx, &w)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow UPDATE operation on work CR status for user in MC identity", func() {
			Eventually(func(g Gomega) error {
				var w workv1alpha1.Work
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: testMemberClusterNamespace}, &w)).Should(Succeed())
				w.Status = workv1alpha1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Applied",
							Status:             metav1.ConditionTrue,
							Reason:             "appliedWorkComplete",
							Message:            "Apply work complete",
							LastTransitionTime: metav1.Now(),
						},
					},
				}
				By("expecting successful UPDATE of work Status")
				return HubCluster.ImpersonateKubeClient.Status().Update(ctx, &w)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		It("should allow UPDATE operation on work CR spec for user in system:masters group", func() {
			Eventually(func(g Gomega) error {
				var w workv1alpha1.Work
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: workName, Namespace: testMemberClusterNamespace}, &w)).Should(Succeed())
				w.Spec.Workload.Manifests = []workv1alpha1.Manifest{}

				By("expecting successful UPDATE of work Spec")
				return HubCluster.KubeClient.Update(ctx, &w)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})

var _ = Describe("Fleet's Reserved Namespace Handler webhook tests", func() {
	Context("deny requests to modify namespace with fleet/kube prefix", func() {
		It("should deny CREATE operation on namespace with fleet-member prefix for user not in system:masters group", func() {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fleet-member-test-mc",
				},
			}
			By("expecting denial of operation CREATE of namespace")
			err := HubCluster.ImpersonateKubeClient.Create(ctx, &ns)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create namespace call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
		})

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
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
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
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Create, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
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
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
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
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
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
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
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
			Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Delete, &namespaceGVK, "", types.NamespacedName{Name: ns.Name})))
		})

		It("should allow create/update/delete operation on namespace without fleet/kube prefix for user not in system:masters group", func() {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mynamespace",
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

var _ = Describe("Fleet's Reserved Namespace Handler fleet network tests", Ordered, func() {
	Context("deny request to modify network resources in fleet member namespaces, for user not in member cluster identity", Ordered, func() {
		var nsName, mcName string
		BeforeEach(func() {
			// Creating this MC for Internal Service Export E2E, this MC will fail to join since it's name is not configured to be recognized by the member agent
			// which it uses to create the namespace to watch for IMC resource. But it serves its purpose for the tests.
			mcName = testMemberCluster + "-" + utils.RandStr()
			nsName = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, randomUser)
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, nsName)

			ise := testutils.InternalServiceExport("test-internal-service-export", nsName)
			Eventually(func() error {
				return HubCluster.KubeClient.Create(ctx, &ise)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		AfterEach(func() {
			ise := fleetnetworkingv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-internal-service-export",
					Namespace: nsName,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &ise))
			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should deny update operation on Internal service export resource in fleet-member namespace for user not in member cluster identity", func() {
			Eventually(func(g Gomega) error {
				var ise fleetnetworkingv1alpha1.InternalServiceExport
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-internal-service-export", Namespace: nsName}, &ise)).Should(Succeed())
				ise.SetLabels(map[string]string{"test-key": "test-value"})
				By("expecting denial of operation UPDATE of Internal Service Export")
				err := HubCluster.ImpersonateKubeClient.Update(ctx, &ise)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update Internal Service Export call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf(validation.ResourceDeniedFormat, testUser, utils.GenerateGroupString(testGroups), admissionv1.Update, &iseGVK, "", types.NamespacedName{Name: ise.Name, Namespace: ise.Namespace})))
				return nil
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})

	Context("allow request to create network resources in fleet member namespaces, for user in member cluster identity", Ordered, func() {
		var ns corev1.Namespace
		BeforeEach(func() {
			ns = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "fleet-member-test-internal-service-export",
					Labels: map[string]string{fleetv1beta1.FleetResourceLabelKey: "true"},
				},
			}
			Expect(HubCluster.KubeClient.Create(ctx, &ns)).Should(Succeed())
			By(fmt.Sprintf("namespace `%s` is created", ns.Name))
		})

		It("should allow CREATE operation on Internal service export resource in fleet-member namespace for user in member cluster identity", func() {
			ise := testutils.InternalServiceExport("test-internal-service-export", ns.Name)
			By("expecting successful CREATE of Internal Service Export")
			Expect(HubCluster.ImpersonateKubeClient.Create(ctx, &ise)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(HubCluster.KubeClient.Delete(ctx, &ns)).Should(Succeed())
			By(fmt.Sprintf("namespace %s is deleted", ns.Name))
		})
	})

	Context("allow request to modify network resources in fleet member namespaces, for user in member cluster identity", Ordered, func() {
		var nsName, mcName string
		BeforeEach(func() {
			// Creating this MC for Internal Service Export E2E, this MC will fail to join since it's name is not configured to be recognized by the member agent
			// which it uses to create the namespace to watch for IMC resource. But it serves its purpose for the tests.
			mcName = testMemberCluster + "-" + utils.RandStr()
			nsName = fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			testutils.CreateMemberClusterResource(ctx, HubCluster, mcName, testUser)
			testutils.CheckInternalMemberClusterExists(ctx, HubCluster, mcName, nsName)

			ise := testutils.InternalServiceExport("test-internal-service-export", nsName)
			Eventually(func() error {
				return HubCluster.KubeClient.Create(ctx, &ise)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})

		AfterEach(func() {
			ise := fleetnetworkingv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-internal-service-export",
					Namespace: nsName,
				},
			}
			Expect(HubCluster.KubeClient.Delete(ctx, &ise))
			testutils.CleanupMemberClusterResources(ctx, HubCluster, mcName)
		})

		It("should allow update operation on Internal service export resource in fleet-member namespace for user in member cluster identity", func() {
			Eventually(func(g Gomega) error {
				var ise fleetnetworkingv1alpha1.InternalServiceExport
				g.Expect(HubCluster.KubeClient.Get(ctx, types.NamespacedName{Name: "test-internal-service-export", Namespace: nsName}, &ise)).Should(Succeed())
				ise.SetLabels(map[string]string{"test-key": "test-value"})
				By("expecting successful UPDATE of Internal Service Export")
				return HubCluster.ImpersonateKubeClient.Update(ctx, &ise)
			}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
		})
	})
})
