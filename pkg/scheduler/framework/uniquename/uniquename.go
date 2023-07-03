/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package uniquename features some utilities that are used to generate unique names in use
// by the scheduler.
package uniquename

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	uuidLength = 6
)

// minInt returns the smaller one of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NewClusterResourceBindingName returns a unique name for a cluster resource binding in the
// format of DNS subdomain names (RFC 1123).
//
// The name is generated using the following format:
// * [CRP-NAME] - [TARGET-CLUSTER-NAME] - [RANDOM-SUFFIX]
//
// Segments will be truncated if necessary.
//
// Note that the name generation is, in essence, a best-effort process, though the chances
// of name collisions are extremely low.
//
// In addition, note that this function assumes that both the CRP name and the cluster name
// are valid DNS subdomain names (RFC 1123).
func NewClusterResourceBindingName(CRPName string, clusterName string) (string, error) {
	reservedSlots := 2 + uuidLength // 2 dashs + 6 character UUID string

	slotsPerSeg := (validation.DNS1123SubdomainMaxLength - reservedSlots) / 2
	uniqueName := fmt.Sprintf("%s-%s-%s",
		CRPName[:minInt(slotsPerSeg, len(CRPName))],
		clusterName[:minInt(slotsPerSeg, len(clusterName))],
		uuid.NewUUID()[:uuidLength],
	)

	if errs := validation.IsDNS1123Subdomain(uniqueName); len(errs) != 0 {
		// Do a sanity check here; normally this would not occur.
		return "", fmt.Errorf("failed to format a unique RFC 1123 DNS subdomain name with namespace %s, name %s: %v", CRPName, clusterName, errs)
	}
	return uniqueName, nil
}
