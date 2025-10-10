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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// FetchUpdateRunFromRequest resolves a controller runtime request to a concrete update run object that implements UpdateRunObj.
func FetchUpdateRunFromRequest(ctx context.Context, c client.Reader, req ctrl.Request) (placementv1beta1.UpdateRunObj, error) {
	var updateRun placementv1beta1.UpdateRunObj
	if req.NamespacedName.Namespace != "" {
		// This is a namespaced StagedUpdateRun
		updateRun = &placementv1beta1.StagedUpdateRun{}
	} else {
		// This is a cluster-scoped ClusterStagedUpdateRun
		updateRun = &placementv1beta1.ClusterStagedUpdateRun{}
	}

	if err := c.Get(ctx, types.NamespacedName{Namespace: req.NamespacedName.Namespace, Name: req.NamespacedName.Name}, updateRun); err != nil {
		return nil, err
	}
	return updateRun, nil
}
