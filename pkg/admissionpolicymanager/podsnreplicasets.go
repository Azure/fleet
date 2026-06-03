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

// TO-DO (chenyu1): refactor the building logic from the current state (static generation)
// to CEL expression trees after the initial set of VAPs are validated to be working as expected.

package admissionpolicymanager

import (
	"fmt"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet/pkg/utils/errors"
)

const (
	PodsAndReplicaSetsVAPGeneratorName = "DenyPodsAndReplicaSetsOutsideReservedNamespaces"
)

const (
	podsAndReplicaSetsVAPPolicyName        = "deny-pods-and-replicasets-outside-reserved-namespaces"
	podsAndReplicaSetsVAPPolicyBindingName = "deny-pods-and-replicasets-outside-reserved-namespaces-binding"
)

const (
	reconcileIfManagedLabelKey   = "fleet.azure.com/reconcile"
	reconcileIfManagedLabelValue = "managed"

	deploymentControllerUserName = "system:serviceaccount:kube-system:deployment-controller"
	replicaSetControllerUserName = "system:serviceaccount:kube-system:replicaset-controller"
)

// Verify that PodsAndReplicaSetsValidatingAdmissionPolicyGenerator implements
// the ValidatingAdmissionPolicyGenerator interface.
var _ ValidatingAdmissionPolicyGenerator = &PodsAndReplicaSetsValidatingAdmissionPolicyGenerator{}

// PodsAndReplicaSetsValidatingAdmissionPolicyGenerator generates a ValidatingAdmissionPolicy
// and its binding that denies creation of pods and replicasets in non-reserved namespaces.
type PodsAndReplicaSetsValidatingAdmissionPolicyGenerator struct {
	ReservedNamespacePrefixes []string
}

// Name returns the name of the generator, which is used to determine if a specific generator
// has been enabled or not.
func (g *PodsAndReplicaSetsValidatingAdmissionPolicyGenerator) Name() string {
	return PodsAndReplicaSetsVAPGeneratorName
}

// Validate validates the configuration of the generator.
func (g *PodsAndReplicaSetsValidatingAdmissionPolicyGenerator) Validate() error {
	if len(g.ReservedNamespacePrefixes) == 0 {
		return errors.NewUserError(nil, "at least one prefix must be specified")
	}
	// Check if any of the prefixes includes illegal characters.
	for _, prefix := range g.ReservedNamespacePrefixes {
		if !reservedNamespacePrefixRegexp.MatchString(prefix) {
			return errors.NewUserError(nil, "prefix contains illegal characters; only lowercase alphanumeric characters and hyphens are allowed", "prefix", prefix)
		}
	}
	return nil
}

// PoliciesWithBindings generates a ValidatingAdmissionPolicy and its policy binding that denies creation of pods and
// replicasets in non-reserved namespaces.
//
// For simplicity reasons, the code here assumes that the generator has been validated before PoliciesWithBindings() is called.
func (g *PodsAndReplicaSetsValidatingAdmissionPolicyGenerator) PoliciesWithBindings() []PolicyWithBindings {
	celExprSegs := []string{}
	for _, prefix := range g.ReservedNamespacePrefixes {
		celExprSegs = append(celExprSegs, fmt.Sprintf(`request.namespace.startsWith("%s")`, prefix))
	}

	// Custom (Azure-specific) logic: allow pods and replica sets to be created if they
	// have the fleet.azure.com/reconcile=managed label and they are created by the deployment or replica set
	// controllers (or the controller manager, just in case per controller service account is not enabled).
	hasReconcileIfManagedLabel := fmt.Sprintf(`object.metadata.labels["%s"] == "%s"`, reconcileIfManagedLabelKey, reconcileIfManagedLabelValue)
	createdByControllerManager := fmt.Sprintf(`request.userInfo.username == "%s" || request.userInfo.username == "%s" || request.userInfo.username == "%s"`, deploymentControllerUserName, replicaSetControllerUserName, kubeControllerManagerUserName)
	celExprSegs = append(celExprSegs, fmt.Sprintf("(%s && (%s))", hasReconcileIfManagedLabel, createdByControllerManager))

	celExpr := strings.Join(celExprSegs, " || ")

	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: podsAndReplicaSetsVAPPolicyName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: ptr.To(admissionregistrationv1.Fail),
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{""},
								APIVersions: []string{"v1"},
								Resources:   []string{"pods"},
							},
						},
					},
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"apps"},
								APIVersions: []string{"v1"},
								Resources:   []string{"replicasets"},
							},
						},
					},
				},
			},
			Validations: []admissionregistrationv1.Validation{
				{
					Expression: celExpr,
					// The error message has been set to match with the checks in some of the E2E tests
					// in our existing release pipeline.
					Message: "creating pods and replicas is disallowed in the fleet hub cluster",
					Reason:  ptr.To(metav1.StatusReasonForbidden),
				},
			},
		},
	}

	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: podsAndReplicaSetsVAPPolicyBindingName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: podsAndReplicaSetsVAPPolicyName,
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
	}
}
