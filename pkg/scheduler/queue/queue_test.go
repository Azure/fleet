/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package queue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSimpleClusterPolicySnapshotKeySchedulingQueueBasicOps tests the basic ops
// (Add, NextClusterPolicySnapshotKey, Done) of a simplePolicySnapshotKeySchedulingQueue.
func TestSimpleClusterPolicySnapshotKeySchedulingQueueBasicOps(t *testing.T) {
	sq := NewSimpleClusterPolicySnapshotKeySchedulingQueue()
	sq.Run()

	keysToAdd := []ClusterPolicySnapshotKey{"A", "B", "C", "D", "E"}
	for _, key := range keysToAdd {
		sq.Add(key)
	}

	keysRecved := []ClusterPolicySnapshotKey{}
	for i := 0; i < len(keysToAdd); i++ {
		key := sq.NextClusterPolicySnapshotKey()
		keysRecved = append(keysRecved, key)
		sq.Done(key)
	}

	if !cmp.Equal(keysToAdd, keysRecved) {
		t.Fatalf("Received keys %v, want %v", keysRecved, keysToAdd)
	}

	sq.Close()
}
