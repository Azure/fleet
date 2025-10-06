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
)

// FetchBindingFromKey resolves a NamespacedName to a concrete binding object that implements BindingObj.
func FetchBindingFromKey(ctx context.Context, c client.Reader, bindingKey types.NamespacedName) (placementv1beta1.BindingObj, error) {
	var binding placementv1beta1.BindingObj
	// If Namespace is empty, it's a cluster-scoped binding (ClusterResourceBinding)
	if bindingKey.Namespace == "" {
		binding = &placementv1beta1.ClusterResourceBinding{}
	} else {
		// Otherwise, it's a namespaced binding (ResourceBinding)
		binding = &placementv1beta1.ResourceBinding{}
	}
	err := c.Get(ctx, bindingKey, binding)
	if err != nil {
		return nil, err
	}
	return binding, nil
}

// ListBindingsFromKey returns all bindings (either ClusterResourceBinding and ResourceBinding)
// that belong to the specified binding key.
// The binding key format determines whether to list ClusterResourceBindings (cluster-scoped)
// or ResourceBindings (namespaced). For namespaced resources, the key format has a namespace.
func ListBindingsFromKey(ctx context.Context, c client.Reader, placementKey types.NamespacedName) ([]placementv1beta1.BindingObj, error) {
	// Extract namespace and name from the binding key
	namespace := placementKey.Namespace
	name := placementKey.Name
	var bindingList placementv1beta1.BindingObjList
	var listOptions []client.ListOption
	listOptions = append(listOptions, client.MatchingLabels{
		placementv1beta1.PlacementTrackingLabel: name,
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

// ConvertRBArrayToBindingObjs converts an array of ResourceBinding pointers to BindingObj interfaces.
// This is the common helper function for converting concrete types to interfaces in the scheduler package.
func ConvertRBArrayToBindingObjs(bindings []*placementv1beta1.ResourceBinding) []placementv1beta1.BindingObj {
	if len(bindings) == 0 {
		return nil
	}
	result := make([]placementv1beta1.BindingObj, len(bindings))
	for i, binding := range bindings {
		result[i] = binding
	}
	return result
}

// TODO(arvindth): remove method once we have updated updateRun controller to use interfaces.
// ConvertBindingObjsToConcreteCRBArray converts a slice of BindingObj interfaces to a slice of concrete ClusterResourceBinding pointers.
func ConvertBindingObjsToConcreteCRBArray(bindingObjs []placementv1beta1.BindingObj) ([]*placementv1beta1.ClusterResourceBinding, error) {
	bindings := make([]*placementv1beta1.ClusterResourceBinding, len(bindingObjs))

	for i, bindingObj := range bindingObjs {
		if crb, ok := bindingObj.(*placementv1beta1.ClusterResourceBinding); ok {
			bindings[i] = crb
		} else {
			return nil, fmt.Errorf("expected ClusterResourceBinding but got %T - initialize function needs further refactoring for namespace-scoped resources", bindingObj)
		}
	}
	return bindings, nil
}
