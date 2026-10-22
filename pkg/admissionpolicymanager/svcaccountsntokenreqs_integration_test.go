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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	// The audience of the service account tokens that the test cases request.
	testAudience = "kubefleet-integration-test"
)

func newServiceAccount(namespace, name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

// requestToken requests a token for the given service account with the given client (i.e., via the TokenRequest
// API); it returns an error if the request fails or no token has been issued.
func requestToken(c client.Client, namespace, name string) error {
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences: []string{testAudience},
		},
	}
	if err := c.SubResource("token").Create(ctx, newServiceAccount(namespace, name), tokenRequest); err != nil {
		return err
	}
	if len(tokenRequest.Status.Token) == 0 {
		return fmt.Errorf("no token has been issued for service account %s/%s", namespace, name)
	}
	return nil
}

var _ = Describe("deny service accounts and token requests in reserved namespaces", Ordered, func() {
	suffix := utils.RandStr()
	workNSName := fmt.Sprintf("work-%s", suffix)
	fleetNSName := fmt.Sprintf("fleet-%s", suffix)

	// The users that the test cases impersonate; all of them can write service accounts and request tokens
	// (as far as RBAC is concerned).
	//
	// A user that is not exempt from the admission policy.
	plainUsername := fmt.Sprintf("plain-%s", suffix)
	// A user that is not whitelisted, but whose name starts with the whitelisted username; it verifies that
	// usernames are matched exactly.
	notWhitelistedUsername := fmt.Sprintf("%s-2", whitelistedUsername)
	// Users that are exempt from the admission policy by their user groups.
	mastersMemberUsername := fmt.Sprintf("masters-member-%s", suffix)
	nodeUsername := fmt.Sprintf("system:node:node-%s", suffix)
	kubeadmAdminUsername := fmt.Sprintf("kubeadm-admin-%s", suffix)
	svcAccountsMemberUsername := fmt.Sprintf("serviceaccounts-member-%s", suffix)
	whitelistedGroupMemberUsername := fmt.Sprintf("whitelisted-group-member-%s", suffix)

	svcAccountWriterRBACName := fmt.Sprintf("serviceaccount-writer-%s", suffix)
	// The service accounts that the cluster admin creates (in both namespaces) for the test cases to write and
	// request tokens for.
	seededSvcAccountName := fmt.Sprintf("seeded-%s", suffix)

	var plainClient client.Client

	BeforeAll(func() {
		createNamespace(workNSName)
		createNamespace(fleetNSName)

		grantClusterWidePermissions(svcAccountWriterRBACName, []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"serviceaccounts"},
				Verbs:     []string{"get", authzVerbCreate, authzVerbUpdate, "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"serviceaccounts/token"},
				Verbs:     []string{authzVerbCreate},
			},
		},
			userSubject(plainUsername),
			userSubject(notWhitelistedUsername),
			userSubject("system:kube-scheduler"),
			userSubject("system:kube-controller-manager"),
			userSubject(whitelistedUsername),
			// Members of system:masters can do anything, and need no additional permissions.
			groupSubject("system:nodes"),
			groupSubject("kubeadm:cluster-admins"),
			groupSubject("system:serviceaccounts"),
			groupSubject(whitelistedUserGroup),
		)

		plainClient = impersonatedClient(plainUsername)

		// The cluster admin is exempt from the admission policy.
		Expect(hubUncachedClient.Create(ctx, newServiceAccount(fleetNSName, seededSvcAccountName))).To(Succeed(), "Failed to create a service account in the reserved namespace")
		Expect(hubUncachedClient.Create(ctx, newServiceAccount(workNSName, seededSvcAccountName))).To(Succeed(), "Failed to create a service account in the non-reserved namespace")

		By("waiting for the admission policy to take effect")
		waitUntilPolicyIsEnforced(func() error {
			return plainClient.Create(ctx, newServiceAccount(fleetNSName, fmt.Sprintf("probe-%s", utils.RandStr())))
		}, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)
	})

	It("should deny creating service accounts in a fleet- prefixed namespace", func() {
		err := plainClient.Create(ctx, newServiceAccount(fleetNSName, fmt.Sprintf("sa-%s", utils.RandStr())))
		Expect(wantDeniedByPolicy(err, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)).To(Succeed())
	})

	It("should deny creating service accounts in a kube- prefixed namespace", func() {
		err := plainClient.Create(ctx, newServiceAccount(kubeSystemNSName, fmt.Sprintf("sa-%s", utils.RandStr())))
		Expect(wantDeniedByPolicy(err, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)).To(Succeed())
	})

	It("should deny updating service accounts in a reserved namespace", func() {
		err := getAndUpdate(plainClient, newServiceAccount(fleetNSName, seededSvcAccountName), func(sa *corev1.ServiceAccount) {
			sa.Labels = map[string]string{testDataKey: testDataValue}
		})
		Expect(wantDeniedByPolicy(err, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)).To(Succeed())
	})

	It("should deny deleting service accounts in a reserved namespace", func() {
		err := plainClient.Delete(ctx, newServiceAccount(fleetNSName, seededSvcAccountName))
		Expect(wantDeniedByPolicy(err, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)).To(Succeed())
	})

	It("should deny requesting tokens for service accounts in a reserved namespace", func() {
		err := requestToken(plainClient, fleetNSName, seededSvcAccountName)
		Expect(wantDeniedByPolicy(err, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)).To(Succeed())
	})

	It("should allow creating, updating, and deleting service accounts in a non-reserved namespace", func() {
		sa := newServiceAccount(workNSName, fmt.Sprintf("sa-%s", utils.RandStr()))
		Expect(plainClient.Create(ctx, sa)).To(Succeed(), "Failed to create a service account")

		err := getAndUpdate(plainClient, sa, func(sa *corev1.ServiceAccount) {
			sa.Labels = map[string]string{testDataKey: testDataValue}
		})
		Expect(err).To(Succeed(), "Failed to update the service account")

		Expect(plainClient.Delete(ctx, sa)).To(Succeed(), "Failed to delete the service account")
	})

	It("should allow requesting tokens for service accounts in a non-reserved namespace", func() {
		Expect(requestToken(plainClient, workNSName, seededSvcAccountName)).To(Succeed(), "Failed to request a token")
	})

	DescribeTable("should allow exempt callers to write service accounts in a reserved namespace and request tokens for them",
		func(username string, userGroups []string) {
			c := impersonatedClient(username, userGroups...)

			saName := fmt.Sprintf("sa-%s", utils.RandStr())
			Expect(c.Create(ctx, newServiceAccount(fleetNSName, saName))).To(Succeed(), "Failed to create a service account")
			Expect(requestToken(c, fleetNSName, saName)).To(Succeed(), "Failed to request a token")
		},
		Entry("members of system:masters (cluster admins)", mastersMemberUsername, []string{"system:masters"}),
		Entry("the Kubernetes scheduler", "system:kube-scheduler", []string{}),
		Entry("the Kubernetes controller manager", "system:kube-controller-manager", []string{}),
		Entry("members of system:nodes (nodes)", nodeUsername, []string{"system:nodes"}),
		Entry("members of kubeadm:cluster-admins (kubeadm cluster admins)", kubeadmAdminUsername, []string{"kubeadm:cluster-admins"}),
		Entry("members of system:serviceaccounts (service accounts)", svcAccountsMemberUsername, []string{"system:serviceaccounts"}),
		Entry("whitelisted users", whitelistedUsername, []string{}),
		Entry("members of whitelisted user groups", whitelistedGroupMemberUsername, []string{whitelistedUserGroup}),
	)

	It("should deny creating service accounts in a reserved namespace for users whose names only start with a whitelisted username", func() {
		err := impersonatedClient(notWhitelistedUsername).Create(ctx, newServiceAccount(fleetNSName, fmt.Sprintf("sa-%s", utils.RandStr())))
		Expect(wantDeniedByPolicy(err, svcAccountsAndTokenRequestsVAPPolicyName, wantSvcAccountsAndTokenRequestsDenialMsg)).To(Succeed())
	})

	AfterAll(func() {
		revokeClusterWidePermissions(svcAccountWriterRBACName)
	})
})
