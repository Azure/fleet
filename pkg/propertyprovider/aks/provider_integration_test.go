/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package aks

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider"
	"go.goms.io/fleet/pkg/propertyprovider/aks/trackers"
)

var (
	ignoreObservationTimeFieldInPropertyValue = cmpopts.IgnoreFields(clusterv1beta1.PropertyValue{}, "ObservationTime")
)

var (
	nodeDeletedActual = func(name string) func() error {
		return func() error {
			node := &corev1.Node{}
			if err := memberClient.Get(ctx, types.NamespacedName{Name: name}, node); !errors.IsNotFound(err) {
				return fmt.Errorf("node has not been deleted yet")
			}
			return nil
		}
	}
	podDeletedActual = func(namespace, name string) func() error {
		return func() error {
			pod := &corev1.Pod{}
			if err := memberClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod); !errors.IsNotFound(err) {
				return fmt.Errorf("pod has not been deleted yet")
			}
			return nil
		}
	}

	shouldCreateNodes = func(nodes ...corev1.Node) func() {
		return func() {
			for idx := range nodes {
				node := nodes[idx].DeepCopy()
				Expect(memberClient.Create(ctx, node)).To(Succeed(), "Failed to create node")

				Expect(memberClient.Get(ctx, types.NamespacedName{Name: node.Name}, node)).To(Succeed(), "Failed to get node")
				node.Status.Allocatable = nodes[idx].Status.Allocatable.DeepCopy()
				node.Status.Capacity = nodes[idx].Status.Capacity.DeepCopy()
				Expect(memberClient.Status().Update(ctx, node)).To(Succeed(), "Failed to update node status")
			}
		}
	}
	shouldDeleteNodes = func(nodes ...corev1.Node) func() {
		return func() {
			for idx := range nodes {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodes[idx].Name,
					},
				}
				Expect(memberClient.Delete(ctx, node)).To(SatisfyAny(Succeed(), MatchError(errors.IsNotFound)), "Failed to delete node")

				// Wait for the node to be deleted.
				Eventually(nodeDeletedActual(node.Name), eventuallyDuration, eventuallyInterval).Should(BeNil())
			}
		}
	}
	shouldCreatePods = func(pods ...corev1.Pod) func() {
		return func() {
			for idx := range pods {
				pod := pods[idx].DeepCopy()
				Expect(memberClient.Create(ctx, pod)).To(Succeed(), "Failed to create pod")
			}
		}
	}
	shouldBindPods = func(pods ...corev1.Pod) func() {
		return func() {
			for idx := range pods {
				pod := pods[idx].DeepCopy()
				binding := &corev1.Binding{
					Target: corev1.ObjectReference{
						Name: pod.Spec.NodeName,
					},
				}
				Expect(memberClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).To(Succeed(), "Failed to get pod")
				Expect(memberClient.SubResource("binding").Create(ctx, pod, binding)).To(Succeed(), "Failed to bind pod to node")
			}
		}
	}
	shouldDeletePods = func(pods ...corev1.Pod) func() {
		return func() {
			for idx := range pods {
				pod := pods[idx].DeepCopy()
				Expect(memberClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)).To(Succeed(), "Failed to get pod")

				// Transition the pod into a terminal state (if it has not been done).
				if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
					pod.Status.Phase = corev1.PodSucceeded
					Expect(memberClient.Status().Update(ctx, pod)).To(Succeed(), "Failed to update pod status")
				}

				// Delete the pod.
				Expect(memberClient.Delete(ctx, pod)).To(SatisfyAny(Succeed(), MatchError(errors.IsNotFound)), "Failed to delete pod")

				// Wait for the pod to be deleted.
				Eventually(podDeletedActual(pod.Namespace, pod.Name), eventuallyDuration, eventuallyInterval).Should(BeNil())
			}
		}

	}
	shouldReportCorrectPropertiesForNodes = func(nodes []corev1.Node, pods []corev1.Pod) func() {
		return func() {
			totalCPUCapacity := resource.Quantity{}
			allocatableCPUCapacity := resource.Quantity{}
			totalMemoryCapacity := resource.Quantity{}
			allocatableMemoryCapacity := resource.Quantity{}

			for idx := range nodes {
				node := nodes[idx]
				totalCPUCapacity.Add(node.Status.Capacity[corev1.ResourceCPU])
				allocatableCPUCapacity.Add(node.Status.Allocatable[corev1.ResourceCPU])
				totalMemoryCapacity.Add(node.Status.Capacity[corev1.ResourceMemory])
				allocatableMemoryCapacity.Add(node.Status.Allocatable[corev1.ResourceMemory])
			}

			totalCPUCores := totalCPUCapacity.AsApproximateFloat64()
			totalMemoryBytes := totalMemoryCapacity.AsApproximateFloat64()
			totalMemoryGBs := totalMemoryBytes / (1024.0 * 1024.0 * 1024.0)

			requestedCPUCapacity := resource.Quantity{}
			requestedMemoryCapacity := resource.Quantity{}
			for idx := range pods {
				pod := pods[idx]
				for cidx := range pod.Spec.Containers {
					c := pod.Spec.Containers[cidx]
					requestedCPUCapacity.Add(c.Resources.Requests[corev1.ResourceCPU])
					requestedMemoryCapacity.Add(c.Resources.Requests[corev1.ResourceMemory])
				}
			}

			availableCPUCapacity := allocatableCPUCapacity.DeepCopy()
			availableCPUCapacity.Sub(requestedCPUCapacity)
			availableMemoryCapacity := allocatableMemoryCapacity.DeepCopy()
			availableMemoryCapacity.Sub(requestedMemoryCapacity)

			Eventually(func() error {
				// Calculate the costs manually; hardcoded values cannot be used as Azure pricing
				// is subject to periodic change.

				// Note that this is done within an eventually block to ensure that the
				// calculation is done using the latest pricing data. Inconsistency
				// should seldom occur though.
				totalCost := 0.0
				for idx := range nodes {
					node := nodes[idx]
					cost, found := pp.OnDemandPrice(node.Labels[trackers.AKSClusterNodeSKULabelName])
					if !found {
						return fmt.Errorf("on-demand price not found for SKU %s", node.Labels[trackers.AKSClusterNodeSKULabelName])
					}
					totalCost += cost
				}
				perCPUCost := fmt.Sprintf(CostPrecisionTemplate, totalCost/totalCPUCores)
				perGBMemoryCost := fmt.Sprintf(CostPrecisionTemplate, totalCost/totalMemoryGBs)

				expectedRes := propertyprovider.PropertyCollectionResponse{
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						NodeCountProperty: {
							Value: fmt.Sprintf("%d", len(nodes)),
						},
						PerCPUCoreCostProperty: {
							Value: perCPUCost,
						},
						PerGBMemoryCostProperty: {
							Value: perGBMemoryCost,
						},
					},
					Resources: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    totalCPUCapacity,
							corev1.ResourceMemory: totalMemoryCapacity,
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    allocatableCPUCapacity,
							corev1.ResourceMemory: allocatableMemoryCapacity,
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    availableCPUCapacity,
							corev1.ResourceMemory: availableMemoryCapacity,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:    PropertyCollectionSucceededConditionType,
							Status:  metav1.ConditionTrue,
							Reason:  PropertyCollectionSucceededReason,
							Message: PropertyCollectionSucceededMessage,
						},
					},
				}

				res := p.Collect(ctx)
				if diff := cmp.Diff(res, expectedRes, ignoreObservationTimeFieldInPropertyValue); diff != "" {
					return fmt.Errorf("property collection response (-got, +want):\n%s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(BeNil())
		}
	}
)

var (
	// The nodes below use actual capacities of their respective AKS SKUs; for more information,
	// see:
	// https://learn.microsoft.com/en-us/azure/virtual-machines/av2-series (for A-series nodes),
	// https://learn.microsoft.com/en-us/azure/virtual-machines/sizes-b-series-burstable (for B-series nodes), and
	// https://learn.microsoft.com/en-us/azure/virtual-machines/dv2-dsv2-series (for D/DS v2 series nodes).
	nodes = []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: aksNodeSKU1,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8130080Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3860m"),
					corev1.ResourceMemory: resource.MustParse("5474848Ki"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName2,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: aksNodeSKU2,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16374624Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3860m"),
					corev1.ResourceMemory: resource.MustParse("12880736Ki"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName3,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: aksNodeSKU3,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("7097684Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1900m"),
					corev1.ResourceMemory: resource.MustParse("4652372Ki"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName4,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: aksNodeSKU3,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("7097684Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1900m"),
					corev1.ResourceMemory: resource.MustParse("4652372Ki"),
				},
			},
		},
	}

	pods = []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName1,
				Namespace: namespaceName1,
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
								corev1.ResourceMemory: resource.MustParse("500Mi"),
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName2,
				Namespace: namespaceName2,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName2,
				Containers: []corev1.Container{
					{
						Name:  containerName2,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1.5"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName3,
				Namespace: namespaceName3,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName3,
				Containers: []corev1.Container{
					{
						Name:  containerName3,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("3Gi"),
							},
						},
					},
					{
						Name:  containerName4,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName4,
				Namespace: namespaceName3,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName4,
				Containers: []corev1.Container{
					{
						Name:  containerName3,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("3Gi"),
							},
						},
					},
					{
						Name:  containerName4,
						Image: imageName,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
		},
	}
)

// All the test cases in this block are serial + ordered, as manipulation of nodes/pods
// in one test case will disrupt another.
var _ = Describe("aks property provider", func() {
	Context("add a new node", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes[0]))

		It("should report correct properties", shouldReportCorrectPropertiesForNodes(nodes[0:1], nil))

		AfterAll(shouldDeleteNodes(nodes[0]))
	})

	Context("add multiple nodes", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		It("should report correct properties", shouldReportCorrectPropertiesForNodes(nodes, nil))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a node", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		It("should report correct properties", shouldReportCorrectPropertiesForNodes(nodes, nil))

		It("can delete a node", shouldDeleteNodes(nodes[0]))

		It("should report correct properties after deletion", shouldReportCorrectPropertiesForNodes(nodes[1:], nil))

		AfterAll(shouldDeleteNodes(nodes[1:]...))
	})

	Context("remove multiple nodes", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		It("should report correct properties", shouldReportCorrectPropertiesForNodes(nodes, nil))

		It("can delete multiple nodes", shouldDeleteNodes(nodes[0], nodes[3]))

		It("should report correct properties after deletion", shouldReportCorrectPropertiesForNodes(nodes[1:3], nil))

		AfterAll(shouldDeleteNodes(nodes[1], nodes[2]))
	})

	Context("add a pod", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", shouldReportCorrectPropertiesForNodes(nodes, pods[0:1]))

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("add a pod (not bound)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(func() {
			pod := pods[0].DeepCopy()
			pod.Spec.NodeName = ""
			Expect(memberClient.Create(ctx, pod)).To(Succeed(), "Failed to create pod")
		})

		It("should report correct properties (pod not bound)", shouldReportCorrectPropertiesForNodes(nodes, nil))

		It("can bind the pod", shouldBindPods(pods[0]))

		It("should report correct properties (pod bound)", shouldReportCorrectPropertiesForNodes(nodes, pods[0:1]))

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("add multiple pods", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods...))

		It("should report correct properties (pods bound)", shouldReportCorrectPropertiesForNodes(nodes, pods))

		AfterAll(shouldDeletePods(pods...))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a pod (deleted)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", shouldReportCorrectPropertiesForNodes(nodes, pods[0:1]))

		It("can delete the pod", shouldDeletePods(pods[0]))

		It("should report correct properties (pod deleted)", shouldReportCorrectPropertiesForNodes(nodes, nil))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a pod (succeeded)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", shouldReportCorrectPropertiesForNodes(nodes, pods[0:1]))

		It("can transition the pod to the succeeded state", func() {
			pod := pods[0].DeepCopy()
			pod.Status.Phase = corev1.PodSucceeded
			Expect(memberClient.Status().Update(ctx, pod)).To(Succeed(), "Failed to update pod status")
		})

		It("should report correct properties (pod succeeded)", shouldReportCorrectPropertiesForNodes(nodes, nil))

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a pod (failed)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", shouldReportCorrectPropertiesForNodes(nodes, pods[0:1]))

		It("can transition the pod to the failed state", func() {
			pod := pods[0].DeepCopy()
			pod.Status.Phase = corev1.PodFailed
			Expect(memberClient.Status().Update(ctx, pod)).To(Succeed(), "Failed to update pod status")
		})

		It("should report correct properties (pod failed)", shouldReportCorrectPropertiesForNodes(nodes, nil))

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove multiple pods", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods...))

		It("should report correct properties (pods bound)", shouldReportCorrectPropertiesForNodes(nodes, pods))

		It("can delete multiple pods", shouldDeletePods(pods[1], pods[2]))

		It("should report correct properties (pods deleted)", shouldReportCorrectPropertiesForNodes(nodes, []corev1.Pod{pods[0], pods[3]}))

		AfterAll(shouldDeletePods(pods[0], pods[3]))

		AfterAll(shouldDeleteNodes(nodes...))
	})
})
