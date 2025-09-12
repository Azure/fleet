/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	admv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const resourceName = "aks-fleet-managed-by-arm"

var forbidden = metav1.StatusReasonForbidden

func GetValidatingAdmissionPolicy(isHub bool) *admv1.ValidatingAdmissionPolicy {
	vap := &admv1.ValidatingAdmissionPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingAdmissionPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: admv1.ValidatingAdmissionPolicySpec{
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
								APIGroups:   []string{""},
								Resources:   []string{"namespaces"},
								APIVersions: []string{"v1"},
							},
							Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
						},
					},
					{
						RuleWithOperations: admv1.RuleWithOperations{
							Rule: admv1.Rule{
								APIGroups:   []string{""},
								Resources:   []string{"resourcequotas"},
								APIVersions: []string{"v1"},
							},
							Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
						},
						ResourceNames: []string{"default"},
					},
					{
						RuleWithOperations: admv1.RuleWithOperations{
							Rule: admv1.Rule{
								APIGroups:   []string{"networking.k8s.io"},
								Resources:   []string{"networkpolicies"},
								APIVersions: []string{"*"},
							},
							Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
						},
						ResourceNames: []string{"default"},
					},
				},
			},
			Validations: []admv1.Validation{
				{
					Expression: `"system:masters" in request.userInfo.groups || "system:serviceaccounts:kube-system" in request.userInfo.groups || "system:serviceaccounts:fleet-system" in request.userInfo.groups`,
					Message:    "Create, Update, or Delete operations on ARM managed resources is forbidden",
					Reason:     &forbidden,
				},
			},
		},
	}

	if isHub {
		vap.Spec.MatchConstraints.ResourceRules = append(vap.Spec.MatchConstraints.ResourceRules, admv1.NamedRuleWithOperations{
			RuleWithOperations: admv1.RuleWithOperations{
				Rule: admv1.Rule{
					APIGroups:   []string{"placement.kubernetes-fleet.io"},
					Resources:   []string{"*"},
					APIVersions: []string{"*"},
				},
				Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
			},
		})
	}

	return vap
}

func GetValidatingAdmissionPolicyBinding() *admv1.ValidatingAdmissionPolicyBinding {
	return &admv1.ValidatingAdmissionPolicyBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingAdmissionPolicyBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: admv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: resourceName,
			ValidationActions: []admv1.ValidationAction{
				admv1.Deny,
			},
		},
	}
}
