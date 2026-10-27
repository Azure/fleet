/*
Copyright 2026 The KubeFleet Authors.

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

// Package sourcequeue feeds requests into a controller-runtime controller from outside the
// manager's watches.
package sourcequeue

import (
	"context"
	"sync"

	"k8s.io/client-go/util/workqueue"
)

// Queue is a controller source that a producer outside the manager adds requests to.
//
// It is a source rather than a channel so that the producer is never blocked and repeated
// requests collapse: Add writes straight into the controller's own workqueue, which never blocks a
// writer and drops a request already queued. A channel would push both problems onto the
// producer, typically the resource watcher, which has a whole fleet's events to deliver and no
// business waiting on one controller.
//
// Requests added before the manager starts are held until it does, so the producer does not have
// to know when the controller became ready. That buffer collapses repeats, so it holds at most one
// entry per distinct request, but it has no other bound and is only ever drained by
// the controller calling Start, so a producer must not outlive the manager it was created for: in
// the hub agent both are started together under the same leader election.
//
// Register it with the controller builder's WatchesRawSource; the zero value is ready to use.
type Queue[T comparable] struct {
	mu      sync.Mutex
	queue   workqueue.TypedRateLimitingInterface[T]
	pending map[T]struct{}
}

// Add enqueues a request. It never blocks, and it is safe to call from any goroutine and before
// the manager has started.
func (q *Queue[T]) Add(req T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queue == nil {
		if q.pending == nil {
			q.pending = make(map[T]struct{})
		}
		// Collapsed the same way the controller's queue collapses, so a request added many times
		// before the manager started still costs one reconcile once it has.
		q.pending[req] = struct{}{}
		return
	}
	q.queue.Add(req)
}

// Start hands the queue to the controller. It is called by the controller, never directly.
func (q *Queue[T]) Start(_ context.Context, queue workqueue.TypedRateLimitingInterface[T]) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue = queue
	for req := range q.pending {
		queue.Add(req)
	}
	q.pending = nil
	return nil
}
