/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package parallelizer features some utilities to help run tasks in parallel.
package parallelizer

import (
	"context"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	// The default number of workers.
	DefaultNumOfWorkers = 4
)

// Parallelizer helps run tasks in parallel.
type Parallerlizer struct {
	numOfWorkers int
}

// NewParallelizer returns a Parallelizer for running tasks in parallel.
func NewParallelizer(workers int) *Parallerlizer {
	return &Parallerlizer{
		numOfWorkers: workers,
	}
}

// ParallelizeUntil wraps workqueue.ParallelizeUntil for running tasks in parallel.
func (p *Parallerlizer) ParallelizeUntil(ctx context.Context, pieces int, doWork workqueue.DoWorkPieceFunc, operation string) {
	doWorkWithLogs := func(piece int) {
		klog.V(4).Infof("run piece %d for operation %s", operation, piece)
		doWork(piece)
		klog.V(4).Infof("completed piece %d for operation %s", operation, piece)
	}

	workqueue.ParallelizeUntil(ctx, p.numOfWorkers, pieces, doWorkWithLogs)
}
