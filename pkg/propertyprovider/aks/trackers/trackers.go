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

// NodeTracker helps track specific stats about nodes in a Kubernetes cluster, e.g., its count.
type NodeTracker struct {
	// totalCapacity and totalAllocatable are the total capacity and allocatable capacity
	// of the cluster, respectively.
	totalCapacity    corev1.ResourceList
	totalAllocatable corev1.ResourceList
	// perCPUCoreHourlyCost and perGBMemoryHourlyCost are the average per CPU core and per GB memory
	// costs in the cluster, respectively.
	perCPUCoreHourlyCost  float64
	perGBMemoryHourlyCost float64

	// Below are a list of maps that tracks information about individual nodes in the cluster.
	capacityByNode    map[string]corev1.ResourceList
	allocatableByNode map[string]corev1.ResourceList
	nodeSetBySKU      map[string]NodeSet
	skuByNode         map[string]string

	// costLastUpdated is the timestamp when the per-resource-unit costs are last calculated.
	costLastUpdated time.Time
	// costErr tracks any error that occurs during cost calculation.
	costErr error

	// pricingProvider facilitates cost calculation.
	pricingProvider PricingProvider

	// mu is a RWMutex that protects the tracker against concurrent access.
	mu sync.RWMutex
}

// NewNodeTracker returns a node tracker.
func NewNodeTracker(pricingProvider PricingProvider) *NodeTracker {
	nt := &NodeTracker{
		totalCapacity:     make(corev1.ResourceList),
		totalAllocatable:  make(corev1.ResourceList),
		capacityByNode:    make(map[string]corev1.ResourceList),
		allocatableByNode: make(map[string]corev1.ResourceList),
		nodeSetBySKU:      make(map[string]NodeSet),
		skuByNode:         make(map[string]string),
		pricingProvider:   pricingProvider,
		costErr:           fmt.Errorf("costs have not been calculated yet"),
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
	}
	// TO-DO (chenyu1): add a cap on the total hourly rate to ensure safe division.

	// Calculate the per CPU core and per GB memory costs.

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
		nt.costErr = fmt.Errorf("failed to calculate costs: cpu quantity is of an invalid value: %v", cpuCores)

		// Reset the cost data.
		nt.perCPUCoreHourlyCost = 0.0
		nt.perGBMemoryHourlyCost = 0.0
		return
	}
	nt.perCPUCoreHourlyCost = totalHourlyRate / cpuCores

	// Cast the memory resource quantitu into a float64 value. Precision might suffer a bit of
	// loss, but it should be mostly acceptable in the case of cost calculation.
	//
	// Note that the minimum memory resource quantity Kubernetes allows is one byte.
	memoryBytes := totalCapacityMemory.AsApproximateFloat64()
	if math.IsInf(memoryBytes, 0) || memoryBytes <= 1 {
		// Report an error if the total memory resource quantity is of an invalid value.
		//
		// This will stop all reportings of cost related properties until the issue is resolved.
		nt.costErr = fmt.Errorf("failed to calculate costs: memory quantity is of an invalid value: %v", memoryBytes)

		// Reset the cost data.
		nt.perCPUCoreHourlyCost = 0.0
		nt.perGBMemoryHourlyCost = 0.0
		return
	}
	nt.perGBMemoryHourlyCost = totalHourlyRate / (memoryBytes / (1024.0 * 1024.0 * 1024.0))
	nt.costLastUpdated = time.Now()
	nt.costErr = nil
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
			ns[node.Name] = true
		}
		nt.nodeSetBySKU[sku] = ns
		return true
	case registeredSKU != sku:
		// The node's SKU has changed.
		//
		// Normally this will never happen.

		// Untrack the old SKU.
		nt.skuByNode[node.Name] = sku
		delete(nt.nodeSetBySKU[registeredSKU], node.Name)
		ns := nt.nodeSetBySKU[sku]
		if ns == nil {
			ns = make(NodeSet)
			ns[node.Name] = true
		}
		nt.nodeSetBySKU[sku] = ns
		return true
	default:
		// No further action is needed if the node's SKU and spot instance status remain the same.
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
			}
		}
	} else {
		// The node's total capacity has not been tracked.
		rc = make(corev1.ResourceList)

		for _, rn := range supportedResourceNames {
			c := node.Status.Capacity[rn]
			rc[rn] = c

			tc := nt.totalCapacity[rn]
			tc.Add(c)
			nt.totalCapacity[rn] = tc
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

	return nt.totalCapacity
}

// TotalAllocatable returns the total allocatable capacity of all resources that
// the node tracker tracks.
func (nt *NodeTracker) TotalAllocatable() corev1.ResourceList {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	return nt.totalAllocatable
}

// Costs returns the per CPU core and per GB memory costs in the cluster.
func (nt *NodeTracker) Costs() (perCPUCoreCost, perGBMemoryCost float64, err error) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if nt.costLastUpdated.Before(nt.pricingProvider.LastUpdated()) {
		nt.calculateCosts()
	}
	return nt.perCPUCoreHourlyCost, nt.perGBMemoryHourlyCost, nt.costErr
}

// NodeTracker helps track specific stats about nodes in a Kubernetes cluster, e.g., the sum
// of its requested resources.
type PodTracker struct {
	totalRequested corev1.ResourceList

	requestedByPod map[string]corev1.ResourceList

	// mu is a RWMutex that protects the tracker against concurrent access.
	mu sync.RWMutex
}

// NewPodTracker returns a pod tracker.
func NewPodTracker() *PodTracker {
	pt := &PodTracker{
		totalRequested: make(corev1.ResourceList),
		requestedByPod: make(map[string]corev1.ResourceList),
	}

	for _, rn := range supportedResourceNames {
		pt.totalRequested[rn] = resource.Quantity{}
	}

	return pt
}

// AddOrUpdate starts tracking a pod or updates the stats about a pod that has been
// tracked.
func (pt *PodTracker) AddOrUpdate(pod *corev1.Pod) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	requestsAcrossAllContainers := make(corev1.ResourceList)
	for _, container := range pod.Spec.Containers {
		for _, rn := range supportedResourceNames {
			r := requestsAcrossAllContainers[rn]
			r.Add(container.Resources.Requests[rn])
			requestsAcrossAllContainers[rn] = r
		}
	}

	podIdentifier := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	rp, ok := pt.requestedByPod[podIdentifier]
	if ok {
		// The pod's requested resources have been tracked.
		//
		// At this moment, a pod's requested resources are immutable after the pod
		// is created; in-place vertical scaling is not yet possible. However, the provider
		// here still performs a sanity check to avoid any inconsistencies.
		for _, rn := range supportedResourceNames {
			r1 := rp[rn]
			r2 := requestsAcrossAllContainers[rn]
			if !r1.Equal(r2) {
				// The reported requested resources have changed.

				// Update the tracked total requested resources.
				tr := pt.totalRequested[rn]
				tr.Sub(r1)
				tr.Add(r2)
				pt.totalRequested[rn] = tr

				// Update the tracked requested resources of the pod.
				rp[rn] = r2
			}
		}
	} else {
		rp = make(corev1.ResourceList)

		// The pod's requested resources have not been tracked.
		for _, rn := range supportedResourceNames {
			r := requestsAcrossAllContainers[rn]
			rp[rn] = r

			tr := pt.totalRequested[rn]
			tr.Add(r)
			pt.totalRequested[rn] = tr
		}

		pt.requestedByPod[podIdentifier] = rp
	}
}

// Remove stops tracking a pod.
func (pt *PodTracker) Remove(podIdentifier string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	rp, ok := pt.requestedByPod[podIdentifier]
	if ok {
		// Untrack the pod's requested resources.
		for _, rn := range supportedResourceNames {
			r := rp[rn]
			tr := pt.totalRequested[rn]
			tr.Sub(r)
			pt.totalRequested[rn] = tr
		}

		delete(pt.requestedByPod, podIdentifier)
	}
}

// TotalRequestedFor returns the total requested resources of a specific resource that the pod
// tracker tracks.
func (pt *PodTracker) TotalRequestedFor(rn corev1.ResourceName) resource.Quantity {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	return pt.totalRequested[rn]
}

// TotalRequested returns the total requested resources of all resources that the pod tracker
// tracks.
func (pt *PodTracker) TotalRequested() corev1.ResourceList {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	return pt.totalRequested
}
