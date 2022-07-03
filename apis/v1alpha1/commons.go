/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

type ClusterState string

const (
	ClusterStateJoin  ClusterState = "Join"
	ClusterStateLeave ClusterState = "Leave"
)

type MembershipControllerState string

const (
	MembershipControllerStateNotReady     MembershipControllerState = "Not Ready"
	MembershipControllerStateReadyToJoin  MembershipControllerState = "Ready To Join"
	MembershipControllerStateReadyToLeave MembershipControllerState = "Ready To Leave"
)

type InternalMemberClusterControllerState string

const (
	InternalMemberClusterControllerStateNotReady     InternalMemberClusterControllerState = "Not Ready"
	InternalMemberClusterControllerStateReadyToJoin  InternalMemberClusterControllerState = "Ready To Join"
	InternalMemberClusterControllerStateReadyToLeave InternalMemberClusterControllerState = "Ready To Leave"
)

const (
	MemberClusterKind         = "MemberCluster"
	InternalMemberClusterKind = "InternalMemberCluster"
)
