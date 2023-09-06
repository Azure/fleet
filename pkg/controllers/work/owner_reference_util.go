/*
Copyright 2021 The Kubernetes Authors.

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

package work

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// addOwnerRef creates or inserts the owner reference to the object
func addOwnerRef(ref metav1.OwnerReference, object metav1.Object) {
	owners := object.GetOwnerReferences()
	if idx := indexOwnerRef(owners, ref); idx == -1 {
		owners = append(owners, ref)
	} else {
		owners[idx] = ref
	}
	object.SetOwnerReferences(owners)
}

// mergeOwnerReference merges two owner reference arrays.
func mergeOwnerReference(owners, newOwners []metav1.OwnerReference) []metav1.OwnerReference {
	for _, newOwner := range newOwners {
		if idx := indexOwnerRef(owners, newOwner); idx == -1 {
			owners = append(owners, newOwner)
		} else {
			owners[idx] = newOwner
		}
	}
	return owners
}

// indexOwnerRef returns the index of the owner reference in the slice if found, or -1.
func indexOwnerRef(ownerReferences []metav1.OwnerReference, ref metav1.OwnerReference) int {
	for index, r := range ownerReferences {
		if isReferSameObject(r, ref) {
			return index
		}
	}
	return -1
}

// isReferSameObject returns true if a and b point to the same object.
func isReferSameObject(a, b metav1.OwnerReference) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		return false
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		return false
	}

	return aGV.Group == bGV.Group && aGV.Version == bGV.Version && a.Kind == b.Kind && a.Name == b.Name
}
