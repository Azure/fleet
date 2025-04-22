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
	//
	// Last transition time of each added condition is omitted if set and will instead be added
	// by the Fleet member agent.
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
