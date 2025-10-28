/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	admv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO use a different name for binding to simplify the code
// and add a migration path
const resourceName = "aks-fleet-managed-by-arm"

var forbidden = metav1.StatusReasonForbidden

func getValidatingAdmissionPolicy() *admv1.ValidatingAdmissionPolicy {
	vap := &admv1.ValidatingAdmissionPolicy{}
	mutateValidatingAdmissionPolicy(vap)
	return vap
}

func mutateValidatingAdmissionPolicy(vap *admv1.ValidatingAdmissionPolicy) {
	ometa := metav1.ObjectMeta{
		Name: resourceName,
		Labels: map[string]string{
			"fleet.azure.com/managed-by": "arm",
		},
		ResourceVersion: vap.ResourceVersion,
	}
	vap.ObjectMeta = ometa
	vap.Spec = admv1.ValidatingAdmissionPolicySpec{
		MatchConstraints: &admv1.MatchResources{
			ObjectSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"fleet.azure.com/managed-by": "arm",
				},
			},
			ResourceRules: []admv1.NamedRuleWithOperations{
				{
					RuleWithOperations: admv1.RuleWithOperations{
						Rule: admv1.Rule{
							APIGroups:   []string{"*"},
							Resources:   []string{"*"},
							APIVersions: []string{"*"},
						},
						Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
					},
				},
			},
		},
		Validations: []admv1.Validation{
			{
				Expression: `
				(
					(
						request.userInfo.username == "aksService" ||
						request.userInfo.username == "acsService" ||
						request.userInfo.username == "fleet-member-agent-sa"
					)
				    &&
					(
						"system:masters" in request.userInfo.groups ||
						"system:serviceaccounts:fleet-system" in request.userInfo.groups
					)
				)
				  ||
				(
					"system:serviceaccounts:openshift-infra" in request.userInfo.groups ||
					"system:serviceaccounts:kube-system" in request.userInfo.groups
				)`,
				Message: "Create, Update, or Delete operations on ARM managed resources is forbidden",
				Reason:  &forbidden,
			},
		},
	}
}

func getValidatingAdmissionPolicyBinding() *admv1.ValidatingAdmissionPolicyBinding {
	vapb := &admv1.ValidatingAdmissionPolicyBinding{}
	mutateValidatingAdmissionPolicyBinding(vapb)
	return vapb
}

func mutateValidatingAdmissionPolicyBinding(vapb *admv1.ValidatingAdmissionPolicyBinding) {
	ometa := metav1.ObjectMeta{
		Name: resourceName,
		Labels: map[string]string{
			"fleet.azure.com/managed-by": "arm",
		},
		ResourceVersion: vapb.ResourceVersion,
	}
	vapb.ObjectMeta = ometa
	vapb.Spec = admv1.ValidatingAdmissionPolicyBindingSpec{
		PolicyName: resourceName,
		ValidationActions: []admv1.ValidationAction{
			admv1.Deny,
		},
	}
}
