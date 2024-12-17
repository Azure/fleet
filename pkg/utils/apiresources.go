/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
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

	serviceImportGVK = schema.GroupVersionKind{
		Group:   NetworkingGroupName,
		Version: "v1alpha1",
		Kind:    "ServiceImport",
	}

	trafficManagerProfileGVK = schema.GroupVersionKind{
		Group:   NetworkingGroupName,
		Version: "v1alpha1",
		Kind:    "TrafficManagerProfile",
	}

	trafficManagerBackendGVK = schema.GroupVersionKind{
		Group:   NetworkingGroupName,
		Version: "v1alpha1",
		Kind:    "TrafficManagerBackend",
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
		groupVersionKinds: map[schema.GroupVersionKind]struct{}{},
	}
	r.isAllowList = isAllowList
	if r.isAllowList {
		return r
	}
	// disable fleet related resource by default
	r.AddGroup(fleetv1alpha1.GroupVersion.Group)
	r.AddGroup(placementv1beta1.GroupVersion.Group)
	r.AddGroup(clusterv1beta1.GroupVersion.Group)
	r.AddGroupVersionKind(WorkV1Alpha1GVK)

	// disable the below built-in resources
	r.AddGroup(eventsv1.GroupName)
	r.AddGroup(coordv1.GroupName)
	r.AddGroup(metricsV1beta1.GroupName)
	r.AddGroupVersionKind(corev1PodGVK)
	r.AddGroupVersionKind(corev1NodeGVK)
	// disable networking resources
	r.AddGroupVersionKind(serviceImportGVK)
	r.AddGroupVersionKind(trafficManagerProfileGVK)
	r.AddGroupVersionKind(trafficManagerBackendGVK)
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

// AddGroupVersionKind stores a GroupVersionKind in the resource config.
func (r *ResourceConfig) AddGroupVersionKind(gvk schema.GroupVersionKind) {
	r.groupVersionKinds[gvk] = struct{}{}
}
