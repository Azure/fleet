/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package uniquename features some utilities that are used to generate unique names.
package uniquename

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	uuidLength = 8
)

// minInt returns the smaller one of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NewClusterResourceBindingName returns a unique name for a cluster resource binding in the
// format of DNS label names (RFC 1123). It will be used as a label on the work resource.
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
// are valid DNS label names (RFC 1123).
func NewClusterResourceBindingName(CRPName string, clusterName string) (string, error) {
	reservedSlots := 2 + uuidLength // 2 dashes + 8 character UUID string

	slotsPerSeg := (validation.DNS1123LabelMaxLength - reservedSlots) / 2
	uniqueName := fmt.Sprintf("%s-%s-%s",
		CRPName[:minInt(slotsPerSeg, len(CRPName))],
		clusterName[:minInt(slotsPerSeg+1, len(clusterName))],
		uuid.NewUUID()[:uuidLength],
	)

	if errs := validation.IsDNS1123Label(uniqueName); len(errs) != 0 {
		// Do a sanity check here; normally this would not occur.
		return "", fmt.Errorf("failed to format a unique RFC 1123 label name with CRP name %s, cluster name %s: %v", CRPName, clusterName, errs)
	}
	return uniqueName, nil
}

// NewClusterApprovalRequestName returns a unique name for a cluster approval request in the
// format of DNS subdomain names (RFC 1123).
//
// The name is generated using the following format:
// * [CLUSTER-STAGED-UPDATE-RUN-NAME] - [STAGE-NAME] - [RANDOM-SUFFIX]
//
// Segments will be truncated if necessary.
//
// Note that the name generation is, in essence, a best-effort process, though the chances
// of name collisions are extremely low.
//
// In addition, note that this function assumes that both the ClusterStagedUpdateRun name and the stage name
// are valid DNS subdomain names (RFC 1123).
func NewClusterApprovalRequestName(updateRunName, stageName string) (string, error) {
	reservedSlots := 2 + uuidLength // 2 dashes + 8 character UUID string
	stageName = strings.ToLower(stageName)

	slotsPerSeg := (validation.DNS1123SubdomainMaxLength - reservedSlots) / 2
	uniqueName := fmt.Sprintf("%s-%s-%s",
		updateRunName[:minInt(slotsPerSeg, len(updateRunName))],
		stageName[:minInt(slotsPerSeg+1, len(stageName))],
		uuid.NewUUID()[:uuidLength],
	)

	if errs := validation.IsDNS1123Subdomain(uniqueName); len(errs) != 0 {
		// Do a sanity check here; normally this would not occur.
		return "", fmt.Errorf("failed to format a unique RFC 1123 submain name with update run name %s, stage name %s: %v", updateRunName, stageName, errs)
	}
	return uniqueName, nil
}
