package validation

import (
	"context"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	mastersGroup = "system:masters"
)

// ValidateUserForCRD checks to see if user is authenticated to make a request to modify fleet CRDs.
func ValidateUserForCRD(whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	return isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo)
}

// ValidateUserForFleetCR checks to see if user is authenticated to make a request to modify Fleet CRs.
func ValidateUserForFleetCR(ctx context.Context, client client.Client, whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	if isMasterGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
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

func isMasterGroupUserOrWhiteListedUser(whiteListedUsers []string, userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(whiteListedUsers, userInfo.Username) || slices.Contains(userInfo.Groups, mastersGroup)
}
