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

// Package apis contains API interfaces for the fleet API group.
package apis

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A Conditioned may have conditions set or retrieved. Conditions typically
// indicate the status of both a resource and its reconciliation process.
// +kubebuilder:object:generate=false
type Conditioned interface {
	SetConditions(...metav1.Condition)
	GetCondition(string) *metav1.Condition
}

// A ConditionedObj is for kubernetes resource with conditions.
// +kubebuilder:object:generate=false
type ConditionedObj interface {
	client.Object
	Conditioned
}
