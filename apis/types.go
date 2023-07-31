/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package apis

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

// A Conditioned may have conditions set or retrieved. Conditions typically
// indicate the status of both a resource and its reconciliation process.
type Conditioned interface {
	SetConditions(...metav1.Condition)
	GetCondition(string) *metav1.Condition
}

// A V1alpha1ConditionedWithType may have conditions set or retrieved based on agent type. Conditions typically
// indicate the status of both a resource and its reconciliation process.
type V1alpha1ConditionedWithType interface {
	SetConditionsWithType(fleetv1alpha1.AgentType, ...metav1.Condition)
	GetConditionWithType(fleetv1alpha1.AgentType, string) *metav1.Condition
}

// A V1beta1ConditionedWithType may have conditions set or retrieved based on agent type. Conditions typically
// indicate the status of both a resource and its reconciliation process.
type V1beta1ConditionedWithType interface {
	SetConditionsWithType(fleetv1beta1.AgentType, ...metav1.Condition)
	GetConditionWithType(fleetv1beta1.AgentType, string) *metav1.Condition
}

// A ConditionedObj is for kubernetes resource with conditions.
type ConditionedObj interface {
	client.Object
	Conditioned
}

// A V1alpha1ConditionedAgentObj is for kubernetes resources where multiple agents can set and update conditions within AgentStatus.
type V1alpha1ConditionedAgentObj interface {
	client.Object
	V1alpha1ConditionedWithType
}

// A V1beta11ConditionedAgentObj is for kubernetes resources where multiple agents can set and update conditions within AgentStatus.
type V1beta11ConditionedAgentObj interface {
	client.Object
	V1beta1ConditionedWithType
}
