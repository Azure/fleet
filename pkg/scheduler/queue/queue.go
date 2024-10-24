/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package queue features a scheduling queue, which keeps track of all placements for the scheduler
// to schedule.
package queue

import (
	"time"

	"k8s.io/client-go/util/workqueue"
)

// ClusterResourcePlacementKey is the unique identifier (its name) for a ClusterResourcePlacement checked
// into a scheduling queue.
type ClusterResourcePlacementKey string

// ClusterResourcePlacementSchedulingQueueWriter is an interface which allows sources, such as controllers, to add
// ClusterResourcePlacementKeys to the scheduling queue.
type ClusterResourcePlacementSchedulingQueueWriter interface {
	// Add adds a ClusterResourcePlacementKey to the work queue.
	//
	// Note that this bypasses the rate limiter.
	Add(crpKey ClusterResourcePlacementKey)
	// AddRateLimited adds a ClusterResourcePlacementKey to the work queue after the rate limiter (if any)
	// says that it is OK.
	AddRateLimited(crpKey ClusterResourcePlacementKey)
	// AddAfter adds a ClusterResourcePlacementKey to the work queue after a set duration.
	AddAfter(crpKey ClusterResourcePlacementKey, duration time.Duration)
}

// ClusterResourcePlacementSchedulingQueue is an interface which queues ClusterResourcePlacements for the scheduler
// to consume; specifically, the scheduler finds the latest scheduling policy snapshot associated with the
// ClusterResourcePlacement.
type ClusterResourcePlacementSchedulingQueue interface {
	ClusterResourcePlacementSchedulingQueueWriter

	// Run starts the scheduling queue.
	Run()
	// Close closes the scheduling queue immediately.
	Close()
	// CloseWithDrain closes the scheduling queue after all items in the queue are processed.
	CloseWithDrain()
	// NextClusterResourcePlacementKey returns the next-in-line ClusterResourcePlacementKey for the scheduler to consume.
	NextClusterResourcePlacementKey() (key ClusterResourcePlacementKey, closed bool)
	// Done marks a ClusterResourcePlacementKey as done.
	Done(crpKey ClusterResourcePlacementKey)
	// Forget untracks a ClusterResourcePlacementKey from rate limiter(s) (if any) set up with the queue.
	Forget(crpKey ClusterResourcePlacementKey)
}

// simpleClusterResourcePlacementSchedulingQueue is a simple implementation of
// ClusterResourcePlacementSchedulingQueue.
//
// At this moment, one single workqueue would suffice, as sources such as the cluster watcher,
// the binding watcher, etc., can catch all changes that need the scheduler's attention.
// In the future, when more features, e.g., inter-placement affinity/anti-affinity, are added,
// more queues, such as a backoff queue, might become necessary.
type simpleClusterResourcePlacementSchedulingQueue struct {
	active workqueue.RateLimitingInterface
}

// Verify that simpleClusterResourcePlacementSchedulingQueue implements
// ClusterResourceSchedulingQueue at compile time.
var _ ClusterResourcePlacementSchedulingQueue = &simpleClusterResourcePlacementSchedulingQueue{}

// simpleClusterResourcePlacementSchedulingQueueOptions are the options for the
// simpleClusterResourcePlacementSchedulingQueue.
type simpleClusterResourcePlacementSchedulingQueueOptions struct {
	rateLimiter workqueue.RateLimiter
	name        string
}

// Option is the function that configures the simpleClusterResourcePlacementSchedulingQueue.
type Option func(*simpleClusterResourcePlacementSchedulingQueueOptions)

var defaultSimpleClusterResourcePlacementSchedulingQueueOptions = simpleClusterResourcePlacementSchedulingQueueOptions{
	rateLimiter: workqueue.DefaultControllerRateLimiter(),
	name:        "clusterResourcePlacementSchedulingQueue",
}

// WithRateLimiter sets a rate limiter for the workqueue.
func WithRateLimiter(rateLimiter workqueue.RateLimiter) Option {
	return func(o *simpleClusterResourcePlacementSchedulingQueueOptions) {
		o.rateLimiter = rateLimiter
	}
}

// WithName sets a name for the workqueue.
func WithName(name string) Option {
	return func(o *simpleClusterResourcePlacementSchedulingQueueOptions) {
		o.name = name
	}
}

// Run starts the scheduling queue.
//
// At this moment, Run is an no-op as there is only one queue present; in the future,
// when more queues are added, Run would start goroutines that move items between queues as
// appropriate.
func (sq *simpleClusterResourcePlacementSchedulingQueue) Run() {}

// Close shuts down the scheduling queue immediately.
func (sq *simpleClusterResourcePlacementSchedulingQueue) Close() {
	sq.active.ShutDown()
}

// CloseWithDrain shuts down the scheduling queue and returns until all items are processed.
func (sq *simpleClusterResourcePlacementSchedulingQueue) CloseWithDrain() {
	sq.active.ShutDownWithDrain()
}

// NextClusterResourcePlacementKey returns the next ClusterResourcePlacementKey in the work queue for
// the scheduler to process.
//
// Note that for now the queue simply wraps a work queue, and consider its state (whether it
// is shut down or not) as its own closedness. In the future, when more queues are added, the
// queue implementation must manage its own state.
func (sq *simpleClusterResourcePlacementSchedulingQueue) NextClusterResourcePlacementKey() (key ClusterResourcePlacementKey, closed bool) {
	// This will block on a condition variable if the queue is empty.
	crpKey, shutdown := sq.active.Get()
	if shutdown {
		return "", true
	}
	return crpKey.(ClusterResourcePlacementKey), false
}

// Done marks a ClusterResourcePlacementKey as done.
func (sq *simpleClusterResourcePlacementSchedulingQueue) Done(crpKey ClusterResourcePlacementKey) {
	sq.active.Done(crpKey)
}

// Add adds a ClusterResourcePlacementKey to the work queue.
//
// Note that this bypasses the rate limiter (if any).
func (sq *simpleClusterResourcePlacementSchedulingQueue) Add(crpKey ClusterResourcePlacementKey) {
	sq.active.Add(crpKey)
}

// AddRateLimited adds a ClusterResourcePlacementKey to the work queue after the rate limiter (if any)
// says that it is OK.
func (sq *simpleClusterResourcePlacementSchedulingQueue) AddRateLimited(crpKey ClusterResourcePlacementKey) {
	sq.active.AddRateLimited(crpKey)
}

// AddAfter adds a ClusterResourcePlacementKey to the work queue after a set duration.
//
// Note that this bypasses the rate limiter (if any)
func (sq *simpleClusterResourcePlacementSchedulingQueue) AddAfter(crpKey ClusterResourcePlacementKey, duration time.Duration) {
	sq.active.AddAfter(crpKey, duration)
}

// Forget untracks a ClusterResourcePlacementKey from rate limiter(s) (if any) set up with the queue.
func (sq *simpleClusterResourcePlacementSchedulingQueue) Forget(crpKey ClusterResourcePlacementKey) {
	sq.active.Forget(crpKey)
}

// NewSimpleClusterResourcePlacementSchedulingQueue returns a
// simpleClusterResourcePlacementSchedulingQueue.
func NewSimpleClusterResourcePlacementSchedulingQueue(opts ...Option) ClusterResourcePlacementSchedulingQueue {
	options := defaultSimpleClusterResourcePlacementSchedulingQueueOptions
	for _, opt := range opts {
		opt(&options)
	}

	return &simpleClusterResourcePlacementSchedulingQueue{
		active: workqueue.NewRateLimitingQueueWithConfig(options.rateLimiter, workqueue.RateLimitingQueueConfig{
			Name: options.name,
		}),
	}
}
