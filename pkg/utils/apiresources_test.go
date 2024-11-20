/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
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
			Group:   "fleet.azure.com",
			Version: "v1beta1",
			Kind:    "MemberCluster",
		},
		{
			Group:   "fleet.azure.com",
			Version: "v1alpha1",
			Kind:    "MemberCluster",
		},
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
			Version: "v1alpha1",
			Kind:    "TrafficManagerProfile",
		},
		{
			Group:   "networking.fleet.azure.com",
			Version: "v1alpha1",
			Kind:    "TrafficManagerBackend",
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
