/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacementeviction

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller/metrics"
	testutilseviction "go.goms.io/fleet/test/utils/eviction"
)

const (
	crbNameTemplate        = "crb-%d"
	anotherCRBNameTemplate = "another-crb-%d"
	crpNameTemplate        = "crp-%d"
	evictionNameTemplate   = "eviction-%d"
)

const (
	eventuallyDuration   = time.Minute * 2
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Second * 10
	consistentlyInterval = time.Millisecond * 500
)

var _ = Describe("Test ClusterResourcePlacementEviction complete metric", Ordered, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())
	var customRegistry *prometheus.Registry

	BeforeEach(func() {
		// Create a test registry
		customRegistry = prometheus.NewRegistry()
		Expect(customRegistry.Register(metrics.FleetEvictionStatus)).Should(Succeed())
		// Reset metrics before each test
		metrics.FleetEvictionStatus.Reset()
		// emit incomplete eviction metric to simulate eviction failed once.
		metrics.FleetEvictionStatus.WithLabelValues(evictionName, "false", "unknown").SetToCurrentTime()
	})

	AfterEach(func() {
		ensureAllBindingsAreRemoved(crpName)
		ensureEvictionRemoved(evictionName)
		ensureCRPRemoved(crpName)
	})

	It("Invalid Eviction - emit complete binding with isValid=false, isComplete=true", func() {
		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, "random-crp", "test-cluster")
			Expect(k8sClient.Create(ctx, eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, k8sClient, evictionName,
				&testutilseviction.IsValidEviction{IsValid: false, Msg: condition.EvictionInvalidMissingCRPMessage},
				nil)
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction complete metric was emitted", func() {
			checkEvictionCompleteMetric(customRegistry, "false", "true")
		})
	})

	It("Valid Eviction - emit complete binding with isValid=true, isComplete=true", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourcePlacement", func() {
			// Create ClusterResourcePlacement.
			crp := buildTestPickNCRP(crpName, 1)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Create ClusterResourceBinding", func() {
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
		})

		By("Create ClusterResourcePlacementEviction", func() {
			eviction := buildTestEviction(evictionName, crpName, "test-cluster")
			Expect(k8sClient.Create(ctx, eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, k8sClient, evictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: condition.EvictionAllowedNoPDBMessage})
			Eventually(evictionStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Ensure eviction complete metric was emitted", func() {
			checkEvictionCompleteMetric(customRegistry, "true", "true")
		})
	})
})

var _ = Describe("Test ClusterResourcePlacementEviction Controller", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	evictionName := fmt.Sprintf(evictionNameTemplate, GinkgoParallelProcess())

	AfterEach(func() {
		ensureCRPDBRemoved(crpName)
		ensureAllBindingsAreRemoved(crpName)
		ensureEvictionRemoved(evictionName)
		ensureCRPRemoved(crpName)
	})

	It("Eviction Blocked - ClusterResourcePlacementDisruptionBudget's maxUnavailable blocks eviction", func() {
		crbName := fmt.Sprintf(crbNameTemplate, GinkgoParallelProcess())

		By("Create ClusterResourcePlacement", func() {
			// Create ClusterResourcePlacement.
			crp := buildTestPickNCRP(crpName, 1)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Create ClusterResourceBinding", func() {
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
		})

		By("Create ClusterResourcePlacementDisruptionBudget", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
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
			Expect(k8sClient.Create(ctx, eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, k8sClient, evictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 0, 1)})
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

		By("Create ClusterResourcePlacement", func() {
			// Create ClusterResourcePlacement.
			crp := buildTestPickNCRP(crpName, 1)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

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
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
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
			Expect(k8sClient.Create(ctx, eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, k8sClient, evictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 1, 1)})
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

		By("Create ClusterResourcePlacement", func() {
			// Create ClusterResourcePlacement.
			crp := buildTestPickNCRP(crpName, 1)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

		By("Create ClusterResourceBinding", func() {
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
		})

		By("Create ClusterResourcePlacementDisruptionBudget", func() {
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
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
			Expect(k8sClient.Create(ctx, eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, k8sClient, evictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: false, Msg: fmt.Sprintf(condition.EvictionBlockedPDBSpecifiedMessageFmt, 0, 1)})
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

		By("Create ClusterResourcePlacement", func() {
			// Create ClusterResourcePlacement.
			crp := buildTestPickNCRP(crpName, 2)
			Expect(k8sClient.Create(ctx, &crp)).Should(Succeed())
			// ensure CRP exists.
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: crp.Name}, &crp)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed())
		})

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
					State:                        placementv1beta1.BindingStateBound,
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
			crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementDisruptionBudgetSpec{
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
			Expect(k8sClient.Create(ctx, eviction)).Should(Succeed())
		})

		By("Check eviction status", func() {
			evictionStatusUpdatedActual := testutilseviction.StatusUpdatedActual(
				ctx, k8sClient, evictionName,
				&testutilseviction.IsValidEviction{IsValid: true, Msg: condition.EvictionValidMessage},
				&testutilseviction.IsExecutedEviction{IsExecuted: true, Msg: fmt.Sprintf(condition.EvictionAllowedPDBSpecifiedMessageFmt, 2, 2)})
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

func buildTestPickNCRP(crpName string, clusterCount int32) placementv1beta1.ClusterResourcePlacement {
	return placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
		Spec: placementv1beta1.ClusterResourcePlacementSpec{
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(clusterCount),
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

func buildTestEviction(evictionName, placementName, clusterName string) *placementv1beta1.ClusterResourcePlacementEviction {
	return &placementv1beta1.ClusterResourcePlacementEviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:       evictionName,
			Generation: 1,
		},
		Spec: placementv1beta1.PlacementEvictionSpec{
			PlacementName: placementName,
			ClusterName:   clusterName,
		},
	}
}

func ensureEvictionRemoved(name string) {
	// Delete eviction.
	eviction := placementv1beta1.ClusterResourcePlacementEviction{
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
	crpdb := placementv1beta1.ClusterResourcePlacementDisruptionBudget{
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

func checkEvictionCompleteMetric(registry *prometheus.Registry, isValid, isComplete string) {
	metricFamilies, err := registry.Gather()
	Expect(err).Should(Succeed())
	var evictionCompleteMetrics []*prometheusclientmodel.Metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "fleet_workload_eviction_complete" {
			evictionCompleteMetrics = mf.GetMetric()
		}
	}
	// we only expect one metric, incomplete eviction metric should be removed.
	Expect(len(evictionCompleteMetrics)).Should(Equal(1))
	metricLabels := evictionCompleteMetrics[0].GetLabel()
	Expect(len(metricLabels)).Should(Equal(3))
	for _, label := range metricLabels {
		if label.GetName() == "isValid" {
			Expect(label.GetValue()).Should(Equal(isValid))
		}
		if label.GetName() == "isComplete" {
			Expect(label.GetValue()).Should(Equal(isComplete))
		}
	}
}
