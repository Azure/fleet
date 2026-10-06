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

package workapplier

import (
	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// setDefaultSyncStrategy fills in a default sync strategy on a work object that carries none.
//
// The individual fields of a sync strategy are defaulted by the API server; this only covers the case where
// the sync strategy itself is absent, which leaves those defaults unapplied.
func setDefaultSyncStrategy(work *placementv1alpha1.Work) {
	if work.Spec.SyncStrategy != nil {
		if work.Spec.SyncStrategy.ApplyMethod == placementv1alpha1.ApplyMethodServerSideApply &&
			work.Spec.SyncStrategy.ServerSideApplyOptions == nil {
			work.Spec.SyncStrategy.ServerSideApplyOptions = &placementv1alpha1.ServerSideApplyOptions{
				ForceConflicts: false,
			}
		}
		return
	}

	work.Spec.SyncStrategy = &placementv1alpha1.SyncStrategy{
		ApplyMethod:               placementv1alpha1.ApplyMethodClientSideApply,
		WhenOwnedByOthers:         placementv1alpha1.WhenOwnedByOthersOptionReportError,
		WhenDrifted:               placementv1alpha1.WhenDriftedOptionApplyAnyway,
		WhenAlreadyExists:         placementv1alpha1.WhenAlreadyExistsOptionReportError,
		WhenPlacementDeleted:      placementv1alpha1.WhenPlacementDeletedOptionCleanUpResources,
		WhenNamespaceDoesNotExist: placementv1alpha1.WhenNamespaceDoesNotExistOptionCreateNamespace,
		ComparisonOption:          placementv1alpha1.ComparisonOptionPartialComparison,
	}
}
