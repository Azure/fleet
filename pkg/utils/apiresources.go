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

package utils

import (
	"fmt"
	"strings"

	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metricsV1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	// TODO: add more default resource configs to skip
	corev1PodGVK = schema.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	}

	corev1NodeGVK = schema.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Node",
	}

	serviceImportGK = schema.GroupKind{
		Group: NetworkingGroupName,
		Kind:  "ServiceImport",
	}

	trafficManagerProfileGK = schema.GroupKind{
		Group: NetworkingGroupName,
		Kind:  "TrafficManagerProfile",
	}

	trafficManagerBackendGK = schema.GroupKind{
		Group: NetworkingGroupName,
		Kind:  "TrafficManagerBackend",
	}

	ClusterResourcePlacementGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourcePlacementKind,
	}

	ResourcePlacementGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ResourcePlacementKind,
	}

	ClusterResourceBindingGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourceBindingKind,
	}

	ResourceBindingGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ResourceBindingKind,
	}

	ClusterResourceSnapshotGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourceSnapshotKind,
	}

	ResourceSnapshotGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ResourceSnapshotKind,
	}

	ClusterSchedulingPolicySnapshotGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterSchedulingPolicySnapshotKind,
	}

	SchedulingPolicySnapshotGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.SchedulingPolicySnapshotKind,
	}

	WorkGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.WorkKind,
	}

	ClusterStagedUpdateRunGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterStagedUpdateRunKind,
	}

	ClusterStagedUpdateStrategyGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterStagedUpdateStrategyKind,
	}

	ClusterApprovalRequestGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterApprovalRequestKind,
	}

	ClusterResourcePlacementEvictionGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourcePlacementEvictionKind,
	}

	ClusterResourcePlacementDisruptionBudgetGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourcePlacementDisruptionBudgetKind,
	}

	ClusterResourceOverrideGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourceOverrideKind,
	}

	ClusterResourceOverrideSnapshotGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ClusterResourceOverrideSnapshotKind,
	}

	ResourceOverrideGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ResourceOverrideKind,
	}

	ResourceOverrideSnapshotGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  placementv1beta1.ResourceOverrideSnapshotKind,
	}

	ClusterResourcePlacementStatusGK = schema.GroupKind{
		Group: placementv1beta1.GroupVersion.Group,
		Kind:  "ClusterResourcePlacementStatus",
	}

	// we use `;` to separate the different api groups
	apiGroupSepToken = ";"
)

// ResourceConfig represents the configuration that identifies the API resources that are parsed from the
// user input to either allow or disable propagating them.
type ResourceConfig struct {
	// groups holds a collection of API group, all resources under this group will be considered.
	groups map[string]struct{}
	// groupVersions holds a collection of API GroupVersion, all resource under this GroupVersion will be considered.
	groupVersions map[schema.GroupVersion]struct{}
	// groupKinds holds a collection of API GroupKind, all resource under this GroupKind will be considered.
	groupKinds map[schema.GroupKind]struct{}
	// groupVersionKinds holds a collection of resource that should be considered.
	groupVersionKinds map[schema.GroupVersionKind]struct{}
	// isAllowList indicates whether the ResourceConfig is an allow list or not.
	isAllowList bool
}

// NewResourceConfig creates an empty ResourceConfig with an allow list flag.
// If the resourceConfig is not an allowlist, it creates a default skipped propagating APIs list.
func NewResourceConfig(isAllowList bool) *ResourceConfig {
	r := &ResourceConfig{
		groups:            map[string]struct{}{},
		groupVersions:     map[schema.GroupVersion]struct{}{},
		groupKinds:        map[schema.GroupKind]struct{}{},
		groupVersionKinds: map[schema.GroupVersionKind]struct{}{},
	}
	r.isAllowList = isAllowList
	if r.isAllowList {
		return r
	}
	// TODO (weiweng): remove workv1alpha1 in next PR
	r.AddGroupVersionKind(WorkV1Alpha1GVK)

	// disable cluster group by default
	r.AddGroup(clusterv1beta1.GroupVersion.Group)

	// disable some fleet networking resources
	r.AddGroupKind(serviceImportGK)
	r.AddGroupKind(trafficManagerProfileGK)
	r.AddGroupKind(trafficManagerBackendGK)

	// disable all fleet placement resources except for the envelope type
	r.AddGroupKind(ClusterResourcePlacementGK)
	r.AddGroupKind(ResourcePlacementGK)
	r.AddGroupKind(ClusterResourceBindingGK)
	r.AddGroupKind(ResourceBindingGK)
	r.AddGroupKind(ClusterResourceSnapshotGK)
	r.AddGroupKind(ResourceSnapshotGK)
	r.AddGroupKind(ClusterSchedulingPolicySnapshotGK)
	r.AddGroupKind(SchedulingPolicySnapshotGK)
	r.AddGroupKind(WorkGK)
	r.AddGroupKind(ClusterStagedUpdateRunGK)
	r.AddGroupKind(ClusterStagedUpdateStrategyGK)
	r.AddGroupKind(ClusterApprovalRequestGK)
	r.AddGroupKind(ClusterResourcePlacementEvictionGK)
	r.AddGroupKind(ClusterResourcePlacementDisruptionBudgetGK)
	r.AddGroupKind(ClusterResourceOverrideGK)
	r.AddGroupKind(ClusterResourceOverrideSnapshotGK)
	r.AddGroupKind(ResourceOverrideGK)
	r.AddGroupKind(ResourceOverrideSnapshotGK)
	r.AddGroupKind(ClusterResourcePlacementStatusGK)

	// disable the below built-in resources
	r.AddGroup(eventsv1.GroupName)
	r.AddGroup(coordv1.GroupName)
	r.AddGroup(metricsV1beta1.GroupName)
	r.AddGroupVersionKind(corev1PodGVK)
	r.AddGroupVersionKind(corev1NodeGVK)

	return r
}

// Parse parses the user inputs that provides apis as GVK, GV or Group.
func (r *ResourceConfig) Parse(c string) error {
	// default(empty) input
	if c == "" {
		return nil
	}

	tokens := strings.Split(c, apiGroupSepToken)
	for _, token := range tokens {
		if err := r.parseSingle(token); err != nil {
			return fmt.Errorf("parse --avoid-selecting-apis %w", err)
		}
	}

	return nil
}

// TODO: reduce cyclo
func (r *ResourceConfig) parseSingle(token string) error {
	switch strings.Count(token, "/") {
	// Assume user don't want to skip the 'core'(no group name) group.
	// So, it should be the case "<group>".
	case 0:
		r.groups[token] = struct{}{}
	// it should be the case "<group>/<version>"
	case 1:
		// for core group which don't have the group name, the case should be "v1/<kind>" or "v1/<kind>,<kind>..."
		if strings.HasPrefix(token, "v1") {
			var kinds []string
			for _, k := range strings.Split(token, ",") {
				if strings.Contains(k, "/") { // "v1/<kind>"
					s := strings.Split(k, "/")
					kinds = append(kinds, s[1])
				} else {
					kinds = append(kinds, k)
				}
			}
			for _, k := range kinds {
				gvk := schema.GroupVersionKind{
					Version: "v1",
					Kind:    k,
				}
				r.groupVersionKinds[gvk] = struct{}{}
			}
		} else { // case "<group>/<version>"
			parts := strings.Split(token, "/")
			if len(parts) != 2 {
				return fmt.Errorf("invalid token: %s", token)
			}
			gv := schema.GroupVersion{
				Group:   parts[0],
				Version: parts[1],
			}
			r.groupVersions[gv] = struct{}{}
		}
	// parameter format: "<group>/<version>/<kind>" or "<group>/<version>/<kind>,<kind>..."
	case 2:
		g := ""
		v := ""
		var kinds []string
		for _, k := range strings.Split(token, ",") {
			if strings.Contains(k, "/") {
				s := strings.Split(k, "/")
				g = s[0]
				v = s[1]
				kinds = append(kinds, s[2])
			} else {
				kinds = append(kinds, k)
			}
		}
		for _, k := range kinds {
			gvk := schema.GroupVersionKind{
				Group:   g,
				Version: v,
				Kind:    k,
			}
			r.groupVersionKinds[gvk] = struct{}{}
		}
	default:
		return fmt.Errorf("invalid parameter: %s", token)
	}

	return nil
}

// IsResourceDisabled returns whether a given GroupVersionKind is disabled.
// A gvk is disabled if its group or group version is disabled.
func (r *ResourceConfig) IsResourceDisabled(gvk schema.GroupVersionKind) bool {
	isConfigured := r.isResourceConfigured(gvk)
	if r.isAllowList {
		return !isConfigured
	}
	return isConfigured
}

// isResourceConfigured returns whether a given GroupVersionKind is found in the ResourceConfig.
// A gvk is configured if its group or group version is configured.
func (r *ResourceConfig) isResourceConfigured(gvk schema.GroupVersionKind) bool {
	if _, ok := r.groups[gvk.Group]; ok {
		return true
	}

	if _, ok := r.groupVersions[gvk.GroupVersion()]; ok {
		return true
	}

	if _, ok := r.groupKinds[gvk.GroupKind()]; ok {
		return true
	}

	if _, ok := r.groupVersionKinds[gvk]; ok {
		return true
	}

	return false
}

// AddGroup stores a group in the resource config.
func (r *ResourceConfig) AddGroup(g string) {
	r.groups[g] = struct{}{}
}

// AddGroupVersion stores a group version in the resource config.
func (r *ResourceConfig) AddGroupVersion(gv schema.GroupVersion) {
	r.groupVersions[gv] = struct{}{}
}

// AddGroupKind stores a group kind in the resource config.
func (r *ResourceConfig) AddGroupKind(gk schema.GroupKind) {
	r.groupKinds[gk] = struct{}{}
}

// AddGroupVersionKind stores a GroupVersionKind in the resource config.
func (r *ResourceConfig) AddGroupVersionKind(gvk schema.GroupVersionKind) {
	r.groupVersionKinds[gvk] = struct{}{}
}
