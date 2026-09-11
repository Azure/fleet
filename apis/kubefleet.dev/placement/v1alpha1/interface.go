/*
Copyright 2026 The KubeFleet Authors.

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

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Verify the implementation of the accessor interfaces for the placement policy,
// placement resource snapshot, and placement binding resources.
var _ PlacementPolicyAccessor = &PlacementPolicy{}
var _ PlacementPolicyAccessor = &ClusterPlacementPolicy{}
var _ PlacementResourceSnapshotAccessor = &PlacementResourceSnapshot{}
var _ PlacementResourceSnapshotAccessor = &ClusterPlacementResourceSnapshot{}
var _ PlacementBindingAccessor = &PlacementBinding{}
var _ PlacementBindingAccessor = &ClusterPlacementBinding{}

// PlacementPolicyAccessor provides unified access to the spec and status of placement policy resources,
// namespace-scoped and cluster-scoped.
//
// +kubebuilder:object:generate=false
type PlacementPolicyAccessor interface {
	client.Object

	GetSpec() *PlacementPolicySpec
	GetStatus() *PlacementPolicyStatus

	SetSpec(PlacementPolicySpec)
	SetStatus(PlacementPolicyStatus)
}

func (p *PlacementPolicy) GetSpec() *PlacementPolicySpec {
	return &p.Spec
}

func (p *PlacementPolicy) GetStatus() *PlacementPolicyStatus {
	return &p.Status
}

func (p *PlacementPolicy) SetSpec(spec PlacementPolicySpec) {
	p.Spec = spec
}

func (p *PlacementPolicy) SetStatus(status PlacementPolicyStatus) {
	p.Status = status
}

func (p *ClusterPlacementPolicy) GetSpec() *PlacementPolicySpec {
	return &p.Spec
}

func (p *ClusterPlacementPolicy) GetStatus() *PlacementPolicyStatus {
	return &p.Status
}

func (p *ClusterPlacementPolicy) SetSpec(spec PlacementPolicySpec) {
	p.Spec = spec
}

func (p *ClusterPlacementPolicy) SetStatus(status PlacementPolicyStatus) {
	p.Status = status
}

// PlacementResourceSnapshotAccessor provides unified access to the spec of placement resource snapshot resources,
// namespace-scoped and cluster-scoped.
//
// +kubebuilder:object:generate=false
type PlacementResourceSnapshotAccessor interface {
	client.Object

	GetSpec() *PlacementResourceSnapshotSpec

	SetSpec(PlacementResourceSnapshotSpec)
}

func (p *PlacementResourceSnapshot) GetSpec() *PlacementResourceSnapshotSpec {
	return &p.Spec
}

func (p *PlacementResourceSnapshot) SetSpec(spec PlacementResourceSnapshotSpec) {
	p.Spec = spec
}

func (p *ClusterPlacementResourceSnapshot) GetSpec() *PlacementResourceSnapshotSpec {
	return &p.Spec
}

func (p *ClusterPlacementResourceSnapshot) SetSpec(spec PlacementResourceSnapshotSpec) {
	p.Spec = spec
}

// PlacementBindingAccessor provides unified access to the spec and status of placement binding resources,
// namespace-scoped and cluster-scoped.
//
// +kubebuilder:object:generate=false
type PlacementBindingAccessor interface {
	client.Object

	GetSpec() *PlacementBindingSpec
	GetStatus() *PlacementBindingStatus

	SetSpec(PlacementBindingSpec)
	SetStatus(PlacementBindingStatus)
}

func (p *PlacementBinding) GetSpec() *PlacementBindingSpec {
	return &p.Spec
}

func (p *PlacementBinding) GetStatus() *PlacementBindingStatus {
	return &p.Status
}

func (p *PlacementBinding) SetSpec(spec PlacementBindingSpec) {
	p.Spec = spec
}

func (p *PlacementBinding) SetStatus(status PlacementBindingStatus) {
	p.Status = status
}

func (p *ClusterPlacementBinding) GetSpec() *PlacementBindingSpec {
	return &p.Spec
}

func (p *ClusterPlacementBinding) GetStatus() *PlacementBindingStatus {
	return &p.Status
}

func (p *ClusterPlacementBinding) SetSpec(spec PlacementBindingSpec) {
	p.Spec = spec
}

func (p *ClusterPlacementBinding) SetStatus(status PlacementBindingStatus) {
	p.Status = status
}
