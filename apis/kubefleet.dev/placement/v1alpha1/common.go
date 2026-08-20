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

package v1alpha1

type ObjectReference struct {
	// The namespace of the referenced object.
	//
	// If the object is cluster-scoped, this field should be left empty.
	//
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// The name of the referenced object.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// The API group, version, and kind of the referenced object.

	// +kubebuilder:validation:Optional
	APIGroup string `json:"apiGroup,omitempty"`

	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion,omitempty"`

	// +kubebuilder:validation:Required
	Kind string `json:"kind,omitempty"`
}
