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

// Package queue features a scheduling queue, which keeps track of all placements for the scheduler
// to schedule.
package queue

import (
	"time"
)

// PlacementKey is the unique identifier for a Placement checked into a scheduling queue.
// If the Placement is a ClusterResourcePlacement, the PlacementKey is the
// ClusterResourcePlacement's name in the form of "name".
// If the Placement is a NamespaceResourcePlacement, the PlacementKey is the
// NamespaceResourcePlacement's name in the form of "namespace/name".
// TODO: rename this to be ObjectKey or something similar, as it is not specific to Placement.
type PlacementKey string

// PlacementSchedulingQueueWriter is an interface which allows sources, such as controllers, to add
// PlacementKeys to the scheduling queue.
type PlacementSchedulingQueueWriter interface {
	// Add adds a PlacementKey to the work queue.
	//
	// Note that this bypasses the rate limiter.
	Add(placementKey PlacementKey)
	// AddRateLimited adds a PlacementKey to the work queue after the rate limiter (if any)
	// says that it is OK.
	AddRateLimited(placementKey PlacementKey)
	// AddAfter adds a PlacementKey to the work queue after a set duration.
	AddAfter(placementKey PlacementKey, duration time.Duration)
	// AddBatched tracks a PlacementKey and adds such keys in batch later to the work queue when appropriate.
	//
	// This is most helpful in cases where certain changes do not require immediate processing by the scheduler.
	AddBatched(placementKey PlacementKey)
}

// PlacementSchedulingQueue is an interface which queues PlacementKeys for the scheduler
// to consume; specifically, the scheduler finds the latest scheduling policy snapshot associated with the
// PlacementKey.
type PlacementSchedulingQueue interface {
	PlacementSchedulingQueueWriter

	// Run starts the scheduling queue.
	Run()
	// Close closes the scheduling queue immediately.
	Close()
	// CloseWithDrain closes the scheduling queue after all items in the queue are processed.
	CloseWithDrain()
	// NextPlacementKey returns the next-in-line PlacementKey for the scheduler to consume.
	NextPlacementKey() (key PlacementKey, closed bool)
	// Done marks a PlacementKey as done.
	Done(placementKey PlacementKey)
	// Forget untracks a PlacementKey from rate limiter(s) (if any) set up with the queue.
	Forget(placementKey PlacementKey)
}
