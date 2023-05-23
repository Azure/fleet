package validation

import (
	"k8s.io/utils/strings/slices"
)

const (
	authenticatedGroup = "system:authenticated"
	mastersGroup       = "system:masters"
)

// TODO: Get valid user names as flag and check to validate those user names.

// ValidateUserGroups checks to see if user is authorized to make a request to the hub cluster's api-server.
func ValidateUserGroups(groups []string) bool {
	if slices.Contains(groups, authenticatedGroup) || slices.Contains(groups, mastersGroup) {
		return true
	}
	return false
}
