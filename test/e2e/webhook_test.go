/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	errors "errors"
	"fmt"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admv1 "k8s.io/api/admissionregistration/v1"
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
		It("should admit operations on Pods within whitelisted namespaces", func() {
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

				var err error

				By(fmt.Sprintf("expecting admission of operation CREATE of Pod in whitelisted namespace %s", ns.ObjectMeta.Name))
				err = HubCluster.KubeClient.Create(ctx, nginxPod)
				Expect(err).ShouldNot(HaveOccurred())

				By(fmt.Sprintf("expecting admission of operation UPDATE of Pod in whitelisted namespace %s", ns.ObjectMeta.Name))
				var podV2 *corev1.Pod
				Eventually(func() error {
					var currentPod corev1.Pod
					err = HubCluster.KubeClient.Get(ctx, objKey, &currentPod)
					Expect(err).ShouldNot(HaveOccurred())
					podV2 = currentPod.DeepCopy()
					podV2.Labels = map[string]string{utils.RandStr(): utils.RandStr()}
					err = HubCluster.KubeClient.Update(ctx, podV2)
					return err
				}, testUtils.PollTimeout, testUtils.PollInterval).ShouldNot(HaveOccurred())

				By(fmt.Sprintf("expecting admission of operation DELETE of Pod in whitelisted namespace %s", ns.ObjectMeta.Name))
				err = HubCluster.KubeClient.Delete(ctx, nginxPod)
				Expect(err).ShouldNot(HaveOccurred())
			}
		})
		It("should deny create operation on Pods within non-whitelisted namespaces", func() {
			ctx = context.Background()

			// Retrieve list of existing namespaces, remove whitelisted namespaces.
			var nsList corev1.NamespaceList
			err := HubCluster.KubeClient.List(ctx, &nsList)
			Expect(err).ToNot(HaveOccurred())
			for _, ns := range whitelistedNamespaces {
				i := findIndexOfNamespace(ns.Name, nsList)
				if i >= 0 {
					nsList.Items[i] = nsList.Items[len(nsList.Items)-1]
					nsList.Items = nsList.Items[:len(nsList.Items)-1]
				}
			}

			for _, ns := range nsList.Items {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: ns.ObjectMeta.Name}
				ctx = context.Background()
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

				By(fmt.Sprintf("expecting denial of operation %s of Pod in non-whitelisted namespace %s", admv1.Create, ns.ObjectMeta.Name))
				err := HubCluster.KubeClient.Create(ctx, nginxPod)
				Expect(err).Should(HaveOccurred())
				var statusErr *k8sErrors.StatusError
				ok := errors.As(err, &statusErr)
				Expect(ok).To(BeTrue())
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook.*denied the request.*`))
			}
		})
	})
	Context("ClusterResourcePlacement validation webhook", func() {
		It("should admit write operations for valid ClusterResourcePlacement resources", func() {
			var createdCRP fleetv1alpha1.ClusterResourcePlacement
			var validCRP fleetv1alpha1.ClusterResourcePlacement

			var err error

			By("attempting to create a valid CRP")
			validCRP = fleetv1alpha1.ClusterResourcePlacement{
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
			err = HubCluster.KubeClient.Create(ctx, &validCRP)
			Expect(err).ShouldNot(HaveOccurred())

			By("attempting to update a valid CRP")
			validCRP = fleetv1alpha1.ClusterResourcePlacement{
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
			err = HubCluster.KubeClient.Create(ctx, &validCRP)
			Expect(err).ShouldNot(HaveOccurred())

			Eventually(func() bool {
				err = HubCluster.KubeClient.Get(ctx, client.ObjectKey{Name: validCRP.Name}, &createdCRP)
				return len(createdCRP.Status.Conditions) > 0
			}, testUtils.PollTimeout, testUtils.PollInterval).Should(BeTrue())

			createdCRP.Spec.ResourceSelectors[0].Name = utils.RandStr()

			Eventually(func() error {
				return HubCluster.KubeClient.Update(ctx, &createdCRP)
			}, testUtils.PollTimeout, testUtils.PollInterval).ShouldNot(HaveOccurred())

			By("attempting to delete a CRP")
			err = HubCluster.KubeClient.Delete(ctx, &createdCRP)
			Expect(err).ShouldNot(HaveOccurred())

		})
		It("should deny write operations for invalid ClusterResourcePlacement resources", func() {
			var err error
			var ok bool
			var statusErr *k8sErrors.StatusError
			var createdCRP fleetv1alpha1.ClusterResourcePlacement
			var invalidCRP fleetv1alpha1.ClusterResourcePlacement
			var validCRP fleetv1alpha1.ClusterResourcePlacement

			By("attempting to create a CRP which specifies both a label & name within a resource selector")
			invalidCRP = fleetv1alpha1.ClusterResourcePlacement{
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

			err = HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			ok = errors.As(err, &statusErr)
			Expect(ok).To(BeTrue())
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))

			By("attempting to create a CRP which specifies an invalid cluster label selector")
			invalidCRP = fleetv1alpha1.ClusterResourcePlacement{
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
			err = HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			ok = errors.As(err, &statusErr)
			Expect(ok).To(BeTrue())
			Expect(statusErr.Status().Message)
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", invalidCRP.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]))))

			By("attempting to create a CRP which specifies an invalid resource label selector ")
			invalidCRP = fleetv1alpha1.ClusterResourcePlacement{
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
			err = HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			ok = errors.As(err, &statusErr)
			Expect(ok).To(BeTrue())
			Expect(statusErr.Status().Message)
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in resource selector %+v is invalid:", invalidCRP.Spec.ResourceSelectors[0]))))

			By("attempting to create a CRP which specifies an invalid cluster label selector")
			invalidCRP = fleetv1alpha1.ClusterResourcePlacement{
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
			err = HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			ok = errors.As(err, &statusErr)
			Expect(ok).To(BeTrue())
			Expect(statusErr.Status().Message)
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", invalidCRP.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]))))

			By("attempting to create a CRP which specifies contains an invalid GVK within the resource selector")
			invalidCRP = fleetv1alpha1.ClusterResourcePlacement{
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

			err = HubCluster.KubeClient.Create(ctx, &invalidCRP)
			Expect(err).Should(HaveOccurred())
			ok = errors.As(err, &statusErr)
			Expect(ok).To(BeTrue())
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.clusterresourceplacement.validating" denied the request`))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the resource is not found in schema (please retry) or it is not a cluster scoped resource: %s", invalidGVK))))

			By("attempting to update an existing CRP with an invalid spec")
			validCRP = fleetv1alpha1.ClusterResourcePlacement{
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
							Name:    "",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"key": "value"},
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      utils.RandStr(),
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{utils.RandStr()},
									},
								},
							},
						},
					},
				},
			}
			err = HubCluster.KubeClient.Create(ctx, &validCRP)
			Expect(err).ShouldNot(HaveOccurred())

			Eventually(func() bool {
				err = HubCluster.KubeClient.Get(ctx, client.ObjectKey{Name: validCRP.Name}, &createdCRP)
				return len(createdCRP.Status.Conditions) > 0
			}, testUtils.PollTimeout, testUtils.PollInterval).Should(BeTrue())

			createdCRP.Spec.ResourceSelectors[0].LabelSelector.MatchExpressions[0].Operator = "invalid-operator"
			Eventually(func() string {
				err = HubCluster.KubeClient.Update(ctx, &createdCRP)
				errors.As(err, &statusErr)
				return statusErr.ErrStatus.Message
			}, testUtils.PollTimeout, testUtils.PollInterval).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("admission webhook \"fleet.clusterresourceplacement.validating\" denied the request: the labelSelector in resource selector %+v is invalid:", createdCRP.Spec.ResourceSelectors[0]))))
		})
	})
})

func findIndexOfNamespace(nsName string, nsList corev1.NamespaceList) int {
	for i, ns := range nsList.Items {
		if ns.Name == nsName {
			return i
		}
	}
	return -1
}
