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

	// we use `;` to separate the different api groups
	apiGroupSepToken = ";"
)

// DisabledResourceConfig represents the configuration that identifies the API resources should not be selected.
type DisabledResourceConfig struct {
	// groups holds a collection of API group, all resources under this group will be avoided.
	groups map[string]struct{}
	// groupVersions holds a collection of API GroupVersion, all resource under this GroupVersion will be avoided.
	groupVersions map[schema.GroupVersion]struct{}
	// groupVersionKinds holds a collection of resource that should be avoided.
	groupVersionKinds map[schema.GroupVersionKind]struct{}
}

// NewDisabledResourceConfig to create DisabledResourceConfig
func NewDisabledResourceConfig() *DisabledResourceConfig {
	r := &DisabledResourceConfig{
		groups:            map[string]struct{}{},
		groupVersions:     map[schema.GroupVersion]struct{}{},
		groupVersionKinds: map[schema.GroupVersionKind]struct{}{},
	}
	// disable fleet related resource by default
	r.DisableGroup(fleetv1alpha1.GroupVersion.Group)
	r.DisableGroup(placementv1beta1.GroupVersion.Group)
	r.DisableGroup(clusterv1beta1.GroupVersion.Group)
	r.DisableGroupVersionKind(WorkGVK)

	// disable the below built-in resources
	r.DisableGroup(eventsv1.GroupName)
	r.DisableGroup(coordv1.GroupName)
	r.DisableGroup(metricsV1beta1.GroupName)
	r.DisableGroupVersionKind(corev1PodGVK)
	r.DisableGroupVersionKind(corev1NodeGVK)
	r.DisableGroupVersionKind(serviceImportGVK)
	return r
}

// Parse parses the --avoid-selecting-apis input.
func (r *DisabledResourceConfig) Parse(c string) error {
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
func (r *DisabledResourceConfig) parseSingle(token string) error {
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
// a gkv is disabled if its group or group version is disabled
func (r *DisabledResourceConfig) IsResourceDisabled(gvk schema.GroupVersionKind) bool {
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

// DisableGroup to disable group.
func (r *DisabledResourceConfig) DisableGroup(g string) {
	r.groups[g] = struct{}{}
}

// DisableGroupVersion to disable group version.
func (r *DisabledResourceConfig) DisableGroupVersion(gv schema.GroupVersion) {
	r.groupVersions[gv] = struct{}{}
}

// DisableGroupVersionKind to disable GroupVersionKind.
func (r *DisabledResourceConfig) DisableGroupVersionKind(gvk schema.GroupVersionKind) {
	r.groupVersionKinds[gvk] = struct{}{}
}
