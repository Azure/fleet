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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// PodTracker helps track specific stats about pods in a Kubernetes cluster, e.g., the sum
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

	// Return a deep copy to avoid leaks and consequent potential data race.
	return pt.totalRequested.DeepCopy()
}
