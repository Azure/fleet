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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
)

const (
	// namespaceSeparator is the separator used between namespace and name in placement keys.
	namespaceSeparator = "/"
)

// FetchPlacementFromKey resolves a PlacementKey to a concrete placement object that implements PlacementObj.
func FetchPlacementFromKey(ctx context.Context, c client.Reader, placementKey queue.PlacementKey) (fleetv1beta1.PlacementObj, error) {
	// Extract namespace and name from the placement key
	namespace, name, err := ExtractNamespaceNameFromKey(placementKey)
	if err != nil {
		return nil, err
	}
	return FetchPlacementFromNamespacedName(ctx, c, types.NamespacedName{Namespace: namespace, Name: name})
}

// FetchPlacementFromNamespacedName resolves a NamespacedName to a concrete placement object that implements PlacementObj.
func FetchPlacementFromNamespacedName(ctx context.Context, c client.Reader, nn types.NamespacedName) (fleetv1beta1.PlacementObj, error) {
	// Check if the key contains a namespace separator
	var placement fleetv1beta1.PlacementObj
	if nn.Namespace != "" {
		// This is a namespaced ResourcePlacement
		placement = &fleetv1beta1.ResourcePlacement{}
	} else {
		// This is a cluster-scoped ClusterResourcePlacement
		placement = &fleetv1beta1.ClusterResourcePlacement{}
	}

	if err := c.Get(ctx, nn, placement); err != nil {
		return nil, err
	}
	return placement, nil
}

// GetObjectKeyFromObj generates a object Key from a meta object.
func GetObjectKeyFromObj(obj metav1.Object) queue.PlacementKey {
	if obj.GetNamespace() == "" {
		// Cluster-scoped placement
		return queue.PlacementKey(obj.GetName())
	} else {
		// Namespaced placement
		return queue.PlacementKey(obj.GetNamespace() + namespaceSeparator + obj.GetName())
	}
}

// GetObjectKeyFromRequest generates an object key from a controller runtime request.
func GetObjectKeyFromRequest(req ctrl.Request) queue.PlacementKey {
	if req.Namespace == "" {
		// Cluster-scoped placement
		return queue.PlacementKey(req.Name)
	} else {
		// Namespaced placement
		return queue.PlacementKey(req.Namespace + namespaceSeparator + req.Name)
	}
}

// GetObjectKeyFromNamespaceName generates a PlacementKey from a namespace and name.
func GetObjectKeyFromNamespaceName(namespace, name string) string {
	if namespace == "" {
		// Cluster-scoped placement
		return name
	} else {
		// Namespaced placement
		return namespace + namespaceSeparator + name
	}
}

// GetNamespacedNameFromObject generates a NamespacedName from a meta object.
func GetNamespacedNameFromObject(obj metav1.Object) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

// ExtractNamespaceNameFromKey resolves a PlacementKey to a (namespace, name) tuple of the placement object.
func ExtractNamespaceNameFromKey(key queue.PlacementKey) (string, string, error) {
	return ExtractNamespaceNameFromKeyStr(string(key))
}

// ExtractNamespaceNameFromKeyStr resolves a PlacementKey string to a (namespace, name) tuple of the placement object.
func ExtractNamespaceNameFromKeyStr(keyStr string) (string, string, error) {
	// Check if the key contains a namespace separator
	if strings.Contains(keyStr, namespaceSeparator) {
		// This is a namespaced ResourcePlacement
		parts := strings.Split(keyStr, namespaceSeparator)
		if len(parts) != 2 {
			return "", "", NewUnexpectedBehaviorError(fmt.Errorf("invalid placement key format: %s", keyStr))
		}
		if len(parts[0]) == 0 || len(parts[1]) == 0 {
			return "", "", NewUnexpectedBehaviorError(fmt.Errorf("empty placement key <namespace/name>: %s", keyStr))
		}
		return parts[0], parts[1], nil
	} else {
		if len(keyStr) == 0 {
			return "", "", NewUnexpectedBehaviorError(fmt.Errorf("empty placement key"))
		}
		// This is a cluster-scoped ClusterResourcePlacement
		return "", keyStr, nil
	}
}
