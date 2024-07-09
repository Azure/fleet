package mutator

import (
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// MutateClusterResourcePlacement mutates a ClusterResourcePlacement object.
func MutateClusterResourcePlacement(crp *placementv1beta1.ClusterResourcePlacement) error {
	rolloutStrategy := crp.Spec.Strategy
	if rolloutStrategy.RollingUpdate != nil {
		if rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds != nil && *rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds < 0 {
			rolloutStrategy.RollingUpdate.UnavailablePeriodSeconds = ptr.To(0)
		}
		if rolloutStrategy.RollingUpdate.MaxUnavailable != nil {
			value, err := intstr.GetScaledValueFromIntOrPercent(rolloutStrategy.RollingUpdate.MaxUnavailable, 10, true)
			if err != nil {
				return err
			}
			if value < 1 {
				rolloutStrategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 1,
				}
			}
		}
		if rolloutStrategy.RollingUpdate.MaxSurge != nil {
			value, err := intstr.GetScaledValueFromIntOrPercent(rolloutStrategy.RollingUpdate.MaxSurge, 10, true)
			if err != nil {
				return err
			}
			if value < 1 {
				rolloutStrategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 0,
				}
			}
		}
	}
	return nil
}
