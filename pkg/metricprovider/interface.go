/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package metricprovider features interfaces and other components that can be used to build
// a Fleet metric provider.
package metricprovider

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

// MetricCollectionResponse is returned by a Fleet metric provider to report metrics and
// metric collection status.
type MetricCollectionResponse struct {
	// Metrics is an array of metrics and their values. The key should be the name of the
	// metric, which is a Kubernetes label name; the value is the metric data.
	Metrics map[string]float64
	// Resources is a group of resources, described by their allocatable capacity and
	// available capacity.
	Resources clusterv1beta1.ResourceUsage
	// Conditions is an array of conditions that explains the metric collection status.
	Conditions []metav1.Condition
}

// MetricProvider is the interface that every metric provider must implement.
type MetricProvider interface {
	// Collect is called periodically by the Fleet member agent to collect metrics.
	Collect() MetricCollectionResponse
	// Start is called when the Fleet member agent starts up to initialize the metric provider.
	Start(config *rest.Config) error
}
