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

// Package uniquename generates the names of placement resource snapshots, which are unique across placement
// policies (including re-created ones of the same name) and always valid object names.
package uniquename

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

const (
	nameLenLimit = 251
	hashSegLen   = 12
)

const (
	// The name format for primary placement resource snapshots is
	//
	// `[PLACEMENT-POLICY-NAME]-resource-snapshot-[SNAPSHOT-INDEX]-[HASH]`,
	//
	// where:
	//
	// * `[PLACEMENT-POLICY-NAME]` is the name of the owner placement policy;
	// * `[SNAPSHOT-INDEX]` is the monotonically increasing index of the snapshot;
	// * `[HASH]` is the first few characters of the hash of the value
	//   `[PLACEMENT-POLICY-NAME]-resource-snapshot-[SNAPSHOT-INDEX]-[The UID of the placement policy]`.
	//
	//   The UID part ensures that snapshot names are never reused across placement policies of the same name
	//   (e.g., when a placement policy is deleted and re-created, and the snapshot index restarts from 0).
	//   KubeFleet tracks resource rollouts by snapshot names (placement bindings, work objects, and placement
	//   policy statuses all refer to a primary snapshot by its name); a reused name with different contents
	//   could be mistaken as already synchronized. Note that snapshots left over from previous placement policies
	//   are detected via owner references, not via names.
	//
	// If the name becomes too long (> 251 characters) or contains dots, KubeFleet will drop the dots and truncate the
	// placement policy name segment and the snapshot index segment as appropriate.
	PrimaryPlacementResourceSnapshotNameWithHashFmt = "%s-resource-snapshot-%s-%s"

	// The name format for secondary placement resource snapshots is
	//
	// `[PLACEMENT-POLICY-NAME]-resource-snapshot-[SNAPSHOT-INDEX]-[SNAPSHOT-SUB-INDEX]-[HASH]`,
	//
	// where:
	//
	// * `[PLACEMENT-POLICY-NAME]` is the name of the owner placement policy;
	// * `[SNAPSHOT-INDEX]` is the monotonically increasing index of the snapshot;
	// * `[SNAPSHOT-SUB-INDEX]` is the monotonically increasing sub-index of the snapshot;
	// * `[HASH]` is the first few characters of the hash of the value
	//   `[PLACEMENT-POLICY-NAME]-resource-snapshot-[SNAPSHOT-INDEX]-[SNAPSHOT-SUB-INDEX]-[The UID of the placement policy]`.
	//
	//   As with primary snapshots, the UID part ensures that snapshot names are never reused across placement
	//   policies of the same name (e.g., when a placement policy is deleted and re-created), so that a secondary
	//   snapshot never shares its name with one left over from a previous placement policy.
	//
	// If the name becomes too long (> 251 characters) or contains dots, KubeFleet will drop the dots and truncate the
	// placement policy name segment and the `[SNAPSHOT-INDEX]-[SNAPSHOT-SUB-INDEX]` segment as appropriate.
	SecondaryPlacementResourceSnapshotNameWithHashFmt = "%s-resource-snapshot-%s-%s"
)

// PrimaryPlacementResourceSnapshotName returns the name of the primary placement resource snapshot at the given
// index for a placement policy.
func PrimaryPlacementResourceSnapshotName(placementPolicyName string, placementPolicyUID types.UID, idx int) string {
	// The hash is computed over the full (unsanitized and untruncated) placement policy name, the snapshot index,
	// and the UID of the placement policy.
	//
	// Note that here only the first few (12) characters are kept. This does lead to increased risk of name
	// collisions, but the chances still remain extremely low. If such a collision does occur, manual intervention
	// is needed for resolution.
	hashInput := fmt.Sprintf("%s-resource-snapshot-%d-%s", placementPolicyName, idx, placementPolicyUID)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))[:hashSegLen]

	idxStr := strconv.Itoa(idx)
	name := fmt.Sprintf(PrimaryPlacementResourceSnapshotNameWithHashFmt, placementPolicyName, idxStr, hash)
	if len(name) <= nameLenLimit && !strings.Contains(name, ".") {
		return name
	}

	// The name is too long or contains dots; sanitize and truncate the placement policy name segment and the
	// snapshot index segment.

	// Compute how many characters are left for the two variable segments (the placement policy name and the
	// snapshot index); the index segment gets the space it needs, and the placement policy name segment takes
	// whatever remains.
	//
	// reservedLen accounts for the static decoration and the hash suffix only.
	//
	// The offset 1 is the length of placeholder index (0).
	reservedLen := len(fmt.Sprintf(PrimaryPlacementResourceSnapshotNameWithHashFmt, "", "0", hash)) - 1
	availableLen := nameLenLimit - reservedLen
	availableLenForIdxSeg := 10 // The maximum number of digits for an int32 value.
	availableLenForNameSeg := availableLen - availableLenForIdxSeg

	// Remove all dots from the placement policy name segment so that truncation cannot leave a trailing dot,
	// which would produce an invalid DNS subdomain label.
	truncatedPlacementPolicyName := strings.ReplaceAll(placementPolicyName, ".", "")
	if len(truncatedPlacementPolicyName) > availableLenForNameSeg {
		truncatedPlacementPolicyName = truncatedPlacementPolicyName[:availableLenForNameSeg]
	}

	truncatedIdxStr := idxStr
	if len(truncatedIdxStr) > availableLenForIdxSeg {
		truncatedIdxStr = truncatedIdxStr[:availableLenForIdxSeg]
	}

	return fmt.Sprintf(PrimaryPlacementResourceSnapshotNameWithHashFmt, truncatedPlacementPolicyName, truncatedIdxStr, hash)
}

// SecondaryPlacementResourceSnapshotName returns the name of the secondary placement resource snapshot at the given
// index and sub-index for a placement policy.
func SecondaryPlacementResourceSnapshotName(placementPolicyName string, placementPolicyUID types.UID, idx, subIdx int) string {
	// The hash is computed over the full (unsanitized and untruncated) placement policy name, the snapshot index,
	// the snapshot sub-index, and the UID of the placement policy.
	//
	// Note that here only the first few (12) characters are kept. This does lead to increased risk of name
	// collisions, but the chances still remain extremely low. If such a collision does occur, manual intervention
	// is needed for resolution.
	hashInput := fmt.Sprintf("%s-resource-snapshot-%d-%d-%s", placementPolicyName, idx, subIdx, placementPolicyUID)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))[:hashSegLen]

	idxStr := fmt.Sprintf("%d-%d", idx, subIdx)
	name := fmt.Sprintf(SecondaryPlacementResourceSnapshotNameWithHashFmt, placementPolicyName, idxStr, hash)
	if len(name) <= nameLenLimit && !strings.Contains(name, ".") {
		return name
	}

	// The name is too long or contains dots; sanitize and truncate the placement policy name segment and the
	// combined snapshot index/sub-index segment.

	// Compute how many characters are left for the two variable segments (the placement policy name and the
	// combined snapshot index/sub-index segment); the index segment gets the space it needs, and the placement
	// policy name segment takes whatever remains.
	//
	// reservedLen accounts for the static decoration and the hash suffix only.
	reservedLen := len(fmt.Sprintf(SecondaryPlacementResourceSnapshotNameWithHashFmt, "", "", hash))
	availableLen := nameLenLimit - reservedLen
	availableLenForIdxSeg := 12 // The maximum number of characters for an int32 value, plus the room for a dash and a single-digit sub-index.
	availableLenForNameSeg := availableLen - availableLenForIdxSeg

	// Remove all dots from the placement policy name segment so that truncation cannot leave a trailing dot,
	// which would produce an invalid DNS subdomain label.
	truncatedPlacementPolicyName := strings.ReplaceAll(placementPolicyName, ".", "")
	if len(truncatedPlacementPolicyName) > availableLenForNameSeg {
		truncatedPlacementPolicyName = truncatedPlacementPolicyName[:availableLenForNameSeg]
	}

	truncatedIdxStr := idxStr
	if len(truncatedIdxStr) > availableLenForIdxSeg {
		truncatedIdxStr = truncatedIdxStr[:availableLenForIdxSeg]
	}

	return fmt.Sprintf(SecondaryPlacementResourceSnapshotNameWithHashFmt, truncatedPlacementPolicyName, truncatedIdxStr, hash)
}
