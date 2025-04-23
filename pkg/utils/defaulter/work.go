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

// Package defaulter sets default values for the fleet resources.
package defaulter

import placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

// SetDefaultsWork sets the default values for the Work resource.
func SetDefaultsWork(w *placementv1beta1.Work) {
	if w.Spec.ApplyStrategy == nil {
		w.Spec.ApplyStrategy = &placementv1beta1.ApplyStrategy{}
	}
	SetDefaultsApplyStrategy(w.Spec.ApplyStrategy)
}
