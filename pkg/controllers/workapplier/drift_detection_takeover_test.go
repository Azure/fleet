/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/wI2L/jsondiff"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var (
	svcName = "web"

	svc = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      svcName,
		},
		Spec: corev1.ServiceSpec{},
	}
)

func toUnstructured(t *testing.T, obj runtime.Object) *unstructured.Unstructured {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("failed to convert obj into a map: %v", err)
	}
	return &unstructured.Unstructured{Object: unstructuredObj}
}

// Note (chenyu1): The fake client Fleet uses for unit tests has trouble processing certain
// requests at the moment; affected test cases will be covered in
// integration tests (w/ real clients) instead.

// TestTakeOverPreExistingObject tests the takeOverPreExistingObject method.
func TestTakeOverPreExistingObject(t *testing.T) {
	ctx := context.Background()

	nsWithNonFleetOwnerUnstructured := nsUnstructured.DeepCopy()
	nsWithNonFleetOwnerUnstructured.SetOwnerReferences([]metav1.OwnerReference{
		dummyOwnerRef,
	})

	nsWithFleetOwnerUnstructured := nsUnstructured.DeepCopy()
	nsWithFleetOwnerUnstructured.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "placement.kubernetes-fleet.io/v1beta1",
			Kind:       "AppliedWork",
			Name:       "dummy-work",
			UID:        "0987-6543-21",
		},
	})

	wantTakenOverObj := nsUnstructured.DeepCopy()
	wantTakenOverObj.SetOwnerReferences([]metav1.OwnerReference{
		*appliedWorkOwnerRef,
	})

	wantTakenOverObjWithAdditionalNonFleetOwner := nsUnstructured.DeepCopy()
	wantTakenOverObjWithAdditionalNonFleetOwner.SetOwnerReferences([]metav1.OwnerReference{
		dummyOwnerRef,
		*appliedWorkOwnerRef,
	})

	nsWithLabelsUnstructured := nsUnstructured.DeepCopy()
	nsWithLabelsUnstructured.SetLabels(map[string]string{
		dummyLabelKey: dummyLabelValue1,
	})

	testCases := []struct {
		name                        string
		gvr                         *schema.GroupVersionResource
		manifestObj                 *unstructured.Unstructured
		inMemberClusterObj          *unstructured.Unstructured
		workObj                     *fleetv1beta1.Work
		applyStrategy               *fleetv1beta1.ApplyStrategy
		expectedAppliedWorkOwnerRef *metav1.OwnerReference
		wantErred                   bool
		wantTakeOverObj             *unstructured.Unstructured
		wantPatchDetails            []fleetv1beta1.PatchDetail
	}{
		{
			name:               "existing non-Fleet owner, co-ownership not allowed",
			gvr:                &nsGVR,
			manifestObj:        nsUnstructured,
			inMemberClusterObj: nsWithNonFleetOwnerUnstructured,
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
				AllowCoOwnership: false,
			},
			expectedAppliedWorkOwnerRef: appliedWorkOwnerRef,
			wantErred:                   true,
		},
		{
			name:               "existing Fleet owner, co-ownership allowed",
			gvr:                &nsGVR,
			manifestObj:        nsUnstructured,
			inMemberClusterObj: nsWithFleetOwnerUnstructured,
			workObj: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dummy-work",
					Namespace: memberReservedNSName,
				},
			},
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
				AllowCoOwnership: true,
			},
			expectedAppliedWorkOwnerRef: appliedWorkOwnerRef,
			wantErred:                   true,
		},
		{
			name:               "no owner, always take over",
			gvr:                &nsGVR,
			manifestObj:        nsUnstructured,
			inMemberClusterObj: nsUnstructured,
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver: fleetv1beta1.WhenToTakeOverTypeAlways,
			},
			expectedAppliedWorkOwnerRef: appliedWorkOwnerRef,
			wantTakeOverObj:             wantTakenOverObj,
		},
		{
			name:               "existing non-Fleet owner,co-ownership allowed, always take over",
			gvr:                &nsGVR,
			manifestObj:        nsUnstructured,
			inMemberClusterObj: nsWithNonFleetOwnerUnstructured,
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeAlways,
				AllowCoOwnership: true,
			},
			expectedAppliedWorkOwnerRef: appliedWorkOwnerRef,
			wantTakeOverObj:             wantTakenOverObjWithAdditionalNonFleetOwner,
		},
		// The fake client Fleet uses for unit tests has trouble processing dry-run requests (server
		// side apply); such test cases will be handled in integration tests instead.
		{
			name:               "no owner, take over if no diff, diff found, full comparison",
			gvr:                &nsGVR,
			manifestObj:        nsUnstructured,
			inMemberClusterObj: nsWithLabelsUnstructured,
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
			},
			expectedAppliedWorkOwnerRef: appliedWorkOwnerRef,
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/metadata/labels/foo",
					ValueInMember: "bar",
				},
			},
		},
		{
			name:               "no owner, take over if no diff, no diff",
			gvr:                &nsGVR,
			manifestObj:        nsUnstructured,
			inMemberClusterObj: nsUnstructured,
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
			},
			expectedAppliedWorkOwnerRef: appliedWorkOwnerRef,
			wantTakeOverObj:             wantTakenOverObj,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClientBuilder := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.workObj != nil {
				fakeHubClientBuilder = fakeHubClientBuilder.WithObjects(tc.workObj)
			}
			fakeHubClient := fakeHubClientBuilder.Build()
			fakeMemberClient := fake.NewSimpleDynamicClient(scheme.Scheme, tc.inMemberClusterObj)
			r := &Reconciler{
				hubClient:          fakeHubClient,
				spokeDynamicClient: fakeMemberClient,
				workNameSpace:      memberReservedNSName,
			}

			takenOverObj, patchDetails, err := r.takeOverPreExistingObject(
				ctx,
				tc.gvr,
				tc.manifestObj, tc.inMemberClusterObj,
				tc.applyStrategy,
				tc.expectedAppliedWorkOwnerRef)
			if tc.wantErred {
				if err == nil {
					t.Errorf("takeOverPreExistingObject() = nil, want erred")
				}
				return
			}

			if err != nil {
				t.Errorf("takeOverPreExistingObject() = %v, want no error", err)
			}
			if diff := cmp.Diff(takenOverObj, tc.wantTakeOverObj); diff != "" {
				t.Errorf("takenOverObject mismatches (-got, +want):\n%s", diff)
			}
			if diff := cmp.Diff(patchDetails, tc.wantPatchDetails); diff != "" {
				t.Errorf("patchDetails mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestPreparePatchDetails tests the preparePatchDetails function.
func TestPreparePatchDetails(t *testing.T) {
	deploy1Manifest := deploy.DeepCopy()
	deploy1Manifest.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType

	deploy2Manifest := deploy.DeepCopy()
	deploy2Manifest.Spec.RevisionHistoryLimit = ptr.To(int32(10))

	deploy3Manifest := deploy.DeepCopy()
	deploy3Manifest.Spec.Paused = true

	deploy4Manifest := deploy.DeepCopy()
	deploy4Manifest.Spec.Selector.MatchLabels = map[string]string{
		"app":  "envoy",
		"team": "red",
	}

	svc1Manifest := svc.DeepCopy()
	svc1Manifest.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(8080),
		},
	}

	svc2Manifest := svc.DeepCopy()
	svc2Manifest.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(8080),
		},
		{
			Name:       "custom",
			Protocol:   corev1.ProtocolUDP,
			Port:       10000,
			TargetPort: intstr.FromInt(10000),
		},
		{
			Name:       "https",
			Protocol:   corev1.ProtocolTCP,
			Port:       443,
			TargetPort: intstr.FromString("https"),
		},
	}
	svc2InMember := svc.DeepCopy()
	svc2InMember.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(8080),
		},
		{
			Name:       "https",
			Protocol:   corev1.ProtocolTCP,
			Port:       443,
			TargetPort: intstr.FromString("https"),
		},
	}

	svc3Manifest := svc.DeepCopy()
	svc3Manifest.Spec.Selector = map[string]string{
		"app": "nginx",
	}

	svc4Manifest := svc.DeepCopy()
	svc4Manifest.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(8080),
		},
		{
			Name:       "https",
			Protocol:   corev1.ProtocolTCP,
			Port:       443,
			TargetPort: intstr.FromString("https"),
		},
	}
	svc4InMember := svc4Manifest.DeepCopy()
	svc4InMember.Spec.Ports[0].Port = 8080
	svc4InMember.Spec.Ports[1].TargetPort = intstr.FromInt(8443)

	testCases := []struct {
		name               string
		manifestObj        *unstructured.Unstructured
		inMemberClusterObj *unstructured.Unstructured
		wantPatchDetails   []fleetv1beta1.PatchDetail
	}{
		{
			name:               "field addition, object field (string)",
			manifestObj:        toUnstructured(t, deploy1Manifest),
			inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/spec/strategy/type",
					ValueInHub: "Recreate",
				},
			},
		},
		{
			name:               "field addition, object field (numeral)",
			manifestObj:        toUnstructured(t, deploy2Manifest),
			inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/spec/revisionHistoryLimit",
					ValueInHub: "10",
				},
			},
		},
		{
			name:               "field addition, object field (bool)",
			manifestObj:        toUnstructured(t, deploy3Manifest),
			inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/spec/paused",
					ValueInHub: "true",
				},
			},
		},
		{
			name:               "field addition, array",
			manifestObj:        toUnstructured(t, svc1Manifest),
			inMemberClusterObj: toUnstructured(t, svc.DeepCopy()),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/spec/ports",
					ValueInHub: "[map[name:http port:80 protocol:TCP targetPort:8080]]",
				},
			},
		},
		{
			name:               "field addition, array item",
			manifestObj:        toUnstructured(t, svc2Manifest),
			inMemberClusterObj: toUnstructured(t, svc2InMember),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/spec/ports/2",
					ValueInHub: "map[name:https port:443 protocol:TCP targetPort:https]",
				},
				{
					Path:          "/spec/ports/1/name",
					ValueInMember: "https",
					ValueInHub:    "custom",
				},
				{
					Path:          "/spec/ports/1/port",
					ValueInMember: "443",
					ValueInHub:    "10000",
				},
				{
					Path:          "/spec/ports/1/protocol",
					ValueInMember: "TCP",
					ValueInHub:    "UDP",
				},
				{
					Path:          "/spec/ports/1/targetPort",
					ValueInMember: "https",
					ValueInHub:    "10000",
				},
			},
		},
		{
			name:               "field addition, object",
			manifestObj:        toUnstructured(t, svc3Manifest),
			inMemberClusterObj: toUnstructured(t, svc.DeepCopy()),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/spec/selector",
					ValueInHub: "map[app:nginx]",
				},
			},
		},
		{
			name:               "field addition, nested (array)",
			manifestObj:        toUnstructured(t, svc4Manifest),
			inMemberClusterObj: toUnstructured(t, svc4InMember),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/spec/ports/0/port",
					ValueInMember: "8080",
					ValueInHub:    "80",
				},
				{
					Path:          "/spec/ports/1/targetPort",
					ValueInMember: "8443",
					ValueInHub:    "https",
				},
			},
		},
		{
			name:               "field addition, nested (object)",
			manifestObj:        toUnstructured(t, deploy4Manifest),
			inMemberClusterObj: toUnstructured(t, deploy.DeepCopy()),
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/spec/selector/matchLabels/app",
					ValueInMember: "nginx",
					ValueInHub:    "envoy",
				},
				{
					Path:       "/spec/selector/matchLabels/team",
					ValueInHub: "red",
				},
			},
		},
		// TO-DO (chenyu1): add more test cases.
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			patchDetails, err := preparePatchDetails(tc.manifestObj, tc.inMemberClusterObj)
			if err != nil {
				t.Fatalf("preparePatchDetails() = %v, want no error", err)
			}

			if diff := cmp.Diff(patchDetails, tc.wantPatchDetails, cmpopts.SortSlices(lessFuncPatchDetail)); diff != "" {
				t.Fatalf("patchDetails mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestRemoveLeftBehindAppliedWorkOwnerRefs tests the removeLeftBehindAppliedWorkOwnerRefs method.
func TestRemoveLeftBehindAppliedWorkOwnerRefs(t *testing.T) {
	ctx := context.Background()

	orphanedAppliedWorkOwnerRef := metav1.OwnerReference{
		APIVersion: fleetv1beta1.GroupVersion.String(),
		Kind:       fleetv1beta1.AppliedWorkKind,
		Name:       "orphaned-work",
		UID:        "123-xyz-abcd",
	}

	testCases := []struct {
		name          string
		ownerRefs     []metav1.OwnerReference
		wantOwnerRefs []metav1.OwnerReference
		workObj       *fleetv1beta1.Work
	}{
		{
			name: "mixed",
			ownerRefs: []metav1.OwnerReference{
				dummyOwnerRef,
				*appliedWorkOwnerRef,
				orphanedAppliedWorkOwnerRef,
			},
			wantOwnerRefs: []metav1.OwnerReference{
				dummyOwnerRef,
				*appliedWorkOwnerRef,
			},
			workObj: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClientBuilder := ctrlfake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.workObj != nil {
				fakeHubClientBuilder = fakeHubClientBuilder.WithObjects(tc.workObj)
			}
			fakeHubClient := fakeHubClientBuilder.Build()

			r := &Reconciler{
				hubClient:     fakeHubClient,
				workNameSpace: memberReservedNSName,
			}

			gotOwnerRefs, err := r.removeLeftBehindAppliedWorkOwnerRefs(ctx, tc.ownerRefs)
			if err != nil {
				t.Fatalf("removeLeftBehindAppliedWorkOwnerRefs() = %v, want no error", err)
			}

			if diff := cmp.Diff(gotOwnerRefs, tc.wantOwnerRefs); diff != "" {
				t.Errorf("owner refs mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestOrganizeJSONPatchIntoFleetPatchDetails tests the organizeJSONPatchIntoFleetPatchDetails function.
func TestOrganizeJSONPatchIntoFleetPatchDetails(t *testing.T) {
	testCases := []struct {
		name             string
		patch            jsondiff.Patch
		manifestObjMap   map[string]interface{}
		wantPatchDetails []fleetv1beta1.PatchDetail
	}{
		{
			name: "add",
			patch: jsondiff.Patch{
				{
					Type:  jsondiff.OperationAdd,
					Path:  "/a",
					Value: "1",
				},
			},
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/a",
					ValueInMember: "1",
				},
			},
		},
		{
			name: "remove",
			patch: jsondiff.Patch{
				{
					Type: jsondiff.OperationRemove,
					Path: "/b",
				},
			},
			manifestObjMap: map[string]interface{}{
				"b": "2",
			},
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/b",
					ValueInHub: "2",
				},
			},
		},
		{
			name: "Replace",
			patch: jsondiff.Patch{
				{
					Type:  jsondiff.OperationReplace,
					Path:  "/c",
					Value: "3",
				},
			},
			manifestObjMap: map[string]interface{}{
				"c": "4",
			},
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/c",
					ValueInMember: "3",
					ValueInHub:    "4",
				},
			},
		},
		{
			name: "Move",
			patch: jsondiff.Patch{
				{
					Type: jsondiff.OperationMove,
					From: "/d",
					Path: "/e",
				},
			},
			manifestObjMap: map[string]interface{}{
				"d": "6",
			},
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:       "/d",
					ValueInHub: "6",
				},
				{
					Path:          "/e",
					ValueInMember: "6",
				},
			},
		},
		{
			name: "Copy",
			patch: jsondiff.Patch{
				{
					Type: jsondiff.OperationCopy,
					From: "/f",
					Path: "/g",
				},
			},
			manifestObjMap: map[string]interface{}{
				"f": "7",
			},
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/g",
					ValueInMember: "7",
				},
			},
		},
		{
			name: "Test",
			patch: jsondiff.Patch{
				{
					Type:  jsondiff.OperationTest,
					Path:  "/h",
					Value: "8",
				},
			},
			manifestObjMap:   map[string]interface{}{},
			wantPatchDetails: []fleetv1beta1.PatchDetail{},
		},
		{
			name: "Mixed",
			patch: jsondiff.Patch{
				{
					Type:  jsondiff.OperationAdd,
					Path:  "/a",
					Value: "1",
				},
				{
					Type: jsondiff.OperationRemove,
					Path: "/b",
				},
				{
					Type:  jsondiff.OperationReplace,
					Path:  "/c",
					Value: "3",
				},
				{
					Type: jsondiff.OperationMove,
					From: "/d",
					Path: "/e",
				},
				{
					Type: jsondiff.OperationCopy,
					From: "/f",
					Path: "/g",
				},
				{
					Type:  jsondiff.OperationTest,
					Path:  "/h",
					Value: "8",
				},
			},
			manifestObjMap: map[string]interface{}{
				"b": "2",
				"c": "4",
				"d": "6",
				"f": "7",
			},
			wantPatchDetails: []fleetv1beta1.PatchDetail{
				{
					Path:          "/a",
					ValueInMember: "1",
				},
				{
					Path:       "/b",
					ValueInHub: "2",
				},
				{
					Path:          "/c",
					ValueInMember: "3",
					ValueInHub:    "4",
				},
				{
					Path:       "/d",
					ValueInHub: "6",
				},
				{
					Path:          "/e",
					ValueInMember: "6",
				},
				{
					Path:          "/g",
					ValueInMember: "7",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotPatchDetails, err := organizeJSONPatchIntoFleetPatchDetails(tc.patch, tc.manifestObjMap)
			if err != nil {
				t.Fatalf("organizeJSONPatchIntoFleetPatchDetails() = %v, want no error", err)
			}

			if diff := cmp.Diff(gotPatchDetails, tc.wantPatchDetails); diff != "" {
				t.Errorf("patchDetails mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}
