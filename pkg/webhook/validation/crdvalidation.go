package validation

import "k8s.io/utils/strings/slices"

var (
	validObjectGroups = []string{"networking.fleet.azure.com", "fleet.azure.com", "multicluster.x-k8s.io"}
)

func ValidateObjectGroup(group string) bool {
	return slices.Contains(validObjectGroups, group)
}
