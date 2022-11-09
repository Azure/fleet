/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	errors "errors"
	"fmt"
	"reflect"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	testUtils "go.goms.io/fleet/test/e2e/utils"
)

const (
	kubeSystemNs  = "kube-system"
	fleetSystemNs = "fleet-system"
)

var (
	whitelistedNamespaces = []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: kubeSystemNs}},
		{ObjectMeta: metav1.ObjectMeta{Name: fleetSystemNs}},
	}
)

var _ = Describe("Fleet's Hub cluster webhook tests", func() {
	Context("Pod validation webhook", func() {
		It("should admit operations on Pods within system reserved namespaces", func() {
			for _, ns := range whitelistedNamespaces {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: ns.ObjectMeta.Name}
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

				By(fmt.Sprintf("expecting admission of operation CREATE in system reserved namespace %s", ns.ObjectMeta.Name))
				Expect(HubCluster.KubeClient.Create(ctx, nginxPod)).Should(Succeed())

				By(fmt.Sprintf("expecting admission of operation UPDATE in system reserved namespace %s", ns.ObjectMeta.Name))
				var podV2 *corev1.Pod
				Eventually(func() error {
					var currentPod corev1.Pod
					Expect(HubCluster.KubeClient.Get(ctx, objKey, &currentPod)).Should(Succeed())
					podV2 = currentPod.DeepCopy()
					podV2.Labels = map[string]string{utils.RandStr(): utils.RandStr()}
					return HubCluster.KubeClient.Update(ctx, podV2)
				}, testUtils.PollTimeout, testUtils.PollInterval).Should(Succeed())

				By(fmt.Sprintf("expecting admission of operation DELETE in system reserved namespace %s", ns.ObjectMeta.Name))
				Expect(HubCluster.KubeClient.Delete(ctx, nginxPod)).Should(Succeed())
			}
		})
		It("should deny create operation on Pods within any non-system reserved namespace", func() {
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

			By(fmt.Sprintf("expecting denial of operation CREATE in non-system reserved namespace %s", rndNs.Name))
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
			By("expecting admission of operation CREATE of a valid CRP")
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
			By("expecting admission of operation DELETE")
			Expect(HubCluster.KubeClient.Delete(ctx, &createdCRP)).Should(Succeed())
		})
		It("should admit write operations for valid ClusterResourcePlacement resources", func() {
			// create & delete write operations are handled within the BeforeEach & AfterEach functions.
			By("expecting admission of operation UPDATE with a valid CRP")
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

			By("expecting denial of operation CREATE with the invalid CRP")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))

			By("expecting denial of operation UPDATE with the invalid CRP")
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

			By("expecting denial of operation CREATE")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", invalidCRP.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]))))

			By("expecting denial of operation UPDATE")
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

			By("expecting denial of operation CREATE with the invalid CRP")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in resource selector %+v is invalid:", invalidCRP.Spec.ResourceSelectors[0]))))

			By("expecting denial of operation UPDATE with the invalid CRP")
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

			By("expecting denial of operation CREATE with the invalid CRP")
			err := HubCluster.KubeClient.Create(ctx, &invalidCRP)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %s", invalidGVK))))

			By("expecting denial of operation UPDATE with the invalid CRP")
			createdCRP.Spec = invalidCRP.Spec
			err = HubCluster.KubeClient.Update(ctx, &createdCRP)
			Expect(err).Should(HaveOccurred())
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %s", invalidGVK))))
		})
	})
})
