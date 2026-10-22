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

package admissionpolicymanager

import (
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	AnnotatedForPlacementObjectsVAPGeneratorName = "DenyPlacementAnnotationIfNoPlacementPolicyAPIAccess"
)

const (
	annotatedForPlacementObjsVAPPolicyName        = "fleet-builtin-deny-placement-annotation-if-no-placement-policy"
	annotatedForPlacementObjsVAPPolicyBindingName = "fleet-builtin-deny-placement-annotation-if-no-placement-policy-binding"
)

const (
	annotatedForPlacementObjsHasAnnotationMatchCondName            = "has-placement-annotation"
	annotatedForPlacementObjsAnnotationAddedOrChangedMatchCondName = "placement-annotation-added-or-changed"

	placementPoliciesResourceName        = "placementpolicies"
	clusterPlacementPoliciesResourceName = "clusterplacementpolicies"

	authzVerbCreate = "create"
	authzVerbUpdate = "update"
)

// Verify that AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator implements
// the ValidatingAdmissionPolicyGenerator interface.
var _ ValidatingAdmissionPolicyGenerator = &AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator{}

// AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator generates a ValidatingAdmissionPolicy
// and its binding that denies the addition of the KubeFleet placement annotation onto objects if the caller
// does not have the permission to create/update a (cluster) placement policy in the cluster/namespace.
//
// The generator has no configurable fields; to enable it in the admission policy manager configuration,
// set it to an empty struct (e.g., `denyPlacementAnnotationIfNoPlacementPolicyAPIAccess: {}` in YAML).
// Leaving the value empty (null) disables the generator.
type AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator struct{}

// Name returns the name of the validating admission policy generator, which is used to determine if
// a specific generator has been enabled or not.
func (g *AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator) Name() string {
	return AnnotatedForPlacementObjectsVAPGeneratorName
}

// Validate validates the configuration of the validating admission policy generator.
func (g *AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator) Validate() error {
	return nil
}

// PoliciesWithBindings generates a ValidatingAdmissionPolicy and its policy binding that denies the addition
// of the KubeFleet placement annotation (kubefleet.dev/cluster-selectors) onto objects if the caller does not
// have the permission to create/update a (cluster) placement policy in the cluster/namespace.
//
// The policy covers the creation of objects with the annotation, and the updates of objects (including those
// made through subresources) that add the annotation or change its value; updates that leave the annotation
// as it is (e.g., system controllers removing finalizers or writing status) are not subject to the check,
// so that they will not be blocked.
//
// For simplicity reasons, the code here assumes that the generator has been validated before
// PoliciesWithBindings() is called.
func (g *AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator) PoliciesWithBindings() ([]PolicyWithBindings, error) {
	annotationKey := kfplacementv1alpha1.ClusterSelectorsAnnotation
	placementAPIGroup := kfplacementv1alpha1.GroupVersion.Group

	// Match conditions are evaluated before the validations; the policy is skipped if any of
	// them evaluates to false. They help filter out (cheaply) requests that this policy is not
	// concerned with, as the policy matches all resources and their subresources.
	hasAnnotationExpr, err := objectHasAnnotation(annotationKey).Build()
	if err != nil {
		return nil, errors.Wraps(err, "failed to build CEL expression", "matchCondition", annotatedForPlacementObjsHasAnnotationMatchCondName)
	}

	// For CREATE requests, oldObject is always null and the condition is always true.
	//
	// The check on the object keeps the condition from failing with a no-such-key error when the annotation
	// is being removed; such requests are filtered out by the has-annotation condition.
	isAnnotationAddedOrChanged := LogicalNot(
		LogicalAnd(
			objectHasAnnotation(annotationKey),
			oldObjectHasAnnotation(annotationKey),
			annotationValueIsUnchanged(annotationKey),
		),
	)
	isAnnotationAddedOrChangedExpr, err := isAnnotationAddedOrChanged.Build()
	if err != nil {
		return nil, errors.Wraps(err, "failed to build CEL expression", "matchCondition", annotatedForPlacementObjsAnnotationAddedOrChangedMatchCondName)
	}

	// The annotation-based placement controller generates a PlacementPolicy object for namespaced objects,
	// and a ClusterPlacementPolicy object for cluster-scoped objects.
	isNamespaced := isNamespacedObject()

	canCreateAndUpdatePlacementPoliciesInNamespace := LogicalOr(
		LogicalNot(isNamespaced),
		LogicalAnd(
			callerCanInObjectNamespace(placementAPIGroup, placementPoliciesResourceName, authzVerbCreate),
			callerCanInObjectNamespace(placementAPIGroup, placementPoliciesResourceName, authzVerbUpdate),
		),
	)
	canCreateAndUpdatePlacementPoliciesInNamespaceExpr, err := canCreateAndUpdatePlacementPoliciesInNamespace.Build()
	if err != nil {
		return nil, errors.Wraps(err, "failed to build CEL expression", "resource", placementPoliciesResourceName)
	}

	canCreateAndUpdateClusterPlacementPolicies := LogicalOr(
		isNamespaced,
		LogicalAnd(
			callerCanClusterWide(placementAPIGroup, clusterPlacementPoliciesResourceName, authzVerbCreate),
			callerCanClusterWide(placementAPIGroup, clusterPlacementPoliciesResourceName, authzVerbUpdate),
		),
	)
	canCreateAndUpdateClusterPlacementPoliciesExpr, err := canCreateAndUpdateClusterPlacementPolicies.Build()
	if err != nil {
		return nil, errors.Wraps(err, "failed to build CEL expression", "resource", clusterPlacementPoliciesResourceName)
	}

	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: annotatedForPlacementObjsVAPPolicyName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: ptr.To(admissionregistrationv1.Fail),
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
								admissionregistrationv1.Update,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"*"},
								APIVersions: []string{"*"},
								// Match all resources and their subresources, as some built-in resources
								// (e.g., Deployments and Namespaces) accept annotation changes through
								// subresources such as status and finalize. Subresources that carry other
								// kinds of objects (e.g., scale and eviction) are filtered out by the
								// match conditions unless the object itself carries the annotation.
								Resources: []string{"*/*"},
							},
						},
					},
				},
			},
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       annotatedForPlacementObjsHasAnnotationMatchCondName,
					Expression: hasAnnotationExpr,
				},
				{
					Name:       annotatedForPlacementObjsAnnotationAddedOrChangedMatchCondName,
					Expression: isAnnotationAddedOrChangedExpr,
				},
			},
			Validations: []admissionregistrationv1.Validation{
				{
					Expression: canCreateAndUpdatePlacementPoliciesInNamespaceExpr,
					Message: fmt.Sprintf(
						"adding or changing the %s annotation on a namespaced object requires the permissions to create and update %s.%s in the namespace of the object",
						annotationKey, placementPoliciesResourceName, placementAPIGroup),
					Reason: ptr.To(metav1.StatusReasonForbidden),
				},
				{
					Expression: canCreateAndUpdateClusterPlacementPoliciesExpr,
					Message: fmt.Sprintf(
						"adding or changing the %s annotation on a cluster-scoped object requires the permissions to create and update %s.%s",
						annotationKey, clusterPlacementPoliciesResourceName, placementAPIGroup),
					Reason: ptr.To(metav1.StatusReasonForbidden),
				},
			},
		},
	}

	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: annotatedForPlacementObjsVAPPolicyBindingName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: annotatedForPlacementObjsVAPPolicyName,
			ValidationActions: []admissionregistrationv1.ValidationAction{
				admissionregistrationv1.Deny,
			},
		},
	}

	return []PolicyWithBindings{
		{
			Policy:   policy,
			Bindings: []*admissionregistrationv1.ValidatingAdmissionPolicyBinding{binding},
		},
	}, nil
}
