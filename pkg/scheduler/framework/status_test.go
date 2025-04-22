/*
Copyright 2025 The KubeFleet Authors.

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

package framework

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const (
	dummyPlugin = "dummyPlugin"
)

var (
	dummyReasons = []string{"reason1", "reason2"}
)

func TestNonNilStatusMethods(t *testing.T) {
	testCases := []struct {
		name         string
		statusCode   StatusCode
		reasons      []string
		err          error
		sourcePlugin string
		desc         string
	}{
		{
			name:         "status success",
			statusCode:   Success,
			reasons:      []string{},
			sourcePlugin: dummyPlugin,
		},
		{
			name:         "status error",
			statusCode:   internalError,
			err:          fmt.Errorf("an unexpected error has occurred"),
			reasons:      dummyReasons,
			sourcePlugin: dummyPlugin,
		},
		{
			name:         "status unschedulable",
			statusCode:   ClusterUnschedulable,
			reasons:      dummyReasons,
			sourcePlugin: dummyPlugin,
		},
		{
			name:         "status skip",
			statusCode:   Skip,
			reasons:      dummyReasons,
			sourcePlugin: dummyPlugin,
		},
		{
			name:         "status cluster already selected",
			statusCode:   ClusterAlreadySelected,
			reasons:      dummyReasons,
			sourcePlugin: dummyPlugin,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var status *Status
			if tc.err != nil {
				status = FromError(tc.err, tc.sourcePlugin, tc.reasons...)
			} else {
				status = NewNonErrorStatus(tc.statusCode, tc.sourcePlugin, tc.reasons...)
			}

			wantCheckOutputs := make([]bool, len(statusCodeNames))
			wantCheckOutputs[tc.statusCode] = true
			checkFuncs := []func() bool{
				status.IsSuccess,
				status.IsInteralError,
				status.IsClusterUnschedulable,
				status.IsClusterAlreadySelected,
				status.IsSkip,
			}
			for idx, checkFunc := range checkFuncs {
				if wantCheckOutputs[idx] != checkFunc() {
					t.Fatalf("check function for %s = %t, want %t", statusCodeNames[idx], checkFunc(), wantCheckOutputs[idx])
				}
			}

			if !cmp.Equal(status.Reasons(), tc.reasons) {
				t.Fatalf("Reasons() = %v, want %v", status.Reasons(), tc.reasons)
			}

			if !cmp.Equal(status.SourcePlugin(), tc.sourcePlugin) {
				t.Fatalf("SourcePlugin() = %s, want %s", status.SourcePlugin(), tc.sourcePlugin)
			}

			if !cmp.Equal(status.InternalError(), tc.err, cmpopts.EquateErrors()) {
				t.Fatalf("InternalError() = %v, want %v", status.InternalError(), tc.err)
			}

			descElems := []string{statusCodeNames[tc.statusCode]}
			if tc.err != nil {
				descElems = append(descElems, tc.err.Error())
			}
			descElems = append(descElems, tc.reasons...)
			wantDesc := strings.Join(descElems, ", ")
			if !cmp.Equal(status.String(), wantDesc) {
				t.Fatalf("String() = %s, want %s", status.String(), wantDesc)
			}
		})
	}
}

func TestNilStatusMethods(t *testing.T) {
	var status *Status
	wantCheckOutputs := make([]bool, len(statusCodeNames))
	wantCheckOutputs[Success] = true
	checkFuncs := []func() bool{
		status.IsSuccess,
		status.IsInteralError,
		status.IsClusterUnschedulable,
		status.IsSkip,
	}
	for idx, checkFunc := range checkFuncs {
		if wantCheckOutputs[idx] != checkFunc() {
			t.Fatalf("check function for %s = %t, want %t", statusCodeNames[idx], checkFunc(), wantCheckOutputs[idx])
		}
	}

	if !cmp.Equal(status.Reasons(), []string{}) {
		t.Fatalf("Reasons() = %v, want %v", status.Reasons(), []string{})
	}

	if !cmp.Equal(status.SourcePlugin(), "") {
		t.Fatalf("SourcePlugin() = %s, want %s", status.SourcePlugin(), "")
	}

	if !cmp.Equal(status.InternalError(), nil, cmpopts.EquateErrors()) {
		t.Fatalf("InternalError() = %v, want %v", status.InternalError(), nil)
	}

	wantDesc := statusCodeNames[Success]
	if !cmp.Equal(status.String(), wantDesc) {
		t.Fatalf("String() = %s, want %s", status.String(), wantDesc)
	}
}
