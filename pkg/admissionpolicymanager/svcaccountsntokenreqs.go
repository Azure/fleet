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

	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	// Exempt the value from the linter as it miscategorizes it as a credential.
	SvcAccountsAndTokenRequestsVAPGeneratorName = "DenyServiceAccountsAndTokenRequestsInReservedNamespaces" //nolint:gosec
)

const (
	svcAccountsAndTokenRequestsVAPPolicyName        = "deny-serviceaccounts-and-tokenrequests-in-reserved-namespaces"
	svcAccountsAndTokenRequestsVAPPolicyBindingName = "deny-serviceaccounts-and-tokenrequests-in-reserved-namespaces-binding"
)

const (
	kubeSchedulerUserName         = "system:kube-scheduler"
	kubeControllerManagerUserName = "system:kube-controller-manager"

	kubeNodeUserGroup     = "system:nodes"
	adminUserGroup        = "system:masters"
	kubeadmAdminUserGroup = "kubeadm:cluster-admins"
	svcAccountUserGroup   = "system:serviceaccounts"
)

// Verify that ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator implements
// the ValidatingAdmissionPolicyGenerator interface.
var _ ValidatingAdmissionPolicyGenerator = &ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator{}

// ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator generates a
// ValidatingAdmissionPolicy and its binding that denies creation/update/deletion of service accounts
// and creation of token requests in reserved namespaces, except for requests from certain
// whitelisted users and user groups.
//
// TO-DO (chenyu1): evaluate if it is appropriate to ban service accounts ops under
// all namespaces.
type ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator struct {
	WhitelistedUsernames      []string
	WhitelistedUserGroups     []string
	ReservedNamespacePrefixes []string
}

// Name returns the name of the generator, which is used to determine if a specific generator
// has been enabled or not.
func (g *ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator) Name() string {
	return SvcAccountsAndTokenRequestsVAPGeneratorName
}

// Validate validates the configuration of the generator.
func (g *ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator) Validate() error {
	if len(g.ReservedNamespacePrefixes) == 0 {
		return errors.NewUserError(nil, "at least one prefix must be specified")
	}
	// Check if any of the prefixes includes illegal characters.
	for _, prefix := range g.ReservedNamespacePrefixes {
		if !reservedNamespacePrefixRegexp.MatchString(prefix) {
			return errors.NewUserError(nil, "prefix contains illegal characters; only lowercase alphanumeric characters and hyphens are allowed", "prefix", prefix)
		}
	}
	// Check if any whitelisted username or user group contains characters that are
	// illegal in a CEL string literal.
	if err := validateCELStringLiterals(g.WhitelistedUsernames...); err != nil {
		return errors.Wraps(err, "invalid string literals", "field", "whitelistedUsernames")
	}
	if err := validateCELStringLiterals(g.WhitelistedUserGroups...); err != nil {
		return errors.Wraps(err, "invalid string literals", "field", "whitelistedUserGroups")
	}
	return nil
}

// PoliciesWithBindings generates a ValidatingAdmissionPolicy and its policy binding that denies
// creation/update/deletion of service accounts and creation of token requests in reserved namespaces,
// except for requests from certain whitelisted users and user groups.
//
// For simplicity reasons, the code here assumes that the generator has been validated before PoliciesWithBindings() is called.
func (g *ServiceAccountsAndTokenRequestsValidatingAdmissionPolicyGenerator) PoliciesWithBindings() []PolicyWithBindings {
	celExprAccSegs := []string{}

	// Exempt whitelisted users from this admission policy.
	for _, username := range g.WhitelistedUsernames {
		celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`request.userInfo.username == "%s"`, username))
	}
	// Exempt whitelisted user groups from this admission policy.
	for _, userGroup := range g.WhitelistedUserGroups {
		celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`"%s" in request.userInfo.groups`, userGroup))
	}
	// Exempt requests from the Kubernetes scheduler, any of the nodes, and (esp.) the
	// Kubernetes controller manager from this admission policy.
	//
	// Important: the Kubernetes controller manager, when deployed with the option
	// --use-service-account-credentials=true, creates a service account token for many of its controllers
	// and uses those tokens to authenticate to the Kubernetes API server. It retrieves a token
	// via the TokenRequest API; failure to exempt this scenario may lead to critical errors.
	celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`request.userInfo.username == "%s"`, kubeSchedulerUserName))
	celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`request.userInfo.username == "%s"`, kubeControllerManagerUserName))
	celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`"%s" in request.userInfo.groups`, kubeNodeUserGroup))
	// Exempt requests from cluster admin users from this admission policy.
	celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`"%s" in request.userInfo.groups`, adminUserGroup))
	// Exempt kubeadm cluster admins from this policy as well, so that bootstrapping a hub cluster with
	// kubeadm credentials can proceed without being blocked.
	celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`"%s" in request.userInfo.groups`, kubeadmAdminUserGroup))
	// Exempt service accounts from this admission policy. Note that VAP check happens after authentication and
	// authorization have been performed. This is added to keep things consistent with the original webhook behavior,
	// and also for the reason that some controller manager components (e.g., the service account controller)
	// need to create service accounts as part of their normal operations.
	celExprAccSegs = append(celExprAccSegs, fmt.Sprintf(`"%s" in request.userInfo.groups`, svcAccountUserGroup))

	celExprAcc := strings.Join(celExprAccSegs, " || ")

	celExprNSSegs := []string{}
	for _, prefix := range g.ReservedNamespacePrefixes {
		celExprNSSegs = append(celExprNSSegs, fmt.Sprintf(`request.namespace.startsWith("%s")`, prefix))
	}
	celExprNS := strings.Join(celExprNSSegs, " || ")

	celExpr := fmt.Sprintf("!(%s) || (%s)", celExprNS, celExprAcc)

	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: svcAccountsAndTokenRequestsVAPPolicyName,
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
								admissionregistrationv1.Delete,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{""},
								APIVersions: []string{"v1"},
								Resources:   []string{"serviceaccounts"},
							},
						},
					},
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{""},
								APIVersions: []string{"v1"},
								// TokenRequest API is implemented as a subresource (token) of service
								// accounts. It only supports the Create operation.
								Resources: []string{"serviceaccounts/token"},
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
					Message: "writing service accounts in reserved namespaces or requesting tokens from such service accounts is disallowed",
					Reason:  ptr.To(metav1.StatusReasonForbidden),
				},
			},
		},
	}

	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: svcAccountsAndTokenRequestsVAPPolicyBindingName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: svcAccountsAndTokenRequestsVAPPolicyName,
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
