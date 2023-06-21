/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package queue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSimpleSchedulingPolicySnapshotKeySchedulingQueueBasicOps tests the basic ops
// (Add, NextSchedulingPolicySnapshotKey, Done) of a simplePolicySnapshotKeySchedulingQueue.
func TestSimpleSchedulingPolicySnapshotKeySchedulingQueueBasicOps(t *testing.T) {
	sq := NewSimpleSchedulingPolicySnapshotKeySchedulingQueue()
	sq.Run()

	keysToAdd := []SchedulingPolicySnapshotKey{"A", "B", "C", "D", "E"}
	for _, key := range keysToAdd {
		sq.Add(key)
	}

	keysRecved := []SchedulingPolicySnapshotKey{}
	for i := 0; i < len(keysToAdd); i++ {
		key, closed := sq.NextSchedulingPolicySnapshotKey()
		if closed {
			t.Fatalf("Queue closed unexpected")
		}
		keysRecved = append(keysRecved, key)
		sq.Done(key)
	}

	if !cmp.Equal(keysToAdd, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, keysToAdd)
	}

	sq.Close()
}
