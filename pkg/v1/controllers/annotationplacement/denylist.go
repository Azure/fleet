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

package annotationplacement

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// isObjectBlockedForAnnotationBasedPlacement blocks admission policies and their bindings in
// every API version. They can still be placed through explicitly authored placement policies.
func isObjectBlockedForAnnotationBasedPlacement(obj *unstructured.Unstructured) bool {
	gvk := obj.GroupVersionKind()
	if gvk.Group != admissionregistrationv1.GroupName {
		return false
	}
	switch gvk.Kind {
	case "ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding",
		"MutatingAdmissionPolicy", "MutatingAdmissionPolicyBinding":
		return true
	default:
		return false
	}
}
