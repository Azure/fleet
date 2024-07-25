package validation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	mastersGroup              = "system:masters"
	serviceAccountsGroup      = "system:serviceaccounts"
	nodeGroup                 = "system:nodes"
	kubeSchedulerUser         = "system:kube-scheduler"
	kubeControllerManagerUser = "system:kube-controller-manager"
	serviceAccountFmt         = "system:serviceaccount:fleet-system:%s"

	allowedModifyResource       = "user in groups is allowed to modify resource"
	deniedModifyResource        = "user in groups is not allowed to modify resource"
	deniedAddFleetAnnotation    = "no user is allowed to add a fleet pre-fixed annotation to an upstream member cluster"
	deniedRemoveFleetAnnotation = "no user is allowed to remove all fleet pre-fixed annotations from a fleet member cluster"

	ResourceAllowedFormat      = "user: '%s' in '%s' is allowed to %s resource %+v/%s: %+v"
	ResourceDeniedFormat       = "user: '%s' in '%s' is not allowed to %s resource %+v/%s: %+v"
	ResourceAllowedGetMCFailed = "user: '%s' in '%s' is allowed to %s resource %+v/%s: %+v because we failed to get MC"
)

var (
	fleetCRDGroups = []string{"networking.fleet.azure.com", "fleet.azure.com", "multicluster.x-k8s.io", "cluster.kubernetes-fleet.io", "placement.kubernetes-fleet.io"}
)

// ValidateUserForFleetCRD checks to see if user is not allowed to modify fleet CRDs.
func ValidateUserForFleetCRD(req admission.Request, whiteListedUsers []string, group string) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	userInfo := req.UserInfo
	if checkCRDGroup(group) && !isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
		klog.V(2).InfoS(deniedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		return admission.Denied(fmt.Sprintf(ResourceDeniedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
	}
	klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
}

// ValidateUserForResource checks to see if user is allowed to modify argued resource modified by request.
func ValidateUserForResource(req admission.Request, whiteListedUsers []string) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	userInfo := req.UserInfo
	if isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) || isUserAuthenticatedServiceAccount(userInfo) || isUserKubeScheduler(userInfo) || isUserKubeControllerManager(userInfo) || isNodeGroupUser(userInfo) {
		klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
	}
	klog.V(2).InfoS(deniedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(ResourceDeniedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
}

// ValidateV1Alpha1MemberClusterUpdate checks to see if user had updated the member cluster resource and allows/denies the request.
func ValidateV1Alpha1MemberClusterUpdate(currentMC, oldMC fleetv1alpha1.MemberCluster, req admission.Request, whiteListedUsers []string) admission.Response {
	namespacedName := types.NamespacedName{Name: currentMC.GetName()}
	userInfo := req.UserInfo
	response := admission.Allowed(fmt.Sprintf("user %s in groups %v most likely %s read-only field/fields of member cluster resource %+v/%s, so no field/fields will be updated", userInfo.Username, userInfo.Groups, req.Operation, req.RequestKind, req.SubResource))
	isLabelUpdated := isMapFieldUpdated(currentMC.GetLabels(), oldMC.GetLabels())
	isAnnotationUpdated := isMapFieldUpdated(currentMC.GetAnnotations(), oldMC.GetAnnotations())
	isObjUpdated, err := isMemberClusterUpdated(&currentMC, &oldMC)
	if err != nil {
		return admission.Denied(err.Error())
	}
	if (isLabelUpdated || isAnnotationUpdated) && !isObjUpdated {
		// we allow any user to modify v1alpha1 MemberCluster labels & annotations.
		klog.V(3).InfoS("user in groups is allowed to modify member cluster labels/annotations", "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		response = admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
	}
	if isObjUpdated {
		response = ValidateUserForResource(req, whiteListedUsers)
	}
	return response
}

// ValidateFleetMemberClusterUpdate checks to see if user had updated the fleet member cluster resource and allows/denies the request.
func ValidateFleetMemberClusterUpdate(currentMC, oldMC clusterv1beta1.MemberCluster, req admission.Request, whiteListedUsers []string) admission.Response {
	namespacedName := types.NamespacedName{Name: currentMC.GetName()}
	userInfo := req.UserInfo
	// set taints field to nil.
	currentMC.Spec.Taints = nil
	oldMC.Spec.Taints = nil
	isAnnotationUpdated, err := isFleetAnnotationUpdated(currentMC.Annotations, oldMC.Annotations)
	if err != nil {
		klog.V(2).InfoS(deniedRemoveFleetAnnotation, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		return admission.Denied(deniedRemoveFleetAnnotation)
	}
	isObjUpdated, err := isMemberClusterUpdated(currentMC.DeepCopy(), oldMC.DeepCopy())
	if err != nil {
		return admission.Denied(err.Error())
	}
	if isObjUpdated || isAnnotationUpdated {
		return ValidateUserForResource(req, whiteListedUsers)
	}
	// any user is allowed to modify labels, annotations, taints on fleet MC except fleet pre-fixed annotations.
	klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
}

// ValidatedUpstreamMemberClusterUpdate checks to see if user had updated the upstream member cluster resource and allows/denies the request.
func ValidatedUpstreamMemberClusterUpdate(currentMC, oldMC clusterv1beta1.MemberCluster, req admission.Request, whiteListedUsers []string) admission.Response {
	namespacedName := types.NamespacedName{Name: currentMC.GetName()}
	userInfo := req.UserInfo
	if isFleetAnnotationAdded(currentMC.Annotations, oldMC.Annotations) {
		klog.V(2).InfoS(deniedAddFleetAnnotation, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		return admission.Denied(deniedAddFleetAnnotation)
	}
	// any user is allowed to modify MC spec for upstream MC.
	if isMemberClusterStatusUpdated(currentMC.Status, oldMC.Status) {
		return ValidateUserForResource(req, whiteListedUsers)
	}
	klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
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
func isMapFieldUpdated(currentMap, oldMap map[string]string) bool {
	return !reflect.DeepEqual(currentMap, oldMap)
}

// isFleetAnnotationUpdated returns true if fleet pre-fixed annotations are updated/deleted,
// also returns an error if all fleet pre-fixed annotations are removed.
func isFleetAnnotationUpdated(currentMap, oldMap map[string]string) (bool, error) {
	currentExists := utils.IsFleetAnnotationPresent(currentMap)
	oldExists := utils.IsFleetAnnotationPresent(oldMap)
	if oldExists && !currentExists {
		return true, errors.New("all fleet pre-fixed annotations are removed")
	}
	for oldKey, oldValue := range oldMap {
		if strings.HasPrefix(oldKey, utils.FleetAnnotationPrefix) {
			currentValue, exists := currentMap[oldKey]
			if exists {
				if currentValue != oldValue {
					return true, nil
				}
			} else {
				return true, nil
			}
		}
	}
	return false, nil
}

// isFleetAnnotationAdded returns true if fleet pre-fixed annotation is added.
func isFleetAnnotationAdded(currentMap, oldMap map[string]string) bool {
	currentExists := utils.IsFleetAnnotationPresent(currentMap)
	oldExists := utils.IsFleetAnnotationPresent(oldMap)
	return !oldExists && currentExists
}

// isMemberClusterStatusUpdated returns true if member cluster status is updated.
func isMemberClusterStatusUpdated(currentMCStatus, oldMCStatus clusterv1beta1.MemberClusterStatus) bool {
	return !equality.Semantic.DeepEqual(currentMCStatus, oldMCStatus)
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
func ValidateMCIdentity(ctx context.Context, client client.Client, req admission.Request, mcName string, isFleetV1Beta1API bool) admission.Response {
	var identity string
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	userInfo := req.UserInfo
	if !isFleetV1Beta1API {
		var mc fleetv1alpha1.MemberCluster
		if err := client.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
			// fail open, if the webhook cannot get member cluster resources we don't block the request.
			klog.ErrorS(err, fmt.Sprintf("failed to get v1alpha1 member cluster resource for request to modify %+v/%s, allowing request to be handled by api server", req.RequestKind, req.SubResource),
				"user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
			return admission.Allowed(fmt.Sprintf(ResourceAllowedGetMCFailed, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
		}
		identity = mc.Spec.Identity.Name
	} else {
		var mc clusterv1beta1.MemberCluster
		if err := client.Get(ctx, types.NamespacedName{Name: mcName}, &mc); err != nil {
			// fail open, if the webhook cannot get member cluster resources we don't block the request.
			klog.ErrorS(err, fmt.Sprintf("failed to get member cluster resource for request to modify %+v/%s, allowing request to be handled by api server", req.RequestKind, req.SubResource),
				"user", userInfo.Username, "groups", userInfo.Groups, "namespacedName", namespacedName)
			return admission.Allowed(fmt.Sprintf(ResourceAllowedGetMCFailed, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
		}
		identity = mc.Spec.Identity.Name
	}

	// For the upstream E2E we use hub agent service account's token which allows member agent to modify Work status, hence we use serviceAccountFmt to make the check.
	if identity == userInfo.Username || fmt.Sprintf(serviceAccountFmt, identity) == userInfo.Username {
		klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
	}
	klog.V(2).InfoS(deniedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Denied(fmt.Sprintf(ResourceDeniedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
}
