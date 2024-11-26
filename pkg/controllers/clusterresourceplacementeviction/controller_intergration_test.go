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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	crbNameTemplate        = "crb-%d"
	anotherCRBNameTemplate = "another-crb-%d"
	crpNameTemplate        = "crp-%d"
	evictionNameTemplate   = "eviction-%d"
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

var _ = It("Invalid Eviction - ClusterResourcePlacement not found", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
	By("Create ClusterResourcePlacementEviction", func() {
		eviction := buildTestEviction(evictionName, crpName, "test-cluster")
		Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
	})

	By("Check eviction status", func() {
		evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: false, msg: evictionInvalidMissingCRPMessage}, nil)
		Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	By("Clean up resources", func() {
		ensureEvictionRemoved(evictionName)
	})
})

var _ = Describe("Test ClusterResourcePlacementEviction Controller", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())

	BeforeEach(func() {
		// Create ClusterResourcePlacement.
		crp := buildTestCRP(crpName)
		Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
		// ensure CRP exists.
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
		}, eventuallyDuration, eventuallyInterval).Should(Succeed())
	})

	AfterEach(func() {
		ensureCRPDBRemoved(crpName)
		ensureAllBindingsAreRemoved(crpName)
		ensureEvictionRemoved(evictionName)
		ensureCRPRemoved(crpName)
	})

	It("Invalid Eviction - ClusterResourceBinding not found", func() {
		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: false, msg: evictionInvalidMissingCRBMessage}, nil)
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	It("Invalid Eviction - Multiple ClusterResourceBindings for one cluster", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())
		anotherCRBName := fmt.Sprintf(anotherCRBNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding", func() {
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

		By("Create another ClusterResourceBinding", func() {
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

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: false, msg: evictionInvalidMultipleCRBMessage}, nil)
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was not successful", func() {
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
	})

	It("Eviction Allowed - Deleting ClusterResourceBinding", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding with custom deletion finalizer", func() {
			// Create CRB.
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       crbName,
					Labels:     map[string]string{placementv1beta1.CRPTrackingLabel: crpName},
					Finalizers: []string{"custom-deletion-finalizer"},
				},
				Spec: placementv1beta1.ResourceBindingSpec{
					State:                        placementv1beta1.BindingStateScheduled,
					ResourceSnapshotName:         "test-resource-snapshot",
					SchedulingPolicySnapshotName: "test-scheduling-policy-snapshot",
					TargetCluster:                "test-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, &crb)).Should(Succeed())
		})

		By("Delete ClusterResourceBinding", func() {
			crb := placementv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: crbName,
				},
			}
			Expect(k8sClient.Delete(ctx, &crb)).Should(Succeed())
		})

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: true, msg: evictionAllowedPlacementRemovedMessage})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Remove custom deleting finalizer from binding", func() {
			Eventually(func() error {
				var crb placementv1beta1.ClusterResourceBinding
				err := k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb)
				if err != nil {
					return err
				}
				crb.SetFinalizers(nil)
				return k8sClient.Update(ctx, &crb)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})
	})

	It("Eviction Allowed - Failed to apply ClusterResourceBinding", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding with applied condition set to false", func() {
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

			// Update CRB status to have applied condition set to false.
			// Ideally binding would contain more condition before applied, but for the sake testing we only specify applied condition.
			appliedCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionFalse,
				Reason:             "applied",
				ObservedGeneration: crb.GetGeneration(),
			}
			crb.SetConditions(appliedCondition)
			Expect(k8sClient.Status().Update(ctx, &crb)).Should(Succeed())
		})

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: true, msg: evictionAllowedPlacementFailedMessage})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// Ensure CRB was deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	It("Eviction Allowed - Failed to be available ClusterResourceBinding", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding with applied condition set to false", func() {
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

			// Update CRB status to have applied condition set to false.
			// Ideally binding would contain more condition before applied, available.
			// But for the sake testing we only specify applied, available condition.
			appliedCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "applied",
				ObservedGeneration: crb.GetGeneration(),
			}
			availableCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionFalse,
				Reason:             "available",
				ObservedGeneration: crb.GetGeneration(),
			}
			crb.SetConditions(appliedCondition, availableCondition)
			Expect(k8sClient.Status().Update(ctx, &crb)).Should(Succeed())
		})

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: true, msg: evictionAllowedPlacementFailedMessage})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// Ensure CRB was deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	It("Eviction Allowed - ClusterResourcePlacementDisruptionBudget is not present", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding", func() {
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

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: true, msg: evictionAllowedNoPDBMessage})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// Ensure CRB was deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	It("Eviction Blocked - ClusterResourcePlacementDisruptionBudget's maxUnavailable blocks eviction", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding", func() {
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

		By("Create ClusterResourcePlacementDisruptionBudget", func() {
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

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: false, msg: fmt.Sprintf(evictionBlockedPDBSpecifiedFmt, 0, 1, 1)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was not successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// check to see CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	It("Eviction Allowed - ClusterResourcePlacementDisruptionBudget's maxUnavailable allows eviction", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding and update status with available condition", func() {
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

			// Update CRB status to have applied, available condition.
			// Ideally binding would contain more condition before applied, available.
			// But for the sake testing we only specify applied, available condition.
			appliedCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "applied",
				ObservedGeneration: crb.GetGeneration(),
			}
			availableCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "available",
				ObservedGeneration: crb.GetGeneration(),
			}
			crb.SetConditions(appliedCondition, availableCondition)
			Expect(k8sClient.Status().Update(ctx, &crb)).Should(Succeed())
		})

		By("Create ClusterResourcePlacementDisruptionBudget", func() {
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

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: true, msg: fmt.Sprintf(evictionAllowedPDBSpecifiedFmt, 1, 1, 1)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// Ensure CRB was deleted.
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	It("Eviction Blocked - ClusterResourcePlacementDisruptionBudget's minAvailable blocks eviction", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourceBinding", func() {
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

		By("Create ClusterResourcePlacementDisruptionBudget", func() {
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

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: false, msg: fmt.Sprintf(evictionBlockedPDBSpecifiedFmt, 0, 1, 1)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was not successful", func() {
			var crb placementv1beta1.ClusterResourceBinding
			// check to see CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	It("Eviction Allowed - ClusterResourcePlacementDisruptionBudget's minUnavailable allows eviction", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())
		anotherCRBName := fmt.Sprintf(anotherCRBNameTemplate, GinkgoParallelProcess())

		By("Create two ClusterResourceBindings with available condition specified", func() {
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

			// Update CRB status to have applied, available condition.
			// Ideally binding would contain more condition before applied, available.
			// But for the sake testing we only specify applied, available condition.
			appliedCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingApplied),
				Status:             metav1.ConditionTrue,
				Reason:             "applied",
				ObservedGeneration: crb.GetGeneration(),
			}
			availableCondition := metav1.Condition{
				Type:               string(placementv1beta1.ResourceBindingAvailable),
				Status:             metav1.ConditionTrue,
				Reason:             "available",
				ObservedGeneration: crb.GetGeneration(),
			}
			crb.SetConditions(appliedCondition, availableCondition)
			Expect(k8sClient.Status().Update(ctx, &crb)).Should(Succeed())
			availableCondition.ObservedGeneration = anotherCRB.GetGeneration()
			anotherCRB.SetConditions(availableCondition)
			Expect(k8sClient.Status().Update(ctx, &anotherCRB)).Should(Succeed())
		})

		By("Create ClusterResourcePlacementDisruptionBudget", func() {
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

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, &eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := evictionStatusUpdatedActual(&isValidEviction{bool: true, msg: evictionValidMessage}, &isExecutedEviction{bool: true, msg: fmt.Sprintf(evictionAllowedPDBSpecifiedFmt, 2, 2, 2)})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction was successful for one ClusterResourceBinding", func() {
			var crb placementv1beta1.ClusterResourceBinding
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, eventuallyDuration, eventuallyInterval).Should(BeTrue())
			Consistently(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, &crb))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})

		By("Ensure other ClusterResourceBinding was not delete", func() {
			var anotherCRB placementv1beta1.ClusterResourceBinding
			// Ensure another CRB was not deleted.
			Consistently(func() bool {
				return !k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: anotherCRBName}, &anotherCRB))
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
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
					Reason:             clusterResourcePlacementEvictionValidReason,
					Message:            isValid.msg,
				}
				conditions = append(conditions, validCondition)
			} else {
				invalidCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             clusterResourcePlacementEvictionInvalidReason,
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
					Reason:             clusterResourcePlacementEvictionExecutedReason,
					Message:            isExecuted.msg,
				}
				conditions = append(conditions, executedCondition)
			} else {
				notExecutedCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             clusterResourcePlacementEvictionNotExecutedReason,
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
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &crp))).Should(Succeed())
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
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &crpdb))).Should(Succeed())
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
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &crb))).Should(Succeed())
	// Ensure CRB doesn't exist.
	Eventually(func() error {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &crb); !k8serrors.IsNotFound(err) {
			return fmt.Errorf("CRB still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval)
}

func ensureAllBindingsAreRemoved(crpName string) {
	// List all bindings associated with the given CRP.
	bindingList := &placementv1beta1.ClusterResourceBindingList{}
	labelSelector := labels.SelectorFromSet(labels.Set{placementv1beta1.CRPTrackingLabel: crpName})
	listOptions := &client.ListOptions{LabelSelector: labelSelector}
	Expect(k8sClient.List(ctx, bindingList, listOptions)).Should(Succeed())

	for i := range bindingList.Items {
		ensureCRBRemoved(bindingList.Items[i].Name)
	}
}

type isValidEviction struct {
	bool
	msg string
}

type isExecutedEviction struct {
	bool
	msg string
}
