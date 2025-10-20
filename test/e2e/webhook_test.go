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
package e2e

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
)

var _ = Describe("webhook tests for CRP CREATE operations", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	Context("validation webhooks tests", func() {

		It("should deny create on CRP with invalid label selector", func() {
			selector := invalidWorkResourceSelector()
			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: selector,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE placement %s", crpName))
			err := hubClient.Create(ctx, crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("the labelSelector and name fields are mutually exclusive in selector"))
		})

		It("should deny create on CRP with invalid placement policy for PickFixed", func() {
			Eventually(func(g Gomega) error {
				var numOfClusters int32 = 1
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickFixedPlacementType,
							NumberOfClusters: &numOfClusters,
						},
					},
				}
				err := hubClient.Create(ctx, &crp)
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("cluster names cannot be empty for policy type"))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("number of clusters must be nil for policy type PickFixed"))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should deny create on CRP with invalid placement policy for PickN", func() {
			Eventually(func(g Gomega) error {
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickNPlacementType,
							Affinity: &placementv1beta1.Affinity{
								ClusterAffinity: &placementv1beta1.ClusterAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []placementv1beta1.PreferredClusterSelector{
										{
											Preference: placementv1beta1.ClusterSelectorTerm{
												LabelSelector: &metav1.LabelSelector{
													MatchExpressions: []metav1.LabelSelectorRequirement{
														{
															Key:      "test-key",
															Operator: metav1.LabelSelectorOpIn,
														},
													},
												},
											},
										},
									},
								},
							},
							TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
								{
									TopologyKey:       "test-key",
									WhenUnsatisfiable: "random-type",
								},
							},
						},
					},
				}
				err := hubClient.Create(ctx, &crp)
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in preferred cluster selector %+v is invalid:", crp.Spec.Policy.Affinity.ClusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Preference.LabelSelector))))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("unknown unsatisfiable type random-type"))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("number of cluster cannot be nil for policy type PickN"))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should deny create CRP with invalid GVK", func() {
			Eventually(func(g Gomega) error {
				// Create the CRP.
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "",
								Kind:    "InvalidNamespace",
								Version: "v1",
								Name:    "invalid",
							},
						},
					},
				}
				By(fmt.Sprintf("creating placement %s", crpName))
				err := hubClient.Create(ctx, &crp)
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta("failed to get GVR of the selector")))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should deny create CRP with namespaced resource selected", func() {
			Eventually(func(g Gomega) error {
				// Create the CRP.
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
						// Add a custom finalizer; this would allow us to better observe
						// the behavior of the controllers.
						Finalizers: []string{customDeletionBlockerFinalizer},
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Kind:    "Deployment",
								Version: "v1",
								Name:    "test-deployment",
							},
						},
					},
				}
				By(fmt.Sprintf("creating placement %s", crpName))
				err := hubClient.Create(ctx, &crp)
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta("the resource is not found in schema (please retry) or it is not a cluster scoped resource")))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("mutation webhooks tests", func() {
		AfterEach(func() {
			// Ensure that the CRP and related resources are deleted after the tests.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should allow create CRP with nil policy and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromString("30%")),
								MaxSurge:                 ptr.To(intstr.FromString("10%")),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
								ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
								WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
								WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
							},
						},
						RevisionHistoryLimit: ptr.To(int32(15)),
					},
				}
				g.Expect(hubClient.Create(ctx, &crp)).To(BeNil(), "Create CRP call should not produce error")
				// Verify that the CRP is created with default values.
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(createdCRP.Spec.Policy).To(Equal(&placementv1beta1.PlacementPolicy{PlacementType: placementv1beta1.PickAllPlacementType}), "CRP should have default policy type PickAll")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow create CRP with TopologySpreadConstraints & Tolerations fields and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType:    placementv1beta1.PickNPlacementType,
							NumberOfClusters: ptr.To(int32(2)),
							TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
								{
									TopologyKey: "kubernetes.io/hostname",
								},
							},
							Tolerations: []placementv1beta1.Toleration{
								{
									Key:   "key",
									Value: "value",
								},
							},
						},
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromString("30%")),
								MaxSurge:                 ptr.To(intstr.FromString("10%")),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
								ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
								WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
								WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
							},
						},
					},
				}
				g.Expect(hubClient.Create(ctx, &crp)).To(BeNil(), "Create CRP call should not produce error")
				// Verify that the CRP is created with default values.
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(createdCRP.Spec.Policy.TopologySpreadConstraints).To(Equal([]placementv1beta1.TopologySpreadConstraint{
					{
						TopologyKey:       "kubernetes.io/hostname",
						MaxSkew:           ptr.To(int32(defaulter.DefaultMaxSkewValue)),
						WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
					},
				}), "CRP should have default topology spread constraint fields")
				g.Expect(createdCRP.Spec.Policy.Tolerations).To(Equal([]placementv1beta1.Toleration{
					{
						Key:      "key",
						Value:    "value",
						Operator: corev1.TolerationOpEqual,
					},
				}), "CRP should have default tolerations fields")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow create CRP with nil strategy and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Policy: &placementv1beta1.PlacementPolicy{
							PlacementType: placementv1beta1.PickAllPlacementType,
						},
						RevisionHistoryLimit: ptr.To(int32(15)),
					},
				}
				g.Expect(hubClient.Create(ctx, &crp)).To(BeNil(), "Create CRP call should not produce error")
				// Verify that the CRP is created with default values.
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(createdCRP.Spec.Strategy).To(Equal(placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						MaxUnavailable:           ptr.To(intstr.FromString(defaulter.DefaultMaxUnavailableValue)),
						MaxSurge:                 ptr.To(intstr.FromString(defaulter.DefaultMaxSurgeValue)),
						UnavailablePeriodSeconds: ptr.To(defaulter.DefaultUnavailablePeriodSeconds),
					},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					},
				}), "CRP should have default strategy type RollingUpdate with default values")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow create CRP with nil revision history limit and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromString("30%")),
								MaxSurge:                 ptr.To(intstr.FromString("10%")),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
								ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
								WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
								WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
							},
						},
					},
				}
				g.Expect(hubClient.Create(ctx, &crp)).To(BeNil(), "Create CRP call should not produce error")
				// Verify that the CRP is created with default values.
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(*createdCRP.Spec.RevisionHistoryLimit).To(Equal(int32(defaulter.DefaultRevisionHistoryLimitValue)), "CRP should have default revision history limit value")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow create CRP with nil serverside apply config and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: crpName,
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: workResourceSelector(),
						Strategy: placementv1beta1.RolloutStrategy{
							Type: placementv1beta1.RollingUpdateRolloutStrategyType,
							RollingUpdate: &placementv1beta1.RollingUpdateConfig{
								MaxUnavailable:           ptr.To(intstr.FromString("30%")),
								MaxSurge:                 ptr.To(intstr.FromString("10%")),
								UnavailablePeriodSeconds: ptr.To(2),
							},
							ApplyStrategy: &placementv1beta1.ApplyStrategy{
								Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
								ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
								WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
								WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
							},
						},
						RevisionHistoryLimit: ptr.To(int32(15)),
					},
				}
				g.Expect(hubClient.Create(ctx, &crp)).To(BeNil(), "Create CRP call should not produce error")
				// Verify that the CRP is created with default values.
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(createdCRP.Spec.Strategy.ApplyStrategy.Type).To(Equal(placementv1beta1.ApplyStrategyTypeServerSideApply), "CRP should have serverside apply strategy type")
				g.Expect(createdCRP.Spec.Strategy.ApplyStrategy.ServerSideApplyConfig).ToNot(BeNil(), "CRP should have serverside apply config")
				g.Expect(createdCRP.Spec.Strategy.ApplyStrategy.ServerSideApplyConfig).To(Equal(&placementv1beta1.ServerSideApplyConfig{
					ForceConflicts: false,
				}), "CRP should have default serverside apply config")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})
})

var _ = Describe("webhook tests for CRP UPDATE operations", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	Context("validation webhooks tests", func() {
		BeforeAll(func() {
			By("creating work resources")
			createWorkResources()

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
				},
			}
			By(fmt.Sprintf("creating placement %s", crpName))
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
		})

		AfterAll(func() {
			By(fmt.Sprintf("deleting placement %s and related resources", crpName))
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should deny update on CRP with invalid label selector", func() {
			Eventually(func(g Gomega) error {
				selector := invalidWorkResourceSelector()
				var crp placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed())
				crp.Spec.ResourceSelectors = selector
				err := hubClient.Update(ctx, &crp)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should deny update on CRP with invalid placement policy for PickAll", func() {
			Eventually(func(g Gomega) error {
				var crp placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed())
				crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
					Affinity: &placementv1beta1.Affinity{
						ClusterAffinity: &placementv1beta1.ClusterAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchExpressions: []metav1.LabelSelectorRequirement{
												{
													Key:      "test-key",
													Operator: metav1.LabelSelectorOpIn,
												},
											},
										},
									},
								},
							},
						},
					},
					TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
						{
							TopologyKey: "test-key",
						},
					},
				}
				err := hubClient.Update(ctx, &crp)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", crp.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms[0].LabelSelector))))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("topology spread constraints needs to be empty for policy type PickAll"))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should deny update on CRP with placement policy type update", func() {
			Eventually(func(g Gomega) error {
				var numOfClusters int32 = 1
				var crp placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed())
				crp.Spec.Policy = &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: &numOfClusters,
				}
				err := hubClient.Update(ctx, &crp)
				if k8sErrors.IsConflict(err) {
					return err
				}
				var statusErr *k8sErrors.StatusError
				g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("placement type is immutable"))
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("mutation webhooks tests", func() {
		BeforeAll(func() {
			By("creating work resources")
			createWorkResources()

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
				},
			}
			By(fmt.Sprintf("creating placement %s", crpName))
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
		})

		AfterAll(func() {
			// Ensure that the CRP and related resources are deleted after the tests.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should deny update of ClusterResourcePlacement with nil policy", func() {
			Eventually(func(g Gomega) error {
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				// Update the CRP with nil policy.
				g.Expect(createdCRP.Spec.Policy).ToNot(BeNil(), "CRP should have a policy")
				createdCRP.Spec.Policy = nil
				var updatedCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &updatedCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(updatedCRP.Spec.Policy).To(Equal(&placementv1beta1.PlacementPolicy{PlacementType: placementv1beta1.PickAllPlacementType}), "CRP should have default policy type PickAll")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow update CRP with empty strategy and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				createdCRP.Spec.Strategy = placementv1beta1.RolloutStrategy{}
				g.Expect(hubClient.Update(ctx, &createdCRP)).Should(Succeed(), "Failed to update CRP %s", crpName)
				// Verify that the CRP is updated with default values.
				var updatedCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &updatedCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(updatedCRP.Spec.Strategy).To(Equal(placementv1beta1.RolloutStrategy{
					Type: placementv1beta1.RollingUpdateRolloutStrategyType,
					RollingUpdate: &placementv1beta1.RollingUpdateConfig{
						MaxUnavailable:           ptr.To(intstr.FromString(defaulter.DefaultMaxUnavailableValue)),
						MaxSurge:                 ptr.To(intstr.FromString(defaulter.DefaultMaxSurgeValue)),
						UnavailablePeriodSeconds: ptr.To(defaulter.DefaultUnavailablePeriodSeconds),
					},
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					},
				}), "CRP should have default strategy type RollingUpdate with default values")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow update CRP with nil revision history limit and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				createdCRP.Spec.RevisionHistoryLimit = nil
				g.Expect(hubClient.Update(ctx, &createdCRP)).Should(Succeed(), "Failed to update CRP %s", crpName)
				// Verify that the CRP is updated with default values.
				var updatedCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &updatedCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(*createdCRP.Spec.RevisionHistoryLimit).To(Equal(int32(defaulter.DefaultRevisionHistoryLimitValue)), "CRP should have default revision history limit value")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("should allow update CRP with nil serverside apply config and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				// Update the CRP with nil serverside apply config and serverside apply strategy type.
				createdCRP.Spec.Strategy.ApplyStrategy.Type = placementv1beta1.ApplyStrategyTypeServerSideApply
				createdCRP.Spec.Strategy.ApplyStrategy.ServerSideApplyConfig = nil
				g.Expect(hubClient.Update(ctx, &createdCRP)).Should(Succeed(), "Failed to update CRP %s", crpName)
				// Verify that the CRP is updated with default values.
				var updatedCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &updatedCRP)).Should(Succeed())
				g.Expect(updatedCRP.Spec.Strategy.ApplyStrategy.Type).To(Equal(placementv1beta1.ApplyStrategyTypeServerSideApply), "CRP should have serverside apply strategy type")
				g.Expect(updatedCRP.Spec.Strategy.ApplyStrategy.ServerSideApplyConfig).ToNot(BeNil(), "CRP should have serverside apply config")
				g.Expect(updatedCRP.Spec.Strategy.ApplyStrategy.ServerSideApplyConfig).To(Equal(&placementv1beta1.ServerSideApplyConfig{
					ForceConflicts: false,
				}), "CRP should have default serverside apply config")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	Context("mutation webhooks tests for CRP with Tolerations & Topology Constraints", func() {
		BeforeAll(func() {
			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: workResourceSelector(),
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType:    placementv1beta1.PickNPlacementType,
						NumberOfClusters: ptr.To(int32(2)),
					},
				},
			}
			By(fmt.Sprintf("creating placement %s", crpName))
			Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
		})

		AfterAll(func() {
			// Ensure that the CRP and related resources are deleted after the tests.
			ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		})

		It("should allow CRP update with TopologySpreadConstraints & Tolerations fields and update fields with default values", func() {
			Eventually(func(g Gomega) error {
				var createdCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &createdCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				createdCRP.Spec.Policy = &placementv1beta1.PlacementPolicy{
					PlacementType:    placementv1beta1.PickNPlacementType,
					NumberOfClusters: ptr.To(int32(2)),
					TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
						{
							TopologyKey: "kubernetes.io/hostname",
						},
					},
					Tolerations: []placementv1beta1.Toleration{
						{
							Key:   "key",
							Value: "value",
						},
					},
				}
				g.Expect(hubClient.Update(ctx, &createdCRP)).Should(Succeed(), "Failed to update CRP %s", crpName)
				// Verify that the CRP is updated with default values.
				var updatedCRP placementv1beta1.ClusterResourcePlacement
				g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &updatedCRP)).Should(Succeed(), "Failed to get CRP %s", crpName)
				g.Expect(updatedCRP.Spec.Policy.TopologySpreadConstraints).To(Equal([]placementv1beta1.TopologySpreadConstraint{
					{
						TopologyKey:       "kubernetes.io/hostname",
						MaxSkew:           ptr.To(int32(defaulter.DefaultMaxSkewValue)),
						WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
					},
				}), "CRP should have default topology spread constraint fields")
				g.Expect(updatedCRP.Spec.Policy.Tolerations).To(Equal([]placementv1beta1.Toleration{
					{
						Key:      "key",
						Value:    "value",
						Operator: corev1.TolerationOpEqual,
					},
				}), "CRP should have default tolerations fields")
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})
})

var _ = Describe("webhook tests for CRP tolerations", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP with tolerations.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.PlacementSpec{
				ResourceSelectors: workResourceSelector(),
				Policy: &placementv1beta1.PlacementPolicy{
					Tolerations: []placementv1beta1.Toleration{
						{
							Key:      "key1",
							Operator: corev1.TolerationOpEqual,
							Value:    "value1",
							Effect:   corev1.TaintEffectNoSchedule,
						},
						{
							Key:      "key2",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s and related resources", crpName))
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
	})

	It("should deny update on CRP with invalid toleration", func() {
		Eventually(func(g Gomega) error {
			var crp placementv1beta1.ClusterResourcePlacement
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed())
			invalidToleration := placementv1beta1.Toleration{
				Operator: corev1.TolerationOpEqual,
				Value:    "test-value",
				Effect:   corev1.TaintEffectNoSchedule,
			}
			crp.Spec.Policy.Tolerations = append(crp.Spec.Policy.Tolerations, invalidToleration)
			err := hubClient.Update(ctx, &crp)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(fmt.Sprintf("invalid toleration %+v: %s", invalidToleration, "toleration key cannot be empty, when operator is Equal")))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update on CRP with update to existing toleration", func() {
		Eventually(func(g Gomega) error {
			var crp placementv1beta1.ClusterResourcePlacement
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed())
			newTolerations := []placementv1beta1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key3",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}
			crp.Spec.Policy.Tolerations = newTolerations
			err := hubClient.Update(ctx, &crp)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("tolerations have been updated/deleted, only additions to tolerations are allowed"))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should allow update on CRP with adding a new toleration", func() {
		Eventually(func(g Gomega) error {
			var crp placementv1beta1.ClusterResourcePlacement
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed())
			newToleration := placementv1beta1.Toleration{
				Key:      "key3",
				Operator: corev1.TolerationOpEqual,
				Value:    "value3",
				Effect:   corev1.TaintEffectNoSchedule,
			}
			crp.Spec.Policy.Tolerations = append(crp.Spec.Policy.Tolerations, newToleration)
			return hubClient.Update(ctx, &crp)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for MC taints", Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil, nil)
	})

	AfterAll(func() {
		ensureMemberClusterAndRelatedResourcesDeletion(mcName)
	})

	It("should deny update on MC with invalid taint", func() {
		Eventually(func(g Gomega) error {
			var mc clusterv1beta1.MemberCluster
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: mcName}, &mc)).Should(Succeed())
			invalidTaint := clusterv1beta1.Taint{
				Key:    "key@1234:",
				Value:  "value1",
				Effect: "NoSchedule",
			}
			mc.Spec.Taints = append(mc.Spec.Taints, invalidTaint)
			err := hubClient.Update(ctx, &mc)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			g.Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update MC call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			g.Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character"))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ClusterResourceOverride CREATE operations", func() {
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	selector := placementv1beta1.ResourceSelectorTerm{
		Group:          "rbac.authorization.k8s.io/v1",
		Kind:           "ClusterRole",
		Version:        "v1",
		Name:           fmt.Sprintf("test-clusterrole-%d", GinkgoParallelProcess()),
		SelectionScope: placementv1beta1.NamespaceWithResources,
	}
	policy := &placementv1beta1.OverridePolicy{
		OverrideRules: []placementv1beta1.OverrideRule{
			{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key": "value",
								},
							},
						},
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
				JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
					{
						Operator: placementv1beta1.JSONPatchOverrideOpRemove,
						Path:     "/meta/labels/test-key",
					},
				},
			},
		},
	}

	It("should deny create CRO with invalid resource selection ", func() {
		Consistently(func(g Gomega) error {
			invalidSelector := placementv1beta1.ResourceSelectorTerm{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test-key": "test-value"},
				},
				SelectionScope: placementv1beta1.NamespaceWithResources,
			}
			invalidSelector1 := placementv1beta1.ResourceSelectorTerm{
				Group:          "rbac.authorization.k8s.io/v1",
				Kind:           "ClusterRole",
				Version:        "v1",
				SelectionScope: placementv1beta1.NamespaceWithResources,
			}
			// Create the CRO.
			cro := &placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						invalidSelector, selector, selector, invalidSelector1,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", croName))
			err := hubClient.Create(ctx, cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("label selector is not supported for resource selection %+v", invalidSelector))))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource selector %+v already exists, and must be unique", selector)))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource name is required for resource selection %+v", invalidSelector1)))
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ClusterResourceOverride CREATE operation limitations", Ordered, Serial, func() {
	BeforeAll(func() {
		By("Create 100 ClusterResourceOverrides")
		createClusterResourceOverrides(100)
	})

	AfterAll(func() {
		By("deleting ClusterResourceOverrides")
		for i := 0; i < 100; i++ {
			cleanupClusterResourceOverride(fmt.Sprintf(croNameTemplate, i))
		}
	})

	It("should deny create CRO with 100 existing CROs", func() {
		Consistently(func(g Gomega) error {
			cro101 := &placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cro-101",
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io/v1",
							Kind:    "ClusterRole",
							Version: "v1",
							Name:    "test-cluster-role-101",
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
													"key": "value",
												},
											},
										},
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key1": "value1",
												},
											},
										},
									},
								},
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE 101st override %s", cro101.Name))
			err := hubClient.Create(ctx, cro101)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("clusterResourceOverride limit has been reached: at most 100 cluster resources can be created"))
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ClusterResourceOverride CREATE operations resource selection limitations", Ordered, Serial, func() {
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	selector := placementv1beta1.ResourceSelectorTerm{
		Group:          "rbac.authorization.k8s.io/v1",
		Kind:           "ClusterRole",
		Version:        "v1",
		Name:           fmt.Sprintf("test-clusterrole-%d", GinkgoParallelProcess()),
		SelectionScope: placementv1beta1.NamespaceWithResources,
	}
	BeforeAll(func() {
		By("create clusterResourceOverride")
		cro := &placementv1beta1.ClusterResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name: croName,
			},
			Spec: placementv1beta1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
					selector,
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key": "value",
											},
										},
									},
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"key1": "value1",
											},
										},
									},
								},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpRemove,
									Path:     "/meta/labels/test-key",
								},
							},
						},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create CRO %s", croName)
	})

	AfterAll(func() {
		By("deleting clusterResourceOverride")
		cleanupClusterResourceOverride(croName)
	})

	It("should deny create for invalid cluster resource override", func() {
		Consistently(func(g Gomega) error {
			cro1 := &placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("test-cro-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						selector,
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
										{
											PropertySelector: &placementv1beta1.PropertySelector{
												MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
													{
														Name:     "example",
														Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
														Values:   []string{"1"},
													},
												},
											},
										},
									},
								},
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/meta/annotations/test-key",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "/kind",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "////",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
								},
							},
						},
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s with the same resource selected", cro1.Name))
			err := hubClient.Create(ctx, cro1)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", selector, cro1.Name, croName)))
			Expect(statusErr.Status().Message).Should(MatchRegexp("only labelSelector is supported"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("remove operation cannot have value"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override typeMeta fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot contain empty string"))
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for CRO UPDATE operations", Ordered, func() {
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	cro1Name := fmt.Sprintf("test-cro-%d", GinkgoParallelProcess())
	cro := &placementv1beta1.ClusterResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name: croName,
		},
		Spec: placementv1beta1.ClusterResourceOverrideSpec{
			ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io/v1",
					Kind:    "ClusterRole",
					Version: "v1",
					Name:    fmt.Sprintf("test-clusterrole-%d", GinkgoParallelProcess()),
				},
			},
			Policy: &placementv1beta1.OverridePolicy{
				OverrideRules: []placementv1beta1.OverrideRule{
					{
						ClusterSelector: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
						},
						JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
							{
								Operator: placementv1beta1.JSONPatchOverrideOpRemove,
								Path:     "/meta/labels/test-key",
							},
						},
					},
				},
			},
		},
	}

	BeforeAll(func() {
		By("creating clusterResourceOverride")
		Expect(hubClient.Create(ctx, cro)).To(Succeed(), "Failed to create CRO %s", croName)
	})

	AfterAll(func() {
		By("deleting clusterResourceOverride")
		cleanupClusterResourceOverride(croName)
		cleanupClusterResourceOverride(cro1Name)
	})

	It("should deny update CRO with invalid cluster resource selections", func() {
		Eventually(func(g Gomega) error {
			var cro placementv1beta1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			invalidSelector := placementv1beta1.ResourceSelectorTerm{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test-key": "test-value"},
				},
				SelectionScope: placementv1beta1.NamespaceWithResources,
			}
			invalidSelector1 := placementv1beta1.ResourceSelectorTerm{
				Group:          "rbac.authorization.k8s.io/v1",
				Kind:           "ClusterRole",
				Version:        "v1",
				SelectionScope: placementv1beta1.NamespaceWithResources,
			}
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, invalidSelector)
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, cro.Spec.ClusterResourceSelectors[0])
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, invalidSelector1)

			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("label selector is not supported for resource selection %+v", invalidSelector))))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource selector %+v already exists, and must be unique", cro.Spec.ClusterResourceSelectors[0])))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("resource name is required for resource selection %+v", invalidSelector1))))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update CRO for invalid cluster resource override", func() {
		Eventually(func(g Gomega) error {
			cro1 := &placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: cro1Name,
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io/v1",
							Kind:    "ClusterRole",
							Version: "v1",
							Name:    "test-clusterrole",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
								},
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, cro1)).To(Succeed(), "Failed to create CRO %s", cro1.Name)
			var cro placementv1beta1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			selector := placementv1beta1.ResourceSelectorTerm{
				Group:          "rbac.authorization.k8s.io/v1",
				Kind:           "ClusterRole",
				Version:        "v1",
				Name:           "test-clusterrole",
				SelectionScope: placementv1beta1.NamespaceWithResources,
			}
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, selector)
			clusterSelectorTerm := placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     "example",
							Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
							Values:   []string{"1"},
						},
					},
				},
			}
			cro.Spec.Policy.OverrideRules[0].ClusterSelector.ClusterSelectorTerms = append(cro.Spec.Policy.OverrideRules[0].ClusterSelector.ClusterSelectorTerms, clusterSelectorTerm)
			cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1beta1.JSONPatchOverride{
				Operator: placementv1beta1.JSONPatchOverrideOpRemove,
				Path:     "/kind",
			})
			cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1beta1.JSONPatchOverride{
				Operator: placementv1beta1.JSONPatchOverrideOpAdd,
				Path:     "",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
			})
			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", selector, cro.Name, cro1.Name)))
			Expect(statusErr.Status().Message).Should(MatchRegexp("only labelSelector is supported"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override typeMeta fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot be empty"))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride CREATE operations", func() {
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	selector := placementv1beta1.ResourceSelector{
		Group:   "apps",
		Kind:    "Deployment",
		Version: "v1",
		Name:    fmt.Sprintf("deployment-test-%d", GinkgoParallelProcess()),
	}
	policy := &placementv1beta1.OverridePolicy{
		OverrideRules: []placementv1beta1.OverrideRule{
			{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key": "value",
								},
							},
						},
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
				JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
					{
						Operator: placementv1beta1.JSONPatchOverrideOpRemove,
						Path:     "/meta/labels/test-key",
					},
				},
			},
		},
	}

	BeforeEach(func() {
		By("creating work resources")
		createWorkResources()
	})

	AfterEach(func() {
		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should deny create RO with invalid resource selection", func() {
		Consistently(func(g Gomega) error {
			ro := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: roNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						selector, selector,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", roName))
			err := hubClient.Create(ctx, ro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource selector %+v already exists, and must be unique", selector)))
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride CREATE operation limitations", Ordered, Serial, func() {
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		By("create 100 resourceOverrides")
		createResourceOverrides(roNamespace, 100)
	})

	AfterAll(func() {
		By("deleting resourceOverrides")
		for i := 0; i < 100; i++ {
			cleanupResourceOverride(fmt.Sprintf(roNameTemplate, i), roNamespace)
		}

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should deny create RO with 100 existing ROs", func() {
		Consistently(func(g Gomega) error {
			By("Try to create the 101st ResourceOverride")
			ro101 := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ro-101",
					Namespace: roNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{
							Group:   "apps",
							Kind:    "Deployment",
							Version: "v1",
							Name:    "test-deployment-101",
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
													"key": "value",
												},
											},
										},
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"key1": "value1",
												},
											},
										},
									},
								},
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, ro101)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("resourceOverride limit has been reached: at most 100 resources can be created"))
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride CREATE operations resource selection limitations", Ordered, Serial, func() {
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	selector := placementv1beta1.ResourceSelector{
		Group:   "apps",
		Kind:    "Deployment",
		Version: "v1",
		Name:    fmt.Sprintf("deployment-test-%d", GinkgoParallelProcess()),
	}
	policy := &placementv1beta1.OverridePolicy{
		OverrideRules: []placementv1beta1.OverrideRule{
			{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key": "value",
								},
							},
						},
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
				JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
					{
						Operator: placementv1beta1.JSONPatchOverrideOpRemove,
						Path:     "/meta/labels/test-key",
					},
				},
			},
		},
	}

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		By("create resourceOverride")
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelector{
					selector,
				},
				Policy: policy,
			},
		}
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create RO %s", roName)
	})

	AfterAll(func() {
		By("deleting ResourceOverride")
		cleanupResourceOverride(roName, roNamespace)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should deny create RO with invalid resource override", func() {
		Consistently(func(g Gomega) error {
			By("create 2nd resourceOverride with same resource selection")
			ro1 := &placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-ro-%d", GinkgoParallelProcess()),
					Namespace: roNamespace,
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						selector,
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								ClusterSelector: nil,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "/kind",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
									{
										Operator: placementv1beta1.JSONPatchOverrideOpReplace,
										Path:     "////",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, ro1)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", selector, ro1.Name, roName)))
			Expect(statusErr.Status().Message).Should(MatchRegexp("remove operation cannot have value"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override typeMeta fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot contain empty string"))
			return nil
		}, consistentlyDuration, consistentlyInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride UPDATE operations", Ordered, func() {
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	ro1Name := fmt.Sprintf("test-ro-%d", GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	selector := placementv1beta1.ResourceSelector{
		Group:   "apps",
		Kind:    "Deployment",
		Version: "v1",
		Name:    fmt.Sprintf("deployment-test-%d", GinkgoParallelProcess()),
	}

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		By("creating ResourceOverride")
		ro := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: []placementv1beta1.ResourceSelector{
					selector,
				},
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpRemove,
									Path:     "/meta/labels/test-key",
								},
							},
						},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, ro)).To(Succeed(), "Failed to create RO %s", roName)
	})

	AfterAll(func() {
		By("deleting ResourceOverride")
		cleanupResourceOverride(roName, roNamespace)
		cleanupResourceOverride(ro1Name, roNamespace)

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should deny update RO with invalid resource selection", func() {
		Eventually(func(g Gomega) error {
			var ro placementv1beta1.ResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, &ro)).Should(Succeed())
			ro.Spec.ResourceSelectors = append(ro.Spec.ResourceSelectors, selector)
			ro.Spec.ResourceSelectors = append(ro.Spec.ResourceSelectors, selector)
			By(fmt.Sprintf("expecting denial of UPDATE override %s", roName))
			err := hubClient.Update(ctx, &ro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource selector %+v already exists, and must be unique", selector)))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	It("should deny update RO with invalid resource override", func() {
		newSelector := placementv1beta1.ResourceSelector{
			Group:   "apps",
			Kind:    "Deployment",
			Version: "v1",
			Name:    "test-deployment-x",
		}
		ro1 := &placementv1beta1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-ro-%d", GinkgoParallelProcess()),
				Namespace: roNamespace,
			},
			Spec: placementv1beta1.ResourceOverrideSpec{
				Policy: &placementv1beta1.OverridePolicy{
					OverrideRules: []placementv1beta1.OverrideRule{
						{
							ClusterSelector: nil,
							JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
								{
									Operator: placementv1beta1.JSONPatchOverrideOpRemove,
									Path:     "/meta/labels/test-key",
								},
							},
						},
					},
				},
			},
		}
		ro1.Spec.ResourceSelectors = append(ro1.Spec.ResourceSelectors, newSelector)
		By("creating a new resource override")
		Expect(hubClient.Create(ctx, ro1)).To(Succeed(), "Failed to create RO %s", ro1.Name)
		Eventually(func(g Gomega) error {
			var ro placementv1beta1.ResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, &ro)).Should(Succeed())
			ro.Spec.ResourceSelectors = append(ro.Spec.ResourceSelectors, newSelector)
			clusterSelectorTerm := placementv1beta1.ClusterSelectorTerm{
				PropertySelector: &placementv1beta1.PropertySelector{
					MatchExpressions: []placementv1beta1.PropertySelectorRequirement{
						{
							Name:     "example",
							Operator: placementv1beta1.PropertySelectorGreaterThanOrEqualTo,
							Values:   []string{"1"},
						},
					},
				},
			}
			ro.Spec.Policy.OverrideRules[0].ClusterSelector.ClusterSelectorTerms = append(ro.Spec.Policy.OverrideRules[0].ClusterSelector.ClusterSelectorTerms, clusterSelectorTerm)
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1beta1.JSONPatchOverride{
				Operator: placementv1beta1.JSONPatchOverrideOpRemove,
				Path:     "/status/conditions/0",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
			})
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1beta1.JSONPatchOverride{
				Operator: placementv1beta1.JSONPatchOverrideOpRemove,
				Path:     "/status/conditions/0",
			})
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1beta1.JSONPatchOverride{
				Operator: placementv1beta1.JSONPatchOverrideOpAdd,
				Path:     "////kind",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
			})
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1beta1.JSONPatchOverride{
				Operator: placementv1beta1.JSONPatchOverrideOpAdd,
				Path:     "/metadata/finalizers/0",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-finalizer"`)},
			})

			By(fmt.Sprintf("expecting denial of UPDATE override %s", roName))
			err := hubClient.Update(ctx, &ro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", newSelector, roName, ro1.Name)))
			Expect(statusErr.Status().Message).Should(MatchRegexp("only labelSelector is supported"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("remove operation cannot have value"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override status fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot contain empty string"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override metadata fields except annotations and labels"))
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})
})
