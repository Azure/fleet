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

package workgenerator

// Verify that all formatter implements the derivedFromSourceFormatter interface.
var _ derivedFromSourceFormatter = &placementResourceSnapshotDerivedFromSourceFormatter{}

// derivedFromSourceFormatter is an interface that helps format the ID of a source that derives a work object for
// various use cases, primarily for preparing unique names for work objects.
type derivedFromSourceFormatter interface {
	// StrictDNSLabel returns a string that is a valid DNS label (max. 63 chars, all lowercase, alphanumeric characters
	// and hyphens, and must start and end with an alphanumeric character).
	//
	// This value is used as a sub-component of the unique name for a work object derived from a source. It is
	// for informational purposes only and does not need to be unique across all work objects that are created/updated
	// for the same placement binding.
	StrictDNSLabel() string
	// SourceType returns a string that identifies the type of the source that derives a work object, e.g.,
	// `placement-resource-snapshot` for placement resource snapshots.
	SourceType() string
	// SourceID returns a string that uniquely identifies the source (of the same type) that derives a
	// work object.
	SourceID() string
}

// placementResourceSnapshotDerivedFromSourceFormatter is a formatter for placement resource snapshots that implements
// the derivedFromSourceFormatter interface.
type placementResourceSnapshotDerivedFromSourceFormatter struct {
	snapshotSubIdx string
}

func (f *placementResourceSnapshotDerivedFromSourceFormatter) SourceID() string {
	return f.snapshotSubIdx
}

func (f *placementResourceSnapshotDerivedFromSourceFormatter) SourceType() string {
	return "placement-resource-snapshot"
}

func (f *placementResourceSnapshotDerivedFromSourceFormatter) StrictDNSLabel() string {
	return f.snapshotSubIdx
}
