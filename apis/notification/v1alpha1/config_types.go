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
// +kubebuilder:resource:scope=Cluster,categories={fleet,fleet-notification},shortName=nc
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// NotificationConfig is a notification setup specification in Fleet.
type NotificationConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the NotificationConfig.
	// +required
	Spec NotificationConfigSpec `json:"spec"`

	// Status is the observed state of the NotificationConfig.
	// +optional
	Status NotificationConfigStatus `json:"status,omitempty"`
}

// NotificationConfigSpec is the desired state of NotificationConfig.
type NotificationConfigSpec struct {
	// TargetSelector is a selector that specifies the Fleet resources that this notification setup
	// should target at.
	// +required
	TargetSelector FleetResourceSelector `json:"targetSelector"`

	// Trigger is a condition of a targeted resource that will trigger a notification once the
	// condition is observed.
	//
	// A trigger can be either a CEL expression or a name that describes a pre-defined expression,
	// e.g.,
	//
	// * "object.status.conditions.filter(x, x.type == "Available").exists(x, x.status != "True")"
	//   This expression evaluates to true if the targeted resource has a condition of type
	//   "Available" but is not of the status "True".
	//
	// * "staged-run-stuck"
	//   This is a pre-defined trigger that detects if a staged update run has been stuck for
	//   some reason.
	//
	// Note that updating this field will reset track records kept by Fleet for notification
	// purposes, i.e., past triggerings will be forgotten.
	//
	// For more information, see the Fleet documentation.
	// +required
	Trigger string `json:"trigger"`

	// OnceEveryMinutes is the interval between two consecutive notifications on the same target
	// resource. Defaults to 60 minutes; the minimum and maximum values are 15 and 1440 minutes
	// respectively.
	//
	// It is recommended that a proper value be set for this field to avoid notification flooding.
	// +optional
	// +kubebuilder:validation:Minimum=15
	// +kubebuilder:validation:Maximum=1440
	// +kubebuilder:default=60
	OnceEveryMinutes *int32 `json:"onceEveryMinutes,omitempty"`

	// PeriodSeconds is the time period in seconds between attempts to evaluate the trigger
	// condition; i.e., how often Fleet will check the trigger condition on the target resources.
	// Defaults to 60 seconds; the minimum and maximum values are 15 and 600 seconds respectively.
	//
	// Use this field in combination with the triggerThreshold field to avoid sending
	// notifications in a premature manner.
	// +optional
	// +kubebuilder:validation:Minimum=15
	// +kubebuilder:validation:Maximum=600
	// +kubebuilder:default=60
	PeriodSeconds *int32 `json:"periodSeconds,omitempty"`

	// TriggerThreshold is the number of consecutive times the trigger condition must be observed
	// before a notification is sent.
	// Defaults to 1; the minimum and maximum values are 1 and 10 respectively.
	//
	// Use this field in combination with the periodSeconds field to avoid sending notifications
	// in a premature manner.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=1
	TriggerThreshold *int32 `json:"triggerThreshold,omitempty"`

	// NotificationMessageTemplate is an object reference to a ConfigMap object that contains the
	// template (Go HTML template) used for preparing the notification message.
	//
	// See the Fleet documentation for more information on how to specify a notification message
	// template.
	//
	// Note that Fleet provides a number of built-in templates that might be of use. Also, this is
	// an optional field; if no reference is provided, a default template will be used.
	// +optional
	NotificationMessageTemplate *corev1.ObjectReference `json:"notificationMessageTemplate,omitempty"`

	// NotificationProviderClass is an object reference to a NotificationProviderClass object that
	// specifies the notification provider to use for sending notifications.
	//
	// For more information, see the comments on the NotificationProviderClass type and the Fleet
	// documentation.
	// +required
	NotificationProviderClass *corev1.ObjectReference `json:"notificationProviderClass"`

	// NotificationAttemptHistoryLimit is the maximum number of notification attempt objects
	// to keep in the system for auditing purposes.
	//
	// Defaults to 10; the minimum and maximum values are 1 and 100 respectively. Note that Fleet
	// will honor this limit only in a best-effort manner.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	NotificationAttemptHistoryLimit *int32 `json:"notificationAttemptHistoryLimit,omitempty"`
}

// FleetResourceSelector is a selector that specifies the Fleet resources that a notification setup
// should target at.
type FleetResourceSelector struct {
	// Kind is the kind of the Fleet resource that this notification setup targets at.
	//
	// Currently the following kinds are supported:
	// * MemberCluster
	// * ClusterResourcePlacement
	// * StagedUpdateRun
	// +required
	// +kubebuilder:validation:Enum=MemberCluster;ClusterResourcePlacement;StagedUpdateRun
	Kind string `json:"kind"`

	// Name is the name of the Fleet resource that this notification setup targets at.
	//
	// If this field is not specified, and no label selector is present either,
	// all resources of the specified kind will be targeted.
	//
	// Also, this field is mutually exclusive with the label selector field; only one of them
	// can be set.
	// +optional
	Name string `json:"name,omitempty"`

	// LabelSelector is a label query over all resources of the specified kind. Any resource
	// that matches this label selector will be targeted by the notification setup.
	//
	// If this field is not specified, and no name is present either, all resources of the
	// specified kind will be targeted.
	//
	// Also, this field is mutually exclusive with the name field; only one of them can be set.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// NotificationConfigStatus is the observed state of NotificationConfig.
type NotificationConfigStatus struct {
	// Conditions is an array of currently observed conditions for this NotificationConfig object.
	//
	// Available condition types include:
	// * Valid: reports whether the NotificationConfig is valid.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NotificationConfigConditionType identifies a specific condition of NotificationConfig.
// +enum
type NotificationConfigConditionType string

const (
	// ConfigValidConditionType indicates whether the NotificationConfig is valid.
	//
	// The following status values are possible:
	// * True: the NotificationConfig is valid; no issues have been detected.
	// * False: the NotificationConfig is invalid; some issues, e.g., failing to evaluate
	//   a trigger condition, have occurred.
	ConfigValidConditionType NotificationConfigConditionType = "Valid"
)

// NotificationConfigList is a collection of NotificationConfig objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NotificationConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of NotificationConfig objects.
	Items []NotificationConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationConfig{}, &NotificationConfigList{})
}
