//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Affinity) DeepCopyInto(out *Affinity) {
	*out = *in
	if in.ClusterAffinity != nil {
		in, out := &in.ClusterAffinity, &out.ClusterAffinity
		*out = new(ClusterAffinity)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Affinity.
func (in *Affinity) DeepCopy() *Affinity {
	if in == nil {
		return nil
	}
	out := new(Affinity)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AgentStatus) DeepCopyInto(out *AgentStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.LastReceivedHeartbeat.DeepCopyInto(&out.LastReceivedHeartbeat)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AgentStatus.
func (in *AgentStatus) DeepCopy() *AgentStatus {
	if in == nil {
		return nil
	}
	out := new(AgentStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterAffinity) DeepCopyInto(out *ClusterAffinity) {
	*out = *in
	if in.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		in, out := &in.RequiredDuringSchedulingIgnoredDuringExecution, &out.RequiredDuringSchedulingIgnoredDuringExecution
		*out = new(ClusterSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.PreferredDuringSchedulingIgnoredDuringExecution != nil {
		in, out := &in.PreferredDuringSchedulingIgnoredDuringExecution, &out.PreferredDuringSchedulingIgnoredDuringExecution
		*out = make([]PreferredClusterSelector, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterAffinity.
func (in *ClusterAffinity) DeepCopy() *ClusterAffinity {
	if in == nil {
		return nil
	}
	out := new(ClusterAffinity)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterDecision) DeepCopyInto(out *ClusterDecision) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterDecision.
func (in *ClusterDecision) DeepCopy() *ClusterDecision {
	if in == nil {
		return nil
	}
	out := new(ClusterDecision)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourcePlacement) DeepCopyInto(out *ClusterResourcePlacement) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourcePlacement.
func (in *ClusterResourcePlacement) DeepCopy() *ClusterResourcePlacement {
	if in == nil {
		return nil
	}
	out := new(ClusterResourcePlacement)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterResourcePlacement) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourcePlacementList) DeepCopyInto(out *ClusterResourcePlacementList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClusterResourcePlacement, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourcePlacementList.
func (in *ClusterResourcePlacementList) DeepCopy() *ClusterResourcePlacementList {
	if in == nil {
		return nil
	}
	out := new(ClusterResourcePlacementList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterResourcePlacementList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourcePlacementSpec) DeepCopyInto(out *ClusterResourcePlacementSpec) {
	*out = *in
	if in.ResourceSelectors != nil {
		in, out := &in.ResourceSelectors, &out.ResourceSelectors
		*out = make([]ClusterResourceSelector, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Policy != nil {
		in, out := &in.Policy, &out.Policy
		*out = new(PlacementPolicy)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourcePlacementSpec.
func (in *ClusterResourcePlacementSpec) DeepCopy() *ClusterResourcePlacementSpec {
	if in == nil {
		return nil
	}
	out := new(ClusterResourcePlacementSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourcePlacementStatus) DeepCopyInto(out *ClusterResourcePlacementStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.SelectedResources != nil {
		in, out := &in.SelectedResources, &out.SelectedResources
		*out = make([]ResourceIdentifier, len(*in))
		copy(*out, *in)
	}
	if in.TargetClusters != nil {
		in, out := &in.TargetClusters, &out.TargetClusters
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FailedResourcePlacements != nil {
		in, out := &in.FailedResourcePlacements, &out.FailedResourcePlacements
		*out = make([]FailedResourcePlacement, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourcePlacementStatus.
func (in *ClusterResourcePlacementStatus) DeepCopy() *ClusterResourcePlacementStatus {
	if in == nil {
		return nil
	}
	out := new(ClusterResourcePlacementStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourceSelector) DeepCopyInto(out *ClusterResourceSelector) {
	*out = *in
	if in.LabelSelector != nil {
		in, out := &in.LabelSelector, &out.LabelSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourceSelector.
func (in *ClusterResourceSelector) DeepCopy() *ClusterResourceSelector {
	if in == nil {
		return nil
	}
	out := new(ClusterResourceSelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterSelector) DeepCopyInto(out *ClusterSelector) {
	*out = *in
	if in.ClusterSelectorTerms != nil {
		in, out := &in.ClusterSelectorTerms, &out.ClusterSelectorTerms
		*out = make([]ClusterSelectorTerm, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterSelector.
func (in *ClusterSelector) DeepCopy() *ClusterSelector {
	if in == nil {
		return nil
	}
	out := new(ClusterSelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterSelectorTerm) DeepCopyInto(out *ClusterSelectorTerm) {
	*out = *in
	in.LabelSelector.DeepCopyInto(&out.LabelSelector)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterSelectorTerm.
func (in *ClusterSelectorTerm) DeepCopy() *ClusterSelectorTerm {
	if in == nil {
		return nil
	}
	out := new(ClusterSelectorTerm)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *FailedResourcePlacement) DeepCopyInto(out *FailedResourcePlacement) {
	*out = *in
	out.ResourceIdentifier = in.ResourceIdentifier
	in.Condition.DeepCopyInto(&out.Condition)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new FailedResourcePlacement.
func (in *FailedResourcePlacement) DeepCopy() *FailedResourcePlacement {
	if in == nil {
		return nil
	}
	out := new(FailedResourcePlacement)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InternalMemberCluster) DeepCopyInto(out *InternalMemberCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InternalMemberCluster.
func (in *InternalMemberCluster) DeepCopy() *InternalMemberCluster {
	if in == nil {
		return nil
	}
	out := new(InternalMemberCluster)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *InternalMemberCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InternalMemberClusterList) DeepCopyInto(out *InternalMemberClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]InternalMemberCluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InternalMemberClusterList.
func (in *InternalMemberClusterList) DeepCopy() *InternalMemberClusterList {
	if in == nil {
		return nil
	}
	out := new(InternalMemberClusterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *InternalMemberClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InternalMemberClusterSpec) DeepCopyInto(out *InternalMemberClusterSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InternalMemberClusterSpec.
func (in *InternalMemberClusterSpec) DeepCopy() *InternalMemberClusterSpec {
	if in == nil {
		return nil
	}
	out := new(InternalMemberClusterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InternalMemberClusterStatus) DeepCopyInto(out *InternalMemberClusterStatus) {
	*out = *in
	in.ResourceUsage.DeepCopyInto(&out.ResourceUsage)
	if in.AgentStatus != nil {
		in, out := &in.AgentStatus, &out.AgentStatus
		*out = make([]AgentStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InternalMemberClusterStatus.
func (in *InternalMemberClusterStatus) DeepCopy() *InternalMemberClusterStatus {
	if in == nil {
		return nil
	}
	out := new(InternalMemberClusterStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MemberCluster) DeepCopyInto(out *MemberCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MemberCluster.
func (in *MemberCluster) DeepCopy() *MemberCluster {
	if in == nil {
		return nil
	}
	out := new(MemberCluster)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MemberCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MemberClusterList) DeepCopyInto(out *MemberClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MemberCluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MemberClusterList.
func (in *MemberClusterList) DeepCopy() *MemberClusterList {
	if in == nil {
		return nil
	}
	out := new(MemberClusterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MemberClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MemberClusterSpec) DeepCopyInto(out *MemberClusterSpec) {
	*out = *in
	out.Identity = in.Identity
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MemberClusterSpec.
func (in *MemberClusterSpec) DeepCopy() *MemberClusterSpec {
	if in == nil {
		return nil
	}
	out := new(MemberClusterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MemberClusterStatus) DeepCopyInto(out *MemberClusterStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.ResourceUsage.DeepCopyInto(&out.ResourceUsage)
	if in.AgentStatus != nil {
		in, out := &in.AgentStatus, &out.AgentStatus
		*out = make([]AgentStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MemberClusterStatus.
func (in *MemberClusterStatus) DeepCopy() *MemberClusterStatus {
	if in == nil {
		return nil
	}
	out := new(MemberClusterStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlacementPolicy) DeepCopyInto(out *PlacementPolicy) {
	*out = *in
	if in.ClusterNames != nil {
		in, out := &in.ClusterNames, &out.ClusterNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.NumberOfClusters != nil {
		in, out := &in.NumberOfClusters, &out.NumberOfClusters
		*out = new(int32)
		**out = **in
	}
	if in.Strategy != nil {
		in, out := &in.Strategy, &out.Strategy
		*out = new(RolloutStrategy)
		**out = **in
	}
	if in.Affinity != nil {
		in, out := &in.Affinity, &out.Affinity
		*out = new(Affinity)
		(*in).DeepCopyInto(*out)
	}
	if in.TopologySpreadConstraints != nil {
		in, out := &in.TopologySpreadConstraints, &out.TopologySpreadConstraints
		*out = make([]TopologySpreadConstraint, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlacementPolicy.
func (in *PlacementPolicy) DeepCopy() *PlacementPolicy {
	if in == nil {
		return nil
	}
	out := new(PlacementPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterPolicySnapShotList) DeepCopyInto(out *ClusterPolicySnapShotList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClusterPolicySnapshot, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterPolicySnapShotList.
func (in *ClusterPolicySnapShotList) DeepCopy() *ClusterPolicySnapShotList {
	if in == nil {
		return nil
	}
	out := new(ClusterPolicySnapShotList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterPolicySnapShotList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PolicySnapShotSpec) DeepCopyInto(out *PolicySnapShotSpec) {
	*out = *in
	if in.Policy != nil {
		in, out := &in.Policy, &out.Policy
		*out = new(PlacementPolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.PolicyHash != nil {
		in, out := &in.PolicyHash, &out.PolicyHash
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PolicySnapShotSpec.
func (in *PolicySnapShotSpec) DeepCopy() *PolicySnapShotSpec {
	if in == nil {
		return nil
	}
	out := new(PolicySnapShotSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PolicySnapShotStatus) DeepCopyInto(out *PolicySnapShotStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ClusterDecisions != nil {
		in, out := &in.ClusterDecisions, &out.ClusterDecisions
		*out = make([]ClusterDecision, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PolicySnapShotStatus.
func (in *PolicySnapShotStatus) DeepCopy() *PolicySnapShotStatus {
	if in == nil {
		return nil
	}
	out := new(PolicySnapShotStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterPolicySnapshot) DeepCopyInto(out *ClusterPolicySnapshot) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterPolicySnapshot.
func (in *ClusterPolicySnapshot) DeepCopy() *ClusterPolicySnapshot {
	if in == nil {
		return nil
	}
	out := new(ClusterPolicySnapshot)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterPolicySnapshot) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreferredClusterSelector) DeepCopyInto(out *PreferredClusterSelector) {
	*out = *in
	in.Preference.DeepCopyInto(&out.Preference)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreferredClusterSelector.
func (in *PreferredClusterSelector) DeepCopy() *PreferredClusterSelector {
	if in == nil {
		return nil
	}
	out := new(PreferredClusterSelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourceBinding) DeepCopyInto(out *ClusterResourceBinding) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourceBinding.
func (in *ClusterResourceBinding) DeepCopy() *ClusterResourceBinding {
	if in == nil {
		return nil
	}
	out := new(ClusterResourceBinding)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterResourceBinding) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourceBindingList) DeepCopyInto(out *ClusterResourceBindingList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClusterResourceBinding, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourceBindingList.
func (in *ClusterResourceBindingList) DeepCopy() *ClusterResourceBindingList {
	if in == nil {
		return nil
	}
	out := new(ClusterResourceBindingList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceBindingSpec) DeepCopyInto(out *ResourceBindingSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceBindingSpec.
func (in *ResourceBindingSpec) DeepCopy() *ResourceBindingSpec {
	if in == nil {
		return nil
	}
	out := new(ResourceBindingSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceBindingStatus) DeepCopyInto(out *ResourceBindingStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceBindingStatus.
func (in *ResourceBindingStatus) DeepCopy() *ResourceBindingStatus {
	if in == nil {
		return nil
	}
	out := new(ResourceBindingStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceContent) DeepCopyInto(out *ResourceContent) {
	*out = *in
	in.Unknown.DeepCopyInto(&out.Unknown)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceContent.
func (in *ResourceContent) DeepCopy() *ResourceContent {
	if in == nil {
		return nil
	}
	out := new(ResourceContent)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceIdentifier) DeepCopyInto(out *ResourceIdentifier) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceIdentifier.
func (in *ResourceIdentifier) DeepCopy() *ResourceIdentifier {
	if in == nil {
		return nil
	}
	out := new(ResourceIdentifier)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourceSnapShotList) DeepCopyInto(out *ClusterResourceSnapShotList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClusterResourceSnapshot, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourceSnapShotList.
func (in *ClusterResourceSnapShotList) DeepCopy() *ClusterResourceSnapShotList {
	if in == nil {
		return nil
	}
	out := new(ClusterResourceSnapShotList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterResourceSnapShotList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceSnapShotSpec) DeepCopyInto(out *ResourceSnapShotSpec) {
	*out = *in
	if in.SelectedResources != nil {
		in, out := &in.SelectedResources, &out.SelectedResources
		*out = make([]ResourceContent, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceSnapShotSpec.
func (in *ResourceSnapShotSpec) DeepCopy() *ResourceSnapShotSpec {
	if in == nil {
		return nil
	}
	out := new(ResourceSnapShotSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceSnapShotStatus) DeepCopyInto(out *ResourceSnapShotStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceSnapShotStatus.
func (in *ResourceSnapShotStatus) DeepCopy() *ResourceSnapShotStatus {
	if in == nil {
		return nil
	}
	out := new(ResourceSnapShotStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterResourceSnapshot) DeepCopyInto(out *ClusterResourceSnapshot) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterResourceSnapshot.
func (in *ClusterResourceSnapshot) DeepCopy() *ClusterResourceSnapshot {
	if in == nil {
		return nil
	}
	out := new(ClusterResourceSnapshot)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterResourceSnapshot) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceUsage) DeepCopyInto(out *ResourceUsage) {
	*out = *in
	if in.Capacity != nil {
		in, out := &in.Capacity, &out.Capacity
		*out = make(corev1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.Allocatable != nil {
		in, out := &in.Allocatable, &out.Allocatable
		*out = make(corev1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	in.ObservationTime.DeepCopyInto(&out.ObservationTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceUsage.
func (in *ResourceUsage) DeepCopy() *ResourceUsage {
	if in == nil {
		return nil
	}
	out := new(ResourceUsage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RolloutStrategy) DeepCopyInto(out *RolloutStrategy) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RolloutStrategy.
func (in *RolloutStrategy) DeepCopy() *RolloutStrategy {
	if in == nil {
		return nil
	}
	out := new(RolloutStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TopologySpreadConstraint) DeepCopyInto(out *TopologySpreadConstraint) {
	*out = *in
	if in.MaxSkew != nil {
		in, out := &in.MaxSkew, &out.MaxSkew
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TopologySpreadConstraint.
func (in *TopologySpreadConstraint) DeepCopy() *TopologySpreadConstraint {
	if in == nil {
		return nil
	}
	out := new(TopologySpreadConstraint)
	in.DeepCopyInto(out)
	return out
}
