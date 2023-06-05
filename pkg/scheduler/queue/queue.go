/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package schedulingqueue features a scheduling queue, which keeps track of all placements for the scheduler
// to schedule.
package schedulingqueue

import (
	"k8s.io/client-go/util/workqueue"
)

// PolicySnapshotKey is the unique identifier for a PolicySnapshot stored in a scheduling queue.
type PolicySnapshotKey string

// PolicySnapshotKeySchedulingQueueWriter is an interface which allows sources, such as controllers, to add
// PolicySnapshots to the scheduling queue.
type PolicySnapshotKeySchedulingQueueWriter interface {
	Add(cpsKey PolicySnapshotKey)
}

// PolicySnapshotSchedulingQueue is an interface which queues PolicySnapshots for the scheduler to schedule.
type PolicySnapshotKeySchedulingQueue interface {
	PolicySnapshotKeySchedulingQueueWriter

	// Run starts the scheduling queue.
	Run()
	// Close closes the scheduling queue immediately.
	Close()
	// CloseWithDrain closes the scheduling queue after all items in the queue are processed.
	CloseWithDrain()
	// NextClusterPolicySnapshotKey returns the next-in-line PolicySnapshot key for the scheduler to schedule.
	NextClusterPolicySnapshotKey() PolicySnapshotKey
	// Done marks a PolicySnapshot key as done.
	Done(cpsKey PolicySnapshotKey)
}

// simplePolicySnapshotKeySchedulingQueue is a simple implementation of PolicySnapshotKeySchedulingQueue.
//
// At this moment, one single workqueue would suffice, as sources such as the cluster watcher,
// the binding watcher, etc., can catch all changes that need the scheduler's attention.
// In the future, when more features, e.g., inter-placement affinity/anti-affinity, are added,
// more queues, such as a backoff queue, might become necessary.
type simplePolicySnapshotKeySchedulingQueue struct {
	policySanpshotWorkQueue workqueue.RateLimitingInterface
}

// Verify that simplePolicySnapshotKeySchedulingQueue implements PolicySnapshotKeySchedulingQueue
// at compile time.
var _ PolicySnapshotKeySchedulingQueue = &simplePolicySnapshotKeySchedulingQueue{}

// simplePolicySnapshotKeySchedulingQueueOptions are the options for the simplePolicySnapshotKeySchedulingQueue.
type simplePolicySnapshotKeySchedulingQueueOptions struct {
	workqueueRateLimiter workqueue.RateLimiter
	workqueueName        string
}

// Option is the function that configures the simplePolicySnapshotKeySchedulingQueue.
type Option func(*simplePolicySnapshotKeySchedulingQueueOptions)

var defaultSimplePolicySnapshotKeySchedulingQueueOptions = simplePolicySnapshotKeySchedulingQueueOptions{
	workqueueRateLimiter: workqueue.DefaultControllerRateLimiter(),
	workqueueName:        "policySnapshotKeySchedulingQueue",
}

// WithWorkqueueRateLimiter sets a rate limiter for the workqueue.
func WithWorkqueueRateLimiter(rateLimiter workqueue.RateLimiter) Option {
	return func(o *simplePolicySnapshotKeySchedulingQueueOptions) {
		o.workqueueRateLimiter = rateLimiter
	}
}

// WithWorkqueueName sets a name for the workqueue.
func WithWorkqueueName(name string) Option {
	return func(o *simplePolicySnapshotKeySchedulingQueueOptions) {
		o.workqueueName = name
	}
}

// Run starts the scheduling queue.
//
// At this moment, Run is an no-op as there is only one queue present; in the future,
// when more queues are added, Run would start goroutines that move items between queues as
// appropriate.
func (sq *simplePolicySnapshotKeySchedulingQueue) Run() {}

// Close shuts down the scheduling queue immediately.
func (sq *simplePolicySnapshotKeySchedulingQueue) Close() {
	sq.policySanpshotWorkQueue.ShutDown()
}

// CloseWithDrain shuts down the scheduling queue and returns until all items are processed.
func (sq *simplePolicySnapshotKeySchedulingQueue) CloseWithDrain() {
	sq.policySanpshotWorkQueue.ShutDownWithDrain()
}

// NextClusterPolicySnapshotKey returns the next PolicySnapshot key in the work queue for
// the scheduler to process.
func (sq *simplePolicySnapshotKeySchedulingQueue) NextClusterPolicySnapshotKey() PolicySnapshotKey {
	// This will block on a condition variable if the queue is empty.
	cpsKey, shutdown := sq.policySanpshotWorkQueue.Get()
	if shutdown {
		return ""
	}
	return cpsKey.(PolicySnapshotKey)
}

// Done marks a PolicySnapshot key as done.
func (sq *simplePolicySnapshotKeySchedulingQueue) Done(cpsKey PolicySnapshotKey) {
	sq.policySanpshotWorkQueue.Done(cpsKey)
}

// Add adds a PolicySnapshot key to the work queue.
func (sq *simplePolicySnapshotKeySchedulingQueue) Add(cpsKey PolicySnapshotKey) {
	sq.policySanpshotWorkQueue.Add(cpsKey)
}

// NewSimplePolicySnapshotKeySchedulingQueue returns a simplePolicySnapshotKeySchedulingQueue.
func NewSimplePolicySnapshotKeySchedulingQueue(opts ...Option) PolicySnapshotKeySchedulingQueue {
	options := defaultSimplePolicySnapshotKeySchedulingQueueOptions
	for _, opt := range opts {
		opt(&options)
	}

	return &simplePolicySnapshotKeySchedulingQueue{
		policySanpshotWorkQueue: workqueue.NewNamedRateLimitingQueue(options.workqueueRateLimiter, options.workqueueName),
	}
}
