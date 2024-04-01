/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package defaulter

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestSetDefaultsWork(t *testing.T) {
	tests := []struct {
		name string
		work placementv1beta1.Work
		want placementv1beta1.Work
	}{
		{
			name: "nil applyStrategy",
			work: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{},
			},
			want: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{Type: placementv1beta1.ApplyStrategyTypeClientSideApply},
				},
			},
		},
		{
			name: "empty strategy type",
			work: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{},
				},
			},
			want: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{Type: placementv1beta1.ApplyStrategyTypeClientSideApply},
				},
			},
		},
		{
			name: "nil server side apply config",
			work: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{Type: placementv1beta1.ApplyStrategyTypeServerSideApply},
				},
			},
			want: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
						ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: false},
					},
				},
			},
		},
		{
			name: "all fields are set",
			work: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
						ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
					},
				},
			},
			want: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
						ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetDefaultsWork(&tc.work)
			if diff := cmp.Diff(tc.want, tc.work); diff != "" {
				t.Errorf("SetDefaultsWork() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
