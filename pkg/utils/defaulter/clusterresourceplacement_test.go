package defaulter

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestSetDefaultsClusterResourcePlacement(t *testing.T) {
	tests := map[string]struct {
		obj     *fleetv1beta1.ClusterResourcePlacement
		wantObj *fleetv1beta1.ClusterResourcePlacement
	}{
		"ClusterResourcePlacement with nil placement Policy": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(5)),
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(5)),
				},
			},
		},
		"ClusterResourcePlacement with nil Strategy": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					RevisionHistoryLimit: ptr.To(int32(5)),
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString(fleetv1beta1.DefaultMaxUnavailableValue)),
							MaxSurge:                 ptr.To(intstr.FromString(fleetv1beta1.DefaultMaxSurgeValue)),
							UnavailablePeriodSeconds: ptr.To(fleetv1beta1.DefaultUnavailablePeriodSeconds),
						},
					},
					RevisionHistoryLimit: ptr.To(int32(5)),
				},
			},
		},
		"ClusterResourcePlacement with nil RevisionLimit": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
		},
		"ClusterResourcePlacement with nil ApplyStrategy Type": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
		},
		"ClusterResourcePlacement with ApplyStrategyTypeServerSideApply": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeServerSideApply,
						},
					},
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type:                  fleetv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &fleetv1beta1.ServerSideApplyConfig{},
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
		},
		"ClusterResourcePlacement with nil TopologySpreadConstraints fields": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								TopologyKey:       "kubernetes.io/hostname",
								MaxSkew:           ptr.To(int32(fleetv1beta1.DefaultMaxSkewValue)),
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								TopologyKey:       "kubernetes.io/hostname",
								MaxSkew:           ptr.To(int32(fleetv1beta1.DefaultMaxSkewValue)),
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
		},
		"ClusterResourcePlacement with nil Tolerations fields": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
						Tolerations: []fleetv1beta1.Toleration{
							{
								Key:   "key",
								Value: "value",
							},
						},
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickNPlacementType,
						Tolerations: []fleetv1beta1.Toleration{
							{
								Key:      "key",
								Value:    "value",
								Operator: corev1.TolerationOpEqual,
							},
						},
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString("%15")),
							MaxSurge:                 ptr.To(intstr.FromString("%15")),
							UnavailablePeriodSeconds: ptr.To(15),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeFailIfExists,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			SetDefaultsClusterResourcePlacement(tt.obj)
			if diff := cmp.Diff(tt.obj, tt.wantObj); diff != "" {
				t.Errorf("SetDefaultsClusterResourcePlacement() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
