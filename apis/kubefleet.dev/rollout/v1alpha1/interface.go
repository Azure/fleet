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

var _ ApprovalRequestAccessor = &ApprovalRequest{}
var _ ApprovalRequestAccessor = &ClusterApprovalRequest{}

// ApprovalRequestAccessor is an interface that provides unified access to the Spec and Status of
// ApprovalRequest and ClusterApprovalRequest resources.
//
// +kubebuilder:object:generate=false
type ApprovalRequestAccessor interface {
	client.Object

	GetSpec() *ApprovalRequestSpec
	GetStatus() *ApprovalRequestStatus

	SetSpec(ApprovalRequestSpec)
	SetStatus(ApprovalRequestStatus)
}

// GetSpec returns the spec of the ApprovalRequest.
func (s *ApprovalRequest) GetSpec() *ApprovalRequestSpec {
	return &s.Spec
}

// GetStatus returns the status of the ApprovalRequest.
func (s *ApprovalRequest) GetStatus() *ApprovalRequestStatus {
	return &s.Status
}

// SetSpec sets the spec of the ApprovalRequest.
func (s *ApprovalRequest) SetSpec(spec ApprovalRequestSpec) {
	s.Spec = spec
}

// SetStatus sets the status of the ApprovalRequest.
func (s *ApprovalRequest) SetStatus(status ApprovalRequestStatus) {
	s.Status = status
}

// GetSpec returns the spec of the ClusterApprovalRequest.
func (s *ClusterApprovalRequest) GetSpec() *ApprovalRequestSpec {
	return &s.Spec
}

// GetStatus returns the status of the ClusterApprovalRequest.
func (s *ClusterApprovalRequest) GetStatus() *ApprovalRequestStatus {
	return &s.Status
}

// SetSpec sets the spec of the ClusterApprovalRequest.
func (s *ClusterApprovalRequest) SetSpec(spec ApprovalRequestSpec) {
	s.Spec = spec
}

// SetStatus sets the status of the ClusterApprovalRequest.
func (s *ClusterApprovalRequest) SetStatus(status ApprovalRequestStatus) {
	s.Status = status
}

var _ StagedUpdateRunAccessor = &StagedUpdateRun{}
var _ StagedUpdateRunAccessor = &ClusterStagedUpdateRun{}

// StagedUpdateRunAccessor is an interface that provides unified access to the Spec and Status of
// StagedUpdateRun and ClusterStagedUpdateRun resources.
//
// +kubebuilder:object:generate=false
type StagedUpdateRunAccessor interface {
	client.Object

	GetSpec() *StagedUpdateRunSpec
	GetStatus() *StagedUpdateRunStatus

	SetSpec(StagedUpdateRunSpec)
	SetStatus(StagedUpdateRunStatus)
}

// GetSpec returns the spec of the StagedUpdateRun.
func (s *StagedUpdateRun) GetSpec() *StagedUpdateRunSpec {
	return &s.Spec
}

// GetStatus returns the status of the StagedUpdateRun.
func (s *StagedUpdateRun) GetStatus() *StagedUpdateRunStatus {
	return &s.Status
}

// SetSpec sets the spec of the StagedUpdateRun.
func (s *StagedUpdateRun) SetSpec(spec StagedUpdateRunSpec) {
	s.Spec = spec
}

// SetStatus sets the status of the StagedUpdateRun.
func (s *StagedUpdateRun) SetStatus(status StagedUpdateRunStatus) {
	s.Status = status
}

// GetSpec returns the spec of the ClusterStagedUpdateRun.
func (s *ClusterStagedUpdateRun) GetSpec() *StagedUpdateRunSpec {
	return &s.Spec
}

// GetStatus returns the status of the ClusterStagedUpdateRun.
func (s *ClusterStagedUpdateRun) GetStatus() *StagedUpdateRunStatus {
	return &s.Status
}

// SetSpec sets the spec of the ClusterStagedUpdateRun.
func (s *ClusterStagedUpdateRun) SetSpec(spec StagedUpdateRunSpec) {
	s.Spec = spec
}

// SetStatus sets the status of the ClusterStagedUpdateRun.
func (s *ClusterStagedUpdateRun) SetStatus(status StagedUpdateRunStatus) {
	s.Status = status
}

var _ StagedUpdateStrategyAccessor = &StagedUpdateStrategy{}
var _ StagedUpdateStrategyAccessor = &ClusterStagedUpdateStrategy{}

// StagedUpdateStrategyAccessor is an interface that provides unified access to the Spec of
// StagedUpdateStrategy and ClusterStagedUpdateStrategy resources.
//
// +kubebuilder:object:generate=false
type StagedUpdateStrategyAccessor interface {
	client.Object

	GetSpec() *StagedUpdateStrategySpec

	SetSpec(StagedUpdateStrategySpec)
}

// GetSpec returns the spec of the StagedUpdateStrategy.
func (s *StagedUpdateStrategy) GetSpec() *StagedUpdateStrategySpec {
	return &s.Spec
}

// SetSpec sets the spec of the StagedUpdateStrategy.
func (s *StagedUpdateStrategy) SetSpec(spec StagedUpdateStrategySpec) {
	s.Spec = spec
}

// GetSpec returns the spec of the ClusterStagedUpdateStrategy.
func (s *ClusterStagedUpdateStrategy) GetSpec() *StagedUpdateStrategySpec {
	return &s.Spec
}

// SetSpec sets the spec of the ClusterStagedUpdateStrategy.
func (s *ClusterStagedUpdateStrategy) SetSpec(spec StagedUpdateStrategySpec) {
	s.Spec = spec
}
