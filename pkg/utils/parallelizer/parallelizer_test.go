/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package parallelizer

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestParallelizer tests the basic ops of a Parallelizer.
func TestParallelizer(t *testing.T) {
	p := NewParallelizer(DefaultNumOfWorkers)
	nums := []int32{1, 2, 3, 4, 5, 6, 7, 8}

	var sum int32
	doWork := func(pieces int) {
		num := nums[pieces]
		atomic.AddInt32(&sum, num)
	}

	ctx := context.Background()
	p.ParallelizeUntil(ctx, len(nums), doWork, "test")
	if sum != 36 {
		t.Errorf("sum of nums, want %d, got %d", 36, sum)
	}
}
