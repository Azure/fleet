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

package work

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	testingclient "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
)

func TestApplyUnstructured(t *testing.T) {
	tests := []struct {
		name             string
		allowCoOwnership bool
		manifest         *unstructured.Unstructured
		owners           []metav1.OwnerReference
		doesExist        bool // return whether the deployment exists
		works            []placementv1beta1.Work
		wantApplyAction  ApplyAction
		wantErr          error
	}{
		{
			name: "the deployment has a generated name",
			manifest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"namespace":    "test-namespace",
						"generateName": "Test",
					},
				},
			},
			wantApplyAction: manifestServerSideAppliedAction,
		},
		{
			name: "the deployment does not exist",
			manifest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"namespace": "test-namespace",
						"name":      "test",
					},
				},
			},
			wantApplyAction: manifestServerSideAppliedAction,
		},
		{
			name: "the deployment exists and has conflicts with other work",
			manifest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"namespace": "test-namespace",
						"name":      "test",
					},
				},
			},
			owners: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       "another-type",
					Name:       "work1",
				},
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			doesExist: true,
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
								ForceConflicts: true,
							},
						},
					},
				},
			},
			wantApplyAction: applyConflictBetweenPlacements,
			wantErr:         controller.ErrUserError,
		},
		{
			name: "the deployment exists and has no conflicts with other work",
			manifest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"namespace": "test-namespace",
						"name":      "test",
					},
				},
			},
			owners: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       placementv1beta1.AppliedWorkKind,
					Name:       "work2",
				},
			},
			doesExist: true,
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testWorkNamespace,
						Name:      "work2",
					},
					Spec: placementv1beta1.WorkSpec{
						ApplyStrategy: &placementv1beta1.ApplyStrategy{
							Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
							ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
								ForceConflicts: false,
							},
						},
					},
				},
			},
			wantApplyAction: manifestServerSideAppliedAction,
		},
		{
			name:             "the deployment exists and is owned by other non-work resource (allow co-ownership)",
			allowCoOwnership: true,
			manifest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"namespace": "test-namespace",
						"name":      "test",
					},
				},
			},
			owners: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       "another-type",
					Name:       "work1",
				},
			},
			doesExist:       true,
			wantApplyAction: manifestServerSideAppliedAction,
		},
		{
			name: "the deployment exists and is owned by other non-work resource (disallow co-ownership)",
			manifest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"namespace": "test-namespace",
						"name":      "test",
					},
				},
			},
			owners: []metav1.OwnerReference{
				{
					APIVersion: placementv1beta1.GroupVersion.String(),
					Kind:       "another-type",
					Name:       "work1",
				},
			},
			doesExist:       true,
			wantApplyAction: manifestAlreadyOwnedByOthers,
			wantErr:         controller.ErrUserError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

			// The fake client does not support PatchType ApplyPatchType
			// see issue: https://github.com/kubernetes/kubernetes/issues/103816
			// always return true for the patch action
			dynamicClient.PrependReactor("patch", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
				return true, tc.manifest.DeepCopy(), nil
			})
			dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
				if tc.doesExist {
					res := tc.manifest.DeepCopy()
					res.SetOwnerReferences(tc.owners)
					return true, res, nil
				}
				return true, nil, &apierrors.StatusError{
					ErrStatus: metav1.Status{
						Status: metav1.StatusFailure,
						Reason: metav1.StatusReasonNotFound,
					}}
			})

			var objects []client.Object
			for i := range tc.works {
				objects = append(objects, &tc.works[i])
			}
			scheme := serviceScheme(t)
			hubClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			applier := &ServerSideApplier{
				SpokeDynamicClient: dynamicClient,
				HubClient:          hubClient,
				WorkNamespace:      testWorkNamespace,
			}
			ctx := context.Background()
			applyStrategy := &placementv1beta1.ApplyStrategy{
				Type: placementv1beta1.ApplyStrategyTypeServerSideApply,
				ServerSideApplyConfig: &placementv1beta1.ServerSideApplyConfig{
					ForceConflicts: false,
				},
				AllowCoOwnership: tc.allowCoOwnership,
			}
			// Certain path of the ApplyUnstructured method will attempt
			// to set default values of the retrieved apply strategy from the mock Work object and
			// compare it with the passed-in apply strategy.
			// To keep things consistent, here the test spec sets the passed-in apply strategy
			// as well.
			defaulter.SetDefaultsApplyStrategy(applyStrategy)
			gvr := schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "Deployment",
			}

			// We don't check the returned unstructured object because the fake client always return the same object we pass in.
			_, gotApplyAction, err := applier.ApplyUnstructured(ctx, applyStrategy, gvr, tc.manifest)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ApplyUnstructured() got error %v, want error %v", err, tc.wantErr)
			}
			// no matter error or not, we should check the apply action
			if gotApplyAction != tc.wantApplyAction {
				t.Errorf("ApplyUnstructured() got apply action %v, want apply action %v", gotApplyAction, tc.wantApplyAction)
			}
		})
	}
}
