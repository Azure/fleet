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

type UncordonCluster struct {
	HubClient   client.Client
	ClusterName string
}

// Uncordon removes the taint from the member cluster.
func (u *UncordonCluster) Uncordon(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := u.HubClient.Get(ctx, types.NamespacedName{Name: u.ClusterName}, &mc); err != nil {
			return err
		}

		if len(mc.Spec.Taints) == 0 {
			return nil
		}

		// remove cordon taint from member cluster.
		cordonTaintIndex := -1
		for i := range mc.Spec.Taints {
			taint := mc.Spec.Taints[i]
			if taint == toolsutils.CordonTaint {
				cordonTaintIndex = i
				break
			}
		}

		if cordonTaintIndex >= 0 {
			mc.Spec.Taints = append(mc.Spec.Taints[:cordonTaintIndex], mc.Spec.Taints[cordonTaintIndex+1:]...)
		} else {
			return nil
		}

		return u.HubClient.Update(ctx, &mc)
	})
}
