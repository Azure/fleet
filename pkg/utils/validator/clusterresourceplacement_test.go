/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"testing"

	"go.goms.io/fleet/apis/placement/v1beta1"
)

func Test_validateRolloutStrategy(t *testing.T) {
	tests := map[string]struct {
		rolloutStrategy v1beta1.RolloutStrategy
		wantErr         bool
	}{
		// TODO: Add test cases.
		"invalid RolloutStrategyType should fail": {
			rolloutStrategy: v1beta1.RolloutStrategy{
				Type: "random type",
			},
			wantErr: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateRolloutStrategy(tt.rolloutStrategy); (err != nil) != tt.wantErr {
				t.Errorf("validateRolloutStrategy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
