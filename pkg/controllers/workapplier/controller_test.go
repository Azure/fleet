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
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	testRunnerNameEnvVarName  = "KUBEFLEET_CI_TEST_RUNNER_NAME"
	runnerNameToSkipTestsInCI = "default"
)

const (
	workName = "work-1"

	deployName      = "deploy-1"
	jobName         = "job-1"
	configMapName   = "configmap-1"
	secretName      = "secret-1"
	nsName          = "ns-1"
	clusterRoleName = "clusterrole-1"
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

	job = &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "busybox",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "busybox",
							Command: []string{
								"sh",
								"-c",
								"sleep 60",
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
			Parallelism: ptr.To(int32(1)),
			Completions: ptr.To(int32(2)),
		},
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

	configMap = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      configMapName,
		},
		Data: map[string]string{
			dummyLabelKey: dummyLabelValue1,
		},
	}

	secret = &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      secretName,
		},
		Data: map[string][]byte{
			dummyLabelKey: []byte(dummyLabelValue1),
		},
	}

	clusterRole = &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	dummyOwnerRef = metav1.OwnerReference{
		APIVersion: "dummy.owner/v1",
		Kind:       "DummyOwner",
		Name:       "dummy-owner",
		UID:        "1234-5678-90",
	}
)

var (
	appliedWorkOwnerRef = &metav1.OwnerReference{
		APIVersion: "placement.kubernetes-fleet.io/v1beta1",
		Kind:       "AppliedWork",
		Name:       workName,
		UID:        "uid",
	}
)

var (
	ignoreFieldTypeMetaInNamespace       = cmpopts.IgnoreFields(corev1.Namespace{}, "TypeMeta")
	ignoreFieldObjectMetaresourceVersion = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")

	lessFuncAppliedResourceMeta = func(i, j fleetv1beta1.AppliedResourceMeta) bool {
		iStr := fmt.Sprintf("%s/%s/%s/%s/%s", i.Group, i.Version, i.Kind, i.Namespace, i.Name)
		jStr := fmt.Sprintf("%s/%s/%s/%s/%s", j.Group, j.Version, j.Kind, j.Namespace, j.Name)
		return iStr < jStr
	}
)

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

func TestMain(m *testing.M) {
	// Skip the tests if in the CI environment the tests are invoked with `go test` instead of the Ginkgo CLI.
	// This has no effect outside the CI environment.
	if v := os.Getenv(testRunnerNameEnvVarName); v == runnerNameToSkipTestsInCI {
		log.Println("Skipping the tests in CI as they are not run with the expected runner")
		os.Exit(0)
	}

	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement/v1beta1) to the runtime scheme: %v", err)
	}

	// Initialize the variables.
	initializeVariables()

	os.Exit(m.Run())
}

func fakeClientScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add placement v1beta1 scheme: %v", err)
	}
	return scheme
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

// TestEnsureAppliedWork tests the ensureAppliedWork method.
func TestEnsureAppliedWork(t *testing.T) {
	ctx := context.Background()

	fakeUID := types.UID("foo")
	testCases := []struct {
		name            string
		work            *fleetv1beta1.Work
		appliedWork     *fleetv1beta1.AppliedWork
		wantWork        *fleetv1beta1.Work
		wantAppliedWork *fleetv1beta1.AppliedWork
	}{
		{
			name: "with work cleanup finalizer present, but no corresponding AppliedWork exists",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
					Finalizers: []string{
						fleetv1beta1.WorkFinalizer,
					},
				},
			},
			wantWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
					Finalizers: []string{
						fleetv1beta1.WorkFinalizer,
					},
				},
			},
			wantAppliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
				Spec: fleetv1beta1.AppliedWorkSpec{
					WorkName:      workName,
					WorkNamespace: memberReservedNSName1,
				},
			},
		},
		{
			name: "with work cleanup finalizer present, and corresponding AppliedWork exists",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
					Finalizers: []string{
						fleetv1beta1.WorkFinalizer,
					},
				},
			},
			appliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
					// Add the UID field to track if the method returns the existing object.
					UID: fakeUID,
				},
				Spec: fleetv1beta1.AppliedWorkSpec{
					WorkName:      workName,
					WorkNamespace: memberReservedNSName1,
				},
			},
			wantWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
					Finalizers: []string{
						fleetv1beta1.WorkFinalizer,
					},
				},
			},
			wantAppliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
					UID:  fakeUID,
				},
				Spec: fleetv1beta1.AppliedWorkSpec{
					WorkName:      workName,
					WorkNamespace: memberReservedNSName1,
				},
			},
		},
		{
			name: "without work cleanup finalizer, but corresponding AppliedWork exists",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
				},
			},
			appliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
					// Add the UID field to track if the method returns the existing object.
					UID: fakeUID,
				},
				Spec: fleetv1beta1.AppliedWorkSpec{
					WorkName:      workName,
					WorkNamespace: memberReservedNSName1,
				},
			},
			wantWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
					Finalizers: []string{
						fleetv1beta1.WorkFinalizer,
					},
				},
			},
			wantAppliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
					UID:  fakeUID,
				},
				Spec: fleetv1beta1.AppliedWorkSpec{
					WorkName:      workName,
					WorkNamespace: memberReservedNSName1,
				},
			},
		},
		{
			name: "without work cleanup finalizer, and no corresponding AppliedWork exists",
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
				},
			},
			wantWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: memberReservedNSName1,
					Finalizers: []string{
						fleetv1beta1.WorkFinalizer,
					},
				},
			},
			wantAppliedWork: &fleetv1beta1.AppliedWork{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
				Spec: fleetv1beta1.AppliedWorkSpec{
					WorkName:      workName,
					WorkNamespace: memberReservedNSName1,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hubClientScheme := fakeClientScheme(t)
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(hubClientScheme).
				WithObjects(tc.work).
				Build()

			memberClientScheme := fakeClientScheme(t)
			fakeMemberClientBuilder := fake.NewClientBuilder().WithScheme(memberClientScheme)
			if tc.appliedWork != nil {
				fakeMemberClientBuilder = fakeMemberClientBuilder.WithObjects(tc.appliedWork)
			}
			fakeMemberClient := fakeMemberClientBuilder.Build()

			r := &Reconciler{
				hubClient:   fakeHubClient,
				spokeClient: fakeMemberClient,
			}

			gotAppliedWork, err := r.ensureAppliedWork(ctx, tc.work)
			if err != nil {
				t.Fatalf("ensureAppliedWork() = %v, want no error", err)
			}

			// Verify the Work object.
			gotWork := &fleetv1beta1.Work{}
			if err := fakeHubClient.Get(ctx, types.NamespacedName{Name: tc.work.Name, Namespace: tc.work.Namespace}, gotWork); err != nil {
				t.Fatalf("failed to get Work object from fake hub client: %v", err)
			}
			if diff := cmp.Diff(gotWork, tc.wantWork, ignoreFieldObjectMetaresourceVersion); diff != "" {
				t.Errorf("Work objects diff (-got +want):\n%s", diff)
			}

			// Verify the AppliedWork object.
			if diff := cmp.Diff(gotAppliedWork, tc.wantAppliedWork, ignoreFieldObjectMetaresourceVersion); diff != "" {
				t.Errorf("AppliedWork objects diff (-got +want):\n%s", diff)
			}
		})
	}
}
