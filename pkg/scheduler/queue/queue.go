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

// Package queue features a scheduling queue, which keeps track of all placements for the scheduler
// to schedule.
package queue

import (
	"time"

	"k8s.io/client-go/util/workqueue"
)

// PlacementKey is the unique identifier for a Placement checked into a scheduling queue.
// If the Placement is a ClusterResourcePlacement, the PlacementKey is the
// ClusterResourcePlacement's name in the form of "name".
// If the Placement is a NamespaceResourcePlacement, the PlacementKey is the
// NamespaceResourcePlacement's name in the form of "namespace/name".
// TODO: rename this to be ObjectKey or something similar, as it is not specific to Placement.
type PlacementKey string

// PlacementSchedulingQueueWriter is an interface which allows sources, such as controllers, to add
// PlacementKeys to the scheduling queue.
type PlacementSchedulingQueueWriter interface {
	// Add adds a PlacementKey to the work queue.
	//
	// Note that this bypasses the rate limiter.
	Add(placementKey PlacementKey)
	// AddRateLimited adds a PlacementKey to the work queue after the rate limiter (if any)
	// says that it is OK.
	AddRateLimited(placementKey PlacementKey)
	// AddAfter adds a PlacementKey to the work queue after a set duration.
	AddAfter(placementKey PlacementKey, duration time.Duration)
}

// PlacementSchedulingQueue is an interface which queues PlacementKeys for the scheduler
// to consume; specifically, the scheduler finds the latest scheduling policy snapshot associated with the
// PlacementKey.
type PlacementSchedulingQueue interface {
	PlacementSchedulingQueueWriter

	// Run starts the scheduling queue.
	Run()
	// Close closes the scheduling queue immediately.
	Close()
	// CloseWithDrain closes the scheduling queue after all items in the queue are processed.
	CloseWithDrain()
	// NextPlacementKey returns the next-in-line PlacementKey for the scheduler to consume.
	NextPlacementKey() (key PlacementKey, closed bool)
	// Done marks a PlacementKey as done.
	Done(placementKey PlacementKey)
	// Forget untracks a PlacementKey from rate limiter(s) (if any) set up with the queue.
	Forget(placementKey PlacementKey)
}

// simplePlacementSchedulingQueue is a simple implementation of
// PlacementSchedulingQueue.
//
// At this moment, one single workqueue would suffice, as sources such as the cluster watcher,
// the binding watcher, etc., can catch all changes that need the scheduler's attention.
// In the future, when more features, e.g., inter-placement affinity/anti-affinity, are added,
// more queues, such as a backoff queue, might become necessary.
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

// Option is the function that configures the simplePlacementSchedulingQueue.
type Option func(*simplePlacementSchedulingQueueOptions)

var defaultSimplePlacementSchedulingQueueOptions = simplePlacementSchedulingQueueOptions{
	rateLimiter: workqueue.DefaultTypedControllerRateLimiter[any](),
	name:        "placementSchedulingQueue",
}

// WithRateLimiter sets a rate limiter for the workqueue.
func WithRateLimiter(rateLimiter workqueue.TypedRateLimiter[any]) Option {
	return func(o *simplePlacementSchedulingQueueOptions) {
		o.rateLimiter = rateLimiter
	}
}

// WithName sets a name for the workqueue.
func WithName(name string) Option {
	return func(o *simplePlacementSchedulingQueueOptions) {
		o.name = name
	}
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
// Note that this bypasses the rate limiter (if any)
func (sq *simplePlacementSchedulingQueue) AddAfter(placementKey PlacementKey, duration time.Duration) {
	sq.active.AddAfter(placementKey, duration)
}

// Forget untracks a PlacementKey from rate limiter(s) (if any) set up with the queue.
func (sq *simplePlacementSchedulingQueue) Forget(placementKey PlacementKey) {
	sq.active.Forget(placementKey)
}

// NewSimplePlacementSchedulingQueue returns a
// simplePlacementSchedulingQueue.
func NewSimplePlacementSchedulingQueue(opts ...Option) PlacementSchedulingQueue {
	options := defaultSimplePlacementSchedulingQueueOptions
	for _, opt := range opts {
		opt(&options)
	}

	return &simplePlacementSchedulingQueue{
		active: workqueue.NewTypedRateLimitingQueueWithConfig(options.rateLimiter, workqueue.TypedRateLimitingQueueConfig[any]{
			Name: options.name,
		}),
	}
}
