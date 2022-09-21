/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package clusterresourceplacement

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/pointer"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

func TestGenerateManifest(t *testing.T) {
	tests := map[string]struct {
		unstructuredObj  func() *unstructured.Unstructured
		expectedManifest func() *workv1alpha1.Manifest
		expectedError    error
	}{
		"should generate sanitized manifest for: k8s.io/apiextensions-apiserver/apis/apiextensions/v1/CustomResourceDefinition": {
			unstructuredObj: func() *unstructured.Unstructured {
				crd := v1.CustomResourceDefinition{
					TypeMeta: metav1.TypeMeta{
						Kind:       "crd-type-name",
						APIVersion: "crd-type-api/v1",
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
						// todo - ask ryan about: ZZZ_DeprecatedClusterName: utilrand.String(10),
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
				crd := v1.CustomResourceDefinition{
					TypeMeta: metav1.TypeMeta{
						Kind:       "crd-type-name",
						APIVersion: "crd-type-api/v1",
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
						// todo - ask Ryan on: ZZZ_DeprecatedClusterName: utilrand.String(10),
					},
				}

				mCRD, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&crd)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}
				unstructured.RemoveNestedField(mCRD, "metadata", "creationTimestamp")
				unstructured.RemoveNestedField(mCRD, "status")
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
		"should generate sanitized manifest for: k8s.io/api/core/v1/service": {
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
						Ports: []corev1.ServicePort{
							{
								Name:        "svc-port",
								Protocol:    corev1.ProtocolTCP,
								AppProtocol: pointer.String("svc.com/my-custom-protocol"),
								Port:        9001,
								NodePort:    int32(utilrand.Int()),
							},
						},
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
						Ports: []corev1.ServicePort{
							{
								Name:        "svc-port",
								Protocol:    corev1.ProtocolTCP,
								AppProtocol: pointer.String("svc.com/my-custom-protocol"),
								Port:        9001,
							},
						},
					},
				}

				mSvc, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&svc)
				if err != nil {
					t.Fatalf("ToUnstructured failed: %v", err)
				}
				unstructured.RemoveNestedField(mSvc, "metadata", "creationTimestamp")
				unstructured.RemoveNestedField(mSvc, "status")
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
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := generateManifest(tt.unstructuredObj())
			expected := tt.expectedManifest()

			if tt.expectedError != nil {
				assert.Containsf(t, err.Error(), tt.expectedError.Error(), "error not matching for Testcase %s", testName)
			} else {
				assert.Truef(t, err == nil, "err is not nil for Testcase %s", testName)
				assert.Equal(t, got, expected, "expected manifest did not match the expected")
			}
		})
	}
}
