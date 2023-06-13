package validation

import (
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/utils/strings/slices"
)

const (
	mastersGroup = "system:masters"
)

// TODO:(Arvindthiru) Get valid usernames as flag and allow those usernames.

// ValidateUserForCRD checks to see if user is authenticated to make a request to modify fleet CRDs.
func ValidateUserForCRD(userInfo authenticationv1.UserInfo) bool {
	return slices.Contains(userInfo.Groups, mastersGroup)
}
