/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package trackers feature implementations that help track specific stats about
// Kubernetes resources, e.g., nodes and pods in the AKS metric provider.
package trackers

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// supportedResourceNames is a list of resource names that the AKS metric provider supports.
var supportedResourceNames []corev1.ResourceName = []corev1.ResourceName{
	corev1.ResourceCPU,
	corev1.ResourceMemory,
}

// NodeTracker helps track specific stats about nodes in a Kubernetes cluster, e.g., its count.
type NodeTracker struct {
	totalCapacity    corev1.ResourceList
	totalAllocatable corev1.ResourceList

	capacityByNode    map[string]corev1.ResourceList
	allocatableByNode map[string]corev1.ResourceList

	// mu is a RWMutex that protects the tracker against concurrent access.
	mu sync.RWMutex
}

// NewNodeTracker returns a node tracker.
func NewNodeTracker() *NodeTracker {
	nt := &NodeTracker{
		totalCapacity:     make(corev1.ResourceList),
		totalAllocatable:  make(corev1.ResourceList),
		capacityByNode:    make(map[string]corev1.ResourceList),
		allocatableByNode: make(map[string]corev1.ResourceList),
	}

	for _, rn := range supportedResourceNames {
		nt.totalCapacity[rn] = resource.Quantity{}
		nt.totalAllocatable[rn] = resource.Quantity{}
	}

	return nt
}

// AddOrUpdate starts tracking a node or updates the stats about a node that has been
// tracked.
func (nt *NodeTracker) AddOrUpdate(node *corev1.Node) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	rc, ok := nt.capacityByNode[node.Name]
	if ok {
		// The node's total capacity has been tracked.
		//
		// Typically, a node's total capacity is immutable after the node
		// is created; here, the provider still performs a sanity check to avoid
		// any inconsistencies.
		for _, rn := range supportedResourceNames {
			c1 := rc[rn]
			c2 := node.Status.Capacity[corev1.ResourceName(rn)]
			if !c1.Equal(c2) {
				// The reported total capacity has changed.

				// Update the total capacity of the cluster.
				tc := nt.totalCapacity[rn]
				tc.Sub(c1)
				tc.Add(c2)
				nt.totalCapacity[rn] = tc

				// Update the tracked total capacity of the node.
				rc[rn] = c2
			}
		}
	} else {
		rc = make(corev1.ResourceList)

		// The node's total capacity has not been tracked.
		for _, rn := range supportedResourceNames {
			c := node.Status.Capacity[corev1.ResourceName(rn)]
			rc[rn] = c

			tc := nt.totalCapacity[rn]
			tc.Add(c)
			nt.totalCapacity[rn] = tc
		}

		nt.capacityByNode[node.Name] = rc
	}

	ra, ok := nt.allocatableByNode[node.Name]
	if ok {
		// The node's allocatable capacity has been tracked.
		//
		// Typically, a node's allocatable capacity is immutable after the node
		// is created; here, the provider still performs a sanity check to avoid
		// any inconsistencies.
		for _, rn := range supportedResourceNames {
			c1 := ra[rn]
			c2 := node.Status.Allocatable[corev1.ResourceName(rn)]
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
			a := node.Status.Allocatable[corev1.ResourceName(rn)]
			ra[rn] = a

			ta := nt.totalAllocatable[rn]
			ta.Add(a)
			nt.totalAllocatable[rn] = ta
		}

		nt.allocatableByNode[node.Name] = ra
	}
}

// Remove stops tracking a node.
func (nt *NodeTracker) Remove(nodeName string) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	rc, ok := nt.capacityByNode[nodeName]
	if ok {
		// Untrack the node's total capacity.
		for _, rn := range supportedResourceNames {
			c := rc[rn]
			tc := nt.totalCapacity[rn]
			tc.Sub(c)
			nt.totalCapacity[rn] = tc
		}

		delete(nt.capacityByNode, nodeName)
	}

	ra, ok := nt.allocatableByNode[nodeName]
	if ok {
		// Untrack the node's allocatable capacity.
		for _, rn := range supportedResourceNames {
			a := ra[rn]
			ta := nt.totalAllocatable[rn]
			ta.Sub(a)
			nt.totalAllocatable[rn] = ta
		}

		delete(nt.allocatableByNode, nodeName)
	}
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
			r.Add(container.Resources.Requests[corev1.ResourceName(rn)])
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
