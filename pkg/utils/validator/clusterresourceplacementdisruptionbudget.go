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

// Package validator provides utils to validate ClusterResourcePlacementDisruptionBudget resources.
package validator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// ValidateClusterResourcePlacementDisruptionBudget validates cluster resource placement disruption budget fields based on crp placement type and returns error.
func ValidateClusterResourcePlacementDisruptionBudget(db *fleetv1beta1.ClusterResourcePlacementDisruptionBudget, crp *fleetv1beta1.ClusterResourcePlacement) error {
	allErr := make([]error, 0)

	// Check ClusterResourcePlacementDisruptionBudget fields if CRP is PickAll placement type
	if crp.Spec.Policy == nil || crp.Spec.Policy.PlacementType == fleetv1beta1.PickAllPlacementType {
		if db.Spec.MaxUnavailable != nil {
			allErr = append(allErr, fmt.Errorf("cluster resource placement policy type PickAll is not supported with any specified max unavailable %v", db.Spec.MaxUnavailable))
		}
		if db.Spec.MinAvailable != nil && db.Spec.MinAvailable.Type == intstr.String {
			allErr = append(allErr, fmt.Errorf("cluster resource placement policy type PickAll is not supported with min available as a percentage %v", db.Spec.MinAvailable))
		}
	}

	return errors.NewAggregate(allErr)
}
