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

	. "github.com/onsi/gomega"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The messages that the admission policies report when they deny a request.
const (
	wantPodsAndReplicaSetsDenialMsg          = "creating pods and replicas is disallowed in the fleet hub cluster"
	wantSvcAccountsAndTokenRequestsDenialMsg = "writing service accounts in reserved namespaces or requesting tokens from such service accounts is disallowed"
	wantNamespacedObjDenialMsg               = "adding or changing the kubefleet.dev/cluster-selectors annotation on a namespaced object requires the permissions to create and update placementpolicies.placement.kubefleet.dev in the namespace of the object"
	wantClusterScopedObjDenialMsg            = "adding or changing the kubefleet.dev/cluster-selectors annotation on a cluster-scoped object requires the permissions to create and update clusterplacementpolicies.placement.kubefleet.dev"
)

// A finalizer that test cases add to objects, so that the objects stay around (with a deletion timestamp set)
// after being deleted.
const testFinalizer = "kubefleet.dev/integration-test"

// impersonatedClient returns a client that sends requests as the given user, who is a member of the given user
// groups (plus system:authenticated, which the API server adds for impersonated users).
//
// Note that the API server checks if the impersonated user is authorized to make a request (RBAC) before it
// runs any admission policy against the request.
func impersonatedClient(username string, userGroups ...string) client.Client {
	impersonatedCfg := rest.CopyConfig(cfg)
	impersonatedCfg.Impersonate = rest.ImpersonationConfig{
		UserName: username,
		Groups:   userGroups,
	}
	c, err := client.New(impersonatedCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred(), "Failed to create a client that impersonates user %s", username)
	return c
}

func userSubject(username string) rbacv1.Subject {
	return rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: username}
}

func groupSubject(userGroup string) rbacv1.Subject {
	return rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: rbacv1.GroupKind, Name: userGroup}
}

// getAndUpdate retrieves the given object with the given client, applies the given mutation to it, and updates
// it with the same client.
func getAndUpdate[T client.Object](c client.Client, obj T, mutate func(T)) error {
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return fmt.Errorf("failed to get object %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	mutate(obj)
	return c.Update(ctx, obj)
}

// getAndUpdateSubResource retrieves the given object with the given client, applies the given mutation to it, and
// sends the whole object to the given subresource (e.g., status) of the object with the same client.
func getAndUpdateSubResource[T client.Object](c client.Client, obj T, subResource string, mutate func(T)) error {
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return fmt.Errorf("failed to get object %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	mutate(obj)
	return c.SubResource(subResource).Update(ctx, obj)
}

// grantClusterWidePermissions creates a cluster role with the given rules, and binds it to the given subjects
// with a cluster role binding of the same name. It returns after the permissions have taken effect.
func grantClusterWidePermissions(name string, rules []rbacv1.PolicyRule, subjects ...rbacv1.Subject) {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: rules,
	}
	Expect(hubUncachedClient.Create(ctx, clusterRole)).To(Succeed(), "Failed to create cluster role %s", name)

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     name,
		},
		Subjects: subjects,
	}
	Expect(hubUncachedClient.Create(ctx, clusterRoleBinding)).To(Succeed(), "Failed to create cluster role binding %s", name)

	waitUntilPermissionsTakeEffect("", rules, subjects)
}

// revokeClusterWidePermissions deletes the cluster role and cluster role binding that
// grantClusterWidePermissions creates.
func revokeClusterWidePermissions(name string) {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(client.IgnoreNotFound(hubUncachedClient.Delete(ctx, clusterRoleBinding))).To(Succeed(), "Failed to delete cluster role binding %s", name)

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(client.IgnoreNotFound(hubUncachedClient.Delete(ctx, clusterRole))).To(Succeed(), "Failed to delete cluster role %s", name)
}

// grantPermissionsInNamespace creates a role with the given rules in the given namespace, and binds it to the
// given subjects with a role binding of the same name. It returns after the permissions have taken effect.
func grantPermissionsInNamespace(namespace, name string, rules []rbacv1.PolicyRule, subjects ...rbacv1.Subject) {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Rules: rules,
	}
	Expect(hubUncachedClient.Create(ctx, role)).To(Succeed(), "Failed to create role %s/%s", namespace, name)

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     name,
		},
		Subjects: subjects,
	}
	Expect(hubUncachedClient.Create(ctx, roleBinding)).To(Succeed(), "Failed to create role binding %s/%s", namespace, name)

	waitUntilPermissionsTakeEffect(namespace, rules, subjects)
}

// waitUntilPermissionsTakeEffect waits until the API server authorizes each of the given subjects to perform the
// first verb on the first resource of the first API group of each given rule (in the given namespace, or across
// all namespaces if the namespace is empty).
//
// The RBAC authorizer reads roles and bindings from its informer caches, so that newly created ones might not take
// effect immediately; one check per rule is sufficient, as a role (or cluster role) and its binding become visible
// to the authorizer as a whole.
func waitUntilPermissionsTakeEffect(namespace string, rules []rbacv1.PolicyRule, subjects []rbacv1.Subject) {
	for _, subject := range subjects {
		for _, rule := range rules {
			resource, subresource, _ := strings.Cut(rule.Resources[0], "/")
			sarSpec := authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   namespace,
					Verb:        rule.Verbs[0],
					Group:       rule.APIGroups[0],
					Resource:    resource,
					Subresource: subresource,
				},
			}
			switch subject.Kind {
			case rbacv1.UserKind:
				sarSpec.User = subject.Name
			case rbacv1.GroupKind:
				sarSpec.Groups = []string{subject.Name}
			}

			Eventually(func() error {
				sar := &authorizationv1.SubjectAccessReview{
					Spec: *sarSpec.DeepCopy(),
				}
				if err := hubUncachedClient.Create(ctx, sar); err != nil {
					return fmt.Errorf("failed to create subject access review: %w", err)
				}
				if !sar.Status.Allowed {
					return fmt.Errorf("%s %s is not yet allowed to %s %s (subresource %q, API group %q) in namespace %q",
						subject.Kind, subject.Name, rule.Verbs[0], resource, subresource, rule.APIGroups[0], namespace)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Permissions have not taken effect")
		}
	}
}

// wantDeniedByPolicy returns an error if the given error does not report that a request has been denied by the
// given validating admission policy with the given message.
//
// As an RBAC denial is also reported as a Forbidden error, the check looks for the policy name and the message in
// the error, so that a request denied for any other reason will not pass as a policy denial.
func wantDeniedByPolicy(err error, policyName, wantMsg string) error {
	if err == nil {
		return fmt.Errorf("request is allowed, want it to be denied by validating admission policy %s", policyName)
	}
	if !apierrors.IsForbidden(err) {
		return fmt.Errorf("request has failed with a non-Forbidden error, want it to be denied by validating admission policy %s: %w", policyName, err)
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, fmt.Sprintf("ValidatingAdmissionPolicy '%s'", policyName)) || !strings.Contains(errMsg, wantMsg) {
		return fmt.Errorf("request has been denied for an unexpected reason, want it to be denied by validating admission policy %s with message %q: %w", policyName, wantMsg, err)
	}
	return nil
}

// waitUntilPolicyIsEnforced waits until the given probe request is denied by the given validating admission policy
// with the given message.
//
// The API server picks up new (or updated) validating admission policies and their bindings periodically (about
// every second); requests sent right after a policy is created might not be checked against it.
func waitUntilPolicyIsEnforced(probe func() error, policyName, wantMsg string) {
	Eventually(func() error {
		return wantDeniedByPolicy(probe(), policyName, wantMsg)
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Validating admission policy %s has not taken effect", policyName)
}

// createNamespace creates a namespace with the given name.
//
// Note: due to restrictions of the test environment (no namespace controller is running), namespaces cannot be
// deleted; namespaces created by the test cases are not cleaned up.
func createNamespace(name string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	Expect(hubUncachedClient.Create(ctx, ns)).To(Succeed(), "Failed to create namespace %s", name)
}
