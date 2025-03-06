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

package framework

import (
	"fmt"
	"strings"
)

// StatusCode is the status code of a Status, returned by a plugin.
type StatusCode int

// Pre-defined status codes.
const (
	// Success signals that a plugin has completed its run successfully.
	// Note that a nil *Status is also considered as a Success Status.
	Success StatusCode = iota
	// internalError signals that a plugin has encountered an internal error.
	// Note that this status code is NOT exported; to return an internalError status, use the
	// FromError() call.
	internalError
	// ClusterUnschedulable signals that a plugin has found that a placement should not be bound
	// to a specific cluster.
	//
	// Return ClusterAlreadySelected if the placement has already been placed on the cluster.
	ClusterUnschedulable
	// ClusterAlreadySelected signals that a plugin has found that a placement has already been
	// placed on the cluster **per latest scheduling policy**.
	ClusterAlreadySelected
	// Skip signals that no action is needed for the plugin to take at the stage.
	// If this is returned by a plugin at the Pre- stages (PreFilter or PreScore), the associated
	// plugin will be skipped at the following stages (Filter or Score) as well. This helps
	// reduce the overhead of having to repeatedly call a plugin that is not needed for every
	// cluster in the Filter or Score stage.
	Skip
)

var statusCodeNames = []string{"Success", "InternalError", "ClusterUnschedulable", "ClusterAlreadySelected", "Skip"}

// Name returns the name of a status code.
func (sc StatusCode) Name() string {
	return statusCodeNames[sc]
}

// Status is the result yielded by a plugin.
type Status struct {
	// statusCode is the status code of a Status.
	statusCode StatusCode
	// The reasons behind a Status; this should be empty if the Status is of the status code
	// Success.
	reasons []string
	// The error associated with a Status; this is only set when the Status is of the status code
	// internalError.
	err error
	// The name of the plugin which returns the Status.
	sourcePlugin string
}

// code returns the status code of a Status.
func (s *Status) code() StatusCode {
	if s == nil {
		return Success
	}
	return s.statusCode
}

// IsSuccess returns if a Status is of the status code Success.
func (s *Status) IsSuccess() bool {
	return s.code() == Success
}

// IsInternalError returns if a Status is of the status code interalError.
func (s *Status) IsInteralError() bool {
	return s.code() == internalError
}

// IsSkip returns if a Status is of the status code Skip.
func (s *Status) IsSkip() bool {
	return s.code() == Skip
}

// IsClusterUnschedulable returns if a Status is of the status code ClusterUnschedulable.
func (s *Status) IsClusterUnschedulable() bool {
	return s.code() == ClusterUnschedulable
}

// IsClusterAlreadySelected returns if a Status is of the status code ClusterAlreadySelected.
func (s *Status) IsClusterAlreadySelected() bool {
	return s.code() == ClusterAlreadySelected
}

// Reasons returns the reasons of a Status.
func (s *Status) Reasons() []string {
	if s == nil {
		return []string{}
	}
	return s.reasons
}

// SourcePlugin returns the source plugin associated with a Status.
func (s *Status) SourcePlugin() string {
	if s == nil {
		return ""
	}
	return s.sourcePlugin
}

// InternalError returns the error associated with a Status.
func (s *Status) InternalError() error {
	if s == nil {
		return nil
	}
	return s.err
}

// String returns the description of a Status.
func (s *Status) String() string {
	if s == nil {
		return s.code().Name()
	}
	desc := []string{s.code().Name()}
	if s.err != nil {
		desc = append(desc, s.err.Error())
	}
	desc = append(desc, s.reasons...)
	return strings.Join(desc, ", ")
}

// AsError returns a status as an error; it returns nil if the status is of the internalError code.
func (s *Status) AsError() error {
	if !s.IsInteralError() {
		return nil
	}

	return fmt.Errorf("plugin %s returned an error %s", s.sourcePlugin, s.String())
}

// NewNonErrorStatus returns a Status with a non-error status code.
// To return a Status of the internalError status code, use FromError() instead.
func NewNonErrorStatus(code StatusCode, sourcePlugin string, reasons ...string) *Status {
	return &Status{
		statusCode:   code,
		reasons:      reasons,
		sourcePlugin: sourcePlugin,
	}
}

// FromError returns a Status from an error.
func FromError(err error, sourcePlugin string, reasons ...string) *Status {
	return &Status{
		statusCode:   internalError,
		reasons:      reasons,
		err:          err,
		sourcePlugin: sourcePlugin,
	}
}
