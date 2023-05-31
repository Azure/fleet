package validation

import (
	"context"
	"regexp"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	authenticatedGroup  = "system:authenticated"
	mastersGroup        = "system:masters"
	serviceAccountGroup = "system:serviceaccounts"
	bootstrapGroup      = "system:bootstrappers"

	serviceAccountUser = "system:serviceaccount"
)

// TODO: Get valid user names as flag and check to validate those user names.

// ValidateUser checks to see if user is authenticated to make a request to the hub cluster's api-server.
func ValidateUser(ctx context.Context, client client.Client, userInfo authenticationv1.UserInfo) bool {
	if slices.Contains(userInfo.Groups, mastersGroup) {
		return true
	}
	if slices.Contains(userInfo.Groups, bootstrapGroup) && slices.Contains(userInfo.Groups, authenticatedGroup) {
		return true
	}
	// This ensures all internal service accounts are validated
	if slices.Contains(userInfo.Groups, serviceAccountGroup) {
		match := regexp.MustCompile(serviceAccountUser).FindStringSubmatch(userInfo.Username)[1]
		if match != "" {
			return true
		}
	}
	// list all the member clusters
	var memberClusterList fleetv1alpha1.MemberClusterList
	if err := client.List(ctx, &memberClusterList); err != nil {
		// log error
		return false
	}
	return false
}
