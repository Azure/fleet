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

package workgenerator

import (
	"crypto/sha256"
	"fmt"
	"strings"

	placementv1alpha1 "go.goms.io/fleet/apis/kubefleet.dev/placement/v1alpha1"
)

const (
	nameLenLimit = 251
	hashSegLen   = 12
)

const (
	// The name format for work objects when they are derived from placement resource snapshots.
	// Typically, these objects are named using the format:
	//
	// `[PLACEMENT-POLICY-NAMESPACED-NAME]-work-[HASH]`, if the work is derived from the primary
	// placement resource snapshot, or
	// `[PLACEMENT-POLICY-NAMESPACED-NAME]-work-[DERIVED-FROM-SOURCE-LABEL]-[HASH]`, if the work is
	// derived from other sources (e.g., a secondary placement resource snapshot).
	//
	// where
	//
	// * `[PLACEMENT-POLICY-NAMESPACED-NAME]` is the namespace and name of the placement policy that owns the
	//   placement resource snapshot (and indirectly owns the work objects via placement binding), in the
	//   format `[NAMESPACE]-[NAME]` (if the placement policy is cluster-scoped, the namespaced name segment is
	//   simply the placement policy name);
	// * `[DERIVED-FROM-SOURCE-LABEL]` is a label that helps identify the source where the work object is derived
	//   from; it is not guaranteed to be unique and is added for informational purposes only.
	// * `[HASH]` is the first few characters of the hash of the value
	// 	 `[PLACEMENT-POLICY-NAMESPACE]/[PLACEMENT-POLICY-NAME]-work` or
	//   `[PLACEMENT-POLICY-NAMESPACE]/[PLACEMENT-POLICY-NAME]-work-[DERIVED-FROM-SOURCE-TYPE]/[DERIVED-FROM-SOURCE-ID]`
	//   respectively, where `[DERIVED-FROM-SOURCE-TYPE]` is the type of the source where the work object is
	//   derived from (e.g., `placement-resource-snapshot` for placement resource snapshots), and
	//   `[DERIVED-FROM-SOURCE-ID]` is an identifier of the source object. Together the segment uniquely identifies
	//   the source where the work object is derived from (among all the work objects that are created/updated
	//   for the placement binding).
	//
	//   The slash is used here instead of a dash to avoid collisions between different namespace/name combinations,
	//   e.g., to make sure that a placement policy named `red` in namespace `team-a` and a placement policy named
	//   `a-red` in namespace `team` do not produce the same hash.
	//
	// If the name becomes too long (> 251 characters), KubeFleet will truncate the placement policy namespaced name
	// segment and the derived from source marker segment as appropriate.
	workDerivedFromPrimarySnapshotSourceNameFmt = "%s-work-%s"
	workDerivedFromOtherSourcesNameFmt          = "%s-work-%s-%s"
)

// uniqueNameForWorkDerivedFromPlacementResourceSnapshot generates a unique name for a work object derived from a
// placement resource snapshot, given the owner placement binding and the snapshot sub-index (0 = primary).
func uniqueNameForWorkDerivedFromPlacementResourceSnapshot(
	placementBinding placementv1alpha1.PlacementBindingAccessor,
	isFromPrimarySnapshot bool,
	derivedFromSrcFormatter derivedFromSourceFormatter,
) string {
	namespace := placementBinding.GetNamespace()
	policyName := placementBinding.GetSpec().PlacementPolicyName

	// The namespaced name of the owner placement policy, in the format `[NAMESPACE]-[NAME]`; for cluster-scoped
	// placement policies, it is simply the placement policy name.
	namespacedName := policyName
	if namespace != "" {
		namespacedName = fmt.Sprintf("%s-%s", namespace, policyName)
	}

	// The hash is computed over the namespace and name (separated by a slash) plus the derived from source marker,
	// so that different namespace/name combinations never collide, and so that a hash suffix is always present.
	hashInput := fmt.Sprintf("%s/%s-work", namespace, policyName)
	if !isFromPrimarySnapshot {
		hashInput = fmt.Sprintf("%s/%s-work-%s/%s", namespace, policyName, derivedFromSrcFormatter.SourceType(), derivedFromSrcFormatter.SourceID())
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))[:hashSegLen]

	// Remove all dots from the namespaced name segment so that truncation cannot leave a trailing dot,
	// which would produce an invalid DNS subdomain label.
	namespacedName = strings.ReplaceAll(namespacedName, ".", "")

	if isFromPrimarySnapshot {
		// The work is derived from the primary placement resource snapshot; the name omits the source marker segment.
		name := fmt.Sprintf(workDerivedFromPrimarySnapshotSourceNameFmt, namespacedName, hash)
		if len(name) <= nameLenLimit {
			return name
		}

		// The name is too long; truncate the namespaced name segment. The hash suffix always disambiguates.
		reservedLen := len(fmt.Sprintf(workDerivedFromPrimarySnapshotSourceNameFmt, "", hash))
		availableLen := nameLenLimit - reservedLen
		if len(namespacedName) > availableLen {
			namespacedName = namespacedName[:availableLen]
		}
		return fmt.Sprintf(workDerivedFromPrimarySnapshotSourceNameFmt, namespacedName, hash)
	}

	// The work is derived from another source (e.g., a secondary placement resource snapshot); the name carries
	// the source marker segment.
	derivedFromSrcLabel := derivedFromSrcFormatter.StrictDNSLabel()
	name := fmt.Sprintf(workDerivedFromOtherSourcesNameFmt, namespacedName, derivedFromSrcLabel, hash)
	if len(name) <= nameLenLimit {
		return name
	}

	// The name is too long; truncate the namespaced name and source marker segments, splitting the available
	// space evenly between them. The hash suffix always disambiguates.
	reservedLen := len(fmt.Sprintf(workDerivedFromOtherSourcesNameFmt, "", "", hash))
	availableLen := nameLenLimit - reservedLen
	availablePerSeg := availableLen / 2
	if len(namespacedName) > availablePerSeg {
		namespacedName = namespacedName[:availablePerSeg]
	}
	if len(derivedFromSrcLabel) > availablePerSeg {
		derivedFromSrcLabel = derivedFromSrcLabel[:availablePerSeg]
	}
	return fmt.Sprintf(workDerivedFromOtherSourcesNameFmt, namespacedName, derivedFromSrcLabel, hash)
}
