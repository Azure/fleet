/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

var (
	nsGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}

	deploy = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: nsName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nginx",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	deployUnstructured *unstructured.Unstructured
	deployJSON         []byte

	ns = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	nsUnstructured *unstructured.Unstructured
	nsJSON         []byte
)

var (
	lessFuncAppliedResourceMeta = func(i, j fleetv1beta1.AppliedResourceMeta) bool {
		iStr := fmt.Sprintf("%s/%s/%s/%s/%s", i.Group, i.Version, i.Kind, i.Namespace, i.Name)
		jStr := fmt.Sprintf("%s/%s/%s/%s/%s", j.Group, j.Version, j.Kind, j.Namespace, j.Name)
		return iStr < jStr
	}

	ignoreFieldConditionLTTMsg = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
)

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement/v1beta1) to the runtime scheme: %v", err)
	}

	// Initialize the variables.
	initializeVariables()

	os.Exit(m.Run())
}

func initializeVariables() {
	var err error

	// Regular objects.
	// Deployment.
	deployGenericMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deploy)
	if err != nil {
		log.Fatalf("failed to convert deployment to unstructured: %v", err)
	}
	deployUnstructured = &unstructured.Unstructured{Object: deployGenericMap}

	deployJSON, err = deployUnstructured.MarshalJSON()
	if err != nil {
		log.Fatalf("failed to marshal deployment to JSON: %v", err)
	}

	// Namespace.
	nsGenericMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ns)
	if err != nil {
		log.Fatalf("failed to convert namespace to unstructured: %v", err)
	}
	nsUnstructured = &unstructured.Unstructured{Object: nsGenericMap}
	nsJSON, err = nsUnstructured.MarshalJSON()
	if err != nil {
		log.Fatalf("failed to marshal namespace to JSON: %v", err)
	}
}

func nsWRI(ordinal int, nsName string) *fleetv1beta1.WorkResourceIdentifier {
	return &fleetv1beta1.WorkResourceIdentifier{
		Ordinal:  ordinal,
		Group:    "",
		Version:  "v1",
		Kind:     "Namespace",
		Resource: "namespaces",
		Name:     nsName,
	}
}

func deployWRI(ordinal int, nsName, deployName string) *fleetv1beta1.WorkResourceIdentifier {
	return &fleetv1beta1.WorkResourceIdentifier{
		Ordinal:   ordinal,
		Group:     "apps",
		Version:   "v1",
		Kind:      "Deployment",
		Resource:  "deployments",
		Name:      deployName,
		Namespace: nsName,
	}
}

func manifestAppliedCond(workGeneration int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             status,
		ObservedGeneration: workGeneration,
		Reason:             reason,
		Message:            message,
	}
}

// TestPrepareManifestProcessingBundles tests the prepareManifestProcessingBundles function.
func TestPrepareManifestProcessingBundles(t *testing.T) {
	deployJSON := deployJSON
	nsJSON := nsJSON
	memberReservedNSName := "fleet-member-experimental"

	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberReservedNSName,
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: []fleetv1beta1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Raw: nsJSON,
						},
					},
					{
						RawExtension: runtime.RawExtension{
							Raw: deployJSON,
						},
					},
				},
			},
		},
	}

	bundles := prepareManifestProcessingBundles(work)
	wantBundles := []*manifestProcessingBundle{
		{
			manifest: &work.Spec.Workload.Manifests[0],
		},
		{
			manifest: &work.Spec.Workload.Manifests[1],
		},
	}
	if diff := cmp.Diff(bundles, wantBundles, cmp.AllowUnexported(manifestProcessingBundle{})); diff != "" {
		t.Errorf("prepareManifestProcessingBundles() mismatches (-got +want):\n%s", diff)
	}
}

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
			manifestCondForWA := prepareManifestCondForWA(tc.wriStr, tc.wri, tc.workGeneration, tc.existingManifestCondQIdx, tc.existingManifestConds)
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
