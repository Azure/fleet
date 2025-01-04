/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"crypto/rand"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note (chenyu1): The fake client Fleet uses for unit tests has trouble processing certain requests
// at the moment; affected test cases will be covered in the integration tests (w/ real clients) instead.

// TestSanitizeManifestObject tests the sanitizeManifestObject function.
func TestSanitizeManifestObject(t *testing.T) {
	// This test spec uses a Deployment object as the target.
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
			Name:      deployName,
			Namespace: nsName,
			Annotations: map[string]string{
				fleetv1beta1.ManifestHashAnnotation:      dummyLabelValue1,
				fleetv1beta1.LastAppliedConfigAnnotation: dummyLabelValue1,
				corev1.LastAppliedConfigAnnotation:       dummyLabelValue1,
				dummyLabelKey:                            dummyLabelValue1,
			},
			Labels: map[string]string{
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
			Name:      deployName,
			Namespace: nsName,
			Annotations: map[string]string{
				dummyLabelKey: dummyLabelValue1,
			},
			Labels: map[string]string{
				dummyLabelKey: dummyLabelValue2,
			},
			Finalizers: []string{
				dummyLabelKey,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
		},
		Status: appsv1.DeploymentStatus{},
	}

	testCases := []struct {
		name             string
		manifestObj      *unstructured.Unstructured
		wantSanitizedObj *unstructured.Unstructured
	}{
		{
			name:             "deploy",
			manifestObj:      toUnstructured(t, deploy),
			wantSanitizedObj: toUnstructured(t, wantDeploy),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotSanitizedObj := sanitizeManifestObject(tc.manifestObj)

			// There are certain fields that need to be set manually as they might got omitted
			// when being cast to an Unstructured object.
			tc.wantSanitizedObj.SetCreationTimestamp(metav1.Time{})
			tc.wantSanitizedObj.SetManagedFields(nil)
			tc.wantSanitizedObj.SetOwnerReferences(nil)
			unstructured.RemoveNestedField(tc.wantSanitizedObj.Object, "status")

			if diff := cmp.Diff(gotSanitizedObj, tc.wantSanitizedObj); diff != "" {
				t.Errorf("sanitized obj mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestValidateOwnerReferences tests the validateOwnerReferences function.
func TestValidateOwnerReferences(t *testing.T) {
	deployManifestObj1 := deploy.DeepCopy()
	deployManifestObj1.OwnerReferences = []metav1.OwnerReference{
		dummyOwnerRef,
	}
	deployInMemberClusterObj1 := deploy.DeepCopy()

	deployManifestObj2 := deploy.DeepCopy()
	deployManifestObj2.OwnerReferences = []metav1.OwnerReference{
		*appliedWorkOwnerRef,
	}
	deployInMemberClusterObj2 := deploy.DeepCopy()

	deployManifestObj3 := deploy.DeepCopy()

	deployManifestObj4 := deploy.DeepCopy()
	deployInMemberClusterObj4 := deploy.DeepCopy()

	deployManifestObj5 := deploy.DeepCopy()
	deployInMemberClusterObj5 := deploy.DeepCopy()
	deployInMemberClusterObj5.OwnerReferences = []metav1.OwnerReference{
		dummyOwnerRef,
		*appliedWorkOwnerRef,
	}

	deployManifestObj6 := deploy.DeepCopy()
	deployInMemberClusterObj6 := deploy.DeepCopy()
	deployInMemberClusterObj6.OwnerReferences = []metav1.OwnerReference{
		*appliedWorkOwnerRef,
		{
			APIVersion: "placement.kubernetes-fleet.io/v1beta1",
			Kind:       "AppliedWork",
			Name:       "work-2",
			UID:        "uid-2",
		},
	}

	deployManifestObj7 := deploy.DeepCopy()
	deployInMemberClusterObj7 := deploy.DeepCopy()
	deployInMemberClusterObj7.OwnerReferences = []metav1.OwnerReference{
		*appliedWorkOwnerRef,
		dummyOwnerRef,
	}

	deployManifestObj8 := deploy.DeepCopy()
	deployInMemberClusterObj8 := deploy.DeepCopy()
	deployInMemberClusterObj8.OwnerReferences = []metav1.OwnerReference{
		*appliedWorkOwnerRef,
	}

	testCases := []struct {
		name               string
		manifestObj        *unstructured.Unstructured
		inMemberClusterObj *unstructured.Unstructured
		applyStrategy      *fleetv1beta1.ApplyStrategy
		wantErred          bool
		wantErrMsgSubStr   string
	}{
		{
			name:               "multiple owners set on manifest, co-ownership is not allowed",
			manifestObj:        toUnstructured(t, deployManifestObj1),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj1),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: false,
			},
			wantErred:        true,
			wantErrMsgSubStr: "manifest is set to have multiple owner references but co-ownership is disallowed",
		},
		{
			name:               "unexpected AppliedWork owner ref",
			manifestObj:        toUnstructured(t, deployManifestObj2),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj2),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: true,
			},
			wantErred:        true,
			wantErrMsgSubStr: "an AppliedWork object is unexpectedly added as an owner",
		},
		{
			name:        "not created yet",
			manifestObj: toUnstructured(t, deployManifestObj3),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: true,
			},
			wantErred: false,
		},
		{
			name:               "not taken over yet",
			manifestObj:        toUnstructured(t, deployManifestObj4),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj4),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: false,
			},
			wantErred:        true,
			wantErrMsgSubStr: "object is not owned by the expected AppliedWork object",
		},
		{
			name:               "multiple owners set on applied object, co-ownership is not allowed",
			manifestObj:        toUnstructured(t, deployManifestObj5),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj5),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: false,
			},
			wantErred:        true,
			wantErrMsgSubStr: "object is co-owned by multiple objects but co-ownership has been disallowed",
		},
		{
			name:               "placed by Fleet in duplication",
			manifestObj:        toUnstructured(t, deployManifestObj6),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj6),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: true,
			},
			wantErred:        true,
			wantErrMsgSubStr: "object is already owned by another AppliedWork object",
		},
		{
			name:               "regular (multiple owners, co-ownership is allowed)",
			manifestObj:        toUnstructured(t, deployManifestObj7),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj7),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: true,
			},
			wantErred: false,
		},
		{
			name:               "regular (single owner, co-ownership is not allowed)",
			manifestObj:        toUnstructured(t, deployManifestObj8),
			inMemberClusterObj: toUnstructured(t, deployInMemberClusterObj8),
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				AllowCoOwnership: false,
			},
			wantErred: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOwnerReferences(tc.manifestObj, tc.inMemberClusterObj, tc.applyStrategy, appliedWorkOwnerRef)
			switch {
			case tc.wantErred && err == nil:
				t.Fatalf("validateOwnerReferences() = nil, want error")
			case !tc.wantErred && err != nil:
				t.Fatalf("validateOwnerReferences() = %v, want no error", err)
			case tc.wantErred && err != nil && !strings.Contains(err.Error(), tc.wantErrMsgSubStr):
				t.Fatalf("validateOwnerReferences() = %v, want error message with sub-string %s", err, tc.wantErrMsgSubStr)
			}
		})
	}
}

// TestSetManifestHashAnnotation tests the setManifestHashAnnotation function.
func TestSetManifestHashAnnotation(t *testing.T) {
	nsManifestObj := ns.DeepCopy()
	wantNSManifestObj := ns.DeepCopy()
	wantNSManifestObj.Annotations = map[string]string{
		fleetv1beta1.ManifestHashAnnotation: string("08a19eb5a085293fdc5b9e252422e44002e5cfeea1ae3cb303ce1a6537d97c69"),
	}

	testCases := []struct {
		name            string
		manifestObj     *unstructured.Unstructured
		wantManifestObj *unstructured.Unstructured
	}{
		{
			name:            "namespace",
			manifestObj:     toUnstructured(t, nsManifestObj),
			wantManifestObj: toUnstructured(t, wantNSManifestObj),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := setManifestHashAnnotation(tc.manifestObj); err != nil {
				t.Fatalf("setManifestHashAnnotation() = %v, want no error", err)
			}

			if diff := cmp.Diff(tc.manifestObj, tc.wantManifestObj); diff != "" {
				t.Errorf("manifest obj mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestGetFleetLastAppliedAnnotation tests the getFleetLastAppliedAnnotation function.
func TestGetFleetLastAppliedAnnotation(t *testing.T) {
	nsInMemberClusterObj1 := ns.DeepCopy()

	nsInMemberClusterObj2 := ns.DeepCopy()
	nsInMemberClusterObj2.SetAnnotations(map[string]string{
		dummyLabelKey: dummyLabelValue1,
	})

	nsInMemberClusterObj3 := ns.DeepCopy()
	nsInMemberClusterObj3.SetAnnotations(map[string]string{
		fleetv1beta1.LastAppliedConfigAnnotation: dummyLabelValue1,
	})

	testCases := []struct {
		name                             string
		inMemberClusterObj               *unstructured.Unstructured
		wantLastAppliedManifestJSONBytes []byte
	}{
		{
			name:               "no annotations",
			inMemberClusterObj: toUnstructured(t, nsInMemberClusterObj1),
		},
		{
			name:               "no last applied annotation",
			inMemberClusterObj: toUnstructured(t, nsInMemberClusterObj2),
		},
		{
			name:                             "last applied annotation set",
			inMemberClusterObj:               toUnstructured(t, nsInMemberClusterObj3),
			wantLastAppliedManifestJSONBytes: []byte(dummyLabelValue1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotBytes := getFleetLastAppliedAnnotation(tc.inMemberClusterObj)
			if diff := cmp.Diff(gotBytes, tc.wantLastAppliedManifestJSONBytes); diff != "" {
				t.Errorf("last applied manifest JSON bytes mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

// TestSetFleetLastAppliedAnnotation tests the setFleetLastAppliedAnnotation function.
func TestSetFleetLastAppliedAnnotation(t *testing.T) {
	nsManifestObj1 := ns.DeepCopy()
	wantNSManifestObj1 := ns.DeepCopy()
	wantNSManifestObj1.SetAnnotations(map[string]string{
		fleetv1beta1.LastAppliedConfigAnnotation: string("{\"apiVersion\":\"v1\",\"kind\":\"Namespace\",\"metadata\":{\"annotations\":{},\"creationTimestamp\":null,\"name\":\"ns-1\"},\"spec\":{},\"status\":{}}\n"),
	})

	nsManifestObj2 := ns.DeepCopy()
	nsManifestObj2.SetAnnotations(map[string]string{
		fleetv1beta1.LastAppliedConfigAnnotation: dummyLabelValue2,
	})
	wantNSManifestObj2 := ns.DeepCopy()
	wantNSManifestObj2.SetAnnotations(map[string]string{
		fleetv1beta1.LastAppliedConfigAnnotation: string("{\"apiVersion\":\"v1\",\"kind\":\"Namespace\",\"metadata\":{\"annotations\":{},\"creationTimestamp\":null,\"name\":\"ns-1\"},\"spec\":{},\"status\":{}}\n"),
	})

	// Annotation size limit is 262144 bytes.
	longDataBytes := make([]byte, 300000)
	_, err := rand.Read(longDataBytes)
	if err != nil {
		t.Fatalf("failed to generate random bytes: %v", err)
	}
	configMapManifestObj3 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: nsName,
		},
		Data: map[string]string{
			"data": string(longDataBytes),
		},
	}
	wantConfigMapManifestObj3 := configMapManifestObj3.DeepCopy()
	wantConfigMapManifestObj3.SetAnnotations(map[string]string{
		fleetv1beta1.LastAppliedConfigAnnotation: "",
	})

	testCases := []struct {
		name            string
		manifestObj     *unstructured.Unstructured
		wantManifestObj *unstructured.Unstructured
		wantSetFlag     bool
	}{
		{
			name:            "last applied annotation set",
			manifestObj:     toUnstructured(t, nsManifestObj1),
			wantManifestObj: toUnstructured(t, wantNSManifestObj1),
			wantSetFlag:     true,
		},
		{
			name:            "last applied annotation replaced",
			manifestObj:     toUnstructured(t, nsManifestObj2),
			wantManifestObj: toUnstructured(t, wantNSManifestObj2),
			wantSetFlag:     true,
		},
		{
			name:            "last applied annotation not set (annotation oversized)",
			manifestObj:     toUnstructured(t, configMapManifestObj3),
			wantManifestObj: toUnstructured(t, wantConfigMapManifestObj3),
			wantSetFlag:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotSetFlag, err := setFleetLastAppliedAnnotation(tc.manifestObj)
			if err != nil {
				t.Fatalf("setFleetLastAppliedAnnotation() = %v, want no error", err)
			}

			if gotSetFlag != tc.wantSetFlag {
				t.Errorf("isLastAppliedAnnotationSet flag mismatches, got %t, want %t", gotSetFlag, tc.wantSetFlag)
			}
			if diff := cmp.Diff(tc.manifestObj, tc.wantManifestObj); diff != "" {
				t.Errorf("manifest obj mismatches (-got +want):\n%s", diff)
			}
		})
	}
}
