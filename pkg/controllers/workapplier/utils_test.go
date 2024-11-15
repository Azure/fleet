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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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
	/**
	deployGVR = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}*/
	jobGVR = schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
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
	deployWRI          = &fleetv1beta1.WorkResourceIdentifier{
		Ordinal:   0,
		Group:     "apps",
		Version:   "v1",
		Kind:      "Deployment",
		Resource:  "deployments",
		Name:      deployName,
		Namespace: nsName,
	}

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
	nsWRI          = &fleetv1beta1.WorkResourceIdentifier{
		Ordinal:  1,
		Group:    "",
		Version:  "v1",
		Kind:     "Namespace",
		Resource: "namespaces",
		Name:     nsName,
	}

	/**
	nsWithGenerateName = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: nsGenerateName,
		},
	}
	nsWithGenerateNameUnstructured *unstructured.Unstructured
	nsWithGenerateNameJSON         []byte
	*/
	nsWithGenerateNameWRI = &fleetv1beta1.WorkResourceIdentifier{
		Ordinal:      2,
		Group:        "",
		Version:      "v1",
		Kind:         "Namespace",
		Resource:     "namespaces",
		GenerateName: nsGenerateName,
	}

	jobWithGenerateName = &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobGenerateName,
			Namespace:    nsName,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "busybox",
						},
					},
				},
			},
		},
	}
	jobWithGenerateNameUnstructured *unstructured.Unstructured
	//jobWithGenerateNameJSON         []byte
	jobWithGenerateNameWRI = &fleetv1beta1.WorkResourceIdentifier{
		Ordinal:      3,
		Group:        "batch",
		Version:      "v1",
		Kind:         "Job",
		Resource:     "jobs",
		GenerateName: jobGenerateName,
		Namespace:    nsName,
	}
)

var (
	lessFuncAppliedResourceMeta = func(i, j fleetv1beta1.AppliedResourceMeta) bool {
		iStr := fmt.Sprintf("%s/%s/%s/%s/%s/%s", i.Group, i.Version, i.Kind, i.Namespace, i.Name, i.GenerateName)
		jStr := fmt.Sprintf("%s/%s/%s/%s/%s/%s", j.Group, j.Version, j.Kind, j.Namespace, j.Name, j.GenerateName)
		return iStr < jStr
	}
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

	// Objects with generate names.
	// Job.
	jobGenericMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(jobWithGenerateName)
	if err != nil {
		log.Fatalf("failed to convert job with GenerateName to unstructured: %v", err)
	}
	jobWithGenerateNameUnstructured = &unstructured.Unstructured{Object: jobGenericMap}

	/**
	jobWithGenerateNameJSON, err = jobWithGenerateNameUnstructured.MarshalJSON()
	if err != nil {
		log.Fatalf("failed to marshal job with GenerateName to JSON: %v", err)
	}
	*/
}

// getNSWithGenerateNameUnstructured returns a Namespace object with a GenerateName field in the form of a K8s unstructured object.
/**
func getNSWithGenerateNameUnstructured(t *testing.T) *unstructured.Unstructured {
	if nsWithGenerateNameUnstructured != nil {
		return nsWithGenerateNameUnstructured
	}

	nsGenericMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(nsWithGenerateName)
	if err != nil {
		t.Fatalf("failed to convert namespace with GenerateName to unstructured: %v", err)
	}
	nsWithGenerateNameUnstructured = &unstructured.Unstructured{Object: nsGenericMap}
	return nsWithGenerateNameUnstructured
}
*/

// getNSWithGenerateNameJSON returns the JSON representation of a Namespace object with a GenerateName field.
/**
func getNSWithGenerateNameJSON(t *testing.T) []byte {
	nsWithGenerateNameUnstructured := getNSWithGenerateNameUnstructured(t)
	var err error
	nsWithGenerateNameJSON, err = nsWithGenerateNameUnstructured.MarshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal namespace with GenerateName to JSON: %v", err)
	}
	return nsWithGenerateNameJSON
}
*/

// getPreparingToProcessAppliedCond returns a manifest condition, which signals that the
// Fleet is preparing to be process the manifest.
func getPreparingToProcessAppliedCond(workGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: workGeneration,
		Reason:             ManifestAppliedCondPreparingToProcessReason,
		Message:            ManifestAppliedCondPreparingToProcessMessage,
	}
}

// getAppliedCond returns a manifest condition, which signals that the manifest has been applied.
func getAppliedCond(workGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: workGeneration,
		Reason:             string(ManifestProcessingApplyResultTypeApplied),
		Message:            ManifestProcessingApplyResultTypeAppliedDescription,
	}
}

// getFailedToApplyCond returns a manifest condition, which signals that the manifest failed to be applied.
func getFailedToApplyCond(reason, message string, workGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               fleetv1beta1.WorkConditionTypeApplied,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: workGeneration,
		Reason:             reason,
		Message:            message,
	}
}

// TestPrepareManifestProcessingBundles tests the prepareManifestProcessingBundles function.
func TestPrepareManifestProcessingBundles(t *testing.T) {
	deployJSON := deployJSON
	nsJSON := nsJSON

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
			name:        "ordinal and manifest object (regular)",
			manifestIdx: 1,
			manifestObj: nsUnstructured,
			wantWRI: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal:      1,
				Group:        "",
				Version:      "v1",
				Kind:         "Namespace",
				Name:         nsName,
				Namespace:    "",
				GenerateName: "",
			},
		},
		{
			name:        "ordinal, manifest object (regular), and GVR",
			manifestIdx: 2,
			gvr: &schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
			manifestObj: deployUnstructured,
			wantWRI: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal:      2,
				Group:        "apps",
				Version:      "v1",
				Kind:         "Deployment",
				Name:         deployName,
				Namespace:    nsName,
				GenerateName: "",
				Resource:     "deployments",
			},
		},
		{
			name:        "ordinal, manifest object (w/ generate name), and GVR",
			manifestIdx: 3,
			gvr: &schema.GroupVersionResource{
				Group:    "batch",
				Version:  "v1",
				Resource: "jobs",
			},
			manifestObj: jobWithGenerateNameUnstructured,
			wantWRI: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal:      3,
				Group:        "batch",
				Version:      "v1",
				Kind:         "Job",
				Name:         "",
				Namespace:    nsName,
				GenerateName: jobGenerateName,
				Resource:     "jobs",
			},
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
			name: "object with generate name",
			wri: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal:      1,
				Group:        "",
				Version:      "v1",
				Kind:         "Namespace",
				GenerateName: nsGenerateName,
				Resource:     "namespaces",
			},
			wantWRIString: fmt.Sprintf("GV=/v1, Kind=Namespace, Namespace=, GenerateName=%s", nsGenerateName),
		},
		{
			name: "regular object",
			wri: &fleetv1beta1.WorkResourceIdentifier{
				Ordinal:   2,
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Name:      deployName,
				Namespace: nsName,
				Resource:  "deployments",
			},
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
