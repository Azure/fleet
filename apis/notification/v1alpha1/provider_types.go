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
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-notification},shortName=npc
// +kubebuilder:storageversion

// NotificationProviderClass describes a notification provider in Fleet.
type NotificationProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the NotificationProviderClass.
	// +required
	Spec NotificationProviderClassSpec `json:"spec"`

	// Status is the observed state of the NotificationProviderClass.
	// +optional
	Status NotificationProviderClassStatus `json:"status,omitempty"`
}

// NotificationProviderClassSpec is the desired state of NotificationProviderClass.
type NotificationProviderClassSpec struct {
	// ProviderType is the type of the notification provider.
	//
	// Below are the currently supported provider types:
	// * email: send an email notification via SMTP.
	//
	// TO-DO (chenyu1): add more provider types, popular candidates include: teams, slack, webhook,
	// etc.
	// +required
	// +kubebuilder:validation:Enum=email
	ProviderType string `json:"providerType"`

	// Parameters is an object reference to a Secret object that contains the provider-specific
	// configuration, e.g., SMTP server address, port, username, password, etc for email provider.
	// +required
	Parameters corev1.ObjectReference `json:"parameters"`

	// MaxDeliveryAttempts is the maximum number of delivery attempts for a notification.
	// Defaults to 3; the minimum and maximum values are 1 and 10 respectively.
	//
	// Note that decreasing this value after the object's creation might lead Fleet to stop
	// retrying the delivery of some notifications; increasing this value, however, will not
	// set Fleet to retry the delivery of notifications that have been deemed stale.
	// +optional
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	MaxDeliveryAttempts *int32 `json:"maxDeliveryAttempts,omitempty"`

	// StaleAfterMinutes is the period of time after which a notification is considered stale;
	// Fleet will stop (re-)trying the delivery of a stale notification, even if
	// the maximum number of delivery attempts has not been reached yet.
	//
	// This is mostly helpful in cases where some delivery attempt objects are left in the system
	// without being cleaned up propoerly, e.g., due to disconnection between the Fleet agent and
	// the cluster.
	//
	// Defaults to 60 minutes; the minimum and maximum values are 15 and 1440 minutes respectively.
	//
	// Note that decreasing this value after the object's creation might lead Fleet to stop
	// (re-)trying the delivery of some notifications; increasing this value, however, will not
	// set Fleet to (re-)try the delivery of notifications that have been deemed stale.
	// +optional
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=15
	// +kubebuilder:validation:Maximum=1440
	StaleAfterMinutes *int32 `json:"staleAfterMinutes,omitempty"`
}

// NotificationProviderClassStatus is the observed state of NotificationProviderClass.
type NotificationProviderClassStatus struct {
	// Conditions is the list of currently observed conditions for this NotificationProviderClass object.
	//
	// Available condition types include:
	// * Valid: whether the provider configuration is valid.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NotificationProviderClassConditionType identifies a specific condition of NotificationProviderClass.
// +enum
type NotificationProviderClassConditionType string

const (
	// ProviderClassValidConditionType indicates whether the NotificationProviderClass is valid.
	//
	// The following status values are possible:
	// * True: the NotificationProviderClass is valid; no issues have been detected.
	// * False: the NotificationProviderClass is invalid.
	ProviderClassValidConditionType NotificationProviderClassConditionType = "Valid"
)

// NotificationConfigList is a collection of NotificationConfig objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NotificationProviderClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of NotificationProviderClass objects.
	Items []NotificationProviderClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationProviderClass{}, &NotificationProviderClassList{})
}
