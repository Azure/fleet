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

package uniquename

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	shortPolicyName           = "policy"
	policyUID       types.UID = "uid-1"
	otherPolicyUID  types.UID = "uid-2"
)

// dottedName returns a name of the given length with a dot as every 10th character, starting from the given offset
// (0-9), except for the last character, e.g., dottedName(23, 9) = "abcdefghi.klmnopqrs.uvw".
func dottedName(length, dotOffset int) string {
	var sb strings.Builder
	for idx := 0; idx < length; idx++ {
		if idx%10 == dotOffset && idx != length-1 {
			sb.WriteByte('.')
			continue
		}
		sb.WriteByte(byte('a' + idx%26))
	}
	return sb.String()
}

// verifyName verifies that a generated name is as wanted, within the length limit, and a valid DNS subdomain.
func verifyName(t *testing.T, call, gotName, wantName string) {
	t.Helper()

	if gotName != wantName {
		t.Errorf("%s = %q, want %q", call, gotName, wantName)
	}
	if len(gotName) > nameLenLimit {
		t.Errorf("len(%s) = %d, want <= %d", call, len(gotName), nameLenLimit)
	}
	if errs := validation.IsDNS1123Subdomain(gotName); len(errs) > 0 {
		t.Errorf("%s = %q, which is not a valid DNS subdomain: %v", call, gotName, errs)
	}
}

// TestPrimaryPlacementResourceSnapshotName tests the PrimaryPlacementResourceSnapshotName function.
//
// The hashes below are the first 12 hex characters of the SHA-256 digests of the hash inputs documented in
// uniquename.go (e.g., `policy-resource-snapshot-0-uid-1`), computed independently of the implementation.
func TestPrimaryPlacementResourceSnapshotName(t *testing.T) {
	// The dot-free form of a dotted name, truncated to 209 characters (251 - len("-resource-snapshot-") - 10 - 1 - 12),
	// where 10 is the room reserved for the snapshot index.
	dottedPolicyName := dottedName(253, 5)
	wantTruncatedDottedPolicyName := strings.ReplaceAll(dottedPolicyName, ".", "")[:209]

	testCases := []struct {
		name       string
		policyName string
		policyUID  types.UID
		idx        int
		wantName   string
	}{
		{
			name:       "short name",
			policyName: shortPolicyName,
			policyUID:  policyUID,
			idx:        0,
			wantName:   "policy-resource-snapshot-0-dcb49e6b8793",
		},
		{
			// A re-created placement policy of the same name gets different snapshot names.
			name:       "short name, different placement policy UID",
			policyName: shortPolicyName,
			policyUID:  otherPolicyUID,
			idx:        0,
			wantName:   "policy-resource-snapshot-0-50e127870132",
		},
		{
			name:       "short name, multi-digit index",
			policyName: shortPolicyName,
			policyUID:  policyUID,
			idx:        12,
			wantName:   "policy-resource-snapshot-12-3f0897595cd2",
		},
		{
			// Dots are removed from the name segment, but the hash is computed over the original name.
			name:       "name with dots",
			policyName: "my.policy.v1",
			policyUID:  policyUID,
			idx:        0,
			wantName:   "mypolicyv1-resource-snapshot-0-df7214812235",
		},
		{
			name:       "exactly at the length limit (not truncated)",
			policyName: strings.Repeat("a", 218),
			policyUID:  policyUID,
			idx:        0,
			wantName:   strings.Repeat("a", 218) + "-resource-snapshot-0-7e773e2d43c3",
		},
		{
			name:       "one character over the length limit (truncated)",
			policyName: strings.Repeat("a", 219),
			policyUID:  policyUID,
			idx:        0,
			wantName:   strings.Repeat("a", 209) + "-resource-snapshot-0-cbb5025ff3ee",
		},
		{
			// The room reserved for the index fits the largest int32 value; the name is right at the length limit.
			name:       "longest valid name with the largest int32 index (truncated)",
			policyName: strings.Repeat("a", 253),
			policyUID:  policyUID,
			idx:        2147483647,
			wantName:   strings.Repeat("a", 209) + "-resource-snapshot-2147483647-3f30dad5f71c",
		},
		{
			// Truncation happens after the dots are removed, so the name never ends with a dot.
			name:       "dotted name over the length limit (truncated)",
			policyName: dottedPolicyName,
			policyUID:  policyUID,
			idx:        0,
			wantName:   wantTruncatedDottedPolicyName + "-resource-snapshot-0-0b77f00041b6",
		},
		{
			// When the name needs sanitization, an index longer than the reserved room is truncated as well; the
			// hash (computed over the full index) keeps the name unique.
			name:       "index over 10 digits with a name over the length limit (both truncated)",
			policyName: strings.Repeat("a", 219),
			policyUID:  policyUID,
			idx:        12345678901,
			wantName:   strings.Repeat("a", 209) + "-resource-snapshot-1234567890-c9a32e12e9c1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotName := PrimaryPlacementResourceSnapshotName(tc.policyName, tc.policyUID, tc.idx)
			call := fmt.Sprintf("PrimaryPlacementResourceSnapshotName(%q, %q, %d)", tc.policyName, tc.policyUID, tc.idx)
			verifyName(t, call, gotName, tc.wantName)
		})
	}
}

// TestSecondaryPlacementResourceSnapshotName tests the SecondaryPlacementResourceSnapshotName function.
//
// The hashes below are the first 12 hex characters of the SHA-256 digests of the hash inputs documented in
// uniquename.go (e.g., `policy-resource-snapshot-0-1-uid-1`), computed independently of the implementation.
func TestSecondaryPlacementResourceSnapshotName(t *testing.T) {
	// The dot-free form of a dotted name, truncated to 207 characters (251 - len("-resource-snapshot-") - 12 - 1 - 12),
	// where 12 is the room reserved for the snapshot index and sub-index.
	dottedPolicyName := dottedName(253, 5)
	wantTruncatedDottedPolicyName := strings.ReplaceAll(dottedPolicyName, ".", "")[:207]

	testCases := []struct {
		name       string
		policyName string
		policyUID  types.UID
		idx        int
		subIdx     int
		wantName   string
	}{
		{
			name:       "short name",
			policyName: shortPolicyName,
			policyUID:  policyUID,
			idx:        0,
			subIdx:     1,
			wantName:   "policy-resource-snapshot-0-1-a314eee601b8",
		},
		{
			// A re-created placement policy of the same name gets different snapshot names.
			name:       "short name, different placement policy UID",
			policyName: shortPolicyName,
			policyUID:  otherPolicyUID,
			idx:        0,
			subIdx:     1,
			wantName:   "policy-resource-snapshot-0-1-1ab4f5f5cf91",
		},
		{
			name:       "short name, multi-digit sub-index",
			policyName: shortPolicyName,
			policyUID:  policyUID,
			idx:        0,
			subIdx:     12,
			wantName:   "policy-resource-snapshot-0-12-c09358bdf1d3",
		},
		{
			// Dots are removed from the name segment, but the hash is computed over the original name.
			name:       "name with dots",
			policyName: "my.policy.v1",
			policyUID:  policyUID,
			idx:        0,
			subIdx:     1,
			wantName:   "mypolicyv1-resource-snapshot-0-1-804481f6a22a",
		},
		{
			name:       "exactly at the length limit (not truncated)",
			policyName: strings.Repeat("a", 216),
			policyUID:  policyUID,
			idx:        0,
			subIdx:     1,
			wantName:   strings.Repeat("a", 216) + "-resource-snapshot-0-1-04c0e779ae04",
		},
		{
			name:       "one character over the length limit (truncated)",
			policyName: strings.Repeat("a", 217),
			policyUID:  policyUID,
			idx:        0,
			subIdx:     1,
			wantName:   strings.Repeat("a", 207) + "-resource-snapshot-0-1-2b4e249846f0",
		},
		{
			// The room reserved for the index segment fits the largest int32 index and a single-digit sub-index;
			// the name is right at the length limit.
			name:       "longest valid name with the largest int32 index (truncated)",
			policyName: strings.Repeat("a", 253),
			policyUID:  policyUID,
			idx:        2147483647,
			subIdx:     9,
			wantName:   strings.Repeat("a", 207) + "-resource-snapshot-2147483647-9-e61b33ff2355",
		},
		{
			// Truncation happens after the dots are removed, so the name never ends with a dot.
			name:       "dotted name over the length limit (truncated)",
			policyName: dottedPolicyName,
			policyUID:  policyUID,
			idx:        0,
			subIdx:     1,
			wantName:   wantTruncatedDottedPolicyName + "-resource-snapshot-0-1-7e5b8d1134ab",
		},
		{
			// When the name needs sanitization, an index segment longer than the reserved room is truncated as
			// well; the hash (computed over the full index and sub-index) keeps the name unique.
			name:       "index segment over 12 characters with a name over the length limit (both truncated)",
			policyName: strings.Repeat("a", 253),
			policyUID:  policyUID,
			idx:        2147483647,
			subIdx:     15,
			wantName:   strings.Repeat("a", 207) + "-resource-snapshot-2147483647-1-8a62c7e54a25",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotName := SecondaryPlacementResourceSnapshotName(tc.policyName, tc.policyUID, tc.idx, tc.subIdx)
			call := fmt.Sprintf("SecondaryPlacementResourceSnapshotName(%q, %q, %d, %d)", tc.policyName, tc.policyUID, tc.idx, tc.subIdx)
			verifyName(t, call, gotName, tc.wantName)
		})
	}
}

// TestPlacementResourceSnapshotNameUniqueness verifies that the generated names are deterministic and distinct
// across placement policies (including re-created ones of the same name), indices, and sub-indices.
func TestPlacementResourceSnapshotNameUniqueness(t *testing.T) {
	type input struct {
		policyName string
		policyUID  types.UID
		idx        int
		// A negative sub-index stands for a primary snapshot.
		subIdx int
	}
	longNameX := strings.Repeat("a", 240) + "x"
	longNameY := strings.Repeat("a", 240) + "y"
	inputs := []input{
		{policyName: shortPolicyName, policyUID: policyUID, idx: 0, subIdx: -1},
		{policyName: shortPolicyName, policyUID: policyUID, idx: 0, subIdx: 1},
		{policyName: shortPolicyName, policyUID: policyUID, idx: 0, subIdx: 2},
		{policyName: shortPolicyName, policyUID: policyUID, idx: 1, subIdx: -1},
		{policyName: shortPolicyName, policyUID: policyUID, idx: 1, subIdx: 1},
		{policyName: shortPolicyName, policyUID: policyUID, idx: 11, subIdx: -1},
		// A re-created placement policy of the same name.
		{policyName: shortPolicyName, policyUID: otherPolicyUID, idx: 0, subIdx: -1},
		{policyName: shortPolicyName, policyUID: otherPolicyUID, idx: 0, subIdx: 1},
		// Names that collide after dot removal.
		{policyName: "ab.c", policyUID: policyUID, idx: 0, subIdx: -1},
		{policyName: "a.bc", policyUID: policyUID, idx: 0, subIdx: -1},
		{policyName: "abc", policyUID: policyUID, idx: 0, subIdx: -1},
		{policyName: "ab.c", policyUID: policyUID, idx: 0, subIdx: 1},
		{policyName: "a.bc", policyUID: policyUID, idx: 0, subIdx: 1},
		// Names that differ only beyond the truncation point.
		{policyName: longNameX, policyUID: policyUID, idx: 0, subIdx: -1},
		{policyName: longNameY, policyUID: policyUID, idx: 0, subIdx: -1},
		{policyName: longNameX, policyUID: policyUID, idx: 0, subIdx: 1},
		{policyName: longNameY, policyUID: policyUID, idx: 0, subIdx: 1},
		// Index segments that are the same after truncation.
		{policyName: longNameX, policyUID: policyUID, idx: 2147483647, subIdx: 15},
		{policyName: longNameX, policyUID: policyUID, idx: 2147483647, subIdx: 16},
	}

	nameOf := func(in input) string {
		if in.subIdx < 0 {
			return PrimaryPlacementResourceSnapshotName(in.policyName, in.policyUID, in.idx)
		}
		return SecondaryPlacementResourceSnapshotName(in.policyName, in.policyUID, in.idx, in.subIdx)
	}

	seen := make(map[string]input, len(inputs))
	for _, in := range inputs {
		gotName := nameOf(in)
		if gotAgain := nameOf(in); gotName != gotAgain {
			t.Errorf("name for %+v = %q then %q, want a deterministic result", in, gotName, gotAgain)
		}
		if other, found := seen[gotName]; found {
			t.Errorf("name for %+v = %q, which collides with the name for %+v", in, gotName, other)
		}
		seen[gotName] = in
	}
}
