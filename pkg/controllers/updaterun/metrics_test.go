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

package updaterun

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	hubmetrics "github.com/kubefleet-dev/kubefleet/pkg/metrics/hub"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

func TestDetermineFailureType(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want hubmetrics.UpdateRunFailureType
	}{
		{
			name: "update run no failure",
			err:  nil,
			want: hubmetrics.UpdateRunFailureTypeNone,
		},
		{
			name: "update run failed with user error",
			err:  fmt.Errorf("cannot continue the updateRun: failed to validate the updateRun: %w", controller.ErrUserError),
			want: hubmetrics.UpdateRunFailureTypeUserError,
		},
		{
			name: "update run failed with internal error",
			err:  errors.New("cannot continue the updateRun"),
			want: hubmetrics.UpdateRunFailureTypeInternalError,
		},
		{
			name: "update run is stuck - internal error",
			err:  errors.New("updateRun is stuck waiting for 1 cluster(s) in stage stage1 to finish updating, please check placement status for potential errors"),
			want: hubmetrics.UpdateRunFailureTypeInternalError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := determineFailureType(tc.err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("determineFailureType() = %v, want %v, diff (-want +got):\n%s", got, tc.want, diff)
			}
		})
	}
}
