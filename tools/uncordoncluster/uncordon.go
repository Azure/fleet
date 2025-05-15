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

package main

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	toolsutils "go.goms.io/fleet/tools/utils"
)

type helper struct {
	hubClient   client.Client
	clusterName string
}

// Uncordon removes the taint from the member cluster.
func (h *helper) Uncordon(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mc clusterv1beta1.MemberCluster
		if err := h.hubClient.Get(ctx, types.NamespacedName{Name: h.clusterName}, &mc); err != nil {
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

		return h.hubClient.Update(ctx, &mc)
	})
}
