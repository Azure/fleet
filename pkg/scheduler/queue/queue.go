/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package queue features a scheduling queue, which keeps track of all placements for the scheduler
// to schedule.
package queue

import (
	"k8s.io/client-go/util/workqueue"
)

// ClusterPolicySnapshotKey is the unique identifier (its name) for a ClusterPolicySnapshot stored in a scheduling queue.
type ClusterPolicySnapshotKey string

// ClusterPolicySnapshotKeySchedulingQueueWriter is an interface which allows sources, such as controllers, to add
// ClusterPolicySnapshots to the scheduling queue.
type ClusterPolicySnapshotKeySchedulingQueueWriter interface {
	Add(cpsKey ClusterPolicySnapshotKey)
}

// ClusterPolicySnapshotSchedulingQueue is an interface which queues ClusterPolicySnapshots for the scheduler to schedule.
type ClusterPolicySnapshotKeySchedulingQueue interface {
	ClusterPolicySnapshotKeySchedulingQueueWriter

	// Run starts the scheduling queue.
	Run()
	// Close closes the scheduling queue immediately.
	Close()
	// CloseWithDrain closes the scheduling queue after all items in the queue are processed.
	CloseWithDrain()
	// NextClusterPolicySnapshotKey returns the next-in-line ClusterPolicySnapshot key for the scheduler to schedule.
	NextClusterPolicySnapshotKey() (key ClusterPolicySnapshotKey, closed bool)
	// Done marks a ClusterPolicySnapshot key as done.
	Done(cpsKey ClusterPolicySnapshotKey)
}

// simpleClusterPolicySnapshotKeySchedulingQueue is a simple implementation of
// ClusterPolicySnapshotKeySchedulingQueue.
//
// At this moment, one single workqueue would suffice, as sources such as the cluster watcher,
// the binding watcher, etc., can catch all changes that need the scheduler's attention.
// In the future, when more features, e.g., inter-placement affinity/anti-affinity, are added,
// more queues, such as a backoff queue, might become necessary.
type simpleClusterPolicySnapshotKeySchedulingQueue struct {
	clusterPolicySanpshotWorkQueue workqueue.RateLimitingInterface
}

// Verify that simpleClusterPolicySnapshotKeySchedulingQueue implements
// ClusterPolicySnapshotKeySchedulingQueue at compile time.
var _ ClusterPolicySnapshotKeySchedulingQueue = &simpleClusterPolicySnapshotKeySchedulingQueue{}

// simpleClusterPolicySnapshotKeySchedulingQueueOptions are the options for the
// simpleClusterPolicySnapshotKeySchedulingQueue.
type simpleClusterPolicySnapshotKeySchedulingQueueOptions struct {
	workqueueRateLimiter workqueue.RateLimiter
	workqueueName        string
}

// Option is the function that configures the simpleClusterPolicySnapshotKeySchedulingQueue.
type Option func(*simpleClusterPolicySnapshotKeySchedulingQueueOptions)

var defaultSimpleClusterPolicySnapshotKeySchedulingQueueOptions = simpleClusterPolicySnapshotKeySchedulingQueueOptions{
	workqueueRateLimiter: workqueue.DefaultControllerRateLimiter(),
	workqueueName:        "clusterPolicySnapshotKeySchedulingQueue",
}

// WithWorkqueueRateLimiter sets a rate limiter for the workqueue.
func WithWorkqueueRateLimiter(rateLimiter workqueue.RateLimiter) Option {
	return func(o *simpleClusterPolicySnapshotKeySchedulingQueueOptions) {
		o.workqueueRateLimiter = rateLimiter
	}
}

// WithWorkqueueName sets a name for the workqueue.
func WithWorkqueueName(name string) Option {
	return func(o *simpleClusterPolicySnapshotKeySchedulingQueueOptions) {
		o.workqueueName = name
	}
}

// Run starts the scheduling queue.
//
// At this moment, Run is an no-op as there is only one queue present; in the future,
// when more queues are added, Run would start goroutines that move items between queues as
// appropriate.
func (sq *simpleClusterPolicySnapshotKeySchedulingQueue) Run() {}

// Close shuts down the scheduling queue immediately.
func (sq *simpleClusterPolicySnapshotKeySchedulingQueue) Close() {
	sq.clusterPolicySanpshotWorkQueue.ShutDown()
}

// CloseWithDrain shuts down the scheduling queue and returns until all items are processed.
func (sq *simpleClusterPolicySnapshotKeySchedulingQueue) CloseWithDrain() {
	sq.clusterPolicySanpshotWorkQueue.ShutDownWithDrain()
}

// NextClusterPolicySnapshotKey returns the next ClusterPolicySnapshot key in the work queue for
// the scheduler to process.
//
// Note that for now the queue simply wraps a work queue, and consider its state (whether it
// is shut down or not) as its own closedness. In the future, when more queues are added, the
// queue implementation must manage its own state.
func (sq *simpleClusterPolicySnapshotKeySchedulingQueue) NextClusterPolicySnapshotKey() (key ClusterPolicySnapshotKey, closed bool) {
	// This will block on a condition variable if the queue is empty.
	cpsKey, shutdown := sq.clusterPolicySanpshotWorkQueue.Get()
	if shutdown {
		return "", true
	}
	return cpsKey.(ClusterPolicySnapshotKey), false
}

// Done marks a ClusterPolicySnapshot key as done.
func (sq *simpleClusterPolicySnapshotKeySchedulingQueue) Done(cpsKey ClusterPolicySnapshotKey) {
	sq.clusterPolicySanpshotWorkQueue.Done(cpsKey)
}

// Add adds a ClusterPolicySnapshot key to the work queue.
func (sq *simpleClusterPolicySnapshotKeySchedulingQueue) Add(cpsKey ClusterPolicySnapshotKey) {
	sq.clusterPolicySanpshotWorkQueue.Add(cpsKey)
}

// NewSimpleClusterPolicySnapshotKeySchedulingQueue returns a
// simpleClusterPolicySnapshotKeySchedulingQueue.
func NewSimpleClusterPolicySnapshotKeySchedulingQueue(opts ...Option) ClusterPolicySnapshotKeySchedulingQueue {
	options := defaultSimpleClusterPolicySnapshotKeySchedulingQueueOptions
	for _, opt := range opts {
		opt(&options)
	}

	return &simpleClusterPolicySnapshotKeySchedulingQueue{
		clusterPolicySanpshotWorkQueue: workqueue.NewNamedRateLimitingQueue(options.workqueueRateLimiter, options.workqueueName),
	}
}
