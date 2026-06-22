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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	nsNameTmpl = "work-%s"
)

var (
	ignoreSystemManagedObjectMetaFields = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "ResourceVersion", "Generation", "CreationTimestamp", "ManagedFields")

	lessFuncValidatingAdmissionPolicy = func(i, j admissionregistrationv1.ValidatingAdmissionPolicy) bool {
		return i.Name < j.Name
	}
	lessFuncValidatingAdmissionPolicyBinding = func(i, j admissionregistrationv1.ValidatingAdmissionPolicyBinding) bool {
		return i.Name < j.Name
	}
)

var _ = Describe("Policies, Policy Bindings and their Effects", Ordered, func() {
	nsName := fmt.Sprintf(nsNameTmpl, utils.RandStr())

	var ns *corev1.Namespace
	BeforeAll(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		// Note: due to test environment restrictions (restrictions from the envtest package);
		// namespaces created cannot be deleted. As a result, this test node will not perform
		// any cleanup.
		Expect(hubUncachedClient.Create(ctx, ns)).To(Succeed())
	})

	It("should have all the expected policies", func() {
		wantPolicies := []admissionregistrationv1.ValidatingAdmissionPolicy{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: podsAndReplicaSetsVAPPolicyName,
					Labels: map[string]string{
						VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
						VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
						VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
					},
				},
				Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
					FailurePolicy: ptr.To(admissionregistrationv1.Fail),
					MatchConstraints: &admissionregistrationv1.MatchResources{
						NamespaceSelector: &metav1.LabelSelector{},
						ObjectSelector:    &metav1.LabelSelector{},
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
										Scope:       ptr.To(admissionregistrationv1.ScopeType("*")), // The system-enforced default.
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
										Scope:       ptr.To(admissionregistrationv1.ScopeType("*")), // The system-enforced default.
									},
								},
							},
						},
						MatchPolicy: ptr.To(admissionregistrationv1.Equivalent), // The system-enforced default.
					},
					Validations: []admissionregistrationv1.Validation{
						{
							Expression: `(request.namespace.startsWith("fleet-")) || (request.namespace.startsWith("kube-"))`,
							Message:    "creating pods and replicas is disallowed in the fleet hub cluster",
							Reason:     ptr.To(metav1.StatusReasonForbidden),
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: svcAccountsAndTokenRequestsVAPPolicyName,
					Labels: map[string]string{
						VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
						VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
						VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
					},
				},
				Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
					FailurePolicy: ptr.To(admissionregistrationv1.Fail),
					MatchConstraints: &admissionregistrationv1.MatchResources{
						NamespaceSelector: &metav1.LabelSelector{},
						ObjectSelector:    &metav1.LabelSelector{},
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
										Scope:       ptr.To(admissionregistrationv1.ScopeType("*")), // The system-enforced default.
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
										Resources:   []string{"serviceaccounts/token"},
										Scope:       ptr.To(admissionregistrationv1.ScopeType("*")), // The system-enforced default.
									},
								},
							},
						},
						MatchPolicy: ptr.To(admissionregistrationv1.Equivalent), // The system-enforced default.
					},
					Validations: []admissionregistrationv1.Validation{
						{
							Expression: `(!((request.namespace.startsWith("fleet-")) || (request.namespace.startsWith("kube-")))) || ((request.userInfo.username == "system:kube-scheduler") || (request.userInfo.username == "system:kube-controller-manager") || ("system:nodes" in request.userInfo.groups) || ("system:masters" in request.userInfo.groups) || ("kubeadm:cluster-admins" in request.userInfo.groups) || ("system:serviceaccounts" in request.userInfo.groups))`,
							Message:    "writing service accounts in reserved namespaces or requesting tokens from such service accounts is disallowed",
							Reason:     ptr.To(metav1.StatusReasonForbidden),
						},
					},
				},
			},
		}

		Eventually(ctx, func() error {
			policyList := &admissionregistrationv1.ValidatingAdmissionPolicyList{}
			matchingLabels := map[string]string{
				VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
				VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
				VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
			}
			if err := hubUncachedClient.List(ctx, policyList, client.MatchingLabels(matchingLabels)); err != nil {
				return fmt.Errorf("failed to list matching policies: %w", err)
			}

			policies := policyList.Items
			if len(policies) != len(wantPolicies) {
				return fmt.Errorf("number of policies mismatch: got %d, want %d", len(policies), len(wantPolicies))
			}

			if diff := cmp.Diff(
				policies, wantPolicies,
				cmpopts.EquateEmpty(),
				cmpopts.SortSlices(lessFuncValidatingAdmissionPolicy),
				ignoreSystemManagedObjectMetaFields,
			); diff != "" {
				return fmt.Errorf("policies mismatch (-got +want):\n%s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Received unexpected list of policies or an error has occurred")
	})

	It("should have all the expected bindings", func() {
		wantPolicyBindings := []admissionregistrationv1.ValidatingAdmissionPolicyBinding{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: podsAndReplicaSetsVAPPolicyBindingName,
					Labels: map[string]string{
						VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
						VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
						VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
					},
				},
				Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: podsAndReplicaSetsVAPPolicyName,
					ValidationActions: []admissionregistrationv1.ValidationAction{
						admissionregistrationv1.Deny,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: svcAccountsAndTokenRequestsVAPPolicyBindingName,
					Labels: map[string]string{
						VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
						VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
						VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
					},
				},
				Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: svcAccountsAndTokenRequestsVAPPolicyName,
					ValidationActions: []admissionregistrationv1.ValidationAction{
						admissionregistrationv1.Deny,
					},
				},
			},
		}

		Eventually(ctx, func() error {
			bindingList := &admissionregistrationv1.ValidatingAdmissionPolicyBindingList{}
			matchingLabels := map[string]string{
				VAPManagedByKubeFleetLabelKey: VAPManagedByKubeFleetLabelValue,
				VAPPartOfKubeFleetLabelKey:    VAPPartOfKubeFleetLabelValue,
				VAPComponentKubeFleetLabelKey: VAPComponentAdmissionPolicyManagerLabelValue,
			}
			if err := hubUncachedClient.List(ctx, bindingList, client.MatchingLabels(matchingLabels)); err != nil {
				return fmt.Errorf("failed to list matching policy bindings: %w", err)
			}

			bindings := bindingList.Items
			if len(bindings) != len(wantPolicyBindings) {
				return fmt.Errorf("number of policy bindings mismatch: got %d, want %d", len(bindings), len(wantPolicyBindings))
			}

			if diff := cmp.Diff(
				bindings, wantPolicyBindings,
				cmpopts.EquateEmpty(),
				cmpopts.SortSlices(lessFuncValidatingAdmissionPolicyBinding),
				ignoreSystemManagedObjectMetaFields,
			); diff != "" {
				return fmt.Errorf("policy bindings mismatch (-got +want):\n%s", diff)
			}
			return nil
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Received unexpected list of policy bindings or an error has occurred")
	})

	// Note: in the integration test environment, it appears that validating admission policies have no effect or are
	// not being enforced as expected. As a result the effects are validated using E2E tests instead.
})
