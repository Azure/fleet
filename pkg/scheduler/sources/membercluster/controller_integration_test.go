/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
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
)

var (
	someKeysEnqueuedActual = func() error {
		errorFormat := "CRP keys %v have not been enqueued"
		requiredKeys := []string{crpName1, crpName2, crpName3, crpName6}
		if allPresent, absentKeys := keyCollector.IsPresent(requiredKeys...); !allPresent {
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
		if allPresent, absentKeys := keyCollector.IsPresent(requiredKeys...); !allPresent {
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
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

var _ = Describe("scheduler member cluster source controller", Serial, Ordered, func() {
	BeforeAll(func() {
		Eventually(noKeyEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Workqueue is not empty")
		Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

		keyCollector.Reset()
	})

	Context("updated a cluster that has left", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &fleetv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName2}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as ready.
			memberCluster.Labels = map[string]string{
				dummyLabel: dummyLabelValue,
			}
			Expect(hubClient.Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")
		})

		It("should enqueue CRPs (case 1b)", func() {
			Eventually(noKeyEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Workqueue is not empty")
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("member cluster gets ready", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &fleetv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as ready.
			memberCluster.Status.AgentStatus = []fleetv1beta1.AgentStatus{
				{
					Type: fleetv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             dummyReason,
						},
						{
							Type:               string(fleetv1beta1.AgentHealthy),
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
			Eventually(someKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(someKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has a label change", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &fleetv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the labels.
			memberCluster.Labels = map[string]string{
				dummyLabel: dummyLabelValue,
			}
			Expect(hubClient.Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster")
		})

		It("should enqueue CRPs (case 1a)", func() {
			Eventually(someKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(someKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster is out of sync", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &fleetv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as out of sync.
			memberCluster.Status.AgentStatus = []fleetv1beta1.AgentStatus{
				{
					Type: fleetv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
							Reason:             dummyReason,
						},
						{
							Type:               string(fleetv1beta1.AgentHealthy),
							Status:             metav1.ConditionFalse,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
							Reason:             dummyReason,
						},
					},
					LastReceivedHeartbeat: metav1.NewTime(time.Now().Add(-time.Hour)),
				},
			}
			Expect(hubClient.Status().Update(ctx, memberCluster)).Should(Succeed(), "Failed to update member cluster")
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
			memberCluster := &fleetv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as ready.
			memberCluster.Status.AgentStatus = []fleetv1beta1.AgentStatus{
				{
					Type: fleetv1beta1.MemberAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now()),
							Reason:             dummyReason,
						},
						{
							Type:               string(fleetv1beta1.AgentHealthy),
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
			Eventually(someKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(someKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("ready cluster has left", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			// Retrieve the cluster.
			memberCluster := &fleetv1beta1.MemberCluster{}
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: clusterName1}, memberCluster)).To(Succeed(), "Failed to get member cluster")

			// Update the status; mark the cluster as ready.
			memberCluster.Spec.State = fleetv1beta1.ClusterStateLeave
			Expect(hubClient.Update(ctx, memberCluster)).To(Succeed(), "Failed to update member cluster status")
		})

		It("should enqueue CRPs (case 1b)", func() {
			Eventually(allKeysEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Keys are not enqueued as expected")
			Consistently(allKeysEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Keys are not enqueued as expected")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})
})
