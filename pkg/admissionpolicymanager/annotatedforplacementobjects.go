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
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kfplacementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/errors"
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

	endpointsKind = "Endpoints"

	authzVerbCreate = "create"
	authzVerbUpdate = "update"
)

// Verify that AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator implements
// the ValidatingAdmissionPolicyGenerator interface.
var _ ValidatingAdmissionPolicyGenerator = &AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator{}

// AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator generates a ValidatingAdmissionPolicy
// and its binding that denies the addition of the KubeFleet placement annotation onto objects if the caller
// does not have the permission to create/update a (cluster) placement policy in the cluster/namespace. In
// reserved namespaces, and on Endpoints objects, the annotation cannot be added at all, regardless of the caller.
//
// Important: this policy should only be used in cases where the workload controllers of kube-controller-manager
// (e.g., the Deployment, StatefulSet, DaemonSet, and CronJob controllers) have been disabled in the hub cluster;
// setting the hub agent option --enable-workload to false does not disable them. These controllers copy
// annotations from the objects they manage (or from the templates in them) onto the objects they create (e.g.,
// the Deployment controller onto ReplicaSets, and the StatefulSet and DaemonSet controllers onto
// ControllerRevisions); these writes are made by the controllers themselves, which have no access to the
// placement policy APIs, so the policy denies them and blocks the controllers.
//
// TO-DO (chenyu1): add a mutating admission policy (MAP) to complement this policy, so that all clusters,
// including those with workload controllers enabled, can have proper protection to use the annotation-based
// placement. The planned setup: this policy allows the annotation on objects with a controller owner reference
// (which the annotation-based placement never places), and the MAP removes the annotation from an object once
// its controller owner reference is removed (e.g., when the object is orphaned). MAPs are enabled by default
// in Kubernetes 1.36 and later.
type AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator struct {
	// ReservedNamespacePrefixes are the prefixes of the reserved namespaces. The annotation can never be
	// added to (or changed on) objects in these namespaces, or the namespaces themselves. The field is
	// optional; if no prefixes are specified, no namespaces are reserved.
	ReservedNamespacePrefixes []string
}

// Name returns the name of the validating admission policy generator, which is used to determine if
// a specific generator has been enabled or not.
func (g *AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator) Name() string {
	return AnnotatedForPlacementObjectsVAPGeneratorName
}

// Validate validates the configuration of the validating admission policy generator.
func (g *AnnotatedForPlacementObjectsValidatingAdmissionPolicyGenerator) Validate() error {
	// Check if any of the prefixes includes illegal characters.
	for _, prefix := range g.ReservedNamespacePrefixes {
		if !reservedNamespacePrefixRegexp.MatchString(prefix) {
			return errors.NewUserError(nil, "prefix contains illegal characters; only lowercase alphanumeric characters and hyphens are allowed", "prefix", prefix)
		}
	}
	return nil
}

// PoliciesWithBindings generates a ValidatingAdmissionPolicy and its policy binding that denies the addition
// of the KubeFleet placement annotation (kubefleet.dev/cluster-selectors) onto objects if the caller does not
// have the permission to create/update a (cluster) placement policy in the cluster/namespace, or if the objects
// are in reserved namespaces (or are reserved namespaces themselves), or are Endpoints objects.
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

	// The API server reports the first failed validation; the checks that no permission can lift come first.
	validations := make([]admissionregistrationv1.Validation, 0, 4)

	// The reserved namespace check is skipped if no prefixes are specified, as no namespaces are reserved.
	if len(g.ReservedNamespacePrefixes) > 0 {
		// For Namespace objects, the API server sets request.namespace to the name of the namespace, so that
		// the reserved namespaces themselves are covered as well.
		isInReservedNamespacePrefixes := LogicalOr()
		for _, prefix := range g.ReservedNamespacePrefixes {
			isInReservedNamespacePrefixes.Add(isInNamespaceWithPrefix(prefix))
		}
		isNotInReservedNamespacesExpr, err := LogicalNot(LogicalAnd(requestHasNamespace(), isInReservedNamespacePrefixes)).Build()
		if err != nil {
			return nil, errors.Wraps(err, "failed to build CEL expression", "reservedNamespacePrefixes", g.ReservedNamespacePrefixes)
		}
		validations = append(validations, admissionregistrationv1.Validation{
			Expression: isNotInReservedNamespacesExpr,
			Message: fmt.Sprintf(
				"adding or changing the %s annotation on reserved namespaces or objects in them is disallowed (reserved namespace prefixes: %s)",
				annotationKey, strings.Join(g.ReservedNamespacePrefixes, ", ")),
			Reason: ptr.To(metav1.StatusReasonForbidden),
		})
	}

	// The EndpointSlice mirroring controller copies the annotations of an Endpoints object onto the EndpointSlices
	// mirrored from it, and keeps them in sync; as the controller has no access to the placement policy APIs, its
	// writes would be denied by this policy. The annotation has no use on Endpoints objects anyway, as they cannot
	// be placed with the placement policy APIs.
	isNotEndpointsExpr, err := LogicalNot(requestIsForKind(corev1.GroupName, endpointsKind)).Build()
	if err != nil {
		return nil, errors.Wraps(err, "failed to build CEL expression", "kind", endpointsKind)
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

	validations = append(validations,
		admissionregistrationv1.Validation{
			Expression: isNotEndpointsExpr,
			Message: fmt.Sprintf(
				"adding or changing the %s annotation on Endpoints objects is disallowed; Endpoints cannot be placed, and the annotation would be copied onto the EndpointSlices mirrored from them",
				annotationKey),
			Reason: ptr.To(metav1.StatusReasonForbidden),
		},
		admissionregistrationv1.Validation{
			Expression: canCreateAndUpdatePlacementPoliciesInNamespaceExpr,
			Message: fmt.Sprintf(
				"adding or changing the %s annotation on a namespaced object requires the permissions to create and update %s.%s in the namespace of the object",
				annotationKey, placementPoliciesResourceName, placementAPIGroup),
			Reason: ptr.To(metav1.StatusReasonForbidden),
		},
		admissionregistrationv1.Validation{
			Expression: canCreateAndUpdateClusterPlacementPoliciesExpr,
			Message: fmt.Sprintf(
				"adding or changing the %s annotation on a cluster-scoped object requires the permissions to create and update %s.%s",
				annotationKey, clusterPlacementPoliciesResourceName, placementAPIGroup),
			Reason: ptr.To(metav1.StatusReasonForbidden),
		},
	)

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
			Validations: validations,
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
