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
		"ClusterResourcePlacement with nil Spec": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						PlacementType: fleetv1beta1.PickAllPlacementType,
					},
					Strategy: fleetv1beta1.RolloutStrategy{
						Type: fleetv1beta1.RollingUpdateRolloutStrategyType,
						RollingUpdate: &fleetv1beta1.RollingUpdateConfig{
							MaxUnavailable:           ptr.To(intstr.FromString(DefaultMaxUnavailableValue)),
							MaxSurge:                 ptr.To(intstr.FromString(DefaultMaxSurgeValue)),
							UnavailablePeriodSeconds: ptr.To(DefaultUnavailablePeriodSeconds),
						},
						ApplyStrategy: &fleetv1beta1.ApplyStrategy{
							Type: fleetv1beta1.ApplyStrategyTypeClientSideApply,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(DefaultRevisionHistoryLimitValue)),
				},
			},
		},
		"ClusterResourcePlacement with nil TopologySpreadConstraints & Tolerations fields": {
			obj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								TopologyKey: "kubernetes.io/hostname",
							},
						},
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
							Type: fleetv1beta1.ApplyStrategyTypeClientSideApply,
						},
					},
					RevisionHistoryLimit: ptr.To(int32(10)),
				},
			},
			wantObj: &fleetv1beta1.ClusterResourcePlacement{
				Spec: fleetv1beta1.ClusterResourcePlacementSpec{
					Policy: &fleetv1beta1.PlacementPolicy{
						TopologySpreadConstraints: []fleetv1beta1.TopologySpreadConstraint{
							{
								TopologyKey:       "kubernetes.io/hostname",
								MaxSkew:           ptr.To(int32(DefaultMaxSkewValue)),
								WhenUnsatisfiable: fleetv1beta1.DoNotSchedule,
							},
						},
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
							Type: fleetv1beta1.ApplyStrategyTypeClientSideApply,
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
			if diff := cmp.Diff(tt.wantObj, tt.obj); diff != "" {
				t.Errorf("SetDefaultsClusterResourcePlacement() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
