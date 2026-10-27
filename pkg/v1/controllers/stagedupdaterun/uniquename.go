/*
Copyright 2026 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package stagedupdaterun

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	beforeStageApprovalRequestNameFmt         = "%s-before-stage-%s"
	beforeStageApprovalRequestWithHashNameFmt = "%s-before-stage-%s-%s"
	afterStageApprovalRequestNameFmt          = "%s-after-stage-%s"
	afterStageApprovalRequestWithHashNameFmt  = "%s-after-stage-%s-%s"

	// Kubernetes custom resources have a name limit of 253 characters. Here the limit is set to 200 to allow
	// a small safety margin.
	approvalRequestNameMaxLength  = 200
	approvalRequestNameHashLength = 8
)

// formatBeforeStageApprovalRequestName formats the approval request name as
// [STAGED-UPDATE-RUN-NAME]-before-stage-[STAGE-NAME].
//
// If the full name is longer than 200 characters, it truncates the two dynamic segments and
// appends an 8-char hash of the full (untruncated) name to avoid collisions.
func formatBeforeStageApprovalRequestName(stagedUpdateRunName, stageName string) string {
	// Note: 14 is the length of the static part of the name: "-before-stage-".
	return formatApprovalRequestName(beforeStageApprovalRequestNameFmt,
		beforeStageApprovalRequestWithHashNameFmt, 14, stagedUpdateRunName, stageName)
}

// formatAfterStageApprovalRequestName formats the approval request name as
// [STAGED-UPDATE-RUN-NAME]-after-stage-[STAGE-NAME].
//
// If the full name is longer than 200 characters, it truncates the two dynamic
// segments and appends an 8-char hash of the full (untruncated) name to avoid
// collisions.
func formatAfterStageApprovalRequestName(stagedUpdateRunName, stageName string) string {
	// Note: 13 is the length of the static part of the name: "-after-stage-".
	return formatApprovalRequestName(afterStageApprovalRequestNameFmt,
		afterStageApprovalRequestWithHashNameFmt, 13, stagedUpdateRunName, stageName)
}

func formatApprovalRequestName(noHashTemplate, withHashTemplate string, staticCharLen int, stagedUpdateRunName, stageName string) string {
	fullName := fmt.Sprintf(noHashTemplate, stagedUpdateRunName, stageName)
	if len(fullName) <= approvalRequestNameMaxLength {
		return fullName
	}

	// The additional one is the extra hyphen before the hash.
	availableSlots := approvalRequestNameMaxLength - staticCharLen - approvalRequestNameHashLength - 1
	stagedUpdateRunSlots := availableSlots / 2
	stageSlots := stagedUpdateRunSlots
	truncatedRunName := stagedUpdateRunName
	if len(truncatedRunName) > stagedUpdateRunSlots {
		truncatedRunName = truncatedRunName[:stagedUpdateRunSlots]
		truncatedRunName = strings.TrimRight(truncatedRunName, ".")
	}
	truncatedStageName := stageName
	if len(truncatedStageName) > stageSlots {
		truncatedStageName = truncatedStageName[:stageSlots]
	}

	sum := sha256.Sum256([]byte(fullName))
	hash := hex.EncodeToString(sum[:])[:approvalRequestNameHashLength]
	return fmt.Sprintf(withHashTemplate, truncatedRunName, truncatedStageName, hash)
}
