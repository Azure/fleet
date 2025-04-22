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

package work

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSetModifiedConfigurationAnnotation(t *testing.T) {
	smallObj, _, _, err := createObjAndDynamicClient(testManifest.Raw)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}
	largeObj, err := createLargeObj()
	if err != nil {
		t.Errorf("failed to create large obj: %s", err)
	}

	tests := map[string]struct {
		obj      runtime.Object
		wantBool bool
		wantErr  error
	}{
		"last applied config annotation is set": {
			obj:      smallObj,
			wantBool: true,
			wantErr:  nil,
		},
		"last applied config annotation is set to an empty string": {
			obj:      largeObj,
			wantBool: false,
			wantErr:  nil,
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotBool, gotErr := setModifiedConfigurationAnnotation(testCase.obj)
			assert.Equalf(t, testCase.wantBool, gotBool, "got bool not matching for Testcase %s", testName)
			assert.Equalf(t, testCase.wantErr, gotErr, "got error not matching for Testcase %s", testName)
		})
	}
}
