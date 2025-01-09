/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	crpdbNameTemplate = "test-crpdb-%d"
)

var _ = Describe("Test placement v1alpha1 API validation", func() {
	Context("Test ClusterPlacementDisruptionBudget API validation - valid cases", func() {
		It("should allow creation of ClusterPlacementDisruptionBudget with valid maxUnavailable - int", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid maxUnavailable less than 10% specified as one digit - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "2%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid maxUnavailable less than 10% specified as two digits - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "02%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid maxUnavailable greater than or equal to 10% and less than 100% - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid maxUnavailable equal to 100% - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "100%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid minAvailable - int", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid minAvailable less than 10% specified as one digit - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "5%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid minAvailable less than 10% specified as two digits - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "05%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid minAvailable greater than or equal to 10% and less than 100% - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		It("should allow creation of ClusterPlacementDisruptionBudget with valid minAvailable equal to 100% - string", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "100%"},
				},
			}
			Expect(hubClient.Create(ctx, &crpdb)).Should(Succeed())
		})

		AfterEach(func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
			}
			Expect(hubClient.Delete(ctx, &crpdb)).Should(Succeed())
		})
	})

	Context("Test ClusterPlacementDisruptionBudget API validation - invalid cases", func() {
		It("should deny creation of ClusterPlacementDisruptionBudget when both maxUnavailable and minAvailable are specified", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
					MinAvailable:   &intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("Both MaxUnavailable and MinAvailable cannot be specified"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid maxUnavailable - negative int", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.maxUnavailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid maxUnavailable - negative percentage", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "-1%"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.maxUnavailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid maxUnavailable - greater than 100", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "101%"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.maxUnavailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid maxUnavailable - no percentage specified", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "-1"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.maxUnavailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid minAvailable - negative int", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: -1},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.minAvailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid minAvailable - negative percentage", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "-1%"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.minAvailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid minAvailable - greater than 100", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "101%"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.minAvailable"))
		})

		It("should deny creation of ClusterPlacementDisruptionBudget with invalid minAvailable - no percentage specified", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(crpdbNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.String, StrVal: "-1"},
				},
			}
			err := hubClient.Create(ctx, &crpdb)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRPDB call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid: spec.minAvailable"))
		})
	})
})
