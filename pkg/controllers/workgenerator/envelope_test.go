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

package workgenerator

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

func TestExtractManifestsFromEnvelopeCR(t *testing.T) {
	tests := []struct {
		name           string
		envelopeReader fleetv1beta1.EnvelopeReader
		want           []fleetv1beta1.Manifest
		wantErr        bool
	}{
		{
			name: "valid ResourceEnvelope with one resource",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"resource1": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm","namespace":"default"},"data":{"key":"value"}}`),
					},
				},
			},
			want: []fleetv1beta1.Manifest{
				{
					RawExtension: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm","namespace":"default"},"data":{"key":"value"}}`),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "config map with valid and invalid entries should fail",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"valid": {
						Raw: []byte(`"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod", "namespace": "default"}}`),
					},
					"invalid": {
						Raw: []byte("{invalid-json}"),
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "valid ClusterResourceEnvelope with one resource",
			envelopeReader: &fleetv1beta1.ClusterResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-envelope",
				},
				Data: map[string]runtime.RawExtension{
					"clusterrole1": {
						Raw: []byte(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"test-role"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}`),
					},
				},
			},
			want: []fleetv1beta1.Manifest{
				{
					RawExtension: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"test-role"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}`),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "envelope with multiple resources should have the right order",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-resource-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"resource1": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm1","namespace":"default"},"data":{"key1":"value1"}}`),
					},
					"resource2": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm2","namespace":"default"},"data":{"key2":"value2"}}`),
					},
				},
			},
			want: []fleetv1beta1.Manifest{
				{
					RawExtension: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm2","namespace":"default"},"data":{"key2":"value2"}}`),
					},
				},
				{
					RawExtension: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm1","namespace":"default"},"data":{"key1":"value1"}}`),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "envelope with invalid resource JSON",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-resource-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"invalid": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{invalid_json}`),
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "empty envelope",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{},
			},
			want:    []fleetv1beta1.Manifest{},
			wantErr: false,
		},
		// New test cases for namespace mismatches
		{
			name: "ResourceEnvelope with manifest in a different namespace",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespace-mismatch-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"resource1": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm","namespace":"other-namespace"},"data":{"key":"value"}}`),
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "ResourceEnvelope containing a cluster-scoped resource",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-resource-in-resource-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"resource1": {
						Raw: []byte(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"test-role"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}`),
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "ClusterResourceEnvelope with namespaced resource",
			envelopeReader: &fleetv1beta1.ClusterResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name: "namespaced-in-cluster-envelope",
				},
				Data: map[string]runtime.RawExtension{
					"resource1": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm","namespace":"default"},"data":{"key":"value"}}`),
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "ResourceEnvelope with mixed namespaced resources",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mixed-namespace-resources",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"resource1": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm1","namespace":"default"},"data":{"key1":"value1"}}`),
					},
					"resource2": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm2","namespace":"other-namespace"},"data":{"key2":"value2"}}`),
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractManifestsFromEnvelopeCR(tt.envelopeReader)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractManifestsFromEnvelopeCR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Use cmp.Diff for comparison
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("extractManifestsFromEnvelopeCR() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateOrUpdateEnvelopeCRWorkObj(t *testing.T) {
	ignoreWorkMeta := cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Name", "OwnerReferences")
	scheme := serviceScheme(t)

	workNamePrefix := "test-work"

	resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-snapshot",
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: "test-crp",
			},
		},
		Spec: fleetv1beta1.ClusterResourceSnapshot{}.Spec,
	}
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-binding",
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: "test-crp",
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:        "test-cluster-1",
			ResourceSnapshotName: resourceSnapshot.Name,
		},
	}
	configMapData := []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm","namespace":"default"},"data":{"key":"value"}}`)
	resourceEnvelope := &fleetv1beta1.ResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-envelope",
			Namespace: "default",
		},
		Data: map[string]runtime.RawExtension{
			"configmap": {
				Raw: configMapData,
			},
		},
	}

	clusterroleData := []byte(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"test-role"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}`)
	clusterResourceEnvelope := &fleetv1beta1.ClusterResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-envelope",
		},
		Data: map[string]runtime.RawExtension{
			"clusterrole": {
				Raw: clusterroleData,
			},
		},
	}

	// Create an existing work for update test
	existingWork := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workNamePrefix,
			Namespace: "test-app",
			Labels: map[string]string{
				fleetv1beta1.ParentBindingLabel:     resourceBinding.Name,
				fleetv1beta1.CRPTrackingLabel:       resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
				fleetv1beta1.EnvelopeTypeLabel:      string(fleetv1beta1.ResourceEnvelopeType),
				fleetv1beta1.EnvelopeNameLabel:      resourceEnvelope.Name,
				fleetv1beta1.EnvelopeNamespaceLabel: resourceEnvelope.Namespace,
			},
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: []fleetv1beta1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"old-cm","namespace":"default"},"data":{"key":"old-value"}}`),
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name                                string
		envelopeReader                      fleetv1beta1.EnvelopeReader
		resourceOverrideSnapshotHash        string
		clusterResourceOverrideSnapshotHash string
		existingObjects                     []client.Object
		want                                *fleetv1beta1.Work
		wantErr                             bool
	}{
		{
			name:                                "create work for ResourceEnvelope",
			envelopeReader:                      resourceEnvelope,
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			existingObjects:                     []client.Object{},
			want: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster),
					Labels: map[string]string{
						fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
						fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
						fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
						fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ResourceEnvelopeType),
						fleetv1beta1.EnvelopeNameLabel:                resourceEnvelope.Name,
						fleetv1beta1.EnvelopeNamespaceLabel:           resourceEnvelope.Namespace,
					},
					Annotations: map[string]string{
						fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.Spec.ResourceSnapshotName,
						fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        "resource-hash",
						fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: "cluster-resource-hash",
					},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{
							{
								RawExtension: runtime.RawExtension{Raw: configMapData},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:                                "create work for ClusterResourceEnvelope",
			envelopeReader:                      clusterResourceEnvelope,
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			existingObjects:                     []client.Object{},
			want: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster),
					Labels: map[string]string{
						fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
						fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
						fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
						fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ClusterResourceEnvelopeType),
						fleetv1beta1.EnvelopeNameLabel:                clusterResourceEnvelope.Name,
						fleetv1beta1.EnvelopeNamespaceLabel:           "",
					},
					Annotations: map[string]string{
						fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.Spec.ResourceSnapshotName,
						fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        "resource-hash",
						fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: "cluster-resource-hash",
					},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{
							{
								RawExtension: runtime.RawExtension{Raw: clusterroleData},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:                                "update existing work for ResourceEnvelope",
			envelopeReader:                      resourceEnvelope,
			resourceOverrideSnapshotHash:        "new-resource-hash",
			clusterResourceOverrideSnapshotHash: "new-cluster-resource-hash",
			existingObjects:                     []client.Object{existingWork},
			want: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-app", //copy from the existing work
					Labels: map[string]string{
						fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
						fleetv1beta1.CRPTrackingLabel:                 resourceBinding.Labels[fleetv1beta1.CRPTrackingLabel],
						fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
						fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ResourceEnvelopeType),
						fleetv1beta1.EnvelopeNameLabel:                resourceEnvelope.Name,
						fleetv1beta1.EnvelopeNamespaceLabel:           resourceEnvelope.Namespace,
					},
					Annotations: map[string]string{
						fleetv1beta1.ParentResourceSnapshotNameAnnotation:                resourceBinding.Spec.ResourceSnapshotName,
						fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        "new-resource-hash",
						fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: "new-cluster-resource-hash",
					},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{
							{
								RawExtension: runtime.RawExtension{
									Raw: configMapData,
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "error with malformed data in ResourceEnvelope",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "malformed-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"malformed": {
						Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"bad-cm",invalid json}}`),
					},
				},
			},
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			existingObjects:                     []client.Object{},
			want:                                nil,
			wantErr:                             true,
		},
		{
			name: "error with ResourceEnvelope containing cluster-scoped object",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-scope-envelope",
					Namespace: "default",
				},
				Data: map[string]runtime.RawExtension{
					"clusterrole": {
						Raw: []byte(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"test-role"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}`),
					},
				},
			},
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			existingObjects:                     []client.Object{},
			want:                                nil,
			wantErr:                             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with scheme
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			// Create reconciler
			r := &Reconciler{
				Client:          fakeClient,
				recorder:        record.NewFakeRecorder(10),
				InformerManager: &informer.FakeManager{},
			}

			// Call the function under test
			got, err := r.createOrUpdateEnvelopeCRWorkObj(ctx, tt.envelopeReader, workNamePrefix,
				resourceBinding, resourceSnapshot, tt.resourceOverrideSnapshotHash, tt.clusterResourceOverrideSnapshotHash)

			if (err != nil) != tt.wantErr {
				t.Errorf("createOrUpdateEnvelopeCRWorkObj() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Use cmp.Diff for comparison
			if diff := cmp.Diff(got, tt.want, ignoreWorkOption, ignoreWorkMeta, ignoreTypeMeta); diff != "" {
				t.Errorf("createOrUpdateEnvelopeCRWorkObj() mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// Test processOneSelectedResource with both envelope types
func TestProcessOneSelectedResource(t *testing.T) {
	scheme := serviceScheme(t)

	workNamePrefix := "test-work"
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-binding",
			Labels: map[string]string{
				fleetv1beta1.CRPTrackingLabel: "test-crp",
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster: "test-cluster",
		},
	}
	snapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-snapshot",
		},
	}

	// Convert the envelope objects to ResourceContent
	resourceEnvelopeContent := createResourceContent(t, &fleetv1beta1.ResourceEnvelope{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fleetv1beta1.GroupVersion.String(),
			Kind:       fleetv1beta1.ResourceEnvelopeKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-resource-envelope",
			Namespace: "default",
		},
		Data: map[string]runtime.RawExtension{
			"configmap": {
				Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm","namespace":"default"},"data":{"key":"value"}}`),
			},
		},
	})

	clusterResourceEnvelopeContent := createResourceContent(t, &fleetv1beta1.ClusterResourceEnvelope{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fleetv1beta1.GroupVersion.String(),
			Kind:       "ClusterResourceEnvelope",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster-envelope",
		},
		Data: map[string]runtime.RawExtension{
			"clusterrole": {
				Raw: []byte(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"test-role"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get","list"]}]}`),
			},
		},
	})

	configMapEnvelopeContent := createResourceContent(t, &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config-map-envelope",
			Namespace: "default",
			Annotations: map[string]string{
				fleetv1beta1.EnvelopeConfigMapAnnotation: "true",
			},
		},
		Data: map[string]string{
			"resource1": `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm1","namespace":"default"},"data":{"key1":"value1"}}`,
		},
	})

	// Regular resource content that's not an envelope
	regularResourceContent := createResourceContent(t, &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regular-config-map",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "value",
		},
	})

	tests := []struct {
		name                                string
		selectedResource                    *fleetv1beta1.ResourceContent
		resourceOverrideSnapshotHash        string
		clusterResourceOverrideSnapshotHash string
		wantNewWorkLen                      int
		wantSimpleManifestsLen              int
		wantErr                             bool
	}{
		{
			name:                                "process ResourceEnvelope",
			selectedResource:                    resourceEnvelopeContent,
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			wantNewWorkLen:                      1, // Should create a new work
			wantSimpleManifestsLen:              0, // Should not add to simple manifests
			wantErr:                             false,
		},
		{
			name:                                "process ClusterResourceEnvelope",
			selectedResource:                    clusterResourceEnvelopeContent,
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			wantNewWorkLen:                      1, // Should create a new work
			wantSimpleManifestsLen:              0, // Should not add to simple manifests
			wantErr:                             false,
		},
		{
			name:                                "process ConfigMap envelope that we no longer support",
			selectedResource:                    configMapEnvelopeContent,
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			wantNewWorkLen:                      0, // Should create a new work
			wantSimpleManifestsLen:              1, // Should not add to simple manifests
			wantErr:                             false,
		},
		{
			name:                                "process regular resource",
			selectedResource:                    regularResourceContent,
			resourceOverrideSnapshotHash:        "resource-hash",
			clusterResourceOverrideSnapshotHash: "cluster-resource-hash",
			wantNewWorkLen:                      0, // Should NOT create a new work
			wantSimpleManifestsLen:              1, // Should add to simple manifests
			wantErr:                             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with scheme
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			// Create reconciler
			r := &Reconciler{
				Client:          fakeClient,
				recorder:        record.NewFakeRecorder(10),
				InformerManager: &informer.FakeManager{},
			}

			// Prepare input parameters
			activeWork := make(map[string]*fleetv1beta1.Work)
			newWork := make([]*fleetv1beta1.Work, 0)
			simpleManifests := make([]fleetv1beta1.Manifest, 0)

			gotNewWork, gotSimpleManifests, err := r.processOneSelectedResource(
				ctx,
				tt.selectedResource,
				resourceBinding,
				snapshot,
				workNamePrefix,
				tt.resourceOverrideSnapshotHash,
				tt.clusterResourceOverrideSnapshotHash,
				activeWork,
				newWork,
				simpleManifests,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("processOneSelectedResource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(gotNewWork) != tt.wantNewWorkLen {
				t.Errorf("processOneSelectedResource() returned %d new works, want %d", len(gotNewWork), tt.wantNewWorkLen)
			}

			if len(gotSimpleManifests) != tt.wantSimpleManifestsLen {
				t.Errorf("processOneSelectedResource() returned %d simple manifests, want %d", len(gotSimpleManifests), tt.wantSimpleManifestsLen)
			}

			// Check active work got populated
			if tt.wantNewWorkLen > 0 && len(activeWork) != tt.wantNewWorkLen {
				t.Errorf("processOneSelectedResource() populated %d active works, want %d", len(activeWork), tt.wantNewWorkLen)
			}
		})
	}
}

func createResourceContent(t *testing.T, obj runtime.Object) *fleetv1beta1.ResourceContent {
	jsonData, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("Failed to marshal object: %v", err)
	}
	return &fleetv1beta1.ResourceContent{
		RawExtension: runtime.RawExtension{
			Raw: jsonData,
		},
	}
}
