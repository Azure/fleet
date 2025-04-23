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
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
)

const (
	propertiesManuallyUpdatedConditionType    = "PropertiesManuallyUpdated"
	propertiesManuallyUpdatedConditionReason1 = "NewPropertiesPushed"
	propertiesManuallyUpdatedConditionMsg1    = "Properties have been manually updated"
	propertiesManuallyUpdatedConditionReason2 = "NewPropertiesPushedAgain"
	propertiesManuallyUpdatedConditionMsg2    = "Properties have been manually updated again"
)

const (
	eventuallyTimeout  = time.Second * 30
	eventuallyInterval = time.Millisecond * 500
)

var _ = Describe("Test InternalMemberCluster Controller", func() {
	// Note that specs in this context run in serial, however, they might run in parallel with
	// the other contexts if parallelization is enabled.
	//
	// This is safe as the controller managers have been configured to watch only their own
	// respective namespaces.
	Context("Test setup with property provider", Ordered, func() {
		var (
			// Add an offset of -1 second to avoid flakiness caused by approximation.
			timeStarted = metav1.Time{Time: time.Now().Add(-time.Second)}

			// The timestamps below are set in later steps.
			timeUpdated metav1.Time
			timeLeft    metav1.Time
		)

		BeforeAll(func() {
			// Create the InternalMemberCluster object.
			imc := &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      member1Name,
					Namespace: member1ReservedNSName,
				},
				Spec: clusterv1beta1.InternalMemberClusterSpec{
					State: clusterv1beta1.ClusterStateJoin,
					// Use a shorter heartbeat period to improve responsiveness.
					HeartbeatPeriodSeconds: 2,
				},
			}
			Expect(hubClient.Create(ctx, imc)).Should(Succeed())

			// Report properties via the property provider.
			observationTime := metav1.Now()
			propertyProvider1.Update(&propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value:           "1",
						ObservationTime: observationTime,
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10"),
						corev1.ResourceMemory: resource.MustParse("10Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					ObservationTime: observationTime,
				},
				Conditions: []metav1.Condition{
					{
						Type:    propertiesManuallyUpdatedConditionType,
						Status:  metav1.ConditionTrue,
						Reason:  propertiesManuallyUpdatedConditionReason1,
						Message: propertiesManuallyUpdatedConditionMsg1,
					},
				},
			})
		})

		It("should join the cluster", func() {
			// Verify that the agent status has been updated.
			Eventually(func() error {
				imc := &clusterv1beta1.InternalMemberCluster{}
				objKey := types.NamespacedName{
					Name:      member1Name,
					Namespace: member1ReservedNSName,
				}
				if err := hubClient.Get(ctx, objKey, imc); err != nil {
					return fmt.Errorf("failed to get InternalMemberCluster: %w", err)
				}

				wantIMCStatus := clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyCollectionSucceededReason,
							Message:            ClusterPropertyCollectionSucceededMessage,
							ObservedGeneration: imc.Generation,
						},
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyProviderStartedReason,
							Message:            ClusterPropertyProviderStartedMessage,
							ObservedGeneration: imc.Generation,
						},
						{
							Type:               propertiesManuallyUpdatedConditionType,
							Status:             metav1.ConditionTrue,
							Reason:             propertiesManuallyUpdatedConditionReason1,
							Message:            propertiesManuallyUpdatedConditionMsg1,
							ObservedGeneration: imc.Generation,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "1",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10"),
							corev1.ResourceMemory: resource.MustParse("10Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("8"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterJoined,
									ObservedGeneration: imc.Generation,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterHealthy,
									ObservedGeneration: imc.Generation,
								},
							},
						},
					},
				}

				if diff := cmp.Diff(
					imc.Status, wantIMCStatus,
					ignoreAllTimeFields,
					sortByConditionType,
				); diff != "" {
					return fmt.Errorf("InternalMemberCluster status diff (-got, +want):\n%s", diff)
				}

				// Verify the timestamps.

				// Verify the last transition timestamps in the conditions.
				//
				// Note that at this point the structure of the InternalMemberCluster status
				// object is already known.
				if cond := imc.Status.Conditions[0]; cond.LastTransitionTime.Before(&timeStarted) {
					return fmt.Errorf("InternalMemberCluster condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeStarted)
				}

				conds := imc.Status.AgentStatus[0].Conditions
				for idx := range conds {
					cond := conds[idx]
					if cond.LastTransitionTime.Before(&timeStarted) {
						return fmt.Errorf("InternalMemberCluster agent status condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeStarted)
					}
				}

				// Verify the observation timestamps in the properties.
				for pn, pv := range imc.Status.Properties {
					if pv.ObservationTime.Before(&timeStarted) {
						return fmt.Errorf("InternalMemberCluster property %s has observation time %v, want before %v", pn, pv.ObservationTime, timeStarted)
					}
				}
				if u := imc.Status.ResourceUsage; u.ObservationTime.Before(&timeStarted) {
					return fmt.Errorf("InternalMemberCluster resource usage has observation time %v, want before %v", u.ObservationTime, timeStarted)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to update the agent status")
		})

		It("can apply a new property update", func() {
			// Add an offset of -1 second to avoid flakiness caused by approximation.
			timeUpdated = metav1.Time{Time: time.Now().Add(-time.Second)}

			observationTime := metav1.Now()
			propertyProvider1.Update(&propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value:           "2",
						ObservationTime: observationTime,
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("15"),
						corev1.ResourceMemory: resource.MustParse("30Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("12"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("6Gi"),
					},
					ObservationTime: observationTime,
				},
				Conditions: []metav1.Condition{
					{
						Type:    propertiesManuallyUpdatedConditionType,
						Status:  metav1.ConditionFalse,
						Reason:  propertiesManuallyUpdatedConditionReason2,
						Message: propertiesManuallyUpdatedConditionMsg2,
					},
				},
			})
		})

		It("should update the properties", func() {
			// Verify that the agent status has been updated.
			Eventually(func() error {
				imc := &clusterv1beta1.InternalMemberCluster{}
				objKey := types.NamespacedName{
					Name:      member1Name,
					Namespace: member1ReservedNSName,
				}
				if err := hubClient.Get(ctx, objKey, imc); err != nil {
					return fmt.Errorf("failed to get InternalMemberCluster: %w", err)
				}

				wantIMCStatus := clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyCollectionSucceededReason,
							Message:            ClusterPropertyCollectionSucceededMessage,
							ObservedGeneration: imc.Generation,
						},
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyProviderStartedReason,
							Message:            ClusterPropertyProviderStartedMessage,
							ObservedGeneration: imc.Generation,
						},
						{
							Type:               propertiesManuallyUpdatedConditionType,
							Status:             metav1.ConditionFalse,
							Reason:             propertiesManuallyUpdatedConditionReason2,
							Message:            propertiesManuallyUpdatedConditionMsg2,
							ObservedGeneration: imc.Generation,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "2",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("15"),
							corev1.ResourceMemory: resource.MustParse("30Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("12"),
							corev1.ResourceMemory: resource.MustParse("24Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("6Gi"),
						},
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterJoined,
									ObservedGeneration: imc.Generation,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterHealthy,
									ObservedGeneration: imc.Generation,
								},
							},
						},
					},
				}

				if diff := cmp.Diff(
					imc.Status, wantIMCStatus,
					ignoreAllTimeFields,
					sortByConditionType,
				); diff != "" {
					return fmt.Errorf("InternalMemberCluster status diff (-got, +want):\n%s", diff)
				}

				// Verify the timestamps.

				// Verify the last transition timestamps in the conditions.
				//
				// Note that at this point the structure of the InternalMemberCluster status
				// object is already known.
				if cond := imc.Status.Conditions[0]; cond.LastTransitionTime.Before(&timeStarted) {
					return fmt.Errorf("InternalMemberCluster condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeStarted)
				}

				conds := imc.Status.AgentStatus[0].Conditions
				for idx := range conds {
					cond := conds[idx]
					if cond.LastTransitionTime.Before(&timeStarted) {
						return fmt.Errorf("InternalMemberCluster agent status condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeStarted)
					}
				}

				// Verify the observation timestamps in the properties.
				for pn, pv := range imc.Status.Properties {
					if pv.ObservationTime.Before(&timeUpdated) {
						return fmt.Errorf("InternalMemberCluster property %s has observation time %v, want before %v", pn, pv.ObservationTime, timeStarted)
					}
				}
				if u := imc.Status.ResourceUsage; u.ObservationTime.Before(&timeUpdated) {
					return fmt.Errorf("InternalMemberCluster resource usage has observation time %v, want before %v", u.ObservationTime, timeStarted)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to update the agent status")
		})

		It("can mark the cluster as left", func() {
			// Add an offset of -1 second to avoid flakiness caused by approximation.
			timeLeft = metav1.Time{Time: time.Now().Add(-time.Second)}

			imc := &clusterv1beta1.InternalMemberCluster{}
			objKey := types.NamespacedName{
				Name:      member1Name,
				Namespace: member1ReservedNSName,
			}
			Expect(hubClient.Get(ctx, objKey, imc)).Should(Succeed())

			imc.Spec.State = clusterv1beta1.ClusterStateLeave
			Expect(hubClient.Update(ctx, imc)).Should(Succeed())
		})

		It("should let the cluster go", func() {
			Eventually(func() error {
				imc := &clusterv1beta1.InternalMemberCluster{}
				objKey := types.NamespacedName{
					Name:      member1Name,
					Namespace: member1ReservedNSName,
				}
				if err := hubClient.Get(ctx, objKey, imc); err != nil {
					return fmt.Errorf("failed to get InternalMemberCluster: %w", err)
				}

				wantIMCStatus := clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyCollectionSucceededReason,
							Message:            ClusterPropertyCollectionSucceededMessage,
							ObservedGeneration: imc.Generation,
						},
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyProviderStartedReason,
							Message:            ClusterPropertyProviderStartedMessage,
							ObservedGeneration: imc.Generation,
						},
						{
							Type:               propertiesManuallyUpdatedConditionType,
							Status:             metav1.ConditionFalse,
							Reason:             propertiesManuallyUpdatedConditionReason2,
							Message:            propertiesManuallyUpdatedConditionMsg2,
							ObservedGeneration: imc.Generation,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "2",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("15"),
							corev1.ResourceMemory: resource.MustParse("30Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("12"),
							corev1.ResourceMemory: resource.MustParse("24Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("6Gi"),
						},
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionFalse,
									Reason:             EventReasonInternalMemberClusterLeft,
									ObservedGeneration: imc.Generation,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterHealthy,
									ObservedGeneration: imc.Generation,
								},
							},
						},
					},
				}

				if diff := cmp.Diff(
					imc.Status, wantIMCStatus,
					ignoreAllTimeFields,
					sortByConditionType,
				); diff != "" {
					return fmt.Errorf("InternalMemberCluster status diff (-got, +want):\n%s", diff)
				}

				// Verify the timestamp; for this spec only the last transition time of the
				// AgentJoined condition needs to be checked.

				// Note that at this point the structure of the InternalMemberCluster status object
				// is already known; this condition is guaranteed to be present.
				cond := imc.GetConditionWithType(clusterv1beta1.MemberAgent, string(clusterv1beta1.AgentJoined))
				if cond.LastTransitionTime.Before(&timeLeft) {
					return fmt.Errorf("InternalMemberCluster agent status condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeLeft)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(Succeed(), "Failed to let the cluster go")
		})
	})

	// Note that specs in this context run in serial, however, they might run in parallel with
	// the other contexts if parallelization is enabled.
	//
	// This is safe as the controller managers have been configured to watch only their own
	// respective namespaces.
	Context("Test setup with no property provider", Ordered, func() {
		var (
			// Add an offset of -1 second to avoid flakiness caused by approximation.
			timeStarted = metav1.Time{Time: time.Now().Add(-time.Second)}

			// The timestamps below are set in later steps.
			timeUpdated metav1.Time
			timeLeft    metav1.Time
		)

		BeforeAll(func() {
			// Create the InternalMemberCluster object.
			imc := &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      member2Name,
					Namespace: member2ReservedNSName,
				},
				Spec: clusterv1beta1.InternalMemberClusterSpec{
					State: clusterv1beta1.ClusterStateJoin,
					// Use a shorter heartbeat period to improve responsiveness.
					HeartbeatPeriodSeconds: 2,
				},
			}
			Expect(hubClient.Create(ctx, imc)).Should(Succeed())
		})

		It("should join the cluster", func() {
			Eventually(func() error {
				imc := &clusterv1beta1.InternalMemberCluster{}
				objKey := types.NamespacedName{
					Name:      member2Name,
					Namespace: member2ReservedNSName,
				}
				if err := hubClient.Get(ctx, objKey, imc); err != nil {
					return fmt.Errorf("failed to get InternalMemberCluster: %w", err)
				}

				wantIMCStatus := clusterv1beta1.InternalMemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "2",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("14"),
							corev1.ResourceMemory: resource.MustParse("48Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("12"),
							corev1.ResourceMemory: resource.MustParse("42Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10"),
							corev1.ResourceMemory: resource.MustParse("36Gi"),
						},
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterJoined,
									ObservedGeneration: imc.Generation,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterHealthy,
									ObservedGeneration: imc.Generation,
								},
							},
						},
					},
				}

				if diff := cmp.Diff(
					imc.Status, wantIMCStatus,
					ignoreAllTimeFields,
					sortByConditionType,
				); diff != "" {
					return fmt.Errorf("InternalMemberCluster status diff (-got, +want):\n%s", diff)
				}

				// Verify the timestamps.

				// Verify the last transition timestamps in the conditions.
				//
				// Note that at this point the structure of the InternalMemberCluster status
				// object is already known.
				conds := imc.Status.AgentStatus[0].Conditions
				for idx := range conds {
					cond := conds[idx]
					if cond.LastTransitionTime.Before(&timeStarted) {
						return fmt.Errorf("InternalMemberCluster agent status condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeStarted)
					}
				}

				// Verify the observation timestamps in the properties.
				for pn, pv := range imc.Status.Properties {
					if pv.ObservationTime.Before(&timeStarted) {
						return fmt.Errorf("InternalMemberCluster property %s has observation time %v, want before %v", pn, pv.ObservationTime, timeStarted)
					}
				}

				if u := imc.Status.ResourceUsage; u.ObservationTime.Before(&timeStarted) {
					return fmt.Errorf("InternalMemberCluster resource usage has observation time %v, want before %v", u.ObservationTime, timeStarted)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to update the agent status")
		})

		It("can add more nodes/pods", func() {
			// Add an offset of -1 second to avoid flakiness caused by approximation.
			timeUpdated = metav1.Time{Time: time.Now().Add(-time.Second)}

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName3,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			}

			Expect(member2Client.Create(ctx, node.DeepCopy())).Should(Succeed())
			Expect(member2Client.Status().Update(ctx, node.DeepCopy())).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName5,
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName1,
					Containers: []corev1.Container{
						{
							Name:  containerName1,
							Image: imageName,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(member2Client.Create(ctx, pod.DeepCopy())).Should(Succeed())
			Expect(member2Client.Status().Update(ctx, pod.DeepCopy())).Should(Succeed())
		})

		It("should update the properties", func() {
			Eventually(func() error {
				imc := &clusterv1beta1.InternalMemberCluster{}
				objKey := types.NamespacedName{
					Name:      member2Name,
					Namespace: member2ReservedNSName,
				}
				if err := hubClient.Get(ctx, objKey, imc); err != nil {
					return fmt.Errorf("failed to get InternalMemberCluster: %w", err)
				}

				wantIMCStatus := clusterv1beta1.InternalMemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "3",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("15"),
							corev1.ResourceMemory: resource.MustParse("49Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("13"),
							corev1.ResourceMemory: resource.MustParse("43Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10"),
							corev1.ResourceMemory: resource.MustParse("36Gi"),
						},
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterJoined,
									ObservedGeneration: imc.Generation,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterHealthy,
									ObservedGeneration: imc.Generation,
								},
							},
						},
					},
				}

				if diff := cmp.Diff(
					imc.Status, wantIMCStatus,
					ignoreAllTimeFields,
					sortByConditionType,
				); diff != "" {
					return fmt.Errorf("InternalMemberCluster status diff (-got, +want):\n%s", diff)
				}

				// Verify the last transition timestamps in the conditions.
				//
				// Note that at this point the structure of the InternalMemberCluster status
				// object is already known.
				conds := imc.Status.AgentStatus[0].Conditions
				for idx := range conds {
					cond := conds[idx]
					if cond.LastTransitionTime.Before(&timeStarted) {
						return fmt.Errorf("InternalMemberCluster agent status condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeStarted)
					}
				}

				// Verify the observation timestamps in the properties.
				for pn, pv := range imc.Status.Properties {
					if pv.ObservationTime.Before(&timeUpdated) {
						return fmt.Errorf("InternalMemberCluster property %s has observation time %v, want before %v", pn, pv.ObservationTime, timeStarted)
					}
				}

				if u := imc.Status.ResourceUsage; u.ObservationTime.Before(&timeUpdated) {
					return fmt.Errorf("InternalMemberCluster resource usage has observation time %v, want before %v", u.ObservationTime, timeStarted)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to update the agent status")
		})

		It("can mark the cluster as left", func() {
			// Add an offset of -1 second to avoid flakiness caused by approximation.
			timeLeft = metav1.Time{Time: time.Now().Add(-time.Second)}

			imc := &clusterv1beta1.InternalMemberCluster{}
			objKey := types.NamespacedName{
				Name:      member2Name,
				Namespace: member2ReservedNSName,
			}
			Expect(hubClient.Get(ctx, objKey, imc)).Should(Succeed())

			imc.Spec.State = clusterv1beta1.ClusterStateLeave
			Expect(hubClient.Update(ctx, imc)).Should(Succeed())
		})

		It("should let the cluster go", func() {
			Eventually(func() error {
				imc := &clusterv1beta1.InternalMemberCluster{}
				objKey := types.NamespacedName{
					Name:      member2Name,
					Namespace: member2ReservedNSName,
				}
				if err := hubClient.Get(ctx, objKey, imc); err != nil {
					return fmt.Errorf("failed to get InternalMemberCluster: %w", err)
				}

				wantIMCStatus := clusterv1beta1.InternalMemberClusterStatus{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						propertyprovider.NodeCountProperty: {
							Value: "3",
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("15"),
							corev1.ResourceMemory: resource.MustParse("49Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("13"),
							corev1.ResourceMemory: resource.MustParse("43Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10"),
							corev1.ResourceMemory: resource.MustParse("36Gi"),
						},
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionFalse,
									Reason:             EventReasonInternalMemberClusterLeft,
									ObservedGeneration: imc.Generation,
								},
								{
									Type:               string(clusterv1beta1.AgentHealthy),
									Status:             metav1.ConditionTrue,
									Reason:             EventReasonInternalMemberClusterHealthy,
									ObservedGeneration: imc.Generation,
								},
							},
						},
					},
				}

				if diff := cmp.Diff(
					imc.Status, wantIMCStatus,
					ignoreAllTimeFields,
					sortByConditionType,
				); diff != "" {
					return fmt.Errorf("InternalMemberCluster status diff (-got, +want):\n%s", diff)
				}

				// Verify the timestamp; for this spec only the last transition time of the
				// AgentJoined condition needs to be checked.

				// Note that at this point the structure of the InternalMemberCluster status object
				// is already known; this condition is guaranteed to be present.
				cond := imc.GetConditionWithType(clusterv1beta1.MemberAgent, string(clusterv1beta1.AgentJoined))
				if cond.LastTransitionTime.Before(&timeLeft) {
					return fmt.Errorf("InternalMemberCluster agent status condition %s has last transition time %v, want before %v", cond.Type, cond.LastTransitionTime, timeLeft)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(Succeed(), "Failed to let the cluster go")
		})
	})
})
