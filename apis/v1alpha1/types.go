package v1alpha1

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

// A ConditionedWithType may have conditions set or retrieved based on agent type. Conditions typically
// indicate the status of both a resource and its reconciliation process.
// +kubebuilder:object:generate=false
type ConditionedWithType interface {
	SetConditionsWithType(AgentType, ...metav1.Condition)
	GetConditionWithType(AgentType, string) *metav1.Condition
}

// A ConditionedObj is for kubernetes resource with conditions.
// +kubebuilder:object:generate=false
type ConditionedObj interface {
	client.Object
	Conditioned
}

// A ConditionedAgentObj is for kubernetes resources where multiple agents can set and update conditions within AgentStatus.
// +kubebuilder:object:generate=false
type ConditionedAgentObj interface {
	client.Object
	ConditionedWithType
}
