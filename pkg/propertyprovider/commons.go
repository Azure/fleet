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

package propertyprovider

const (
	// A list of property names that should be supported by every property provider and
	// is available out of the box in Fleet without any property provider configuration.

	// The non-resource properties.
	// NodeCountProperty is a property that describes the number of nodes in the cluster.
	NodeCountProperty = "kubernetes-fleet.io/node-count"

	// The resource properties.
	// Total and allocatable CPU resource properties.
	TotalCPUCapacityProperty       = "resources.kubernetes-fleet.io/total-cpu"
	AllocatableCPUCapacityProperty = "resources.kubernetes-fleet.io/allocatable-cpu"
	AvailableCPUCapacityProperty   = "resources.kubernetes-fleet.io/available-cpu"

	// Total and allocatable memory resource properties.
	TotalMemoryCapacityProperty       = "resources.kubernetes-fleet.io/total-memory"
	AllocatableMemoryCapacityProperty = "resources.kubernetes-fleet.io/allocatable-memory"
	AvailableMemoryCapacityProperty   = "resources.kubernetes-fleet.io/available-memory"

	// ResourcePropertyNamePrefix is the prefix (also known as the subdomain) of the label name
	// associated with all resource properties.
	ResourcePropertyNamePrefix = "resources.kubernetes-fleet.io/"

	// Below are a list of supported capacity types.
	TotalCapacityName       = "total"
	AllocatableCapacityName = "allocatable"
	AvailableCapacityName   = "available"
)
