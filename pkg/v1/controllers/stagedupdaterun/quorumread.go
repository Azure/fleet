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

package stagedupdaterun

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

// placementBindingResourceSnapshotChanged reads the placement binding straight from the API server, and reports
// whether the resource snapshot name in its spec differs from a given desired resource snapshot name.
//
// A binding that no longer exists is reported as an API server error; the returned error wraps the original one,
// so callers can check it with apierrors.IsNotFound.
func (r *Reconciler) placementBindingResourceSnapshotChanged(
	ctx context.Context,
	placementBinding placementv1alpha1.PlacementBindingAccessor,
	wantResourceSnapshotName string,
) (bool, placementv1alpha1.PlacementBindingAccessor, error) {
	var latestBinding placementv1alpha1.PlacementBindingAccessor
	if placementBinding.GetNamespace() == "" {
		latestBinding = &placementv1alpha1.ClusterPlacementBinding{}
	} else {
		latestBinding = &placementv1alpha1.PlacementBinding{}
	}

	// Read from the API server directly, bypassing the (possibly stale) cache.
	key := types.NamespacedName{Namespace: placementBinding.GetNamespace(), Name: placementBinding.GetName()}
	if err := r.hubUncachedClient.Get(ctx, key, latestBinding); err != nil {
		return false, nil, errors.NewAPIServerError(err, "failed to get the placement binding", false,
			"placementBinding", klog.KObj(placementBinding))
	}
	return latestBinding.GetSpec().ResourceSnapshotName != wantResourceSnapshotName, latestBinding, nil
}
