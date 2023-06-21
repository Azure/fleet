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

// SchedulingPolicySnapshotKey is the unique identifier (its name) for a SchedulingPolicySnapshot stored in a scheduling queue.
type SchedulingPolicySnapshotKey string

// SchedulingPolicySnapshotKeySchedulingQueueWriter is an interface which allows sources, such as controllers, to add
// SchedulingPolicySnapshots to the scheduling queue.
type SchedulingPolicySnapshotKeySchedulingQueueWriter interface {
	Add(cpsKey SchedulingPolicySnapshotKey)
}

// SchedulingPolicySnapshotSchedulingQueue is an interface which queues SchedulingPolicySnapshots for the scheduler to schedule.
type SchedulingPolicySnapshotKeySchedulingQueue interface {
	SchedulingPolicySnapshotKeySchedulingQueueWriter

	// Run starts the scheduling queue.
	Run()
	// Close closes the scheduling queue immediately.
	Close()
	// CloseWithDrain closes the scheduling queue after all items in the queue are processed.
	CloseWithDrain()
	// NextSchedulingPolicySnapshotKey returns the next-in-line SchedulingPolicySnapshot key for the scheduler to schedule.
	NextSchedulingPolicySnapshotKey() (key SchedulingPolicySnapshotKey, closed bool)
	// Done marks a SchedulingPolicySnapshot key as done.
	Done(cpsKey SchedulingPolicySnapshotKey)
}

// simpleSchedulingPolicySnapshotKeySchedulingQueue is a simple implementation of
// SchedulingPolicySnapshotKeySchedulingQueue.
//
// At this moment, one single workqueue would suffice, as sources such as the cluster watcher,
// the binding watcher, etc., can catch all changes that need the scheduler's attention.
// In the future, when more features, e.g., inter-placement affinity/anti-affinity, are added,
// more queues, such as a backoff queue, might become necessary.
type simpleSchedulingPolicySnapshotKeySchedulingQueue struct {
	clusterPolicySanpshotWorkQueue workqueue.RateLimitingInterface
}

// Verify that simpleSchedulingPolicySnapshotKeySchedulingQueue implements
// SchedulingPolicySnapshotKeySchedulingQueue at compile time.
var _ SchedulingPolicySnapshotKeySchedulingQueue = &simpleSchedulingPolicySnapshotKeySchedulingQueue{}

// simpleSchedulingPolicySnapshotKeySchedulingQueueOptions are the options for the
// simpleSchedulingPolicySnapshotKeySchedulingQueue.
type simpleSchedulingPolicySnapshotKeySchedulingQueueOptions struct {
	workqueueRateLimiter workqueue.RateLimiter
	workqueueName        string
}

// Option is the function that configures the simpleSchedulingPolicySnapshotKeySchedulingQueue.
type Option func(*simpleSchedulingPolicySnapshotKeySchedulingQueueOptions)

var defaultSimpleSchedulingPolicySnapshotKeySchedulingQueueOptions = simpleSchedulingPolicySnapshotKeySchedulingQueueOptions{
	workqueueRateLimiter: workqueue.DefaultControllerRateLimiter(),
	workqueueName:        "schedulingPolicySnapshotKeySchedulingQueue",
}

// WithWorkqueueRateLimiter sets a rate limiter for the workqueue.
func WithWorkqueueRateLimiter(rateLimiter workqueue.RateLimiter) Option {
	return func(o *simpleSchedulingPolicySnapshotKeySchedulingQueueOptions) {
		o.workqueueRateLimiter = rateLimiter
	}
}

// WithWorkqueueName sets a name for the workqueue.
func WithWorkqueueName(name string) Option {
	return func(o *simpleSchedulingPolicySnapshotKeySchedulingQueueOptions) {
		o.workqueueName = name
	}
}

// Run starts the scheduling queue.
//
// At this moment, Run is an no-op as there is only one queue present; in the future,
// when more queues are added, Run would start goroutines that move items between queues as
// appropriate.
func (sq *simpleSchedulingPolicySnapshotKeySchedulingQueue) Run() {}

// Close shuts down the scheduling queue immediately.
func (sq *simpleSchedulingPolicySnapshotKeySchedulingQueue) Close() {
	sq.clusterPolicySanpshotWorkQueue.ShutDown()
}

// CloseWithDrain shuts down the scheduling queue and returns until all items are processed.
func (sq *simpleSchedulingPolicySnapshotKeySchedulingQueue) CloseWithDrain() {
	sq.clusterPolicySanpshotWorkQueue.ShutDownWithDrain()
}

// NextSchedulingPolicySnapshotKey returns the next SchedulingPolicySnapshot key in the work queue for
// the scheduler to process.
//
// Note that for now the queue simply wraps a work queue, and consider its state (whether it
// is shut down or not) as its own closedness. In the future, when more queues are added, the
// queue implementation must manage its own state.
func (sq *simpleSchedulingPolicySnapshotKeySchedulingQueue) NextSchedulingPolicySnapshotKey() (key SchedulingPolicySnapshotKey, closed bool) {
	// This will block on a condition variable if the queue is empty.
	cpsKey, shutdown := sq.clusterPolicySanpshotWorkQueue.Get()
	if shutdown {
		return "", true
	}
	return cpsKey.(SchedulingPolicySnapshotKey), false
}

// Done marks a SchedulingPolicySnapshot key as done.
func (sq *simpleSchedulingPolicySnapshotKeySchedulingQueue) Done(cpsKey SchedulingPolicySnapshotKey) {
	sq.clusterPolicySanpshotWorkQueue.Done(cpsKey)
}

// Add adds a SchedulingPolicySnapshot key to the work queue.
func (sq *simpleSchedulingPolicySnapshotKeySchedulingQueue) Add(cpsKey SchedulingPolicySnapshotKey) {
	sq.clusterPolicySanpshotWorkQueue.Add(cpsKey)
}

// NewSimpleSchedulingPolicySnapshotKeySchedulingQueue returns a
// simpleSchedulingPolicySnapshotKeySchedulingQueue.
func NewSimpleSchedulingPolicySnapshotKeySchedulingQueue(opts ...Option) SchedulingPolicySnapshotKeySchedulingQueue {
	options := defaultSimpleSchedulingPolicySnapshotKeySchedulingQueueOptions
	for _, opt := range opts {
		opt(&options)
	}

	return &simpleSchedulingPolicySnapshotKeySchedulingQueue{
		clusterPolicySanpshotWorkQueue: workqueue.NewNamedRateLimitingQueue(options.workqueueRateLimiter, options.workqueueName),
	}
}
