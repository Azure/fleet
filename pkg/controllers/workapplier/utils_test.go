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

package workapplier

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// TestFormatWRIString tests the formatWRIString function.
func TestFormatWRIString(t *testing.T) {
	testCases := []struct {
		name          string
		wri           *fleetv1beta1.WorkResourceIdentifier
		wantWRIString string
		wantErred     bool
	}{
		{
			name: "ordinal only",
			wri: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal: 0,
			},
			wantErred: true,
		},
		{
			name:          "regular object",
			wri:           deployWRI(2, nsName, deployName),
			wantWRIString: fmt.Sprintf("GV=apps/v1, Kind=Deployment, Namespace=%s, Name=%s", nsName, deployName),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wriString, err := formatWRIString(tc.wri)
			if tc.wantErred {
				if err == nil {
					t.Errorf("formatWRIString() = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("formatWRIString() = %v, want no error", err)
			}

			if wriString != tc.wantWRIString {
				t.Errorf("formatWRIString() mismatches: got %q, want %q", wriString, tc.wantWRIString)
			}
		})
	}
}

// TestIsPlacedByFleetInDuplicate tests the isPlacedByFleetInDuplicate function.
func TestIsPlacedByFleetInDuplicate(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{
			name: "in duplicate",
		},
		{
			name: "not in duplicate",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
		})
	}
}

// TestDiscardFieldsIrrelevantInComparisonFrom tests the discardFieldsIrrelevantInComparisonFrom function.
func TestDiscardFieldsIrrelevantInComparisonFrom(t *testing.T) {
	// This test spec uses a Deployment object as the target.
	generateName := "app-"
	dummyManager := "dummy-manager"
	now := metav1.Now()
	dummyGeneration := int64(1)
	dummyResourceVersion := "abc"
	dummySelfLink := "self-link"
	dummyUID := "123-xyz-abcd"

	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:         deployName,
			Namespace:    nsName,
			GenerateName: generateName,
			Annotations: map[string]string{
				fmt.Sprintf("%s/%s", k8sReservedLabelAnnotationFullDomain, dummyLabelKey): dummyLabelValue1,
				fmt.Sprintf("%s/%s", k8sReservedLabelAnnotationAbbrDomain, dummyLabelKey): dummyLabelValue1,
				fmt.Sprintf("%s/%s", fleetReservedLabelAnnotationDomain, dummyLabelKey):   dummyLabelValue1,
				dummyLabelKey: dummyLabelValue1,
			},
			Labels: map[string]string{
				fmt.Sprintf("%s/%s", k8sReservedLabelAnnotationFullDomain, dummyLabelKey): dummyLabelValue1,
				fmt.Sprintf("%s/%s", k8sReservedLabelAnnotationAbbrDomain, dummyLabelKey): dummyLabelValue1,
				fmt.Sprintf("%s/%s", fleetReservedLabelAnnotationDomain, dummyLabelKey):   dummyLabelValue1,
				dummyLabelKey: dummyLabelValue2,
			},
			Finalizers: []string{
				dummyLabelKey,
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:    dummyManager,
					Operation:  metav1.ManagedFieldsOperationUpdate,
					APIVersion: "v1",
					Time:       &now,
					FieldsType: "FieldsV1",
					FieldsV1:   &metav1.FieldsV1{},
				},
			},
			OwnerReferences: []metav1.OwnerReference{
				dummyOwnerRef,
			},
			CreationTimestamp:          now,
			DeletionTimestamp:          &now,
			DeletionGracePeriodSeconds: ptr.To(int64(30)),
			Generation:                 dummyGeneration,
			ResourceVersion:            dummyResourceVersion,
			SelfLink:                   dummySelfLink,
			UID:                        types.UID(dummyUID),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
		},
		Status: appsv1.DeploymentStatus{
			Replicas: 1,
		},
	}
	wantDeploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				dummyLabelKey: dummyLabelValue1,
			},
			Labels: map[string]string{
				dummyLabelKey: dummyLabelValue2,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
		},
		Status: appsv1.DeploymentStatus{},
	}

	testCases := []struct {
		name                string
		unstructuredObj     *unstructured.Unstructured
		wantUnstructuredObj *unstructured.Unstructured
	}{
		{
			name:                "deploy",
			unstructuredObj:     toUnstructured(t, deploy),
			wantUnstructuredObj: toUnstructured(t, wantDeploy),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := discardFieldsIrrelevantInComparisonFrom(tc.unstructuredObj)

			// There are certain fields that need to be set manually as they might got omitted
			// when being cast to an Unstructured object.
			tc.wantUnstructuredObj.SetFinalizers([]string{})
			tc.wantUnstructuredObj.SetCreationTimestamp(metav1.Time{})
			tc.wantUnstructuredObj.SetManagedFields([]metav1.ManagedFieldsEntry{})
			tc.wantUnstructuredObj.SetOwnerReferences([]metav1.OwnerReference{})
			unstructured.RemoveNestedField(tc.wantUnstructuredObj.Object, "status")

			if diff := cmp.Diff(got, tc.wantUnstructuredObj, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("discardFieldsIrrelevantInComparisonFrom() mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}
