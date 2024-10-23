/*
Copyright 2021 The Kubernetes Authors.

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

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package work

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	testingclient "k8s.io/client-go/testing"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// TestCalculateNewAppliedWork validates the calculation logic between the Work & AppliedWork resources.
// The result of the tests pass back a collection of resources that should either
// be applied to the member cluster or removed.
func TestCalculateNewAppliedWork(t *testing.T) {
	workIdentifier := generateResourceIdentifier()
	diffOrdinalIdentifier := workIdentifier
	diffOrdinalIdentifier.Ordinal = rand.Int()
	tests := map[string]struct {
		spokeDynamicClient dynamic.Interface
		inputWork          fleetv1beta1.Work
		inputAppliedWork   fleetv1beta1.AppliedWork
		expectedNewRes     []fleetv1beta1.AppliedResourceMeta
		expectedStaleRes   []fleetv1beta1.AppliedResourceMeta
		hasErr             bool
	}{
		"Test work and appliedWork in sync with no manifest applied": {
			spokeDynamicClient: nil,
			inputWork:          generateWorkObj(nil),
			inputAppliedWork:   generateAppliedWorkObj(nil),
			expectedNewRes:     []fleetv1beta1.AppliedResourceMeta(nil),
			expectedStaleRes:   []fleetv1beta1.AppliedResourceMeta(nil),
			hasErr:             false,
		},
		"Test work and appliedWork in sync with one manifest applied": {
			spokeDynamicClient: nil,
			inputWork:          generateWorkObj(&workIdentifier),
			inputAppliedWork:   generateAppliedWorkObj(&workIdentifier),
			expectedNewRes: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: workIdentifier,
				},
			},
			expectedStaleRes: []fleetv1beta1.AppliedResourceMeta(nil),
			hasErr:           false,
		},
		"Test work and appliedWork has the same resource but with different ordinal": {
			spokeDynamicClient: nil,
			inputWork:          generateWorkObj(&workIdentifier),
			inputAppliedWork:   generateAppliedWorkObj(&diffOrdinalIdentifier),
			expectedNewRes: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: workIdentifier,
				},
			},
			expectedStaleRes: []fleetv1beta1.AppliedResourceMeta(nil),
			hasErr:           false,
		},
		"Test work is missing one manifest": {
			spokeDynamicClient: nil,
			inputWork:          generateWorkObj(nil),
			inputAppliedWork:   generateAppliedWorkObj(&workIdentifier),
			expectedNewRes:     []fleetv1beta1.AppliedResourceMeta(nil),
			expectedStaleRes: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: workIdentifier,
				},
			},
			hasErr: false,
		},
		"Test work has more manifest but not applied": {
			spokeDynamicClient: nil,
			inputWork: func() fleetv1beta1.Work {
				return fleetv1beta1.Work{
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: workIdentifier,
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
					},
				}
			}(),
			inputAppliedWork: generateAppliedWorkObj(nil),
			expectedNewRes:   []fleetv1beta1.AppliedResourceMeta(nil),
			expectedStaleRes: []fleetv1beta1.AppliedResourceMeta(nil),
			hasErr:           false,
		},
		"Test work is adding one manifest, happy case": {
			spokeDynamicClient: func() *fake.FakeDynamicClient {
				uObj := unstructured.Unstructured{}
				uObj.SetUID(types.UID(rand.String(10)))
				dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
				dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, uObj.DeepCopy(), nil
				})
				return dynamicClient
			}(),
			inputWork:        generateWorkObj(&workIdentifier),
			inputAppliedWork: generateAppliedWorkObj(nil),
			expectedNewRes: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: workIdentifier,
				},
			},
			expectedStaleRes: []fleetv1beta1.AppliedResourceMeta(nil),
			hasErr:           false,
		},
		"Test work is adding one manifest but not found on the member cluster": {
			spokeDynamicClient: func() *fake.FakeDynamicClient {
				dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
				dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, &apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReasonNotFound,
						}}
				})
				return dynamicClient
			}(),
			inputWork:        generateWorkObj(&workIdentifier),
			inputAppliedWork: generateAppliedWorkObj(nil),
			expectedNewRes:   []fleetv1beta1.AppliedResourceMeta(nil),
			expectedStaleRes: []fleetv1beta1.AppliedResourceMeta(nil),
			hasErr:           false,
		},
		"Test work is adding one manifest but failed to get it on the member cluster": {
			spokeDynamicClient: func() *fake.FakeDynamicClient {
				dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
				dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("get failed")
				})
				return dynamicClient
			}(),
			inputWork:        generateWorkObj(&workIdentifier),
			inputAppliedWork: generateAppliedWorkObj(nil),
			expectedNewRes:   nil,
			expectedStaleRes: nil,
			hasErr:           true,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			r := &ApplyWorkReconciler{
				spokeDynamicClient: tt.spokeDynamicClient,
			}
			newRes, staleRes, err := r.generateDiff(context.Background(), &tt.inputWork, &tt.inputAppliedWork)
			if len(tt.expectedNewRes) != len(newRes) {
				t.Errorf("Testcase %s: get newRes contains different number of elements than the want newRes.", testName)
			}
			for i := 0; i < len(newRes); i++ {
				diff := cmp.Diff(tt.expectedNewRes[i].WorkResourceIdentifier, newRes[i].WorkResourceIdentifier)
				if len(diff) != 0 {
					t.Errorf("Testcase %s: get newRes is different from the want newRes, diff = %s", testName, diff)
				}
			}
			if len(tt.expectedStaleRes) != len(staleRes) {
				t.Errorf("Testcase %s: get staleRes contains different number of elements than the want staleRes.", testName)
			}
			for i := 0; i < len(staleRes); i++ {
				diff := cmp.Diff(tt.expectedStaleRes[i].WorkResourceIdentifier, staleRes[i].WorkResourceIdentifier)
				if len(diff) != 0 {
					t.Errorf("Testcase %s: get staleRes is different from the want staleRes, diff = %s", testName, diff)
				}
			}
			if tt.hasErr {
				assert.Truef(t, err != nil, "Testcase %s: Should get an err.", testName)
			}
		})
	}
}

func TestDeleteStaleManifest(t *testing.T) {
	tests := map[string]struct {
		spokeDynamicClient dynamic.Interface
		staleManifests     []fleetv1beta1.AppliedResourceMeta
		owner              metav1.OwnerReference
		wantErr            error
	}{
		"test staled manifests  already deleted": {
			spokeDynamicClient: func() *fake.FakeDynamicClient {
				dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
				dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, &apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReasonNotFound,
						}}
				})
				return dynamicClient
			}(),
			staleManifests: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Name: "does not matter 1",
					},
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Name: "does not matter 2",
					},
				},
			},
			owner: metav1.OwnerReference{
				APIVersion: "does not matter",
			},
			wantErr: nil,
		},
		"test failed to get staled manifest": {
			spokeDynamicClient: func() *fake.FakeDynamicClient {
				dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
				dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("get failed")
				})
				return dynamicClient
			}(),
			staleManifests: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Name: "does not matter",
					},
				},
			},
			owner: metav1.OwnerReference{
				APIVersion: "does not matter",
			},
			wantErr: utilerrors.NewAggregate([]error{fmt.Errorf("get failed")}),
		},
		"test not remove a staled manifest that work does not own": {
			spokeDynamicClient: func() *fake.FakeDynamicClient {
				uObj := unstructured.Unstructured{}
				uObj.SetOwnerReferences([]metav1.OwnerReference{
					{
						APIVersion: "not owned by work",
					},
				})
				dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
				dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, uObj.DeepCopy(), nil
				})
				dynamicClient.PrependReactor("delete", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("should not call")
				})
				return dynamicClient
			}(),
			staleManifests: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Name: "does not matter",
					},
				},
			},
			owner: metav1.OwnerReference{
				APIVersion: "does not match",
			},
			wantErr: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := &ApplyWorkReconciler{
				spokeDynamicClient: tt.spokeDynamicClient,
			}
			gotErr := r.deleteStaleManifest(context.Background(), tt.staleManifests, tt.owner)
			if tt.wantErr == nil {
				if gotErr != nil {
					t.Errorf("test case `%s` didn't return the expected error,  want no error, got error = %+v ", name, gotErr)
				}
			} else if gotErr == nil || gotErr.Error() != tt.wantErr.Error() {
				t.Errorf("test case `%s` didn't return the expected error, want error = %+v, got error = %+v", name, tt.wantErr, gotErr)
			}
		})
	}
}

func generateWorkObj(identifier *fleetv1beta1.WorkResourceIdentifier) fleetv1beta1.Work {
	if identifier != nil {
		return fleetv1beta1.Work{
			Status: fleetv1beta1.WorkStatus{
				ManifestConditions: []fleetv1beta1.ManifestCondition{
					{
						Identifier: *identifier,
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		}
	}
	return fleetv1beta1.Work{}
}

func generateAppliedWorkObj(identifier *fleetv1beta1.WorkResourceIdentifier) fleetv1beta1.AppliedWork {
	if identifier != nil {
		return fleetv1beta1.AppliedWork{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       fleetv1beta1.AppliedWorkSpec{},
			Status: fleetv1beta1.AppliedWorkStatus{
				AppliedResources: []fleetv1beta1.AppliedResourceMeta{
					{
						WorkResourceIdentifier: *identifier,
						UID:                    types.UID(rand.String(20)),
					},
				},
			},
		}
	}
	return fleetv1beta1.AppliedWork{}
}

func generateResourceIdentifier() fleetv1beta1.WorkResourceIdentifier {
	return fleetv1beta1.WorkResourceIdentifier{
		Ordinal:   rand.Int(),
		Group:     rand.String(10),
		Version:   rand.String(10),
		Kind:      rand.String(10),
		Resource:  rand.String(10),
		Namespace: rand.String(10),
		Name:      rand.String(10),
	}
}
