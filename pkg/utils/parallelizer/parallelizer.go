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
		klog.V(4).Infof("run piece %d for operation %s", piece, operation)
		doWork(piece)
		klog.V(4).Infof("completed piece %d for operation %s", piece, operation)
	}

	workqueue.ParallelizeUntil(ctx, p.numOfWorkers, pieces, doWorkWithLogs)
}
