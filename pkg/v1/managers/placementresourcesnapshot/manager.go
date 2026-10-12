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

package placementresourcesnapshot

import (
	"fmt"
	"hash/fnv"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	errors "go.goms.io/fleet/pkg/utils/errors"
	"go.goms.io/fleet/pkg/utils/informer"
)

const (
	managerName = "placementresourcesnapshot"
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

	restMapper meta.RESTMapper

	mus       []sync.Mutex
	muSlotCnt uint32
}

// New returns a new Manager.
func New(mgr ctrl.Manager,
	hubDynamicClient dynamic.Interface,
	hubDynamicInformerManager informer.Manager,
	restMapper meta.RESTMapper,
	muSlotCnt int32,
) (*Manager, error) {
	if muSlotCnt < minSlotCnt {
		return nil, errors.NewUserError(nil, "mu slot size must be greater than or equal to the minimum limit",
			"manager", managerName, "limit", minSlotCnt, "actual", muSlotCnt)
	}

	return &Manager{
		hubClient:                 mgr.GetClient(),
		hubUncachedReader:         mgr.GetAPIReader(),
		hubDynamicClient:          hubDynamicClient,
		hubDynamicInformerManager: hubDynamicInformerManager,
		restMapper:                restMapper,
		mus:                       make([]sync.Mutex, muSlotCnt),
		muSlotCnt:                 uint32(muSlotCnt),
	}, nil
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
