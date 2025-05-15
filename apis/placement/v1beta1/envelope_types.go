/*
Copyright 2025 The KubeFleet Authors.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion

// ClusterResourceEnvelope wraps cluster-scoped resources for placement.
type ClusterResourceEnvelope struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The manifests wrapped in this envelope.
	//
	// Each manifest is uniquely identified by a string key, typically a filename that represents
	// the manifest. The value is the manifest object itself.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=50
	Data map[string]runtime.RawExtension `json:"data"`
}

// ClusterResourceEnvelopeList contains a list of ClusterResourceEnvelope objects.
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterResourceEnvelopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ClusterResourceEnvelope objects.
	Items []ClusterResourceEnvelope `json:"items"`
}

// +genclient
// +genclient:Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Namespaced",categories={fleet,fleet-placement}
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion

// ResourceEnvelope wraps namespaced resources for placement.
type ResourceEnvelope struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The manifests wrapped in this envelope.
	//
	// Each manifest is uniquely identified by a string key, typically a filename that represents
	// the manifest. The value is the manifest object itself.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=50
	Data map[string]runtime.RawExtension `json:"data"`
}

// ResourceEnvelopeList contains a list of ResourceEnvelope objects.
// +kubebuilder:resource:scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceEnvelopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of ResourceEnvelope objects.
	Items []ResourceEnvelope `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&ClusterResourceEnvelope{},
		&ClusterResourceEnvelopeList{},
		&ResourceEnvelope{},
		&ResourceEnvelopeList{})
}

// +kubebuilder:object:generate=false
// EnvelopeReader is an interface that allows retrieval of common information across all envelope CRs.
type EnvelopeReader interface {
	// GetData returns the raw data in the envelope.
	GetData() map[string]runtime.RawExtension

	// GetEnvelopeObjRef returns a klog object reference to the envelope.
	GetEnvelopeObjRef() klog.ObjectRef

	// GetNamespace returns the namespace of the envelope.
	GetNamespace() string

	// GetName returns the name of the envelope.
	GetName() string

	// GetEnvelopeType returns the type of the envelope.
	GetEnvelopeType() string
}

// Ensure that both ClusterResourceEnvelope and ResourceEnvelope implement the
// EnvelopeReader interface at compile time.
var (
	_ EnvelopeReader = &ClusterResourceEnvelope{}
	_ EnvelopeReader = &ResourceEnvelope{}
)

// Implements the EnvelopeReader interface for ClusterResourceEnvelope.

func (e *ClusterResourceEnvelope) GetData() map[string]runtime.RawExtension {
	return e.Data
}

func (e *ClusterResourceEnvelope) GetEnvelopeObjRef() klog.ObjectRef {
	return klog.KObj(e)
}

func (e *ClusterResourceEnvelope) GetEnvelopeType() string {
	return string(ClusterResourceEnvelopeType)
}

// Implements the EnvelopeReader interface for ResourceEnvelope.

func (e *ResourceEnvelope) GetData() map[string]runtime.RawExtension {
	return e.Data
}

func (e *ResourceEnvelope) GetEnvelopeObjRef() klog.ObjectRef {
	return klog.KObj(e)
}

func (e *ResourceEnvelope) GetEnvelopeType() string {
	return string(ResourceEnvelopeType)
}
