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

// Package naming derives the names and label values of objects that KubeFleet generates on a
// user's behalf.
//
// A controller that names an object after another one runs into the same two problems every time:
// a name legal for a Kubernetes object is not always legal where the controller puts it (object
// names run to 253 bytes, label values stop at 63), and shortening a name to fit can both produce
// a syntactically invalid result and collapse two distinct identities onto one string. Getting
// either wrong fails at the API server rather than at the call site, which surfaces as a
// reconciler that retries forever while the user sees nothing happen at all.
//
// The helpers here exist so that the answer is written once. They are deliberately primitive:
// callers compose the identity that is hashed, since only the caller knows what makes one of its
// objects distinct from another.
package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// HashLength is the number of hexadecimal characters of Hash's output, i.e. 64 bits of a
	// SHA-256 sum. A narrower hash is tempting and wrong: these hashes are what keep two distinct
	// objects from generating one name, and whichever of them loses the race is not merely
	// misnamed, it silently never gets its object at all.
	HashLength = 16

	// labelValuePrefixMaxLength is how much of an over-long value survives in front of the hash
	// that LabelValue appends, allowing for the separator between the two.
	labelValuePrefixMaxLength = validation.LabelValueMaxLength - HashLength - 1

	// separators are the characters a Kubernetes name may contain but may not begin or end with.
	separators = "-_."
)

// Hash returns a short, stable, collision-resistant digest of s.
//
// Callers should hash the whole identity of an object, never the shortened form of it that appears
// in a generated name: shortening is precisely what collapses two identities into one, so a hash
// taken afterwards would agree where the identities differ.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:HashLength]
}

// TrimSeparators removes the separators that a name may not end with, which truncating an
// otherwise valid name can leave exposed.
//
// The result can be empty only if the fragment consisted entirely of separators; since Kubernetes
// object names must begin with an alphanumeric character, a fragment taken from the front of one
// never can.
func TrimSeparators(fragment string) string {
	return strings.TrimRight(fragment, separators)
}

// Truncate shortens s to at most maxLength bytes, leaving a result that is still legal at the end
// of a Kubernetes name.
//
// It does not make the result unique; a caller that truncates must append Hash of the full,
// untruncated identity for that.
//
// A budget of zero or less leaves nothing, which is returned as the empty string rather than as a
// panic: this package exists so that a caller's arithmetic mistake surfaces as a name it can
// inspect, not as a crash in a reconcile loop.
func Truncate(s string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	if len(s) <= maxLength {
		return s
	}
	return TrimSeparators(s[:maxLength])
}

// Sanitize renders s legal as a single label of a generated DNS-1123 name: it lowercases the value
// and replaces every character outside [a-z0-9-] with a dash. A name legal for its own API but not
// as a Kubernetes object name -- an RBAC name like system:aggregate-to-admin, whose colon a DNS-1123
// name forbids -- becomes usable this way.
//
// The dot is mapped to a dash along with everything else, rather than kept as a label separator: a
// dot is only legal between two alphanumerics, so preserving it would let an invalid character next
// to it (or another dot) produce an empty or dash-bounded label that the API server still rejects --
// the very failure Sanitize exists to prevent. Its one caller joins the sanitized parts with dashes
// anyway, so no dot is needed to keep them apart.
//
// It is lossy: distinct inputs can sanitize to the same string, so a caller must still append Hash
// of the full, unsanitized identity for uniqueness.
func Sanitize(s string) string {
	return TrimSeparators(strings.TrimLeft(mapInvalid(strings.ToLower(s), isDNS1123Char), separators))
}

// sanitizeLabelValue is Sanitize's counterpart for a label value, whose legal character set is
// wider (it keeps case and underscores). It exists so LabelValue never returns a value the API
// server would reject; like Sanitize it is lossy and relies on a hash for identity.
func sanitizeLabelValue(value string) string {
	return TrimSeparators(strings.TrimLeft(mapInvalid(value, isLabelValueChar), separators))
}

func mapInvalid(s string, valid func(rune) bool) string {
	return strings.Map(func(r rune) rune {
		if valid(r) {
			return r
		}
		return '-'
	}, s)
}

func isDNS1123Char(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}

func isLabelValueChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
}

// LabelValue renders value so that it fits in a label value, sanitizing characters the API server
// would reject and shortening it to a prefix and a hash of the whole when it is too long.
//
// The result is lossy by design -- both sanitization and shortening can collapse distinct values --
// and callers must treat it that way: selecting on the label with the original value matches nothing
// once it has been rewritten, so anything that needs to resolve an exact identity has to read it
// from a field that has neither a character nor a length limit.
func LabelValue(value string) string {
	sanitized := sanitizeLabelValue(value)
	if len(sanitized) <= validation.LabelValueMaxLength {
		return sanitized
	}
	return fmt.Sprintf("%s-%s", TrimSeparators(sanitized[:labelValuePrefixMaxLength]), Hash(value))
}
