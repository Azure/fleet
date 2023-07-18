package validation

import (
	"fmt"
	"reflect"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
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
	if checkCRDGroup(group) && !isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
		klog.V(2).InfoS("user in groups is not allowed to modify CRD", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
		return admission.Denied(fmt.Sprintf(crdDeniedFormat, userInfo.Username, userInfo.Groups, namespacedName))
	}
	klog.V(2).InfoS("user in groups is allowed to modify CRD", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
	return admission.Allowed(fmt.Sprintf(crdAllowedFormat, userInfo.Username, userInfo.Groups, namespacedName))
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

func ValidateMemberClusterUpdate(currentMC, oldMC fleetv1alpha1.MemberCluster, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	var response admission.Response
	namespacedName := types.NamespacedName{Name: currentMC.Name}
	if isMemberClusterLabelUpdated(currentMC.Labels, oldMC.Labels) {
		klog.V(2).InfoS("member cluster labels was updated", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
		// clearing labels to compare other member cluster fields.
		currentMC.Labels = make(map[string]string, 0)
		oldMC.Labels = make(map[string]string, 0)
		if isMemberClusterUpdated(currentMC, oldMC) {
			klog.V(2).InfoS("fields other than member cluster labels were also updated", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
			response = ValidateUserForFleetCR(currentMC.Kind, namespacedName, whiteListedUsers, userInfo)
		} else {
			// we allow any user to update the labels for member cluster CR.
			klog.V(2).InfoS("user in groups is allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", currentMC.Kind, "namespacedName", namespacedName)
			response = admission.Allowed(fmt.Sprintf(fleetResourceAllowedFormat, userInfo.Username, userInfo.Groups, currentMC.Kind, namespacedName))
		}
	}
	return response
}

// isMasterGroupUserOrWhiteListedUser returns true is user belongs to white listed users or user belongs to system:masters group.
func isMasterGroupUserOrWhiteListedUser(whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(whiteListedUsers, userInfo.Username) || slices.Contains(userInfo.Groups, mastersGroup)
}

// isUserAuthenticatedServiceAccount returns true if user is a valid service account.
func isUserAuthenticatedServiceAccount(userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(userInfo.Groups, serviceAccountsGroup)
}

func isMemberClusterLabelUpdated(currentMCLabels, oldMCLabels map[string]string) bool {
	return !reflect.DeepEqual(currentMCLabels, oldMCLabels)
}

func isMemberClusterUpdated(currentMC, oldMC fleetv1alpha1.MemberCluster) bool {
	// Not comparing the whole ObjectMeta object because it has live fields.
	return !reflect.DeepEqual(currentMC.Spec, oldMC.Spec) ||
		// compare all non-read only fields in ObjectMeta, TypeMeta.
		!reflect.DeepEqual(currentMC.GenerateName, oldMC.GenerateName) ||
		!reflect.DeepEqual(currentMC.Annotations, oldMC.Annotations) ||
		!reflect.DeepEqual(currentMC.OwnerReferences, oldMC.OwnerReferences) ||
		!reflect.DeepEqual(currentMC.Finalizers, oldMC.Finalizers) ||
		!reflect.DeepEqual(currentMC.ManagedFields, oldMC.ManagedFields) ||
		!reflect.DeepEqual(currentMC.APIVersion, oldMC.APIVersion)
}

// checkCRDGroup returns true if the input CRD group is a fleet CRD group.
func checkCRDGroup(group string) bool {
	return slices.Contains(fleetCRDGroups, group)
}
