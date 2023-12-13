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
		r := NewResourceConfig(false)
		if err := r.Parse(test.input); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		for i, o := range test.disabled {
			ok := r.IsResourceConfigured(o)
			if !ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
		for i, o := range test.enabled {
			ok := r.IsResourceConfigured(o)
			if ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
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
		r := NewResourceConfig(false)
		if err := r.Parse(test.input); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		for i, o := range test.disabled {
			ok := r.IsResourceConfigured(o)
			if !ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
		for i, o := range test.enabled {
			ok := r.IsResourceConfigured(o)
			if ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
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
		r := NewResourceConfig(false)
		if err := r.Parse(test.input); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		for i, o := range test.disabled {
			ok := r.IsResourceConfigured(o)
			if !ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
		for i, o := range test.enabled {
			ok := r.IsResourceConfigured(o)
			if ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
	}
}

func TestResourceConfigMixedParse(t *testing.T) {
	tests := []struct {
		input    string
		disabled []schema.GroupVersionKind
		enabled  []schema.GroupVersionKind
	}{
		{
			input: "v1/Node,Pod;networking.k8s.io;apps/v1;authorization.k8s.io/v1/SelfSubjectRulesReview",
			disabled: []schema.GroupVersionKind{
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
			},
			enabled: []schema.GroupVersionKind{
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
			},
		},
	}
	for _, test := range tests {
		r := NewResourceConfig(false)
		if err := r.Parse(test.input); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		for i, o := range test.disabled {
			ok := r.IsResourceConfigured(o)
			if !ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
		for i, o := range test.enabled {
			ok := r.IsResourceConfigured(o)
			if ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
	}
}

func TestDefaultDisabledResourceConfigGroupVersionKindParse(t *testing.T) {
	tests := []struct {
		disabled []schema.GroupVersionKind
		enabled  []schema.GroupVersionKind
	}{
		{
			disabled: []schema.GroupVersionKind{
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
			},
			enabled: []schema.GroupVersionKind{
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
			},
		},
	}
	for _, test := range tests {
		r := NewResourceConfig(false)
		if err := r.Parse(""); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		for i, o := range test.disabled {
			ok := r.IsResourceConfigured(o)
			if !ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
		for i, o := range test.enabled {
			ok := r.IsResourceConfigured(o)
			if ok {
				t.Errorf("%d: unexpected error: %v", i, o)
			}
		}
	}
}

func TestResourceConfigIsEmpty(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{
			input: "",
			want:  true,
		},
		{
			input: "v1/Node,Pod;networking.k8s.io;apps/v1;authorization.k8s.io/v1/SelfSubjectRulesReview",
			want:  false,
		},
	}
	for _, test := range tests {
		r := NewResourceConfig(false)
		if err := r.Parse(test.input); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if got := r.IsEmpty(); got != test.want {
			t.Errorf("Unexpected result: %v", got)
		}
	}
}
