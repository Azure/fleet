package controllers

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/stretchr/testify/assert"
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
