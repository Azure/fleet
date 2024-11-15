/*
Copyright 2021 The Kubernetes Authors.

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

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// The following definitions are originally declared in the controllers/workv1alpha1/manager.go file.
const (
	// ManifestHashAnnotation is the annotation that indicates whether the spec of the object has been changed or not.
	ManifestHashAnnotation = fleetPrefix + "spec-hash"

	// LastAppliedConfigAnnotation is to record the last applied configuration on the object.
	LastAppliedConfigAnnotation = fleetPrefix + "last-applied-configuration"

	// WorkConditionTypeApplied represents workload in Work is applied successfully on the spoke cluster.
	WorkConditionTypeApplied = "Applied"

	// WorkConditionTypeAvailable represents workload in Work is available on the spoke cluster.
	WorkConditionTypeAvailable = "Available"
)

// This api is copied from https://github.com/kubernetes-sigs/work-api/blob/master/pkg/apis/v1alpha1/work_types.go.
// Renamed original "ResourceIdentifier" so that it won't conflict with ResourceIdentifier defined in the clusterresourceplacement_types.go.

// WorkSpec defines the desired state of Work.
type WorkSpec struct {
	// Workload represents the manifest workload to be deployed on spoke cluster
	Workload WorkloadTemplate `json:"workload,omitempty"`

	// ApplyStrategy describes how to resolve the conflict if the resource to be placed already exists in the target cluster
	// and is owned by other appliers.
	// +optional
	ApplyStrategy *ApplyStrategy `json:"applyStrategy,omitempty"`
}

// WorkloadTemplate represents the manifest workload to be deployed on spoke cluster
type WorkloadTemplate struct {
	// Manifests represents a list of kubernetes resources to be deployed on the spoke cluster.
	// +optional
	Manifests []Manifest `json:"manifests,omitempty"`
}

// Manifest represents a resource to be deployed on spoke cluster.
type Manifest struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	runtime.RawExtension `json:",inline"`
}

// WorkStatus defines the observed state of Work.
type WorkStatus struct {
	// Conditions contains the different condition statuses for this work.
	// Valid condition types are:
	// 1. Applied represents workload in Work is applied successfully on the spoke cluster.
	// 2. Progressing represents workload in Work in the transitioning from one state to another the on the spoke cluster.
	// 3. Available represents workload in Work exists on the spoke cluster.
	// 4. Degraded represents the current state of workload does not match the desired
	// state for a certain period.
	Conditions []metav1.Condition `json:"conditions"`

	// ManifestConditions represents the conditions of each resource in work deployed on
	// spoke cluster.
	// +optional
	ManifestConditions []ManifestCondition `json:"manifestConditions,omitempty"`
}

// WorkResourceIdentifier provides the identifiers needed to interact with any arbitrary object.
// Renamed original "ResourceIdentifier" so that it won't conflict with ResourceIdentifier defined in the clusterresourceplacement_types.go.
type WorkResourceIdentifier struct {
	// Ordinal represents an index in manifests list, so the condition can still be linked
	// to a manifest even though manifest cannot be parsed successfully.
	Ordinal int `json:"ordinal"`

	// Group is the group of the resource.
	Group string `json:"group,omitempty"`

	// Version is the version of the resource.
	Version string `json:"version,omitempty"`

	// Kind is the kind of the resource.
	Kind string `json:"kind,omitempty"`

	// Resource is the resource type of the resource.
	Resource string `json:"resource,omitempty"`

	// Namespace is the namespace of the resource, the resource is cluster scoped if the value
	// is empty.
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the resource.
	Name string `json:"name,omitempty"`

	// GenerateName is the generate name of the resource.
	GenerateName string `json:"generateName,omitempty"`
}

// DriftDetails describes the observed configuration drifts.
type DriftDetails struct {
	// ObservationTime is the timestamp when the drift was last detected.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	ObservationTime metav1.Time `json:"observationTime"`

	// ObservedInMemberClusterGeneration is the generation of the applied manifest on the member
	// cluster side.
	//
	// +kubebuilder:validation:Required
	ObservedInMemberClusterGeneration int64 `json:"observedInMemberClusterGeneration"`

	// FirsftDriftedObservedTime is the timestamp when the drift was first detected.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	FirstDriftedObservedTime metav1.Time `json:"firstDriftedObservedTime"`

	// ObservedDrifts describes each drifted field found from the applied manifest.
	// Fleet might truncate the details as appropriate to control object size.
	//
	// Each entry specifies how the live state (the state on the member cluster side) compares
	// against the desired state (the state kept in the hub cluster manifest).
	//
	// +kubebuilder:validation:Optional
	ObservedDrifts []PatchDetail `json:"observedDrifts,omitempty"`
}

// DiffDetails describes the observed configuration differences.
type DiffDetails struct {
	// ObservationTime is the timestamp when the configuration difference was last detected.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	ObservationTime metav1.Time `json:"observationTime"`

	// ObservedInMemberClusterGeneration is the generation of the applied manifest on the member
	// cluster side.
	//
	// +kubebuilder:validation:Required
	ObservedInMemberClusterGeneration int64 `json:"observedInMemberClusterGeneration"`

	// FirsftDiffedObservedTime is the timestamp when the configuration difference
	// was first detected.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	FirstDiffedObservedTime metav1.Time `json:"firstDiffedObservedTime"`

	// ObservedDiffs describes each field with configuration difference as found from the
	// member cluster side.
	//
	// Fleet might truncate the details as appropriate to control object size.
	//
	// Each entry specifies how the live state (the state on the member cluster side) compares
	// against the desired state (the state kept in the hub cluster manifest).
	//
	// +kubebuilder:validation:Optional
	ObservedDiffs []PatchDetail `json:"observedDiffs,omitempty"`
}

// ManifestCondition represents the conditions of the resources deployed on
// spoke cluster.
type ManifestCondition struct {
	// resourceId represents a identity of a resource linking to manifests in spec.
	// +required
	Identifier WorkResourceIdentifier `json:"identifier,omitempty"`

	// Conditions represents the conditions of this resource on spoke cluster
	// +required
	Conditions []metav1.Condition `json:"conditions"`

	// DriftDetails explains about the observed configuration drifts.
	// Fleet might truncate the details as appropriate to control object size.
	//
	// Note that configuration drifts can only occur on a resource if it is currently owned by
	// Fleet and its corresponding placement is set to use the ClientSideApply or ServerSideApply
	// apply strategy. In other words, DriftDetails and DiffDetails will not be populated
	// at the same time.
	//
	// +kubebuilder:validation:Optional
	DriftDetails *DriftDetails `json:"driftDetails,omitempty"`

	// DiffDetails explains the details about the observed configuration differences.
	// Fleet might truncate the details as appropriate to control object size.
	//
	// Note that configuration differences can only occur on a resource if it is not currently owned
	// by Fleet (i.e., it is a pre-existing resource that needs to be taken over), or if its
	// corresponding placement is set to use the ReportDiff apply strategy. In other words,
	// DiffDetails and DriftDetails will not be populated at the same time.
	//
	// +kubebuilder:validation:Optional
	DiffDetails *DiffDetails `json:"diffDetails,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={fleet,fleet-placement}
// +kubebuilder:storageversion

// Work is the Schema for the works API.
type Work struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the workload of a work.
	// +optional
	Spec WorkSpec `json:"spec,omitempty"`
	// status defines the status of each applied manifest on the spoke cluster.
	Status WorkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkList contains a list of Work.
type WorkList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of works.
	// +listType=set
	Items []Work `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Work{}, &WorkList{})
}
