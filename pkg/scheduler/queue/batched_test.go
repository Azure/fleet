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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// TestBatchedProcessingPlacementSchedulingQueue_BasicOps tests the basic ops
// (Add, Next, Done) of a batchedProcessingPlacementSchedulingQueue.
func TestBatchedProcessingPlacementSchedulingQueue_BasicOps(t *testing.T) {
	bq := NewBatchedProcessingPlacementSchedulingQueue("TestOnly", nil, nil, 0)
	bq.Run()

	keysToAdd := []PlacementKey{"A", "B", "C", "D", "E"}
	for _, key := range keysToAdd {
		bq.Add(key)
	}

	keysRecved := []PlacementKey{}
	for i := 0; i < len(keysToAdd); i++ {
		key, closed := bq.NextPlacementKey()
		if closed {
			t.Fatalf("Queue closed unexpected")
		}
		keysRecved = append(keysRecved, key)
		bq.Done(key)
		bq.Forget(key)
	}

	if !cmp.Equal(keysToAdd, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, keysToAdd)
	}

	bq.Close()
}

// TestBatchedProcessingPlacementSchedulingQueue_BatchedOps tests the batched ops
// (AddBatched) of a batchedProcessingPlacementSchedulingQueue.
func TestBatchedProcessingPlacementSchedulingQueue_BatchedOps(t *testing.T) {
	movePeriodSeconds := int32(5) // 5 seconds
	bq := NewBatchedProcessingPlacementSchedulingQueue("TestOnly", nil, nil, movePeriodSeconds)
	bq.Run()

	addedTimestamp := time.Now()
	keysToAddBatched := []PlacementKey{"A", "B", "C"}
	for _, key := range keysToAddBatched {
		bq.AddBatched(key)
	}

	keysRecved := []PlacementKey{}
	for i := 0; i < len(keysToAddBatched); i++ {
		key, closed := bq.NextPlacementKey()
		if closed {
			t.Fatalf("Queue closed unexpected")
		}
		keysRecved = append(keysRecved, key)
		bq.Done(key)
		bq.Forget(key)
	}

	if !cmp.Equal(keysToAddBatched, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, keysToAddBatched)
	}
	// Allow some buffer time (+1 second).
	if timeSpent := time.Since(addedTimestamp); timeSpent < time.Duration(movePeriodSeconds-1)*time.Second {
		t.Fatalf("time to move keys, want no less than %f seconds, got %f seconds", float64(movePeriodSeconds-1), timeSpent.Seconds())
	}
}

// TestBatchedProcessingPlacementSchedulingQueue_MoveNow tests the moveNow signal
// built in a batchedProcessingPlacementSchedulingQueue.
func TestBatchedProcessingPlacementSchedulingQueue_MoveNow(t *testing.T) {
	movePeriodSeconds := int32(10) // 10 seconds
	bq := NewBatchedProcessingPlacementSchedulingQueue("TestOnly", nil, nil, movePeriodSeconds)
	bq.Run()

	keysToAddBatched := []PlacementKey{"A", "B", "C"}
	for _, key := range keysToAddBatched {
		bq.AddBatched(key)
	}

	// Send a move now signal.
	bqStruct, ok := bq.(*batchedProcessingPlacementSchedulingQueue)
	if !ok {
		t.Fatalf("Failed to cast to batchedProcessingPlacementSchedulingQueue")
	}
	bqStruct.moveNow <- struct{}{}

	moveNowTriggeredTimestamp := time.Now()
	keysRecved := []PlacementKey{}
	for i := 0; i < len(keysToAddBatched); i++ {
		key, closed := bq.NextPlacementKey()
		if closed {
			t.Fatalf("Queue closed unexpected")
		}
		keysRecved = append(keysRecved, key)
		bq.Done(key)
		bq.Forget(key)
	}

	if !cmp.Equal(keysToAddBatched, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, keysToAddBatched)
	}
	// Allow some buffer time (1 seconds).
	if timeSpent := time.Since(moveNowTriggeredTimestamp); timeSpent > time.Second {
		t.Fatalf("time to move keys after move now triggered, want no more than %f seconds, got %f seconds", 1.0, timeSpent.Seconds())
	}
}

// TestBatchedProcessingPlacementSchedulingQueue_CloseWithDrain tests the CloseWithDrain
// method of a batchedProcessingPlacementSchedulingQueue.
func TestBatchedProcessingPlacementSchedulingQueue_CloseWithDrain(t *testing.T) {
	movePeriodSeconds := int32(600) // 10 minutes
	bq := NewBatchedProcessingPlacementSchedulingQueue("TestOnly", nil, nil, movePeriodSeconds)
	bq.Run()

	keysToAdd := []PlacementKey{"A", "B", "C"}
	for _, key := range keysToAdd {
		bq.Add(key)
	}

	keysToAddBatched := []PlacementKey{"D", "E", "F"}
	for _, key := range keysToAddBatched {
		bq.AddBatched(key)
	}

	// Send a move now signal.
	bqStruct, ok := bq.(*batchedProcessingPlacementSchedulingQueue)
	if !ok {
		t.Fatalf("Failed to cast to batchedProcessingPlacementSchedulingQueue")
	}
	bqStruct.moveNow <- struct{}{}

	keysRecved := []PlacementKey{}
	for i := 0; i < len(keysToAdd)+len(keysToAddBatched); i++ {
		key, closed := bq.NextPlacementKey()
		if closed {
			t.Fatalf("Queue closed unexpected")
		}
		keysRecved = append(keysRecved, key)
		// Do not yet mark the keys as Done.
	}

	timerPeriodSeconds := int32(5)
	go func() {
		timer := time.NewTimer(time.Duration(timerPeriodSeconds) * time.Second)
		<-timer.C
		// Mark all keys as Done after 5 seconds.
		for _, key := range keysRecved {
			bq.Done(key)
			bq.Forget(key)
		}
	}()

	// Close and drain the queue; this should block until all keys are marked Done.
	closeWithDrainTimestamp := time.Now()
	bq.CloseWithDrain()

	wantKeys := make([]PlacementKey, 0, len(keysToAdd)+len(keysToAddBatched))
	wantKeys = append(wantKeys, keysToAdd...)
	wantKeys = append(wantKeys, keysToAddBatched...)
	if !cmp.Equal(wantKeys, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, wantKeys)
	}
	// Allow some buffer time (+1 second).
	if timeSpent := time.Since(closeWithDrainTimestamp); timeSpent > time.Duration(timerPeriodSeconds+1)*time.Second {
		t.Fatalf("time to close with drain, want no more than %f seconds, got %f seconds", float64(timerPeriodSeconds+1), timeSpent.Seconds())
	}
}
