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
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// FetchUpdateStrategyFromNamespacedName resolves a NamespacedName to a concrete staged update strategy object that implements UpdateStrategyObj.
// If Namespace is empty, it fetches a ClusterStagedUpdateStrategy (cluster-scoped).
// If Namespace is not empty, it fetches a StagedUpdateStrategy (namespace-scoped).
func FetchUpdateStrategyFromNamespacedName(ctx context.Context, c client.Reader, strategyKey types.NamespacedName) (placementv1beta1.UpdateStrategyObj, error) {
	var strategy placementv1beta1.UpdateStrategyObj
	// If Namespace is empty, it's a cluster-scoped strategy (ClusterStagedUpdateStrategy)
	if strategyKey.Namespace == "" {
		strategy = &placementv1beta1.ClusterStagedUpdateStrategy{}
	} else {
		// Otherwise, it's a namespaced strategy (StagedUpdateStrategy)
		strategy = &placementv1beta1.StagedUpdateStrategy{}
	}
	err := c.Get(ctx, strategyKey, strategy)
	if err != nil {
		return nil, err
	}
	return strategy, nil
}
