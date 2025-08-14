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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	crpdbNameTemplate                 = "test-crpdb-%d"
	validupdateRunNameTemplate        = "test-update-run-%d"
	invalidupdateRunNameTemplate      = "test-update-run-with-invalid-length-0123456789-0123456789-0123456789-0123456789-0123456789-0123456789-0123456789-0123456789-0123456789-%d"
	updateRunStrategyNameTemplate     = "test-update-run-strategy-%d"
	updateRunStageNameTemplate        = "stage%d%d"
	invalidupdateRunStageNameTemplate = "stage012345678901234567890123456789012345678901234567890123456789%d%d"
	approveRequestNameTemplate        = "test-approve-request-%d"
	crpNameTemplate                   = "test-crp-%d"
	croNameTemplate                   = "test-cro-%d"
	roNameTemplate                    = "test-ro-%d"
	testNamespace                     = "test-ns"
)

var _ = Describe("Test placement v1beta1 API validation", func() {
	Context("Test ClusterResourcePlacement API validation - invalid cases", func() {
		var crp placementv1beta1.ClusterResourcePlacement
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeEach(func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"cluster1", "cluster2"},
					},
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &crp)).Should(Succeed())
		})

		It("should deny update of ClusterResourcePlacement with nil policy", func() {
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed(), "Get CRP call failed")
			crp.Spec.Policy = nil
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("placement type is immutable"))
		})

		It("should deny update of ClusterResourcePlacement with different placement type", func() {
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &crp)).Should(Succeed(), "Get CRP call failed")
			crp.Spec.Policy.PlacementType = placementv1beta1.PickAllPlacementType
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("placement type is immutable"))
		})
	})

	Context("Test ClusterResourcePlacement StatusReportingScope validation - create, allow cases", func() {
		var crp placementv1beta1.ClusterResourcePlacement
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &crp)).Should(Succeed())
		})

		It("should allow creation of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible and single namespace selector", func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns",
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		It("should allow creation of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible and one namespace plus other cluster-scoped resources", func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolume",
							Name:    "test-pv",
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		It("should allow creation of ClusterResourcePlacement with StatusReportingScope ClusterScopeOnly and multiple namespace selector plus other cluster-scoped resources", func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-1",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-2",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolume",
							Name:    "test-pv",
						},
					},
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		It("should allow creation of ClusterResourcePlacement with default StatusReportingScope and multiple namespace selectors plus other cluster-scoped resources", func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-1",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-2",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolume",
							Name:    "test-pv",
						},
					},
					// StatusReportingScope not specified - should default to ClusterScopeOnly
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})
	})

	Context("Test ClusterResourcePlacement StatusReportingScope validation - create, deny cases", func() {
		var crp placementv1beta1.ClusterResourcePlacement
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		It("should deny creation of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible and multiple namespace selectors", func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-1",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-2",
						},
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			err := hubClient.Create(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("when statusReportingScope is NamespaceAccessible, exactly one resourceSelector with kind 'Namespace' is required"))
		})

		It("should deny creation of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible and no namespace selector", func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "ClusterRole",
							Name:    "test-cluster-role",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolume",
							Name:    "test-pv",
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			err := hubClient.Create(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("when statusReportingScope is NamespaceAccessible, exactly one resourceSelector with kind 'Namespace' is required"))
		})
	})

	Context("Test ClusterResourcePlacement StatusReportingScope validation - update cases", func() {
		var crp placementv1beta1.ClusterResourcePlacement
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		BeforeEach(func() {
			crp = placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    "test-ns-1",
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &crp)).Should(Succeed())
		})

		It("should allow update of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible, one namespace selector", func() {
			crp.Spec.StatusReportingScope = placementv1beta1.NamespaceAccessible
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
		})

		It("should allow update of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible, one namespace plus other cluster-scoped resources", func() {
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "PersistentVolume",
					Name:    "test-pv",
				},
			}...)
			crp.Spec.StatusReportingScope = placementv1beta1.NamespaceAccessible
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
		})

		It("should allow update of ClusterResourcePlacement with StatusReportingScope ClusterScopeOnly, multiple namespace selectors", func() {
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns-2",
				},
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "PersistentVolume",
					Name:    "test-pv",
				},
			}...)
			crp.Spec.StatusReportingScope = placementv1beta1.ClusterScopeOnly
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
		})

		It("should allow update of ClusterResourcePlacement with default StatusReportingScope, multiple namespace selectors", func() {
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns-2",
				},
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "PersistentVolume",
					Name:    "test-pv",
				},
			}...)
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
		})

		It("should deny update of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible and multiple namespace selectors", func() {
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns-2",
				},
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
			}...)
			crp.Spec.StatusReportingScope = placementv1beta1.NamespaceAccessible
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("when statusReportingScope is NamespaceAccessible, exactly one resourceSelector with kind 'Namespace' is required"))
		})

		It("should deny update of ClusterResourcePlacement with StatusReportingScope NamespaceAccessible, no namespace selectors", func() {
			crp.Spec.ResourceSelectors = []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "test-cluster-role",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "PersistentVolume",
					Name:    "test-pv",
				},
			}
			crp.Spec.StatusReportingScope = placementv1beta1.NamespaceAccessible
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("when statusReportingScope is NamespaceAccessible, exactly one resourceSelector with kind 'Namespace' is required"))
		})
	})

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

	Context("Test ClusterStagedUpdateRun API validation - valid cases", func() {
		It("Should allow creation of ClusterStagedUpdateRun with valid name length", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
			}
			Expect(hubClient.Create(ctx, &updateRun)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &updateRun)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateRun API validation - invalid cases", func() {
		It("Should deny creation of ClusterStagedUpdateRun with name length > 127", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(invalidupdateRunNameTemplate, GinkgoParallelProcess()),
				},
			}
			err := hubClient.Create(ctx, &updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("metadata.name max length is 127"))
		})

		It("Should deny update of ClusterStagedUpdateRun spec", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateRunSpec{
					PlacementName: "test-placement",
				},
			}
			Expect(hubClient.Create(ctx, &updateRun)).Should(Succeed())

			updateRun.Spec.PlacementName = "test-placement-2"
			err := hubClient.Update(ctx, &updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update updateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The spec field is immutable"))
			Expect(hubClient.Delete(ctx, &updateRun)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateStrategy API validation - valid cases", func() {
		It("Should allow creation of ClusterStagedUpdateStrategy with valid stage config", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.AfterStageTask{
								{
									Type: placementv1beta1.AfterStageTaskTypeApproval,
								},
								{
									Type:     placementv1beta1.AfterStageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Second * 10},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())
			Expect(strategy.Spec.Stages[0].AfterStageTasks[0].WaitTime).Should(BeNil())
			Expect(strategy.Spec.Stages[0].AfterStageTasks[1].WaitTime).Should(Equal(&metav1.Duration{Duration: time.Second * 10}))
			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateStrategy API validation - invalid cases", func() {
		It("Should deny creation of ClusterStagedUpdateStrategy with more than allowed staged", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
			}
			for i := 0; i < 32; i++ {
				strategy.Spec.Stages = append(strategy.Spec.Stages, placementv1beta1.StageConfig{
					Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), i),
				})
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("spec.stages: Too many: 32: must have at most 31 items"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with invalid stage config - too long stage name", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(invalidupdateRunStageNameTemplate, GinkgoParallelProcess(), 1),
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("Too long: may not be longer than 63"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with invalid stage config - stage name with invalid characters", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1) + "-A",
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("in body should match.*a-z0-9"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with invalid stage config - more than 2 AfterStageTasks", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.AfterStageTask{
								{
									Type: placementv1beta1.AfterStageTaskTypeApproval,
								},
								{
									Type: placementv1beta1.AfterStageTaskTypeApproval,
								},
								{
									Type:     placementv1beta1.AfterStageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Second * 10},
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("Too many: 3: must have at most 2 items"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with AfterStageTask of type Approval with waitTime specified", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.AfterStageTask{
								{
									Type:     placementv1beta1.AfterStageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Minute * 30},
								},
								{
									Type:     placementv1beta1.AfterStageTaskTypeApproval,
									WaitTime: &metav1.Duration{Duration: time.Minute * 10},
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("AfterStageTaskType is Approval, waitTime is not allowed"))
		})

		It("Should deny update of ClusterStagedUpdateStrategy when adding waitTime to AfterStageTask of type Approval", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.AfterStageTask{
								{
									Type: placementv1beta1.AfterStageTaskTypeApproval,
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())

			strategy.Spec.Stages[0].AfterStageTasks[0].WaitTime = &metav1.Duration{Duration: time.Minute * 10}
			err := hubClient.Update(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("AfterStageTaskType is Approval, waitTime is not allowed"))

			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with AfterStageTask of type TimedWait with waitTime not specified", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.AfterStageTask{
								{
									Type: placementv1beta1.AfterStageTaskTypeTimedWait,
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("AfterStageTaskType is TimedWait, waitTime is required"))
		})

		It("Should deny update of ClusterStagedUpdateStrategy when removing waitTime from AfterStageTask of type TimedWait", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.StagedUpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.AfterStageTask{
								{
									Type:     placementv1beta1.AfterStageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Minute * 10},
								},
								{
									Type: placementv1beta1.AfterStageTaskTypeApproval,
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())

			strategy.Spec.Stages[0].AfterStageTasks[0].WaitTime = nil
			err := hubClient.Update(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("AfterStageTaskType is TimedWait, waitTime is required"))

			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})
	})

	Context("Test ClusterApprovalRequest API validation - valid cases", func() {
		It("Should allow creation of ClusterApprovalRequest with valid configurations", func() {
			appReq := placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(approveRequestNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
					TargetStage:     fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
				},
			}
			Expect(hubClient.Create(ctx, &appReq)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &appReq)).Should(Succeed())
		})
	})

	Context("Test ClusterApprovalRequest API validation - invalid cases", func() {
		It("Should deny update of ClusterApprovalRequest spec", func() {
			appReq := placementv1beta1.ClusterApprovalRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(approveRequestNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ApprovalRequestSpec{
					TargetUpdateRun: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
					TargetStage:     fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
				},
			}
			Expect(hubClient.Create(ctx, &appReq)).Should(Succeed())

			appReq.Spec.TargetUpdateRun += "1"
			err := hubClient.Update(ctx, &appReq)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update clusterApprovalRequest call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The spec field is immutable"))
			Expect(hubClient.Delete(ctx, &appReq)).Should(Succeed())
		})
	})

	Context("Test ClusterResourceOverride API validation - valid cases", func() {
		It("should allow creation of ClusterResourceOverride without placement reference", func() {
			cro := placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
		})

		It("should allow creation of ClusterResourceOverride with cluster-scoped placement reference", func() {
			cro := placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.ClusterScoped,
					},
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
		})

		It("should allow creation of ClusterResourceOverride without specifying scope in placement reference", func() {
			cro := placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name: "test-placement",
					},
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
		})
	})

	Context("Test ClusterResourceOverride API validation - invalid cases", func() {
		It("should deny creation of ClusterResourceOverride with namespaced placement reference", func() {
			cro := placementv1beta1.ClusterResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ClusterResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.NamespaceScoped,
					},
					ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, &cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("clusterResourceOverride placement reference cannot be Namespaced scope"))
		})
	})

	Context("Test ResourceOverride API validation - valid cases", func() {
		It("should allow creation of ResourceOverride without placement reference", func() {
			ro := placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})

		It("should allow creation of ResourceOverride with cluster-scoped placement reference", func() {
			ro := placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.ClusterScoped,
					},
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})

		It("should allow creation of ResourceOverride without specifying scope in placement reference", func() {
			ro := placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name: "test-placement",
					},
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})

		It("should allow creation of ResourceOverride with namespace-scoped placement reference", func() {
			ro := placementv1beta1.ResourceOverride{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.ResourceOverrideSpec{
					Placement: &placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.NamespaceScoped,
					},
					ResourceSelectors: []placementv1beta1.ResourceSelector{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.OverridePolicy{
						OverrideRules: []placementv1beta1.OverrideRule{
							{
								OverrideType: placementv1beta1.JSONPatchOverrideType,
								JSONPatchOverrides: []placementv1beta1.JSONPatchOverride{
									{
										Operator: placementv1beta1.JSONPatchOverrideOpAdd,
										Path:     "/metadata/labels/test",
										Value:    apiextensionsv1.JSON{Raw: []byte(`"test-value"`)},
									},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})
	})
})
