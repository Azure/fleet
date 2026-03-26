package utils

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func (r *Runner) CreateStagedUpdateRunStrategy(ctx context.Context) {
	stagedUpdateRunStrategy := &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: commonStagedUpdateRunStrategyName,
		},
		Spec: placementv1beta1.UpdateStrategySpec{
			Stages: []placementv1beta1.StageConfig{
				{
					Name: "staging",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelKey: stagingEnvLabelValue,
						},
					},
					MaxConcurrency: ptr.To(intstr.FromString(r.maxConcurrencyPerStage)),
				},
				{
					Name: "canary",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelKey: canaryEnvLabelValue,
						},
					},
					MaxConcurrency: ptr.To(intstr.FromString(r.maxConcurrencyPerStage)),
				},
				{
					Name: "prod",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							envLabelKey: prodEnvLabelValue,
						},
					},
					MaxConcurrency: ptr.To(intstr.FromString(r.maxConcurrencyPerStage)),
				},
			},
		},
	}
	errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
		return err != nil && !errors.IsAlreadyExists(err)
	}, func() error {
		return r.hubClient.Create(ctx, stagedUpdateRunStrategy)
	})
	if errAfterRetries != nil && !errors.IsAlreadyExists(errAfterRetries) {
		panic(fmt.Sprintf("Failed to create staged update run strategy: %v", errAfterRetries))
	}
}
