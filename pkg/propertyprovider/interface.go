/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package propertyprovider features interfaces and other components that can be used to build
// a Fleet property provider.
package propertyprovider

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

// PropertyCollectionResponse is returned by a Fleet property provider to report cluster properties
// and their property collection status.
type PropertyCollectionResponse struct {
	// Properties is an array of non-resource properties and their values. The key should be the
	// name of the property, which is a Kubernetes label name; the value is the property data.
	Properties map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue
	// Resources is a group of resources, described by their allocatable capacity and
	// available capacity.
	Resources clusterv1beta1.ResourceUsage
	// Conditions is an array of conditions that explains the property collection status.
	Conditions []metav1.Condition
}

// PropertyProvider is the interface that every property provider must implement.
type PropertyProvider interface {
	// Collect is called periodically by the Fleet member agent to collect properties.
	//
	// Note that this call should complete promptly. Fleet member agent will cancel the
	// context if the call does not complete in time.
	Collect(ctx context.Context) PropertyCollectionResponse
	// Start is called when the Fleet member agent starts up to initialize the property provider.
	// This call should not block.
	//
	// Note that Fleet member agent will cancel the context when it exits.
	Start(ctx context.Context, config *rest.Config) error
}
