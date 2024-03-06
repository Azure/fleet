/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	eventuallyDuration   = time.Second * 5
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Second
	consistentlyInterval = time.Millisecond * 200
)

const (
	dummyReason     = "dummyReason"
	dummyLabel      = "dummy-label"
	dummyLabelValue = "dummy-label-value"

	dummyNonResourcePropertyName  = "dummy-non-resource-property"
	dummyNonResourcePropertyValue = "0"
)

var (
	qualifiedKeysEnqueuedActual = func() error {
		errorFormat := "CRP keys %v have not been enqueued"
		requiredKeys := []string{crpName1, crpName2, crpName3, crpName6}
		if isAllPresent, absentKeys := keyCollector.IsPresent(requiredKeys...); !isAllPresent {
			return fmt.Errorf(errorFormat, absentKeys)
		}

		if queueLen := keyCollector.Len(); queueLen != len(requiredKeys) {
			return fmt.Errorf("work queue is not of the required length: got %d, want %d", queueLen, len(requiredKeys))
		}

		return nil
	}

	allKeysEnqueuedActual = func() error {
		errorFormat := "CRP keys %v have not been enqueued"
		requiredKeys := []string{crpName1, crpName2, crpName3, crpName4, crpName5, crpName6}
		if isAllPresent, absentKeys := keyCollector.IsPresent(requiredKeys...); !isAllPresent {
			return fmt.Errorf(errorFormat, absentKeys)
		}

		if queueLen := keyCollector.Len(); queueLen != len(requiredKeys) {
			return fmt.Errorf("work queue is not of the required length: got %d, want %d", queueLen, len(requiredKeys))
		}

		return nil
	}

	noKeyEnqueuedActual = func() error {
		if queueLen := keyCollector.Len(); queueLen != 0 {
			return fmt.Errorf("work queue is not empty: current len %d, want 0", queueLen)
		}
		return nil
	}
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// TODO (ryanzhang): Fix the tests so that they don't rely on the order of execution.
var _ = Describe("scheduler member cluster source controller", Serial, Ordered, func() {
	BeforeAll(func() {
		Eventually(noKeyEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Workqueue is not empty")
		Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")
	})

	Context("member cluster gets ready", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as ready.
			memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
				{
					Type: clusterv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             dummyReason,
						},
						{
							Type:               string(clusterv1beta1.AgentHealthy),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             dummyReason,
						},
					},
					LastReceivedHeartbeat: metav1.NewTime(time.Now()),
				},
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")
		})

		It("should enqueue CRPs (case 1b)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has a label change", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the labels and finalizers
			memberCluster.Finalizers = append(memberCluster.Finalizers, placementv1beta1.MemberClusterFinalizer)
			memberCluster.Labels = map[string]string{
				dummyLabel: dummyLabelValue,
			}
			Expect(hubClient.Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster labels")
		})

		It("should enqueue CRPs (case 1a)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has a non-resource property change", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the list of non-resource properties.
			memberCluster.Status.Properties = map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				dummyNonResourcePropertyName: {
					Value:           dummyNonResourcePropertyValue,
					ObservationTime: metav1.NewTime(time.Now()),
				},
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster non-resource properties")
		})

		It("should enqueue CRPs (case 1a)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has a total capacity change", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the list of non-resource properties.
			memberCluster.Status.ResourceUsage.Capacity = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster non-resource properties")
		})

		It("should enqueue CRPs (case 1a)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has an allocatable capacity change", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the list of non-resource properties.
			memberCluster.Status.ResourceUsage.Allocatable = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster non-resource properties")
		})

		It("should enqueue CRPs (case 1a)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has an available capacity change", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the list of non-resource properties.
			memberCluster.Status.ResourceUsage.Available = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster non-resource properties")
		})

		It("should enqueue CRPs (case 1a)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster is out of sync", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as out of sync.
			memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
				{
					Type: clusterv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
							Reason:             dummyReason,
						},
						{
							Type:               string(clusterv1beta1.AgentHealthy),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
							Reason:             dummyReason,
						},
					},
					LastReceivedHeartbeat: metav1.NewTime(time.Now().Add(-time.Hour)),
				},
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster status")
		})

		It("should not enqueue CRPs (case 2b)", func() {
			Eventually(noKeyEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Workqueue is not empty")
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("out of sync cluster becomes in sync", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as ready.
			memberCluster.Status.AgentStatus = []clusterv1beta1.AgentStatus{
				{
					Type: clusterv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             dummyReason,
						},
						{
							Type:               string(clusterv1beta1.AgentHealthy),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             dummyReason,
						},
					},
					LastReceivedHeartbeat: metav1.NewTime(time.Now()),
				},
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")
		})

		It("should enqueue CRPs (case 1b)", func() {
			Eventually(qualifiedKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(qualifiedKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has left", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &clusterv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the spec as leave.
			Expect(hubClient.Delete(ctx, memberCluster)).To(Succeed(), "Failed to delete member cluster")
		})

		It("should enqueue all CRPs for cluster left (case 1b)", func() {
			Eventually(allKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(allKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})
})
