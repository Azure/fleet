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
	"fmt"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// The maximum length of approval request names, as documented.
	wantMaxApprovalRequestNameLength = 200
	// The maximum length of staged update run names (i.e., Kubernetes object names).
	maxStagedUpdateRunNameLength = 253
	// The maximum length of stage names, as the staged update strategy API enforces.
	maxStageNameLength = 63
)

// Truncated approval request names end with a hyphen and an 8-char (hex) hash.
var hashSuffixRegexp = regexp.MustCompile(`-[0-9a-f]{8}$`)

// dottedName returns a valid DNS subdomain name of the given length, with a dot as every 10th character, starting
// from the given offset (1-9), except for the last character, e.g., dottedName(23, 9) = "abcdefghi.klmnopqrs.uvw".
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

// TestFormatBeforeStageApprovalRequestName tests the formatBeforeStageApprovalRequestName function.
func TestFormatBeforeStageApprovalRequestName(t *testing.T) {
	testCases := []struct {
		name                string
		stagedUpdateRunName string
		stageName           string
		wantName            string
	}{
		{
			name:                "short names",
			stagedUpdateRunName: "run-1",
			stageName:           canaryStageName,
			wantName:            "run-1-before-stage-canary",
		},
		{
			// 123 + 14 + 63 = 200 characters.
			name:                "full name at the length limit (not truncated)",
			stagedUpdateRunName: strings.Repeat("a", 123),
			stageName:           strings.Repeat("s", 63),
			wantName:            strings.Repeat("a", 123) + "-before-stage-" + strings.Repeat("s", 63),
		},
		{
			// 124 + 14 + 63 = 201 characters; the run name is truncated to 88 characters ((200 - 14 - 9) / 2), and the
			// stage name fits as is.
			name:                "full name over the length limit (truncated, with hash)",
			stagedUpdateRunName: strings.Repeat("a", 124),
			stageName:           strings.Repeat("s", 63),
			wantName:            strings.Repeat("a", 88) + "-before-stage-" + strings.Repeat("s", 63) + "-bac2165e",
		},
		{
			// The run name is cut right after a dot (the 88th character), which is trimmed.
			name:                "truncated run name ending with a dot",
			stagedUpdateRunName: strings.Repeat("a", 87) + "." + strings.Repeat("b", 165),
			stageName:           canaryStageName,
			wantName:            strings.Repeat("a", 87) + "-before-stage-canary-0ff3d5af",
		},
		{
			// The staged update strategy API limits stage names to 63 characters, which never need truncation;
			// the function still truncates longer ones.
			name:                "both names truncated",
			stagedUpdateRunName: strings.Repeat("a", 150),
			stageName:           strings.Repeat("s", 100),
			wantName:            strings.Repeat("a", 88) + "-before-stage-" + strings.Repeat("s", 88) + "-bf938dd2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotName := formatBeforeStageApprovalRequestName(tc.stagedUpdateRunName, tc.stageName)
			if gotName != tc.wantName {
				t.Errorf("formatBeforeStageApprovalRequestName(%q, %q) = %q, want %q", tc.stagedUpdateRunName, tc.stageName, gotName, tc.wantName)
			}
		})
	}
}

// TestFormatAfterStageApprovalRequestName tests the formatAfterStageApprovalRequestName function.
func TestFormatAfterStageApprovalRequestName(t *testing.T) {
	testCases := []struct {
		name                string
		stagedUpdateRunName string
		stageName           string
		wantName            string
	}{
		{
			name:                "short names",
			stagedUpdateRunName: "run-1",
			stageName:           canaryStageName,
			wantName:            "run-1-after-stage-canary",
		},
		{
			// 124 + 13 + 63 = 200 characters.
			name:                "full name at the length limit (not truncated)",
			stagedUpdateRunName: strings.Repeat("a", 124),
			stageName:           strings.Repeat("s", 63),
			wantName:            strings.Repeat("a", 124) + "-after-stage-" + strings.Repeat("s", 63),
		},
		{
			// 125 + 13 + 63 = 201 characters; the run name is truncated to 89 characters ((200 - 13 - 9) / 2), and the
			// stage name fits as is.
			name:                "full name over the length limit (truncated, with hash)",
			stagedUpdateRunName: strings.Repeat("a", 125),
			stageName:           strings.Repeat("s", 63),
			wantName:            strings.Repeat("a", 89) + "-after-stage-" + strings.Repeat("s", 63) + "-60f7932f",
		},
		{
			// The run name is cut right after a dot (the 89th character), which is trimmed.
			name:                "truncated run name ending with a dot",
			stagedUpdateRunName: strings.Repeat("a", 88) + "." + strings.Repeat("b", 164),
			stageName:           canaryStageName,
			wantName:            strings.Repeat("a", 88) + "-after-stage-canary-cdbf87d2",
		},
		{
			// The staged update strategy API limits stage names to 63 characters, which never need truncation;
			// the function still truncates longer ones.
			name:                "both names truncated",
			stagedUpdateRunName: strings.Repeat("a", 150),
			stageName:           strings.Repeat("s", 100),
			wantName:            strings.Repeat("a", 89) + "-after-stage-" + strings.Repeat("s", 89) + "-551fb0a0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotName := formatAfterStageApprovalRequestName(tc.stagedUpdateRunName, tc.stageName)
			if gotName != tc.wantName {
				t.Errorf("formatAfterStageApprovalRequestName(%q, %q) = %q, want %q", tc.stagedUpdateRunName, tc.stageName, gotName, tc.wantName)
			}
		})
	}
}

// TestFormatApprovalRequestNameUniqueness verifies that the hash suffix keeps truncated approval request names apart.
func TestFormatApprovalRequestNameUniqueness(t *testing.T) {
	// The two run names share their first 100 characters, i.e., their truncated forms are the same.
	runName1 := strings.Repeat("a", 100) + "x" + strings.Repeat("c", 99)
	runName2 := strings.Repeat("a", 100) + "y" + strings.Repeat("c", 99)
	stageName := canaryStageName

	formatFuncs := map[string]func(string, string) string{
		"formatBeforeStageApprovalRequestName": formatBeforeStageApprovalRequestName,
		"formatAfterStageApprovalRequestName":  formatAfterStageApprovalRequestName,
	}
	for funcName, formatFunc := range formatFuncs {
		t.Run(funcName, func(t *testing.T) {
			name1 := formatFunc(runName1, stageName)
			name2 := formatFunc(runName2, stageName)
			if name1 == name2 {
				t.Errorf("%s(%q, %q) = %s(%q, %q) = %q, want different names", funcName, runName1, stageName, funcName, runName2, stageName, name1)
			}

			// The names differ only in their hash suffixes.
			if !hashSuffixRegexp.MatchString(name1) || !hashSuffixRegexp.MatchString(name2) {
				t.Fatalf("%s() = %q, %q, want both names to end with a hash suffix", funcName, name1, name2)
			}
			prefix1 := hashSuffixRegexp.ReplaceAllString(name1, "")
			prefix2 := hashSuffixRegexp.ReplaceAllString(name2, "")
			if prefix1 != prefix2 {
				t.Errorf("%s() = %q, %q, want the same name before the hash suffix", funcName, name1, name2)
			}

			// The function is deterministic.
			if again := formatFunc(runName1, stageName); again != name1 {
				t.Errorf("%s(%q, %q) = %q, then %q, want the same name", funcName, runName1, stageName, name1, again)
			}
		})
	}

	// The before-stage and after-stage approval requests of the same stage have different names.
	beforeStageName := formatBeforeStageApprovalRequestName(runName1, stageName)
	afterStageName := formatAfterStageApprovalRequestName(runName1, stageName)
	if beforeStageName == afterStageName {
		t.Errorf("formatBeforeStageApprovalRequestName(%q, %q) = formatAfterStageApprovalRequestName(%q, %q) = %q, want different names",
			runName1, stageName, runName1, stageName, beforeStageName)
	}
}

// TestFormatApprovalRequestNameLengthAndValidity verifies, for all the valid lengths of staged update run names (and
// a few lengths of stage names), that approval request names never exceed the length limit, are left as is when
// they fit, carry a hash suffix when they do not, and are valid Kubernetes object names (DNS subdomains).
//
// The run names have dots at varying positions, so that some of them are truncated right after a dot (which must be
// trimmed for the result to stay a valid name).
func TestFormatApprovalRequestNameLengthAndValidity(t *testing.T) {
	formatFuncs := []struct {
		name         string
		formatFunc   func(string, string) string
		fullNameTmpl string
	}{
		{
			name:         "formatBeforeStageApprovalRequestName",
			formatFunc:   formatBeforeStageApprovalRequestName,
			fullNameTmpl: "%s-before-stage-%s",
		},
		{
			name:         "formatAfterStageApprovalRequestName",
			formatFunc:   formatAfterStageApprovalRequestName,
			fullNameTmpl: "%s-after-stage-%s",
		},
	}

	for _, ff := range formatFuncs {
		t.Run(ff.name, func(t *testing.T) {
			for _, stageNameLength := range []int{1, 10, maxStageNameLength} {
				stageName := strings.Repeat("s", stageNameLength)
				for dotOffset := 1; dotOffset <= 9; dotOffset++ {
					for runNameLength := 1; runNameLength <= maxStagedUpdateRunNameLength; runNameLength++ {
						runName := dottedName(runNameLength, dotOffset)
						gotName := ff.formatFunc(runName, stageName)
						fullName := fmt.Sprintf(ff.fullNameTmpl, runName, stageName)

						if len(gotName) > wantMaxApprovalRequestNameLength {
							t.Errorf("%s(%q, %q) = %q (%d characters), want at most %d characters",
								ff.name, runName, stageName, gotName, len(gotName), wantMaxApprovalRequestNameLength)
						}
						if len(fullName) <= wantMaxApprovalRequestNameLength && gotName != fullName {
							t.Errorf("%s(%q, %q) = %q, want %q", ff.name, runName, stageName, gotName, fullName)
						}
						if len(fullName) > wantMaxApprovalRequestNameLength && !hashSuffixRegexp.MatchString(gotName) {
							t.Errorf("%s(%q, %q) = %q, want a name with a hash suffix", ff.name, runName, stageName, gotName)
						}
						if errs := validation.IsDNS1123Subdomain(gotName); len(errs) > 0 {
							t.Errorf("%s(%q, %q) = %q, want a valid DNS subdomain name (errors: %v)", ff.name, runName, stageName, gotName, errs)
						}
					}
				}
			}
		})
	}
}
