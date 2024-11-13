/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacementeviction

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	crbNameTemplate      = "crb-%d"
	crpNameTemplate      = "crp-%d"
	evictionNameTemplate = "eviction-%d"
)

var (
	lessFuncCondition = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}

	evictionStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
		cmpopts.EquateEmpty(),
	}
)

const (
	eventuallyDuration   = time.Minute * 2
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Second * 10
	consistentlyInterval = time.Millisecond * 500
)

var _ = Describe("Test ClusterResourcePlacementEviction Controller", Ordered, func() {
	Context("Invalid Eviction - ClusterResourcePlacement not found", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: false, msg: evictionInvalidMissingCRP}, nil)
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Clean up resources", func() {
			// Delete eviction.
			ensureEvictionRemoved(evictionName)
		})
	})

	Context("Invalid Eviction - ClusterResourceBinding not found", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		It("Create ClusterResourcePlacement", func() {
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: false, msg: evictionInvalidMissingCRB}, nil)
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Clean up resources", func() {
			ensureEvictionRemoved(evictionName)
			ensureCRPRemoved(crpName)
		})
	})

	Context("Invalid Eviction - Multiple ClusterResourceBindings for one cluster", func() {
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())
		anotherCRBName := fmt.Sprintf("another-crb-%d", GinkgoParallelProcess())

		It("Create ClusterResourcePlacement", func() {
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourceBinding", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   crbName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create another ClusterResourceBinding", func() {
			// Create anotherCRB.
			anotherCRB := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   anotherCRBName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &anotherCRB)).Should(Succeed())
			// ensure another CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: anotherCRB.Name}, &anotherCRB)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: false, msg: evictionInvalidMultipleCRB}, nil)
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Ensure eviction was not successful", func() {
			var crb, anotherCRB placementv1beta1.ClusterResourceBinding
			// check to see CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
			// check to see another CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: anotherCRBName}, &anotherCRB))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Clean up resources", func() {
			ensureEvictionRemoved(evictionName)
			ensureCRBRemoved(anotherCRBName)
			ensureCRBRemoved(crbName)
			ensureCRPRemoved(crpName)
		})
	})

	Context("Eviction Allowed - ClusterResourcePlacementDisruptionBudget is not present", func() {
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		It("Create ClusterResourcePlacement", func() {
			// Create CRP.
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourceBinding", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   crbName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValid}, &isExecutedEviction{bool: true, msg: evictionAllowedNoPDB})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Ensure eviction was successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// Ensure CRB was deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Clean up resources", func() {
			ensureEvictionRemoved(evictionName)
			ensureCRPRemoved(crpName)
		})
	})

	Context("Eviction Blocked - ClusterResourcePlacementDisruptionBudget's maxUnavailable blocks eviction", func() {
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		It("Create ClusterResourcePlacement", func() {
			// Create CRP.
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourceBinding", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   crbName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementDisruptionBudget", func() {
			crpdb := placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crpdb)).Should(Succeed())
			// ensure CRPDB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crpdb.Name}, &crpdb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValid}, &isExecutedEviction{bool: false, msg: fmt.Sprintf(evictionBlockedPDBSpecified, 0, 0, 1, 1)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Ensure eviction was not successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// check to see CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Clean up resources", func() {
			ensureEvictionRemoved(evictionName)
			ensureCRPDBRemoved(crpName)
			ensureCRBRemoved(crbName)
			ensureCRPRemoved(crpName)
		})
	})

	Context("Eviction Allowed - ClusterResourcePlacementDisruptionBudget's maxUnavailable allows eviction", func() {
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		It("Create ClusterResourcePlacement", func() {
			// Create CRP.
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourceBinding and update status with available condition", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   crbName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateBound,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Update CRB status to have available condition.
			// Ideally binding would contain more condition before available, but for the sake testing we only specify available condition.
			availableCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "available",
				ObservedGeneration: crb.GetGeneration(),
			}
			crb.SetConditions(availableCondition)
			Expect(k8sClient.Status().Update(ctx, &crb)).Should(Succeed())
		})

		It("Create ClusterResourcePlacementDisruptionBudget", func() {
			crpdb := placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crpdb)).Should(Succeed())
			// ensure CRPDB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crpdb.Name}, &crpdb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValid}, &isExecutedEviction{bool: true, msg: fmt.Sprintf(evictionAllowedPDBSpecified, 1, 1, 1, 1)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Ensure eviction was successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// Ensure CRB was deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Clean up resources", func() {
			ensureEvictionRemoved(evictionName)
			ensureCRPDBRemoved(crpName)
			ensureCRPRemoved(crpName)
		})
	})

	Context("Eviction Blocked - ClusterResourcePlacementDisruptionBudget's minAvailable blocks eviction", func() {
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		It("Create ClusterResourcePlacement", func() {
			// Create CRP.
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourceBinding", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   crbName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementDisruptionBudget", func() {
			crpdb := placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crpdb)).Should(Succeed())
			// ensure CRPDB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crpdb.Name}, &crpdb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValid}, &isExecutedEviction{bool: false, msg: fmt.Sprintf(evictionBlockedPDBSpecified, 0, 0, 1, 1)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Ensure eviction was not successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// check to see CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Clean up resources", func() {
			ensureEvictionRemoved(evictionName)
			ensureCRPDBRemoved(crpName)
			ensureCRBRemoved(crbName)
			ensureCRPRemoved(crpName)
		})
	})

	Context("Eviction Allowed - ClusterResourcePlacementDisruptionBudget's minUnavailable allows eviction", func() {
		evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
		crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())
		anotherCRBName := fmt.Sprintf("another-crb-%d", GinkgoParallelProcess())

		It("Create ClusterResourcePlacement", func() {
			// Create CRP.
			crp := buildTestCRP(crpName)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create two ClusterResourceBindings with available condition specified", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   crbName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateBound,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Create another CRB.
			anotherCRB := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:   anotherCRBName,
					Labels: map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "another-test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &anotherCRB)).Should(Succeed())
			// ensure CRB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crb.Name}, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())

			// Update CRB status to have available condition.
			// Ideally binding would contain more condition before available, but for the sake testing we only specify available condition.
			availableCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "available",
				ObservedGeneration: crb.GetGeneration(),
			}
			crb.SetConditions(availableCondition)
			Expect(k8sClient.Status().Update(ctx, &crb)).Should(Succeed())
			availableCondition.ObservedGeneration = anotherCRB.GetGeneration()
			anotherCRB.SetConditions(availableCondition)
			Expect(k8sClient.Status().Update(ctx, &anotherCRB)).Should(Succeed())
		})

		It("Create ClusterResourcePlacementDisruptionBudget", func() {
			crpdb := placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1alpha1.PlacementDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crpdb)).Should(Succeed())
			// ensure CRPDB exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crpdb.Name}, &crpdb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		It("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValid}, &isExecutedEviction{bool: true, msg: fmt.Sprintf(evictionAllowedPDBSpecified, 1, 2, 2, 2)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		It("Ensure eviction was successful for one ClusterResourceBinding", func() {
			var crb placementv1beta1.ClusterResourceBinding
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Ensure other ClusterResourceBinding was not delete", func() {
			var anotherCRB placementv1beta1.ClusterResourceBinding
			// Ensure another CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: anotherCRBName}, &anotherCRB))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		It("Clean up resources", func() {
			// Clean up resources.
			ensureEvictionRemoved(evictionName)
			ensureCRPDBRemoved(crpName)
			ensureCRBRemoved(anotherCRBName)
			ensureCRPRemoved(crpName)
		})
	})
})

func evictionStatusUpdatedActual(isValid *isValidEviction, isExecuted *isExecutedEviction) func() error {
	evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
	return func() error {
		var eviction placementv1alpha1.ClusterResourcePlacementEviction
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
			return err
		}
		var conditions []metav1.Condition
		if isValid != nil {
			if isValid.bool {
				validCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             reasonClusterResourcePlacementEvictionValid,
					Message:            isValid.msg,
				}
				conditions = append(conditions, validCondition)
			} else {
				invalidCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             reasonClusterResourcePlacementEvictionInvalid,
					Message:            isValid.msg,
				}
				conditions = append(conditions, invalidCondition)
			}
		}
		if isExecuted != nil {
			if isExecuted.bool {
				executedCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             reasonClusterResourcePlacementEvictionExecuted,
					Message:            isExecuted.msg,
				}
				conditions = append(conditions, executedCondition)
			} else {
				notExecutedCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             reasonClusterResourcePlacementEvictionNotExecuted,
					Message:            isExecuted.msg,
				}
				conditions = append(conditions, notExecutedCondition)
			}
		}
		wantStatus := placementv1alpha1.PlacementEvictionStatus{
			Conditions: conditions,
		}
		if diff := cmp.Diff(eviction.Status, wantStatus, evictionStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

func buildTestCRP(crpName string) placementv1beta1.ClusterResourcePlacement {
	return placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
				{
					Group:   "",
					Kind:    "Namespace",
					Version: "v1",
					Name:    "test-ns",
				},
			},
		},
	}
}

func buildTestEviction(evictionName, placementName, clusterName string) placementv1alpha1.ClusterResourcePlacementEviction {
	return placementv1alpha1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: evictionName,
		},
		Spec: placementv1alpha1.PlacementEvictionSpec{
			PlacementName: placementName,
			ClusterName:   clusterName,
		},
	}
}

func ensureEvictionRemoved(name string) {
	// Delete eviction.
	eviction := placementv1alpha1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, &eviction)).Should(Succeed())
	// Ensure eviction doesn't exist.
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &eviction); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("eviction still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval)
}

func ensureCRPRemoved(name string) {
	// Delete CRP.
	crp := placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, &crp)).Should(Succeed())
	// Ensure CRP doesn't exist.
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &crp); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("CRP still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval)
}

func ensureCRPDBRemoved(name string) {
	// Delete CRPDB.
	crpdb := placementv1alpha1.ClusterResourcePlacementDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, &crpdb)).Should(Succeed())
	// Ensure CRPDB doesn't exist.
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &crpdb); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("CRPDB still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval)
}

func ensureCRBRemoved(name string) {
	// Delete CRB.
	crb := placementv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(k8sClient.Delete(ctx, &crb)).Should(Succeed())
	// Ensure CRB doesn't exist.
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &crb); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("CRB still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval)
}

type isValidEviction struct {
	bool
	msg string
}

type isExecutedEviction struct {
	bool
	msg string
}
