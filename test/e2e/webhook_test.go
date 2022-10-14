/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	kubeSystemNs             = "kube-system"
	fleetSystemNs            = "fleet-system"
	fleetWebhookCfgName      = "fleet-validating-webhook-configuration"
	podValidationWebhookName = "fleet.pod.validating"
	crpValidationWebhookName = "fleet.clusterresourceplacement.validating"
)

var (
	whitelistedNamespaces = []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: kubeSystemNs}},
		{ObjectMeta: metav1.ObjectMeta{Name: fleetSystemNs}},
	}
)

var _ = Describe("Fleet's Hub cluster webhook tests", func() {
	Context("Validation webhook configuration", func() {
		It("Should have desired settings & rules", func() {
			var cfg admv1.ValidatingWebhookConfiguration
			By("Verifying the fleet webhook configuration exists", func() {
				objKey := client.ObjectKey{Name: fleetWebhookCfgName}
				err := HubCluster.KubeClient.Get(ctx, objKey, &cfg)
				Expect(err).ToNot(HaveOccurred())
			})

			var podVW admv1.ValidatingWebhook
			By("Verifying the Pod validation webhook settings & rules", func() {
				expectedFailurePolicy := admv1.Fail
				expectedSideEffects := admv1.SideEffectClassNone
				expectedAdmissionReviewVersions := []string{"v1", "v1beta1"}
				for _, vW := range cfg.Webhooks {
					if vW.Name == podValidationWebhookName {
						podVW = vW
						break
					}
				}

				Expect(podVW).ShouldNot(BeNil())
				Expect(podVW.AdmissionReviewVersions).Should(Equal(expectedAdmissionReviewVersions))
				Expect(*podVW.FailurePolicy).Should(Equal(expectedFailurePolicy))
				Expect(*podVW.SideEffects).Should(Equal(expectedSideEffects))

				podRules := podVW.Rules
				Expect(len(podRules)).Should(Equal(1))

				expectedScope := admv1.NamespacedScope
				expectedRule := admv1.RuleWithOperations{
					Operations: []admv1.OperationType{admv1.OperationAll},
					Rule: admv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
						Scope:       &expectedScope,
					},
				}

				Expect(cmp.Diff(expectedRule, podRules[0])).Should(BeEmpty())
			})
			var crpVW admv1.ValidatingWebhook
			By("Verifying the ClusterResourcePlacement validation webhook settings & rules", func() {
				expectedFailurePolicy := admv1.Fail
				expectedSideEffects := admv1.SideEffectClassNone
				expectedAdmissionReviewVersions := []string{"v1", "v1beta1"}
				for _, vW := range cfg.Webhooks {
					if vW.Name == crpValidationWebhookName {
						crpVW = vW
						break
					}
				}

				Expect(crpVW).ShouldNot(BeNil())
				Expect(crpVW.AdmissionReviewVersions).Should(Equal(expectedAdmissionReviewVersions))
				Expect(*crpVW.FailurePolicy).Should(Equal(expectedFailurePolicy))
				Expect(*crpVW.SideEffects).Should(Equal(expectedSideEffects))

				crpRules := crpVW.Rules
				Expect(len(crpRules)).Should(Equal(1))

				expectedScope := admv1.ClusterScope
				expectedRule := admv1.RuleWithOperations{
					Operations: []admv1.OperationType{admv1.OperationAll},
					Rule: admv1.Rule{
						APIGroups:   []string{"fleet.azure.com"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{fleetv1alpha1.ClusterResourcePlacementResource},
						Scope:       &expectedScope,
					},
				}

				Expect(cmp.Diff(expectedRule, crpRules[0])).Should(BeEmpty())
			})

		})
	})
	Context("Pod validation webhook", func() {
		It("Admission operations on Pods within whitelisted namespaces should be admitted", func() {
			for _, ns := range whitelistedNamespaces {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: ns.ObjectMeta.Name}
				pod := generateGenericPod(objKey.Name, objKey.Namespace)

				var retrievedPod corev1.Pod
				opsToTest := buildOrderedWriteOperationSlice(admv1.OperationAll)
				for _, op := range opsToTest {
					By(fmt.Sprintf("expecting successful operation %s of Pod in whitelisted namespace %s", op, ns.ObjectMeta.Name), func() {
						switch op {
						case admv1.Create:
							err := executeKubeClientAdmissionOperation(op, pod)
							Expect(err).ShouldNot(HaveOccurred())
						case admv1.Update:
							var podV2 *corev1.Pod
							Eventually(func() error {
								err := HubCluster.KubeClient.Get(ctx, objKey, &retrievedPod)
								Expect(err).ShouldNot(HaveOccurred())
								podV2 = retrievedPod.DeepCopy()
								podV2.Labels = map[string]string{utils.RandStr(): utils.RandStr()}
								err = executeKubeClientAdmissionOperation(op, podV2)
								return err
							}, timeout, interval).ShouldNot(HaveOccurred())
						case admv1.Delete:
							err := executeKubeClientAdmissionOperation(op, pod)
							Expect(err).ShouldNot(HaveOccurred())
						}
					})
				}
			}
		})
		It("Operations on Pods within non-whitelisted namespaces should be rejected", func() {
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

			// Attempt operations of pod resource within all non-whitelisted namespaces.
			for _, ns := range nsList.Items {
				objKey := client.ObjectKey{Name: utils.RandStr(), Namespace: ns.ObjectMeta.Name}
				ctx = context.Background()
				pod := generateGenericPod(objKey.Name, objKey.Namespace)

				opsToTest := buildOrderedWriteOperationSlice(admv1.OperationAll)
				for _, op := range opsToTest {
					By(fmt.Sprintf("expecting unsuccessful operation %s of Pod in non-whitelisted namespace %s", op, ns.ObjectMeta.Name), func() {
						switch op {
						case admv1.Create:
							err := executeKubeClientAdmissionOperation(op, pod)
							Expect(err).Should(HaveOccurred())
						case admv1.Update:
							podV2 := pod.DeepCopy()
							podV2.Labels = map[string]string{utils.RandStr(): utils.RandStr()}
							err := executeKubeClientAdmissionOperation(op, podV2)
							Expect(err).Should(HaveOccurred())
						case admv1.Delete:
							err := executeKubeClientAdmissionOperation(op, pod)
							Expect(err).Should(HaveOccurred())
						}
					})
				}
			}
		})
	})
})

func generateGenericPod(name string, namespace string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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
}

func buildOrderedWriteOperationSlice(option admv1.OperationType) []admv1.OperationType {
	switch option {
	case admv1.OperationAll:
		return []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete}
	default:
		return []admv1.OperationType{option}
	}
}

func executeKubeClientAdmissionOperation(operation admv1.OperationType, obj client.Object) error {
	switch operation {
	case admv1.Create:
		return HubCluster.KubeClient.Create(ctx, obj)
	case admv1.Update:
		return HubCluster.KubeClient.Update(ctx, obj)
	case admv1.Delete:
		return HubCluster.KubeClient.Delete(ctx, obj)
	case admv1.Connect:
		return fmt.Errorf("operation %s is not supported", admv1.Connect)
	default:
		return fmt.Errorf("operation must be one of {%s, %s, %s}", admv1.Create, admv1.Update, admv1.Delete)
	}
}

func findIndexOfNamespace(nsName string, nsList corev1.NamespaceList) int {
	for i, ns := range nsList.Items {
		if ns.Name == nsName {
			return i
		}
	}
	return -1
}
