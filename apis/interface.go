/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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
