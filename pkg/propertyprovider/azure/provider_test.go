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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/karpenter-provider-azure/pkg/providers/pricing"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure/trackers"
)

const (
	nodeName1 = "node-1"
	nodeName2 = "node-2"
	nodeName3 = "node-3"
	nodeName4 = "node-4"

	podName1 = "pod-1"
	podName2 = "pod-2"
	podName3 = "pod-3"
	podName4 = "pod-4"

	containerName1 = "container-1"
	containerName2 = "container-2"
	containerName3 = "container-3"
	containerName4 = "container-4"

	namespaceName1 = "work-1"
	namespaceName2 = "work-2"
	namespaceName3 = "work-3"

	nodeSKU1 = "Standard_1"
	nodeSKU2 = "Standard_2"
	nodeSKU3 = "Standard_3"

	nodeKnownMissingSKU = "Standard_Missing"

	imageName = "nginx"
)

var (
	currentTime = time.Now()
)

// dummyPricingProvider is a mock implementation that implements the PricingProvider interface.
type dummyPricingProvider struct{}

var _ trackers.PricingProvider = &dummyPricingProvider{}

func (d *dummyPricingProvider) OnDemandPrice(instanceType string) (float64, bool) {
	switch instanceType {
	case nodeSKU1:
		return 1.0, true
	case nodeSKU2:
		return 1.0, true
	case nodeKnownMissingSKU:
		return pricing.MissingPrice, true
	default:
		return 0.0, false
	}
}

func (d *dummyPricingProvider) LastUpdated() time.Time {
	return currentTime
}

// dummyPricingProviderWithStaleData is a mock implementation that implements the PricingProvider interface.
type dummyPricingProviderWithStaleData struct{}

var _ trackers.PricingProvider = &dummyPricingProviderWithStaleData{}

func (d *dummyPricingProviderWithStaleData) OnDemandPrice(instanceType string) (float64, bool) {
	switch instanceType {
	case nodeSKU1:
		return 1.0, true
	case nodeSKU2:
		return 1.0, true
	case nodeKnownMissingSKU:
		return pricing.MissingPrice, true
	default:
		return 0.0, false
	}
}

func (d *dummyPricingProviderWithStaleData) LastUpdated() time.Time {
	return currentTime.Add(-time.Hour * 48)
}

func TestCollect(t *testing.T) {
	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: nodeSKU1,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3.2"),
					corev1.ResourceMemory: resource.MustParse("15.2Gi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName2,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: nodeSKU2,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("30Gi"),
				},
			},
		},
	}
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName1,
				Namespace: namespaceName1,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName1,
				Containers: []corev1.Container{
					{
						Name: containerName1,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
					{
						Name: containerName2,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1.5"),
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName2,
				Namespace: namespaceName1,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName2,
				Containers: []corev1.Container{
					{
						Name: containerName3,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("6"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
						},
					},
				},
			},
		},
	}

	testCases := []struct {
		name                         string
		nodes                        []corev1.Node
		pods                         []corev1.Pod
		pricingprovider              trackers.PricingProvider
		wantMetricCollectionResponse propertyprovider.PropertyCollectionResponse
	}{
		{
			name:            "can report properties",
			nodes:           nodes,
			pods:            pods,
			pricingprovider: &dummyPricingProvider{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU2)): {
						Value: "1",
					},
					PerCPUCoreCostProperty: {
						Value: "0.167",
					},
					PerGBMemoryCostProperty: {
						Value: "0.042",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("12"),
						corev1.ResourceMemory: resource.MustParse("48Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10.2"),
						corev1.ResourceMemory: resource.MustParse("45.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1.7"),
						corev1.ResourceMemory: resource.MustParse("23.2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:    CostPropertiesCollectionSucceededCondType,
						Status:  metav1.ConditionTrue,
						Reason:  CostPropertiesCollectionSucceededReason,
						Message: CostPropertiesCollectionSucceededMsg,
					},
				},
			},
		},
		{
			name:  "will report zero values if the requested resources exceed the allocatable resources",
			nodes: nodes,
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName1,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("10"),
										corev1.ResourceMemory: resource.MustParse("20Gi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("20"),
										corev1.ResourceMemory: resource.MustParse("20Gi"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName2,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName2,
						Containers: []corev1.Container{
							{
								Name: containerName3,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("20"),
										corev1.ResourceMemory: resource.MustParse("20Gi"),
									},
								},
							},
						},
					},
				},
			},
			pricingprovider: &dummyPricingProvider{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU2)): {
						Value: "1",
					},
					PerCPUCoreCostProperty: {
						Value: "0.167",
					},
					PerGBMemoryCostProperty: {
						Value: "0.042",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("12"),
						corev1.ResourceMemory: resource.MustParse("48Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10.2"),
						corev1.ResourceMemory: resource.MustParse("45.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:    CostPropertiesCollectionSucceededCondType,
						Status:  metav1.ConditionTrue,
						Reason:  CostPropertiesCollectionSucceededReason,
						Message: CostPropertiesCollectionSucceededMsg,
					},
				},
			},
		},
		{
			name: "can report cost properties collection failures (no pricing data for any SKU in presence)",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeSKU3,
						},
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
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName2,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeSKU3,
						},
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
				},
			},
			pods:            []corev1.Pod{},
			pricingprovider: &dummyPricingProvider{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU3)): {
						Value: "2",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   CostPropertiesCollectionSucceededCondType,
						Status: metav1.ConditionFalse,
						Reason: CostPropertiesCollectionFailedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionFailedMsgTemplate,
							fmt.Sprintf("nodes are present, but no pricing data is available for any node SKUs (%v)", []string{nodeSKU3}),
						),
					},
				},
			},
		},
		{
			name: "can report cost properties collection failures (no pricing data for some SKUs in presence)",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3.2"),
							corev1.ResourceMemory: resource.MustParse("15.2Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName2,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeSKU3,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
			pods:            []corev1.Pod{},
			pricingprovider: &dummyPricingProvider{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU3)): {
						Value: "1",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("18Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("5.2"),
						corev1.ResourceMemory: resource.MustParse("17.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("5.2"),
						corev1.ResourceMemory: resource.MustParse("17.2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   CostPropertiesCollectionSucceededCondType,
						Status: metav1.ConditionFalse,
						Reason: CostPropertiesCollectionFailedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionFailedMsgTemplate,
							fmt.Sprintf("no pricing data is available for one or more of the node SKUs (%v) in the cluster", []string{nodeSKU3}),
						),
					},
				},
			},
		},
		{
			name: "can report cost properties collection failures (no SKU labels)",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3.2"),
							corev1.ResourceMemory: resource.MustParse("15.2Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName2,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
			pods:            []corev1.Pod{},
			pricingprovider: &dummyPricingProvider{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, trackers.ReservedNameForUndefinedSKU)): {
						Value: "1",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("18Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("5.2"),
						corev1.ResourceMemory: resource.MustParse("17.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("5.2"),
						corev1.ResourceMemory: resource.MustParse("17.2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   CostPropertiesCollectionSucceededCondType,
						Status: metav1.ConditionFalse,
						Reason: CostPropertiesCollectionFailedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionFailedMsgTemplate,
							fmt.Sprintf("no pricing data is available for one or more of the node SKUs (%v) in the cluster", []string{}),
						),
					},
				},
			},
		},
		{
			name: "can report cost properties collection failures (known missing node SKU)",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3.2"),
							corev1.ResourceMemory: resource.MustParse("15.2Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName2,
						Labels: map[string]string{
							trackers.AKSClusterNodeSKULabelName: nodeKnownMissingSKU,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
			pods:            []corev1.Pod{},
			pricingprovider: &dummyPricingProvider{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeKnownMissingSKU)): {
						Value: "1",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("18Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("5.2"),
						corev1.ResourceMemory: resource.MustParse("17.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("5.2"),
						corev1.ResourceMemory: resource.MustParse("17.2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   CostPropertiesCollectionSucceededCondType,
						Status: metav1.ConditionFalse,
						Reason: CostPropertiesCollectionFailedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionFailedMsgTemplate,
							fmt.Sprintf("no pricing data is available for one or more of the node SKUs (%v) in the cluster", []string{nodeKnownMissingSKU}),
						),
					},
				},
			},
		},
		{
			name:            "can report cost properties collection warnings (stale pricing data)",
			nodes:           nodes,
			pods:            pods,
			pricingprovider: &dummyPricingProviderWithStaleData{},
			wantMetricCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "2",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU2)): {
						Value: "1",
					},
					PerCPUCoreCostProperty: {
						Value: "0.167",
					},
					PerGBMemoryCostProperty: {
						Value: "0.042",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("12"),
						corev1.ResourceMemory: resource.MustParse("48Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10.2"),
						corev1.ResourceMemory: resource.MustParse("45.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1.7"),
						corev1.ResourceMemory: resource.MustParse("23.2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   CostPropertiesCollectionSucceededCondType,
						Status: metav1.ConditionTrue,
						Reason: CostPropertiesCollectionDegradedReason,
						Message: fmt.Sprintf(CostPropertiesCollectionDegradedMsgTemplate,
							[]string{
								fmt.Sprintf("the pricing data is stale (last updated at %v); the system might have issues connecting to the Azure Retail Prices API, or the current region is unsupported", currentTime.Add(-time.Hour*48)),
							},
						),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Build the trackers manually for testing purposes.
			nodeTracker := trackers.NewNodeTracker(tc.pricingprovider)
			podTracker := trackers.NewPodTracker()
			for idx := range tc.nodes {
				nodeTracker.AddOrUpdate(&tc.nodes[idx])
			}
			for idx := range tc.pods {
				podTracker.AddOrUpdate(&tc.pods[idx])
			}
			k8sversion := "1.35.5"
			p := &PropertyProvider{
				nodeTracker:                           nodeTracker,
				podTracker:                            podTracker,
				isCostCollectionEnabled:               true,
				isAvailableResourcesCollectionEnabled: true,
				cachedK8sVersion:                      k8sversion,
				cachedK8sVersionObservedTime:          time.Now(),
			}
			tc.wantMetricCollectionResponse.Properties[propertyprovider.K8sVersionProperty] = clusterv1beta1.PropertyValue{
				Value: k8sversion,
			}
			res := p.Collect(ctx)
			if diff := cmp.Diff(res, tc.wantMetricCollectionResponse, ignoreObservationTimeFieldInPropertyValue); diff != "" {
				t.Fatalf("Collect() property collection response diff (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestCollectWithDisabledFeatures(t *testing.T) {
	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
				Labels: map[string]string{
					trackers.AKSClusterNodeSKULabelName: nodeSKU1,
				},
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3.2"),
					corev1.ResourceMemory: resource.MustParse("15.2Gi"),
				},
			},
		},
	}
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName1,
				Namespace: namespaceName1,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName1,
				Containers: []corev1.Container{
					{
						Name: containerName1,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
					{
						Name: containerName2,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1.5"),
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
		},
	}

	testCases := []struct {
		name                                  string
		nodeTracker                           *trackers.NodeTracker
		podTracker                            *trackers.PodTracker
		isCostCollectionEnabled               bool
		isAvailableResourcesCollectionEnabled bool
		wantPropertyCollectionResponse        propertyprovider.PropertyCollectionResponse
	}{
		{
			name:                                  "cost collection disabled",
			nodeTracker:                           trackers.NewNodeTracker(nil),
			podTracker:                            trackers.NewPodTracker(),
			isCostCollectionEnabled:               false,
			isAvailableResourcesCollectionEnabled: true,
			wantPropertyCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("3.2"),
						corev1.ResourceMemory: resource.MustParse("15.2Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("0.7"),
						corev1.ResourceMemory: resource.MustParse("9.2Gi"),
					},
				},
				Conditions: []metav1.Condition{},
			},
		},
		{
			name:                                  "available resources collection disabled",
			nodeTracker:                           trackers.NewNodeTracker(&dummyPricingProvider{}),
			isCostCollectionEnabled:               true,
			isAvailableResourcesCollectionEnabled: false,
			wantPropertyCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
					PerCPUCoreCostProperty: {
						Value: "0.250",
					},
					PerGBMemoryCostProperty: {
						Value: "0.062",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("3.2"),
						corev1.ResourceMemory: resource.MustParse("15.2Gi"),
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:    CostPropertiesCollectionSucceededCondType,
						Status:  metav1.ConditionTrue,
						Reason:  CostPropertiesCollectionSucceededReason,
						Message: CostPropertiesCollectionSucceededMsg,
					},
				},
			},
		},
		{
			name:                                  "both cost and available resources collection disabled",
			nodeTracker:                           trackers.NewNodeTracker(nil),
			isCostCollectionEnabled:               false,
			isAvailableResourcesCollectionEnabled: false,
			wantPropertyCollectionResponse: propertyprovider.PropertyCollectionResponse{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "1",
					},
					clusterv1beta1.PropertyName(fmt.Sprintf(NodeCountPerSKUPropertyTmpl, nodeSKU1)): {
						Value: "1",
					},
				},
				Resources: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("3.2"),
						corev1.ResourceMemory: resource.MustParse("15.2Gi"),
					},
				},
				Conditions: []metav1.Condition{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Build the trackers manually for testing purposes.
			nodeTracker := tc.nodeTracker
			for idx := range nodes {
				nodeTracker.AddOrUpdate(&nodes[idx])
			}
			podTracker := tc.podTracker
			if podTracker != nil {
				for idx := range pods {
					podTracker.AddOrUpdate(&pods[idx])
				}
			}
			k8sversion := "1.34.6"
			p := &PropertyProvider{
				nodeTracker:                           nodeTracker,
				podTracker:                            podTracker,
				isCostCollectionEnabled:               tc.isCostCollectionEnabled,
				isAvailableResourcesCollectionEnabled: tc.isAvailableResourcesCollectionEnabled,
				cachedK8sVersion:                      k8sversion,
				cachedK8sVersionObservedTime:          time.Now(),
				clusterCertificateAuthority:           []byte("CADATA"),
			}
			tc.wantPropertyCollectionResponse.Properties[propertyprovider.K8sVersionProperty] = clusterv1beta1.PropertyValue{
				Value: k8sversion,
			}
			tc.wantPropertyCollectionResponse.Properties[propertyprovider.ClusterCertificateAuthorityProperty] = clusterv1beta1.PropertyValue{
				Value: "CADATA",
			}
			res := p.Collect(ctx)
			if diff := cmp.Diff(res, tc.wantPropertyCollectionResponse, ignoreObservationTimeFieldInPropertyValue); diff != "" {
				t.Fatalf("Collect() property collection response diff (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestCollectK8sVersion(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name                   string
		discoveryClient        *fake.FakeDiscovery
		cachedVersion          string
		cacheStartTime         time.Time
		cacheTTL               time.Duration
		wantVersion            string
		wantDiscoveryCallsMade bool
	}{
		{
			name: "cache miss - no cached version",
			discoveryClient: &fake.FakeDiscovery{
				Fake: &k8stesting.Fake{},
				FakedServerVersion: &version.Info{
					GitVersion: "v1.28.0",
				},
			},
			cachedVersion:          "",
			cacheStartTime:         time.Now().Add(-1 * time.Hour), // 1 hour ago
			cacheTTL:               15 * time.Minute,
			wantVersion:            "v1.28.0",
			wantDiscoveryCallsMade: true,
		},
		{
			name: "cache hit - cached version still valid",
			discoveryClient: &fake.FakeDiscovery{
				Fake: &k8stesting.Fake{},
				FakedServerVersion: &version.Info{
					GitVersion: "v1.28.0",
				},
			},
			cachedVersion:          "v1.27.0",
			cacheStartTime:         time.Now().Add(-5 * time.Minute), // 5 minutes ago
			cacheTTL:               15 * time.Minute,
			wantVersion:            "v1.27.0",
			wantDiscoveryCallsMade: false,
		},
		{
			name: "cache expired - cached version too old",
			discoveryClient: &fake.FakeDiscovery{
				Fake: &k8stesting.Fake{},
				FakedServerVersion: &version.Info{
					GitVersion: "v1.28.0",
				},
			},
			cachedVersion:          "v1.27.0",
			cacheStartTime:         time.Now().Add(-20 * time.Minute), // 20 minutes ago
			cacheTTL:               1 * time.Minute,
			wantVersion:            "v1.28.0",
			wantDiscoveryCallsMade: true,
		},
		{
			name: "cache at TTL boundary - should be expired",
			discoveryClient: &fake.FakeDiscovery{
				Fake: &k8stesting.Fake{},
				FakedServerVersion: &version.Info{
					GitVersion: "v1.28.0",
				},
			},
			cachedVersion:          "v1.27.0",
			cacheStartTime:         time.Now().Add(-5*time.Minute - time.Second), // Just over 15 minutes
			cacheTTL:               5 * time.Minute,
			wantVersion:            "v1.28.0",
			wantDiscoveryCallsMade: true,
		},
		{
			name: "discovery client error - no property set",
			discoveryClient: func() *fake.FakeDiscovery {
				f := &fake.FakeDiscovery{
					Fake: &k8stesting.Fake{},
				}
				f.AddReactor("get", "version", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("connection refused")
				})
				return f
			}(),
			cachedVersion:          "",
			cacheStartTime:         time.Time{},
			cacheTTL:               15 * time.Minute,
			wantVersion:            "", // No property should be set
			wantDiscoveryCallsMade: true,
		},
		{
			name: "cache hit - cached version still valid even if the client errors",
			discoveryClient: func() *fake.FakeDiscovery {
				f := &fake.FakeDiscovery{
					Fake: &k8stesting.Fake{},
				}
				f.AddReactor("get", "version", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("connection refused")
				})
				return f
			}(),
			cachedVersion:          "v1.27.0",
			cacheStartTime:         time.Now().Add(-5 * time.Minute), // 5 minutes ago
			cacheTTL:               15 * time.Minute,
			wantVersion:            "v1.27.0",
			wantDiscoveryCallsMade: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k8sVersionCacheTTL = tc.cacheTTL
			p := &PropertyProvider{
				cachedK8sVersion:             tc.cachedVersion,
				cachedK8sVersionObservedTime: tc.cacheStartTime,
			}
			// Only set discoveryClient if it's not nil to avoid interface nil pointer issues
			if tc.discoveryClient != nil {
				p.discoveryClient = tc.discoveryClient
			}

			properties := make(map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue)
			p.collectK8sVersion(ctx, properties)

			if tc.wantVersion == "" {
				// No property should be set
				if _, ok := properties[propertyprovider.K8sVersionProperty]; ok {
					t.Errorf("Expected no Kubernetes version property to be set, but got one")
				}
			} else {
				// Check that the property is set correctly
				gotProperty, ok := properties[propertyprovider.K8sVersionProperty]
				if !ok {
					t.Fatalf("Expected Kubernetes version property to be set, but it was not")
				}
				if gotProperty.Value != tc.wantVersion {
					t.Errorf("collectK8sVersion() version = %v, want %v", gotProperty.Value, tc.wantVersion)
				}
			} // Check if discovery client was called the expected number of times
			if tc.discoveryClient != nil {
				if tc.wantDiscoveryCallsMade && len(tc.discoveryClient.Actions()) == 0 {
					t.Errorf("Expected discovery client to be called, but it was not")
				}
				if !tc.wantDiscoveryCallsMade && len(tc.discoveryClient.Actions()) > 0 {
					t.Errorf("Expected discovery client not to be called, but it was called %d times", len(tc.discoveryClient.Actions()))
				}
			}

			// Verify cache was updated when discovery client was called successfully
			if tc.wantDiscoveryCallsMade && tc.discoveryClient != nil && tc.discoveryClient.FakedServerVersion != nil {
				if p.cachedK8sVersion != tc.wantVersion {
					t.Errorf("Expected cached version to be %v, but got %v", tc.wantVersion, p.cachedK8sVersion)
				}
				if p.cachedK8sVersionObservedTime.IsZero() {
					t.Errorf("Expected cache time to be updated, but it was not")
				}
			}
		})
	}
}

func TestCollectK8sVersionConcurrency(t *testing.T) {
	ctx := context.Background()

	discoveryClient := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{},
		FakedServerVersion: &version.Info{
			GitVersion: "v1.28.0",
		},
	}

	p := &PropertyProvider{
		discoveryClient: discoveryClient,
	}

	// Run multiple concurrent calls to collectK8sVersion
	const numGoroutines = 100
	k8sVersionCacheTTL = 1 * time.Second // Reduce cache TTL to test cache expire
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			properties := make(map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue)
			p.collectK8sVersion(ctx, properties)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify that the discovery client was called at least once
	if len(discoveryClient.Actions()) == 0 {
		t.Errorf("Expected discovery client to be called at least once, but it was not")
	}

	// Verify that the cache was populated
	if p.cachedK8sVersion != "v1.28.0" {
		t.Errorf("Expected cached version to be v1.28.0, but got %v", p.cachedK8sVersion)
	}
}
