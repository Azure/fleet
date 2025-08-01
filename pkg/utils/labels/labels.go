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

// Package labels provides utils related to object labels.
package labels

import (
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// ExtractResourceIndexFromResourceSnapshot extracts the resource index from the label of a type of resourceSnapshot.
func ExtractResourceIndexFromResourceSnapshot(snapshot client.Object) (int, error) {
	return ExtractIndex(snapshot, fleetv1beta1.ResourceIndexLabel)
}

// ExtractResourceSnapshotIndexFromWork extracts the resource snapshot index from the work.
func ExtractResourceSnapshotIndexFromWork(work client.Object) (int, error) {
	return ExtractIndex(work, fleetv1beta1.ParentResourceSnapshotIndexLabel)
}

// ExtractIndex extracts the numeric index from the a label with labelKey.
func ExtractIndex(object client.Object, labelKey string) (int, error) {
	indexStr := object.GetLabels()[labelKey]
	v, err := strconv.Atoi(indexStr)
	if err != nil || v < 0 {
		return -1, fmt.Errorf("invalid resource index %q, error: %w", indexStr, err)
	}
	return v, nil
}

// ParsePolicyIndexFromLabel extracts and validates the policy index from a ClusterSchedulingPolicySnapshot label.
// Works with both ClusterSchedulingPolicySnapshot and PolicySnapshot interfaces.
func ParsePolicyIndexFromLabel(policySnapshot client.Object) (int, error) {
	labels := policySnapshot.GetLabels()
	if labels == nil {
		return -1, fmt.Errorf("no labels found on policy snapshot")
	}

	indexLabel := labels[fleetv1beta1.PolicyIndexLabel]
	v, err := strconv.Atoi(indexLabel)
	if err != nil || v < 0 {
		return -1, fmt.Errorf("invalid policy index %q, error: %w", indexLabel, err)
	}
	return v, nil
}
