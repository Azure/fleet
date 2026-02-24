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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// A list of property names that should be supported by every property provider and
	// is available out of the box in Fleet without any property provider configuration.

	// The non-resource properties.
	// NodeCountProperty is a property that describes the number of nodes in the cluster.
	NodeCountProperty = "kubernetes-fleet.io/node-count"

	// K8sVersionProperty is a property that describes the Kubernetes version of the cluster.
	K8sVersionProperty = "k8s.io/k8s-version"

	// ClusterEntryPointProperty is a property that describes the cluster entry point (API server endpoint).
	ClusterEntryPointProperty = "k8s.io/cluster-entrypoint"

	// ClusterCertificateAuthorityProperty is a property that describes the cluster's certificate authority data (base64 encoded).
	ClusterCertificateAuthorityProperty = "k8s.io/cluster-certificate-authority-data"

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

const (
	NamespaceCollectionSucceededCondType = "NamespaceCollectionSucceeded"
	NamespaceCollectionSucceededReason   = "Succeeded"
	NamespaceCollectionDegradedReason    = "Degraded"
	NamespaceCollectionSucceededMsg      = "All namespaces have been collected successfully"
	NamespaceCollectionDegradedMsg       = "Namespaces are collected in a degraded mode since the number of namespaces reaches the track limit"
)

// BuildNamespaceCollectionConditions builds the conditions based on the reachLimit value.
func BuildNamespaceCollectionConditions(reachLimit bool) []metav1.Condition {
	conds := make([]metav1.Condition, 0, 1)
	var cond metav1.Condition
	if reachLimit {
		cond = metav1.Condition{
			Type:    NamespaceCollectionSucceededCondType,
			Status:  metav1.ConditionFalse,
			Reason:  NamespaceCollectionDegradedReason,
			Message: NamespaceCollectionDegradedMsg,
		}
	} else {
		cond = metav1.Condition{
			Type:    NamespaceCollectionSucceededCondType,
			Status:  metav1.ConditionTrue,
			Reason:  NamespaceCollectionSucceededReason,
			Message: NamespaceCollectionSucceededMsg,
		}
	}
	return append(conds, cond)
}
