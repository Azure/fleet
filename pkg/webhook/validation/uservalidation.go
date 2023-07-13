package validation

import (
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	mastersGroup         = "system:masters"
	serviceAccountsGroup = "system:serviceaccounts"

	crdAllowedFormat           = "user: %s in groups: %v is allowed to modify fleet CRD: %+v"
	crdDeniedFormat            = "user: %s in groups: %v is not allowed to modify fleet CRD: %+v"
	fleetResourceAllowedFormat = "user: %s in groups: %v is allowed to modify fleet resource %s: %+v"
	fleetResourceDeniedFormat  = "user: %s in groups: %v is not allowed to modify fleet resource %s: %+v"
)

var (
	fleetCRDGroups = []string{"networking.fleet.azure.com", "fleet.azure.com", "multicluster.x-k8s.io", "placement.azure.com"}
)

// ValidateUserForFleetCRD checks to see if user is not allowed to modify fleet CRDs.
func ValidateUserForFleetCRD(group string, namespacedName types.NamespacedName, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	if !checkCRDGroup(group) && isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
		klog.V(2).InfoS("user in groups is allowed to modify CRD", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(crdAllowedFormat, userInfo.Username, userInfo.Groups, namespacedName))
	}
	klog.V(2).InfoS("user in groups is not allowed to modify CRD", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(crdDeniedFormat, userInfo.Username, userInfo.Groups, namespacedName))
}

// ValidateUserForFleetCR checks to see if user is allowed to make a request to modify fleet CRs.
func ValidateUserForFleetCR(resKind string, namespacedName types.NamespacedName, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	var response admission.Response
	// TODO(Arvindthiru): this switch will be expanded for all fleet CR validations.
	switch resKind {
	case "MemberCluster":
		response = ValidateUserForResource(resKind, namespacedName, whiteListedUsers, userInfo)
	}
	return response
}

// ValidateUserForResource checks to see if user is allowed to modify argued fleet resource.
func ValidateUserForResource(resKind string, namespacedName types.NamespacedName, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	if isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) || isUserAuthenticatedServiceAccount(userInfo) {
		klog.V(2).InfoS("user in groups is allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resKind, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(fleetResourceAllowedFormat, userInfo.Username, userInfo.Groups, resKind, namespacedName))
	}
	klog.V(2).InfoS("user in groups is not allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resKind, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(fleetResourceDeniedFormat, userInfo.Username, userInfo.Groups, resKind, namespacedName))
}

// isMasterGroupUserOrWhiteListedUser returns true is user belongs to white listed users or user belongs to system:masters group.
func isMasterGroupUserOrWhiteListedUser(whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(whiteListedUsers, userInfo.Username) || slices.Contains(userInfo.Groups, mastersGroup)
}

// isUserAuthenticatedServiceAccount returns true if user is a valid/authenticated service account.
func isUserAuthenticatedServiceAccount(userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(userInfo.Groups, serviceAccountsGroup)
}

// checkCRDGroup returns true if the input CRD group is a fleet CRD group.
func checkCRDGroup(group string) bool {
	return slices.Contains(fleetCRDGroups, group)
}
