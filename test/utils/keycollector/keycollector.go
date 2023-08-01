/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package keycollector features a utility that is used for testing scheduler-related logic.
package keycollector

import (
	"context"
	"sync"

	"go.goms.io/fleet/pkg/scheduler/queue"
)

// SchedulerWorkqueueKeyCollector helps collect keys from a scheduler work queue for testing
// purposes.
type SchedulerWorkqueueKeyCollector struct {
	schedulerWorkqueue queue.ClusterResourcePlacementSchedulingQueue
	// Uses a mutex to guard against concurrent access; for simplicity reasons, the struct
	// uses a regular map rather than its currency safe variant.
	lock          sync.Mutex
	collectedKeys map[string]bool
}

// NewSchedulerWorkqueueKeyCollector returns a new SchedulerWorkqueueKeyCollector.
func NewSchedulerWorkqueueKeyCollector(wq queue.ClusterResourcePlacementSchedulingQueue) *SchedulerWorkqueueKeyCollector {
	return &SchedulerWorkqueueKeyCollector{
		schedulerWorkqueue: wq,
		collectedKeys:      make(map[string]bool),
	}
}

// Run runs the SchedulerWorkqueueKeyCollector.
func (kc *SchedulerWorkqueueKeyCollector) Run(ctx context.Context) {
	go func() {
		for {
			key, closed := kc.schedulerWorkqueue.NextClusterResourcePlacementKey()
			if closed {
				break
			}

			kc.lock.Lock()
			kc.collectedKeys[string(key)] = true
			kc.schedulerWorkqueue.Done(key)
			kc.schedulerWorkqueue.Forget(key)
			kc.lock.Unlock()
		}
	}()

	<-ctx.Done()
}

// IsPresent returns whether a given key is has been collected.
func (kc *SchedulerWorkqueueKeyCollector) IsPresent(keys ...string) (isAllPresent bool, absentKeys []string) {
	kc.lock.Lock()
	defer kc.lock.Unlock()

	absentKeys = make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := kc.collectedKeys[key]; !ok {
			absentKeys = append(absentKeys, key)
		}
	}
	return len(absentKeys) == 0, absentKeys
}

// Reset clears all the collected keys.
func (kc *SchedulerWorkqueueKeyCollector) Reset() {
	kc.lock.Lock()
	defer kc.lock.Unlock()

	kc.collectedKeys = make(map[string]bool)
}

// Len returns the count of collected keys.
func (kc *SchedulerWorkqueueKeyCollector) Len() int {
	kc.lock.Lock()
	defer kc.lock.Unlock()

	return len(kc.collectedKeys)
}
