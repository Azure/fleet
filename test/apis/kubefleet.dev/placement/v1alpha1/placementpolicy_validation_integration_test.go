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

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	placementPolicyNameTemplate = "test-placement-policy-%d"

	tolerationKey   = "foo"
	tolerationValue = "bar"

	operatorMustBeExistsMsg = "operator must be Exists when key is empty"
	valueMustBeEmptyMsg     = "value must be empty when operator is Exists"
)

// testResourceSelectors is a resource selector list that is valid for both the
// PlacementPolicy and the ClusterPlacementPolicy APIs.
var testResourceSelectors = []placementv1alpha1.ResourceSelector{
	{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "test-cm",
	},
}

// createAndCheckValidation creates the given object and verifies that the API server accepts it
// (when wantMessage is empty) or rejects it with an error that contains wantMessage. Accepted
// objects are deleted when the current spec completes.
func createAndCheckValidation(obj client.Object, wantMessage string) {
	GinkgoHelper()

	err := hubClient.Create(ctx, obj)
	if wantMessage == "" {
		Expect(err).To(Succeed(), "Expected the object to be accepted")
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(hubClient.Delete(ctx, obj))).To(Succeed())
		})
		return
	}
	Expect(err).To(HaveOccurred(), "Expected the object to be rejected")
	var statusErr *k8sErrors.StatusError
	Expect(errors.As(err, &statusErr)).To(BeTrue(), "The returned error is not a StatusError")
	Expect(statusErr.Status().Message).Should(ContainSubstring(wantMessage))
}

var _ = Describe("Test PlacementPolicy API validation", func() {
	Context("Test toleration validation", func() {
		// PlacementPolicy and ClusterPlacementPolicy share the same spec type, so each case
		// runs against both kinds.
		DescribeTable("the tolerations of a placement policy",
			func(toleration placementv1alpha1.Toleration, wantMessage string) {
				name := fmt.Sprintf(placementPolicyNameTemplate, GinkgoParallelProcess())
				spec := placementv1alpha1.PlacementPolicySpec{
					ResourceSelectors: testResourceSelectors,
					Tolerations:       []placementv1alpha1.Toleration{toleration},
				}

				createAndCheckValidation(&placementv1alpha1.PlacementPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: metav1.NamespaceDefault,
					},
					Spec: *spec.DeepCopy(),
				}, wantMessage)
				createAndCheckValidation(&placementv1alpha1.ClusterPlacementPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: *spec.DeepCopy(),
				}, wantMessage)
			},
			Entry("an Exists toleration without a key or a value (tolerate all) is accepted", placementv1alpha1.Toleration{
				Operator: corev1.TolerationOpExists,
			}, ""),
			Entry("an Exists toleration with a key is accepted", placementv1alpha1.Toleration{
				Key:      tolerationKey,
				Operator: corev1.TolerationOpExists,
			}, ""),
			Entry("an Equal toleration with a key and a value is accepted", placementv1alpha1.Toleration{
				Key:      tolerationKey,
				Operator: corev1.TolerationOpEqual,
				Value:    tolerationValue,
			}, ""),
			Entry("a toleration with only a key (operator defaults to Equal) is accepted", placementv1alpha1.Toleration{
				Key: tolerationKey,
			}, ""),
			Entry("an empty toleration (operator defaults to Equal) is rejected", placementv1alpha1.Toleration{}, operatorMustBeExistsMsg),
			Entry("an Equal toleration without a key is rejected", placementv1alpha1.Toleration{
				Operator: corev1.TolerationOpEqual,
				Value:    tolerationValue,
			}, operatorMustBeExistsMsg),
			Entry("an Exists toleration with a key and a value is rejected", placementv1alpha1.Toleration{
				Key:      tolerationKey,
				Operator: corev1.TolerationOpExists,
				Value:    tolerationValue,
			}, valueMustBeEmptyMsg),
			Entry("an Exists toleration with a value but no key is rejected", placementv1alpha1.Toleration{
				Operator: corev1.TolerationOpExists,
				Value:    tolerationValue,
			}, valueMustBeEmptyMsg),
		)

		// The typed client drops empty keys and values (omitempty), so these cases use unstructured
		// objects to send them explicitly, as a manifest applied with kubectl might.
		DescribeTable("the explicitly empty fields of a toleration",
			func(toleration map[string]interface{}, wantMessage string) {
				name := fmt.Sprintf(placementPolicyNameTemplate, GinkgoParallelProcess())
				for _, kind := range []string{placementv1alpha1.PlacementPolicyKind, placementv1alpha1.ClusterPlacementPolicyKind} {
					obj := &unstructured.Unstructured{
						Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"resourceSelectors": []interface{}{
									map[string]interface{}{
										"apiVersion": testResourceSelectors[0].APIVersion,
										"kind":       testResourceSelectors[0].Kind,
										"name":       testResourceSelectors[0].Name,
									},
								},
								"tolerations": []interface{}{toleration},
							},
						},
					}
					obj.SetGroupVersionKind(placementv1alpha1.GroupVersion.WithKind(kind))
					obj.SetName(name)
					if kind == placementv1alpha1.PlacementPolicyKind {
						obj.SetNamespace(metav1.NamespaceDefault)
					}
					createAndCheckValidation(obj, wantMessage)
				}
			},
			Entry("an Exists toleration with an empty key is accepted", map[string]interface{}{
				"key":      "",
				"operator": string(corev1.TolerationOpExists),
			}, ""),
			Entry("an Exists toleration with an empty value is accepted", map[string]interface{}{
				"key":      tolerationKey,
				"operator": string(corev1.TolerationOpExists),
				"value":    "",
			}, ""),
			Entry("an Equal toleration with an empty key is rejected", map[string]interface{}{
				"key":      "",
				"operator": string(corev1.TolerationOpEqual),
				"value":    tolerationValue,
			}, operatorMustBeExistsMsg),
		)
	})
})
