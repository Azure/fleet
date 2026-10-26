/*
Copyright 2026 The KubeFleet Authors.

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

package placementresourcesnapshot

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

// TestSnapshotResource tests the snapshotResource function.
func TestSnapshotResource(t *testing.T) {
	const (
		namespaceName = "app"
		svcName       = "svc"
		jobName       = "job"
		controllerUID = "controller-uid-value"
	)

	svcIdentifier := placementv1alpha1.ObjectReference{
		Namespace:  namespaceName,
		Name:       svcName,
		APIGroup:   "",
		APIVersion: "v1",
		Kind:       "Service",
	}
	jobIdentifier := placementv1alpha1.ObjectReference{
		Namespace:  namespaceName,
		Name:       jobName,
		APIGroup:   "batch",
		APIVersion: "v1",
		Kind:       "Job",
	}

	toRawExtension := func(t *testing.T, obj map[string]any) runtime.RawExtension {
		t.Helper()
		raw, err := json.Marshal(obj)
		if err != nil {
			t.Fatalf("json.Marshal() = %v, want no error", err)
		}
		return runtime.RawExtension{Raw: raw}
	}

	// Compare manifests semantically rather than byte by byte for more readable diffs.
	rawExtensionTransformer := cmp.Transformer("RawExtensionToMap", func(r runtime.RawExtension) map[string]any {
		m := map[string]any{}
		if err := json.Unmarshal(r.Raw, &m); err != nil {
			return map[string]any{"unmarshalError": err.Error()}
		}
		return m
	})

	testCases := []struct {
		name         string
		resource     *unstructured.Unstructured
		wantManifest map[string]any
		wantID       placementv1alpha1.ObjectReference
	}{
		{
			name: "service with hub-allocated cluster IPs and node ports",
			resource: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata": map[string]any{
						"name":      svcName,
						"namespace": namespaceName,
					},
					"spec": map[string]any{
						"type":                "LoadBalancer",
						"clusterIP":           "10.0.0.1",
						"clusterIPs":          []any{"10.0.0.1"},
						"healthCheckNodePort": int64(30000),
						"ports": []any{
							map[string]any{
								"name":     "http",
								"port":     int64(80),
								"protocol": "TCP",
								"nodePort": int64(30080),
							},
							map[string]any{
								"name":     "https",
								"port":     int64(443),
								"protocol": "TCP",
								"nodePort": int64(30443),
							},
						},
						"selector": map[string]any{
							"app": "nginx",
						},
					},
					"status": map[string]any{
						"loadBalancer": map[string]any{},
					},
				},
			},
			wantManifest: map[string]any{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]any{
					"name":      svcName,
					"namespace": namespaceName,
				},
				"spec": map[string]any{
					"type": "LoadBalancer",
					"ports": []any{
						map[string]any{
							"name":     "http",
							"port":     int64(80),
							"protocol": "TCP",
						},
						map[string]any{
							"name":     "https",
							"port":     int64(443),
							"protocol": "TCP",
						},
					},
					"selector": map[string]any{
						"app": "nginx",
					},
				},
			},
			wantID: svcIdentifier,
		},
		{
			name: "headless service",
			resource: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata": map[string]any{
						"name":      svcName,
						"namespace": namespaceName,
					},
					"spec": map[string]any{
						"clusterIP":  "None",
						"clusterIPs": []any{"None"},
						"ports": []any{
							map[string]any{
								"port":     int64(80),
								"protocol": "TCP",
							},
						},
					},
				},
			},
			wantManifest: map[string]any{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]any{
					"name":      svcName,
					"namespace": namespaceName,
				},
				"spec": map[string]any{
					"clusterIP":  "None",
					"clusterIPs": []any{"None"},
					"ports": []any{
						map[string]any{
							"port":     int64(80),
							"protocol": "TCP",
						},
					},
				},
			},
			wantID: svcIdentifier,
		},
		{
			name: "job without manual selector",
			resource: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"metadata": map[string]any{
						"name":      jobName,
						"namespace": namespaceName,
					},
					"spec": map[string]any{
						"selector": map[string]any{
							"matchLabels": map[string]any{
								"controller-uid":                     controllerUID,
								"batch.kubernetes.io/controller-uid": controllerUID,
							},
						},
						"template": map[string]any{
							"metadata": map[string]any{
								"creationTimestamp": nil,
								"labels": map[string]any{
									"app":                                "job",
									"controller-uid":                     controllerUID,
									"batch.kubernetes.io/controller-uid": controllerUID,
								},
							},
							"spec": map[string]any{
								"restartPolicy": "Never",
							},
						},
					},
				},
			},
			wantManifest: map[string]any{
				"apiVersion": "batch/v1",
				"kind":       "Job",
				"metadata": map[string]any{
					"name":      jobName,
					"namespace": namespaceName,
				},
				"spec": map[string]any{
					"selector": map[string]any{
						"matchLabels": map[string]any{},
					},
					"template": map[string]any{
						"metadata": map[string]any{
							"labels": map[string]any{
								"app": "job",
							},
						},
						"spec": map[string]any{
							"restartPolicy": "Never",
						},
					},
				},
			},
			wantID: jobIdentifier,
		},
		{
			name: "job with manual selector",
			resource: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"metadata": map[string]any{
						"name":      jobName,
						"namespace": namespaceName,
					},
					"spec": map[string]any{
						"manualSelector": true,
						"selector": map[string]any{
							"matchLabels": map[string]any{
								"controller-uid": controllerUID,
							},
						},
						"template": map[string]any{
							"metadata": map[string]any{
								"labels": map[string]any{
									"controller-uid": controllerUID,
								},
							},
						},
					},
				},
			},
			wantManifest: map[string]any{
				"apiVersion": "batch/v1",
				"kind":       "Job",
				"metadata": map[string]any{
					"name":      jobName,
					"namespace": namespaceName,
				},
				"spec": map[string]any{
					"manualSelector": true,
					"selector": map[string]any{
						"matchLabels": map[string]any{
							"controller-uid": controllerUID,
						},
					},
					"template": map[string]any{
						"metadata": map[string]any{
							"labels": map[string]any{
								"controller-uid": controllerUID,
							},
						},
					},
				},
			},
			wantID: jobIdentifier,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.resource.DeepCopy()

			got, err := snapshotResource(tc.resource)
			if err != nil {
				t.Fatalf("snapshotResource() = %v, want no error", err)
			}

			want := placementv1alpha1.SnapshottedResource{
				Identifier: tc.wantID,
				Manifest:   toRawExtension(t, tc.wantManifest),
			}
			if diff := cmp.Diff(got, want, rawExtensionTransformer); diff != "" {
				t.Errorf("snapshotResource() mismatch (-got, +want):\n%s", diff)
			}

			// The input resource must not be modified.
			if diff := cmp.Diff(tc.resource, original); diff != "" {
				t.Errorf("snapshotResource() modified the input resource (-got, +want):\n%s", diff)
			}
		})
	}
}
