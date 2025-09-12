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

package azure

import (
	"fmt"
	"slices"
	"time"

	"github.com/Azure/karpenter-provider-azure/pkg/providers/pricing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure/trackers"
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
	shouldReportCorrectProperties = func(
		p propertyprovider.PropertyProvider,
		nodes []corev1.Node, pods []corev1.Pod,
		isCostsEnabled, isAvailableResourcesEnabled bool,
	) {
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
			missingSKUSet := map[string]bool{}
			isPricingDataStale := false

			pricingDataLastUpdated := pp.LastUpdated()
			pricingDataBestAfter := time.Now().Add(-trackers.PricingDataShelfLife)
			if pricingDataLastUpdated.Before(pricingDataBestAfter) {
				isPricingDataStale = true
			}

			for idx := range nodes {
				node := nodes[idx]
				sku := node.Labels[trackers.AKSClusterNodeSKULabelName]
				cost, found := pp.OnDemandPrice(sku)
				if !found || cost == pricing.MissingPrice {
					missingSKUSet[sku] = true
					continue
				}
				totalCost += cost
			}
			missingSKUs := []string{}
			for sku := range missingSKUSet {
				missingSKUs = append(missingSKUs, sku)
			}
			slices.Sort(missingSKUs)

			perCPUCoreCost := "0.0"
			perGBMemoryCost := "0.0"
			costCollectionWarnings := []string{}
			var costCollectionErr error

			switch {
			case len(nodes) == 0:
			case totalCost == 0.0:
				costCollectionErr = fmt.Errorf("nodes are present, but no pricing data is available for any node SKUs (%v)", missingSKUs)
			case len(missingSKUs) > 0:
				costCollectionErr = fmt.Errorf("no pricing data is available for one or more of the node SKUs (%v) in the cluster", missingSKUs)
			default:
				perCPUCoreCost = fmt.Sprintf(CostPrecisionTemplate, totalCost/totalCPUCores)
				perGBMemoryCost = fmt.Sprintf(CostPrecisionTemplate, totalCost/totalMemoryGBs)
			}

			if isPricingDataStale {
				costCollectionWarnings = append(costCollectionWarnings,
					fmt.Sprintf("the pricing data is stale (last updated at %v); the system might have issues connecting to the Azure Retail Prices API, or the current region is unsupported", pricingDataLastUpdated),
				)
			}

			wantProperties := map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
				propertyprovider.NodeCountProperty: {
					Value: fmt.Sprintf("%d", len(nodes)),
				},
			}

			if costCollectionErr == nil && isCostsEnabled {
				wantProperties[PerCPUCoreCostProperty] = clusterv1beta1.PropertyValue{
					Value: perCPUCoreCost,
				}
				wantProperties[PerGBMemoryCostProperty] = clusterv1beta1.PropertyValue{
					Value: perGBMemoryCost,
				}
			}

			var wantConditions []metav1.Condition
			switch {
			case !isCostsEnabled:
				// Cost calculation has been disabled. No need to add a condition.
			case costCollectionErr != nil:
				wantConditions = []metav1.Condition{
					{
						Type:    CostPropertiesCollectionSucceededCondType,
						Status:  metav1.ConditionFalse,
						Reason:  CostPropertiesCollectionFailedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionFailedMsgTemplate, costCollectionErr),
					},
				}
			case len(costCollectionWarnings) > 0:
				wantConditions = []metav1.Condition{
					{
						Type:    CostPropertiesCollectionSucceededCondType,
						Status:  metav1.ConditionTrue,
						Reason:  CostPropertiesCollectionDegradedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionDegradedMsgTemplate, costCollectionWarnings),
					},
				}
			default:
				wantConditions = []metav1.Condition{
					{
						Type:    CostPropertiesCollectionSucceededCondType,
						Status:  metav1.ConditionTrue,
						Reason:  CostPropertiesCollectionSucceededReason,
						Message: CostPropertiesCollectionSucceededMsg,
					},
				}
			}

			expectedRes := propertyprovider.PropertyCollectionResponse{
				Properties: wantProperties,
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    totalCPUCapacity,
						corev1.ResourceMemory: totalMemoryCapacity,
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    allocatableCPUCapacity,
						corev1.ResourceMemory: allocatableMemoryCapacity,
					},
				},
				Conditions: wantConditions,
			}

			if isAvailableResourcesEnabled {
				expectedRes.Resources.Available = corev1.ResourceList{
					corev1.ResourceCPU:    availableCPUCapacity,
					corev1.ResourceMemory: availableMemoryCapacity,
				}
			}

			res := p.Collect(ctx)
			if diff := cmp.Diff(res, expectedRes, ignoreObservationTimeFieldInPropertyValue, cmpopts.EquateEmpty()); diff != "" {
				return fmt.Errorf("property collection response (-got, +want):\n%s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(BeNil())
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

	nodesWithSomeUnsupportedSKUs = []corev1.Node{
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
					trackers.AKSClusterNodeSKULabelName: unsupportedSKU1,
				},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName3,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: unsupportedSKU2,
				},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2000000Ki"),
				},
			},
		},
	}

	nodesWithSomeEmptySKUs = []corev1.Node{
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
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName3,
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2000000Ki"),
				},
			},
		},
	}

	nodesWithAllUnsupportedSKUs = []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: unsupportedSKU1,
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
	}

	nodesWithAllEmptySKUs = []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
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
	}

	nodesWithSomeKnownMissingSKUs = []corev1.Node{
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
					trackers.AKSClusterNodeSKULabelName: aksNodeKnownMissingSKU1,
				},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
			},
		},
	}

	nodesWithAllKnownMissingSKUs = []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: aksNodeKnownMissingSKU1,
				},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName2,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: aksNodeKnownMissingSKU2,
				},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1000000Ki"),
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
var _ = Describe("azure property provider", func() {
	Context("add a new node", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes[0]))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodes[0:1], nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodes[0]))
	})

	Context("add multiple nodes", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a node", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		It("can delete a node", shouldDeleteNodes(nodes[0]))

		It("should report correct properties after deletion", func() {
			shouldReportCorrectProperties(p, nodes[1:], nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodes[1:]...))
	})

	Context("remove multiple nodes", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		It("can delete multiple nodes", shouldDeleteNodes(nodes[0], nodes[3]))

		It("should report correct properties after deletion", func() {
			shouldReportCorrectProperties(p, nodes[1:3], nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodes[1], nodes[2]))
	})

	Context("add a pod", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods[0:1], true, true)
		})

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

		It("should report correct properties (pod not bound)", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		It("can bind the pod", shouldBindPods(pods[0]))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods[0:1], true, true)
		})

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("add multiple pods", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods...))

		It("should report correct properties (pods bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods, true, true)
		})

		AfterAll(shouldDeletePods(pods...))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a pod (deleted)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods[0:1], true, true)
		})

		It("can delete the pod", shouldDeletePods(pods[0]))

		It("should report correct properties (pod deleted)", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a pod (succeeded)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods[0:1], true, true)
		})

		It("can transition the pod to the succeeded state", func() {
			pod := pods[0].DeepCopy()
			pod.Status.Phase = corev1.PodSucceeded
			Expect(memberClient.Status().Update(ctx, pod)).To(Succeed(), "Failed to update pod status")
		})

		It("should report correct properties (pod succeeded)", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove a pod (failed)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods[0]))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods[0:1], true, true)
		})

		It("can transition the pod to the failed state", func() {
			pod := pods[0].DeepCopy()
			pod.Status.Phase = corev1.PodFailed
			Expect(memberClient.Status().Update(ctx, pod)).To(Succeed(), "Failed to update pod status")
		})

		It("should report correct properties (pod failed)", func() {
			shouldReportCorrectProperties(p, nodes, nil, true, true)
		})

		AfterAll(shouldDeletePods(pods[0]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("remove multiple pods", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods...))

		It("should report correct properties (pods bound)", func() {
			shouldReportCorrectProperties(p, nodes, pods, true, true)
		})

		It("can delete multiple pods", shouldDeletePods(pods[1], pods[2]))

		It("should report correct properties (pods deleted)", func() {
			shouldReportCorrectProperties(p, nodes, []corev1.Pod{pods[0], pods[3]}, true, true)
		})

		AfterAll(shouldDeletePods(pods[0], pods[3]))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("nodes with some unsupported SKUs", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodesWithSomeUnsupportedSKUs...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodesWithSomeUnsupportedSKUs, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodesWithSomeUnsupportedSKUs...))
	})

	Context("nodes with some empty SKUs", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodesWithSomeEmptySKUs...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodesWithSomeEmptySKUs, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodesWithSomeEmptySKUs...))
	})

	Context("nodes with all unsupported SKUs", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodesWithAllUnsupportedSKUs...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodesWithAllUnsupportedSKUs, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodesWithAllUnsupportedSKUs...))
	})

	Context("nodes with all empty SKUs", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodesWithAllEmptySKUs...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodesWithAllEmptySKUs, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodesWithAllEmptySKUs...))
	})

	// This covers a known issue with Azure Retail Prices API, where some deprecated SKUs are no longer
	// reported by the API, but can still be used in an AKS cluster.
	Context("nodes with some known missing SKUs", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodesWithSomeKnownMissingSKUs...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodesWithSomeKnownMissingSKUs, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodesWithSomeKnownMissingSKUs...))
	})

	// This covers a known issue with Azure Retail Prices API, where some deprecated SKUs are no longer
	// reported by the API, but can still be used in an AKS cluster.
	Context("nodes with all known missing SKUs", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodesWithAllKnownMissingSKUs...))

		It("should report correct properties", func() {
			shouldReportCorrectProperties(p, nodesWithAllKnownMissingSKUs, nil, true, true)
		})

		AfterAll(shouldDeleteNodes(nodesWithAllKnownMissingSKUs...))
	})
})

var _ = Describe("feature gates", func() {
	Context("nodes and pods (cost info disabled)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods...))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(pWithNoCosts, nodes, pods, false, true)
		})

		AfterAll(shouldDeletePods(pods...))

		AfterAll(shouldDeleteNodes(nodes...))
	})

	Context("nodes and pods (available resources disabled)", Serial, Ordered, func() {
		BeforeAll(shouldCreateNodes(nodes...))

		BeforeAll(shouldCreatePods(pods...))

		It("should report correct properties (pod bound)", func() {
			shouldReportCorrectProperties(pWithNoAvailableResources, nodes, pods, true, false)
		})

		AfterAll(shouldDeletePods(pods...))

		AfterAll(shouldDeleteNodes(nodes...))
	})
})
