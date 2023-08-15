package validation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"

	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	fleetCRDGroups = []string{"networking.fleet.azure.com", "fleet.azure.com", "multicluster.x-k8s.io", "cluster.kubernetes-fleet.io", "placement.kubernetes-fleet.io"}
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

// ValidateMemberClusterUpdate checks to see if user is allowed to update argued fleet resource.
func ValidateMemberClusterUpdate(currentMC, oldMC fleetv1alpha1.MemberCluster, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	response := admission.Allowed(fmt.Sprintf("user %s in groups %v most likely updated read-only field/fields of member cluster resource, so no field/fields will be updated", userInfo.Username, userInfo.Groups))
	namespacedName := types.NamespacedName{Name: currentMC.Name}
	isMCLabelUpdated := isMemberClusterMapFieldUpdated(currentMC.Labels, oldMC.Labels)
	isMCAnnotationUpdated := isMemberClusterMapFieldUpdated(currentMC.Annotations, oldMC.Annotations)
	isMCUpdated, err := isMemberClusterUpdated(currentMC, oldMC)
	if err != nil {
		return admission.Denied(err.Error())
	}
	if (isMCLabelUpdated || isMCAnnotationUpdated) && !isMCUpdated {
		// we allow any user to modify MemberCluster labels/annotations.
		klog.V(2).InfoS("user in groups is allowed to modify member cluster labels/annotations", "user", userInfo.Username, "groups", userInfo.Groups, "kind", currentMC.Kind, "namespacedName", namespacedName)
		response = admission.Allowed(fmt.Sprintf(fleetResourceAllowedFormat, userInfo.Username, userInfo.Groups, currentMC.Kind, namespacedName))
	}
	if isMCUpdated {
		response = ValidateUserForFleetCR(currentMC.Kind, types.NamespacedName{Name: currentMC.Name}, whiteListedUsers, userInfo)
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

// isMemberClusterMapFieldUpdated return true if member cluster label is updated.
func isMemberClusterMapFieldUpdated(currentMCLabels, oldMCLabels map[string]string) bool {
	return !reflect.DeepEqual(currentMCLabels, oldMCLabels)
}

// isMemberClusterUpdated returns true is member cluster spec or status is updated.
func isMemberClusterUpdated(currentMC, oldMC fleetv1alpha1.MemberCluster) (bool, error) {
	// Set labels, annotations to be nil. Read-only field updates are not received by the admission webhook.
	currentMC.SetLabels(nil)
	currentMC.SetAnnotations(nil)
	oldMC.SetLabels(nil)
	oldMC.SetAnnotations(nil)
	// Remove all live fields from current MC objectMeta.
	currentMC.SetSelfLink("")
	currentMC.SetUID("")
	currentMC.SetResourceVersion("")
	currentMC.SetGeneration(0)
	currentMC.SetCreationTimestamp(v1.Time{})
	currentMC.SetDeletionTimestamp(nil)
	currentMC.SetDeletionGracePeriodSeconds(nil)
	currentMC.SetManagedFields(nil)
	// Remove all live fields from old MC objectMeta.
	oldMC.SetSelfLink("")
	oldMC.SetUID("")
	oldMC.SetResourceVersion("")
	oldMC.SetGeneration(0)
	oldMC.SetCreationTimestamp(v1.Time{})
	oldMC.SetDeletionTimestamp(nil)
	oldMC.SetDeletionGracePeriodSeconds(nil)
	oldMC.SetManagedFields(nil)

	currentMCBytes, err := json.Marshal(currentMC)
	if err != nil {
		return false, err
	}
	oldMCBytes, err := json.Marshal(oldMC)
	if err != nil {
		return false, err
	}
	currentMCHash := sha256.Sum256(currentMCBytes)
	oldMCHash := sha256.Sum256(oldMCBytes)

	return currentMCHash != oldMCHash, nil
}

// checkCRDGroup returns true if the input CRD group is a fleet CRD group.
func checkCRDGroup(group string) bool {
	return slices.Contains(fleetCRDGroups, group)
}
