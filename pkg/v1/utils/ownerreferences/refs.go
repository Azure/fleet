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

// Package ownerreferences provides utilities for managing owner references of Kubernetes objects.
package ownerreferences

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Disown removes a specific owner reference from the given object.
//
// Note that this function will check for UID matches. To disown an object without checking for UIDs,
// call controllerutil.RemoveOwnerReference instead.
func Disown(obj *unstructured.Unstructured, owner *metav1.OwnerReference) {
	ownerRefs := obj.GetOwnerReferences()
	updatedOwnerRefs := make([]metav1.OwnerReference, 0, len(ownerRefs))

	// Re-build the owner references; remove the given one from the list.
	for idx := range ownerRefs {
		if AreEqual(&ownerRefs[idx], owner) {
			// Skip the expected owner reference.
			continue
		}
		updatedOwnerRefs = append(updatedOwnerRefs, ownerRefs[idx])
	}
	obj.SetOwnerReferences(updatedOwnerRefs)
}

// AreEqual checks if two owner references are equal by comparing their UID, Name, Kind, and APIVersion fields.
func AreEqual(a, b *metav1.OwnerReference) bool {
	return a.UID == b.UID &&
		a.Name == b.Name &&
		a.Kind == b.Kind &&
		a.APIVersion == b.APIVersion
}

// Own adds a specific owner reference to the given object if it is not already present.
func Own(obj *unstructured.Unstructured, owner *metav1.OwnerReference) {
	ownerRefs := obj.GetOwnerReferences()
	for idx := range ownerRefs {
		if AreEqual(&ownerRefs[idx], owner) {
			// The object is already owned by the given owner.
			return
		}
	}
	ownerRefs = append(ownerRefs, *owner)
	obj.SetOwnerReferences(ownerRefs)
}
