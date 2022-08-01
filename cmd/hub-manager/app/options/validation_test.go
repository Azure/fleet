/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// a callback function to modify options
type ModifyOptions func(option *Options)

// New an Options with default parameters
func New(modifyOptions ModifyOptions) Options {
	option := Options{
		SkippedPropagatingAPIs: "fleet.azure.com;multicluster.x-k8s.io",
		SecurePort:             8090,
		WorkPendingGracePeriod: metav1.Duration{Duration: 10 * time.Second},
	}

	if modifyOptions != nil {
		modifyOptions(&option)
	}
	return option
}

func TestValidateControllerManagerConfiguration(t *testing.T) {
	successCases := []Options{
		New(nil),
	}

	for _, successCase := range successCases {
		if errs := successCase.Validate(); len(errs) != 0 {
			t.Errorf("expected success: %v", errs)
		}
	}

	newPath := field.NewPath("Options")
	testCases := map[string]struct {
		opt          Options
		expectedErrs field.ErrorList
	}{
		"invalid SkippedPropagatingAPIs": {
			opt: New(func(options *Options) {
				options.SkippedPropagatingAPIs = "a/b/c/d?"
			}),
			expectedErrs: field.ErrorList{field.Invalid(newPath.Child("SkippedPropagatingAPIs"), "a/b/c/d?", "Invalid API string")},
		},
		"invalid SecurePort": {
			opt: New(func(options *Options) {
				options.SecurePort = -10
			}),
			expectedErrs: field.ErrorList{field.Invalid(newPath.Child("SecurePort"), -10, "must be between 0 and 65535 inclusive")},
		},
		"invalid WorkPendingGracePeriod": {
			opt: New(func(options *Options) {
				options.WorkPendingGracePeriod.Duration = -40 * time.Second
			}),
			expectedErrs: field.ErrorList{field.Invalid(newPath.Child("WorkPendingGracePeriod"), metav1.Duration{Duration: -40 * time.Second}, "must be greater than 0")},
		},
	}

	for _, testCase := range testCases {
		errs := testCase.opt.Validate()
		if len(testCase.expectedErrs) != len(errs) {
			t.Fatalf("Expected %d errors, got %d errors: %v", len(testCase.expectedErrs), len(errs), errs)
		}
		for i, err := range errs {
			if err.Error() != testCase.expectedErrs[i].Error() {
				t.Fatalf("Expected error: %s, got %s", testCase.expectedErrs[i], err.Error())
			}
		}
	}
}
