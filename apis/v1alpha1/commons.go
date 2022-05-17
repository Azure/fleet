/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

type ClusterState string

const (
	ClusterStateJoin  ClusterState = "Join"
	ClusterStateLeave ClusterState = "Leave"
	// TestCaseMsg is used in the table driven test
	TestCaseMsg string = "\nTest case:  %s"
)
