//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2021 The Kruise Authors.

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"go.goms.io/fleet/test/utils/apis/kruise/apps/pub"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BroadcastJob) DeepCopyInto(out *BroadcastJob) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BroadcastJob.
func (in *BroadcastJob) DeepCopy() *BroadcastJob {
	if in == nil {
		return nil
	}
	out := new(BroadcastJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *BroadcastJob) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BroadcastJobList) DeepCopyInto(out *BroadcastJobList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]BroadcastJob, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BroadcastJobList.
func (in *BroadcastJobList) DeepCopy() *BroadcastJobList {
	if in == nil {
		return nil
	}
	out := new(BroadcastJobList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *BroadcastJobList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BroadcastJobSpec) DeepCopyInto(out *BroadcastJobSpec) {
	*out = *in
	if in.Parallelism != nil {
		in, out := &in.Parallelism, &out.Parallelism
		*out = new(intstr.IntOrString)
		**out = **in
	}
	in.Template.DeepCopyInto(&out.Template)
	in.CompletionPolicy.DeepCopyInto(&out.CompletionPolicy)
	out.FailurePolicy = in.FailurePolicy
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BroadcastJobSpec.
func (in *BroadcastJobSpec) DeepCopy() *BroadcastJobSpec {
	if in == nil {
		return nil
	}
	out := new(BroadcastJobSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BroadcastJobStatus) DeepCopyInto(out *BroadcastJobStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]JobCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.StartTime != nil {
		in, out := &in.StartTime, &out.StartTime
		*out = (*in).DeepCopy()
	}
	if in.CompletionTime != nil {
		in, out := &in.CompletionTime, &out.CompletionTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BroadcastJobStatus.
func (in *BroadcastJobStatus) DeepCopy() *BroadcastJobStatus {
	if in == nil {
		return nil
	}
	out := new(BroadcastJobStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSet) DeepCopyInto(out *CloneSet) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSet.
func (in *CloneSet) DeepCopy() *CloneSet {
	if in == nil {
		return nil
	}
	out := new(CloneSet)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CloneSet) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSetCondition) DeepCopyInto(out *CloneSetCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSetCondition.
func (in *CloneSetCondition) DeepCopy() *CloneSetCondition {
	if in == nil {
		return nil
	}
	out := new(CloneSetCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSetList) DeepCopyInto(out *CloneSetList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CloneSet, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSetList.
func (in *CloneSetList) DeepCopy() *CloneSetList {
	if in == nil {
		return nil
	}
	out := new(CloneSetList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CloneSetList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSetScaleStrategy) DeepCopyInto(out *CloneSetScaleStrategy) {
	*out = *in
	if in.PodsToDelete != nil {
		in, out := &in.PodsToDelete, &out.PodsToDelete
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.MaxUnavailable != nil {
		in, out := &in.MaxUnavailable, &out.MaxUnavailable
		*out = new(intstr.IntOrString)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSetScaleStrategy.
func (in *CloneSetScaleStrategy) DeepCopy() *CloneSetScaleStrategy {
	if in == nil {
		return nil
	}
	out := new(CloneSetScaleStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSetSpec) DeepCopyInto(out *CloneSetSpec) {
	*out = *in
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	in.Template.DeepCopyInto(&out.Template)
	if in.VolumeClaimTemplates != nil {
		in, out := &in.VolumeClaimTemplates, &out.VolumeClaimTemplates
		*out = make([]corev1.PersistentVolumeClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.ScaleStrategy.DeepCopyInto(&out.ScaleStrategy)
	in.UpdateStrategy.DeepCopyInto(&out.UpdateStrategy)
	if in.RevisionHistoryLimit != nil {
		in, out := &in.RevisionHistoryLimit, &out.RevisionHistoryLimit
		*out = new(int32)
		**out = **in
	}
	if in.Lifecycle != nil {
		in, out := &in.Lifecycle, &out.Lifecycle
		*out = new(pub.Lifecycle)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSetSpec.
func (in *CloneSetSpec) DeepCopy() *CloneSetSpec {
	if in == nil {
		return nil
	}
	out := new(CloneSetSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSetStatus) DeepCopyInto(out *CloneSetStatus) {
	*out = *in
	if in.CollisionCount != nil {
		in, out := &in.CollisionCount, &out.CollisionCount
		*out = new(int32)
		**out = **in
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]CloneSetCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSetStatus.
func (in *CloneSetStatus) DeepCopy() *CloneSetStatus {
	if in == nil {
		return nil
	}
	out := new(CloneSetStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloneSetUpdateStrategy) DeepCopyInto(out *CloneSetUpdateStrategy) {
	*out = *in
	if in.Partition != nil {
		in, out := &in.Partition, &out.Partition
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.MaxUnavailable != nil {
		in, out := &in.MaxUnavailable, &out.MaxUnavailable
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.MaxSurge != nil {
		in, out := &in.MaxSurge, &out.MaxSurge
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.PriorityStrategy != nil {
		in, out := &in.PriorityStrategy, &out.PriorityStrategy
		*out = new(pub.UpdatePriorityStrategy)
		(*in).DeepCopyInto(*out)
	}
	if in.ScatterStrategy != nil {
		in, out := &in.ScatterStrategy, &out.ScatterStrategy
		*out = make(UpdateScatterStrategy, len(*in))
		copy(*out, *in)
	}
	if in.InPlaceUpdateStrategy != nil {
		in, out := &in.InPlaceUpdateStrategy, &out.InPlaceUpdateStrategy
		*out = new(pub.InPlaceUpdateStrategy)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloneSetUpdateStrategy.
func (in *CloneSetUpdateStrategy) DeepCopy() *CloneSetUpdateStrategy {
	if in == nil {
		return nil
	}
	out := new(CloneSetUpdateStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CompletionPolicy) DeepCopyInto(out *CompletionPolicy) {
	*out = *in
	if in.ActiveDeadlineSeconds != nil {
		in, out := &in.ActiveDeadlineSeconds, &out.ActiveDeadlineSeconds
		*out = new(int64)
		**out = **in
	}
	if in.TTLSecondsAfterFinished != nil {
		in, out := &in.TTLSecondsAfterFinished, &out.TTLSecondsAfterFinished
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CompletionPolicy.
func (in *CompletionPolicy) DeepCopy() *CompletionPolicy {
	if in == nil {
		return nil
	}
	out := new(CompletionPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *FailurePolicy) DeepCopyInto(out *FailurePolicy) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new FailurePolicy.
func (in *FailurePolicy) DeepCopy() *FailurePolicy {
	if in == nil {
		return nil
	}
	out := new(FailurePolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobCondition) DeepCopyInto(out *JobCondition) {
	*out = *in
	in.LastProbeTime.DeepCopyInto(&out.LastProbeTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobCondition.
func (in *JobCondition) DeepCopy() *JobCondition {
	if in == nil {
		return nil
	}
	out := new(JobCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in UpdateScatterStrategy) DeepCopyInto(out *UpdateScatterStrategy) {
	{
		in := &in
		*out = make(UpdateScatterStrategy, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpdateScatterStrategy.
func (in UpdateScatterStrategy) DeepCopy() UpdateScatterStrategy {
	if in == nil {
		return nil
	}
	out := new(UpdateScatterStrategy)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpdateScatterTerm) DeepCopyInto(out *UpdateScatterTerm) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpdateScatterTerm.
func (in *UpdateScatterTerm) DeepCopy() *UpdateScatterTerm {
	if in == nil {
		return nil
	}
	out := new(UpdateScatterTerm)
	in.DeepCopyInto(out)
	return out
}
