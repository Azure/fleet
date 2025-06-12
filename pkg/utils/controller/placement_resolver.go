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

package controller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
)

const (
	// namespaceSeparator is the separator used between namespace and name in placement keys.
	namespaceSeparator = "/"
)

// FetchPlacementFromKey resolves a PlacementKey to a concrete placement object that implements PlacementObj.
func FetchPlacementFromKey(ctx context.Context, c client.Client, placementKey queue.PlacementKey) (fleetv1beta1.PlacementObj, error) {
	keyStr := string(placementKey)

	// Check if the key contains a namespace separator
	if strings.Contains(keyStr, namespaceSeparator) {
		// This is a namespaced ResourcePlacement
		parts := strings.Split(keyStr, namespaceSeparator)
		if len(parts) != 2 {
			return nil, NewUnexpectedBehaviorError(fmt.Errorf("invalid placement key format: %s", keyStr))
		}

		namespace := parts[0]
		name := parts[1]

		rp := &fleetv1beta1.ResourcePlacement{}
		key := types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}

		if err := c.Get(ctx, key, rp); err != nil {
			return nil, err
		}

		return rp, nil
	} else {
		if len(keyStr) == 0 {
			return nil, NewUnexpectedBehaviorError(fmt.Errorf("invalid placement key format: %s", keyStr))
		}
		// This is a cluster-scoped ClusterResourcePlacement
		crp := &fleetv1beta1.ClusterResourcePlacement{}
		key := types.NamespacedName{
			Name: keyStr,
		}

		if err := c.Get(ctx, key, crp); err != nil {
			return nil, err
		}

		return crp, nil
	}
}

// GetPlacementKeyFromObj generates a PlacementKey from a placement object.
func GetPlacementKeyFromObj(obj fleetv1beta1.PlacementObj) queue.PlacementKey {
	if obj.GetNamespace() == "" {
		// Cluster-scoped placement
		return queue.PlacementKey(obj.GetName())
	} else {
		// Namespaced placement
		return queue.PlacementKey(obj.GetNamespace() + namespaceSeparator + obj.GetName())
	}
}
