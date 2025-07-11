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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/queue"
)

// FetchBindingFromKey resolves a bindingKey to a concrete binding object that implements BindingObj.
func FetchBindingFromKey(ctx context.Context, c client.Reader, bindingKey queue.PlacementKey) (placementv1beta1.BindingObj, error) {
	// Extract namespace and name from the placement key
	namespace, name, err := ExtractNamespaceNameFromKey(bindingKey)
	if err != nil {
		return nil, err
	}
	// Check if the key contains a namespace separator
	if namespace != "" {
		// This is a namespaced ResourceBinding
		rb := &placementv1beta1.ResourceBinding{}
		key := types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}
		if err := c.Get(ctx, key, rb); err != nil {
			return nil, err
		}
		return rb, nil
	} else {
		// This is a cluster-scoped ClusterResourceBinding
		crb := &placementv1beta1.ClusterResourceBinding{}
		key := types.NamespacedName{
			Name: name,
		}
		if err := c.Get(ctx, key, crb); err != nil {
			return nil, err
		}
		return crb, nil
	}
}

// ListBindingsFromKey returns all bindings (either ClusterResourceBinding and ResourceBinding)
// that belong to the specified placement key.
// The placement key format determines whether to list ClusterResourceBindings (cluster-scoped)
// or ResourceBindings (namespaced). For namespaced resources, the key format is "namespace/name".
func ListBindingsFromKey(ctx context.Context, c client.Reader, placementKey queue.PlacementKey) ([]placementv1beta1.BindingObj, error) {
	// Extract namespace and name from the placement key
	namespace, name, err := ExtractNamespaceNameFromKey(placementKey)
	if err != nil {
		return nil, fmt.Errorf("failed to list binding for a placement: %w", err)
	}
	var bindingList placementv1beta1.BindingObjList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		placementv1beta1.CRPTrackingLabel: name,
	})
	// Check if the key contains a namespace separator
	if namespace != "" {
		// This is a namespaced binding
		bindingList = &placementv1beta1.ResourceBindingList{}
		listOptions = append(listOptions, client.InNamespace(namespace))
	} else {
		bindingList = &placementv1beta1.ClusterResourceBindingList{}
	}
	if err := c.List(ctx, bindingList, listOptions...); err != nil {
		return nil, NewAPIServerError(false, err)
	}

	return bindingList.GetBindingObjs(), nil
}

// ConvertCRBObjsToBindingObjs converts a slice of ClusterResourceBinding items to BindingObj array.
// This helper is needed when working with List.Items which are value types, not pointers.
func ConvertCRBObjsToBindingObjs(bindings []placementv1beta1.ClusterResourceBinding) []placementv1beta1.BindingObj {
	if len(bindings) == 0 {
		return nil
	}

	result := make([]placementv1beta1.BindingObj, len(bindings))
	for i := range bindings {
		result[i] = &bindings[i]
	}
	return result
}

// ConvertCRBArrayToBindingObjs converts an array of ClusterResourceBinding pointers to BindingObj interfaces.
// This is the common helper function for converting concrete types to interfaces in the scheduler package.
func ConvertCRBArrayToBindingObjs(bindings []*placementv1beta1.ClusterResourceBinding) []placementv1beta1.BindingObj {
	if len(bindings) == 0 {
		return nil
	}
	result := make([]placementv1beta1.BindingObj, len(bindings))
	for i, binding := range bindings {
		result[i] = binding
	}
	return result
}

// ConvertCRB2DArrayToBindingObjs converts a 2D array of ClusterResourceBinding pointers to BindingObj interfaces.
// This is used for nested binding arrays in test cases.
func ConvertCRB2DArrayToBindingObjs(bindingSets [][]*placementv1beta1.ClusterResourceBinding) [][]placementv1beta1.BindingObj {
	if len(bindingSets) == 0 {
		return nil
	}
	result := make([][]placementv1beta1.BindingObj, len(bindingSets))
	for i, bindings := range bindingSets {
		result[i] = ConvertCRBArrayToBindingObjs(bindings)
	}
	return result
}
