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

package trackers

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	nodeName1 = "node-1"
	nodeName2 = "node-2"
	nodeName3 = "node-3"
	nodeName4 = "node-4"

	namespaceName1 = "namespace-1"
	namespaceName2 = "namespace-2"
	namespaceName3 = "namespace-3"

	podName1 = "pod-1"
	podName2 = "pod-2"
	podName3 = "pod-3"

	containerName1 = "container-1"
	containerName2 = "container-2"
	containerName3 = "container-3"

	nodeSKU1 = "Standard_1"
	nodeSKU2 = "Standard_2"
	nodeSKU3 = "Standard_3"
	nodeSKU4 = "Standard_4"
)

var (
	currentTime = time.Now()
)

// dummyPricingProvider is a mock implementation that implements the PricingProvider interface.
type dummyPricingProvider struct{}

var _ PricingProvider = &dummyPricingProvider{}

func (d *dummyPricingProvider) OnDemandPrice(instanceType string) (float64, bool) {
	switch instanceType {
	case nodeSKU1:
		return 1.0, true
	case nodeSKU2:
		return 5.0, true
	case nodeSKU3:
		return 10.0, true
	default:
		return 0.0, false
	}
}

func (d *dummyPricingProvider) LastUpdated() time.Time {
	return currentTime
}

var (
	ignoreNodeTrackerFields    = cmpopts.IgnoreFields(NodeTracker{}, "mu", "pricingProvider")
	ignoreCostInfoFields       = cmpopts.IgnoreFields(costInfo{}, "lastUpdated")
	ignorePodTrackerMutexField = cmpopts.IgnoreFields(PodTracker{}, "mu")

	// This variable should only work with test cases that do not mutate the tracker.
	nodeTrackerWith3Nodes = &NodeTracker{
		totalCapacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("11"),
			corev1.ResourceMemory: resource.MustParse("30Gi"),
		},
		totalAllocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("8.3"),
			corev1.ResourceMemory: resource.MustParse("25.8Gi"),
		},
		capacityByNode: map[string]corev1.ResourceList{
			nodeName1: {
				corev1.ResourceCPU:    resource.MustParse("5"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
			nodeName2: {
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("12Gi"),
			},
			nodeName3: {
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		allocatableByNode: map[string]corev1.ResourceList{
			nodeName1: {
				corev1.ResourceCPU:    resource.MustParse("3.5"),
				corev1.ResourceMemory: resource.MustParse("12.5Gi"),
			},
			nodeName2: {
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("11.5Gi"),
			},
			nodeName3: {
				corev1.ResourceCPU:    resource.MustParse("1.8"),
				corev1.ResourceMemory: resource.MustParse("1.8Gi"),
			},
		},
		pricingProvider: &dummyPricingProvider{},
	}

	// This variable should only work with test cases that do not mutate the tracker.
	podTrackerWith3Pods = &PodTracker{
		totalRequested: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("8"),
			corev1.ResourceMemory: *resource.NewQuantity(2384461824, resource.BinarySI),
		},
		requestedByPod: map[string]corev1.ResourceList{
			fmt.Sprintf("%s/%s", namespaceName1, podName1): {
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("250Mi"),
			},
			fmt.Sprintf("%s/%s", namespaceName2, podName2): {
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			fmt.Sprintf("%s/%s", namespaceName3, podName3): {
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("1000Mi"),
			},
		},
	}
)

// TestCalculateCosts tests the calculateCosts function.
func TestCalculateCosts(t *testing.T) {
	now := time.Now()

	testCases := []struct {
		name                 string
		nt                   *NodeTracker
		wantPerCPUCoreCost   float64
		wantPerGBMemoryCost  float64
		wantCostErrStrPrefix string
	}{
		{
			name: "one SKU only",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("5Gi"),
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				pricingProvider: &dummyPricingProvider{},
				costs:           &costInfo{},
			},
			wantPerCPUCoreCost:  0.2,
			wantPerGBMemoryCost: 0.2,
		},
		{
			name: "multiple SKUs",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName2: true,
					},
					nodeSKU2: {
						nodeName3: true,
					},
					nodeSKU3: {
						nodeName4: true,
					},
				},
				pricingProvider: &dummyPricingProvider{},
				costs:           &costInfo{},
			},
			wantPerCPUCoreCost:  1.416,
			wantPerGBMemoryCost: 0.708,
		},
		{
			name: "unsupported SKU",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName2: true,
					},
					nodeSKU2: {
						nodeName3: true,
					},
					nodeSKU4: {
						nodeName4: true,
					},
				},
				pricingProvider: &dummyPricingProvider{},
				costs:           &costInfo{},
			},
			wantPerCPUCoreCost:  0.583,
			wantPerGBMemoryCost: 0.292,
		},
		{
			name: "invalid CPU capacity (zero)",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				pricingProvider: &dummyPricingProvider{},
				costs:           &costInfo{},
			},
			wantCostErrStrPrefix: "failed to calculate costs",
		},
		{
			name: "invalid memory capacity (zero)",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("12"),
				},
				pricingProvider: &dummyPricingProvider{},
				costs:           &costInfo{},
			},
			wantCostErrStrPrefix: "failed to calculate costs",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			tc.nt.calculateCosts()

			if tc.wantCostErrStrPrefix != "" {
				if tc.nt.costs.err == nil {
					t.Errorf("calculateCosts() costErr = nil, want error with prefix %s", tc.wantCostErrStrPrefix)
				}
				if !strings.HasPrefix(tc.nt.costs.err.Error(), tc.wantCostErrStrPrefix) {
					t.Errorf("calculateCosts() costErr = %s, want error with prefix %s", tc.nt.costs.err.Error(), tc.wantCostErrStrPrefix)
				}
				return
			}

			if tc.nt.costs.err != nil {
				t.Errorf("calculateCosts() costErr = %s, want no error", tc.nt.costs.err.Error())
			}

			// Account for possible decision issues in float calculations.
			if !cmp.Equal(tc.nt.costs.perCPUCoreHourlyCost, tc.wantPerCPUCoreCost, cmpopts.EquateApprox(0.0, 0.01)) {
				t.Errorf("calculateCosts() perCPUCoreHourlyCost = %f, want %f", tc.nt.costs.perCPUCoreHourlyCost, tc.wantPerCPUCoreCost)
			}
			if !cmp.Equal(tc.nt.costs.perGBMemoryHourlyCost, tc.wantPerGBMemoryCost, cmpopts.EquateApprox(0.0, 0.01)) {
				t.Errorf("calculateCosts() perGBMemoryHourlyCost = %f, want %f", tc.nt.costs.perGBMemoryHourlyCost, tc.wantPerGBMemoryCost)
			}

			if !now.Before(tc.nt.costs.lastUpdated) {
				t.Errorf("calculateCosts() costLastUpdated = %s, want time before", tc.nt.costs.lastUpdated)
			}
		})
	}
}

// TestNodeTrackerTrackSKU tests the trackSKU method of the NodeTracker.
func TestNodeTrackerTrackSKU(t *testing.T) {
	testCases := []struct {
		name             string
		node             *corev1.Node
		nt               *NodeTracker
		wantNT           *NodeTracker
		wantSKUByNode    map[string]string
		wantNodeSetBySKU map[string]NodeSet
		wantRecalcNeeded bool
	}{
		{
			name: "new node with new SKU",
			nt: &NodeTracker{
				skuByNode:    map[string]string{},
				nodeSetBySKU: map[string]NodeSet{},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
					Labels: map[string]string{
						AKSClusterNodeSKULabelName: nodeSKU1,
					},
				},
			},
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
			wantRecalcNeeded: true,
		},
		{
			name: "new node with known SKU",
			nt: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName2,
					Labels: map[string]string{
						AKSClusterNodeSKULabelName: nodeSKU1,
					},
				},
			},
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName2: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName2: true,
					},
				},
			},
			wantRecalcNeeded: true,
		},
		{
			name: "SKU change",
			nt: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
					Labels: map[string]string{
						AKSClusterNodeSKULabelName: nodeSKU2,
					},
				},
			},
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU2,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU2: {
						nodeName1: true,
					},
				},
			},
			wantRecalcNeeded: true,
		},
		{
			name: "no SKU change",
			nt: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
					Labels: map[string]string{
						AKSClusterNodeSKULabelName: nodeSKU1,
					},
				},
			},
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			recalcNeeded := tc.nt.trackSKU(tc.node)
			if recalcNeeded != tc.wantRecalcNeeded {
				t.Errorf("trackSKU() = %v, want %v", recalcNeeded, tc.wantRecalcNeeded)
			}

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				ignoreNodeTrackerFields,
				cmp.AllowUnexported(NodeTracker{}),
			); diff != "" {
				t.Errorf("node tracker diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestNodeTrackerTrackAllocatableCapacity tests the trackAllocatableCapacity method of the NodeTracker.
func TestNodeTrackerTrackAllocatableCapacity(t *testing.T) {
	testCases := []struct {
		name   string
		node   *corev1.Node
		nt     *NodeTracker
		wantNT *NodeTracker
	}{
		{
			name: "new node with allocatable capacity for known resource types",
			nt: &NodeTracker{
				totalAllocatable:  make(corev1.ResourceList),
				allocatableByNode: make(map[string]corev1.ResourceList),
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
		},
		{
			// Normally this will never occur.
			name: "new node with total capacity for unsupported resource types",
			nt: &NodeTracker{
				totalAllocatable:  make(corev1.ResourceList),
				allocatableByNode: make(map[string]corev1.ResourceList),
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("30Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
		},
		{
			name: "node updated with no total capacity change",
			nt: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
		},
		{
			name: "node updated with total capacity change",
			nt: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
		{
			name: "additional node",
			nt: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName2,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			tc.nt.trackAllocatableCapacity(tc.node)

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				ignoreNodeTrackerFields,
				cmp.AllowUnexported(NodeTracker{}),
			); diff != "" {
				t.Errorf("node tracker diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestNodeTrackerTrackTotalCapacity tests the trackTotalCapacity method of the NodeTracker.
func TestNodeTrackerTrackTotalCapacity(t *testing.T) {
	testCases := []struct {
		name   string
		nt     *NodeTracker
		wantNT *NodeTracker
		node   *corev1.Node
	}{
		{
			name: "new node with total capacity for known resource types",
			nt: &NodeTracker{
				totalCapacity:  corev1.ResourceList{},
				capacityByNode: map[string]corev1.ResourceList{},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
		},
		{
			// Normally this will never occur.
			name: "new node with total capacity for unsupported resource types",
			nt: &NodeTracker{
				totalCapacity:  corev1.ResourceList{},
				capacityByNode: map[string]corev1.ResourceList{},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("200Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
		},
		{
			name: "node updated with total capacity change",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
		{
			name: "node updated with total capacity change",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName1,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
		},
		{
			name: "additional node",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName2,
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			tc.nt.trackTotalCapacity(tc.node)

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				ignoreNodeTrackerFields,
				cmp.AllowUnexported(NodeTracker{}),
			); diff != "" {
				t.Errorf("node tracker diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestNodeTrackerUntrackSKU tests the UntrackSKU method of the NodeTracker.
func TestNodeTrackerUntrackSKU(t *testing.T) {
	testCases := []struct {
		name     string
		nt       *NodeTracker
		nodeName string
		wantNT   *NodeTracker
	}{
		{
			name: "untrack a known node and its SKU",
			nt: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName2: nodeSKU1,
					nodeName3: nodeSKU3,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName2: true,
					},
					nodeSKU3: {
						nodeName3: true,
					},
				},
			},
			nodeName: nodeName1,
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName2: nodeSKU1,
					nodeName3: nodeSKU3,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName2: true,
					},
					nodeSKU3: {
						nodeName3: true,
					},
				},
			},
		},
		{
			name: "untrack a known node and its SKU (last one with such SKU)",
			nt: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName3: nodeSKU3,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
					nodeSKU3: {
						nodeName3: true,
					},
				},
			},
			nodeName: nodeName1,
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName3: nodeSKU3,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU3: {
						nodeName3: true,
					},
				},
			},
		},
		{
			name: "untrack an unknown node",
			nt: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
			nodeName: nodeName4,
			wantNT: &NodeTracker{
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			tc.nt.untrackSKU(tc.nodeName)

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				ignoreNodeTrackerFields,
				cmp.AllowUnexported(NodeTracker{}),
			); diff != "" {
				t.Errorf("node tracker diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestNodeTrackerUntrackAllocatableCapacity tests the UntrackAllocatableCapacity method of the NodeTracker.
func TestNodeTrackerUntrackAllocatableCapacity(t *testing.T) {
	testCases := []struct {
		name     string
		nt       *NodeTracker
		nodeName string
		wantNT   *NodeTracker
	}{
		{
			name: "untrack a known node",
			nt: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			nodeName: nodeName1,
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
		{
			name: "untrack an unknown node",
			nt: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			nodeName: nodeName3,
			wantNT: &NodeTracker{
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			tc.nt.untrackAllocatableCapacity(tc.nodeName)

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				ignoreNodeTrackerFields,
				cmp.AllowUnexported(NodeTracker{}),
			); diff != "" {
				t.Errorf("node tracker diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestNodeTrackerUntrackTotalCapacity tests the UntrackTotalCapacity method of the NodeTracker.
func TestNodeTrackerUntrackTotalCapacity(t *testing.T) {
	testCases := []struct {
		name     string
		nt       *NodeTracker
		nodeName string
		wantNT   *NodeTracker
	}{
		{
			name: "untrack a known node",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			nodeName: nodeName1,
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
		{
			name: "untrack an unknown node",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			nodeName: nodeName3,
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("24Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.nt.mu.Lock()
			defer tc.nt.mu.Unlock()

			tc.nt.untrackTotalCapacity(tc.nodeName)

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				ignoreNodeTrackerFields,
				cmp.AllowUnexported(NodeTracker{}),
			); diff != "" {
				t.Errorf("node tracker diff (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestNodeTrackerAddOrUpdateNode tests the AddOrUpdateNode method of the NodeTracker.
func TestNodeTrackerAddOrUpdateNode(t *testing.T) {
	testCases := []struct {
		name   string
		nt     *NodeTracker
		nodes  []*corev1.Node
		wantNT *NodeTracker
	}{
		{
			name: "can track a single node",
			nt:   NewNodeTracker(&dummyPricingProvider{}),
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("15Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("15Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("15Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					"Standard_1": {
						"node-1": true,
					},
				},
				skuByNode: map[string]string{
					"node-1": "Standard_1",
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.25,
					perGBMemoryHourlyCost: 0.0625,
				},
			},
		},
		{
			name: "can track multiple nodes",
			nt:   NewNodeTracker(&dummyPricingProvider{}),
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName2,
						Labels: map[string]string{
							AKSClusterNodeSKULabelName: nodeSKU2,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("12Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("10Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName3,
						Labels: map[string]string{
							AKSClusterNodeSKULabelName: nodeSKU3,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("15Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("9"),
					corev1.ResourceMemory: resource.MustParse("36Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("29Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("10Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("15Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
					nodeSKU2: {
						nodeName2: true,
					},
					nodeSKU3: {
						nodeName3: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName2: nodeSKU2,
					nodeName3: nodeSKU3,
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  1.777,
					perGBMemoryHourlyCost: 0.444,
				},
			},
		},
		{
			name: "can update existing node with no total/allocatable capacity change",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				pricingProvider: &dummyPricingProvider{},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.5,
					perGBMemoryHourlyCost: 0.125,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.5,
					perGBMemoryHourlyCost: 0.125,
				},
			},
		},
		{
			name: "can update existing node with total/allocatable capacity change",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				pricingProvider: &dummyPricingProvider{},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.5,
					perGBMemoryHourlyCost: 0.125,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
						Labels: map[string]string{
							AKSClusterNodeSKULabelName: nodeSKU1,
						},
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("12Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("11Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("11Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("11Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.333,
					perGBMemoryHourlyCost: 0.0833,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.nodes {
				tc.nt.AddOrUpdate(n)
			}

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				cmp.AllowUnexported(NodeTracker{}),
				ignoreNodeTrackerFields,
				ignoreCostInfoFields,
				cmp.AllowUnexported(costInfo{}),
				cmpopts.EquateApprox(0.0, 0.01),
				cmpopts.EquateErrors(),
			); diff != "" {
				t.Fatalf("AddOrUpdateNode(), node tracker diff (-got, +want): \n%s", diff)
			}
		})
	}
}

// TestNodeTrackerRemove tests the Remove method of the NodeTracker.
func TestNodeTrackerRemoveNode(t *testing.T) {
	testCases := []struct {
		name      string
		nt        *NodeTracker
		nodeNames []string
		wantNT    *NodeTracker
	}{
		{
			name: "can remove a tracked node",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("11"),
					corev1.ResourceMemory: resource.MustParse("30Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8.3"),
					corev1.ResourceMemory: resource.MustParse("25.8Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("5"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3.5"),
						corev1.ResourceMemory: resource.MustParse("12.5Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("11.5Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("1.8"),
						corev1.ResourceMemory: resource.MustParse("1.8Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName3: true,
					},
					nodeSKU2: {
						nodeName2: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName2: nodeSKU2,
					nodeName3: nodeSKU1,
				},
				pricingProvider: &dummyPricingProvider{},
				costs:           &costInfo{},
			},
			nodeNames: []string{nodeName2},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("18Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("5.3"),
					corev1.ResourceMemory: resource.MustParse("14.3Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("5"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3.5"),
						corev1.ResourceMemory: resource.MustParse("12.5Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("1.8"),
						corev1.ResourceMemory: resource.MustParse("1.8Gi"),
					},
				},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName3: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName3: nodeSKU1,
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.285,
					perGBMemoryHourlyCost: 0.111,
				},
			},
		},
		{
			name: "can remove multiple tracked nodes",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("11"),
					corev1.ResourceMemory: resource.MustParse("30Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8.3"),
					corev1.ResourceMemory: resource.MustParse("25.8Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("5"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3.5"),
						corev1.ResourceMemory: resource.MustParse("12.5Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("11.5Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("1.8"),
						corev1.ResourceMemory: resource.MustParse("1.8Gi"),
					},
				},
				pricingProvider: &dummyPricingProvider{},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
					nodeSKU2: {
						nodeName2: true,
					},
					nodeSKU3: {
						nodeName3: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName2: nodeSKU2,
					nodeName3: nodeSKU3,
				},
				costs: &costInfo{},
			},
			nodeNames: []string{
				nodeName1,
				nodeName2,
				nodeName3,
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				capacityByNode:    map[string]corev1.ResourceList{},
				allocatableByNode: map[string]corev1.ResourceList{},
				nodeSetBySKU:      map[string]NodeSet{},
				skuByNode:         map[string]string{},
				costs: &costInfo{
					err: cmpopts.AnyError,
				},
			},
		},
		{
			name: "can remove an untracked node",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				pricingProvider: &dummyPricingProvider{},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.5,
					perGBMemoryHourlyCost: 0.125,
				},
			},
			nodeNames: []string{nodeName2},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				pricingProvider: &dummyPricingProvider{},
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				costs: &costInfo{
					perCPUCoreHourlyCost:  0.5,
					perGBMemoryHourlyCost: 0.125,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range tc.nodeNames {
				tc.nt.Remove(name)
			}

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				cmp.AllowUnexported(NodeTracker{}),
				ignoreNodeTrackerFields,
				ignoreCostInfoFields,
				cmp.AllowUnexported(costInfo{}),
				cmpopts.EquateApprox(0.0, 0.01),
				cmpopts.EquateErrors(),
			); diff != "" {
				t.Fatalf("RemoveNode(), node tracker diff (-got, +want): \n%s", diff)
			}
		})
	}
}

// TestNodeTrackerNodeCount tests the NodeCount method of the NodeTracker.
func TestNodeTrackerNodeCount(t *testing.T) {
	testCases := []struct {
		name string
		nt   *NodeTracker
		want int
	}{
		{
			name: "can return the number of tracked nodes",
			nt:   nodeTrackerWith3Nodes,
			want: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.nt.NodeCount(); got != tc.want {
				t.Fatalf("NodeCount() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestNodeTrackerTotalCapacityFor tests the TotalCapacityFor method of the NodeTracker.
func TestNodeTrackerTotalCapacityFor(t *testing.T) {
	testCases := []struct {
		name string
		nt   *NodeTracker
		rn   corev1.ResourceName
		want resource.Quantity
	}{
		{
			name: "can return the total CPU capacity",
			nt:   nodeTrackerWith3Nodes,
			rn:   corev1.ResourceCPU,
			want: resource.MustParse("11"),
		},
		{
			name: "can return the total memory capacity",
			nt:   nodeTrackerWith3Nodes,
			rn:   corev1.ResourceMemory,
			want: resource.MustParse("30Gi"),
		},
		{
			name: "can return the total capacity for a non-supported resource",
			nt:   nodeTrackerWith3Nodes,
			rn:   corev1.ResourceStorage,
			want: resource.Quantity{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.nt.TotalCapacityFor(tc.rn); !got.Equal(tc.want) {
				t.Fatalf("TotalCapacity() = %s, want %s", got.String(), tc.want.String())
			}
		})
	}
}

// TestNodeTrackerAllocatableCapacityFor tests the TotalAllocatableFor method of the NodeTracker.
func TestNodeTrackerAllocatableCapacityFor(t *testing.T) {
	testCases := []struct {
		name string
		nt   *NodeTracker
		rn   corev1.ResourceName
		want resource.Quantity
	}{
		{
			name: "can return the allocatable CPU capacity",
			nt:   nodeTrackerWith3Nodes,
			rn:   corev1.ResourceCPU,
			want: resource.MustParse("8.3"),
		},
		{
			name: "can return the allocatable memory capacity",
			nt:   nodeTrackerWith3Nodes,
			rn:   corev1.ResourceMemory,
			want: resource.MustParse("25.8Gi"),
		},
		{
			name: "can return the allocatable capacity for a non-supported resource",
			nt:   nodeTrackerWith3Nodes,
			rn:   corev1.ResourceStorage,
			want: resource.Quantity{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.nt.TotalAllocatableFor(tc.rn); !got.Equal(tc.want) {
				t.Fatalf("TotalAllocatable() = %s, want %s", got.String(), tc.want.String())
			}
		})
	}
}

// TestNodeTrackerAddOrUpdate tests the AddOrUpdate method of the PodTracker.
func TestPodTrackerAddOrUpdate(t *testing.T) {
	testCases := []struct {
		name   string
		pt     *PodTracker
		pods   []*corev1.Pod
		wantPT *PodTracker
	}{
		{
			name: "can track a pod with a single container",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
		},
		{
			name: "can track a pod with multiple containers",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("500Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("700Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("700Mi"),
					},
				},
			},
		},
		{
			name: "can track multiple pods",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("50Mi"),
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
						Containers: []corev1.Container{
							{
								Name: containerName3,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
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
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1.5"),
										corev1.ResourceMemory: resource.MustParse("600Mi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2.5"),
										corev1.ResourceMemory: resource.MustParse("400Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: *resource.NewQuantity(2384461824, resource.BinarySI),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("250Mi"),
					},
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					fmt.Sprintf("%s/%s", namespaceName3, podName3): {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("1000Mi"),
					},
				},
			},
		},
		{
			name: "can update existing pod with requested capacity change",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("400Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("400Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("400Mi"),
					},
				},
			},
		},
		{
			name: "can update existing pod with no requested capacity change",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
		},
		{
			name: "can track a pod with no requested capacity for supported resources",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("800Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
		},
		{
			name: "can track a pod with containers that do not have resource requests",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
							{
								Name: containerName2,
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range tc.pods {
				tc.pt.AddOrUpdate(p)
			}

			if diff := cmp.Diff(
				tc.pt, tc.wantPT,
				cmp.AllowUnexported(PodTracker{}),
				ignorePodTrackerMutexField,
			); diff != "" {
				t.Fatalf("AddOrUpdatePod(), pod tracker diff (-got, +want): \n%s", diff)
			}
		})
	}
}

// TestPodTrackerRemove tests the Remove method of the PodTracker.
func TestPodTrackerRemove(t *testing.T) {
	testCases := []struct {
		name           string
		pt             *PodTracker
		podIdentifiers []string
		wantPT         *PodTracker
	}{
		{
			name: "can remove a tracked pod",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: *resource.NewQuantity(2384461824, resource.BinarySI),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("250Mi"),
					},
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					fmt.Sprintf("%s/%s", namespaceName3, podName3): {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("1000Mi"),
					},
				},
			},
			podIdentifiers: []string{
				fmt.Sprintf("%s/%s", namespaceName2, podName2),
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: *resource.NewQuantity(1310720000, resource.BinarySI),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("250Mi"),
					},
					fmt.Sprintf("%s/%s", namespaceName3, podName3): {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("1000Mi"),
					},
				},
			},
		},
		{
			name: "can remove multiple tracked pods",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: *resource.NewQuantity(2384461824, resource.BinarySI),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("250Mi"),
					},
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					fmt.Sprintf("%s/%s", namespaceName3, podName3): {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("1000Mi"),
					},
				},
			},
			podIdentifiers: []string{
				fmt.Sprintf("%s/%s", namespaceName1, podName1),
				fmt.Sprintf("%s/%s", namespaceName3, podName3),
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		},
		{
			name: "can remove an untracked pod",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
			podIdentifiers: []string{
				fmt.Sprintf("%s/%s", namespaceName1, podName1),
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		},
		{
			name: "can remove a tracked pod with no requested capacity for supported resources",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
			podIdentifiers: []string{
				fmt.Sprintf("%s/%s", namespaceName1, podName1),
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				requestedByPod: map[string]corev1.ResourceList{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, identifier := range tc.podIdentifiers {
				tc.pt.Remove(identifier)
			}

			if diff := cmp.Diff(
				tc.pt, tc.wantPT,
				cmp.AllowUnexported(PodTracker{}),
				ignorePodTrackerMutexField,
			); diff != "" {
				t.Fatalf("RemovePod(), pod tracker diff (-got, +want): \n%s", diff)
			}
		})
	}
}

// TestPodTotalRequestedFor tests the TotalRequestedFor method of the PodTracker.
func TestPodTotalRequestedFor(t *testing.T) {
	testCases := []struct {
		name string
		pt   *PodTracker
		rn   corev1.ResourceName
		want resource.Quantity
	}{
		{
			name: "can return the total requested CPU capacity",
			pt:   podTrackerWith3Pods,
			rn:   corev1.ResourceCPU,
			want: resource.MustParse("8"),
		},
		{
			name: "can return the total requested memory capacity",
			pt:   podTrackerWith3Pods,
			rn:   corev1.ResourceMemory,
			want: *resource.NewQuantity(2384461824, resource.BinarySI),
		},
		{
			name: "can return the total requested capacity for a non-supported resource",
			pt:   podTrackerWith3Pods,
			rn:   corev1.ResourceStorage,
			want: resource.Quantity{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pt.TotalRequestedFor(tc.rn); !got.Equal(tc.want) {
				t.Fatalf("TotalRequested() = %s, want %s", got.String(), tc.want.String())
			}
		})
	}
}
