package validation

import (
	"context"
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	mastersGroup = "system:masters"
)

// ValidateUserForFleetCR checks to see if user is allowed to make a request to modify fleet CRs.
func ValidateUserForFleetCR(ctx context.Context, client client.Client, whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	if IsMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
		return true
	}
	var memberClusterList fleetv1alpha1.MemberClusterList
	if err := client.List(ctx, &memberClusterList); err != nil {
		klog.V(2).ErrorS(err, "failed to list member clusters")
		return false
	}
	identities := make([]string, len(memberClusterList.Items))
	for i := range memberClusterList.Items {
		identities = append(identities, memberClusterList.Items[i].Spec.Identity.Name)
	}
	// this ensures will allow all member agents are validated.
	return slices.Contains(identities, userInfo.Username)
}

// ValidateUserForResource checks to see if user is allowed to modify argued fleet resource.
func ValidateUserForResource(whiteListedUsers []string, userInfo authenticationv1.UserInfo, resKind, resName string) admission.Response {
	if !IsMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
		return admission.Denied(fmt.Sprintf("failed to validate user: %s in groups: %v to modify fleet resource %s: %s", userInfo.Username, userInfo.Groups, resKind, resName))
	}
	klog.V(2).InfoS("user in groups is allowed to modify fleet resource", "user", userInfo.Username, "groups", userInfo.Groups, resKind, resName)
	return admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify fleet resource %s: %s", userInfo.Username, userInfo.Groups, resKind, resName))
}

func IsMasterGroupUserOrWhiteListedUser(whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(whiteListedUsers, userInfo.Username) || slices.Contains(userInfo.Groups, mastersGroup)
}
