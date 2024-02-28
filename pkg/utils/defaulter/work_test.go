/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package defaulter

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestSetDefaultsWork(t *testing.T) {
	tests := []struct {
		name string
		work fleetv1beta1.Work
		want fleetv1beta1.Work
	}{
		{
			name: "nil applyStrategy",
			work: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{},
			},
			want: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeFailIfExists},
				},
			},
		},
		{
			name: "empty strategy type",
			work: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{},
				},
			},
			want: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeFailIfExists},
				},
			},
		},
		{
			name: "nil server side apply config",
			work: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeServerSideApply},
				},
			},
			want: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type:                  fleetv1beta1.ApplyStrategyTypeServerSideApply,
						ServerSideApplyConfig: &fleetv1beta1.ServerSideApplyConfig{ForceConflicts: false},
					},
				},
			},
		},
		{
			name: "all fields are set",
			work: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type:                  fleetv1beta1.ApplyStrategyTypeServerSideApply,
						ServerSideApplyConfig: &fleetv1beta1.ServerSideApplyConfig{ForceConflicts: true},
					},
				},
			},
			want: fleetv1beta1.Work{
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						Type:                  fleetv1beta1.ApplyStrategyTypeServerSideApply,
						ServerSideApplyConfig: &fleetv1beta1.ServerSideApplyConfig{ForceConflicts: true},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetDefaultsWork(&tc.work)
			if diff := cmp.Diff(tc.work, tc.work); diff != "" {
				t.Errorf("SetDefaultsWork() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
