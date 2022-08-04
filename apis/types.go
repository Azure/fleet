package apis

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

// A Conditioned may have conditions set or retrieved. Conditions are typically
// indicate the status of both a resource and its reconciliation process.
type Conditioned interface {
	SetConditions(fleetv1alpha1.AgentType, ...metav1.Condition)
	GetCondition(fleetv1alpha1.AgentType, string) *metav1.Condition
}

// A ConditionedObj is for kubernetes resource with conditions.
type ConditionedObj interface {
	client.Object
	Conditioned
}
