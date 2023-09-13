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
	mastersGroup              = "system:masters"
	serviceAccountsGroup      = "system:serviceaccounts"
	nodeGroup                 = "system:nodes"
	kubeSchedulerUser         = "system:kube-scheduler"
	kubeControllerManagerUser = "system:kube-controller-manager"
	serviceAccountFmt         = "system:serviceaccount:fleet-system:%s"

	resourceAllowedGetMCFailed = "user: %s in groups: %v is allowed to updated %s: %+v because we failed to get MC"
	crdAllowedFormat           = "user: %s in groups: %v is allowed to modify fleet CRD: %+v"
	crdDeniedFormat            = "user: %s in groups: %v is not allowed to modify fleet CRD: %+v"
	resourceAllowedFormat      = "user: %s in groups: %v is allowed to modify resource %s: %+v"
	resourceDeniedFormat       = "user: %s in groups: %v is not allowed to modify resource %s: %+v"
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
	if isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) || isUserAuthenticatedServiceAccount(userInfo) || isUserKubeScheduler(userInfo) || isUserKubeControllerManager(userInfo) || isNodeGroupUser(userInfo) {
		klog.V(2).InfoS("user in groups is allowed to modify resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resKind, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(resourceAllowedFormat, userInfo.Username, userInfo.Groups, resKind, namespacedName))
	}
	klog.V(2).InfoS("user in groups is not allowed to modify resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resKind, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(resourceDeniedFormat, userInfo.Username, userInfo.Groups, resKind, namespacedName))
}

// ValidateMemberClusterUpdate checks to see if user had updated the member cluster resource and allows/denies the request.
func ValidateMemberClusterUpdate(currentObj, oldObj client.Object, whiteListedUsers []string, userInfo authenticationv1.UserInfo) admission.Response {
	kind := currentObj.GetObjectKind().GroupVersionKind().Kind
	response := admission.Allowed(fmt.Sprintf("user %s in groups %v most likely updated read-only field/fields of member cluster resource, so no field/fields will be updated", userInfo.Username, userInfo.Groups))
	namespacedName := types.NamespacedName{Name: currentObj.GetName()}
	isLabelUpdated := isMapFieldUpdated(currentObj.GetLabels(), oldObj.GetLabels())
	isAnnotationUpdated := isMapFieldUpdated(currentObj.GetAnnotations(), oldObj.GetAnnotations())
	isObjUpdated, err := isMemberClusterUpdated(currentObj, oldObj)
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

// isMasterGroupUserOrWhiteListedUser returns true is user belongs to white listed users or user belongs to system:masters group.
func isMasterGroupUserOrWhiteListedUser(whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(whiteListedUsers, userInfo.Username) || slices.Contains(userInfo.Groups, mastersGroup)
}

// isUserAuthenticatedServiceAccount returns true if user is a valid service account.
func isUserAuthenticatedServiceAccount(userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(userInfo.Groups, serviceAccountsGroup)
}

// isUserKubeScheduler returns true if user is kube-scheduler.
func isUserKubeScheduler(userInfo authenticationv1.UserInfo) bool {
	// system:kube-scheduler user only belongs to system:authenticated group hence comparing username.
	return userInfo.Username == kubeSchedulerUser
}

// isUserKubeControllerManager return true if user is kube-controller-manager.
func isUserKubeControllerManager(userInfo authenticationv1.UserInfo) bool {
	// system:kube-controller-manager user only belongs to system:authenticated group hence comparing username.
	return userInfo.Username == kubeControllerManagerUser
}

// isNodeGroupUser returns true if user belongs to system:nodes group.
func isNodeGroupUser(userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(userInfo.Groups, nodeGroup)
}

// isMemberClusterMapFieldUpdated return true if member cluster label is updated.
func isMapFieldUpdated(currentMCLabels, oldMCLabels map[string]string) bool {
	return !reflect.DeepEqual(currentMCLabels, oldMCLabels)
}

// isMemberClusterUpdated returns true is member cluster spec or status is updated.
func isMemberClusterUpdated(currentObj, oldObj client.Object) (bool, error) {
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

// ValidateMCIdentity returns admission allowed/denied based on the member cluster's identity.
func ValidateMCIdentity(ctx context.Context, client client.Client, userInfo authenticationv1.UserInfo, resourceNamespacedName types.NamespacedName, resourceKind, subResource, mcName string) admission.Response {
	var mc fleetv1alpha1.MemberCluster
	if err := client.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
		// fail open, if the webhook cannot get member cluster resources we don't block the request.
		klog.V(2).ErrorS(err, fmt.Sprintf("failed to get member cluster resource for request to modify %s, allowing request to be handled by api server", resourceKind),
			"user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", resourceNamespacedName)
		return admission.Allowed(fmt.Sprintf(resourceAllowedGetMCFailed, userInfo.Username, userInfo.Groups, resourceKind, resourceNamespacedName))
	}
	// For the upstream E2E we use hub agent service account's token which allows member agent to modify Work status, hence we use serviceAccountFmt to make the check.
	if mc.Spec.Identity.Name == userInfo.Username || fmt.Sprintf(serviceAccountFmt, mc.Spec.Identity.Name) == userInfo.Username {
		klog.V(2).InfoS("user in groups is allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", resourceNamespacedName, "kind", resourceKind, "subResource", subResource)
		return admission.Allowed(fmt.Sprintf(resourceAllowedFormat, userInfo.Username, userInfo.Groups, resourceKind, resourceNamespacedName))
	}
	klog.V(2).InfoS("user is not allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, "kind", resourceKind, "namespacedName", resourceNamespacedName, "kind", resourceKind, "subResource", subResource)
	return admission.Denied(fmt.Sprintf(resourceDeniedFormat, userInfo.Username, userInfo.Groups, resourceKind, resourceNamespacedName))
}
