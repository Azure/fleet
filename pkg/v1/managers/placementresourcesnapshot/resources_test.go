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
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

// TestRetrieveResourceByName tests the retrieveResourceByName method, in particular how it falls back to reading
// from the API server directly when the selected resource is not found in the cache.
func TestRetrieveResourceByName(t *testing.T) {
	const (
		namespaceName         = "app"
		configMapName         = "app-config"
		selectedNamespaceName = "selected-ns"

		sourceCache     = "cache"
		sourceAPIServer = "api-server"
	)
	configMapGVR := corev1.SchemeGroupVersion.WithResource("configmaps")
	namespaceGVR := corev1.SchemeGroupVersion.WithResource("namespaces")

	// The objects carry their source in their data (or labels), so that the tests can tell where they come from.
	newConfigMap := func(source string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]interface{}{"name": configMapName, "namespace": namespaceName},
			"data":       map[string]interface{}{"source": source},
		}}
	}
	newNamespace := func(source string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name":   selectedNamespaceName,
				"labels": map[string]interface{}{"source": source},
			},
		}}
	}

	placementPolicy := &placementv1alpha1.PlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespaceName, Name: "policy"},
	}
	clusterPlacementPolicy := &placementv1alpha1.ClusterPlacementPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
	}
	configMapSelector := placementv1alpha1.ResourceSelector{APIVersion: "v1", Kind: "ConfigMap", Name: configMapName}
	namespaceSelector := placementv1alpha1.ResourceSelector{APIVersion: "v1", Kind: "Namespace", Name: selectedNamespaceName}

	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), meta.RESTScopeNamespace)
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("Namespace"), meta.RESTScopeRoot)

	testCases := []struct {
		name            string
		placementPolicy placementv1alpha1.PlacementPolicyAccessor
		selector        placementv1alpha1.ResourceSelector
		gvr             schema.GroupVersionResource
		informerSynced  bool
		cachedObjs      []runtime.Object
		cacheErr        error
		apiServerObjs   []runtime.Object
		apiServerErr    error
		// The object that the snapshotted resource should derive from; nil if an error is expected.
		wantResource      *unstructured.Unstructured
		wantErrCategory   kferrors.ErrCategory
		wantNotFound      bool
		wantAPIServerGets int
	}{
		{
			name:              "found in the cache",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    true,
			cachedObjs:        []runtime.Object{newConfigMap(sourceCache)},
			apiServerObjs:     []runtime.Object{newConfigMap(sourceAPIServer)},
			wantResource:      newConfigMap(sourceCache),
			wantAPIServerGets: 0,
		},
		{
			name:              "not found in the (stale) cache, found in the API server",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    true,
			apiServerObjs:     []runtime.Object{newConfigMap(sourceAPIServer)},
			wantResource:      newConfigMap(sourceAPIServer),
			wantAPIServerGets: 1,
		},
		{
			name:              "cluster-scoped, not found in the (stale) cache, found in the API server",
			placementPolicy:   clusterPlacementPolicy,
			selector:          namespaceSelector,
			gvr:               namespaceGVR,
			informerSynced:    true,
			apiServerObjs:     []runtime.Object{newNamespace(sourceAPIServer)},
			wantResource:      newNamespace(sourceAPIServer),
			wantAPIServerGets: 1,
		},
		{
			name:              "found in neither the cache nor the API server",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    true,
			wantErrCategory:   kferrors.ErrCategoryUser,
			wantNotFound:      true,
			wantAPIServerGets: 1,
		},
		{
			name:              "cluster-scoped, found in neither the cache nor the API server",
			placementPolicy:   clusterPlacementPolicy,
			selector:          namespaceSelector,
			gvr:               namespaceGVR,
			informerSynced:    true,
			wantErrCategory:   kferrors.ErrCategoryUser,
			wantNotFound:      true,
			wantAPIServerGets: 1,
		},
		{
			name:              "cache read fails with an error other than not found",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    true,
			cacheErr:          errors.New("the cache is unavailable"),
			apiServerObjs:     []runtime.Object{newConfigMap(sourceAPIServer)},
			wantErrCategory:   kferrors.ErrCategoryAPIServer,
			wantAPIServerGets: 0,
		},
		{
			name:              "not found in the cache, API server read fails with an error other than not found",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    true,
			apiServerErr:      apierrors.NewServiceUnavailable("the API server is unavailable"),
			wantErrCategory:   kferrors.ErrCategoryAPIServer,
			wantAPIServerGets: 1,
		},
		{
			name:              "informer not synced, found in the API server",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    false,
			cachedObjs:        []runtime.Object{newConfigMap(sourceCache)},
			apiServerObjs:     []runtime.Object{newConfigMap(sourceAPIServer)},
			wantResource:      newConfigMap(sourceAPIServer),
			wantAPIServerGets: 1,
		},
		{
			name:              "informer not synced, not found in the API server",
			placementPolicy:   placementPolicy,
			selector:          configMapSelector,
			gvr:               configMapGVR,
			informerSynced:    false,
			wantErrCategory:   kferrors.ErrCategoryUser,
			wantNotFound:      true,
			wantAPIServerGets: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, tc.apiServerObjs...)
			if tc.apiServerErr != nil {
				dynamicClient.PrependReactor("get", tc.gvr.Resource, func(_ clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, tc.apiServerErr
				})
			}
			m := &Manager{
				hubDynamicClient: dynamicClient,
				hubDynamicInformerManager: &testinformer.FakeManager{
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						tc.gvr: {Objects: tc.cachedObjs, Err: tc.cacheErr},
					},
					InformerSynced: ptr.To(tc.informerSynced),
				},
				restMapper: restMapper,
			}

			got, err := m.retrieveResourceByName(context.Background(), tc.placementPolicy, tc.selector)

			apiServerGets := 0
			for _, action := range dynamicClient.Actions() {
				if action.GetVerb() == "get" {
					apiServerGets++
				}
			}
			if apiServerGets != tc.wantAPIServerGets {
				t.Errorf("retrieveResourceByName() made %d reads from the API server, want %d", apiServerGets, tc.wantAPIServerGets)
			}

			if tc.wantResource == nil {
				if err == nil {
					t.Fatalf("retrieveResourceByName() = %v, want an error", got)
				}
				if gotCategory := kferrors.Category(err); gotCategory != tc.wantErrCategory {
					t.Errorf("retrieveResourceByName() error category = %q, want %q (err: %v)", gotCategory, tc.wantErrCategory, err)
				}
				if gotNotFound := apierrors.IsNotFound(err); gotNotFound != tc.wantNotFound {
					t.Errorf("retrieveResourceByName() apierrors.IsNotFound(err) = %t, want %t (err: %v)", gotNotFound, tc.wantNotFound, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("retrieveResourceByName() = %v, want no error", err)
			}
			want, err := snapshotResource(tc.wantResource)
			if err != nil {
				t.Fatalf("snapshotResource() = %v, want no error", err)
			}
			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("retrieveResourceByName() mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

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
