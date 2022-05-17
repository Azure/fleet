package common

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Condition types.
const (
	// ConditionTypeReady resources are believed to be ready to handle work.
	ConditionTypeReady string = "Ready"

	// ConditionTypeSynced resources are believed to be in sync with the
	// Kubernetes resources that manage their lifecycle.
	ConditionTypeSynced string = "Synced"
)

// Reasons a resource is or is not synced.
const (
	ReasonReconcileSuccess string = "ReconcileSuccess"
	ReasonReconcileError   string = "ReconcileError"
)

// ReconcileErrorCondition returns a condition indicating that we encountered an
// error while reconciling the resource.
func ReconcileErrorCondition(err error) metav1.Condition {
	return metav1.Condition{
		Type:    ConditionTypeSynced,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonReconcileError,
		Message: err.Error(),
	}
}

// ReconcileSuccessCondition returns a condition indicating that we successfully reconciled the resource
func ReconcileSuccessCondition() metav1.Condition {
	return metav1.Condition{
		Type:   ConditionTypeSynced,
		Status: metav1.ConditionTrue,
		Reason: ReasonReconcileSuccess,
	}
}
