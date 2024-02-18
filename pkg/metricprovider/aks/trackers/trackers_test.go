/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trackers

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	nodeName1 = "node-1"
	nodeName2 = "node-2"
	nodeName3 = "node-3"

	namespaceName1 = "namespace-1"
	namespaceName2 = "namespace-2"
	namespaceName3 = "namespace-3"

	podName1 = "pod-1"
	podName2 = "pod-2"
	podName3 = "pod-3"

	containerName1 = "container-1"
	containerName2 = "container-2"
	containerName3 = "container-3"
)

var (
	ignoreNodeTrackerMutexField = cmpopts.IgnoreFields(NodeTracker{}, "mu")
	ignorePodTrackerMutexField  = cmpopts.IgnoreFields(PodTracker{}, "mu")
)

// TestNodeTrackerAddOrUpdateNode tests the AddOrUpdateNode method of the NodeTracker.
func TestNodeTrackerAddOrUpdateNode(t *testing.T) {
	testCases := []struct {
		name   string
		nt     *NodeTracker
		nodes  []*corev1.Node
		wantNT *NodeTracker
	}{
		{
			name: "can track a single node",
			nt:   NewNodeTracker(),
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("15Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("15Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("15Gi"),
					},
				},
			},
		},
		{
			name: "can track multiple nodes",
			nt:   NewNodeTracker(),
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName2,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("12Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("10Gi"),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName3,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("15Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("9"),
					corev1.ResourceMemory: resource.MustParse("36Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("29Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					nodeName2: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("10Gi"),
					},
					nodeName3: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("15Gi"),
					},
				},
			},
		},
		{
			name: "can update existing node with no total/allocatable capacity change",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
		},
		{
			name: "can update existing node with total/allocatable capacity change",
			nt: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("12Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("11Gi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("11Gi"),
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("11Gi"),
					},
				},
			},
		},
		{
			name: "can track a node with no total/allocatable capacity for supported resources",
			nt:   NewNodeTracker(),
			nodes: []*corev1.Node{
				// Note that this is a case that should not happen in practice.
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nodeName1,
					},
					Spec: corev1.NodeSpec{},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("800Mi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("700Mi"),
						},
					},
				},
			},
			wantNT: &NodeTracker{
				totalCapacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				totalAllocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				capacityByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
				allocatableByNode: map[string]corev1.ResourceList{
					nodeName1: {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.nodes {
				tc.nt.AddOrUpdate(n)
			}

			if diff := cmp.Diff(
				tc.nt, tc.wantNT,
				cmp.AllowUnexported(NodeTracker{}),
				ignoreNodeTrackerMutexField,
			); diff != "" {
				t.Fatalf("AddOrUpdateNode(), node tracker diff (-got, +want): \n%s", diff)
			}
		})
	}
}

// TestNodeTrackerAddOrUpdate tests the AddOrUpdate method of the PodTracker.
func TestPodTrackerAddOrUpdate(t *testing.T) {
	testCases := []struct {
		name   string
		pt     *PodTracker
		pods   []*corev1.Pod
		wantPT *PodTracker
	}{
		{
			name: "can track a pod with a single container",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
		},
		{
			name: "can track a pod with multiple containers",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("500Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("700Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("700Mi"),
					},
				},
			},
		},
		{
			name: "can track multiple pods",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("50Mi"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName2,
						Namespace: namespaceName2,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName3,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName3,
						Namespace: namespaceName3,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1.5"),
										corev1.ResourceMemory: resource.MustParse("600Mi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2.5"),
										corev1.ResourceMemory: resource.MustParse("400Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: *resource.NewQuantity(2384461824, resource.BinarySI),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("3"),
						corev1.ResourceMemory: resource.MustParse("250Mi"),
					},
					fmt.Sprintf("%s/%s", namespaceName2, podName2): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					fmt.Sprintf("%s/%s", namespaceName3, podName3): {
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("1000Mi"),
					},
				},
			},
		},
		{
			name: "can update existing pod with requested capacity change",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("400Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("400Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("400Mi"),
					},
				},
			},
		},
		{
			name: "can update existing pod with no requested capacity change",
			pt: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
			},
		},
		{
			name: "can track a pod with no requested capacity for supported resources",
			pt:   NewPodTracker(),
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName1,
						Namespace: namespaceName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("800Mi"),
									},
								},
							},
						},
					},
				},
			},
			wantPT: &PodTracker{
				totalRequested: corev1.ResourceList{
					corev1.ResourceCPU:    resource.Quantity{},
					corev1.ResourceMemory: resource.Quantity{},
				},
				requestedByPod: map[string]corev1.ResourceList{
					fmt.Sprintf("%s/%s", namespaceName1, podName1): {
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range tc.pods {
				tc.pt.AddOrUpdate(p)
			}

			if diff := cmp.Diff(
				tc.pt, tc.wantPT,
				cmp.AllowUnexported(PodTracker{}),
				ignorePodTrackerMutexField,
			); diff != "" {
				t.Fatalf("AddOrUpdatePod(), pod tracker diff (-got, +want): \n%s", diff)
			}
		})
	}
}
