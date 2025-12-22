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

package queue

import (
	"time"

	"k8s.io/client-go/util/workqueue"
)

// simplePlacementSchedulingQueue is a simple implementation of
// PlacementSchedulingQueue.
//
// This implementation is essentially a thin wrapper around one rate limiting
// workqueue, which queues all placement keys indiscriminately for processing.
type simplePlacementSchedulingQueue struct {
	active workqueue.TypedRateLimitingInterface[any]
}

// Verify that simplePlacementSchedulingQueue implements
// PlacementSchedulingQueue at compile time.
var _ PlacementSchedulingQueue = &simplePlacementSchedulingQueue{}

// simplePlacementSchedulingQueueOptions are the options for the
// simplePlacementSchedulingQueue.
type simplePlacementSchedulingQueueOptions struct {
	rateLimiter workqueue.TypedRateLimiter[any]
	name        string
}

var defaultSimplePlacementSchedulingQueueOptions = simplePlacementSchedulingQueueOptions{
	rateLimiter: workqueue.DefaultTypedControllerRateLimiter[any](),
	name:        "simplePlacementSchedulingQueue",
}

// Run starts the scheduling queue.
//
// At this moment, Run is an no-op as there is only one queue present; in the future,
// when more queues are added, Run would start goroutines that move items between queues as
// appropriate.
func (sq *simplePlacementSchedulingQueue) Run() {}

// Close shuts down the scheduling queue immediately.
func (sq *simplePlacementSchedulingQueue) Close() {
	sq.active.ShutDown()
}

// CloseWithDrain shuts down the scheduling queue and returns until all items are processed.
func (sq *simplePlacementSchedulingQueue) CloseWithDrain() {
	sq.active.ShutDownWithDrain()
}

// NextPlacementKey returns the next PlacementKey (either clusterResourcePlacementKey or resourcePlacementKey)
// in the work queue for the scheduler to process.
//
// Note that for now the queue simply wraps a work queue, and consider its state (whether it
// is shut down or not) as its own closedness. In the future, when more queues are added, the
// queue implementation must manage its own state.
func (sq *simplePlacementSchedulingQueue) NextPlacementKey() (key PlacementKey, closed bool) {
	// This will block on a condition variable if the queue is empty.
	placementKey, shutdown := sq.active.Get()
	if shutdown {
		return "", true
	}
	return placementKey.(PlacementKey), false
}

// Done marks a PlacementKey as done.
func (sq *simplePlacementSchedulingQueue) Done(placementKey PlacementKey) {
	sq.active.Done(placementKey)
}

// Add adds a PlacementKey to the work queue.
//
// Note that this bypasses the rate limiter (if any).
func (sq *simplePlacementSchedulingQueue) Add(placementKey PlacementKey) {
	sq.active.Add(placementKey)
}

// AddRateLimited adds a PlacementKey to the work queue after the rate limiter (if any)
// says that it is OK.
func (sq *simplePlacementSchedulingQueue) AddRateLimited(placementKey PlacementKey) {
	sq.active.AddRateLimited(placementKey)
}

// AddAfter adds a PlacementKey to the work queue after a set duration.
//
// Note that this bypasses the rate limiter (if any).
func (sq *simplePlacementSchedulingQueue) AddAfter(placementKey PlacementKey, duration time.Duration) {
	sq.active.AddAfter(placementKey, duration)
}

// AddBatched tracks a PlacementKey and adds such keys in batch later to the work queue when appropriate.
//
// For the simple queue implementation, this is equivalent to Add.
func (sq *simplePlacementSchedulingQueue) AddBatched(placementKey PlacementKey) {
	sq.active.Add(placementKey)
}

// Forget untracks a PlacementKey from rate limiter(s) (if any) set up with the queue.
func (sq *simplePlacementSchedulingQueue) Forget(placementKey PlacementKey) {
	sq.active.Forget(placementKey)
}

// NewSimplePlacementSchedulingQueue returns a simplePlacementSchedulingQueue.
func NewSimplePlacementSchedulingQueue(name string, rateLimiter workqueue.TypedRateLimiter[any]) PlacementSchedulingQueue {
	if len(name) == 0 {
		name = defaultSimplePlacementSchedulingQueueOptions.name
	}
	if rateLimiter == nil {
		rateLimiter = defaultSimplePlacementSchedulingQueueOptions.rateLimiter
	}

	return &simplePlacementSchedulingQueue{
		active: workqueue.NewTypedRateLimitingQueueWithConfig(rateLimiter, workqueue.TypedRateLimitingQueueConfig[any]{
			Name: name,
		}),
	}
}
