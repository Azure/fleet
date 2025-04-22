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
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/parallelizer"
)

// TestBuildWorkResourceIdentifier tests the buildWorkResourceIdentifier function.
func TestBuildWorkResourceIdentifier(t *testing.T) {
	testCases := []struct {
		name        string
		manifestIdx int
		gvr         *schema.GroupVersionResource
		manifestObj *unstructured.Unstructured
		wantWRI     *fleetv1beta1.WorkResourceIdentifier
	}{
		{
			name:        "ordinal only",
			manifestIdx: 0,
			wantWRI: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal: 0,
			},
		},
		{
			name:        "ordinal and manifest object",
			manifestIdx: 1,
			manifestObj: nsUnstructured,
			wantWRI: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal: 1,
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
				Name:    nsName,
			},
		},
		{
			name:        "ordinal, manifest object, and GVR",
			manifestIdx: 2,
			gvr: &schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
			manifestObj: deployUnstructured,
			wantWRI:     deployWRI(2, nsName, deployName),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wri := buildWorkResourceIdentifier(tc.manifestIdx, tc.gvr, tc.manifestObj)
			if diff := cmp.Diff(wri, tc.wantWRI); diff != "" {
				t.Errorf("buildWorkResourceIdentifier() mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestRemoveLeftOverManifests tests the removeLeftOverManifests method.
func TestRemoveLeftOverManifests(t *testing.T) {
	ctx := context.Background()

	additionalOwnerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "SuperNamespace",
		Name:       "super-ns",
		UID:        "super-ns-uid",
	}

	nsName0 := fmt.Sprintf(nsNameTemplate, "0")
	nsName1 := fmt.Sprintf(nsNameTemplate, "1")
	nsName2 := fmt.Sprintf(nsNameTemplate, "2")
	nsName3 := fmt.Sprintf(nsNameTemplate, "3")

	testCases := []struct {
		name                           string
		leftOverManifests              []fleetv1beta1.AppliedResourceMeta
		inMemberClusterObjs            []runtime.Object
		wantInMemberClusterObjs        []corev1.Namespace
		wantRemovedInMemberClusterObjs []corev1.Namespace
	}{
		{
			name: "mixed",
			leftOverManifests: []fleetv1beta1.AppliedResourceMeta{
				// The object is present.
				{
					WorkResourceIdentifier: *nsWRI(0, nsName0),
				},
				// The object cannot be found.
				{
					WorkResourceIdentifier: *nsWRI(1, nsName1),
				},
				// The object is not owned by Fleet.
				{
					WorkResourceIdentifier: *nsWRI(2, nsName2),
				},
				// The object has multiple owners.
				{
					WorkResourceIdentifier: *nsWRI(3, nsName3),
				},
			},
			inMemberClusterObjs: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName0,
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName2,
						OwnerReferences: []metav1.OwnerReference{
							*additionalOwnerRef,
						},
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName3,
						OwnerReferences: []metav1.OwnerReference{
							*additionalOwnerRef,
							*appliedWorkOwnerRef,
						},
					},
				},
			},
			wantInMemberClusterObjs: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName2,
						OwnerReferences: []metav1.OwnerReference{
							*additionalOwnerRef,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName3,
						OwnerReferences: []metav1.OwnerReference{
							*additionalOwnerRef,
						},
					},
				},
			},
			wantRemovedInMemberClusterObjs: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName0,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme, tc.inMemberClusterObjs...)
			r := &Reconciler{
				spokeDynamicClient: fakeClient,
				parallelizer:       parallelizer.NewParallelizer(2),
			}
			if err := r.removeLeftOverManifests(ctx, tc.leftOverManifests, appliedWorkOwnerRef); err != nil {
				t.Errorf("removeLeftOverManifests() = %v, want no error", err)
			}

			for idx := range tc.wantInMemberClusterObjs {
				wantNS := tc.wantInMemberClusterObjs[idx]

				gotUnstructured, err := fakeClient.
					Resource(nsGVR).
					Namespace(wantNS.GetNamespace()).
					Get(ctx, wantNS.GetName(), metav1.GetOptions{})
				if err != nil {
					t.Errorf("Get Namespace(%v) = %v, want no error", klog.KObj(&wantNS), err)
					continue
				}

				gotNS := wantNS.DeepCopy()
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(gotUnstructured.Object, &gotNS); err != nil {
					t.Errorf("FromUnstructured() = %v, want no error", err)
				}

				if diff := cmp.Diff(gotNS, &wantNS, ignoreFieldTypeMetaInNamespace); diff != "" {
					t.Errorf("NS(%v) mismatches (-got +want):\n%s", klog.KObj(&wantNS), diff)
				}
			}

			for idx := range tc.wantRemovedInMemberClusterObjs {
				wantRemovedNS := tc.wantRemovedInMemberClusterObjs[idx]

				gotUnstructured, err := fakeClient.
					Resource(nsGVR).
					Namespace(wantRemovedNS.GetNamespace()).
					Get(ctx, wantRemovedNS.GetName(), metav1.GetOptions{})
				if err != nil {
					t.Errorf("Get Namespace(%v) = %v, want no error", klog.KObj(&wantRemovedNS), err)
				}

				gotRemovedNS := wantRemovedNS.DeepCopy()
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(gotUnstructured.Object, &gotRemovedNS); err != nil {
					t.Errorf("FromUnstructured() = %v, want no error", err)
				}

				if !gotRemovedNS.DeletionTimestamp.IsZero() {
					t.Errorf("Namespace(%v) has not been deleted", klog.KObj(&wantRemovedNS))
				}
			}
		})
	}
}

// TestRemoveOneLeftOverManifest tests the removeOneLeftOverManifest method.
func TestRemoveOneLeftOverManifest(t *testing.T) {
	ctx := context.Background()
	now := metav1.Now().Rfc3339Copy()
	leftOverManifest := fleetv1beta1.AppliedResourceMeta{
		WorkResourceIdentifier: *nsWRI(0, nsName),
	}
	additionalOwnerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "SuperNamespace",
		Name:       "super-ns",
		UID:        "super-ns-uid",
	}

	testCases := []struct {
		name string
		// To simplify things, for this test Fleet uses a fixed concrete type.
		inMemberClusterObj     *corev1.Namespace
		wantInMemberClusterObj *corev1.Namespace
	}{
		{
			name: "not found",
		},
		{
			name: "already deleted",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              nsName,
					DeletionTimestamp: &now,
				},
			},
			wantInMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              nsName,
					DeletionTimestamp: &now,
				},
			},
		},
		{
			name: "not derived from manifest object",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						*additionalOwnerRef,
					},
				},
			},
			wantInMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						*additionalOwnerRef,
					},
				},
			},
		},
		{
			name: "multiple owners",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						*additionalOwnerRef,
						*appliedWorkOwnerRef,
					},
				},
			},
			wantInMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						*additionalOwnerRef,
					},
				},
			},
		},
		{
			name: "deletion",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						*appliedWorkOwnerRef,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var fakeClient *fake.FakeDynamicClient
			if tc.inMemberClusterObj != nil {
				fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme, tc.inMemberClusterObj)
			} else {
				fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme)
			}

			r := &Reconciler{
				spokeDynamicClient: fakeClient,
			}
			if err := r.removeOneLeftOverManifest(ctx, leftOverManifest, appliedWorkOwnerRef); err != nil {
				t.Errorf("removeOneLeftOverManifest() = %v, want no error", err)
			}

			if tc.inMemberClusterObj != nil {
				var gotUnstructured *unstructured.Unstructured
				var err error
				// The method is expected to modify the object.
				gotUnstructured, err = fakeClient.
					Resource(nsGVR).
					Namespace(tc.inMemberClusterObj.GetNamespace()).
					Get(ctx, tc.inMemberClusterObj.GetName(), metav1.GetOptions{})
				switch {
				case errors.IsNotFound(err) && tc.wantInMemberClusterObj == nil:
					// The object is expected to be deleted.
					return
				case errors.IsNotFound(err):
					// An object is expected to be found.
					t.Errorf("Get(%v) = %v, want no error", klog.KObj(tc.inMemberClusterObj), err)
					return
				case err != nil:
					// An unexpected error occurred.
					t.Errorf("Get(%v) = %v, want no error", klog.KObj(tc.inMemberClusterObj), err)
					return
				}

				got := tc.wantInMemberClusterObj.DeepCopy()
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(gotUnstructured.Object, &got); err != nil {
					t.Errorf("FromUnstructured() = %v, want no error", err)
					return
				}

				if diff := cmp.Diff(got, tc.wantInMemberClusterObj, ignoreFieldTypeMetaInNamespace); diff != "" {
					t.Errorf("NS(%v) mismatches (-got +want):\n%s", klog.KObj(tc.inMemberClusterObj), diff)
				}
				return
			}
		})
	}
}

// TestPrepareExistingManifestCondQIdx tests the prepareExistingManifestCondQIdx function.
func TestPrepareExistingManifestCondQIdx(t *testing.T) {
	testCases := []struct {
		name                  string
		existingManifestConds []fleetv1beta1.ManifestCondition
		wantQIdx              map[string]int
	}{
		{
			name: "mixed",
			existingManifestConds: []fleetv1beta1.ManifestCondition{
				{
					Identifier: *nsWRI(0, nsName),
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
				},
				{
					Identifier: *deployWRI(2, nsName, deployName),
				},
			},
			wantQIdx: map[string]int{
				fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName):                    0,
				fmt.Sprintf("GV=apps/v1, Kind=Deployment, Namespace=%s, Name=%s", nsName, deployName): 2,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			qIdx := prepareExistingManifestCondQIdx(tc.existingManifestConds)
			if diff := cmp.Diff(qIdx, tc.wantQIdx); diff != "" {
				t.Errorf("prepareExistingManifestCondQIdx() mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestPrepareManifestCondForWA tests the prepareManifestCondForWA function.
func TestPrepareManifestCondForWA(t *testing.T) {
	workGeneration := int64(0)

	testCases := []struct {
		name                     string
		wriStr                   string
		wri                      *fleetv1beta1.WorkResourceIdentifier
		workGeneration           int64
		existingManifestCondQIdx map[string]int
		existingManifestConds    []fleetv1beta1.ManifestCondition
		wantManifestCondForWA    *fleetv1beta1.ManifestCondition
	}{
		{
			name:           "match found",
			wriStr:         fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName),
			wri:            nsWRI(0, nsName),
			workGeneration: workGeneration,
			existingManifestCondQIdx: map[string]int{
				fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName): 0,
			},
			existingManifestConds: []fleetv1beta1.ManifestCondition{
				{
					Identifier: *nsWRI(0, nsName),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration, metav1.ConditionTrue, string(ManifestProcessingApplyResultTypeApplied), ManifestProcessingApplyResultTypeAppliedDescription),
					},
				},
			},
			wantManifestCondForWA: &fleetv1beta1.ManifestCondition{
				Identifier: *nsWRI(0, nsName),
				Conditions: []metav1.Condition{
					manifestAppliedCond(workGeneration, metav1.ConditionTrue, string(ManifestProcessingApplyResultTypeApplied), ManifestProcessingApplyResultTypeAppliedDescription),
				},
			},
		},
		{
			name:           "match not found",
			wriStr:         fmt.Sprintf("GV=apps/v1, Kind=Deployment, Namespace=%s, Name=%s", nsName, deployName),
			wri:            deployWRI(1, nsName, deployName),
			workGeneration: workGeneration,
			wantManifestCondForWA: &fleetv1beta1.ManifestCondition{
				Identifier: *deployWRI(1, nsName, deployName),
				Conditions: []metav1.Condition{
					manifestAppliedCond(workGeneration, metav1.ConditionFalse, ManifestAppliedCondPreparingToProcessReason, ManifestAppliedCondPreparingToProcessMessage),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manifestCondForWA := prepareManifestCondForWriteAhead(tc.wriStr, tc.wri, tc.workGeneration, tc.existingManifestCondQIdx, tc.existingManifestConds)
			if diff := cmp.Diff(&manifestCondForWA, tc.wantManifestCondForWA, ignoreFieldConditionLTTMsg); diff != "" {
				t.Errorf("prepareManifestCondForWA() mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestFindLeftOverManifests tests the findLeftOverManifests function.
func TestFindLeftOverManifests(t *testing.T) {
	workGeneration0 := int64(0)
	workGeneration1 := int64(1)

	nsName0 := fmt.Sprintf(nsNameTemplate, "0")
	nsName1 := fmt.Sprintf(nsNameTemplate, "1")
	nsName2 := fmt.Sprintf(nsNameTemplate, "2")
	nsName3 := fmt.Sprintf(nsNameTemplate, "3")
	nsName4 := fmt.Sprintf(nsNameTemplate, "4")

	testCases := []struct {
		name                       string
		manifestCondsForWA         []fleetv1beta1.ManifestCondition
		existingManifestCondQIdx   map[string]int
		existingManifestConditions []fleetv1beta1.ManifestCondition
		wantLeftOverManifests      []fleetv1beta1.AppliedResourceMeta
	}{
		{
			name: "mixed",
			manifestCondsForWA: []fleetv1beta1.ManifestCondition{
				// New manifest.
				{
					Identifier: *nsWRI(0, nsName0),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration1, metav1.ConditionFalse, ManifestAppliedCondPreparingToProcessReason, ManifestAppliedCondPreparingToProcessMessage),
					},
				},
				// Existing manifest.
				{
					Identifier: *nsWRI(1, nsName1),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration0, metav1.ConditionTrue, string(ManifestProcessingApplyResultTypeApplied), ManifestProcessingApplyResultTypeAppliedDescription),
					},
				},
			},
			existingManifestConditions: []fleetv1beta1.ManifestCondition{
				// Manifest condition that signals a decoding error.
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
				},
				// Manifest condition that corresponds to the existing manifest.
				{
					Identifier: *nsWRI(1, nsName1),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration0, metav1.ConditionTrue, string(ManifestProcessingApplyResultTypeApplied), ManifestProcessingApplyResultTypeAppliedDescription),
					},
				},
				// Manifest condition that corresponds to a previously applied and now gone manifest.
				{
					Identifier: *nsWRI(2, nsName2),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration0, metav1.ConditionTrue, string(ManifestProcessingApplyResultTypeApplied), ManifestProcessingApplyResultTypeAppliedDescription),
					},
				},
				// Manifest condition that corresponds to a gone manifest that failed to be applied.
				{
					Identifier: *nsWRI(3, nsName3),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration0, metav1.ConditionFalse, string(ManifestProcessingApplyResultTypeFailedToApply), ""),
					},
				},
				// Manifest condition that corresponds to a gone manifest that has been marked as to be applied (preparing to be processed).
				{
					Identifier: *nsWRI(4, nsName4),
					Conditions: []metav1.Condition{
						manifestAppliedCond(workGeneration0, metav1.ConditionFalse, ManifestAppliedCondPreparingToProcessReason, ManifestAppliedCondPreparingToProcessMessage),
					},
				},
			},
			existingManifestCondQIdx: map[string]int{
				fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName1): 1,
				fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName2): 2,
				fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName3): 3,
				fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, Name=%s", nsName4): 4,
			},
			wantLeftOverManifests: []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: *nsWRI(2, nsName2),
				},
				{
					WorkResourceIdentifier: *nsWRI(4, nsName4),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			leftOverManifests := findLeftOverManifests(tc.manifestCondsForWA, tc.existingManifestCondQIdx, tc.existingManifestConditions)
			if diff := cmp.Diff(leftOverManifests, tc.wantLeftOverManifests, cmpopts.SortSlices(lessFuncAppliedResourceMeta)); diff != "" {
				t.Errorf("findLeftOverManifests() mismatches (-got +want):\n%s", diff)
			}
		})
	}
}
