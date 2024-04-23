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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	testutils "go.goms.io/fleet/test/e2e/v1alpha1/utils"
)

var _ = Describe("webhook tests for CRP CREATE operations", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	It("should deny create on CRP with invalid label selector", func() {
		selector := invalidWorkResourceSelector()
		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: selector,
			},
		}
		By(fmt.Sprintf("expecting denial of CREATE placement %s", crpName))
		err := hubClient.Create(ctx, crp)
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(statusErr.Status().Message).Should(ContainSubstring(fmt.Sprintf("the labelSelector and name fields are mutually exclusive in selector %+v", selector[0])))
	})

	It("should deny create on CRP with invalid placement policy for PickFixed", func() {
		Eventually(func(g Gomega) error {
			var numOfClusters int32 = 1
			crp := placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("cluster names cannot be empty for policy type"))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("number of clusters must be nil for policy type PickFixed"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create on CRP with invalid placement policy for PickN", func() {
		Eventually(func(g Gomega) error {
			crp := placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in preferred cluster selector %+v is invalid:", crp.Spec.Policy.Affinity.ClusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Preference.LabelSelector))))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("unknown unsatisfiable type random-type"))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("number of cluster cannot be nil for policy type PickN"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta("failed to get GVR of the selector")))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
				Spec: placementv1beta1.ClusterResourcePlacementSpec{
					ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta("the resource is not found in schema (please retry) or it is not a cluster scoped resource")))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for CRP UPDATE operations", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: workResourceSelector(),
			},
		}
		By(fmt.Sprintf("creating placement %s", crpName))
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("the labelSelector and name fields are mutually exclusive"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", crp.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms[0].LabelSelector))))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("topology spread constraints needs to be empty for policy type PickAll"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("placement type is immutable"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
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
		By(fmt.Sprintf("deleting placement %s", crpName))
		cleanupCRP(crpName)

		By("deleting created work resources")
		cleanupWorkResources()
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(fmt.Sprintf("invalid toleration %+v: %s", invalidToleration, "toleration key cannot be empty, when operator is Equal")))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("tolerations have been updated/deleted, only additions to tolerations are allowed"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for MC taints", Ordered, func() {
	mcName := fmt.Sprintf(mcNameTemplate, GinkgoParallelProcess())

	BeforeAll(func() {
		createMemberCluster(mcName, testUser, nil)
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ClusterResourceOverride CREATE operations", func() {
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	selector := placementv1beta1.ClusterResourceSelector{
		Group:   "rbac.authorization.k8s.io/v1",
		Kind:    "ClusterRole",
		Version: "v1",
		Name:    fmt.Sprintf("test-clusterrole-%d", GinkgoParallelProcess()),
	}
	policy := &placementv1alpha1.OverridePolicy{
		OverrideRules: []placementv1alpha1.OverrideRule{
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
				JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
					{
						Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
						Path:     "/meta/labels/test-key",
					},
				},
			},
		},
	}

	It("should deny create CRO with label selector", func() {
		Eventually(func(g Gomega) error {
			invalidSelector := placementv1beta1.ClusterResourceSelector{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test-key": "test-value"},
				},
			}

			// Create the CRO.
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						invalidSelector,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", croName))
			err := hubClient.Create(ctx, cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("label selector is not supported for resource selection %+v", invalidSelector))))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create CRO with duplicate selector", func() {
		Eventually(func(g Gomega) error {
			// Create the CRO.
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						selector, selector,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", croName))
			err := hubClient.Create(ctx, cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource selector %+v already exists, and must be unique", selector)))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create CRO with selector name empty", func() {
		Eventually(func(g Gomega) error {
			invalidSelector := placementv1beta1.ClusterResourceSelector{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
			}
			// Create the CRO.
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						invalidSelector,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", croName))
			err := hubClient.Create(ctx, cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("resource name is required for resource selection %+v", invalidSelector))))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create CRO with invalid cluster selector term", func() {
		Eventually(func(g Gomega) error {
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io/v1",
							Kind:    "ClusterRole",
							Version: "v1",
							Name:    "test-clusterrole",
						},
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", cro.Name))
			err := hubClient.Create(ctx, cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("only labelSelector is supported"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create CRO with invalid JSON patch override", func() {
		Eventually(func(g Gomega) error {
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io/v1",
							Kind:    "ClusterRole",
							Version: "v1",
							Name:    "test-cluster-role",
						},
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
								},
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "/kind",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "////",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
								},
							},
						},
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", cro.Name))
			err := hubClient.Create(ctx, cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("remove operation cannot have value"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override typeMeta fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot contain empty string"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create CRO for a resource already selected by another CRO", func() {
		Eventually(func(g Gomega) error {
			By("Create the 1st ClusterResourceOverride")
			cro := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: croName,
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						selector,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", cro.Name))
			err := hubClient.Create(ctx, cro)
			Expect(err).NotTo(HaveOccurred(), "Failed to create CRO %s", cro.Name)

			By("Try to create the 2nd ClusterResourceOverride")
			cro2 := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("test-cro-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						selector,
					},
					Policy: policy,
				},
			}
			err = hubClient.Create(ctx, cro2)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", selector, cro2.Name, cro.Name)))
			cleanupClusterResourceOverride(croName)
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
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
		Eventually(func(g Gomega) error {
			By("Try to create the 101st ClusterResourceOverride")
			cro101 := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cro-101",
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io/v1",
							Kind:    "ClusterRole",
							Version: "v1",
							Name:    "test-cluster-role-101",
						},
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, cro101)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("clusterResourceOverride limit has been reached: at most 100 cluster resources can be created"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for CRO UPDATE operations", Ordered, func() {
	croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())
	cro := &placementv1alpha1.ClusterResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name: croName,
		},
		Spec: placementv1alpha1.ClusterResourceOverrideSpec{
			ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
				{
					Group:   "rbac.authorization.k8s.io/v1",
					Kind:    "ClusterRole",
					Version: "v1",
					Name:    fmt.Sprintf("test-clusterrole-%d", GinkgoParallelProcess()),
				},
			},
			Policy: &placementv1alpha1.OverridePolicy{
				OverrideRules: []placementv1alpha1.OverrideRule{
					{
						ClusterSelector: &placementv1beta1.ClusterSelector{
							ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
						},
						JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
							{
								Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
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
	})

	It("should deny update CRO with label selector", func() {
		Eventually(func(g Gomega) error {
			var cro placementv1alpha1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			invalidSelector := placementv1beta1.ClusterResourceSelector{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test-key": "test-value"},
				},
			}
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, invalidSelector)

			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("label selector is not supported for resource selection %+v", invalidSelector))))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update CRO with duplicate selector", func() {
		Eventually(func(g Gomega) error {
			var cro placementv1alpha1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, cro.Spec.ClusterResourceSelectors[0])

			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("resource selector %+v already exists, and must be unique", cro.Spec.ClusterResourceSelectors[0])))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update CRO with selector name empty", func() {
		Eventually(func(g Gomega) error {
			var cro placementv1alpha1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			invalidSelector := placementv1beta1.ClusterResourceSelector{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
			}
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, invalidSelector)

			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("resource name is required for resource selection %+v", invalidSelector))))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update CRO with invalid cluster selector term", func() {
		Eventually(func(g Gomega) error {
			var cro placementv1alpha1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
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

			By(fmt.Sprintf("expecting denial of UPDATE override %s", cro.Name))
			err := hubClient.Update(ctx, &cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("only labelSelector is supported"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update CRO for a resource already selected by another CRO", func() {
		Eventually(func(g Gomega) error {
			By("Create another cluster resource override")
			cro1 := &placementv1alpha1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("test-cro-%d", GinkgoParallelProcess()),
				},
				Spec: placementv1alpha1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
						{
							Group:   "rbac.authorization.k8s.io/v1",
							Kind:    "ClusterRole",
							Version: "v1",
							Name:    "test-clusterrole",
						},
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
								},
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, cro1)).To(Succeed(), "Failed to create CRO %s", cro1.Name)
			var cro placementv1alpha1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			selector := placementv1beta1.ClusterResourceSelector{
				Group:   "rbac.authorization.k8s.io/v1",
				Kind:    "ClusterRole",
				Version: "v1",
				Name:    "test-clusterrole",
			}
			cro.Spec.ClusterResourceSelectors = append(cro.Spec.ClusterResourceSelectors, selector)

			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", selector, cro.Name, cro1.Name)))
			cleanupClusterResourceOverride(cro1.Name)
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update CRO with invalid JSONPatchOverride", func() {
		Eventually(func(g Gomega) error {
			var cro placementv1alpha1.ClusterResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: croName}, &cro)).Should(Succeed())
			cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1alpha1.JSONPatchOverride{
				Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
				Path:     "/kind",
			})
			cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(cro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1alpha1.JSONPatchOverride{
				Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
				Path:     "",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
			})

			By(fmt.Sprintf("expecting denial of UPDATE override %s", croName))
			err := hubClient.Update(ctx, &cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override typeMeta fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot be empty"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride CREATE operations", func() {
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	selector := placementv1alpha1.ResourceSelector{
		Group:   "apps",
		Kind:    "Deployment",
		Version: "v1",
		Name:    fmt.Sprintf("deployment-test-%d", GinkgoParallelProcess()),
	}
	policy := &placementv1alpha1.OverridePolicy{
		OverrideRules: []placementv1alpha1.OverrideRule{
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
				JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
					{
						Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
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

	It("should deny create RO with duplicate selector", func() {
		Eventually(func(g Gomega) error {
			ro := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create RO with invalid JSON patch override", func() {
		Eventually(func(g Gomega) error {
			ro := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						selector,
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: nil,
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "/kind",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpReplace,
										Path:     "////",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"new-kind"`)},
									},
								},
							},
						},
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", ro.Name))
			err := hubClient.Create(ctx, ro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("remove operation cannot have value"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override typeMeta fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot contain empty string"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny create RO for a resource already selected by another RO", func() {
		Eventually(func(g Gomega) error {
			By("Create the 1st ResourceOverride")
			ro := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      roName,
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						selector,
					},
					Policy: policy,
				},
			}
			err := hubClient.Create(ctx, ro)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RO %s", ro.Name)

			By("Try to create the 2nd ResourceOverride")
			ro1 := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-ro-%d", GinkgoParallelProcess()),
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						selector,
					},
					Policy: policy,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE override %s", ro1.Name))
			err = hubClient.Create(ctx, ro1)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", selector, ro1.Name, ro.Name)))
			cleanupResourceOverride(roName, roNamespace)
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride CREATE operation limitations", Ordered, Serial, func() {
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		By("Create 100 ResourceOverrides")
		createResourceOverrides(roNamespace, 100)
	})

	AfterAll(func() {
		By("deleting ResourceOverrides")
		for i := 0; i < 100; i++ {
			cleanupResourceOverride(fmt.Sprintf(roNameTemplate, i), roNamespace)
		}

		By("deleting created work resources")
		cleanupWorkResources()
	})
	It("should deny create RO with 100 existing ROs", func() {
		Eventually(func(g Gomega) error {
			By("Try to create the 101st ResourceOverride")
			ro101 := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ro-101",
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							Group:   "apps",
							Kind:    "Deployment",
							Version: "v1",
							Name:    "test-deployment-101",
						},
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
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
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
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
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})

var _ = Describe("webhook tests for ResourceOverride UPDATE operations", Ordered, func() {
	roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())
	roNamespace := fmt.Sprintf(workNamespaceNameTemplate, GinkgoParallelProcess())
	selector := placementv1alpha1.ResourceSelector{
		Group:   "apps",
		Kind:    "Deployment",
		Version: "v1",
		Name:    fmt.Sprintf("deployment-test-%d", GinkgoParallelProcess()),
	}

	BeforeAll(func() {
		By("creating work resources")
		createWorkResources()

		By("creating ResourceOverride")
		ro := &placementv1alpha1.ResourceOverride{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roName,
				Namespace: roNamespace,
			},
			Spec: placementv1alpha1.ResourceOverrideSpec{
				ResourceSelectors: []placementv1alpha1.ResourceSelector{
					selector,
				},
				Policy: &placementv1alpha1.OverridePolicy{
					OverrideRules: []placementv1alpha1.OverrideRule{
						{
							ClusterSelector: &placementv1beta1.ClusterSelector{
								ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
							},
							JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
								{
									Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
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

		By("deleting created work resources")
		cleanupWorkResources()
	})

	It("should deny update RO with duplicate selector", func() {
		Eventually(func(g Gomega) error {
			var ro placementv1alpha1.ResourceOverride
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
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update RO for a resource already selected by another RO", func() {
		Eventually(func(g Gomega) error {
			By("creating a new resource override")
			ro1 := &placementv1alpha1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-ro-%d", GinkgoParallelProcess()),
					Namespace: roNamespace,
				},
				Spec: placementv1alpha1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1alpha1.ResourceSelector{
						{
							Group:   "apps",
							Kind:    "Deployment",
							Version: "v1",
							Name:    "test-deployment-1",
						},
					},
					Policy: &placementv1alpha1.OverridePolicy{
						OverrideRules: []placementv1alpha1.OverrideRule{
							{
								ClusterSelector: &placementv1beta1.ClusterSelector{
									ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{},
								},
								JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
									{
										Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
										Path:     "/meta/labels/test-key",
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, ro1)).To(Succeed(), "Failed to create RO %s", ro1.Name)

			var ro placementv1alpha1.ResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, &ro)).Should(Succeed())
			newSelector := placementv1alpha1.ResourceSelector{
				Group:   "apps",
				Kind:    "Deployment",
				Version: "v1",
				Name:    fmt.Sprintf("test-deployment-%d", 1),
			}
			ro.Spec.ResourceSelectors = append(ro.Spec.ResourceSelectors, newSelector)

			By(fmt.Sprintf("expecting denial of UPDATE override %s", roName))
			err := hubClient.Update(ctx, &ro)
			if k8sErrors.IsConflict(err) {
				return err
			}
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp(fmt.Sprintf("invalid resource selector %+v: the resource has been selected by both %v and %v, which is not supported", newSelector, roName, ro1.Name)))
			cleanupResourceOverride(ro1.Name, roNamespace)
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update RO with invalid cluster selector term", func() {
		Eventually(func(g Gomega) error {
			var ro placementv1alpha1.ResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, &ro)).Should(Succeed())
			policy := &placementv1alpha1.OverridePolicy{
				OverrideRules: []placementv1alpha1.OverrideRule{
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
						JSONPatchOverrides: []placementv1alpha1.JSONPatchOverride{
							{
								Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
								Path:     "/meta/labels/test-key",
							},
						},
					},
				},
			}
			ro.Spec.Policy = policy

			By(fmt.Sprintf("expecting denial of UPDATE override %s", ro.Name))
			err := hubClient.Update(ctx, &ro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("only labelSelector is supported"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update RO with invalid paths in JSONPatchOverride", func() {
		Eventually(func(g Gomega) error {
			var ro placementv1alpha1.ResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, &ro)).Should(Succeed())
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1alpha1.JSONPatchOverride{
				Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
				Path:     "/status/conditions/0",
			})
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1alpha1.JSONPatchOverride{
				Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
				Path:     "////kind",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
			})
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1alpha1.JSONPatchOverride{
				Operator: placementv1alpha1.JSONPatchOverrideOpAdd,
				Path:     "/metadata/finalizers/0",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-finalizer"`)},
			})

			By(fmt.Sprintf("expecting denial of UPDATE override %s", roName))
			err := hubClient.Update(ctx, &ro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override status fields"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("path cannot contain empty string"))
			Expect(statusErr.Status().Message).Should(MatchRegexp("cannot override metadata fields except annotations and labels"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})

	It("should deny update RO with invalid JSONPatchOverride", func() {
		Eventually(func(g Gomega) error {
			var ro placementv1alpha1.ResourceOverride
			g.Expect(hubClient.Get(ctx, types.NamespacedName{Name: roName, Namespace: roNamespace}, &ro)).Should(Succeed())
			ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides = append(ro.Spec.Policy.OverrideRules[0].JSONPatchOverrides, placementv1alpha1.JSONPatchOverride{
				Operator: placementv1alpha1.JSONPatchOverrideOpRemove,
				Path:     "/status/conditions/0",
				Value:    apiextensionsv1.JSON{Raw: []byte(`"new-value"`)},
			})

			By(fmt.Sprintf("expecting denial of UPDATE override %s", roName))
			err := hubClient.Update(ctx, &ro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RO call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(MatchRegexp("remove operation cannot have value"))
			return nil
		}, testutils.PollTimeout, testutils.PollInterval).Should(Succeed())
	})
})
