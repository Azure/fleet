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
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"

	"go.goms.io/fleet/test/utils/resource"
)

func TestResourceConfigGVKParse(t *testing.T) {
	tests := []struct {
		input    string
		disabled []schema.GroupVersionKind
		enabled  []schema.GroupVersionKind
	}{
		{
			input: "v1/Node,Pod;networking.k8s.io/v1beta1/Ingress,IngressClass",
			disabled: []schema.GroupVersionKind{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Node",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1beta1",
					Kind:    "Ingress",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1beta1",
					Kind:    "IngressClass",
				},
			},
			enabled: []schema.GroupVersionKind{
				{
					Group:   "",
					Version: "v1",
					Kind:    "ResourceQuota",
				},
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "ControllerRevision",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1",
					Kind:    "Ingress",
				},
				{
					Group:   "certificates.k8s.io",
					Version: "v1beta1",
					Kind:    "CertificateSigningRequest",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1beta1",
					Kind:    "",
				},
			},
		},
	}
	for _, test := range tests {
		r := newTestResourceConfig(t, false, test.input)
		checkIfResourcesAreDisabledInConfig(t, r, test.disabled)
		checkIfResourcesAreEnabledInConfig(t, r, test.enabled)
	}
}

func TestResourceConfigGVParse(t *testing.T) {
	tests := []struct {
		input    string
		disabled []schema.GroupVersionKind
		enabled  []schema.GroupVersionKind
	}{
		{
			input: "networking.k8s.io/v1;test/v1beta1",
			disabled: []schema.GroupVersionKind{
				{
					Group:   "networking.k8s.io",
					Version: "v1",
					Kind:    "Ingress",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1",
					Kind:    "EgressClass",
				},
				{
					Group:   "test",
					Version: "v1beta1",
					Kind:    "Lease",
				},
				{
					Group:   "test",
					Version: "v1beta1",
					Kind:    "HealthState",
				},
			},
			enabled: []schema.GroupVersionKind{
				{
					Group:   "networking.k8s.io",
					Version: "v1beta1",
					Kind:    "Ingress",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1beta1",
					Kind:    "IngressClass",
				},
				{
					Group:   "test",
					Version: "v1",
					Kind:    "Service",
				},
			},
		},
	}
	for _, test := range tests {
		r := newTestResourceConfig(t, false, test.input)
		checkIfResourcesAreDisabledInConfig(t, r, test.disabled)
		checkIfResourcesAreEnabledInConfig(t, r, test.enabled)
	}
}

func TestResourceConfigGroupParse(t *testing.T) {
	tests := []struct {
		input    string
		disabled []schema.GroupVersionKind
		enabled  []schema.GroupVersionKind
	}{
		{
			input: "networking.k8s.io;apps;secrets-store.csi.x-k8s.io",
			disabled: []schema.GroupVersionKind{
				{
					Group:   "networking.k8s.io",
					Version: "v1",
					Kind:    "Ingress",
				},
				{
					Group:   "networking.k8s.io",
					Version: "v1beta1",
					Kind:    "EgressClass",
				},
				{
					Group:   "apps",
					Version: "v1beta1",
					Kind:    "Lease",
				},
				{
					Group:   "secrets-store.csi.x-k8s.io",
					Version: "v1beta1",
					Kind:    "HealthState",
				},
			},
			enabled: []schema.GroupVersionKind{
				{
					Group:   "",
					Version: "v1beta1",
					Kind:    "Ingress",
				},
				{
					Group:   "apiregistration.k8s.io",
					Version: "v1beta1",
					Kind:    "IngressClass",
				},
				{
					Group:   "authentication.k8s.i",
					Version: "v1",
					Kind:    "Service",
				},
			},
		},
	}
	for _, test := range tests {
		r := newTestResourceConfig(t, false, test.input)
		checkIfResourcesAreDisabledInConfig(t, r, test.disabled)
		checkIfResourcesAreEnabledInConfig(t, r, test.enabled)
	}
}

func TestResourceConfigMixedParse(t *testing.T) {
	input := "v1/Node,Pod;networking.k8s.io;apps/v1;authorization.k8s.io/v1/SelfSubjectRulesReview"

	// these are the resources that are in the scope of the user specified input
	resourcesInUserInput := []schema.GroupVersionKind{
		{
			Group:   "networking.k8s.io",
			Version: "v1beta1",
			Kind:    "Ingress",
		},
		{
			Group:   "networking.k8s.io",
			Version: "v1",
			Kind:    "IngressClass",
		},
		{
			Group:   "",
			Version: "v1",
			Kind:    "Node",
		},
		{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		},
		{
			Group:   "authorization.k8s.io",
			Version: "v1",
			Kind:    "SelfSubjectRulesReview",
		},
		{
			Group:   "apps",
			Version: "v1",
			Kind:    "HealthState",
		},
	}

	// these are the resources that are not in the scope of the user specified input
	resourcesNotInUserInput := []schema.GroupVersionKind{
		{
			Group:   "",
			Version: "v1",
			Kind:    "Ingress",
		},
		{
			Group:   "apps",
			Version: "v1beta1",
			Kind:    "IngressClass",
		},
		{
			Group:   "authorization.k8s.io",
			Version: "v1",
			Kind:    "LocalSubjectAccessReview",
		},
	}

	tests := map[string]struct {
		isAllowList bool
		disabled    []schema.GroupVersionKind
		enabled     []schema.GroupVersionKind
	}{
		"disabled list": {
			isAllowList: false,
			disabled:    resourcesInUserInput,
			enabled:     resourcesNotInUserInput,
		},
		"enabled list": {
			isAllowList: true,
			disabled:    resourcesNotInUserInput,
			enabled:     resourcesInUserInput,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			r := newTestResourceConfig(t, test.isAllowList, input)
			checkIfResourcesAreDisabledInConfig(t, r, test.disabled)
			checkIfResourcesAreEnabledInConfig(t, r, test.enabled)
		})
	}
}

func TestDefaultResourceConfigGroupVersionKindParse(t *testing.T) {
	resourcesInDefaultDisabledList := []schema.GroupVersionKind{
		corev1PodGVK, corev1NodeGVK,
		{
			Group:   "events.k8s.io",
			Version: "v1beta1",
			Kind:    "Event",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1alpha1",
			Kind:    "ServiceImport",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1beta1",
			Kind:    "ServiceImport",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1alpha1",
			Kind:    "TrafficManagerProfile",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1alpha1",
			Kind:    "TrafficManagerBackend",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1beta1",
			Kind:    "TrafficManagerProfile",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1beta1",
			Kind:    "TrafficManagerBackend",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1beta2",
			Kind:    "TrafficManagerBackend",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1",
			Kind:    "TrafficManagerProfile",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1",
			Kind:    "TrafficManagerBackend",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v2",
			Kind:    "TrafficManagerProfile",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v2",
			Kind:    "TrafficManagerBackend",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourcePlacement",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourcePlacement",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ResourcePlacement",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ResourcePlacement",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourceBinding",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourceBinding",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ResourceBinding",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ResourceBinding",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourceSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourceSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ResourceSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ResourceSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterSchedulingPolicySnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterSchedulingPolicySnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "SchedulingPolicySnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "SchedulingPolicySnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "Work",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "Work",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterStagedUpdateRun",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterStagedUpdateRun",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterStagedUpdateStrategy",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterStagedUpdateStrategy",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterApprovalRequest",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterApprovalRequest",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourcePlacementEviction",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourcePlacementEviction",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourcePlacementDisruptionBudget",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourcePlacementDisruptionBudget",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourceOverride",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourceOverride",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ClusterResourceOverrideSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ClusterResourceOverrideSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ResourceOverride",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ResourceOverride",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1beta1",
			Kind:    "ResourceOverrideSnapshot",
		},
		{
			Group:   "placement.kubernetes-fleet.io",
			Version: "v1",
			Kind:    "ResourceOverrideSnapshot",
		},
	}

	resourcesNotInDefaultResourcesList := []schema.GroupVersionKind{
		{
			Group:   "",
			Version: "v1",
			Kind:    "Namespace",
		},
		{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		},
		{
			Group:   "",
			Version: "v1",
			Kind:    "Event",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1alpha1",
			Kind:    "ServiceExport",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1beta1",
			Kind:    "ServiceExport",
		},
	}

	tests := map[string]struct {
		isAllowList bool
		disabled    []schema.GroupVersionKind
		enabled     []schema.GroupVersionKind
	}{
		"default disabled list": {
			isAllowList: false,
			disabled:    resourcesInDefaultDisabledList,
			enabled:     resourcesNotInDefaultResourcesList,
		},
		"default enabled list": {
			isAllowList: true,
			disabled:    append(resourcesNotInDefaultResourcesList, resourcesInDefaultDisabledList...),
			enabled:     []schema.GroupVersionKind{},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			r := newTestResourceConfig(t, test.isAllowList, "")
			checkIfResourcesAreDisabledInConfig(t, r, test.disabled)
			checkIfResourcesAreEnabledInConfig(t, r, test.enabled)
		})
	}
}

// newTestResourceConfig creates a new ResourceConfig for either allow or disable list
// for testing with resources parsed from the input string. If the input string is not
// valid, it will fail the test.
func newTestResourceConfig(t *testing.T, isAllowList bool, input string) *ResourceConfig {
	r := NewResourceConfig(isAllowList)
	if err := r.Parse(input); err != nil {
		t.Fatalf("Parse() returned error: %v", err)
	}
	return r
}

// checkIfResourcesAreDisabledInConfig checks if the resources are disabled in the ResourceConfig.
// If the check fails, it will fail the test.
func checkIfResourcesAreDisabledInConfig(t *testing.T, r *ResourceConfig, resources []schema.GroupVersionKind) {
	for _, o := range resources {
		if ok := r.IsResourceDisabled(o); !ok {
			t.Errorf("IsResourceDisabled(%v) = false, want true", o)
		}
	}
}

// checkIfResourcesAreEnabledInConfig checks if the resources are enabled in the ResourceConfig.
// If the check fails, it will fail the test.
func checkIfResourcesAreEnabledInConfig(t *testing.T, r *ResourceConfig, resources []schema.GroupVersionKind) {
	for _, o := range resources {
		if ok := r.IsResourceDisabled(o); ok {
			t.Errorf("IsResourceDisabled(%v) = true, want false", o)
		}
	}
}

// testResource represents a simplified API resource for testing
type testResource struct {
	Group    string
	Version  string
	Resource string
	Kind     string
}

// newTestRESTMapper creates a RESTMapper with the specified resources for testing.
// Each resource is configured with standard settings (namespaced, standard verbs).
// Assumes input resources are valid and well-formed.
func newTestRESTMapper(resources ...testResource) meta.RESTMapper {
	groupMap := make(map[string]*restmapper.APIGroupResources)

	for _, res := range resources {
		groupVersion := res.Version
		if res.Group != "" {
			groupVersion = res.Group + "/" + res.Version
		}

		// Initialize group if not exists
		if groupMap[res.Group] == nil {
			groupMap[res.Group] = &restmapper.APIGroupResources{
				Group: metav1.APIGroup{
					Name: res.Group,
					Versions: []metav1.GroupVersionForDiscovery{
						{GroupVersion: groupVersion, Version: res.Version},
					},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: groupVersion,
						Version:      res.Version,
					},
				},
				VersionedResources: make(map[string][]metav1.APIResource),
			}
		}

		// Add resource to the version
		groupMap[res.Group].VersionedResources[res.Version] = append(
			groupMap[res.Group].VersionedResources[res.Version],
			metav1.APIResource{
				Name:       res.Resource,
				Kind:       res.Kind,
				Namespaced: true,
				Verbs:      resource.VerbsAll,
			},
		)
	}

	// Convert map to slice
	groupResources := make([]*restmapper.APIGroupResources, 0, len(groupMap))
	for _, group := range groupMap {
		groupResources = append(groupResources, group)
	}

	return restmapper.NewDiscoveryRESTMapper(groupResources)
}

func TestShouldProcessResource(t *testing.T) {
	tests := []struct {
		name           string
		gvr            schema.GroupVersionResource
		resourceConfig *ResourceConfig
		setupMapper    func() meta.RESTMapper
		expected       bool
	}{
		{
			name:           "returns true when resourceConfig is nil",
			gvr:            schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
			resourceConfig: nil,
			setupMapper: func() meta.RESTMapper {
				return newTestRESTMapper(
					testResource{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap"},
				)
			},
			expected: true,
		},
		{
			name: "returns true when resource is not disabled",
			gvr:  schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
			resourceConfig: func() *ResourceConfig {
				rc := NewResourceConfig(false)
				// Disable secrets, but not configmaps
				_ = rc.Parse("v1/Secret")
				return rc
			}(),
			setupMapper: func() meta.RESTMapper {
				return newTestRESTMapper(
					testResource{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap"},
				)
			},
			expected: true,
		},
		{
			name: "returns false when resource is disabled",
			gvr:  schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
			resourceConfig: func() *ResourceConfig {
				rc := NewResourceConfig(false)
				_ = rc.Parse("v1/Secret")
				return rc
			}(),
			setupMapper: func() meta.RESTMapper {
				return newTestRESTMapper(
					testResource{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret"},
				)
			},
			expected: false,
		},
		{
			name: "returns false when GVR mapping fails",
			gvr: schema.GroupVersionResource{
				Group:    "invalid.group",
				Version:  "v1",
				Resource: "nonexistent",
			},
			resourceConfig: NewResourceConfig(false),
			setupMapper: func() meta.RESTMapper {
				// Empty mapper - will fail to map the GVR
				return newTestRESTMapper()
			},
			expected: false,
		},
		{
			name:           "handles apps group resources correctly",
			gvr:            schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			resourceConfig: nil,
			setupMapper: func() meta.RESTMapper {
				return newTestRESTMapper(
					testResource{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment"},
				)
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restMapper := tt.setupMapper()
			result := ShouldProcessResource(tt.gvr, restMapper, tt.resourceConfig)
			if result != tt.expected {
				t.Errorf("ShouldProcessResource() = %v, want %v", result, tt.expected)
			}
		})
	}
}
