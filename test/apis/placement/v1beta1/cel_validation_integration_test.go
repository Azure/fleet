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
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var _ = Describe("Test ClusterResourcePlacement CEL validations", func() {
	Context("Name length validation", func() {
		It("should deny creating ClusterResourcePlacement with name longer than 63 characters", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			}
			
			By(fmt.Sprintf("expecting denial of CREATE ClusterResourcePlacement with name length %d > 63", len(name)))
			err := hubClient.Create(ctx, crp)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should allow creating ClusterResourcePlacement with name of 63 characters", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-12345"
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			}
			
			By(fmt.Sprintf("expecting success when creating ClusterResourcePlacement with name length = 63"))
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			Expect(hubClient.Delete(ctx, crp)).Should(Succeed())
		})
	})

	Context("Immutable placement type validation", func() {
		It("should deny updating ClusterResourcePlacement placement type", func() {
			crpName := fmt.Sprintf("crp-placement-type-%d", GinkgoParallelProcess())
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
				},
			}
			
			By("Creating initial ClusterResourcePlacement with PickAll placement type")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			
			By("Attempting to update placement type to PickN")
			updatedCRP := crp.DeepCopy()
			updatedCRP.Spec.Policy.PlacementType = placementv1beta1.PickNPlacementType
			updatedCRP.Spec.Policy.NumberOfClusters = new(int32)
			*updatedCRP.Spec.Policy.NumberOfClusters = 2
			
			err := hubClient.Update(ctx, updatedCRP)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Update ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("placement type is immutable"))
			
			Expect(hubClient.Delete(ctx, crp)).Should(Succeed())
		})

		It("should allow updating ClusterResourcePlacement without changing placement type", func() {
			crpName := fmt.Sprintf("crp-valid-update-%d", GinkgoParallelProcess())
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
					},
				},
			}
			
			By("Creating initial ClusterResourcePlacement with PickAll placement type")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			
			By("Updating ClusterResourcePlacement without changing placement type")
			updatedCRP := crp.DeepCopy()
			updatedCRP.Spec.ResourceSelectors[0].Name = "another-test-namespace"
			
			Expect(hubClient.Update(ctx, updatedCRP)).Should(Succeed())
			Expect(hubClient.Delete(ctx, updatedCRP)).Should(Succeed())
		})
	})

	Context("Resource selector mutual exclusivity validation", func() {
		It("should deny creating ClusterResourcePlacement with both name and labelSelector in resource selector", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-invalid-selector-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
					},
				},
			}
			
			By("Expecting denial of CREATE ClusterResourcePlacement with both name and labelSelector")
			err := hubClient.Create(ctx, crp)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("the labelSelector and name fields are mutually exclusive in resource selectors"))
		})

		It("should allow creating ClusterResourcePlacement with only name in resource selector", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-valid-name-selector-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
				},
			}
			
			By("Expecting success when creating ClusterResourcePlacement with only name")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			Expect(hubClient.Delete(ctx, crp)).Should(Succeed())
		})

		It("should allow creating ClusterResourcePlacement with only labelSelector in resource selector", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-valid-labelsel-selector-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
					},
				},
			}
			
			By("Expecting success when creating ClusterResourcePlacement with only labelSelector")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			Expect(hubClient.Delete(ctx, crp)).Should(Succeed())
		})
	})

	Context("Tolerations update validation", func() {
		It("should deny removing tolerations from ClusterResourcePlacement", func() {
			crpName := fmt.Sprintf("crp-tolerations-removal-%d", GinkgoParallelProcess())
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Creating initial ClusterResourcePlacement with tolerations")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			
			By("Attempting to remove tolerations")
			updatedCRP := crp.DeepCopy()
			updatedCRP.Spec.Policy.Tolerations = []placementv1beta1.Toleration{}
			
			err := hubClient.Update(ctx, updatedCRP)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Update ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("tolerations cannot be updated or deleted"))
			
			Expect(hubClient.Delete(ctx, crp)).Should(Succeed())
		})

		It("should allow adding tolerations to ClusterResourcePlacement", func() {
			crpName := fmt.Sprintf("crp-tolerations-add-%d", GinkgoParallelProcess())
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "key1",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Creating initial ClusterResourcePlacement with tolerations")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			
			By("Adding tolerations")
			updatedCRP := crp.DeepCopy()
			updatedCRP.Spec.Policy.Tolerations = append(updatedCRP.Spec.Policy.Tolerations, placementv1beta1.Toleration{
				Key:      "key2",
				Operator: corev1.TolerationOpEqual,
				Value:    "value2",
				Effect:   corev1.TaintEffectNoSchedule,
			})
			
			Expect(hubClient.Update(ctx, updatedCRP)).Should(Succeed())
			Expect(hubClient.Delete(ctx, updatedCRP)).Should(Succeed())
		})
	})
	
	Context("Toleration format validation", func() {
		It("should deny creating ClusterResourcePlacement with invalid toleration key format", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-invalid-tol-key-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "invalid_key!",
								Operator: corev1.TolerationOpEqual,
								Value:    "value1",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Expecting denial of CREATE ClusterResourcePlacement with invalid toleration key format")
			err := hubClient.Create(ctx, crp)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("toleration key must be a valid label name"))
		})

		It("should deny creating ClusterResourcePlacement with invalid toleration value format", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-invalid-tol-value-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "valid-key",
								Operator: corev1.TolerationOpEqual,
								Value:    "invalid value!",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Expecting denial of CREATE ClusterResourcePlacement with invalid toleration value format")
			err := hubClient.Create(ctx, crp)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("toleration value must be a valid label value"))
		})

		It("should deny creating ClusterResourcePlacement with non-empty toleration value when operator is Exists", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-invalid-tol-exists-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "valid-key",
								Operator: corev1.TolerationOpExists,
								Value:    "should-be-empty",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Expecting denial of CREATE ClusterResourcePlacement with non-empty value for Exists operator")
			err := hubClient.Create(ctx, crp)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("toleration value needs to be empty when operator is Exists"))
		})

		It("should deny creating ClusterResourcePlacement with empty toleration key when operator is Equal", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-invalid-tol-equal-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "",
								Operator: corev1.TolerationOpEqual,
								Value:    "valid-value",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Expecting denial of CREATE ClusterResourcePlacement with empty key for Equal operator")
			err := hubClient.Create(ctx, crp)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), 
				fmt.Sprintf("Create ClusterResourcePlacement call produced error %s. Error type wanted is %s.", 
				reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("toleration key cannot be empty when operator is Equal"))
		})

		It("should allow creating ClusterResourcePlacement with valid toleration formats", func() {
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crp-valid-tolerations-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-namespace",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickAllPlacementType,
						Tolerations: []placementv1beta1.Toleration{
							{
								Key:      "valid.key",
								Operator: corev1.TolerationOpEqual,
								Value:    "valid-value",
								Effect:   corev1.TaintEffectNoSchedule,
							},
							{
								Key:      "another-key",
								Operator: corev1.TolerationOpExists,
								Value:    "",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			}
			
			By("Expecting success when creating ClusterResourcePlacement with valid toleration formats")
			Expect(hubClient.Create(ctx, crp)).Should(Succeed())
			Expect(hubClient.Delete(ctx, crp)).Should(Succeed())
		})
	})
})