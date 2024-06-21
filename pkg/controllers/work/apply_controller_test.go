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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	testcontroller "go.goms.io/fleet/test/utils/controller"
)

var (
	fakeDynamicClient = fake.NewSimpleDynamicClient(runtime.NewScheme())
	ownerRef          = metav1.OwnerReference{
		APIVersion: fleetv1beta1.GroupVersion.String(),
		Kind:       "AppliedWork",
		Name:       "default-work",
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
	testManifest         = fleetv1beta1.Manifest{RawExtension: runtime.RawExtension{
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
			Resource:         utils.DeploymentGVR,
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
				alterObj.Annotations[fleetv1beta1.ManifestHashAnnotation] = utilrand.String(10)
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
				alterObj.OwnerReferences[0].APIVersion = fleetv1beta1.GroupVersion.String()
				alterObj.OwnerReferences[0].Kind = fleetv1beta1.AppliedWorkKind
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
				alterObj.Spec.Replicas = ptr.To(int32(100))
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
			manifestHash := uManifestObj.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation]
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
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       fleetv1beta1.WorkKind,
				},
			},
			isManaged: false,
		},
		"one appliedWork": {
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       fleetv1beta1.AppliedWorkKind,
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
			},
			isManaged: true,
		},
		"multiple appliedWork": {
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       fleetv1beta1.AppliedWorkKind,
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       fleetv1beta1.AppliedWorkKind,
					UID:        types.UID(utilrand.String(10)),
				},
			},
			isManaged: true,
		},
		"include one non-appliedWork owner": {
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       fleetv1beta1.AppliedWorkKind,
					Name:       utilrand.String(10),
					UID:        types.UID(utilrand.String(10)),
				},
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       "another-kind",
					UID:        types.UID(utilrand.String(10)),
				},
			},
			isManaged: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.isManaged, isManifestManagedByWork(tt.ownerRefs), "isManifestManagedByWork(%v)", tt.ownerRefs)
		})
	}
}

func TestBuildManifestCondition(t *testing.T) {
	tests := map[string]struct {
		err    error
		action ApplyAction
		want   []metav1.Condition
	}{
		"TestNoErrorManifestCreated": {
			err:    nil,
			action: manifestCreatedAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(manifestCreatedAction),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: ManifestNeedsUpdateReason,
				},
			},
		},
		"TestNoErrorManifestServerSideApplied": {
			err:    nil,
			action: manifestServerSideAppliedAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(manifestServerSideAppliedAction),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: ManifestNeedsUpdateReason,
				},
			},
		},
		"TestNoErrorManifestThreeWayMergePatch": {
			err:    nil,
			action: manifestThreeWayMergePatchAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(manifestThreeWayMergePatchAction),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: ManifestNeedsUpdateReason,
				},
			},
		},
		"TestNoErrorManifestNotAvailable": {
			err:    nil,
			action: manifestNotAvailableYetAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: ManifestAlreadyUpToDateReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: string(manifestNotAvailableYetAction),
				},
			},
		},
		"TestNoErrorManifestNotTrackableAction": {
			err:    nil,
			action: manifestNotTrackableAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: ManifestAlreadyUpToDateReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(manifestNotTrackableAction),
				},
			},
		},
		"TestNoErrorManifestAvailableAction": {
			err:    nil,
			action: manifestAvailableAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: ManifestAlreadyUpToDateReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(manifestAvailableAction),
				},
			},
		},
		"TestApplyError": {
			err:    errors.New("test error"),
			action: errorApplyAction,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: ManifestApplyFailedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: ManifestApplyFailedReason,
				},
			},
		},
		"TestApplyConflictBetweenPlacements": {
			err:    errors.New("test error"),
			action: applyConflictBetweenPlacements,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: ApplyConflictBetweenPlacementsReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: ManifestApplyFailedReason,
				},
			},
		},
		"TestManifestOwnedByOthers": {
			err:    errors.New("test error"),
			action: manifestAlreadyOwnedByOthers,
			want: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: ManifestsAlreadyOwnedByOthersReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: ManifestApplyFailedReason,
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conditions := buildManifestCondition(tt.err, tt.action, 1)
			diff := testcontroller.CompareConditions(tt.want, conditions)
			assert.Empty(t, diff, "buildManifestCondition() test %v failed, (-want +got):\n%s", name, diff)
		})
	}
}

func TestGenerateWorkCondition(t *testing.T) {
	tests := map[string]struct {
		manifestConditions []fleetv1beta1.ManifestCondition
		expected           []metav1.Condition
	}{
		"Test applied one failed": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionUnknown,
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: workAppliedFailedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: workAppliedFailedReason,
				},
			},
		},
		"Test applied one of the two failed": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionUnknown,
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: workAppliedFailedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: workAppliedFailedReason,
				},
			},
		},
		"Test applied one succeed but available unknown yet": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(manifestNotTrackableAction),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionUnknown,
							Reason: ManifestNeedsUpdateReason,
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: workAppliedCompletedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: workAvailabilityUnknownReason,
				},
			},
		},
		"Test applied one succeed but not available yet": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(manifestNotTrackableAction),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: workAppliedCompletedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: workNotAvailableYetReason,
				},
			},
		},
		"Test applied all succeeded but one of two not available yet": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionFalse,
							Reason: string(manifestNotAvailableYetAction),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(manifestNotTrackableAction),
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: workAppliedCompletedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: workNotAvailableYetReason,
				},
			},
		},
		"Test applied all succeeded but one unknown, one unavailable, one available": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionFalse,
							Reason: string(manifestNotAvailableYetAction),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionUnknown,
							Reason: ManifestNeedsUpdateReason,
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 3,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(manifestNotTrackableAction),
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: workAppliedCompletedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionUnknown,
					Reason: workAvailabilityUnknownReason,
				},
			},
		},
		"Test applied all succeeded but one of two not trackable": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(manifestAvailableAction),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(manifestNotTrackableAction),
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: workAppliedCompletedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: WorkNotTrackableReason,
				},
			},
		},
		"Test applied all available": {
			manifestConditions: []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(manifestAvailableAction),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(manifestAvailableAction),
						},
					},
				},
			},
			expected: []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: workAppliedCompletedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: WorkAvailableReason,
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conditions := buildWorkCondition(tt.manifestConditions, 1)
			diff := testcontroller.CompareConditions(tt.expected, conditions)
			assert.Empty(t, diff, "buildWorkCondition() test %v failed, (-want +got):\n%s", name, diff)
		})
	}
}

func TestIsDataResource(t *testing.T) {
	tests := map[string]struct {
		gvr  schema.GroupVersionResource
		want bool
	}{
		"Namespace resource": {
			gvr: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "namespaces",
			},
			want: true,
		},
		"Secret resource": {
			gvr: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			},
			want: true,
		},
		"ConfigMap resource": {
			gvr: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			},
			want: true,
		},
		"Role resource": {
			gvr:  utils.RoleGVR,
			want: true,
		},
		"RoleBinding resource": {
			gvr:  utils.RoleBindingGVR,
			want: true,
		},
		"ClusterRole resource": {
			gvr:  utils.ClusterRoleGVR,
			want: true,
		},
		"ClusterRoleBinding resource": {
			gvr:  utils.ClusterRoleBindingGVR,
			want: true,
		},
		"Non-data resource (Pod)": {
			gvr: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			want: false,
		},
		"Non-data resource (Service)": {
			gvr:  utils.ServiceGVR,
			want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := isDataResource(tt.gvr)
			if got != tt.want {
				t.Errorf("isDataResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrackResourceAvailability(t *testing.T) {
	tests := map[string]struct {
		gvr      schema.GroupVersionResource
		obj      *unstructured.Unstructured
		expected ApplyAction
		err      error
	}{
		"Test a mal-formated object": {
			gvr: utils.DeploymentGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"generation": 1,
						"name":       "test-deployment",
					},
					"spec": "wrongspec",
					"status": map[string]interface{}{
						"observedGeneration": 1,
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			},
			expected: errorApplyAction,
			err:      controller.ErrUnexpectedBehavior,
		},
		"Test Deployment available": {
			gvr: utils.DeploymentGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"generation": 1,
						"name":       "test-deployment",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 1,
						"availableReplicas":  3,
						"updatedReplicas":    3,
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test Deployment available with default replica": {
			gvr: utils.DeploymentGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"generation": 1,
						"name":       "test-deployment",
					},
					"status": map[string]interface{}{
						"observedGeneration": 1,
						"availableReplicas":  1,
						"updatedReplicas":    1,
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test Deployment not observe the latest generation": {
			gvr: utils.DeploymentGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"generation": 2,
						"name":       "test-deployment",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 1,
						"availableReplicas":  3,
						"updatedReplicas":    3,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test Deployment not available as not enough available": {
			gvr: utils.DeploymentGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"generation": 2,
						"name":       "test-deployment",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 2,
						"availableReplicas":  2,
						"updatedReplicas":    3,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test Deployment not available as not enough updated": {
			gvr: utils.DeploymentGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"generation": 2,
						"name":       "test-deployment",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 2,
						"availableReplicas":  3,
						"updatedReplicas":    2,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test StatefulSet available": {
			gvr: utils.StatefulSettGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "StatefulSet",
					"metadata": map[string]interface{}{
						"generation": 5,
						"name":       "test-statefulset",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 5,
						"availableReplicas":  3,
						"currentReplicas":    3,
						"updatedReplicas":    3,
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test StatefulSet not available": {
			gvr: utils.StatefulSettGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "StatefulSet",
					"metadata": map[string]interface{}{
						"generation": 3,
						"name":       "test-statefulset",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 3,
						"availableReplicas":  2,
						"currentReplicas":    3,
						"updatedReplicas":    3,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test StatefulSet observed old generation": {
			gvr: utils.StatefulSettGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "StatefulSet",
					"metadata": map[string]interface{}{
						"generation": 3,
						"name":       "test-statefulset",
					},
					"spec": map[string]interface{}{
						"replicas": 3,
					},
					"status": map[string]interface{}{
						"observedGeneration": 2,
						"availableReplicas":  2,
						"currentReplicas":    3,
						"updatedReplicas":    3,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test DaemonSet Available": {
			gvr: utils.DaemonSettGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "DaemonSet",
					"metadata": map[string]interface{}{
						"generation": 1,
					},
					"status": map[string]interface{}{
						"observedGeneration":     1,
						"numberAvailable":        1,
						"desiredNumberScheduled": 1,
						"currentNumberScheduled": 1,
						"updatedNumberScheduled": 1,
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test DaemonSet not available": {
			gvr: utils.DaemonSettGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "DaemonSet",
					"metadata": map[string]interface{}{
						"generation": 1,
					},
					"status": map[string]interface{}{
						"observedGeneration":     1,
						"numberAvailable":        0,
						"desiredNumberScheduled": 1,
						"currentNumberScheduled": 1,
						"updatedNumberScheduled": 1,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test DaemonSet not observe current generation": {
			gvr: utils.DaemonSettGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "DaemonSet",
					"metadata": map[string]interface{}{
						"generation": 2,
					},
					"status": map[string]interface{}{
						"observedGeneration":     1,
						"numberAvailable":        0,
						"desiredNumberScheduled": 1,
						"currentNumberScheduled": 1,
						"updatedNumberScheduled": 1,
					},
				},
			},
			expected: manifestNotAvailableYetAction,
			err:      nil,
		},
		"Test Job not trackable": {
			gvr: utils.JobGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"status": map[string]interface{}{
						"succeeded": 2,
						"ready":     1,
					},
				},
			},
			expected: manifestNotTrackableAction,
			err:      nil,
		},
		"Test configMap is considered ready after it is applied": {
			gvr: utils.ConfigMapGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-configmap",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"key": "value",
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test secret is considered ready after it is applied": {
			gvr: utils.ConfigMapGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-configmap",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"key": "value",
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test namespace is considered ready after it is applied": {
			gvr: utils.ConfigMapGVR,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "NameSpace",
					"metadata": map[string]interface{}{
						"name":      "test-namespae",
						"namespace": "default",
					},
				},
			},
			expected: manifestAvailableAction,
			err:      nil,
		},
		"Test UnknownResource": {
			gvr: schema.GroupVersionResource{
				Group:    "unknown",
				Version:  "v1",
				Resource: "unknown",
			},
			obj:      &unstructured.Unstructured{},
			expected: manifestNotTrackableAction,
			err:      nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			action, err := trackResourceAvailability(tt.gvr, tt.obj)
			assert.Equal(t, tt.expected, action, "action not matching in test %s", name)
			assert.Equal(t, errors.Is(err, tt.err), true, "applyErr not matching in test %s", name)
		})
	}
}

func TestTrackServiceAvailability(t *testing.T) {
	tests := map[string]struct {
		service  *v1.Service
		expected ApplyAction
	}{
		"externalName service type not trackable": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:       v1.ServiceTypeExternalName,
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			expected: manifestNotTrackableAction,
		},
		"Empty service type with IP": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:       "",
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			expected: manifestAvailableAction,
		},
		"ClusterIP service with IP": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:       v1.ServiceTypeClusterIP,
					ClusterIP:  "192.168.1.1",
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			expected: manifestAvailableAction,
		},
		"Headless clusterIP service": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:       v1.ServiceTypeClusterIP,
					ClusterIPs: []string{"None"},
				},
			},
			expected: manifestAvailableAction,
		},
		"Nodeport service with IP": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:       v1.ServiceTypeNodePort,
					ClusterIP:  "13.6.2.2",
					ClusterIPs: []string{"192.168.1.1"},
				},
			},
			expected: manifestAvailableAction,
		},
		"ClusterIP service without IP": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type:      v1.ServiceTypeClusterIP,
					ClusterIP: "13.6.2.2",
				},
			},
			expected: manifestNotAvailableYetAction,
		},
		"LoadBalancer service with IP": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{
								IP: "10.1.2.4",
							},
						},
					},
				},
			},
			expected: manifestAvailableAction,
		},
		"LoadBalancer service with hostname": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{
								Hostname: "one.microsoft.com",
							},
						},
					},
				},
			},
			expected: manifestAvailableAction,
		},
		"LoadBalancer service with empty load balancer ingress not ready": {
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{},
					},
				},
			},
			expected: manifestNotAvailableYetAction,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rawService, _ := json.Marshal(tt.service)
			serviceObj := &unstructured.Unstructured{}
			_ = serviceObj.UnmarshalJSON(rawService)

			action, err := trackServiceAvailability(serviceObj)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if action != tt.expected {
				t.Errorf("Expected action to be %v, but got %v", tt.expected, action)
			}
		})
	}
}

func TestApplyUnstructuredAndTrackAvailability(t *testing.T) {
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
				ownerRef,
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
	//correctObj, correctDynamicClient, correctSpecHash, err := createObjAndDynamicClient(testManifest.Raw)
	diffOwnerDynamicObj, diffOwnerDynamicClient, diffOwnerSpechHash, err := createObjAndDynamicClient(rawTestDeploymentWithDifferentOwner)
	if err != nil {
		t.Errorf("failed to create obj and dynamic client: %s", err)
	}

	testDeploymentOwnedByAnotherWork := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: fleetv1beta1.GroupVersion.String(),
					Kind:       fleetv1beta1.AppliedWorkKind,
					Name:       "another-work",
					UID:        types.UID(utilrand.String(10)),
				},
			},
		},
	}
	rawTestDeploymentByAnotherWork, _ := json.Marshal(testDeploymentOwnedByAnotherWork)
	_, deploymentOwnedByAnotherWorkClient, _, err := createObjAndDynamicClient(rawTestDeploymentByAnotherWork)
	if err != nil {
		t.Errorf("Failed to create obj and dynamic client: %s", err)
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
	largeObj.SetAnnotations(map[string]string{fleetv1beta1.ManifestHashAnnotation: largeObjSpecHash})
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
		reconciler       ApplyWorkReconciler
		allowCoOwnership bool
		workObj          *unstructured.Unstructured
		resultSpecHash   string
		resultAction     ApplyAction
		resultErr        error
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
			resultAction:   manifestNotAvailableYetAction,
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
			resultAction:   manifestNotAvailableYetAction,
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
			resultAction: errorApplyAction,
			resultErr:    errors.New("client error"),
		},
		"owner reference comparison failure / fail": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
						}
						return nil
					},
				},
				spokeDynamicClient: diffOwnerDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      correctObj.DeepCopy(),
			resultAction: manifestAlreadyOwnedByOthers,
			resultErr:    controller.ErrUserError,
		},
		"co-ownership is allowed": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{
									AllowCoOwnership: true,
								},
							},
						}
						return nil
					},
				},
				spokeDynamicClient: diffOwnerDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			allowCoOwnership: true,
			workObj:          diffOwnerDynamicObj.DeepCopy(),
			resultSpecHash:   diffOwnerSpechHash,
			resultAction:     manifestNotAvailableYetAction,
			resultErr:        nil,
		},
		"resource is owned by another conflicted work": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "another-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeServerSideApply},
							},
						}
						return nil
					},
				},
				spokeDynamicClient: deploymentOwnedByAnotherWorkClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      correctObj.DeepCopy(),
			resultAction: applyConflictBetweenPlacements,
			resultErr:    errors.New("manifest is already managed by placement"),
		},
		// TODO add a test case: resource is co-owned by another work
		// Right now the mock framework cannot send back the correct result unless we mock the behavior.
		// Need to rewrite the setup to use the fake client.
		"equal spec hash of current vs work object /  not available yet": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: correctDynamicClient,
				recorder:           utils.NewFakeRecorder(1),
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply},
							},
						}
						return nil
					},
				},
			},
			workObj:        correctObj.DeepCopy(),
			resultSpecHash: correctSpecHash,
			resultAction:   manifestNotAvailableYetAction,
			resultErr:      nil,
		},
		"unequal spec hash of current vs work object / client patch fail": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: patchFailClient,
				recorder:           utils.NewFakeRecorder(1),
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply},
							},
						}
						return nil
					},
				},
			},
			workObj:      correctObj.DeepCopy(),
			resultAction: errorApplyAction,
			resultErr:    errors.New("patch failed"),
		},
		"happy path - with updates (three way merge patch)": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: diffSpecDynamicClient,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply},
							},
						}
						return nil
					},
				},
			},
			workObj:        correctObj,
			resultSpecHash: diffSpecHash,
			resultAction:   manifestNotAvailableYetAction,
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
			resultAction:   manifestNotAvailableYetAction,
			resultErr:      nil,
		},
		"test apply succeeds on update for large manifest when object exists": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjFound,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply},
							},
						}
						return nil
					},
				},
			},
			workObj:        updatedLargeObj,
			resultSpecHash: updatedLargeObjSpecHash,
			resultAction:   manifestNotAvailableYetAction,
			resultErr:      nil,
		},
		"test create fails for large manifest when object does not exist": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjCreateFail,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
			},
			workObj:      largeObj,
			resultAction: errorApplyAction,
			resultErr:    errors.New("create error"),
		},
		"test apply fails for large manifest when object exists": {
			reconciler: ApplyWorkReconciler{
				spokeDynamicClient: dynamicClientLargeObjApplyFail,
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Name: "default-work",
							},
							Spec: fleetv1beta1.WorkSpec{
								ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply},
							},
						}
						return nil
					},
				},
			},
			workObj:      updatedLargeObj,
			resultAction: errorApplyAction,
			resultErr:    errors.New("apply error"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			r := testCase.reconciler
			r.appliers = map[fleetv1beta1.ApplyStrategyType]Applier{
				fleetv1beta1.ApplyStrategyTypeClientSideApply: &ClientSideApplier{
					HubClient:          r.client,
					WorkNamespace:      r.workNameSpace,
					SpokeDynamicClient: r.spokeDynamicClient,
				},
			}
			strategy := &fleetv1beta1.ApplyStrategy{
				Type:             fleetv1beta1.ApplyStrategyTypeClientSideApply,
				AllowCoOwnership: testCase.allowCoOwnership,
			}
			applyResult, applyAction, err := r.applyUnstructuredAndTrackAvailability(context.Background(), utils.DeploymentGVR, testCase.workObj, strategy)
			assert.Equalf(t, testCase.resultAction, applyAction, "updated boolean not matching for Testcase %s", testName)
			if testCase.resultErr != nil {
				assert.Containsf(t, err.Error(), testCase.resultErr.Error(), "error not matching for Testcase %s", testName)
			} else {
				assert.Truef(t, err == nil, "applyErr is not nil for Testcase %s", testName)
				assert.Truef(t, applyResult != nil, "applyResult is not nil for Testcase %s", testName)
				// Not checking last applied config because it has live fields.
				assert.Equalf(t, testCase.resultSpecHash, applyResult.GetAnnotations()[fleetv1beta1.ManifestHashAnnotation],
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
	InvalidManifest := fleetv1beta1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawInvalidResource,
	}}
	MissingManifest := fleetv1beta1.Manifest{RawExtension: runtime.RawExtension{
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
		reconciler     ApplyWorkReconciler
		manifestList   []fleetv1beta1.Manifest
		wantGeneration int64
		wantAction     ApplyAction
		wantGvr        schema.GroupVersionResource
		wantErr        error
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
			manifestList:   []fleetv1beta1.Manifest{testManifest},
			wantGeneration: 0,
			wantAction:     manifestNotAvailableYetAction,
			wantGvr:        expectedGvr,
			wantErr:        nil,
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
			manifestList:   append([]fleetv1beta1.Manifest{}, InvalidManifest),
			wantGeneration: 0,
			wantAction:     errorApplyAction,
			wantGvr:        emptyGvr,
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
			manifestList:   append([]fleetv1beta1.Manifest{}, MissingManifest),
			wantGeneration: 0,
			wantAction:     errorApplyAction,
			wantGvr:        emptyGvr,
			wantErr:        errors.New("failed to find group/version/resource from restmapping: test error: mapping does not exist"),
		},
		"manifest is in proper format/ should fail applyUnstructuredAndTrackAvailability": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
				recorder:           utils.NewFakeRecorder(1),
				joined:             atomic.NewBool(true),
			},
			manifestList:   append([]fleetv1beta1.Manifest{}, testManifest),
			wantGeneration: 0,
			wantAction:     errorApplyAction,
			wantGvr:        expectedGvr,
			wantErr:        errors.New(failMsg),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			r := testCase.reconciler
			r.appliers = map[fleetv1beta1.ApplyStrategyType]Applier{
				fleetv1beta1.ApplyStrategyTypeClientSideApply: &ClientSideApplier{
					HubClient:          r.client,
					WorkNamespace:      r.workNameSpace,
					SpokeDynamicClient: r.spokeDynamicClient,
				},
			}
			applyStrategy := &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply}
			resultList := r.applyManifests(context.Background(), testCase.manifestList, ownerRef, applyStrategy)
			for _, result := range resultList {
				if testCase.wantErr != nil {
					assert.Containsf(t, result.applyErr.Error(), testCase.wantErr.Error(), "Incorrect error for Testcase %s", testName)
				} else {
					assert.Equalf(t, testCase.wantGeneration, result.generation, "Testcase %s: wantGeneration incorrect", testName)
					assert.Equalf(t, testCase.wantAction, result.action, "Testcase %s: Updated wantAction incorrect", testName)
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
		o, _ := obj.(*fleetv1beta1.Work)
		*o = fleetv1beta1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  key.Namespace,
				Name:       key.Name,
				Finalizers: []string{fleetv1beta1.WorkFinalizer},
			},
			Spec: fleetv1beta1.WorkSpec{
				Workload:      fleetv1beta1.WorkloadTemplate{Manifests: []fleetv1beta1.Manifest{testManifest}},
				ApplyStrategy: &fleetv1beta1.ApplyStrategy{Type: fleetv1beta1.ApplyStrategyTypeClientSideApply},
			},
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
					APIVersion: fleetv1beta1.GroupVersion.String(),
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
	happyManifest := fleetv1beta1.Manifest{RawExtension: runtime.RawExtension{
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
		o, _ := obj.(*fleetv1beta1.AppliedWork)
		*o = fleetv1beta1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: appliedWorkName,
			},
			Spec: fleetv1beta1.AppliedWorkSpec{
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
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
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
						o, _ := obj.(*fleetv1beta1.Work)
						*o = fleetv1beta1.Work{
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
			r := testCase.reconciler
			r.workNameSpace = workNamespace
			r.appliers = map[fleetv1beta1.ApplyStrategyType]Applier{
				fleetv1beta1.ApplyStrategyTypeClientSideApply: &ClientSideApplier{
					HubClient:          r.client,
					WorkNamespace:      r.workNameSpace,
					SpokeDynamicClient: r.spokeDynamicClient,
				},
			}
			ctrlResult, err := r.Reconcile(context.Background(), testCase.req)
			if testCase.wantErr != nil {
				assert.Containsf(t, err.Error(), testCase.wantErr.Error(), "incorrect error for Testcase %s", testName)
			} else {
				if testCase.requeue {
					if testCase.reconciler.joined.Load() {
						assert.Equal(t, ctrl.Result{RequeueAfter: time.Second * 3}, ctrlResult, "incorrect ctrlResult for Testcase %s", testName)
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
	uObj.SetAnnotations(map[string]string{fleetv1beta1.ManifestHashAnnotation: validSpecHash})
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
