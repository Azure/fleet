/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package trackers feature implementations that help track specific stats about
// Kubernetes resources, e.g., nodes and pods in the AKS property provider.
package trackers

import (
	"fmt"
	"math"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	// AKSClusterNodeSKULabelName is the node label added by AKS, which indicated the SKU
	// of the node.
	AKSClusterNodeSKULabelName = "beta.kubernetes.io/instance-type"
)

// supportedResourceNames is a list of resource names that the AKS property provider supports.
//
// Currently the supported resources are CPU and memory.
var supportedResourceNames []corev1.ResourceName = []corev1.ResourceName{
	corev1.ResourceCPU,
	corev1.ResourceMemory,
}

// NodeSet is a set of nodes.
type NodeSet map[string]bool

// costInfo is a struct that keeps cost related information.
type costInfo struct {
	// perCPUCoreHourlyCost and perGBMemoryHourlyCost are the average per CPU core and per GB memory
	// costs in the cluster, respectively.
	//
	// For reference,
	// per CPU core hourly cost = total hourly costs of all nodes / total CPU capacity; and
	// per GB of memory hourly cost = total hourly costs of all nodes / total memory capacity.
	perCPUCoreHourlyCost  float64
	perGBMemoryHourlyCost float64

	// lastUpdated is the timestamp when the per-resource-unit costs above are last calculated.
	lastUpdated time.Time
	// err tracks any error that occurs during cost calculation.
	err error
}

// NodeTracker helps track specific stats about nodes in a Kubernetes cluster, e.g., its count.
type NodeTracker struct {
	// totalCapacity and totalAllocatable are the total capacity and allocatable capacity
	// of the cluster, respectively.
	totalCapacity    corev1.ResourceList
	totalAllocatable corev1.ResourceList

	// costs tracks the cost-related information about the cluster.
	costs *costInfo

	// Below are a list of maps that tracks information about individual nodes in the cluster.
	capacityByNode    map[string]corev1.ResourceList
	allocatableByNode map[string]corev1.ResourceList
	nodeSetBySKU      map[string]NodeSet
	skuByNode         map[string]string

	// pricingProvider facilitates cost calculation.
	pricingProvider PricingProvider

	// mu is a RWMutex that protects the tracker against concurrent access.
	mu sync.RWMutex
}

// NewNodeTracker returns a node tracker.
func NewNodeTracker(pp PricingProvider) *NodeTracker {
	nt := &NodeTracker{
		totalCapacity:     make(corev1.ResourceList),
		totalAllocatable:  make(corev1.ResourceList),
		capacityByNode:    make(map[string]corev1.ResourceList),
		allocatableByNode: make(map[string]corev1.ResourceList),
		nodeSetBySKU:      make(map[string]NodeSet),
		skuByNode:         make(map[string]string),
		pricingProvider:   pp,
		costs: &costInfo{
			err: fmt.Errorf("costs have not been calculated yet"),
		},
	}

	for _, rn := range supportedResourceNames {
		nt.totalCapacity[rn] = resource.Quantity{}
		nt.totalAllocatable[rn] = resource.Quantity{}
	}

	return nt
}

// calculateCosts calculates the per CPU core and per GB memory cost in the cluster. This method
// is called every time a capacity or SKU change has been detected.
//
// At this moment the AKS property provider calculates costs using a simplified logic (average costs);
// it runs under the assumption that:
//
// a) all the nodes in the cluster are AKS on-demand nodes; discounts from spot instances,
// reserved instances, savings plans, and enterprise special pricing are unaccounted for
// at this moment.
//
// b) if a node is of an unrecognizable SKU, i.e., the SKU is absent from the Azure Retail Prices
// API reportings, the node is considered to be free of charge. This should be a very rare occurrence.
//
// Note that this method assumes that the access lock has been acquired.
func (nt *NodeTracker) calculateCosts() {
	totalCapacityCPU := nt.totalCapacity[corev1.ResourceCPU]
	totalCapacityMemory := nt.totalCapacity[corev1.ResourceMemory]

	// Sum up the total costs.
	totalHourlyRate := 0.0
	for sku, ns := range nt.nodeSetBySKU {
		hourlyRate, found := nt.pricingProvider.OnDemandPrice(sku)
		if !found {
			// The SKU is not found in the pricing data.
			continue
		}
		totalHourlyRate += hourlyRate * float64(len(ns))
		klog.V(4).InfoS("Tallying total hourly rate of the cluster", "sku", sku, "hourlyRate", hourlyRate, "nodeCount", len(ns))
	}
	// TO-DO (chenyu1): add a cap on the total hourly rate to ensure safe division.

	// Calculate the per CPU core and per GB memory costs.
	ci := nt.costs

	// Cast the CPU resource quantity into a float64 value. Precision might suffer a bit of loss,
	// but it should be mostly acceptable in the case of cost calculation.
	//
	// Note that the minimum CPU resource quantity Kubernetes allows is one millicore; internally
	// the quantity is stored in the unit of cores (1000 millicores).
	cpuCores := totalCapacityCPU.AsApproximateFloat64()
	if math.IsInf(cpuCores, 0) || cpuCores <= 0.001 {
		// Report an error if the total CPU resource quantity is of an invalid value.
		//
		// This will stop all reportings of cost related properties until the issue is resolved.
		costErr := fmt.Errorf("failed to calculate costs: cpu quantity is of an invalid value: %v", cpuCores)
		klog.Error(costErr)
		ci.err = costErr

		// Reset the cost data.
		ci.perCPUCoreHourlyCost = 0.0
		ci.perGBMemoryHourlyCost = 0.0
		return
	}
	ci.perCPUCoreHourlyCost = totalHourlyRate / cpuCores
	klog.V(4).InfoS("Calculated per CPU core hourly cost", "perCPUCoreHourlyCost", ci.perCPUCoreHourlyCost)

	// Cast the memory resource quantitu into a float64 value. Precision might suffer a bit of
	// loss, but it should be mostly acceptable in the case of cost calculation.
	//
	// Note that the minimum memory resource quantity Kubernetes allows is one byte.
	memoryBytes := totalCapacityMemory.AsApproximateFloat64()
	if math.IsInf(memoryBytes, 0) || memoryBytes <= 1 {
		// Report an error if the total memory resource quantity is of an invalid value.
		//
		// This will stop all reportings of cost related properties until the issue is resolved.
		costErr := fmt.Errorf("failed to calculate costs: memory quantity is of an invalid value: %v", memoryBytes)
		klog.Error(costErr)
		ci.err = costErr

		// Reset the cost data.
		ci.perCPUCoreHourlyCost = 0.0
		ci.perGBMemoryHourlyCost = 0.0
		return
	}
	ci.perGBMemoryHourlyCost = totalHourlyRate / (memoryBytes / (1024.0 * 1024.0 * 1024.0))
	klog.V(4).InfoS("Calculated per GB memory hourly cost", "perGBMemoryHourlyCost", ci.perGBMemoryHourlyCost)

	ci.lastUpdated = time.Now()
	ci.err = nil
}

// trackSKU tracks the SKU of a node. It returns true if a recalculation of costs is needed.
//
// Note that this method assumes that the access lock has been acquired.
func (nt *NodeTracker) trackSKU(node *corev1.Node) bool {
	sku := node.Labels[AKSClusterNodeSKULabelName]
	registeredSKU, found := nt.skuByNode[node.Name]

	switch {
	case !found:
		// The node's SKU has not been tracked.
		nt.skuByNode[node.Name] = sku
		ns := nt.nodeSetBySKU[sku]
		if ns == nil {
			ns = make(NodeSet)
		}
		ns[node.Name] = true
		nt.nodeSetBySKU[sku] = ns
		klog.V(4).InfoS("The node's SKU has not been tracked", "sku", sku, "node", klog.KObj(node))
		return true
	case registeredSKU != sku:
		// The node's SKU has changed.
		//
		// Normally this will never happen.

		// Untrack the old SKU.
		nt.skuByNode[node.Name] = sku
		delete(nt.nodeSetBySKU[registeredSKU], node.Name)
		if len(nt.nodeSetBySKU[registeredSKU]) == 0 {
			delete(nt.nodeSetBySKU, registeredSKU)
		}
		ns := nt.nodeSetBySKU[sku]
		if ns == nil {
			ns = make(NodeSet)
		}
		ns[node.Name] = true
		nt.nodeSetBySKU[sku] = ns
		klog.V(4).InfoS("The node's SKU has changed", "oldSKU", registeredSKU, "newSKU", sku, "node", klog.KObj(node))
		return true
	default:
		// No further action is needed if the node's SKU remains the same.
		klog.V(4).InfoS("The node's SKU has not changed", "sku", sku, "node", klog.KObj(node))
		return false
	}
}

// trackAllocatableCapacity tracks the allocatable capacity of a node.
//
// Note that this method assumes that the access lock has been acquired.
func (nt *NodeTracker) trackAllocatableCapacity(node *corev1.Node) {
	ra, ok := nt.allocatableByNode[node.Name]
	if ok {
		// The node's allocatable capacity has been tracked.
		//
		// Typically, a node's allocatable capacity is immutable after the node
		// is created; here, the provider still performs a sanity check to avoid
		// any inconsistencies.
		klog.V(4).InfoS("Node's allocatable capacity has been tracked", "node", klog.KObj(node))
		for _, rn := range supportedResourceNames {
			c1 := ra[rn]
			c2 := node.Status.Allocatable[rn]
			if !c1.Equal(c2) {
				// The reported allocatable capacity has changed.

				// Update the allocatable capacity of the cluster.
				ta := nt.totalAllocatable[rn]
				ta.Sub(c1)
				ta.Add(c2)
				nt.totalAllocatable[rn] = ta

				// Update the tracked total capacity of the node.
				ra[rn] = c2
			}
			klog.V(4).InfoS("Found an allocatable capacity change", "resource", rn, "node", klog.KObj(node), "oldCapacity", c1, "newCapacity", c2)
		}
	} else {
		ra = make(corev1.ResourceList)

		// The node's allocatable capacity has not been tracked.
		for _, rn := range supportedResourceNames {
			a := node.Status.Allocatable[rn]
			ra[rn] = a

			ta := nt.totalAllocatable[rn]
			ta.Add(a)
			nt.totalAllocatable[rn] = ta
			klog.V(4).InfoS("Added allocatable capacity", "resource", rn, "node", klog.KObj(node), "capacity", a)
		}

		nt.allocatableByNode[node.Name] = ra
	}
}

// trackTotalCapacity tracks the total capacity of a node. It returns true if a
// recalculation of costs is needed.
//
// Note that this method assumes that the access lock has been acquired.
func (nt *NodeTracker) trackTotalCapacity(node *corev1.Node) bool {
	rc, ok := nt.capacityByNode[node.Name]
	isCapacityChanged := false
	if ok {
		// The node's total capacity has been tracked.
		//
		// Typically, a node's total capacity is immutable after the node
		// is created; here, the provider still performs a sanity check to avoid
		// any inconsistencies.
		klog.V(4).InfoS("Node's total capacity has been tracked", "node", klog.KObj(node))
		for _, rn := range supportedResourceNames {
			c1 := rc[rn]
			c2 := node.Status.Capacity[rn]
			if !c1.Equal(c2) {
				// The reported total capacity has changed.

				// Update the total capacity of the cluster.
				tc := nt.totalCapacity[rn]
				tc.Sub(c1)
				tc.Add(c2)
				nt.totalCapacity[rn] = tc

				// Update the tracked total capacity of the node.
				rc[rn] = c2

				isCapacityChanged = true
				klog.V(4).InfoS("Found a total capacity change", "resource", rn, "node", klog.KObj(node), "oldCapacity", c1, "newCapacity", c2)
			}
		}
	} else {
		// The node's total capacity has not been tracked.
		klog.V(4).InfoS("Node's total capacity has not been tracked yet", "node", klog.KObj(node))
		rc = make(corev1.ResourceList)

		for _, rn := range supportedResourceNames {
			c := node.Status.Capacity[rn]
			rc[rn] = c

			tc := nt.totalCapacity[rn]
			tc.Add(c)
			nt.totalCapacity[rn] = tc
			klog.V(4).InfoS("Added total capacity", "resource", rn, "node", klog.KObj(node), "capacity", c)
		}

		nt.capacityByNode[node.Name] = rc

		isCapacityChanged = true
	}
	return isCapacityChanged
}

// AddOrUpdate starts tracking a node or updates the stats about a node that has been
// tracked.
func (nt *NodeTracker) AddOrUpdate(node *corev1.Node) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	// Track the total capacity of the node.
	isCapacityChanged := nt.trackTotalCapacity(node)
	// Track the allocatable capacity of the node.
	nt.trackAllocatableCapacity(node)
	// Track the SKU of the node.
	isSKUChanged := nt.trackSKU(node)

	if isCapacityChanged || isSKUChanged {
		// Only re-calculate cost information if the capacity or the SKU of any node has changed.
		nt.calculateCosts()
	}
}

// untrackSKU untracks the SKU of a node.
//
// Note that this method assumes that the access lock has been acquired.
func (nt *NodeTracker) untrackSKU(nodeName string) {
	sku, found := nt.skuByNode[nodeName]
	if found {
		delete(nt.skuByNode, nodeName)
		delete(nt.nodeSetBySKU[sku], nodeName)
		if len(nt.nodeSetBySKU[sku]) == 0 {
			delete(nt.nodeSetBySKU, sku)
		}
		klog.V(4).InfoS("Untracked the node's SKU", "sku", sku, "node", nodeName)
	}
}

// untrackTotalCapacity untracks the total capacity of a node.
//
// Note that this method assumes that the access lock has been acquired.
func (nt *NodeTracker) untrackTotalCapacity(nodeName string) {
	rc, ok := nt.capacityByNode[nodeName]
	if ok {
		for _, rn := range supportedResourceNames {
			c := rc[rn]
			tc := nt.totalCapacity[rn]
			tc.Sub(c)
			nt.totalCapacity[rn] = tc
		}
		delete(nt.capacityByNode, nodeName)
		klog.V(4).InfoS("Untracked the node's total capacity", "node", nodeName)
	}
}

// untrackAllocatableCapacity untracks the allocatable capacity of a node.
func (nt *NodeTracker) untrackAllocatableCapacity(nodeName string) {
	ra, ok := nt.allocatableByNode[nodeName]
	if ok {
		for _, rn := range supportedResourceNames {
			a := ra[rn]
			ta := nt.totalAllocatable[rn]
			ta.Sub(a)
			nt.totalAllocatable[rn] = ta
		}

		delete(nt.allocatableByNode, nodeName)
		klog.V(4).InfoS("Untracked the node's allocatable capacity", "node", nodeName)
	}
}

// Remove stops tracking a node.
func (nt *NodeTracker) Remove(nodeName string) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	// Untrack the total and allocatable capacity of the node.
	nt.untrackTotalCapacity(nodeName)
	nt.untrackAllocatableCapacity(nodeName)

	// Untrack the node's SKU.
	nt.untrackSKU(nodeName)

	// Re-calculate costs.
	//
	// Note that it would be very rare for the informer to receive a node deletion event before
	// having a chance to track it first.
	nt.calculateCosts()
}

// NodeCount returns the node count stat that a node tracker tracks.
func (nt *NodeTracker) NodeCount() int {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	// capacityByNode and allocatableByNode should always have the same length.
	return len(nt.allocatableByNode)
}

// TotalCapacityFor returns the total capacity of a specific resource that the node
// tracker tracks.
func (nt *NodeTracker) TotalCapacityFor(rn corev1.ResourceName) resource.Quantity {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	return nt.totalCapacity[rn]
}

// TotalAllocatableFor returns the total allocatable capacity of a specific resource that the node
// tracker tracks.
func (nt *NodeTracker) TotalAllocatableFor(rn corev1.ResourceName) resource.Quantity {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	return nt.totalAllocatable[rn]
}

// TotalCapacity returns the total capacity of all resources that the node tracker tracks.
func (nt *NodeTracker) TotalCapacity() corev1.ResourceList {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	// Return a deep copy to avoid leaks and consequent potential data race.
	return nt.totalCapacity.DeepCopy()
}

// TotalAllocatable returns the total allocatable capacity of all resources that
// the node tracker tracks.
func (nt *NodeTracker) TotalAllocatable() corev1.ResourceList {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	// Return a deep copy to avoid leaks and consequent potential data race.
	return nt.totalAllocatable.DeepCopy()
}

// Costs returns the per CPU core and per GB memory costs in the cluster.
func (nt *NodeTracker) Costs() (perCPUCoreCost, perGBMemoryCost float64, err error) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if nt.costs.lastUpdated.Before(nt.pricingProvider.LastUpdated()) {
		nt.calculateCosts()
	}
	return nt.costs.perCPUCoreHourlyCost, nt.costs.perGBMemoryHourlyCost, nt.costs.err
}
