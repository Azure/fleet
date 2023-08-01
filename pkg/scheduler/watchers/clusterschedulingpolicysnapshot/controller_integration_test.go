/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterschedulingpolicysnapshot

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	eventuallyDuration   = time.Second * 5
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Second
	consistentlyInterval = time.Millisecond * 200
)

const (
	crpName = "crp"

	policySnapshotName1 = "policy-snapshot-1"
	policySnapshotName2 = "policy-snapshot-2"

	policyHash1 = "policy-hash-1"
	policyHash2 = "policy-hash-2"
)

var (
	numOfClusters = int32(10)
)

var (
	// expectedKeySetEnqueuedActual is a function that checks if the expected key set has been enqueued.
	expectedKeySetEnqueuedActual = func() error {
		if isAllPresent, absentKeys := keyCollector.IsPresent(crpName); !isAllPresent {
			return fmt.Errorf("expected key(s) %v is not found", absentKeys)
		}

		if queueLen := keyCollector.Len(); queueLen != 1 {
			return fmt.Errorf("more than one key is enqueued: current len %d, want 1", queueLen)
		}

		return nil
	}

	// noKeyEnqueuedActual is a function that checks if the work queue is empty.
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

var _ = Describe("cluster scheduling policy snapshot scheduler source controller", Serial, Ordered, func() {
	BeforeAll(func() {
		policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name: policySnapshotName1,
				Labels: map[string]string{
					fleetv1beta1.IsLatestSnapshotLabel: "true",
					fleetv1beta1.CRPTrackingLabel:      crpName,
				},
				Annotations: map[string]string{
					fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(int(numOfClusters)),
				},
			},
			Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
				Policy: &fleetv1beta1.PlacementPolicy{
					PlacementType:    fleetv1beta1.PickNPlacementType,
					NumberOfClusters: &numOfClusters,
				},
				PolicyHash: []byte(policyHash1),
			},
		}
		Expect(hubClient.Create(ctx, &policySnapshot)).Should(Succeed(), "Failed to create cluster scheduling policy snapshot")
	})

	Context("first policy snapshot created", func() {
		It("should enqueue the CRP when first policy snapshot created", func() {
			Eventually(expectedKeySetEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to enqueue expected key set")
			Consistently(expectedKeySetEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to enqueue expected key set")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("number of clusters annotation updated", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Name: policySnapshotName1}, &policySnapshot)).Should(Succeed(), "Failed to get cluster scheduling policy snapshot")

			policySnapshot.Annotations[fleetv1beta1.NumberOfClustersAnnotation] = strconv.Itoa(int(numOfClusters) + 1)
			Expect(hubClient.Update(ctx, &policySnapshot)).Should(Succeed(), "Failed to update cluster scheduling policy snapshot")
		})

		It("should enqueue the CRP when number of clusters annotation updated", func() {
			Eventually(expectedKeySetEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to enqueue expected key set")
			Consistently(expectedKeySetEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to enqueue expected key set")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("next policy snapshot created", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Name: policySnapshotName1}, &policySnapshot)).Should(Succeed(), "Failed to get cluster scheduling policy snapshot")

			policySnapshot.Labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
			Expect(hubClient.Update(ctx, &policySnapshot)).Should(Succeed(), "Failed to update cluster scheduling policy snapshot")

			nextPolicySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName2,
					Labels: map[string]string{
						fleetv1beta1.IsLatestSnapshotLabel: "true",
						fleetv1beta1.CRPTrackingLabel:      crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.NumberOfClustersAnnotation: strconv.Itoa(int(numOfClusters)),
					},
				},
				Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType:    fleetv1beta1.PickNPlacementType,
						NumberOfClusters: &numOfClusters,
					},
					PolicyHash: []byte(policyHash2),
				},
			}
			Expect(hubClient.Create(ctx, &nextPolicySnapshot)).Should(Succeed(), "Failed to create cluster scheduling policy snapshot")
		})

		It("should enqueue the CRP when next policy snapshot created", func() {
			Eventually(expectedKeySetEnqueuedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to enqueue expected key set")
			Consistently(expectedKeySetEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Failed to enqueue expected key set")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("policy snapshot becomes inactive", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{}
			Expect(hubClient.Get(ctx, client.ObjectKey{Name: policySnapshotName2}, &policySnapshot)).Should(Succeed(), "Failed to get cluster scheduling policy snapshot")

			policySnapshot.Labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
			Expect(hubClient.Update(ctx, &policySnapshot)).Should(Succeed(), "Failed to update cluster scheduling policy snapshot")
		})

		It("should not enqueue the CRP when poliy snapshot becomes inactive", func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})

	Context("old policy snapshot is deleted", func() {
		BeforeAll(func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")

			policySnapshot := fleetv1beta1.ClusterSchedulingPolicySnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: policySnapshotName1,
				},
			}
			Expect(hubClient.Delete(ctx, &policySnapshot)).Should(Succeed(), "Failed to delete cluster scheduling policy snapshot")
		})

		It("should not enqueue the CRP when poliy snapshot is deleted", func() {
			Consistently(noKeyEnqueuedActual, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Workqueue is not empty")
		})

		AfterAll(func() {
			keyCollector.Reset()
		})
	})
})
