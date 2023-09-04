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

package workv1alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	"go.uber.org/atomic"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	testingclient "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/pkg/utils"
)

var (
	fakeDynamicClient = fake.NewSimpleDynamicClient(runtime.NewScheme())
	ownerRef          = metav1.OwnerReference{
		APIVersion: workv1alpha1.GroupVersion.String(),
		Kind:       "AppliedWork",
	}
	testGvr = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}
	testDeployment = appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Spec: appsv1.DeploymentSpec{
			MinReadySeconds: 5,
		},
	}
	rawTestDeployment, _ = json.Marshal(testDeployment)
	testManifest         = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawTestDeployment,
	}}
)

// This interface is needed for testMapper abstract class.
type testMapper struct {
	meta.RESTMapper
}

func (m testMapper) RESTMapping(gk schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	if gk.Kind == "Deployment" {
		return &meta.RESTMapping{
			Resource:         testGvr,
			GroupVersionKind: testDeployment.GroupVersionKind(),
			Scope:            nil,
		}, nil
	}
	return nil, errors.New("test error: mapping does not exist")
}

func TestSetManifestHashAnnotation(t *testing.T) {
	// basic setup
	manifestObj := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: utilrand.String(10),
					Kind:       utilrand.String(10),
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
			},
			Annotations: map[string]string{utilrand.String(10): utilrand.String(10)},
		},
		Spec: appsv1.DeploymentSpec{
			Paused: true,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
		},
	}
	// pre-compute the hash
	preObj := manifestObj.DeepCopy()
	var uPreObj unstructured.Unstructured
	uPreObj.Object, _ = runtime.DefaultUnstructuredConverter.ToUnstructured(preObj)
	preHash, _ := computeManifestHash(&uPreObj)

	tests := map[string]struct {
		manifestObj interface{}
		isSame      bool
	}{
		"manifest same, same": {
			manifestObj: func() *appsv1.Deployment {
				extraObj := manifestObj.DeepCopy()
				return extraObj
			}(),
			isSame: true,
		},
		"manifest status changed, same": {
			manifestObj: func() *appsv1.Deployment {
				extraObj := manifestObj.DeepCopy()
				extraObj.Status.ReadyReplicas = 10
				return extraObj
			}(),
			isSame: true,
		},
		"manifest's has hashAnnotation, same": {
			manifestObj: func() *appsv1.Deployment {
				alterObj := manifestObj.DeepCopy()
				alterObj.Annotations[manifestHashAnnotation] = utilrand.String(10)
				return alterObj
			}(),
			isSame: true,
		},
		"manifest has extra metadata, same": {
			manifestObj: func() *appsv1.Deployment {
				noObj := manifestObj.DeepCopy()
				noObj.SetSelfLink(utilrand.String(2))
				noObj.SetResourceVersion(utilrand.String(4))
				noObj.SetGeneration(3)
				noObj.SetUID(types.UID(utilrand.String(3)))
				noObj.SetCreationTimestamp(metav1.Now())
				return noObj
			}(),
			isSame: true,
		},
		"manifest has a new appliedWork ownership, need update": {
			manifestObj: func() *appsv1.Deployment {
				alterObj := manifestObj.DeepCopy()
				alterObj.OwnerReferences[0].APIVersion = workv1alpha1.GroupVersion.String()
				alterObj.OwnerReferences[0].Kind = workv1alpha1.AppliedWorkKind
				return alterObj
			}(),
			isSame: false,
		},
		"manifest is has changed ownership, need update": {
			manifestObj: func() *appsv1.Deployment {
				alterObj := manifestObj.DeepCopy()
				alterObj.OwnerReferences[0].APIVersion = utilrand.String(10)
				return alterObj
			}(),
			isSame: false,
		},
		"manifest has a different label, need update": {
			manifestObj: func() *appsv1.Deployment {
				alterObj := manifestObj.DeepCopy()
				alterObj.SetLabels(map[string]string{utilrand.String(5): utilrand.String(10)})
				return alterObj
			}(),
			isSame: false,
		},
		"manifest has a different annotation, need update": {
			manifestObj: func() *appsv1.Deployment {
				alterObj := manifestObj.DeepCopy()
				alterObj.SetAnnotations(map[string]string{utilrand.String(5): utilrand.String(10)})
				return alterObj
			}(),
			isSame: false,
		},
		"manifest has a different spec, need update": {
			manifestObj: func() *appsv1.Deployment {
				alterObj := manifestObj.DeepCopy()
				alterObj.Spec.Replicas = pointer.Int32(100)
				return alterObj
			}(),
			isSame: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var uManifestObj unstructured.Unstructured
			uManifestObj.Object, _ = runtime.DefaultUnstructuredConverter.ToUnstructured(tt.manifestObj)
			err := setManifestHashAnnotation(&uManifestObj)
			if err != nil {
				t.Error("failed to marshall the manifest", err.Error())
			}
			manifestHash := uManifestObj.GetAnnotations()[manifestHashAnnotation]
			if tt.isSame != (manifestHash == preHash) {
				t.Errorf("testcase %s failed: manifestObj = (%+v)", name, tt.manifestObj)
			}
		})
	}
}

func TestIsManifestManagedByWork(t *testing.T) {
	tests := map[string]struct {
		ownerRefs []metav1.OwnerReference
		isManaged bool
	}{
		"empty owner list": {
			ownerRefs: nil,
			isManaged: false,
		},
		"no appliedWork": {
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: workv1alpha1.GroupVersion.String(),
					Kind:       workv1alpha1.WorkKind,
				},
			},
			isManaged: false,
		},
		"one appliedWork": {
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: workv1alpha1.GroupVersion.String(),
					Kind:       workv1alpha1.AppliedWorkKind,
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
			},
			isManaged: true,
		},
		"multiple appliedWork": {
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: workv1alpha1.GroupVersion.String(),
					Kind:       workv1alpha1.AppliedWorkKind,
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
				{
					APIVersion: workv1alpha1.GroupVersion.String(),
					Kind:       workv1alpha1.AppliedWorkKind,
					UID:        types.UID(utilrand.String(10)),
				},
			},
			isManaged: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.isManaged, isManifestManagedByWork(tt.ownerRefs), "isManifestManagedByWork(%v)", tt.ownerRefs)
		})
	}
}

func TestApplyUnstructured(t *testing.T) {
	correctObj, correctDynamicClient, correctSpecHash, err := createObjAndDynamicClient(testManifest.Raw)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}

	testDeploymentGenerated := testDeployment.DeepCopy()
	testDeploymentGenerated.Name = ""
	testDeploymentGenerated.GenerateName = utilrand.String(10)
	rawGenerated, _ := json.Marshal(testDeploymentGenerated)
	generatedSpecObj, generatedSpecDynamicClient, generatedSpecHash, err := createObjAndDynamicClient(rawGenerated)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}

	testDeploymentDiffSpec := testDeployment.DeepCopy()
	testDeploymentDiffSpec.Spec.MinReadySeconds = 0
	rawDiffSpec, _ := json.Marshal(testDeploymentDiffSpec)
	diffSpecObj, diffSpecDynamicClient, diffSpecHash, err := createObjAndDynamicClient(rawDiffSpec)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}

	patchFailClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	patchFailClient.PrependReactor("patch", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("patch failed")
	})
	patchFailClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, diffSpecObj.DeepCopy(), nil
	})

	dynamicClientNotFound := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientNotFound.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true,
			nil,
			&apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
	})

	dynamicClientError := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientError.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true,
			nil,
			errors.New("client error")
	})

	testDeploymentWithDifferentOwner := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: utilrand.String(10),
					Kind:       utilrand.String(10),
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
			},
		},
	}
	rawTestDeploymentWithDifferentOwner, _ := json.Marshal(testDeploymentWithDifferentOwner)
	_, diffOwnerDynamicClient, _, err := createObjAndDynamicClient(rawTestDeploymentWithDifferentOwner)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}

	specHashFailObj := correctObj.DeepCopy()
	specHashFailObj.Object["test"] = math.Inf(1)

	largeObj, err := createLargeObj()
	if err != nil {
		t.Errorf("failed to create large obj: %s", err)
	}
	updatedLargeObj := largeObj.DeepCopy()

	largeObjSpecHash, err := computeManifestHash(largeObj)
	if err != nil {
		t.Errorf("failed to compute manifest hash: %s", err)
	}

	// Not mocking create for dynamicClientLargeObjNotFound because by default it somehow deep copies the object as the test runs and returns it.
	dynamicClientLargeObjNotFound := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientLargeObjNotFound.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true,
			nil,
			&apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
	})

	updatedLargeObj.SetLabels(map[string]string{"test-label-key": "test-label"})
	updatedLargeObjSpecHash, err := computeManifestHash(updatedLargeObj)
	if err != nil {
		t.Errorf("failed to compute manifest hash: %s", err)
	}

	// Need to mock patch because apply return error if not.
	dynamicClientLargeObjFound := fake.NewSimpleDynamicClient(runtime.NewScheme())
	// Need to set annotation to ensure on comparison between curObj and manifestObj is different.
	largeObj.SetAnnotations(map[string]string{manifestHashAnnotation: largeObjSpecHash})
	dynamicClientLargeObjFound.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, largeObj.DeepCopy(), nil
	})
	dynamicClientLargeObjFound.PrependReactor("patch", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		// updatedLargeObj.DeepCopy() is executed when the test runs meaning the deep copy is computed as the test runs and since we pass updatedLargeObj as reference
		// in the test case input all changes made by the controller will be included when DeepCopy is computed.
		return true, updatedLargeObj.DeepCopy(), nil
	})

	dynamicClientLargeObjCreateFail := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientLargeObjCreateFail.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true,
			nil,
			&apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
	})
	dynamicClientLargeObjCreateFail.PrependReactor("create", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("create error")
	})

	dynamicClientLargeObjApplyFail := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClientLargeObjApplyFail.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, largeObj.DeepCopy(), nil
	})
	dynamicClientLargeObjApplyFail.PrependReactor("patch", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("apply error")
	})

	testCases := map[string]struct {
		reconciler     ApplyWorkReconciler
		workObj        *unstructured.Unstructured
		resultSpecHash string
		resultAction   applyAction
		resultErr      error
	}{
		"test creation succeeds when the object does not exist": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: dynamicClientNotFound,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:        correctObj.DeepCopy(),
			resultSpecHash: correctSpecHash,
			resultAction:   ManifestCreatedAction,
			resultErr:      nil,
		},
		"test creation succeeds when the object has a generated name": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: generatedSpecDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:        generatedSpecObj.DeepCopy(),
			resultSpecHash: generatedSpecHash,
			resultAction:   ManifestCreatedAction,
			resultErr:      nil,
		},
		"client error looking for object / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: dynamicClientError,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      correctObj.DeepCopy(),
			resultAction: ManifestNoChangeAction,
			resultErr:    errors.New("client error"),
		},
		"owner reference comparison failure / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: diffOwnerDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      correctObj.DeepCopy(),
			resultAction: ManifestNoChangeAction,
			resultErr:    errors.New("resource is not managed by the work controller"),
		},
		"equal spec hash of current vs work object / succeed without updates": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: correctDynamicClient,
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:        correctObj.DeepCopy(),
			resultSpecHash: correctSpecHash,
			resultAction:   ManifestNoChangeAction,
			resultErr:      nil,
		},
		"unequal spec hash of current vs work object / client patch fail": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: patchFailClient,
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      correctObj.DeepCopy(),
			resultAction: ManifestNoChangeAction,
			resultErr:    errors.New("patch failed"),
		},
		"happy path - with updates": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: diffSpecDynamicClient,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:        correctObj,
			resultSpecHash: diffSpecHash,
			resultAction:   ManifestThreeWayMergePatchAction,
			resultErr:      nil,
		},
		"test create succeeds for large manifest when object does not exist": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjNotFound,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:        largeObj,
			resultSpecHash: largeObjSpecHash,
			resultAction:   ManifestCreatedAction,
			resultErr:      nil,
		},
		"test apply succeeds on update for large manifest when object exists": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjFound,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:        updatedLargeObj,
			resultSpecHash: updatedLargeObjSpecHash,
			resultAction:   ManifestServerSideAppliedAction,
			resultErr:      nil,
		},
		"test create fails for large manifest when object does not exist": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjCreateFail,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      largeObj,
			resultAction: ManifestNoChangeAction,
			resultErr:    errors.New("create error"),
		},
		"test apply fails for large manifest when object exists": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjApplyFail,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      updatedLargeObj,
			resultAction: ManifestNoChangeAction,
			resultErr:    errors.New("apply error"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			applyResult, applyAction, err := testCase.reconciler.applyUnstructured(context.Background(), testGvr, testCase.workObj)
			assert.Equalf(t, testCase.resultAction, applyAction, "updated boolean not matching for Testcase %s", testName)
			if testCase.resultErr != nil {
				assert.Containsf(t, err.Error(), testCase.resultErr.Error(), "error not matching for Testcase %s", testName)
			} else {
				assert.Truef(t, err == nil, "err is not nil for Testcase %s", testName)
				assert.Truef(t, applyResult != nil, "applyResult is not nil for Testcase %s", testName)
				// Not checking last applied config because it has live fields.
				assert.Equalf(t, testCase.resultSpecHash, applyResult.GetAnnotations()[manifestHashAnnotation],
					"specHash not matching for Testcase %s", testName)
				assert.Equalf(t, ownerRef, applyResult.GetOwnerReferences()[0], "ownerRef not matching for Testcase %s", testName)
			}
		})
	}
}

func TestApplyManifest(t *testing.T) {
	failMsg := "manifest apply failed"
	// Manifests
	rawInvalidResource, _ := json.Marshal([]byte(utilrand.String(10)))
	rawMissingResource, _ := json.Marshal(
		v1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "core/v1",
			},
		})
	InvalidManifest := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawInvalidResource,
	}}
	MissingManifest := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawMissingResource,
	}}

	// GVRs
	expectedGvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}
	emptyGvr := schema.GroupVersionResource{}

	// DynamicClients
	clientFailDynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	clientFailDynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New(failMsg)
	})

	testCases := map[string]struct {
		reconciler   ApplyWorkReconciler
		manifestList []workv1alpha1.Manifest
		generation   int64
		action       applyAction
		wantGvr      schema.GroupVersionResource
		wantErr      error
	}{
		"manifest is in proper format/ happy path": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			manifestList: []workv1alpha1.Manifest{testManifest},
			generation:   0,
			action:       ManifestCreatedAction,
			wantGvr:      expectedGvr,
			wantErr:      nil,
		},
		"manifest has incorrect syntax/ decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			manifestList: append([]workv1alpha1.Manifest{}, InvalidManifest),
			generation:   0,
			action:       ManifestNoChangeAction,
			wantGvr:      emptyGvr,
			wantErr: &json.UnmarshalTypeError{
				Value: "string",
				Type:  reflect.TypeOf(map[string]interface{}{}),
			},
		},
		"manifest is correct / object not mapped in restmapper / decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			manifestList: append([]workv1alpha1.Manifest{}, MissingManifest),
			generation:   0,
			action:       ManifestNoChangeAction,
			wantGvr:      emptyGvr,
			wantErr:      errors.New("failed to find group/version/resource from restmapping: test error: mapping does not exist"),
		},
		"manifest is in proper format/ should fail applyUnstructured": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			manifestList: append([]workv1alpha1.Manifest{}, testManifest),
			generation:   0,
			action:       ManifestNoChangeAction,
			wantGvr:      expectedGvr,
			wantErr:      errors.New(failMsg),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			resultList := testCase.reconciler.applyManifests(context.Background(), testCase.manifestList, ownerRef)
			for _, result := range resultList {
				if testCase.wantErr != nil {
					assert.Containsf(t, result.err.Error(), testCase.wantErr.Error(), "Incorrect error for Testcase %s", testName)
				} else {
					assert.Equalf(t, testCase.generation, result.generation, "Testcase %s: generation incorrect", testName)
					assert.Equalf(t, testCase.action, result.action, "Testcase %s: Updated action incorrect", testName)
				}
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	failMsg := "manifest apply failed"
	workNamespace := utilrand.String(10)
	workName := utilrand.String(10)
	appliedWorkName := utilrand.String(10)
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workNamespace,
			Name:      workName,
		},
	}
	wrongReq := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: utilrand.String(10),
			Name:      utilrand.String(10),
		},
	}
	invalidReq := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "",
			Name:      "",
		},
	}

	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Namespace != workNamespace {
			return &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
		}
		o, _ := obj.(*workv1alpha1.Work)
		*o = workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  workNamespace,
				Name:       workName,
				Finalizers: []string{workFinalizer},
			},
			Spec: workv1alpha1.WorkSpec{Workload: workv1alpha1.WorkloadTemplate{Manifests: []workv1alpha1.Manifest{testManifest}}},
		}
		return nil
	}

	happyDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: workv1alpha1.GroupVersion.String(),
					Kind:       "AppliedWork",
					Name:       appliedWorkName,
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			MinReadySeconds: 5,
		},
	}
	rawHappyDeployment, _ := json.Marshal(happyDeployment)
	happyManifest := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawHappyDeployment,
	}}
	_, happyDynamicClient, _, err := createObjAndDynamicClient(happyManifest.Raw)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}

	getMockAppliedWork := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name != workName {
			return &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
		}
		o, _ := obj.(*workv1alpha1.AppliedWork)
		*o = workv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: appliedWorkName,
			},
			Spec: workv1alpha1.AppliedWorkSpec{
				WorkName:      workNamespace,
				WorkNamespace: workName,
			},
		}
		return nil
	}

	clientFailDynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	clientFailDynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New(failMsg)
	})

	testCases := map[string]struct {
		reconciler ApplyWorkReconciler
		req        ctrl.Request
		wantErr    error
		requeue    bool
	}{
		"controller is being stopped": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: happyDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(false),
			},
			req:     req,
			wantErr: nil,
			requeue: true,
		},
		"work cannot be retrieved, client failed due to client error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return fmt.Errorf("client failing")
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			req:     invalidReq,
			wantErr: errors.New("client failing"),
		},
		"work cannot be retrieved, client failed due to not found error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			req:     wrongReq,
			wantErr: nil,
		},
		"work without finalizer / no error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*workv1alpha1.Work)
						*o = workv1alpha1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: workNamespace,
								Name:      workName,
							},
						}
						return nil
					},
					MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return nil
					},
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient: &test.MockClient{
					MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						return nil
					},
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				restMapper: testMapper{},
				recorder:   utils.NewFakeRecorder(1),
				joined:     atomic.NewBool(true),
			},
			req:     req,
			wantErr: nil,
		},
		"work with non-zero deletion-timestamp / succeed": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*workv1alpha1.Work)
						*o = workv1alpha1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Namespace:         workNamespace,
								Name:              workName,
								Finalizers:        []string{"multicluster.x-k8s.io/work-cleanup"},
								DeletionTimestamp: &metav1.Time{Time: time.Now()},
							},
						}
						return nil
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			req:     req,
			wantErr: nil,
		},
		"Retrieving appliedwork fails, will create": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return &apierrors.StatusError{
							ErrStatus: metav1.Status{
								Status: metav1.StatusFailure,
								Reason: metav1.StatusReasonNotFound,
							}}
					},
					MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						return nil
					},
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				restMapper: testMapper{},
				recorder:   utils.NewFakeRecorder(1),
				joined:     atomic.NewBool(true),
			},
			req:     req,
			wantErr: nil,
		},
		"ApplyManifest fails": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				restMapper: testMapper{},
				recorder:   utils.NewFakeRecorder(2),
				joined:     atomic.NewBool(true),
			},
			req:     req,
			wantErr: errors.New(failMsg),
		},
		"client update fails": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return errors.New("failed")
					},
				},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
				},
				restMapper: testMapper{},
				recorder:   utils.NewFakeRecorder(2),
				joined:     atomic.NewBool(true),
			},
			req:     req,
			wantErr: errors.New("failed"),
		},
		"Happy Path": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				spokeDynamicClient: happyDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return nil
					},
				},
				restMapper: testMapper{},
				recorder:   utils.NewFakeRecorder(1),
				joined:     atomic.NewBool(true),
			},
			req:     req,
			wantErr: nil,
			requeue: true,
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			ctrlResult, err := testCase.reconciler.Reconcile(context.Background(), testCase.req)
			if testCase.wantErr != nil {
				assert.Containsf(t, err.Error(), testCase.wantErr.Error(), "incorrect error for Testcase %s", testName)
			} else {
				if testCase.requeue {
					if testCase.reconciler.joined.Load() {
						assert.Equal(t, ctrl.Result{RequeueAfter: time.Minute * 5}, ctrlResult, "incorrect ctrlResult for Testcase %s", testName)
					} else {
						assert.Equal(t, ctrl.Result{RequeueAfter: time.Second * 5}, ctrlResult, "incorrect ctrlResult for Testcase %s", testName)
					}
				}
				assert.Equalf(t, false, ctrlResult.Requeue, "incorrect ctrlResult for Testcase %s", testName)
			}
		})
	}
}

func createObjAndDynamicClient(rawManifest []byte) (*unstructured.Unstructured, dynamic.Interface, string, error) {
	uObj := unstructured.Unstructured{}
	err := uObj.UnmarshalJSON(rawManifest)
	if err != nil {
		return nil, nil, "", err
	}
	validSpecHash, err := computeManifestHash(&uObj)
	if err != nil {
		return nil, nil, "", err
	}
	uObj.SetAnnotations(map[string]string{manifestHashAnnotation: validSpecHash})
	_, err = setModifiedConfigurationAnnotation(&uObj)
	if err != nil {
		return nil, nil, "", err
	}
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, uObj.DeepCopy(), nil
	})
	dynamicClient.PrependReactor("patch", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, uObj.DeepCopy(), nil
	})
	return &uObj, dynamicClient, validSpecHash, nil
}

func createLargeObj() (*unstructured.Unstructured, error) {
	var largeSecret v1.Secret
	if err := utils.GetObjectFromManifest("../../../test/integration/manifests/resources/test-large-secret.yaml", &largeSecret); err != nil {
		return nil, err
	}
	largeSecret.ObjectMeta = metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			ownerRef,
		},
	}
	rawSecret, err := json.Marshal(largeSecret)
	if err != nil {
		return nil, err
	}
	var largeObj unstructured.Unstructured
	if err := largeObj.UnmarshalJSON(rawSecret); err != nil {
		return nil, err
	}
	return &largeObj, nil
}
