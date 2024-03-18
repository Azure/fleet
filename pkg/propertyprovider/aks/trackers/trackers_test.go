/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trackers

import (
	"fmt"
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
	ignoreNodeTrackerFields    = cmpopts.IgnoreFields(NodeTracker{}, "mu", "pricingProvider", "costLastUpdated")
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
				perCPUCoreHourlyCost:  0.25,
				perGBMemoryHourlyCost: 0.0625,
				nodeSetBySKU: map[string]NodeSet{
					"Standard_1": {
						"node-1": true,
					},
				},
				skuByNode: map[string]string{
					"node-1": "Standard_1",
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
				perCPUCoreHourlyCost:  1.777,
				perGBMemoryHourlyCost: 0.444,
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
				perCPUCoreHourlyCost:  0.5,
				perGBMemoryHourlyCost: 0.125,
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				pricingProvider: &dummyPricingProvider{},
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
				perCPUCoreHourlyCost:  0.5,
				perGBMemoryHourlyCost: 0.125,
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
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
				perCPUCoreHourlyCost:  0.5,
				perGBMemoryHourlyCost: 0.125,
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
				},
				pricingProvider: &dummyPricingProvider{},
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
				perCPUCoreHourlyCost:  0.333,
				perGBMemoryHourlyCost: 0.0833,
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
					},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
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
				perCPUCoreHourlyCost:  0.285,
				perGBMemoryHourlyCost: 0.111,
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {
						nodeName1: true,
						nodeName3: true,
					},
					nodeSKU2: {},
				},
				skuByNode: map[string]string{
					nodeName1: nodeSKU1,
					nodeName3: nodeSKU1,
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
				costErr:           cmpopts.AnyError,
				nodeSetBySKU: map[string]NodeSet{
					nodeSKU1: {},
					nodeSKU2: {},
					nodeSKU3: {},
				},
				skuByNode: map[string]string{},
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
				perCPUCoreHourlyCost:  0.5,
				perGBMemoryHourlyCost: 0.125,
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
				perCPUCoreHourlyCost:  0.5,
				perGBMemoryHourlyCost: 0.125,
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
