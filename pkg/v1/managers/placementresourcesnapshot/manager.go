/*
Copyright 2026 The KubeFleet Authors.

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

// Package placementresourcesnapshot provides the manager for handling placement resource snapshots in KubeFleet.
package placementresourcesnapshot

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	errors "go.goms.io/fleet/pkg/utils/errors"
	"go.goms.io/fleet/pkg/utils/informer"
	"go.goms.io/fleet/pkg/utils/resourceeligibility"
)

const (
	managerName = "placementresourcesnapshot"

	defaultRevisionHistoryLimit    = int32(3)
	snapshotGCRateLimiterBaseDelay = time.Second
	snapshotGCRateLimiterMaxDelay  = 600 * time.Second
)

var (
	_ manager.Runnable = (*Manager)(nil)
)

const (
	// The format of the key used to find the mutex for a placement policy in the mutex array.
	//
	// Note that slashes are used to avoid unexpected collisions.
	placementPolicyKeyFmt = "%s/%s"

	minSlotCnt = 256
)

type Manager struct {
	hubClient                 client.Client
	hubUncachedReader         client.Reader
	hubDynamicClient          dynamic.Interface
	hubDynamicInformerManager informer.Manager

	resourceEligibilityChecker resourceeligibility.Checker

	gcwq workqueue.TypedRateLimitingInterface[snapshotGarbageCollectionRequest]

	restMapper meta.RESTMapper

	mus       []sync.Mutex
	muSlotCnt uint32

	maxPerSnapshotResourceDataSizeBytes int
	maxPerSnapshotResourceCnt           int
}

// New returns a new Manager.
func New(mgr ctrl.Manager,
	hubDynamicClient dynamic.Interface,
	hubDynamicInformerManager informer.Manager,
	resourceEligibilityChecker resourceeligibility.Checker,
	restMapper meta.RESTMapper,
	muSlotCnt int32,
	maxPerSnapshotResourceDataSizeBytes int,
	maxPerSnapshotResourceCnt int,
) (*Manager, error) {
	if muSlotCnt < minSlotCnt {
		return nil, errors.NewUserError(nil, "mu slot size must be greater than or equal to the minimum limit",
			"manager", managerName, "limit", minSlotCnt, "actual", muSlotCnt)
	}

	if maxPerSnapshotResourceDataSizeBytes < MinPerSnapshotResourceDataSizeBytes || maxPerSnapshotResourceDataSizeBytes > MaxPerSnapshotResourceDataSizeBytes {
		return nil, errors.NewUserError(nil, "an inappropriate max per snapshot resource data size is set",
			"manager", managerName, "limitRange", fmt.Sprintf("%d~%d", MinPerSnapshotResourceDataSizeBytes, MaxPerSnapshotResourceDataSizeBytes), "actual", maxPerSnapshotResourceDataSizeBytes)
	}
	if maxPerSnapshotResourceCnt < MinPerSnapshotResourceCnt || maxPerSnapshotResourceCnt > MaxPerSnapshotResourceCnt {
		return nil, errors.NewUserError(nil, "an inappropriate max per snapshot resource count is set",
			"manager", managerName, "limitRange", fmt.Sprintf("%d~%d", MinPerSnapshotResourceCnt, MaxPerSnapshotResourceCnt), "actual", maxPerSnapshotResourceCnt)
	}

	if resourceEligibilityChecker == nil {
		return nil, errors.NewUnexpectedError(nil, "a resource eligibility checker must be set",
			"manager", managerName)
	}

	// Set up the resource snapshot GC workqueue.
	//
	// The work queue uses an exponential backoff rate limiter (power of 2, starting at 1 second and capped
	// at 600 seconds).
	gcwq := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.NewTypedItemExponentialFailureRateLimiter[snapshotGarbageCollectionRequest](snapshotGCRateLimiterBaseDelay, snapshotGCRateLimiterMaxDelay),
		workqueue.TypedRateLimitingQueueConfig[snapshotGarbageCollectionRequest]{
			Name: "placementresourcesnapshotmanager-garbage-collection",
		})

	return &Manager{
		hubClient:                           mgr.GetClient(),
		hubUncachedReader:                   mgr.GetAPIReader(),
		hubDynamicClient:                    hubDynamicClient,
		hubDynamicInformerManager:           hubDynamicInformerManager,
		resourceEligibilityChecker:          resourceEligibilityChecker,
		gcwq:                                gcwq,
		restMapper:                          restMapper,
		mus:                                 make([]sync.Mutex, muSlotCnt),
		muSlotCnt:                           uint32(muSlotCnt),
		maxPerSnapshotResourceDataSizeBytes: maxPerSnapshotResourceDataSizeBytes,
		maxPerSnapshotResourceCnt:           maxPerSnapshotResourceCnt,
	}, nil
}

// Start starts the GC process for the placement resource snapshots.
func (m *Manager) Start(ctx context.Context) error {
	var wg sync.WaitGroup

	// Start a goroutine to shutdown the GC workqueue when the main context is canceled.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		m.gcwq.ShutDown()
	}()

	// Start a goroutine to process garbage collection of stale resource snapshots.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			snapshotGCRequest, shutdown := m.gcwq.Get()
			if shutdown {
				return
			}
			if ctx.Err() != nil {
				// The Get() method signals that the workqueue has been shut down only when the queue is empty.
				// Here we just short-circuit the processing loop if the context has been canceled.
				m.gcwq.Done(snapshotGCRequest)
				return
			}

			if err := m.garbageCollect(ctx, snapshotGCRequest); err != nil {
				wrappedErr := errors.Wraps(err, "", "snapshotGarbageCollectionRequest", snapshotGCRequest)
				klog.ErrorS(wrappedErr, "Failed to garbage collect placement resource snapshot", errors.Args(wrappedErr)...)
				m.gcwq.Done(snapshotGCRequest)

				// No need to requeue if the manager sees an unexpected error.
				if errors.Category(err) == errors.ErrCategoryUnexpected {
					m.gcwq.Forget(snapshotGCRequest)
					continue
				}
				m.gcwq.AddRateLimited(snapshotGCRequest)
				continue
			}
			m.gcwq.Done(snapshotGCRequest)
			m.gcwq.Forget(snapshotGCRequest)
		}
	}()

	wg.Wait()
	return nil
}

// acquireLock acquires a mutex for a given placement policy.
//
// The placement resource snapshot manager features a slot-based locking mechanism to ensure that KubeFleet always
// snapshots resources for one placement policy at a time. A fixed number of slots are assigned when the manager
// is initialized. There might be a small chance where two placement policies need to contend for the same slot.
//
// Slots are used to avoid GC complications.
func (m *Manager) acquireLock(placementPolicy placementv1alpha1.PlacementPolicyAccessor) {
	placementPolicyKey := fmt.Sprintf(placementPolicyKeyFmt, placementPolicy.GetNamespace(), placementPolicy.GetName())

	hasher := fnv.New32a()
	hasher.Write([]byte(placementPolicyKey))

	slot := int(hasher.Sum32() % m.muSlotCnt)
	m.mus[slot].Lock()
}

// releaseLock releases the mutex for a given placement policy.
func (m *Manager) releaseLock(placementPolicy placementv1alpha1.PlacementPolicyAccessor) {
	placementPolicyKey := fmt.Sprintf(placementPolicyKeyFmt, placementPolicy.GetNamespace(), placementPolicy.GetName())

	hasher := fnv.New32a()
	hasher.Write([]byte(placementPolicyKey))

	slot := int(hasher.Sum32() % m.muSlotCnt)
	m.mus[slot].Unlock()
}
