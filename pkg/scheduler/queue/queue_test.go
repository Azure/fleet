/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package queue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSimpleClusterResourcePlacementSchedulingQueueBasicOps tests the basic ops
// (Add, Next, Done) of a simpleClusterResourcePlacementSchedulingQueue.
func TestSimpleClusterResourcePlacementSchedulingQueueBasicOps(t *testing.T) {
	sq := NewSimpleClusterResourcePlacementSchedulingQueue()
	sq.Run()

	keysToAdd := []ClusterResourcePlacementKey{"A", "B", "C", "D", "E"}
	for _, key := range keysToAdd {
		sq.Add(key)
	}

	keysRecved := []ClusterResourcePlacementKey{}
	for i := 0; i < len(keysToAdd); i++ {
		key, closed := sq.NextClusterResourcePlacementKey()
		if closed {
			t.Fatalf("Queue closed unexpected")
		}
		keysRecved = append(keysRecved, key)
		sq.Done(key)
		sq.Forget(key)
	}

	if !cmp.Equal(keysToAdd, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, keysToAdd)
	}

	sq.Close()
}
