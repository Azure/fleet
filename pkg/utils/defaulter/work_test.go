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
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					},
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
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
						ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					},
				},
			},
		},
		{
			name: "nil server side apply config",
			work: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
					},
				},
			},
			want: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
						ComparisonOption:      placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:           placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:        placementv1beta1.WhenToTakeOverTypeAlways,
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
						ComparisonOption:      placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:           placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:        placementv1beta1.WhenToTakeOverTypeAlways,
						ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{ForceConflicts: true},
					},
				},
			},
			want: placementv1beta1.Work{
				Spec: placementv1beta1.WorkSpec{
					ApplyStrategy: &placementv1beta1.ApplyStrategy{
						Type:                  placementv1beta1.ApplyStrategyTypeServerSideApply,
						ComparisonOption:      placementv1beta1.ComparisonOptionTypePartialComparison,
						WhenToApply:           placementv1beta1.WhenToApplyTypeAlways,
						WhenToTakeOver:        placementv1beta1.WhenToTakeOverTypeAlways,
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
