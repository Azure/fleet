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

	"github.com/google/go-cmp/cmp"
)

// TestSimplePlacementSchedulingQueue_BasicOps tests the basic ops
// (Add, Next, Done) of a simpleClusterResourcePlacementSchedulingQueue.
func TestSimplePlacementSchedulingQueue_BasicOps(t *testing.T) {
	sq := NewSimplePlacementSchedulingQueue("", nil)
	sq.Run()

	keysToAdd := []PlacementKey{"A", "B", "C", "D", "E"}
	for _, key := range keysToAdd {
		sq.Add(key)
	}

	keysRecved := []PlacementKey{}
	for i := 0; i < len(keysToAdd); i++ {
		key, closed := sq.NextPlacementKey()
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
