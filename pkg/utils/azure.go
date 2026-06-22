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
	"slices"

	authenticationv1 "k8s.io/api/authentication/v1"
)

// This file contains constants and utility functions related to Azure-specific logic.
// We define these in a separate file to avoid potential merge conflicts from the KubeFleet repository when backporting changes.
const (
	// ReconcileLabelKey is the label key added to resources that should be reconciled.
	// The value indicates the reason the resource should be reconciled.
	ReconcileLabelKey = "fleet.azure.com/reconcile"
	// ReconcileLabelValue is the label value paired with ReconcileLabelKey,
	// indicating the resource is managed and should be reconciled.
	ReconcileLabelValue = "managed"

	// AKSServiceUserName is the username whose deployment requests should be mutated.
	AKSServiceUserName = "aksService"
	// SystemMastersGroup is the Kubernetes group representing cluster administrators.
	SystemMastersGroup = "system:masters"
)

// IsAKSService reports whether the user is the aksService user with
// system:masters group membership.
func IsAKSService(userInfo authenticationv1.UserInfo) bool {
	return userInfo.Username == AKSServiceUserName && slices.Contains(userInfo.Groups, SystemMastersGroup)
}

// HasReconcileLabel reports whether the given labels contain the fleet reconcile
// label key with a non-empty value.
func HasReconcileLabel(labels map[string]string) bool {
	return labels[ReconcileLabelKey] != ""
}
