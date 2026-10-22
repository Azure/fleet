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
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsObjectBlockedForAnnotationBasedPlacement(t *testing.T) {
	for _, kind := range []string{
		"ValidatingAdmissionPolicy",
		"ValidatingAdmissionPolicyBinding",
		"MutatingAdmissionPolicy",
		"MutatingAdmissionPolicyBinding",
		"ValidatingWebhookConfiguration",
		"MutatingWebhookConfiguration",
		"Deployment",
	} {
		for _, group := range []string{admissionregistrationv1.GroupName, "example.com", ""} {
			for _, version := range []string{"v1alpha1", "v1beta1", "v1"} {
				gvk := schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
				t.Run(gvk.String(), func(t *testing.T) {
					obj := &unstructured.Unstructured{}
					obj.SetGroupVersionKind(gvk)
					want := group == admissionregistrationv1.GroupName &&
						(kind == "ValidatingAdmissionPolicy" || kind == "ValidatingAdmissionPolicyBinding" ||
							kind == "MutatingAdmissionPolicy" || kind == "MutatingAdmissionPolicyBinding")
					if got := isObjectBlockedForAnnotationBasedPlacement(obj); got != want {
						t.Errorf("isObjectBlockedForAnnotationBasedPlacement(%v) = %v, want %v", gvk, got, want)
					}
				})
			}
		}
	}
}
