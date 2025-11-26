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
	rpNameTemplate                    = "test-rp-%d"
	croNameTemplate                   = "test-cro-%d"
	roNameTemplate                    = "test-ro-%d"
	testNamespace                     = "test-ns"
	unknownScope                      = "UnknownScope"
)

// createValidClusterResourceOverride creates a valid ClusterResourceOverride for testing purposes.
// The placement parameter is optional - pass nil for no placement reference.
func createValidClusterResourceOverride(name string, placement *placementv1beta1.PlacementRef) placementv1beta1.ClusterResourceOverride {
	return placementv1beta1.ClusterResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: placementv1beta1.ClusterResourceOverrideSpec{
			Placement: placement,
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
}

// createValidResourceOverride creates a valid ResourceOverride for testing purposes.
// The placement parameter is optional - pass nil for no placement reference.
func createValidResourceOverride(namespace, name string, placement *placementv1beta1.PlacementRef) placementv1beta1.ResourceOverride {
	return placementv1beta1.ResourceOverride{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: placementv1beta1.ResourceOverrideSpec{
			Placement: placement,
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
}

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
			crp.Spec.Policy = nil
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("policy cannot be removed once set"))
		})

		It("should deny update of ClusterResourcePlacement with different placement type", func() {
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

		It("should allow creation of ClusterResourcePlacement with empty string as StatusReportingScope and multiple namespace selectors plus other cluster-scoped resources", func() {
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
					StatusReportingScope: "", // defaults to ClusterScopeOnly.
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})
	})

	Context("Test ClusterResourcePlacement StatusReportingScope validation - create, deny cases", func() {
		var crp placementv1beta1.ClusterResourcePlacement
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

		It("should deny creation of ClusterResourcePlacement with Unknown StatusReportingScope and multiple namespace selectors", func() {
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
					StatusReportingScope: unknownScope, // Invalid scope
				},
			}
			err := hubClient.Create(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("supported values: \"ClusterScopeOnly\", \"NamespaceAccessible\""))
		})

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

	Context("Test ClusterResourcePlacement ClusterScopeOnly StatusReportingScope validation - update cases", func() {
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
					// By default, StatusReportingScope is ClusterScopeOnly
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &crp)).Should(Succeed())
		})

		It("should allow empty string for StatusReportingScope in a ClusterResourcePlacement when StatusReportingScope is not set", func() {
			Expect(crp.Spec.StatusReportingScope).To(Equal(placementv1beta1.ClusterScopeOnly), "CRP should have default StatusReportingScope ClusterScopeOnly")
			crp.Spec.StatusReportingScope = "" // Empty string should default to ClusterScopeOnly
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
			Expect(crp.Spec.StatusReportingScope).To(Equal(placementv1beta1.ClusterScopeOnly), "CRP should have default StatusReportingScope ClusterScopeOnly")
		})

		It("should allow update of ClusterResourcePlacement which has default StatusReportingScope, multiple namespace resource selectors", func() {
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns-2",
				},
			}...)
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
		})

		It("should allow update of ClusterResourcePlacement with StatusReportingScope ClusterScopeOnly, multiple namespace resource selectors", func() {
			crp.Spec.ResourceSelectors = append(crp.Spec.ResourceSelectors, []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-ns-2",
				},
			}...)
			crp.Spec.StatusReportingScope = placementv1beta1.ClusterScopeOnly
			Expect(hubClient.Update(ctx, &crp)).Should(Succeed())
		})

		It("should deny update of ClusterResourcePlacement StatusReportingScope to NamespaceAccessible due to immutability", func() {
			crp.Spec.StatusReportingScope = placementv1beta1.NamespaceAccessible
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("statusReportingScope is immutable"))
		})

		It("should deny update of ClusterResourcePlacement StatusReportingScope to unknown scope", func() {
			crp.Spec.StatusReportingScope = unknownScope // Invalid scope
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("supported values: \"ClusterScopeOnly\", \"NamespaceAccessible\""))
		})
	})

	Context("Test ClusterResourcePlacement NamespaceAccessible StatusReportingScope validation - update cases", func() {
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
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, &crp)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &crp)).Should(Succeed())
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
			}...)
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
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("when statusReportingScope is NamespaceAccessible, exactly one resourceSelector with kind 'Namespace' is required"))
		})

		It("should deny update of ClusterResourcePlacement StatusReportingScope to ClusterScopeOnly due to immutability", func() {
			crp.Spec.StatusReportingScope = placementv1beta1.ClusterScopeOnly
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("statusReportingScope is immutable"))
		})

		It("should deny update of ClusterResourcePlacement StatusReportingScope to empty string", func() {
			crp.Spec.StatusReportingScope = ""
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("statusReportingScope is immutable"))
		})

		It("should deny update of ClusterResourcePlacement StatusReportingScope to unknown scope", func() {
			crp.Spec.StatusReportingScope = unknownScope // Invalid scope
			err := hubClient.Update(ctx, &crp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("supported values: \"ClusterScopeOnly\", \"NamespaceAccessible\""))
		})
	})

	Context("Test ResourcePlacement API validation - invalid cases", func() {
		var rp placementv1beta1.ResourcePlacement
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		BeforeEach(func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames:  []string{"cluster1", "cluster2"},
					},
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &rp)).Should(Succeed())
		})

		It("should deny update of ResourcePlacement with nil policy", func() {
			rp.Spec.Policy = nil
			err := hubClient.Update(ctx, &rp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("policy cannot be removed once set"))
		})

		It("should deny update of ResourcePlacement with different placement type", func() {
			rp.Spec.Policy.PlacementType = placementv1beta1.PickAllPlacementType
			err := hubClient.Update(ctx, &rp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("placement type is immutable"))
		})
	})

	Context("Test ResourcePlacement StatusReportingScope validation, allow cases", func() {
		var rp placementv1beta1.ResourcePlacement
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, &rp)).Should(Succeed())
		})

		It("should allow creation of ResourcePlacement with StatusReportingScope NamespaceAccessible, with no namespace resource selected", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm-1",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "Secret",
							Name:    "test-secret",
						},
					},
					StatusReportingScope: placementv1beta1.NamespaceAccessible,
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
		})

		It("should allow creation of ResourcePlacement with StatusReportingScope ClusterScopeOnly", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
						{
							Group:   "",
							Version: "v1",
							Kind:    "Secret",
							Name:    "test-secret",
						},
					},
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
		})

		It("should allow creation of ResourcePlacement with StatusReportingScope set to empty string", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					StatusReportingScope: "",
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
		})

		It("should allow creation of ResourcePlacement with StatusReportingScope not specified", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
		})

		It("should allow update of ResourcePlacement StatusReportingScope, no immutability constraint", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
			rp.Spec.StatusReportingScope = placementv1beta1.NamespaceAccessible
			Expect(hubClient.Update(ctx, &rp)).Should(Succeed())
		})
	})

	Context("Test ResourcePlacement StatusReportingScope validation, deny cases", func() {
		var rp placementv1beta1.ResourcePlacement
		rpName := fmt.Sprintf(rpNameTemplate, GinkgoParallelProcess())

		It("should deny creation of ResourcePlacement with Unknown StatusReportingScope value", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					StatusReportingScope: unknownScope, // Invalid scope
				},
			}
			err := hubClient.Create(ctx, &rp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create RP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("supported values: \"ClusterScopeOnly\", \"NamespaceAccessible\""))
		})

		It("should deny update of ResourcePlacement StatusReportingScope to unknown scope due to enum validation", func() {
			rp = placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rpName,
					Namespace: testNamespace,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
							Name:    "test-cm",
						},
					},
					StatusReportingScope: placementv1beta1.ClusterScopeOnly,
				},
			}
			Expect(hubClient.Create(ctx, &rp)).Should(Succeed())
			rp.Spec.StatusReportingScope = unknownScope // Invalid scope - should fail due to enum validation
			err := hubClient.Update(ctx, &rp)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update RP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("supported values: \"ClusterScopeOnly\", \"NamespaceAccessible\""))

			// Cleanup after the test.
			Expect(hubClient.Delete(ctx, &rp)).Should(Succeed())
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

		It("Should deny update of ClusterStagedUpdateRun placementName field", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "1",
					StagedUpdateStrategyName: "test-strategy",
				},
			}
			Expect(hubClient.Create(ctx, &updateRun)).Should(Succeed())

			updateRun.Spec.PlacementName = "test-placement-2"
			err := hubClient.Update(ctx, &updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update updateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("placementName is immutable"))
			Expect(hubClient.Delete(ctx, &updateRun)).Should(Succeed())
		})

		It("Should deny update of ClusterStagedUpdateRun resourceSnapshotIndex field", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "1",
					StagedUpdateStrategyName: "test-strategy",
				},
			}
			Expect(hubClient.Create(ctx, &updateRun)).Should(Succeed())

			updateRun.Spec.ResourceSnapshotIndex = "2"
			err := hubClient.Update(ctx, &updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update updateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("resourceSnapshotIndex is immutable"))
			Expect(hubClient.Delete(ctx, &updateRun)).Should(Succeed())
		})

		It("Should deny update of ClusterStagedUpdateRun stagedRolloutStrategyName field", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "1",
					StagedUpdateStrategyName: "test-strategy",
				},
			}
			Expect(hubClient.Create(ctx, &updateRun)).Should(Succeed())

			updateRun.Spec.StagedUpdateStrategyName = "test-strategy-2"
			err := hubClient.Update(ctx, &updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update updateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("stagedRolloutStrategyName is immutable"))
			Expect(hubClient.Delete(ctx, &updateRun)).Should(Succeed())
		})

		It("Should allow update of ClusterStagedUpdateRun state field", func() {
			updateRun := placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "1",
					StagedUpdateStrategyName: "test-strategy",
					State:                    placementv1beta1.StateNotStarted,
				},
			}
			Expect(hubClient.Create(ctx, &updateRun)).Should(Succeed())

			updateRun.Spec.State = placementv1beta1.StateStarted
			Expect(hubClient.Update(ctx, &updateRun)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &updateRun)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateStrategy API validation - valid cases", func() {
		It("Should allow creation of ClusterStagedUpdateStrategy with valid stage config", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
								{
									Type:     placementv1beta1.StageTaskTypeTimedWait,
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

		It("Should allow creation of ClusterStagedUpdateStrategy with valid BeforeStageTask of type Approval", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							BeforeStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
							},
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type:     placementv1beta1.StageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Second * 10},
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())
			Expect(strategy.Spec.Stages[0].BeforeStageTasks[0].Type).Should(Equal(placementv1beta1.StageTaskTypeApproval))
			Expect(strategy.Spec.Stages[0].BeforeStageTasks[0].WaitTime).Should(BeNil())
			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})

		It("Should allow creation of ClusterStagedUpdateStrategy with MaxConcurrency as integer between 1-100", func() {
			maxConcurrency := intstr.FromInt(70)
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())
			Expect(*strategy.Spec.Stages[0].MaxConcurrency).Should(Equal(maxConcurrency))
			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})

		It("Should allow creation of ClusterStagedUpdateStrategy with MaxConcurrency as integer greater than 100", func() {
			maxConcurrency := intstr.FromInt(150)
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())
			Expect(*strategy.Spec.Stages[0].MaxConcurrency).Should(Equal(maxConcurrency))
			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})

		It("Should allow creation of ClusterStagedUpdateStrategy with MaxConcurrency as 1%", func() {
			maxConcurrency := intstr.FromString("1%")
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())
			Expect(*strategy.Spec.Stages[0].MaxConcurrency).Should(Equal(maxConcurrency))
			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})

		It("Should allow creation of ClusterStagedUpdateStrategy with MaxConcurrency as 100%", func() {
			maxConcurrency := intstr.FromString("100%")
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())
			Expect(*strategy.Spec.Stages[0].MaxConcurrency).Should(Equal(maxConcurrency))
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
				Spec: placementv1beta1.UpdateStrategySpec{
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
				Spec: placementv1beta1.UpdateStrategySpec{
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
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
								{
									Type:     placementv1beta1.StageTaskTypeTimedWait,
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
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type:     placementv1beta1.StageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Minute * 30},
								},
								{
									Type:     placementv1beta1.StageTaskTypeApproval,
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
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeApproval,
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
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeTimedWait,
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
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							AfterStageTasks: []placementv1beta1.StageTask{
								{
									Type:     placementv1beta1.StageTaskTypeTimedWait,
									WaitTime: &metav1.Duration{Duration: time.Minute * 10},
								},
								{
									Type: placementv1beta1.StageTaskTypeApproval,
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

		It("Should deny creation of ClusterStagedUpdateStrategy with BeforeStageTask of type TimedWait", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							BeforeStageTasks: []placementv1beta1.StageTask{
								{
									Type:     placementv1beta1.StageTaskTypeTimedWait,
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
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("BeforeStageTaskType cannot be TimedWait"))
		})

		It("Should deny update of ClusterStagedUpdateStrategy when changing BeforeStageTask type to TimedWait", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							BeforeStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
							},
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, &strategy)).Should(Succeed())

			strategy.Spec.Stages[0].BeforeStageTasks[0].Type = placementv1beta1.StageTaskTypeTimedWait
			strategy.Spec.Stages[0].BeforeStageTasks[0].WaitTime = &metav1.Duration{Duration: time.Minute * 10}
			err := hubClient.Update(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("BeforeStageTaskType cannot be TimedWait"))

			Expect(hubClient.Delete(ctx, &strategy)).Should(Succeed())
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with more than 1 BeforeStageTask", func() {
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name: fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							BeforeStageTasks: []placementv1beta1.StageTask{
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
								{
									Type: placementv1beta1.StageTaskTypeApproval,
								},
							},
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("Too many: 2: must have at most 1 items"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with MaxConcurrency set to a negative value", func() {
			maxConcurrency := intstr.FromInt(-1)
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("maxConcurrency must be at least 1"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with MaxConcurrency set to 0", func() {
			maxConcurrency := intstr.FromInt(0)
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("maxConcurrency must be at least 1"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with MaxConcurrency set to '0'", func() {
			maxConcurrency := intstr.FromString("0")
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("spec.stages\\[0\\].maxConcurrency in body should match"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with MaxConcurrency set to '50'", func() {
			maxConcurrency := intstr.FromString("50")
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("spec.stages\\[0\\].maxConcurrency in body should match"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with MaxConcurrency set to 0%", func() {
			maxConcurrency := intstr.FromString("0%")
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("spec.stages\\[0\\].maxConcurrency in body should match"))
		})

		It("Should deny creation of ClusterStagedUpdateStrategy with MaxConcurrency set to 101%", func() {
			maxConcurrency := intstr.FromString("101%")
			strategy := placementv1beta1.ClusterStagedUpdateStrategy{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(updateRunStrategyNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateStrategySpec{
					Stages: []placementv1beta1.StageConfig{
						{
							Name:           fmt.Sprintf(updateRunStageNameTemplate, GinkgoParallelProcess(), 1),
							MaxConcurrency: &maxConcurrency,
						},
					},
				},
			}
			err := hubClient.Create(ctx, &strategy)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create updateRunStrategy call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("spec.stages\\[0\\].maxConcurrency in body should match"))
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

	Context("Test ClusterStagedUpdateRun State API validation - valid Initialize state transitions", func() {
		var updateRun *placementv1beta1.ClusterStagedUpdateRun
		updateRunName := fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess())

		BeforeEach(func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateNotStarted,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, updateRun)).Should(Succeed())
		})

		It("should allow creation of ClusterStagedUpdateRun when state in unspecified", func() {
			updateRunWithDefaultState := &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unspecfied-state-update-run-" + fmt.Sprintf("%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					// State not specified - should default to Initialize
				},
			}
			Expect(hubClient.Create(ctx, updateRunWithDefaultState)).Should(Succeed())
			Expect(updateRunWithDefaultState.Spec.State).To(Equal(placementv1beta1.StateNotStarted))
			Expect(hubClient.Delete(ctx, updateRunWithDefaultState)).Should(Succeed())
		})

		It("should allow creation of ClusterStagedUpdateRun with empty state (defaults to Initialize)", func() {
			updateRun := &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "empty-state-update-run-" + fmt.Sprintf("%d", GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: "",
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())
			Expect(updateRun.Spec.State).To(Equal(placementv1beta1.StateNotStarted))
			Expect(hubClient.Delete(ctx, updateRun)).Should(Succeed())
		})

		It("should allow transition from Initialize to Execute", func() {
			updateRun.Spec.State = placementv1beta1.StateStarted
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})

		It("should allow transition from Initialize to Abandon", func() {
			updateRun.Spec.State = placementv1beta1.StateAbandoned
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateRun State API validation - valid Execute state transitions", func() {
		var updateRun *placementv1beta1.ClusterStagedUpdateRun
		updateRunName := fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess())

		BeforeEach(func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateStarted,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, updateRun)).Should(Succeed())
		})

		It("should allow transition from Execute to Pause", func() {
			updateRun.Spec.State = placementv1beta1.StateStopped
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})

		It("should allow transition from Execute to Abandon", func() {
			updateRun.Spec.State = placementv1beta1.StateAbandoned
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateRun State API validation - valid Pause state transitions", func() {
		var updateRun *placementv1beta1.ClusterStagedUpdateRun
		updateRunName := fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess())

		BeforeEach(func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateStarted,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())
			// Transition to Pause state first
			updateRun.Spec.State = placementv1beta1.StateStopped
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, updateRun)).Should(Succeed())
		})

		It("should allow transition from Pause to Execute", func() {
			updateRun.Spec.State = placementv1beta1.StateStarted
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})

		It("should allow transition from Pause to Abandon", func() {
			updateRun.Spec.State = placementv1beta1.StateAbandoned
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())
		})
	})

	Context("Test ClusterStagedUpdateRun State API validation - invalid state transitions", func() {
		var updateRun *placementv1beta1.ClusterStagedUpdateRun
		updateRunName := fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess())

		AfterEach(func() {
			if updateRun != nil {
				Expect(hubClient.Delete(ctx, updateRun)).Should(Succeed())
			}
		})

		It("should deny transition from Initialize to Pause", func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateNotStarted,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())

			updateRun.Spec.State = placementv1beta1.StateStopped
			err := hubClient.Update(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid state transition: cannot transition from Initialize to Pause"))
		})

		It("should deny transition from Execute to Initialize", func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateStarted,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())

			updateRun.Spec.State = placementv1beta1.StateNotStarted
			err := hubClient.Update(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid state transition: cannot transition from Execute to Initialize"))
		})

		It("should deny transition from Pause to Initialize", func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateStarted,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())

			// Transition to Pause first
			updateRun.Spec.State = placementv1beta1.StateStopped
			Expect(hubClient.Update(ctx, updateRun)).Should(Succeed())

			// Try to transition back to Initialize
			updateRun.Spec.State = placementv1beta1.StateNotStarted
			err := hubClient.Update(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid state transition: cannot transition from Pause to Initialize"))
		})

		It("should deny transition from Abandon to Initialize", func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateAbandoned,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())

			updateRun.Spec.State = placementv1beta1.StateNotStarted
			err := hubClient.Update(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid state transition: Abandon is a terminal state and cannot transition to any other state"))
		})

		It("should deny transition from Abandon to Execute", func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateAbandoned,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())

			updateRun.Spec.State = placementv1beta1.StateStarted
			err := hubClient.Update(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid state transition: Abandon is a terminal state and cannot transition to any other state"))
		})

		It("should deny transition from Abandon to Pause", func() {
			updateRun = &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: updateRunName,
				},
				Spec: placementv1beta1.UpdateRunSpec{
					State: placementv1beta1.StateAbandoned,
				},
			}
			Expect(hubClient.Create(ctx, updateRun)).Should(Succeed())

			updateRun.Spec.State = placementv1beta1.StateStopped
			err := hubClient.Update(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("invalid state transition: Abandon is a terminal state and cannot transition to any other state"))
		})
	})

	Context("Test ClusterStagedUpdateRun State API validation - invalid state values", func() {
		It("should deny creation of ClusterStagedUpdateRun with invalid state value", func() {
			updateRun := &placementv1beta1.ClusterStagedUpdateRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(validupdateRunNameTemplate, GinkgoParallelProcess()),
				},
				Spec: placementv1beta1.UpdateRunSpec{
					PlacementName:            "test-placement",
					ResourceSnapshotIndex:    "1",
					StagedUpdateStrategyName: "test-strategy",
					State:                    "InvalidState",
				},
			}
			err := hubClient.Create(ctx, updateRun)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterStagedUpdateRun call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("supported values: \"Initialize\", \"Execute\", \"Pause\", \"Abandon\""))
		})
	})

	Context("Test ClusterResourceOverride API validation - valid cases", func() {
		It("should allow creation of ClusterResourceOverride without placement reference", func() {
			cro := createValidClusterResourceOverride(
				fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				nil,
			)
			Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
		})

		It("should allow creation of ClusterResourceOverride with cluster-scoped placement reference", func() {
			cro := createValidClusterResourceOverride(
				fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				&placementv1beta1.PlacementRef{
					Name:  "test-placement",
					Scope: placementv1beta1.ClusterScoped,
				},
			)
			Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
		})

		It("should allow creation of ClusterResourceOverride without specifying scope in placement reference", func() {
			cro := createValidClusterResourceOverride(
				fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				&placementv1beta1.PlacementRef{
					Name: "test-placement",
				},
			)
			Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
		})
	})

	Context("Test ClusterResourceOverride API validation - invalid cases", func() {
		It("should deny creation of ClusterResourceOverride with namespaced placement reference", func() {
			cro := createValidClusterResourceOverride(
				fmt.Sprintf(croNameTemplate, GinkgoParallelProcess()),
				&placementv1beta1.PlacementRef{
					Name:  "test-placement",
					Scope: placementv1beta1.NamespaceScoped,
				},
			)
			err := hubClient.Create(ctx, &cro)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ClusterResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("clusterResourceOverride placement reference cannot be Namespaced scope"))
		})

		Context("Test ClusterResourceOverride API validation - placement update invalid cases", func() {
			var cro placementv1beta1.ClusterResourceOverride
			croName := fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())

			BeforeEach(func() {
				cro = createValidClusterResourceOverride(
					croName,
					&placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.ClusterScoped,
					},
				)
				Expect(hubClient.Create(ctx, &cro)).Should(Succeed())
			})

			AfterEach(func() {
				Expect(hubClient.Delete(ctx, &cro)).Should(Succeed())
			})

			It("should deny update of ClusterResourceOverride placement name", func() {
				cro.Spec.Placement.Name = "different-placement"
				err := hubClient.Update(ctx, &cro)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))
			})

			It("should deny update of ClusterResourceOverride placement scope", func() {
				cro.Spec.Placement.Scope = placementv1beta1.NamespaceScoped
				err := hubClient.Update(ctx, &cro)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(ContainSubstring("placement reference cannot be Namespaced scope"))
			})

			It("should deny update of ClusterResourceOverride placement from nil to non-nil", func() {
				croWithoutPlacement := createValidClusterResourceOverride(
					fmt.Sprintf(croNameTemplate, GinkgoParallelProcess())+"-nil",
					nil,
				)
				Expect(hubClient.Create(ctx, &croWithoutPlacement)).Should(Succeed())

				croWithoutPlacement.Spec.Placement = &placementv1beta1.PlacementRef{
					Name:  "new-placement",
					Scope: placementv1beta1.ClusterScoped,
				}
				err := hubClient.Update(ctx, &croWithoutPlacement)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))

				Expect(hubClient.Delete(ctx, &croWithoutPlacement)).Should(Succeed())
			})

			It("should deny update of ClusterResourceOverride placement from non-nil to nil", func() {
				cro.Spec.Placement = nil
				err := hubClient.Update(ctx, &cro)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ClusterResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))
			})
		})
	})

	Context("Test ResourceOverride API validation - valid cases", func() {
		It("should allow creation of ResourceOverride without placement reference", func() {
			ro := createValidResourceOverride(
				testNamespace,
				fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				nil,
			)
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})

		It("should allow creation of ResourceOverride with cluster-scoped placement reference", func() {
			ro := createValidResourceOverride(
				testNamespace,
				fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				&placementv1beta1.PlacementRef{
					Name:  "test-placement",
					Scope: placementv1beta1.ClusterScoped,
				},
			)
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})

		It("should allow creation of ResourceOverride without specifying scope in placement reference", func() {
			ro := createValidResourceOverride(
				testNamespace,
				fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				&placementv1beta1.PlacementRef{
					Name: "test-placement",
				},
			)
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})

		It("should allow creation of ResourceOverride with namespace-scoped placement reference", func() {
			ro := createValidResourceOverride(
				testNamespace,
				fmt.Sprintf(roNameTemplate, GinkgoParallelProcess()),
				&placementv1beta1.PlacementRef{
					Name:  "test-placement",
					Scope: placementv1beta1.NamespaceScoped,
				},
			)
			Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
		})
	})

	Context("Test ResourceOverride API validation - invalid cases", func() {

		Context("Test ResourceOverride API validation - placement update invalid cases", func() {
			var ro placementv1beta1.ResourceOverride
			roName := fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())

			BeforeEach(func() {
				ro = createValidResourceOverride(
					testNamespace,
					roName,
					&placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.ClusterScoped,
					},
				)
				Expect(hubClient.Create(ctx, &ro)).Should(Succeed())
			})

			AfterEach(func() {
				Expect(hubClient.Delete(ctx, &ro)).Should(Succeed())
			})

			It("should deny update of ResourceOverride placement name", func() {
				ro.Spec.Placement.Name = "different-placement"
				err := hubClient.Update(ctx, &ro)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))
			})

			It("should deny update of ResourceOverride placement from nil to non-nil", func() {
				roWithoutPlacement := createValidResourceOverride(
					testNamespace,
					fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())+"-nil",
					nil,
				)
				Expect(hubClient.Create(ctx, &roWithoutPlacement)).Should(Succeed())

				roWithoutPlacement.Spec.Placement = &placementv1beta1.PlacementRef{
					Name:  "new-placement",
					Scope: placementv1beta1.ClusterScoped,
				}
				err := hubClient.Update(ctx, &roWithoutPlacement)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))

				Expect(hubClient.Delete(ctx, &roWithoutPlacement)).Should(Succeed())
			})

			It("should deny update of ResourceOverride placement from non-nil to nil", func() {
				ro.Spec.Placement = nil
				err := hubClient.Update(ctx, &ro)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))
			})

			It("should deny update of ResourceOverride placement from cluster-scoped to namespace-scoped", func() {
				ro.Spec.Placement.Scope = placementv1beta1.NamespaceScoped
				err := hubClient.Update(ctx, &ro)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))
			})

			It("should deny update of ResourceOverride placement from namespace-scoped to cluster-scoped", func() {
				roWithNamespaceScope := createValidResourceOverride(
					testNamespace,
					fmt.Sprintf(roNameTemplate, GinkgoParallelProcess())+"-ns",
					&placementv1beta1.PlacementRef{
						Name:  "test-placement",
						Scope: placementv1beta1.NamespaceScoped,
					},
				)
				Expect(hubClient.Create(ctx, &roWithNamespaceScope)).Should(Succeed())

				roWithNamespaceScope.Spec.Placement.Scope = placementv1beta1.ClusterScoped
				err := hubClient.Update(ctx, &roWithNamespaceScope)
				var statusErr *k8sErrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update ResourceOverride call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
				Expect(statusErr.ErrStatus.Message).Should(MatchRegexp("The placement field is immutable"))

				Expect(hubClient.Delete(ctx, &roWithNamespaceScope)).Should(Succeed())
			})
		})
	})
})
