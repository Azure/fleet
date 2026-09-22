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
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	errors "go.goms.io/fleet/pkg/utils/errors"
)

func (m *Manager) primaryPlacementResourceSnapshotExistsAtIdx(
	ctx context.Context,
	placementPolicy placementv1alpha1.PlacementPolicyAccessor,
	idx int) (bool, error) {
	name := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), idx)
	namespace := placementPolicy.GetNamespace()

	kind := placementv1alpha1.ClusterPlacementResourceSnapshotKind
	if namespace != "" {
		kind = placementv1alpha1.PlacementResourceSnapshotKind
	}

	// A metadata-only read; the snapshot spec may be large and is not needed here.
	metadata := metav1.PartialObjectMetadata{}
	metadata.SetGroupVersionKind(placementv1alpha1.GroupVersion.WithKind(kind))

	// Read from the API server directly, bypassing the (possibly stale) cache.
	if err := m.hubUncachedReader.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &metadata); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, errors.NewAPIServerError(err,
			"failed to get the partial object metadata of the primary placement resource snapshot", false,
			"primaryPlacementResourceSnapshotName", name, "snapshotIndex", idx)
	}
	return true, nil
}
