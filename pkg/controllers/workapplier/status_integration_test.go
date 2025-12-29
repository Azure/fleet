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

package workapplier

import (
	"context"
	"fmt"
	"sync"
	"time"

	crossplanetest "github.com/crossplane/crossplane-runtime/v2/pkg/test"
	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	testutilsactuals "github.com/kubefleet-dev/kubefleet/test/utils/actuals"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Note (chenyu1): all test cases in this file use a separate test environment
// (same hub cluster, different fleet member reserved namespace, different
// work applier instance) from the other integration tests. This is needed
// as a client wrapper is used to verify the work applier behavior.

type clientWrapperWithStatusUpdateCounter struct {
	*crossplanetest.MockClient

	mu                sync.Mutex
	statusUpdateCount map[string]int
}

func (c *clientWrapperWithStatusUpdateCounter) GetStatusUpdateCount(workNS, workName string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.statusUpdateCount[fmt.Sprintf("%s/%s", workNS, workName)]
}

func NewClientWrapperWithStatusUpdateCounter(realClient client.Client) client.Client {
	wrapper := &clientWrapperWithStatusUpdateCounter{
		statusUpdateCount: make(map[string]int),
	}

	wrapper.MockClient = &crossplanetest.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			return realClient.Get(ctx, key, obj)
		},
		MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			return realClient.List(ctx, list, opts...)
		},
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			return realClient.Create(ctx, obj, opts...)
		},
		MockDelete: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
			return realClient.Delete(ctx, obj, opts...)
		},
		MockDeleteAllOf: func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
			return realClient.DeleteAllOf(ctx, obj, opts...)
		},
		MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
			return realClient.Update(ctx, obj, opts...)
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return realClient.Patch(ctx, obj, patch, opts...)
		},
		MockApply: func(ctx context.Context, config runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			return realClient.Apply(ctx, config, opts...)
		},
		MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			wrapper.mu.Lock()
			defer wrapper.mu.Unlock()

			objNS := obj.GetNamespace()
			objName := obj.GetName()
			key := fmt.Sprintf("%s/%s", objNS, objName)
			wrapper.statusUpdateCount[key]++
			return realClient.Status().Update(ctx, obj, opts...)
		},
		MockStatusPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			return realClient.Status().Patch(ctx, obj, patch, opts...)
		},
	}

	return wrapper
}

var _ = Describe("skipping status update", func() {
	Context("apply new manifests", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularCM *corev1.ConfigMap

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a ConfigMap object.
			regularCM = configMap.DeepCopy()
			regularCM.Namespace = nsName
			regularCM.Name = configMapName
			regularCMJSON := marshalK8sObjJSON(regularCM)

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName4, nil, nil, regularNSJSON, regularCMJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(memberReservedNSName4, workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(memberClient4, memberReservedNSName4, workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(memberClient4, memberReservedNSName4, workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(memberClient4, nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient4.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the ConfigMap object has been applied as expected.
			regularConfigMapObjectAppliedActual := regularConfigMapObjectAppliedActual(memberClient4, nsName, configMapName, appliedWorkOwnerRef)
			Eventually(regularConfigMapObjectAppliedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to apply the ConfigMap object")

			Expect(memberClient4.Get(ctx, client.ObjectKey{Namespace: nsName, Name: configMapName}, regularCM)).To(Succeed(), "Failed to retrieve the ConfigMap object")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      configMapName,
						Namespace: nsName,
					},
					UID: regularCM.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(memberClient4, workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      configMapName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName4, workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update work status")
			// With the default backoff setup, this Consistently duration should allow 10-15 reconciliations.
			Consistently(workStatusUpdatedActual, time.Minute*2, time.Second*10).Should(Succeed(), "Work status was updated unexpectedly")
		})

		It("should have skipped most status updates", func() {
			memberClientStatusUpdateCount := memberClient4Wrapper.GetStatusUpdateCount("", workName)
			// There should be 1 status update on the member cluster side in total:
			// 1) one status update for populating the initial appliedWork status after all manifests have been applied.
			wantMemberClientStatusUpdateCount := 1
			Expect(memberClientStatusUpdateCount).To(Equal(wantMemberClientStatusUpdateCount), "Unexpected number of status updates")

			hubClientStatusUpdateCount := hubClientWrapperForWorkApplier4.GetStatusUpdateCount(memberReservedNSName4, workName)
			// There should be 2 status updates on the hub cluster side in total:
			// 1) one status update for writing ahead the manifests to be applied;
			// 2) one status update for populating the initial work status after all manifests have been applied.
			wantHubClientStatusUpdateCount := 2
			Expect(hubClientStatusUpdateCount).To(Equal(wantHubClientStatusUpdateCount), "Unexpected number of status updates")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName4)

			// Ensure applied manifest has been removed.
			regularCMRemovedActual := regularConfigMapRemovedActual(memberClient4, nsName, configMapName)
			Eventually(regularCMRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the ConfigMap object")

			// Kubebuilder suggests that in a testing environment like this, to check for the existence of the AppliedWork object
			// OwnerReference in the Namespace object (https://book.kubebuilder.io/reference/envtest.html#testing-considerations).
			checkNSOwnerReferences(memberClient4, workName, nsName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient4, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := testutilsactuals.WorkObjectRemovedActual(ctx, hubClient, workName, memberReservedNSName4)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})
})
