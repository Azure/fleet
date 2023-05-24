package validation

import "k8s.io/utils/strings/slices"

var (
	validObjectGroups = []string{"networking.fleet.azure.com", "fleet.azure.com", "multicluster.x-k8s.io"}
)

// CheckCRDGroup checks to see if the input CRD group is a fleet CRD group.
func CheckCRDGroup(group string) bool {
	return slices.Contains(validObjectGroups, group)
}
