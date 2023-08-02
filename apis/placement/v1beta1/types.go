package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A Conditioned may have conditions set or retrieved. Conditions typically
// indicate the status of both a resource and its reconciliation process.
type Conditioned interface {
	SetConditions(...metav1.Condition)
	GetCondition(string) *metav1.Condition
}

// A ConditionedWithType may have conditions set or retrieved based on agent type. Conditions typically
// indicate the status of both a resource and its reconciliation process.
type ConditionedWithType interface {
	SetConditionsWithType(AgentType, ...metav1.Condition)
	GetConditionWithType(AgentType, string) *metav1.Condition
}

// A ConditionedObj is for kubernetes resource with conditions.
type ConditionedObj interface {
	client.Object
	Conditioned
}

// A ConditionedAgentObj is for kubernetes resources where multiple agents can set and update conditions within AgentStatus.
type ConditionedAgentObj interface {
	client.Object
	ConditionedWithType
}
