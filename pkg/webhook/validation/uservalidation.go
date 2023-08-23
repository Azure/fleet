package validation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	mastersGroup         = "system:masters"
	serviceAccountsGroup = "system:serviceaccounts"
	serviceAccountFmt    = "system:serviceaccount:fleet-system:%s"

	imcStatusUpdateNotAllowedFormat = "user: %s in groups: %v is not allowed to update IMC status: %+v"
	imcAllowedGetMCFailed           = "user: %s in groups: %v is allowed to update IMC: %+v because we failed to get MC"
	crdAllowedFormat                = "user: %s in groups: %v is allowed to modify fleet CRD: %+v"
	crdDeniedFormat                 = "user: %s in groups: %v is not allowed to modify fleet CRD: %+v"
	resourceAllowedFormat           = "user: %s in groups: %v is allowed to modify resource %s: %+v"
	resourceDeniedFormat            = "user: %s in groups: %v is not allowed to modify resource %s: %+v"
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

// ValidateUserForResource checks to see if user is allowed to modify argued resource.
func ValidateUserForResource(resKind string, namespacedName types.NamespacedName, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	if isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) || isUserAuthenticatedServiceAccount(userInfo) {
		klog.V(2).InfoS("user in groups is allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resKind, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(resourceAllowedFormat, userInfo.Username, userInfo.Groups, resKind, namespacedName))
	}
	klog.V(2).InfoS("user in groups is not allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resKind, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(resourceDeniedFormat, userInfo.Username, userInfo.Groups, resKind, namespacedName))
}

// ValidateMCOrNSUpdate checks to see if user is allowed to update argued member cluster or namespace resource.
func ValidateMCOrNSUpdate(currentObj, oldObj client.Object, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	kind := currentObj.GetObjectKind().GroupVersionKind().Kind
	response := admission.Allowed(fmt.Sprintf("user %s in groups %v most likely updated read-only field/fields of member cluster resource, so no field/fields will be updated", userInfo.Username, userInfo.Groups))
	namespacedName := types.NamespacedName{Name: currentObj.GetName()}
	isLabelUpdated := isMapFieldUpdated(currentObj.GetLabels(), oldObj.GetLabels())
	isAnnotationUpdated := isMapFieldUpdated(currentObj.GetAnnotations(), oldObj.GetAnnotations())
	isObjUpdated, err := isMCOrNSUpdated(currentObj, oldObj)
	if err != nil {
		return admission.Denied(err.Error())
	}
	if (isLabelUpdated || isAnnotationUpdated) && !isObjUpdated {
		// we allow any user to modify MemberCluster/Namespace labels/annotations.
		klog.V(2).InfoS("user in groups is allowed to modify member cluster labels/annotations", "user", userInfo.Username, "groups", userInfo.Groups, "kind", kind, "namespacedName", namespacedName)
		response = admission.Allowed(fmt.Sprintf(resourceAllowedFormat, userInfo.Username, userInfo.Groups, kind, namespacedName))
	}
	if isObjUpdated {
		response = ValidateUserForResource(kind, types.NamespacedName{Name: currentObj.GetName()}, whiteListedUsers, userInfo)
	}
	return response
}

// ValidateInternalMemberClusterUpdate checks to see if user is allowed to update argued internal member cluster resource.
func ValidateInternalMemberClusterUpdate(ctx context.Context, client client.Client, currentIMC, oldIMC fleetv1alpha1.InternalMemberCluster, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	imcKind := "InternalMemberCluster"
	namespacedName := types.NamespacedName{Name: currentIMC.Name, Namespace: currentIMC.Namespace}
	imcSpecUpdated := isInternalMemberClusterSpecUpdated(currentIMC, oldIMC)
	imcStatusUpdated := isInternalMemberClusterStatusUpdated(currentIMC.Status, oldIMC.Status)
	if !imcSpecUpdated && imcStatusUpdated {
		var mc fleetv1alpha1.MemberCluster
		if err := client.Get(ctx, types.NamespacedName{Name: namespacedName.Name}, &mc); err != nil {
			// fail open, if the webhook cannot get member cluster resources we don't block the request.
			klog.V(2).ErrorS(err, "failed to get member cluster resource for request to modify internal member cluster, allowing request to be handled by api server",
				"user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
			return admission.Allowed(fmt.Sprintf(imcAllowedGetMCFailed, userInfo.Username, userInfo.Groups, namespacedName))
		}
		// For the upstream E2E we use hub agent service account's token which allows member agent to modify IMC status, hence we use serviceAccountFmt to make the check.
		if mc.Spec.Identity.Name == userInfo.Username || fmt.Sprintf(serviceAccountFmt, mc.Spec.Identity.Name) == userInfo.Username {
			klog.V(2).InfoS("user in groups is allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", imcKind, "namespacedName", namespacedName)
			return admission.Allowed(fmt.Sprintf(resourceAllowedFormat, userInfo.Username, userInfo.Groups, "InternalMemberCluster", namespacedName))
		}
		klog.V(2).InfoS("user is not allowed to update IMC status", "user", userInfo.Username, "groups", userInfo.Groups, "kind", imcKind, "namespacedName", namespacedName)
		return admission.Denied(fmt.Sprintf(imcStatusUpdateNotAllowedFormat, userInfo.Username, userInfo.Groups, namespacedName))
	}
	return ValidateUserForResource(currentIMC.Kind, namespacedName, whiteListedUsers, userInfo)
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
func isMapFieldUpdated(currentMCLabels, oldMCLabels map[string]string) bool {
	return !reflect.DeepEqual(currentMCLabels, oldMCLabels)
}

// isInternalMemberClusterSpecUpdated returns true if internal member cluster spec is updated.
func isInternalMemberClusterSpecUpdated(currentIMC, oldIMC fleetv1alpha1.InternalMemberCluster) bool {
	return currentIMC.Generation > oldIMC.Generation
}

// isInternalMemberClusterStatusUpdated returns true if internal member cluster status is updated.
func isInternalMemberClusterStatusUpdated(currentIMCStatus, oldIMCStatus fleetv1alpha1.InternalMemberClusterStatus) bool {
	// cannot compare resource versions because they are the same when webhook receives the request.
	return !reflect.DeepEqual(currentIMCStatus, oldIMCStatus)
}

// isMemberClusterUpdated returns true is member cluster spec or status is updated.
func isMCOrNSUpdated(currentObj, oldObj client.Object) (bool, error) {
	// Set labels, annotations to be nil. Read-only field updates are not received by the admission webhook.
	currentObj.SetLabels(nil)
	currentObj.SetAnnotations(nil)
	oldObj.SetLabels(nil)
	oldObj.SetAnnotations(nil)
	// Remove all live fields from current MC objectMeta.
	currentObj.SetSelfLink("")
	currentObj.SetUID("")
	currentObj.SetResourceVersion("")
	currentObj.SetGeneration(0)
	currentObj.SetCreationTimestamp(metav1.Time{})
	currentObj.SetDeletionTimestamp(nil)
	currentObj.SetDeletionGracePeriodSeconds(nil)
	currentObj.SetManagedFields(nil)
	// Remove all live fields from old MC objectMeta.
	oldObj.SetSelfLink("")
	oldObj.SetUID("")
	oldObj.SetResourceVersion("")
	oldObj.SetGeneration(0)
	oldObj.SetCreationTimestamp(metav1.Time{})
	oldObj.SetDeletionTimestamp(nil)
	oldObj.SetDeletionGracePeriodSeconds(nil)
	oldObj.SetManagedFields(nil)

	currentMCBytes, err := json.Marshal(currentObj)
	if err != nil {
		return false, err
	}
	oldMCBytes, err := json.Marshal(oldObj)
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
