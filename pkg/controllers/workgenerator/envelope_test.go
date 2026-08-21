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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/test/utils/informer"
)

const testWorkNamePrefix = "test-work"

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

func TestApplyOverridesToEnvelopeManifests(t *testing.T) {
	cluster := &clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
	}
	fakeInformer := informer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			utils.ConfigMapGVK:  true,
			utils.DeploymentGVK: true,
		},
		IsClusterScopedResource: false,
	}

	deploymentRaw := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"app"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"nginx","env":[]}]}}}}`
	configMapRaw := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"app"},"data":{"key":"value"}}`
	clusterRoleRaw := `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":"reader"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get"]}]}`
	otherDeploymentRaw := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"app"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"api"}},"template":{"metadata":{"labels":{"app":"api"}},"spec":{"containers":[{"name":"api","image":"nginx"}]}}}}`

	tests := []struct {
		name                 string
		envelopeReader       fleetv1beta1.EnvelopeReader
		croMap               map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ClusterResourceOverrideSnapshot
		roMap                map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot
		wantByResourceKey    map[string]string
		wantRawByResourceKey map[string]string
		wantErrSubstrings    []string
	}{
		{
			name: "ResourceEnvelope applies ResourceOverride to inner Deployment replicas",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{Name: "app-envelope", Namespace: "app"},
				Data: map[string]runtime.RawExtension{
					"deployment": {Raw: []byte(deploymentRaw)},
				},
			},
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("replicas-ro", "app", placementPatchRule("/spec/replicas", []byte(`5`)))},
			},
			wantByResourceKey: map[string]string{
				"Deployment/app/web": `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"app"},"spec":{"replicas":5,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"nginx","env":[]}]}}}}`,
			},
		},
		{
			name: "ClusterResourceEnvelope applies ClusterResourceOverride to inner ClusterRole",
			envelopeReader: &fleetv1beta1.ClusterResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-envelope"},
				Data: map[string]runtime.RawExtension{
					"clusterrole": {Raw: []byte(clusterRoleRaw)},
				},
			},
			croMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ClusterResourceOverrideSnapshot{
				clusterRoleResourceIdentifier("reader"): {clusterResourceOverrideSnapshot("clusterrole-cro", addPatchRule("/metadata/labels", []byte(`{"patched":"true"}`)))},
			},
			wantByResourceKey: map[string]string{
				"ClusterRole//reader": `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"labels":{"patched":"true"},"name":"reader"},"rules":[{"apiGroups":[""],"resources":["pods"],"verbs":["get"]}]}`,
			},
		},
		{
			name: "override matching only one inner resource leaves siblings byte-identical",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{Name: "mixed-envelope", Namespace: "app"},
				Data: map[string]runtime.RawExtension{
					"configmap":  {Raw: []byte(configMapRaw)},
					"deployment": {Raw: []byte(deploymentRaw)},
				},
			},
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("label-ro", "app", addPatchRule("/metadata/labels", []byte(`{"patched":"true"}`)))},
			},
			wantByResourceKey: map[string]string{
				"ConfigMap/app/settings": configMapRaw,
				"Deployment/app/web":     `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"labels":{"patched":"true"},"name":"web","namespace":"app"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"nginx","env":[]}]}}}}`,
			},
			wantRawByResourceKey: map[string]string{
				"ConfigMap/app/settings": configMapRaw,
			},
		},
		{
			name: "Delete override omits only the matching inner manifest",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-one-envelope", Namespace: "app"},
				Data: map[string]runtime.RawExtension{
					"configmap":  {Raw: []byte(configMapRaw)},
					"deployment": {Raw: []byte(deploymentRaw)},
				},
			},
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("delete-ro", "app", deleteOverrideRule())},
			},
			wantByResourceKey: map[string]string{
				"ConfigMap/app/settings": configMapRaw,
			},
		},
		{
			name: "Delete overrides can produce an empty manifest list",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-all-envelope", Namespace: "app"},
				Data: map[string]runtime.RawExtension{
					"api": {Raw: []byte(otherDeploymentRaw)},
					"web": {Raw: []byte(deploymentRaw)},
				},
			},
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				deploymentResourceIdentifier("app", "api"): {resourceOverrideSnapshot("delete-api-ro", "app", deleteOverrideRule())},
				deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("delete-web-ro", "app", deleteOverrideRule())},
			},
			wantByResourceKey: map[string]string{},
		},
		{
			name: "JSONPatch error identifies inner target and containing envelope",
			envelopeReader: &fleetv1beta1.ResourceEnvelope{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-patch-envelope", Namespace: "app"},
				Data: map[string]runtime.RawExtension{
					"deployment": {Raw: []byte(deploymentRaw)},
				},
			},
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("bad-ro", "app", placementPatchRule("/spec/missing/value", []byte(`1`)))},
			},
			wantErrSubstrings: []string{
				`Deployment "web" in namespace "app"`,
				"bad-patch-envelope",
				`ResourceOverrideSnapshot "bad-ro"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{InformerManager: &fakeInformer}
			manifests, err := extractManifestsFromEnvelopeCR(tt.envelopeReader)
			if err != nil {
				t.Fatalf("extractManifestsFromEnvelopeCR() error = %v, want nil", err)
			}

			got, overrideFailed, err := r.applyOverridesToEnvelopeManifests(manifests, &overrideContext{cluster: cluster, croMap: tt.croMap, roMap: tt.roMap}, tt.envelopeReader)
			if len(tt.wantErrSubstrings) > 0 {
				if err == nil {
					t.Fatalf("applyOverridesToEnvelopeManifests() error = nil, want non-nil")
				}
				if !overrideFailed {
					t.Errorf("applyOverridesToEnvelopeManifests() overrideFailed = false, want true")
				}
				for _, want := range tt.wantErrSubstrings {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("applyOverridesToEnvelopeManifests() error = %q, want to contain %q", err.Error(), want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("applyOverridesToEnvelopeManifests() error = %v, want nil", err)
			}
			if overrideFailed {
				t.Errorf("applyOverridesToEnvelopeManifests() overrideFailed = true, want false")
			}

			gotByResourceKey := manifestObjectByResourceKey(t, got)
			wantByResourceKey := manifestObjectByResourceKeyFromRaw(t, tt.wantByResourceKey)
			if diff := cmp.Diff(wantByResourceKey, gotByResourceKey); diff != "" {
				t.Errorf("applyOverridesToEnvelopeManifests() mismatch (-want +got):\n%s", diff)
			}
			gotRawByResourceKey := manifestRawByResourceKey(t, got)
			for key, want := range tt.wantRawByResourceKey {
				if got := gotRawByResourceKey[key]; got != want {
					t.Errorf("applyOverridesToEnvelopeManifests() raw manifest %q = %s, want %s", key, got, want)
				}
			}
		})
	}
}

func TestCreateOrUpdateEnvelopeCRWorkObj_EmptyManifestListRetained(t *testing.T) {
	scheme := serviceScheme(t)
	ctx := context.Background()
	deploymentRaw := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"app"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"nginx"}]}}}}`
	resourceEnvelope := &fleetv1beta1.ResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-envelope", Namespace: "app"},
		Data: map[string]runtime.RawExtension{
			"deployment": {Raw: []byte(deploymentRaw)},
		},
	}
	resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snapshot",
			Labels: map[string]string{
				fleetv1beta1.ResourceIndexLabel:     "0",
				fleetv1beta1.PlacementTrackingLabel: "crp",
			},
		},
	}
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "binding",
			Labels: map[string]string{
				fleetv1beta1.PlacementTrackingLabel: "crp",
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:        "cluster-1",
			ResourceSnapshotName: "snapshot",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Reconciler{
		Client: fakeClient,
		InformerManager: &informer.FakeManager{
			APIResources:            map[schema.GroupVersionKind]bool{utils.DeploymentGVK: true},
			IsClusterScopedResource: false,
		},
		recorder: record.NewFakeRecorder(10),
	}
	roMap := map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
		deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("delete-ro", "app", deleteOverrideRule())},
	}

	got, overrideFailed, err := r.createOrUpdateEnvelopeCRWorkObj(ctx, resourceEnvelope, testWorkNamePrefix, resourceBinding, resourceSnapshot, &overrideContext{cluster: &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"}}, roMap: roMap}, "ro-hash", "cro-hash")
	if err != nil {
		t.Fatalf("createOrUpdateEnvelopeCRWorkObj() error = %v, want nil", err)
	}
	if overrideFailed {
		t.Errorf("createOrUpdateEnvelopeCRWorkObj() overrideFailed = true, want false")
	}
	if got == nil {
		t.Fatalf("createOrUpdateEnvelopeCRWorkObj() = nil, want Work")
	}
	if len(got.Spec.Workload.Manifests) != 0 {
		t.Errorf("createOrUpdateEnvelopeCRWorkObj() manifests len = %d, want 0", len(got.Spec.Workload.Manifests))
	}
	if got.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation] != "ro-hash" {
		t.Errorf("resource override hash annotation = %q, want %q", got.Annotations[fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation], "ro-hash")
	}
}

func manifestObjectByResourceKey(t *testing.T, manifests []fleetv1beta1.Manifest) map[string]map[string]interface{} {
	t.Helper()
	byKey := make(map[string]map[string]interface{}, len(manifests))
	for i := range manifests {
		key, obj := manifestKeyAndObject(t, manifests[i].Raw)
		byKey[key] = obj
	}
	return byKey
}

func manifestObjectByResourceKeyFromRaw(t *testing.T, manifests map[string]string) map[string]map[string]interface{} {
	t.Helper()
	byKey := make(map[string]map[string]interface{}, len(manifests))
	for key, raw := range manifests {
		gotKey, obj := manifestKeyAndObject(t, []byte(raw))
		if gotKey != key {
			t.Fatalf("manifest key from raw = %q, want %q", gotKey, key)
		}
		byKey[key] = obj
	}
	return byKey
}

func manifestRawByResourceKey(t *testing.T, manifests []fleetv1beta1.Manifest) map[string]string {
	t.Helper()
	byKey := make(map[string]string, len(manifests))
	for i := range manifests {
		key, _ := manifestKeyAndObject(t, manifests[i].Raw)
		byKey[key] = string(manifests[i].Raw)
	}
	return byKey
}

func manifestKeyAndObject(t *testing.T, raw []byte) (string, map[string]interface{}) {
	t.Helper()
	var u unstructured.Unstructured
	if err := u.UnmarshalJSON(raw); err != nil {
		t.Fatalf("UnmarshalJSON(%q) error = %v, want nil", string(raw), err)
	}
	key := fmt.Sprintf("%s/%s/%s", u.GetKind(), u.GetNamespace(), u.GetName())
	return key, u.Object
}

func deploymentResourceIdentifier(namespace, name string) fleetv1beta1.ResourceIdentifier {
	return fleetv1beta1.ResourceIdentifier{
		Group:     utils.DeploymentGVK.Group,
		Version:   utils.DeploymentGVK.Version,
		Kind:      utils.DeploymentGVK.Kind,
		Namespace: namespace,
		Name:      name,
	}
}

func clusterRoleResourceIdentifier(name string) fleetv1beta1.ResourceIdentifier {
	return fleetv1beta1.ResourceIdentifier{
		Group:   utils.ClusterRoleGVK.Group,
		Version: utils.ClusterRoleGVK.Version,
		Kind:    utils.ClusterRoleGVK.Kind,
		Name:    name,
	}
}

func resourceOverrideSnapshot(name, namespace string, rule fleetv1beta1.OverrideRule) *fleetv1beta1.ResourceOverrideSnapshot {
	return &fleetv1beta1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: fleetv1beta1.ResourceOverrideSnapshotSpec{
			OverrideSpec: fleetv1beta1.ResourceOverrideSpec{
				Policy: &fleetv1beta1.OverridePolicy{
					OverrideRules: []fleetv1beta1.OverrideRule{rule},
				},
			},
		},
	}
}

func clusterResourceOverrideSnapshot(name string, rule fleetv1beta1.OverrideRule) *fleetv1beta1.ClusterResourceOverrideSnapshot {
	return &fleetv1beta1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: fleetv1beta1.ClusterResourceOverrideSnapshotSpec{
			OverrideSpec: fleetv1beta1.ClusterResourceOverrideSpec{
				Policy: &fleetv1beta1.OverridePolicy{
					OverrideRules: []fleetv1beta1.OverrideRule{rule},
				},
			},
		},
	}
}

func placementPatchRule(path string, value []byte) fleetv1beta1.OverrideRule {
	return fleetv1beta1.OverrideRule{
		ClusterSelector: &fleetv1beta1.ClusterSelector{},
		JSONPatchOverrides: []fleetv1beta1.JSONPatchOverride{
			{
				Operator: fleetv1beta1.JSONPatchOverrideOpReplace,
				Path:     path,
				Value:    apiextensionsv1.JSON{Raw: value},
			},
		},
	}
}

func addPatchRule(path string, value []byte) fleetv1beta1.OverrideRule {
	return fleetv1beta1.OverrideRule{
		ClusterSelector: &fleetv1beta1.ClusterSelector{},
		JSONPatchOverrides: []fleetv1beta1.JSONPatchOverride{
			{
				Operator: fleetv1beta1.JSONPatchOverrideOpAdd,
				Path:     path,
				Value:    apiextensionsv1.JSON{Raw: value},
			},
		},
	}
}

func deleteOverrideRule() fleetv1beta1.OverrideRule {
	return fleetv1beta1.OverrideRule{
		ClusterSelector: &fleetv1beta1.ClusterSelector{},
		OverrideType:    fleetv1beta1.DeleteOverrideType,
	}
}

func TestCreateOrUpdateEnvelopeCRWorkObj(t *testing.T) {
	ignoreWorkMeta := cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Name", "OwnerReferences")
	scheme := serviceScheme(t)

	resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-snapshot",
			Labels: map[string]string{
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
			},
		},
		Spec: fleetv1beta1.ClusterResourceSnapshot{}.Spec,
	}
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-binding",
			Labels: map[string]string{
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
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
			Name:      testWorkNamePrefix,
			Namespace: "fleet-member-test-cluster-1",
			Labels: map[string]string{
				fleetv1beta1.ParentBindingLabel:     resourceBinding.Name,
				fleetv1beta1.PlacementTrackingLabel: resourceBinding.Labels[fleetv1beta1.PlacementTrackingLabel],
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
						fleetv1beta1.PlacementTrackingLabel:           resourceBinding.Labels[fleetv1beta1.PlacementTrackingLabel],
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
						fleetv1beta1.PlacementTrackingLabel:           resourceBinding.Labels[fleetv1beta1.PlacementTrackingLabel],
						fleetv1beta1.ParentResourceSnapshotIndexLabel: resourceSnapshot.Labels[fleetv1beta1.ResourceIndexLabel],
						fleetv1beta1.EnvelopeTypeLabel:                string(fleetv1beta1.ClusterResourceEnvelopeType),
						fleetv1beta1.EnvelopeNameLabel:                clusterResourceEnvelope.Name,
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
					Namespace: "fleet-member-test-cluster-1", //copy from the existing work
					Labels: map[string]string{
						fleetv1beta1.ParentBindingLabel:               resourceBinding.Name,
						fleetv1beta1.PlacementTrackingLabel:           resourceBinding.Labels[fleetv1beta1.PlacementTrackingLabel],
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
		{
			// Duplicate envelope Works indicate an environment that was stuck before
			// deterministic naming was introduced. The controller surfaces the state
			// (error + Event) but does NOT auto-delete, because deleting a Work the
			// member agent has already applied would trigger resource fluctuation on
			// the member side. Full post-conditions are verified in
			// TestCreateOrUpdateEnvelopeCRWorkObj_DuplicateWorksSurfaceWithoutMutation.
			name:                                "two existing works surface an UnexpectedBehaviorError without mutation",
			envelopeReader:                      resourceEnvelope,
			resourceOverrideSnapshotHash:        "new-resource-hash",
			clusterResourceOverrideSnapshotHash: "new-cluster-resource-hash",
			existingObjects: func() []client.Object {
				existingWork1 := existingWork.DeepCopy()
				existingWork1.Name = "test-work-1"
				return []client.Object{existingWork, existingWork1}
			}(),
			wantErr: true,
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
			got, overrideFailed, err := r.createOrUpdateEnvelopeCRWorkObj(ctx, tt.envelopeReader, testWorkNamePrefix,
				resourceBinding, resourceSnapshot, &overrideContext{cluster: &clusterv1beta1.MemberCluster{}}, tt.resourceOverrideSnapshotHash, tt.clusterResourceOverrideSnapshotHash)

			if (err != nil) != tt.wantErr {
				t.Errorf("createOrUpdateEnvelopeCRWorkObj() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Use cmp.Diff for comparison
			if diff := cmp.Diff(got, tt.want, ignoreWorkOption, ignoreWorkMeta, ignoreTypeMeta); diff != "" {
				t.Errorf("createOrUpdateEnvelopeCRWorkObj() mismatch (-got +want):\n%s", diff)
			}
			if overrideFailed {
				t.Errorf("createOrUpdateEnvelopeCRWorkObj() overrideFailed = true, want false")
			}
		})
	}
}

// Test processOneSelectedResource with both envelope types
func TestProcessOneSelectedResource(t *testing.T) {
	scheme := serviceScheme(t)

	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-binding",
			Labels: map[string]string{
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
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

			gotNewWork, gotSimpleManifests, overrideFailed, err := r.processOneSelectedResource(
				ctx,
				tt.selectedResource,
				&overrideContext{cluster: &clusterv1beta1.MemberCluster{}},
				resourceBinding,
				snapshot,
				testWorkNamePrefix,
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
			if overrideFailed {
				t.Errorf("processOneSelectedResource() overrideFailed = true, want false")
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

func TestProcessOneSelectedResource_OverrideBehavior(t *testing.T) {
	scheme := serviceScheme(t)
	ctx := context.Background()
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-binding",
			Labels: map[string]string{
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:        "test-cluster",
			ResourceSnapshotName: "test-snapshot",
		},
	}
	snapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-snapshot",
			Labels: map[string]string{
				fleetv1beta1.ResourceIndexLabel:     "0",
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
			},
		},
	}
	cluster := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}
	deploymentRaw := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"app"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"nginx","env":[]}]}}}}`

	resourceEnvelope := &fleetv1beta1.ResourceEnvelope{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fleetv1beta1.GroupVersion.String(),
			Kind:       fleetv1beta1.ResourceEnvelopeKind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "wrapper", Namespace: "app"},
		Data: map[string]runtime.RawExtension{
			"deployment": {Raw: []byte(deploymentRaw)},
		},
	}
	resourceEnvelopeContent := createResourceContent(t, resourceEnvelope)
	originalEnvelopeRaw := append([]byte(nil), resourceEnvelopeContent.Raw...)

	regularDeployment := &unstructured.Unstructured{}
	if err := regularDeployment.UnmarshalJSON([]byte(deploymentRaw)); err != nil {
		t.Fatalf("UnmarshalJSON(%q) error = %v, want nil", deploymentRaw, err)
	}
	regularDeploymentContent := createResourceContent(t, regularDeployment)

	tests := []struct {
		name             string
		selectedResource *fleetv1beta1.ResourceContent
		roMap            map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot
		validate         func(t *testing.T, gotNewWork []*fleetv1beta1.Work, gotSimpleManifests []fleetv1beta1.Manifest)
	}{
		{
			name:             "wrapper-targeting override is ignored for ResourceEnvelope",
			selectedResource: resourceEnvelopeContent,
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				{
					Group:     fleetv1beta1.GroupVersion.Group,
					Version:   fleetv1beta1.GroupVersion.Version,
					Kind:      fleetv1beta1.ResourceEnvelopeKind,
					Namespace: "app",
					Name:      "wrapper",
				}: {resourceOverrideSnapshot("wrapper-ro", "app", addPatchRule("/data/injected", []byte(`{"raw":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"bad","namespace":"app"}}}`)))},
			},
			validate: func(t *testing.T, gotNewWork []*fleetv1beta1.Work, gotSimpleManifests []fleetv1beta1.Manifest) {
				t.Helper()
				if len(gotSimpleManifests) != 0 {
					t.Fatalf("processOneSelectedResource() simple manifests len = %d, want 0", len(gotSimpleManifests))
				}
				if len(gotNewWork) != 1 {
					t.Fatalf("processOneSelectedResource() new works len = %d, want 1", len(gotNewWork))
				}
				gotByResourceKey := manifestObjectByResourceKey(t, gotNewWork[0].Spec.Workload.Manifests)
				wantByResourceKey := map[string]string{"Deployment/app/web": deploymentRaw}
				wantObjectByResourceKey := manifestObjectByResourceKeyFromRaw(t, wantByResourceKey)
				if diff := cmp.Diff(wantObjectByResourceKey, gotByResourceKey); diff != "" {
					t.Errorf("processOneSelectedResource() envelope manifests mismatch (-want +got):\n%s", diff)
				}
				if string(resourceEnvelopeContent.Raw) != string(originalEnvelopeRaw) {
					t.Errorf("processOneSelectedResource() mutated envelope wrapper raw = %s, want %s", string(resourceEnvelopeContent.Raw), string(originalEnvelopeRaw))
				}
			},
		},
		{
			name:             "non-envelope override is applied exactly once",
			selectedResource: regularDeploymentContent,
			roMap: map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
				deploymentResourceIdentifier("app", "web"): {resourceOverrideSnapshot("env-ro", "app", addPatchRule("/spec/template/spec/containers/0/env/-", []byte(`{"name":"ADDED","value":"true"}`)))},
			},
			validate: func(t *testing.T, gotNewWork []*fleetv1beta1.Work, gotSimpleManifests []fleetv1beta1.Manifest) {
				t.Helper()
				if len(gotNewWork) != 0 {
					t.Fatalf("processOneSelectedResource() new works len = %d, want 0", len(gotNewWork))
				}
				if len(gotSimpleManifests) != 1 {
					t.Fatalf("processOneSelectedResource() simple manifests len = %d, want 1", len(gotSimpleManifests))
				}
				var u unstructured.Unstructured
				if err := u.UnmarshalJSON(gotSimpleManifests[0].Raw); err != nil {
					t.Fatalf("UnmarshalJSON() error = %v, want nil", err)
				}
				containers, found, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
				if err != nil || !found || len(containers) != 1 {
					t.Fatalf("containers lookup error = %v, found = %v, len = %d, want one container", err, found, len(containers))
				}
				container, ok := containers[0].(map[string]interface{})
				if !ok {
					t.Fatalf("container type = %T, want map[string]interface{}", containers[0])
				}
				env, ok := container["env"].([]interface{})
				if !ok {
					t.Fatalf("env type = %T, want []interface{}", container["env"])
				}
				if len(env) != 1 {
					t.Fatalf("env entries len = %d, want 1", len(env))
				}
				gotEnv, ok := env[0].(map[string]interface{})
				if !ok {
					t.Fatalf("env entry type = %T, want map[string]interface{}", env[0])
				}
				wantEnv := map[string]interface{}{"name": "ADDED", "value": "true"}
				if diff := cmp.Diff(wantEnv, gotEnv); diff != "" {
					t.Errorf("env entry mismatch (-want +got):\n%s", diff)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &Reconciler{
				Client: fakeClient,
				InformerManager: &informer.FakeManager{
					APIResources: map[schema.GroupVersionKind]bool{
						utils.DeploymentGVK: true,
						fleetv1beta1.GroupVersion.WithKind(fleetv1beta1.ResourceEnvelopeKind): true,
					},
					IsClusterScopedResource: false,
				},
				recorder: record.NewFakeRecorder(10),
			}
			activeWork := make(map[string]*fleetv1beta1.Work)
			gotNewWork, gotSimpleManifests, overrideFailed, err := r.processOneSelectedResource(
				ctx,
				tt.selectedResource,
				&overrideContext{cluster: cluster, roMap: tt.roMap},
				resourceBinding,
				snapshot,
				testWorkNamePrefix,
				"ro-hash",
				"cro-hash",
				activeWork,
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("processOneSelectedResource() error = %v, want nil", err)
			}
			if overrideFailed {
				t.Errorf("processOneSelectedResource() overrideFailed = true, want false")
			}
			tt.validate(t, gotNewWork, gotSimpleManifests)
		})
	}
}

func TestProcessOneSelectedResource_EnvelopeInnerOverrideFailureClassifiedAsOverrideFailure(t *testing.T) {
	scheme := serviceScheme(t)
	ctx := context.Background()
	deploymentRaw := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"app"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"web","image":"nginx"}]}}}}`
	resourceEnvelopeContent := createResourceContent(t, &fleetv1beta1.ResourceEnvelope{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fleetv1beta1.GroupVersion.String(),
			Kind:       fleetv1beta1.ResourceEnvelopeKind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "bad-inner-override", Namespace: "app"},
		Data: map[string]runtime.RawExtension{
			"deployment": {Raw: []byte(deploymentRaw)},
		},
	})
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-binding",
			Labels: map[string]string{
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
			},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:        "test-cluster",
			ResourceSnapshotName: "test-snapshot",
		},
	}
	snapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-snapshot",
			Labels: map[string]string{
				fleetv1beta1.ResourceIndexLabel:     "0",
				fleetv1beta1.PlacementTrackingLabel: "test-crp",
			},
		},
	}
	r := &Reconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		InformerManager: &informer.FakeManager{
			APIResources: map[schema.GroupVersionKind]bool{
				utils.DeploymentGVK: true,
				fleetv1beta1.GroupVersion.WithKind(fleetv1beta1.ResourceEnvelopeKind): true,
			},
			IsClusterScopedResource: false,
		},
		recorder: record.NewFakeRecorder(10),
	}
	roMap := map[fleetv1beta1.ResourceIdentifier][]*fleetv1beta1.ResourceOverrideSnapshot{
		deploymentResourceIdentifier("app", "web"): {
			resourceOverrideSnapshot("bad-ro", "app", placementPatchRule("/spec/missing/value", []byte(`1`))),
		},
	}

	_, _, overrideFailed, err := r.processOneSelectedResource(
		ctx,
		resourceEnvelopeContent,
		&overrideContext{cluster: &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}, roMap: roMap},
		resourceBinding,
		snapshot,
		testWorkNamePrefix,
		"ro-hash",
		"cro-hash",
		make(map[string]*fleetv1beta1.Work),
		nil,
		nil,
	)

	if err == nil {
		t.Fatalf("processOneSelectedResource() error = nil, want non-nil")
	}
	if !overrideFailed {
		t.Errorf("processOneSelectedResource() overrideFailed = false, want true")
	}
	if !errors.Is(err, controller.ErrUserError) {
		t.Errorf("processOneSelectedResource() error = %v, want wrapping controller.ErrUserError", err)
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

// TestEnvelopeWorkNameSuffix verifies that the suffix is a stable, length-bounded function
// of the envelope's identity. Determinism is what lets the API server enforce the
// "one Work per (binding, envelope)" invariant via name uniqueness.
func TestEnvelopeWorkNameSuffix(t *testing.T) {
	base := &fleetv1beta1.ResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{Name: "env-a", Namespace: "ns-1"},
	}

	tests := []struct {
		name      string
		other     fleetv1beta1.EnvelopeReader
		wantEqual bool // true → suffix must match base; false → suffix must differ
	}{
		{
			name:      "same identity",
			other:     &fleetv1beta1.ResourceEnvelope{ObjectMeta: metav1.ObjectMeta{Name: "env-a", Namespace: "ns-1"}},
			wantEqual: true,
		},
		{
			name:      "different name",
			other:     &fleetv1beta1.ResourceEnvelope{ObjectMeta: metav1.ObjectMeta{Name: "env-b", Namespace: "ns-1"}},
			wantEqual: false,
		},
		{
			name:      "different namespace",
			other:     &fleetv1beta1.ResourceEnvelope{ObjectMeta: metav1.ObjectMeta{Name: "env-a", Namespace: "ns-2"}},
			wantEqual: false,
		},
		{
			// Same name as base, but cluster-scoped — the type field in the hash input disambiguates.
			name:      "different type, same name",
			other:     &fleetv1beta1.ClusterResourceEnvelope{ObjectMeta: metav1.ObjectMeta{Name: "env-a"}},
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase := envelopeWorkNameSuffix(base)
			gotOther := envelopeWorkNameSuffix(tt.other)
			if tt.wantEqual && gotBase != gotOther {
				t.Errorf("envelopeWorkNameSuffix(%v) = %q, envelopeWorkNameSuffix(%v) = %q, want equal", base, gotBase, tt.other, gotOther)
			}
			if !tt.wantEqual && gotBase == gotOther {
				t.Errorf("envelopeWorkNameSuffix(%v) = envelopeWorkNameSuffix(%v) = %q, want different", base, tt.other, gotBase)
			}
		})
	}

	// Suffix is bounded and DNS-1123-safe (lowercase hex).
	suffix := envelopeWorkNameSuffix(base)
	if len(suffix) != 16 {
		t.Errorf("envelopeWorkNameSuffix length = %d, want 16", len(suffix))
	}
	if strings.ToLower(suffix) != suffix {
		t.Errorf("envelopeWorkNameSuffix(%v) = %q, want lowercase", base, suffix)
	}
}

// TestCreateOrUpdateEnvelopeCRWorkObj_DuplicateWorksSurfaceWithoutMutation verifies that
// when the label query returns multiple Works for the same (binding, envelope), the
// controller surfaces the condition (UnexpectedBehaviorError + Event) but does NOT
// mutate cluster state. Auto-deleting duplicates is unsafe because the member agent
// may have applied them; deleting one triggers resource cleanup that conflicts with
// the survivor. Deterministic naming (see buildNewWorkForEnvelopeCR) prevents new
// duplicates; pre-existing ones require operator cleanup.
func TestCreateOrUpdateEnvelopeCRWorkObj_DuplicateWorksSurfaceWithoutMutation(t *testing.T) {
	scheme := serviceScheme(t)
	ctx := context.Background()

	resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-snapshot",
			Labels: map[string]string{fleetv1beta1.PlacementTrackingLabel: "test-crp"},
		},
	}
	resourceBinding := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-binding",
			Labels: map[string]string{fleetv1beta1.PlacementTrackingLabel: "test-crp"},
		},
		Spec: fleetv1beta1.ResourceBindingSpec{
			TargetCluster:        "test-cluster-1",
			ResourceSnapshotName: resourceSnapshot.Name,
		},
	}
	resourceEnvelope := &fleetv1beta1.ResourceEnvelope{
		ObjectMeta: metav1.ObjectMeta{Name: "dup-envelope", Namespace: "default"},
		Data: map[string]runtime.RawExtension{
			"configmap": {Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm","namespace":"default"}}`)},
		},
	}

	workNamespace := fmt.Sprintf(utils.NamespaceNameFormat, resourceBinding.Spec.TargetCluster)
	labelsForWork := map[string]string{
		fleetv1beta1.ParentBindingLabel:     resourceBinding.Name,
		fleetv1beta1.PlacementTrackingLabel: resourceBinding.Labels[fleetv1beta1.PlacementTrackingLabel],
		fleetv1beta1.EnvelopeTypeLabel:      string(fleetv1beta1.ResourceEnvelopeType),
		fleetv1beta1.EnvelopeNameLabel:      resourceEnvelope.Name,
		fleetv1beta1.EnvelopeNamespaceLabel: resourceEnvelope.Namespace,
	}
	dupNames := []string{"dup-envelope-a", "dup-envelope-b", "dup-envelope-c"}
	mkWork := func(name string) *fleetv1beta1.Work {
		return &fleetv1beta1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: workNamespace,
				Labels:    labelsForWork,
			},
		}
	}
	objs := make([]client.Object, 0, len(dupNames))
	for _, n := range dupNames {
		objs = append(objs, mkWork(n))
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()

	recorder := record.NewFakeRecorder(10)
	r := &Reconciler{
		Client:          fakeClient,
		recorder:        recorder,
		InformerManager: &informer.FakeManager{},
	}

	got, overrideFailed, err := r.createOrUpdateEnvelopeCRWorkObj(ctx, resourceEnvelope, testWorkNamePrefix,
		resourceBinding, resourceSnapshot, &overrideContext{cluster: &clusterv1beta1.MemberCluster{}}, "", "")

	if got != nil {
		t.Errorf("createOrUpdateEnvelopeCRWorkObj() = %v, want nil on duplicate-detected path", got)
	}
	if err == nil {
		t.Fatalf("createOrUpdateEnvelopeCRWorkObj() error = nil, want UnexpectedBehaviorError")
	}
	if !errors.Is(err, controller.ErrUnexpectedBehavior) {
		t.Errorf("createOrUpdateEnvelopeCRWorkObj() error = %v, want wrapping controller.ErrUnexpectedBehavior", err)
	}
	if overrideFailed {
		t.Errorf("createOrUpdateEnvelopeCRWorkObj() overrideFailed = true, want false")
	}

	// Post-condition: all duplicates remain untouched — no auto-deletion.
	remaining := &fleetv1beta1.WorkList{}
	if err := fakeClient.List(ctx, remaining, client.InNamespace(workNamespace)); err != nil {
		t.Fatalf("List Works in %q: %v", workNamespace, err)
	}
	sortStrings := cmpopts.SortSlices(func(a, b string) bool { return a < b })
	if diff := cmp.Diff(dupNames, workNames(remaining.Items), sortStrings); diff != "" {
		t.Errorf("Works after call mismatch (-want +got):\n%s", diff)
	}

	// Post-condition: a Warning Event was emitted so operators can see the stuck state.
	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, "DuplicateEnvelopeWorks") {
			t.Errorf("event = %q, want reason DuplicateEnvelopeWorks", ev)
		}
	default:
		t.Error("no Event emitted for duplicate envelope Works; operators would have no surface signal")
	}
}

func workNames(items []fleetv1beta1.Work) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].Name
	}
	return out
}
