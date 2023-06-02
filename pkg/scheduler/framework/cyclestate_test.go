/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import "testing"

// TestCycleStateBasicOps tests the basic ops (Read, Write, and Delete) of a CycleState.
func TestCycleStateBasicOps(t *testing.T) {
	cs := NewCycleState()

	k, v := "key", "value"
	cs.Write(StateKey(k), StateValue(v))
	if out, err := cs.Read("key"); out != "value" || err != nil {
		t.Fatalf("Read(%v) = %v, %v, want %v, nil", k, out, err, v)
	}
	cs.Delete(StateKey(k))
	if out, err := cs.Read("key"); out != nil || err == nil {
		t.Fatalf("Read(%v) = %v, %v, want nil, not found error", k, out, err)
	}
}
