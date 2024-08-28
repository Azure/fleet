/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-notification},shortName=na
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// NotificationAttempt is a notification attempt record in Fleet. This object is expected to
// be owned by a NotificationConfig object.
type NotificationAttempt struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the NotificationAttempt.
	// +required
	Spec NotificationAttemptSpec `json:"spec"`

	// Status is the observed state of the NotificationAttempt.
	// +optional
	Status NotificationAttemptStatus `json:"status,omitempty"`
}

// NotificationAttemptSpec is the desired state of NotificationAttempt.
type NotificationAttemptSpec struct {
	// Regarding is the reference to the Fleet resource that this notification attempt is about.
	// +required
	Regarding corev1.ObjectReference `json:"regarding"`

	// Message is the notification message, as prepared using the template. To avoid
	// oversizing issues, the message is kept in a ConfigMap object.
	// +required
	Message corev1.ObjectReference `json:"message"`

	// NotificationProviderClass is the reference to the notification provider class that
	// this notification attempt uses.
	// +required
	NotificationProviderClass corev1.ObjectReference `json:"notificationProviderClass"`
}

// NotificationAttemptStatus is the observed state of NotificationAttempt.
type NotificationAttemptStatus struct {
	// FailedAttempts is the number of failed delivery attempts for this notification.
	FailedAttempts *int32 `json:"failedAttempts,omitempty"`

	// LastAttemptedTimestamp is the time of the last delivery attempt.
	LastAttemptedTimestamp *metav1.Time `json:"lastAttemptedTimestamp,omitempty"`

	// Conditions is the list of currently observed conditions for this NotificationAttempt object.
	//
	// Available condition types include:
	// * Delivered: whether the notification has been successfully delivered.
	// * FailedOrStale: whether this attempt has been abandoned due to too many failed attempts
	//   or becoming stale.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NotificationAttemptConditionType identifies a specific condition of NotificationAttempt.
// +enum
type NotificationAttemptConditionType string

const (
	// NotificationDeliveredConditionType indicates whether the notification has been delivered.
	//
	// The following status values are possible:
	// * True: the notification has been delivered.
	// * False: the notification has not been delivered; a failure has occurred.
	NotificationDeliveredConditionType NotificationAttemptConditionType = "Delivered"

	// NotificationFailedOrStaleConditionType indicates whether the notification attempt has failed or become stale.
	//
	// The following status values are possible:
	// * True: the notification attempt has failed or become stale.
	// * False: the notification attempt has not failed or become stale.
	NotificationFailedOrStaleConditionType NotificationAttemptConditionType = "FailedOrStale"
)

// NotificationAttemptList contains a list of NotificationAttempt objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NotificationAttemptList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of NotificationAttempt objects.
	Items []NotificationAttempt `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationAttempt{}, &NotificationAttemptList{})
}
