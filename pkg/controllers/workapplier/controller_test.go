/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/parallelizer"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
)

const (
	workName = "work-1"

	deployName    = "deploy-1"
	configMapName = "configmap-1"
	nsName        = "ns-1"
)

var (
	appliedWorkOwnerRef = &metav1.OwnerReference{
		APIVersion: "placement.kubernetes-fleet.io/v1beta1",
		Kind:       "AppliedWork",
		Name:       workName,
		UID:        "0",
	}
)

var (
	ignoreFieldTypeMetaInNamespace = cmpopts.IgnoreFields(corev1.Namespace{}, "TypeMeta")
)

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

// TestFindInMemberClusterObjectFor tests the findInMemberClusterObjectFor method.
func TestFindInMemberClusterObjectFor(t *testing.T) {
	ctx := context.Background()

	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns-2",
		},
	}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      configMapName,
		},
		Data: map[string]string{
			"key": "value",
		},
	}
	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns-2",
			Name:      "configmap-2",
		},
	}

	inMemberClusterObjs := []runtime.Object{
		ns1,
		cm1,
	}

	ns1UnstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ns1)
	if err != nil {
		t.Fatalf("Namespace ToUnstructured() = %v, want no error", err)
	}
	ns1Unstructured := &unstructured.Unstructured{Object: ns1UnstructuredMap}
	ns1Unstructured.SetAPIVersion("v1")
	ns1Unstructured.SetKind("Namespace")

	ns2UnstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ns2)
	if err != nil {
		t.Fatalf("Namespace ToUnstructured() = %v, want no error", err)
	}
	ns2Unstrucutred := &unstructured.Unstructured{Object: ns2UnstructuredMap}

	cm1UnstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm1)
	if err != nil {
		t.Fatalf("ConfigMap ToUnstructured() = %v, want no error", err)
	}
	cm1Unstructured := &unstructured.Unstructured{Object: cm1UnstructuredMap}
	cm1Unstructured.SetAPIVersion("v1")
	cm1Unstructured.SetKind("ConfigMap")

	cm2UnstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm2)
	if err != nil {
		t.Fatalf("ConfigMap ToUnstructured() = %v, want no error", err)
	}
	cm2Unstructured := &unstructured.Unstructured{Object: cm2UnstructuredMap}

	testCases := []struct {
		name                   string
		gvr                    *schema.GroupVersionResource
		manifestObj            *unstructured.Unstructured
		wantInMemberClusterObj *unstructured.Unstructured
	}{
		{
			name:                   "found (namespace scoped)",
			gvr:                    &nsGVR,
			manifestObj:            ns1Unstructured,
			wantInMemberClusterObj: ns1Unstructured,
		},
		{
			name:                   "found (cluster scoped)",
			gvr:                    &cmGVR,
			manifestObj:            cm1Unstructured,
			wantInMemberClusterObj: cm1Unstructured,
		},
		{
			name:        "not found (namespace scoped)",
			gvr:         &nsGVR,
			manifestObj: cm2Unstructured,
		},
		{
			name:        "not found (cluster scoped)",
			gvr:         &cmGVR,
			manifestObj: ns2Unstrucutred,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleDynamicClient(scheme.Scheme, inMemberClusterObjs...)
			r := &Reconciler{
				spokeDynamicClient: fakeClient,
			}
			got, err := r.findInMemberClusterObjectFor(ctx, tc.gvr, tc.manifestObj)
			if err != nil {
				t.Errorf("findInMemberClusterObjectFor() = %v, want no error", err)
			}

			if diff := cmp.Diff(got, tc.wantInMemberClusterObj); diff != "" {
				t.Errorf("findInMemberClusterObjectFor() mismatches (-got +want):\n%s", diff)
			}
		})
	}
}
