/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// a callback function to modify options
type ModifyOptions func(option *Options)

// newTestOptions creates an Options with default parameters.
func newTestOptions(modifyOptions ModifyOptions) Options {
	option := Options{
		SkippedPropagatingAPIs:      "fleet.azure.com;multicluster.x-k8s.io",
		WorkPendingGracePeriod:      metav1.Duration{Duration: 10 * time.Second},
		ClusterUnhealthyThreshold:   metav1.Duration{Duration: 1 * time.Second},
		WebhookClientConnectionType: "url",
		EnablePlacementV1Alpha1APIs: true,
	}

	if modifyOptions != nil {
		modifyOptions(&option)
	}
	return option
}

func TestValidateControllerManagerConfiguration(t *testing.T) {
	newPath := field.NewPath("Options")
	testCases := map[string]struct {
		opt  Options
		want field.ErrorList
	}{
		"valid Options": {
			opt:  newTestOptions(nil),
			want: field.ErrorList{},
		},
		"invalid SkippedPropagatingAPIs": {
			opt: newTestOptions(func(options *Options) {
				options.SkippedPropagatingAPIs = "a/b/c/d?"
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("SkippedPropagatingAPIs"), "a/b/c/d?", "Invalid API string")},
		},
		"invalid ClusterUnhealthyThreshold": {
			opt: newTestOptions(func(options *Options) {
				options.ClusterUnhealthyThreshold.Duration = -40 * time.Second
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("ClusterUnhealthyThreshold"), metav1.Duration{Duration: -40 * time.Second}, "Must be greater than 0")},
		},
		"invalid WorkPendingGracePeriod": {
			opt: newTestOptions(func(options *Options) {
				options.WorkPendingGracePeriod.Duration = -40 * time.Second
			}),
			want: field.ErrorList{field.Invalid(newPath.Child("WorkPendingGracePeriod"), metav1.Duration{Duration: -40 * time.Second}, "Must be greater than 0")},
		},
		"invalid EnablePlacementV1Alpha1APIs": {
			opt: newTestOptions(func(option *Options) {
				option.EnablePlacementV1Alpha1APIs = false
			}),
			want: field.ErrorList{field.Required(newPath.Child("EnablePlacementV1Alpha1APIs"), "Either EnablePlacementV1Alpha1APIs or EnablePlacementV1Beta1APIs is required")},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := tc.opt.Validate()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Validate() errs mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
