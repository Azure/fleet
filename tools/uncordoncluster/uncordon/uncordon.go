/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package uncordon

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	toolsutils "go.goms.io/fleet/tools/utils"
)

type Helper struct {
	HubClient   client.Client
	ClusterName string
}

// Uncordon removes the taint from the member cluster.
func (h *Helper) Uncordon(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := h.HubClient.Get(ctx, types.NamespacedName{Name: h.ClusterName}, &mc); err != nil {
			return err
		}

		if len(mc.Spec.Taints) == 0 {
			return nil
		}

		// remove cordon taint from member cluster.
		var newTaints []clusterv1beta1.Taint
		for i := range mc.Spec.Taints {
			taint := mc.Spec.Taints[i]
			if taint == toolsutils.CordonTaint {
				continue
			}
			newTaints = append(newTaints, taint)
		}
		mc.Spec.Taints = newTaints

		return h.HubClient.Update(ctx, &mc)
	})
}
