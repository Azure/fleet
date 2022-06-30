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

const (
	MemberClusterKind         = "MemberCluster"
	InternalMemberClusterKind = "InternalMemberCluster"
)
