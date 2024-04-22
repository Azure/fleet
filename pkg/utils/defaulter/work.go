/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package defaulter sets default values for the fleet resources.
package defaulter

import placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

// SetDefaultsWork sets the default values for the Work resource.
func SetDefaultsWork(w *placementv1beta1.Work) {
	if w.Spec.ApplyStrategy == nil {
		w.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{}
	}

	if w.Spec.ApplyStrategy.Type == "" {
		w.Spec.ApplyStrategy.Type = placementv1beta1.ApplyStrategyTypeClientSideApply
	}

	if w.Spec.ApplyStrategy.Type == placementv1beta1.ApplyStrategyTypeServerSideApply && w.Spec.ApplyStrategy.ServerSideApplyConfig == nil {
		w.Spec.ApplyStrategy.ServerSideApplyConfig = &placementv1beta1.ServerSideApplyConfig{
			ForceConflicts: false,
		}
	}
}
