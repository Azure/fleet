/*
Copyright 2026 The KubeFleet Authors.

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

package resourceeligibility

import (
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

var (
	// GVKsDeniedByDefault lists the GVKs that KubeFleet blocks from placement regardless of the API
	// version in use; it covers the KubeFleet APIs themselves plus a few Kubernetes system APIs whose
	// objects are node-local or control-plane managed.
	//
	// An empty version or kind acts as a wildcard; see gvkMatcherTrie.Register.
	GVKsDeniedByDefault = []schema.GroupVersionKind{
		// Deny all resources in the cluster.kubernetes-fleet.io API group.
		{
			Group: clusterv1beta1.GroupVersion.Group,
		},
		// Deny node resources.
		{
			Group: corev1.GroupName,
			Kind:  "Node",
		},
		// Deny pod resources.
		{
			Group: corev1.GroupName,
			Kind:  utils.PodKind,
		},
		// Deny all resources in the events.k8s.io API group.
		{
			Group: eventsv1.GroupName,
		},
		// Deny all resources in the coordination.k8s.io API group.
		{
			Group: coordv1.GroupName,
		},
		// Deny all resources in the metrics.k8s.io API group.
		{
			Group: metricsv1beta1.GroupName,
		},
		// Deny the fleet networking resources that are managed by the fleet networking controllers.
		{
			Group: utils.NetworkingGroupName,
			Kind:  "ServiceImport",
		},
		{
			Group: utils.NetworkingGroupName,
			Kind:  "TrafficManagerProfile",
		},
		{
			Group: utils.NetworkingGroupName,
			Kind:  "TrafficManagerBackend",
		},
		// Deny all non-envelope resources in the placement.kubernetes-fleet.io API group.
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourcePlacementKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ResourcePlacementKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourceBindingKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ResourceBindingKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourceSnapshotKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ResourceSnapshotKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterSchedulingPolicySnapshotKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.SchedulingPolicySnapshotKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.WorkKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterStagedUpdateRunKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterStagedUpdateStrategyKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterApprovalRequestKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.StagedUpdateRunKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.StagedUpdateStrategyKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ApprovalRequestKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourcePlacementEvictionKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourcePlacementDisruptionBudgetKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourceOverrideKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourceOverrideSnapshotKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ResourceOverrideKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ResourceOverrideSnapshotKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourcePlacementStatusKind,
		},
		// Deny all resources in the placement.kubefleet.dev API group.
		//
		// TO-DO (chenyu1): deny resources by kinds when envelopes are added to the API group.
		{
			Group: kfplacementv1alpha1.GroupVersion.Group,
		},
	}

	// GVKsDeniedForV1APIs lists the GVKs that are blocked from placement only when the new placement APIs (placement
	// policies) are in use.
	GVKsDeniedForV1APIs = []schema.GroupVersionKind{
		// Deny endpoints resources; the API is deprecated and the objects are typically managed by the control plane.
		//
		// Note (chenyu1): v0 placement APIs handle this API type differently.
		{
			Group: corev1.GroupName,
			Kind:  "Endpoints",
		},
		// Deny envelope APIs in the placement.kubernetes-fleet.io API group.
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ClusterResourceEnvelopeKind,
		},
		{
			Group: placementv1beta1.GroupVersion.Group,
			Kind:  placementv1beta1.ResourceEnvelopeKind,
		},
	}
)
