/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/pointer"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

func TestGenerateManifest(t *testing.T) {
	tests := map[string]struct {
		unstructuredObj  interface{}
		expectedManifest interface{}
		expectedError    error
	}{
		"should generate sanitized manifest for Kind: CustomResourceDefinition": {
			unstructuredObj: func() *unstructured.Unstructured {
				crd := apiextensionsv1.CustomResourceDefinition{
					TypeMeta: metav1.TypeMeta{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:                       "object-name",
						GenerateName:               "object-generateName",
						Namespace:                  "object-namespace",
						SelfLink:                   "object-selflink",
						UID:                        types.UID(utilrand.String(10)),
						ResourceVersion:            utilrand.String(10),
						Generation:                 int64(utilrand.Int()),
						CreationTimestamp:          metav1.Time{Time: time.Date(utilrand.IntnRange(0, 999), time.January, 1, 1, 1, 1, 1, time.UTC)},
						DeletionTimestamp:          &metav1.Time{Time: time.Date(utilrand.IntnRange(1000, 1999), time.January, 1, 1, 1, 1, 1, time.UTC)},
						DeletionGracePeriodSeconds: pointer.Int64(9999),
						Labels: map[string]string{
							"label-key": "label-value",
						},
						Annotations: map[string]string{
							corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
							"svc-annotation-key":               "svc-object-annotation-key-value",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "svc-ownerRef-api/v1",
								Kind:       "svc-owner-kind",
								Name:       "svc-owner-name",
								UID:        "svc-owner-uid",
							},
						},
						Finalizers: []string{"object-finalizer"},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{
								Manager:    utilrand.String(10),
								Operation:  metav1.ManagedFieldsOperationApply,
								APIVersion: utilrand.String(10),
							},
						},
					},
				}

				mCrd, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&crd)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}

				return &unstructured.Unstructured{Object: mCrd}
			},
			expectedManifest: func() *workv1alpha1.Manifest {
				crd := apiextensionsv1.CustomResourceDefinition{
					TypeMeta: metav1.TypeMeta{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:                       "object-name",
						GenerateName:               "object-generateName",
						Namespace:                  "object-namespace",
						DeletionGracePeriodSeconds: pointer.Int64(9999),
						Labels: map[string]string{
							"label-key": "label-value",
						},
						Annotations: map[string]string{
							"svc-annotation-key": "svc-object-annotation-key-value",
						},
						Finalizers: []string{"object-finalizer"},
					},
				}

				mCRD, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&crd)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}
				delete(mCRD["metadata"].(map[string]interface{}), "creationTimestamp")
				delete(mCRD, "status")

				uCRD := unstructured.Unstructured{Object: mCRD}
				rawCRD, err := uCRD.MarshalJSON()
				if err != nil {
					t.Fatalf("MarshalJSON failed: %v", err)
				}

				return &workv1alpha1.Manifest{
					RawExtension: runtime.RawExtension{
						Raw: rawCRD,
					},
				}
			},
			expectedError: nil,
		},
		"should generate sanitized manifest for Kind: Service": {
			unstructuredObj: func() *unstructured.Unstructured {
				svc := corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "svc-name",
						Namespace:         "svc-namespace",
						SelfLink:          utilrand.String(10),
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{
								Manager:    "svc-manager",
								Operation:  metav1.ManagedFieldsOperationApply,
								APIVersion: "svc-manager-api/v1",
							},
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "svc-ownerRef-api/v1",
								Kind:       "svc-owner-kind",
								Name:       "svc-owner-name",
								UID:        "svc-owner-uid",
							},
						},
						Annotations: map[string]string{
							corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
							"svc-annotation-key":               "svc-object-annotation-key-value",
						},
						ResourceVersion:   "svc-object-resourceVersion",
						Generation:        int64(utilrand.Int()),
						CreationTimestamp: metav1.Time{Time: time.Date(00001, time.January, 1, 1, 1, 1, 1, time.UTC)},
						UID:               types.UID(utilrand.String(10)),
					},
					Spec: corev1.ServiceSpec{
						ClusterIP:           utilrand.String(10),
						ClusterIPs:          []string{},
						HealthCheckNodePort: int32(utilrand.Int()),
						Selector:            map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
						Ports: []corev1.ServicePort{
							{
								Name:        "svc-port",
								Protocol:    corev1.ProtocolTCP,
								AppProtocol: pointer.String("svc.com/my-custom-protocol"),
								Port:        9001,
								NodePort:    int32(utilrand.Int()),
							},
						},
						Type:                     corev1.ServiceType("svc-spec-type"),
						ExternalIPs:              []string{"svc-spec-externalIps-1"},
						SessionAffinity:          corev1.ServiceAffinity("svc-spec-sessionAffinity"),
						LoadBalancerIP:           "192.168.1.3",
						LoadBalancerSourceRanges: []string{"192.168.1.1"},
						ExternalName:             "svc-spec-externalName",
						ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyType("svc-spec-externalTrafficPolicy"),
						PublishNotReadyAddresses: false,
						SessionAffinityConfig:    &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: pointer.Int32(60)}},
						IPFamilies: []corev1.IPFamily{
							corev1.IPv4Protocol,
							corev1.IPv6Protocol,
						},
						IPFamilyPolicy:                makeIPFamilyPolicyTypePointer(corev1.IPFamilyPolicySingleStack),
						AllocateLoadBalancerNodePorts: pointer.Bool(false),
						LoadBalancerClass:             pointer.String("svc-spec-loadBalancerClass"),
						InternalTrafficPolicy:         makeServiceInternalTrafficPolicyPointer(corev1.ServiceInternalTrafficPolicyCluster),
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP:       "192.168.1.1",
									Hostname: "loadbalancer-ingress-hostname",
									Ports: []corev1.PortStatus{
										{
											Port:     9003,
											Protocol: corev1.ProtocolTCP,
										},
									},
								},
							},
						},
					},
				}

				mSvc, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&svc)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}

				return &unstructured.Unstructured{Object: mSvc}
			},
			expectedManifest: func() *workv1alpha1.Manifest {
				svc := corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-name",
						Namespace: "svc-namespace",
						Annotations: map[string]string{
							"svc-annotation-key": "svc-object-annotation-key-value",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
						Ports: []corev1.ServicePort{
							{
								Name:        "svc-port",
								Protocol:    corev1.ProtocolTCP,
								AppProtocol: pointer.String("svc.com/my-custom-protocol"),
								Port:        9001,
							},
						},
						Type:                     corev1.ServiceType("svc-spec-type"),
						ExternalIPs:              []string{"svc-spec-externalIps-1"},
						SessionAffinity:          corev1.ServiceAffinity("svc-spec-sessionAffinity"),
						LoadBalancerIP:           "192.168.1.3",
						LoadBalancerSourceRanges: []string{"192.168.1.1"},
						ExternalName:             "svc-spec-externalName",
						ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyType("svc-spec-externalTrafficPolicy"),
						PublishNotReadyAddresses: false,
						SessionAffinityConfig:    &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: pointer.Int32(60)}},
						IPFamilies: []corev1.IPFamily{
							corev1.IPv4Protocol,
							corev1.IPv6Protocol,
						},
						IPFamilyPolicy:                makeIPFamilyPolicyTypePointer(corev1.IPFamilyPolicySingleStack),
						AllocateLoadBalancerNodePorts: pointer.Bool(false),
						LoadBalancerClass:             pointer.String("svc-spec-loadBalancerClass"),
						InternalTrafficPolicy:         makeServiceInternalTrafficPolicyPointer(corev1.ServiceInternalTrafficPolicyCluster),
					},
				}

				mSvc, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&svc)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}
				delete(mSvc["metadata"].(map[string]interface{}), "creationTimestamp")
				delete(mSvc, "status")

				uSvc := unstructured.Unstructured{Object: mSvc}
				rawSvc, err := uSvc.MarshalJSON()
				if err != nil {
					t.Fatalf("MarshalJSON failed: %v", err)
				}

				return &workv1alpha1.Manifest{
					RawExtension: runtime.RawExtension{
						Raw: rawSvc,
					},
				}
			},
			expectedError: nil,
		},
		"should generate sanitized manifest for Kind: Job": {
			// Test that we remove the automatically generated select and labels
			unstructuredObj: func() *unstructured.Unstructured {
				indexedCompletion := batchv1.IndexedCompletion
				job := batchv1.Job{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "batch/v1",
						Kind:       "Job",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ryan-name",
						Namespace:         "ryan-namespace",
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{
								Manager:    "svc-manager",
								Operation:  metav1.ManagedFieldsOperationApply,
								APIVersion: "svc-manager-api/v1",
							},
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "svc-ownerRef-api/v1",
								Kind:       "svc-owner-kind",
								Name:       "svc-owner-name",
								UID:        "svc-owner-uid",
							},
						},
						Annotations: map[string]string{
							corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
							"svc-annotation-key":               "svc-object-annotation-key-value",
						},
						ResourceVersion:   "svc-object-resourceVersion",
						Generation:        int64(utilrand.Int()),
						CreationTimestamp: metav1.Time{Time: time.Date(00001, time.January, 1, 1, 1, 1, 1, time.UTC)},
						UID:               types.UID(utilrand.String(10)),
					},
					Spec: batchv1.JobSpec{
						BackoffLimit:   pointer.Int32(5),
						CompletionMode: &indexedCompletion,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo":                                "bar",
								"job-name":                           "ryan-name",
								"controller-uid":                     utilrand.String(10),
								"batch.kubernetes.io/controller-uid": utilrand.String(10),
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"foo":                                "bar",
									"controller-uid":                     utilrand.String(10),
									"batch.kubernetes.io/controller-uid": utilrand.String(10),
									"job-name":                           "ryan-name",
									"batch.kubernetes.io/job-name":       "ryan-name",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Image: "foo/bar"},
								},
							},
						},
					},
					Status: batchv1.JobStatus{
						Active:                  1,
						Failed:                  3,
						UncountedTerminatedPods: &batchv1.UncountedTerminatedPods{},
					},
				}
				mJob, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&job)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}

				return &unstructured.Unstructured{Object: mJob}
			},
			expectedManifest: func() *workv1alpha1.Manifest {
				indexedCompletion := batchv1.IndexedCompletion
				job := batchv1.Job{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "batch/v1",
						Kind:       "Job",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ryan-name",
						Namespace: "ryan-namespace",
						Annotations: map[string]string{
							"svc-annotation-key": "svc-object-annotation-key-value",
						},
					},
					Spec: batchv1.JobSpec{
						BackoffLimit:   pointer.Int32(5),
						CompletionMode: &indexedCompletion,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo":      "bar",
								"job-name": "ryan-name",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"foo":                          "bar",
									"job-name":                     "ryan-name",
									"batch.kubernetes.io/job-name": "ryan-name",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Image: "foo/bar"},
								},
							},
						},
					},
				}
				mJob, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&job)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}
				unstructured.RemoveNestedField(mJob, "status")
				unstructured.RemoveNestedField(mJob, "metadata", "creationTimestamp")
				unstructured.RemoveNestedField(mJob, "spec", "template", "metadata", "creationTimestamp")

				uJob := unstructured.Unstructured{Object: mJob}
				rawJob, err := uJob.MarshalJSON()
				if err != nil {
					t.Fatalf("MarshalJSON failed: %v", err)
				}

				return &workv1alpha1.Manifest{
					RawExtension: runtime.RawExtension{
						Raw: rawJob,
					},
				}
			},
			expectedError: nil,
		},
		"should not touch select for Kind: Job with manualSelector": {
			// Test that we remove the automatically generated select and labels
			unstructuredObj: func() *unstructured.Unstructured {
				indexedCompletion := batchv1.IndexedCompletion
				job := batchv1.Job{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "batch/v1",
						Kind:       "Job",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ryan-name",
						Namespace:         "ryan-namespace",
						DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
						ResourceVersion:   "svc-object-resourceVersion",
						Generation:        int64(utilrand.Int()),
						CreationTimestamp: metav1.Time{Time: time.Date(00001, time.January, 1, 1, 1, 1, 1, time.UTC)},
						UID:               types.UID(utilrand.String(10)),
					},
					Spec: batchv1.JobSpec{
						BackoffLimit:   pointer.Int32(5),
						CompletionMode: &indexedCompletion,
						ManualSelector: pointer.Bool(true),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo":            "bar",
								"controller-uid": "ghjdfhsakdfj7824",
								"job-name":       "ryan-name",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"foo":                          "bar",
									"controller-uid":               "ghjdfhsakdfj7824",
									"job-name":                     "ryan-name",
									"batch.kubernetes.io/job-name": "ryan-name",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Image: "foo/bar"},
								},
							},
						},
					},
					Status: batchv1.JobStatus{
						Active:                  1,
						Failed:                  3,
						UncountedTerminatedPods: &batchv1.UncountedTerminatedPods{},
					},
				}
				mJob, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&job)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}

				return &unstructured.Unstructured{Object: mJob}
			},
			expectedManifest: func() *workv1alpha1.Manifest {
				indexedCompletion := batchv1.IndexedCompletion
				job := batchv1.Job{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "batch/v1",
						Kind:       "Job",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ryan-name",
						Namespace: "ryan-namespace",
					},
					Spec: batchv1.JobSpec{
						BackoffLimit:   pointer.Int32(5),
						CompletionMode: &indexedCompletion,
						ManualSelector: pointer.Bool(true),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo":            "bar",
								"controller-uid": "ghjdfhsakdfj7824",
								"job-name":       "ryan-name",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"foo":                          "bar",
									"controller-uid":               "ghjdfhsakdfj7824",
									"job-name":                     "ryan-name",
									"batch.kubernetes.io/job-name": "ryan-name",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Image: "foo/bar"},
								},
							},
						},
					},
				}
				mJob, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&job)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}
				unstructured.RemoveNestedField(mJob, "status")
				unstructured.RemoveNestedField(mJob, "metadata", "creationTimestamp")

				uJob := unstructured.Unstructured{Object: mJob}
				rawJob, err := uJob.MarshalJSON()
				if err != nil {
					t.Fatalf("MarshalJSON failed: %v", err)
				}

				return &workv1alpha1.Manifest{
					RawExtension: runtime.RawExtension{
						Raw: rawJob,
					},
				}
			},
			expectedError: nil,
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := generateManifest(tt.unstructuredObj.(func() *unstructured.Unstructured)())
			expected := tt.expectedManifest.(func() *workv1alpha1.Manifest)()

			if tt.expectedError != nil {
				assert.Containsf(t, err.Error(), tt.expectedError.Error(), "error not matching for Testcase %s", testName)
			} else {
				assert.Truef(t, err == nil, "err is not nil for Testcase %s", testName)
				assert.Equalf(t, got, expected, "expected manifest did not match the generated manifest, got %+v, want %+v", got, expected)
			}
		})
	}
}

func makeIPFamilyPolicyTypePointer(policyType corev1.IPFamilyPolicyType) *corev1.IPFamilyPolicyType {
	return &policyType
}
func makeServiceInternalTrafficPolicyPointer(policyType corev1.ServiceInternalTrafficPolicyType) *corev1.ServiceInternalTrafficPolicyType {
	return &policyType
}

func TestGenerateResourceContent(t *testing.T) {
	tests := map[string]struct {
		resource     interface{}
		wantResource interface{}
	}{
		"should generate sanitized resource content for Kind: CustomResourceDefinition": {
			resource: apiextensionsv1.CustomResourceDefinition{
				TypeMeta: metav1.TypeMeta{
					Kind:       "CustomResourceDefinition",
					APIVersion: "apiextensions.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "object-name",
					GenerateName:               "object-generateName",
					Namespace:                  "object-namespace",
					SelfLink:                   "object-selflink",
					UID:                        types.UID(utilrand.String(10)),
					ResourceVersion:            utilrand.String(10),
					Generation:                 int64(utilrand.Int()),
					CreationTimestamp:          metav1.Time{Time: time.Date(utilrand.IntnRange(0, 999), time.January, 1, 1, 1, 1, 1, time.UTC)},
					DeletionTimestamp:          &metav1.Time{Time: time.Date(utilrand.IntnRange(1000, 1999), time.January, 1, 1, 1, 1, 1, time.UTC)},
					DeletionGracePeriodSeconds: pointer.Int64(9999),
					Labels: map[string]string{
						"label-key": "label-value",
					},
					Annotations: map[string]string{
						corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
						"svc-annotation-key":               "svc-object-annotation-key-value",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "svc-ownerRef-api/v1",
							Kind:       "svc-owner-kind",
							Name:       "svc-owner-name",
							UID:        "svc-owner-uid",
						},
					},
					Finalizers: []string{"object-finalizer"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager:    utilrand.String(10),
							Operation:  metav1.ManagedFieldsOperationApply,
							APIVersion: utilrand.String(10),
						},
					},
				},
			},
			wantResource: apiextensionsv1.CustomResourceDefinition{
				TypeMeta: metav1.TypeMeta{
					Kind:       "CustomResourceDefinition",
					APIVersion: "apiextensions.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "object-name",
					GenerateName:               "object-generateName",
					Namespace:                  "object-namespace",
					DeletionGracePeriodSeconds: pointer.Int64(9999),
					Labels: map[string]string{
						"label-key": "label-value",
					},
					Annotations: map[string]string{
						"svc-annotation-key": "svc-object-annotation-key-value",
					},
					Finalizers: []string{"object-finalizer"},
				},
			},
		},
		"should generate sanitized resource content for Kind: Service": {
			resource: corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "svc-name",
					Namespace:         "svc-namespace",
					SelfLink:          utilrand.String(10),
					DeletionTimestamp: &metav1.Time{Time: time.Date(00002, time.January, 1, 1, 1, 1, 1, time.UTC)},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager:    "svc-manager",
							Operation:  metav1.ManagedFieldsOperationApply,
							APIVersion: "svc-manager-api/v1",
						},
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "svc-ownerRef-api/v1",
							Kind:       "svc-owner-kind",
							Name:       "svc-owner-name",
							UID:        "svc-owner-uid",
						},
					},
					Annotations: map[string]string{
						corev1.LastAppliedConfigAnnotation: "svc-object-annotation-lac-value",
						"svc-annotation-key":               "svc-object-annotation-key-value",
					},
					ResourceVersion:   "svc-object-resourceVersion",
					Generation:        int64(utilrand.Int()),
					CreationTimestamp: metav1.Time{Time: time.Date(00001, time.January, 1, 1, 1, 1, 1, time.UTC)},
					UID:               types.UID(utilrand.String(10)),
				},
				Spec: corev1.ServiceSpec{
					ClusterIP:           utilrand.String(10),
					ClusterIPs:          []string{},
					HealthCheckNodePort: int32(utilrand.Int()),
					Selector:            map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
					Ports: []corev1.ServicePort{
						{
							Name:        "svc-port",
							Protocol:    corev1.ProtocolTCP,
							AppProtocol: pointer.String("svc.com/my-custom-protocol"),
							Port:        9001,
							NodePort:    int32(utilrand.Int()),
						},
					},
					Type:                     corev1.ServiceType("svc-spec-type"),
					ExternalIPs:              []string{"svc-spec-externalIps-1"},
					SessionAffinity:          corev1.ServiceAffinity("svc-spec-sessionAffinity"),
					LoadBalancerIP:           "192.168.1.3",
					LoadBalancerSourceRanges: []string{"192.168.1.1"},
					ExternalName:             "svc-spec-externalName",
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyType("svc-spec-externalTrafficPolicy"),
					PublishNotReadyAddresses: false,
					SessionAffinityConfig:    &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: pointer.Int32(60)}},
					IPFamilies: []corev1.IPFamily{
						corev1.IPv4Protocol,
						corev1.IPv6Protocol,
					},
					IPFamilyPolicy:                makeIPFamilyPolicyTypePointer(corev1.IPFamilyPolicySingleStack),
					AllocateLoadBalancerNodePorts: pointer.Bool(false),
					LoadBalancerClass:             pointer.String("svc-spec-loadBalancerClass"),
					InternalTrafficPolicy:         makeServiceInternalTrafficPolicyPointer(corev1.ServiceInternalTrafficPolicyCluster),
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP:       "192.168.1.1",
								Hostname: "loadbalancer-ingress-hostname",
								Ports: []corev1.PortStatus{
									{
										Port:     9003,
										Protocol: corev1.ProtocolTCP,
									},
								},
							},
						},
					},
				},
			},
			wantResource: corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-name",
					Namespace: "svc-namespace",
					Annotations: map[string]string{
						"svc-annotation-key": "svc-object-annotation-key-value",
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"svc-spec-selector-key": "svc-spec-selector-value"},
					Ports: []corev1.ServicePort{
						{
							Name:        "svc-port",
							Protocol:    corev1.ProtocolTCP,
							AppProtocol: pointer.String("svc.com/my-custom-protocol"),
							Port:        9001,
						},
					},
					Type:                     corev1.ServiceType("svc-spec-type"),
					ExternalIPs:              []string{"svc-spec-externalIps-1"},
					SessionAffinity:          corev1.ServiceAffinity("svc-spec-sessionAffinity"),
					LoadBalancerIP:           "192.168.1.3",
					LoadBalancerSourceRanges: []string{"192.168.1.1"},
					ExternalName:             "svc-spec-externalName",
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyType("svc-spec-externalTrafficPolicy"),
					PublishNotReadyAddresses: false,
					SessionAffinityConfig:    &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: pointer.Int32(60)}},
					IPFamilies: []corev1.IPFamily{
						corev1.IPv4Protocol,
						corev1.IPv6Protocol,
					},
					IPFamilyPolicy:                makeIPFamilyPolicyTypePointer(corev1.IPFamilyPolicySingleStack),
					AllocateLoadBalancerNodePorts: pointer.Bool(false),
					LoadBalancerClass:             pointer.String("svc-spec-loadBalancerClass"),
					InternalTrafficPolicy:         makeServiceInternalTrafficPolicyPointer(corev1.ServiceInternalTrafficPolicyCluster),
				},
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tt.resource)
			if err != nil {
				t.Fatalf("ToUnstructured failed: %v", err)
			}
			got, err := generateResourceContent(&unstructured.Unstructured{Object: object})
			if err != nil {
				t.Fatalf("failed to generateResourceContent(): %v", err)
			}
			wantResourceContent := createResourceContentForTest(t, &tt.wantResource)
			if diff := cmp.Diff(wantResourceContent, got); diff != "" {
				t.Errorf("generateResourceContent() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func createResourceContentForTest(t *testing.T, obj interface{}) *fleetv1beta1.ResourceContent {
	want, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	if err != nil {
		t.Fatalf("ToUnstructured failed: %v", err)
	}
	delete(want["metadata"].(map[string]interface{}), "creationTimestamp")
	delete(want, "status")

	uWant := unstructured.Unstructured{Object: want}
	rawWant, err := uWant.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	return &fleetv1beta1.ResourceContent{
		RawExtension: runtime.RawExtension{
			Raw: rawWant,
		},
	}
}

func TestSortResource(t *testing.T) {
	// Create the Namespace object
	namespace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}

	// Create the Deployment object
	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-nginx",
				"namespace": "test",
			},
		},
	}

	// Create the CustomResourceDefinition object
	crd := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": "test-crd",
			},
		},
	}

	// Create the ClusterRole object
	clusterRole := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "test-clusterrole",
			},
		},
	}

	tests := map[string]struct {
		resources []runtime.Object
		want      []runtime.Object
	}{
		"should gather selected resources with Namespace in front": {
			resources: []runtime.Object{deployment, namespace},
			want:      []runtime.Object{namespace, deployment},
		},
		"should gather selected resources with CRD in front": {
			resources: []runtime.Object{clusterRole, crd},
			want:      []runtime.Object{crd, clusterRole},
		},
		"should gather selected resources with CRD or Namespace in front": {
			resources: []runtime.Object{deployment, clusterRole, crd, namespace},
			want:      []runtime.Object{namespace, crd, clusterRole, deployment},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			sortResources(tt.resources)

			// Check that the returned resources match the expected resources
			diff := cmp.Diff(tt.want, tt.resources)
			if diff != "" {
				t.Errorf("sortResources() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
