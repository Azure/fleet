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
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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
		Expect(string(statusErr.Status().Message)).Should(ContainSubstring(fmt.Sprintf("the labelSelector and name fields are mutually exclusive in selector %+v", selector[0])))
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
											LabelSelector: metav1.LabelSelector{
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in preferred cluster selector %+v is invalid:", &crp.Spec.Policy.Affinity.ClusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Preference.LabelSelector))))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("unknown when unsatisfiable type random-type"))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("number of cluster cannot be nil for policy type PickN"))
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
									LabelSelector: metav1.LabelSelector{
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(regexp.QuoteMeta(fmt.Sprintf("the labelSelector in cluster selector %+v is invalid:", &crp.Spec.Policy.Affinity.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms[0].LabelSelector))))
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
