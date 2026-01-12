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
	"fmt"
	"time"

	"k8s.io/client-go/util/workqueue"
)

const (
	maxNumberOfKeysToMoveFromBatchedToActiveQueuePerGo = 20000
)

// batchedProcessingPlacementSchedulingQueue implements the PlacementSchedulingQueue
// interface.
//
// It consists of two work queues to allow processing for both immediate and batched
// processing for scheduling related events (changes) of different responsiveness levels.
type batchedProcessingPlacementSchedulingQueue struct {
	active  workqueue.TypedRateLimitingInterface[any]
	batched workqueue.TypedRateLimitingInterface[any]

	moveNow           chan struct{}
	movePeriodSeconds int32
}

// Verify that batchedProcessingPlacementSchedulingQueue implements
// PlacementSchedulingQueue at compile time.
var _ PlacementSchedulingQueue = &batchedProcessingPlacementSchedulingQueue{}

// batchedProcessingPlacementSchedulingQueueOptions are the options for the
// batchedProcessingPlacementSchedulingQueue.
type batchedProcessingPlacementSchedulingQueueOptions struct {
	activeQueueRateLimiter  workqueue.TypedRateLimiter[any]
	batchedQueueRateLimiter workqueue.TypedRateLimiter[any]
	name                    string
	movePeriodSeconds       int32
}

var defaultBatchedProcessingPlacementSchedulingQueueOptions = batchedProcessingPlacementSchedulingQueueOptions{
	activeQueueRateLimiter:  workqueue.DefaultTypedControllerRateLimiter[any](),
	batchedQueueRateLimiter: workqueue.DefaultTypedControllerRateLimiter[any](),
	name:                    "batchedProcessingPlacementSchedulingQueue",
	movePeriodSeconds:       int32(300), // 5 minutes
}

// Close shuts down the scheduling queue immediately.
//
// Note that items remaining in the active queue might not get processed any more, and items
// left in the batched queue might not be moved to the active queue any more either.
func (bq *batchedProcessingPlacementSchedulingQueue) Close() {
	// Signal the mover goroutine to exit.
	//
	// Note that this will trigger the mover goroutine to attempt another key move, but the
	// active queue might not be able to accept the key any more (which is OK and does not
	// result in an error).
	close(bq.moveNow)

	bq.batched.ShutDown()
	bq.active.ShutDown()
}

// CloseWithDrain shuts down the scheduling queue and returns until:
// a) all the items in the batched queue have been moved to the active queue; and
// b) all the items in the active queue have been processed.
func (bq *batchedProcessingPlacementSchedulingQueue) CloseWithDrain() {
	// Signal that all items in the batched queue should be moved to the active queue right away.
	close(bq.moveNow)

	// Wait until all the items in the moving process from the batched queue to the active queue have completed
	// their moves.
	bq.batched.ShutDownWithDrain()
	// Wait until all the items that are currently being processed by the scheduler to finish.
	bq.active.ShutDownWithDrain()
}

// NextPlacementKey returns the next PlacementKey (either clusterResourcePlacementKey or resourcePlacementKey)
// in the work queue for the scheduler to process.
func (bq *batchedProcessingPlacementSchedulingQueue) NextPlacementKey() (key PlacementKey, closed bool) {
	// This will block on a condition variable if the queue is empty.
	placementKey, shutdown := bq.active.Get()
	if shutdown {
		return "", true
	}
	return placementKey.(PlacementKey), false
}

// Done marks a PlacementKey as done.
func (bq *batchedProcessingPlacementSchedulingQueue) Done(placementKey PlacementKey) {
	bq.active.Done(placementKey)
	// The keys in the batched queue are marked as done as soon as they are moved to the active queue.
}

// Add adds a PlacementKey to the work queue for immediate processing.
//
// Note that this bypasses the rate limiter (if any).
func (bq *batchedProcessingPlacementSchedulingQueue) Add(placementKey PlacementKey) {
	bq.active.Add(placementKey)
}

// AddAfter adds a PlacementKey to the work queue after a set duration for immediate processing.
//
// Note that this bypasses the rate limiter (if any).
func (bq *batchedProcessingPlacementSchedulingQueue) AddAfter(placementKey PlacementKey, duration time.Duration) {
	bq.active.AddAfter(placementKey, duration)
}

// AddRateLimited adds a PlacementKey to the work queue after the rate limiter (if any)
// says that it is OK, for immediate processing.
func (bq *batchedProcessingPlacementSchedulingQueue) AddRateLimited(placementKey PlacementKey) {
	bq.active.AddRateLimited(placementKey)
}

// Forget untracks a PlacementKey from rate limiter(s) (if any) set up with the queue.
func (bq *batchedProcessingPlacementSchedulingQueue) Forget(placementKey PlacementKey) {
	bq.active.Forget(placementKey)
	// The keys in the batched queue are forgotten as soon as they are moved to the active queue.
}

// AddBatched tracks a PlacementKey and adds such keys in batch later to the work queue when appropriate.
func (bq *batchedProcessingPlacementSchedulingQueue) AddBatched(placementKey PlacementKey) {
	bq.batched.Add(placementKey)
}

// Run starts the scheduling queue.
func (bq *batchedProcessingPlacementSchedulingQueue) Run() {
	// Spin up a goroutine to move items periodically from the batched queue to the active queue.
	go func() {
		timer := time.NewTimer(time.Duration(bq.movePeriodSeconds) * time.Second)
		for {
			select {
			case _, closed := <-bq.moveNow:
				if closed && bq.batched.ShuttingDown() {
					// The batched queue has been shut down, and the moveNow channel has been closed;
					// now it is safe to assume that after moving all the items from the batched queue to the active queue
					// this time, the batched queue will be drained.
					bq.moveAllBatchedItemsToActiveQueue()
					return
				}

				// The batched queue might still be running; move all items and re-enter the loop.
				bq.moveAllBatchedItemsToActiveQueue()
			case <-timer.C:
				// The timer has fired; move all items.
				bq.moveAllBatchedItemsToActiveQueue()
			}

			// Reset the timer for the next round.
			timer.Reset(time.Duration(bq.movePeriodSeconds) * time.Second)
		}
	}()
}

func (bq *batchedProcessingPlacementSchedulingQueue) moveAllBatchedItemsToActiveQueue() {
	keysToMove := []PlacementKey{}

	for bq.batched.Len() > 0 {
		// Note that the batched queue is an internal object and is only read here by the scheduling queue
		// itself (i.e., the batched queue has only one reader, though there might be multiple writers);
		// consequently, if the Len() > 0 check passes, the subsequent Get() call is guaranteed to return
		// an item (i.e., the call will not block). For simplicity reasons we do not do additional
		// sanity checks here.
		placementKey, shutdown := bq.batched.Get()
		if shutdown {
			break
		}
		keysToMove = append(keysToMove, placementKey.(PlacementKey))

		if len(keysToMove) > maxNumberOfKeysToMoveFromBatchedToActiveQueuePerGo {
			// The keys popped from the batched queue are not yet added to the active queue, in other words,
			// they are not yet marked as done; the batched queue will still track them and adding them
			// to the batched queue again at this moment will not trigger the batched queue to yield the same
			// keys again. This implies that the at maximum we will be moving a number of keys equal to
			// the number of placement objects in the system at a time, which should be a finite number.
			// Still, to be on the safer side here KubeFleet sets a cap the number of keys to move per go.
			break
		}
	}

	for _, key := range keysToMove {
		// Mark the keys as done in the batched queue and add the keys to the active queue in batch. Here the
		// implementation keeps the keys in memory first and does not move keys right after they are popped as
		// this pattern risks synchronized processing (i.e., a key is popped from the batched queue, immeidiately added to the
		// active queue and gets marked as done by the scheduler, then added back to the batched queue again by
		// one of the watchers before the key moving attempt is finished, which results in perpetual key moving).
		bq.active.Add(key)
		bq.batched.Done(key)
		bq.batched.Forget(key)
	}
}

// NewBatchedProcessingPlacementSchedulingQueue returns a batchedProcessingPlacementSchedulingQueue.
func NewBatchedProcessingPlacementSchedulingQueue(name string, activeQRateLimiter, batchedQRateLimiter workqueue.TypedRateLimiter[any], movePeriodSeconds int32) PlacementSchedulingQueue {
	if len(name) == 0 {
		name = defaultBatchedProcessingPlacementSchedulingQueueOptions.name
	}
	if activeQRateLimiter == nil {
		activeQRateLimiter = defaultBatchedProcessingPlacementSchedulingQueueOptions.activeQueueRateLimiter
	}
	if batchedQRateLimiter == nil {
		batchedQRateLimiter = defaultBatchedProcessingPlacementSchedulingQueueOptions.batchedQueueRateLimiter
	}
	if movePeriodSeconds <= 0 {
		movePeriodSeconds = defaultBatchedProcessingPlacementSchedulingQueueOptions.movePeriodSeconds
	}

	return &batchedProcessingPlacementSchedulingQueue{
		active: workqueue.NewTypedRateLimitingQueueWithConfig(activeQRateLimiter, workqueue.TypedRateLimitingQueueConfig[any]{
			Name: fmt.Sprintf("%s_Active", name),
		}),
		batched: workqueue.NewTypedRateLimitingQueueWithConfig(batchedQRateLimiter, workqueue.TypedRateLimitingQueueConfig[any]{
			Name: fmt.Sprintf("%s_Batched", name),
		}),
		moveNow:           make(chan struct{}),
		movePeriodSeconds: movePeriodSeconds,
	}
}
