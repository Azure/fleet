package managedresource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	admv1 "k8s.io/api/admissionregistration/v1"
)

func TestGetValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()

		vap := GetValidatingAdmissionPolicy(false)
		assert.NotNil(t, vap)
		assert.NotContains(t, vap.Spec.MatchConstraints.ResourceRules, admv1.NamedRuleWithOperations{
			RuleWithOperations: admv1.RuleWithOperations{
				Rule: admv1.Rule{
					APIGroups:   []string{"placement.kubernetes-fleet.io"},
					Resources:   []string{"clusterresourceplacements"},
					APIVersions: []string{"*"},
				},
				Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
			},
		})
	})

	t.Run("hub", func(t *testing.T) {
		t.Parallel()

		vap := GetValidatingAdmissionPolicy(true)
		assert.NotNil(t, vap)
		assert.Contains(t, vap.Spec.MatchConstraints.ResourceRules, admv1.NamedRuleWithOperations{
			RuleWithOperations: admv1.RuleWithOperations{
				Rule: admv1.Rule{
					APIGroups:   []string{"placement.kubernetes-fleet.io"},
					Resources:   []string{"clusterresourceplacements"},
					APIVersions: []string{"*"},
				},
				Operations: []admv1.OperationType{admv1.Create, admv1.Update, admv1.Delete},
			},
		})
	})
}

func TestGetValidatingAdmissionPolicyBinding(t *testing.T) {
	t.Parallel()

	vap := GetValidatingAdmissionPolicyBinding()
	assert.NotNil(t, vap)
}
