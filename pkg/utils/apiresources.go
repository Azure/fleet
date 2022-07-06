/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

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
)

// DisabledResourceConfig represents the configuration that identifies the API resources should not be selected.
type DisabledResourceConfig struct {
	// Groups holds a collection of API group, all resources under this group will be avoided.
	Groups map[string]struct{}
	// GroupVersions holds a collection of API GroupVersion, all resource under this GroupVersion will be avoided.
	GroupVersions map[schema.GroupVersion]struct{}
	// GroupVersionKinds holds a collection of resource that should be avoided.
	GroupVersionKinds map[schema.GroupVersionKind]struct{}
}

// NewDisabledResourceConfig to create DisabledResourceConfig
func NewDisabledResourceConfig() *DisabledResourceConfig {
	r := &DisabledResourceConfig{
		Groups:            map[string]struct{}{},
		GroupVersions:     map[schema.GroupVersion]struct{}{},
		GroupVersionKinds: map[schema.GroupVersionKind]struct{}{},
	}
	// disable fleet group by default
	r.DisableGroup(fleetv1alpha1.GroupVersion.Group)
	// disable event by default
	r.DisableGroup(eventsv1.GroupName)
	r.DisableGroupVersionKind(corev1PodGVK)
	r.DisableGroupVersionKind(corev1NodeGVK)
	return r
}

// Parse parses the --avoid-selecting-apis input.
func (r *DisabledResourceConfig) Parse(c string) error {
	// default(empty) input
	if c == "" {
		return nil
	}

	tokens := strings.Split(c, ";")
	for _, token := range tokens {
		if err := r.parseSingle(token); err != nil {
			return fmt.Errorf("parse --avoid-selecting-apis %w", err)
		}
	}

	return nil
}

func (r *DisabledResourceConfig) parseSingle(token string) error {
	switch strings.Count(token, "/") {
	// Assume user don't want to skip the 'core'(no group name) group.
	// So, it should be the case "<group>".
	case 0:
		r.Groups[token] = struct{}{}
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
				r.GroupVersionKinds[gvk] = struct{}{}
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
			r.GroupVersions[gv] = struct{}{}
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
			r.GroupVersionKinds[gvk] = struct{}{}
		}
	default:
		return fmt.Errorf("invalid parameter: %s", token)
	}

	return nil
}

// GroupVersionDisabled returns whether GroupVersion is disabled.
func (r *DisabledResourceConfig) GroupVersionDisabled(gv schema.GroupVersion) bool {
	if _, ok := r.GroupVersions[gv]; ok {
		return true
	}
	return false
}

// GroupVersionKindDisabled returns whether GroupVersionKind is disabled.
func (r *DisabledResourceConfig) GroupVersionKindDisabled(gvk schema.GroupVersionKind) bool {
	if _, ok := r.GroupVersionKinds[gvk]; ok {
		return true
	}
	return false
}

// GroupDisabled returns whether Group is disabled.
func (r *DisabledResourceConfig) GroupDisabled(g string) bool {
	if _, ok := r.Groups[g]; ok {
		return true
	}
	return false
}

// DisableGroup to disable group.
func (r *DisabledResourceConfig) DisableGroup(g string) {
	r.Groups[g] = struct{}{}
}

// DisableGroupVersionKind to disable GroupVersionKind.
func (r *DisabledResourceConfig) DisableGroupVersionKind(gvk schema.GroupVersionKind) {
	r.GroupVersionKinds[gvk] = struct{}{}
}
