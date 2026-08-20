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

package overrider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/test/utils/informer"
	"go.goms.io/fleet/test/utils/resource"
)

var (
	crpName = "my-test-crp"
	rpName  = "my-test-rp"
)

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add placement v1beta1 scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add cluster v1beta1 scheme: %v", err)
	}
	return scheme
}

func clusterResourceSnapshotForTest(resources ...placementv1beta1.ResourceContent) *placementv1beta1.ClusterResourceSnapshot {
	return &placementv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
			Labels: map[string]string{
				placementv1beta1.ResourceIndexLabel:     "0",
				placementv1beta1.PlacementTrackingLabel: crpName,
			},
			Annotations: map[string]string{
				placementv1beta1.ResourceGroupHashAnnotation:         "abc",
				placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
			},
		},
		Spec: placementv1beta1.ResourceSnapshotSpec{
			SelectedResources: resources,
		},
	}
}

func resourceEnvelopeContentForTest(t *testing.T, namespace, name string, data map[string]string) placementv1beta1.ResourceContent {
	t.Helper()
	return envelopeContentForTest(t, string(placementv1beta1.ResourceEnvelopeType), namespace, name, data)
}

func clusterResourceEnvelopeContentForTest(t *testing.T, name string, data map[string]string) placementv1beta1.ResourceContent {
	t.Helper()
	return envelopeContentForTest(t, string(placementv1beta1.ClusterResourceEnvelopeType), "", name, data)
}

func envelopeContentForTest(t *testing.T, kind, namespace, name string, data map[string]string) placementv1beta1.ResourceContent {
	t.Helper()
	rawData := make(map[string]json.RawMessage, len(data))
	for key, value := range data {
		rawData[key] = json.RawMessage(value)
	}
	metadata := map[string]string{"name": name}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	raw, err := json.Marshal(map[string]interface{}{
		"apiVersion": placementv1beta1.GroupVersion.String(),
		"kind":       kind,
		"metadata":   metadata,
		"data":       rawData,
	})
	if err != nil {
		t.Fatalf("json.Marshal(%s) = %v, want no error", kind, err)
	}
	return placementv1beta1.ResourceContent{RawExtension: runtime.RawExtension{Raw: raw}}
}

func deploymentRawForTest(namespace, name string) string {
	return fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"namespace":%q,"name":%q}}`, namespace, name)
}

func secretRawForTest(namespace, name string) string {
	return fmt.Sprintf(`{"apiVersion":"v1","kind":"Secret","metadata":{"namespace":%q,"name":%q}}`, namespace, name)
}

func clusterRoleRawForTest(name string) string {
	return fmt.Sprintf(`{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"name":%q}}`, name)
}

func latestCROSnapshotForTest(name string, selectors ...placementv1beta1.ResourceSelectorTerm) placementv1beta1.ClusterResourceOverrideSnapshot {
	return placementv1beta1.ClusterResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				placementv1beta1.IsLatestSnapshotLabel: "true",
			},
		},
		Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
			OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
				ClusterResourceSelectors: selectors,
			},
		},
	}
}

func latestROSnapshotForTest(namespace, name string, selectors ...placementv1beta1.ResourceSelector) placementv1beta1.ResourceOverrideSnapshot {
	return placementv1beta1.ResourceOverrideSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				placementv1beta1.IsLatestSnapshotLabel: "true",
			},
		},
		Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
			OverrideSpec: placementv1beta1.ResourceOverrideSpec{
				ResourceSelectors: selectors,
			},
		},
	}
}

func clusterResourceOverrideSnapshotPtrForTest(snapshot placementv1beta1.ClusterResourceOverrideSnapshot) *placementv1beta1.ClusterResourceOverrideSnapshot {
	return &snapshot
}

func resourceOverrideSnapshotPtrForTest(snapshot placementv1beta1.ResourceOverrideSnapshot) *placementv1beta1.ResourceOverrideSnapshot {
	return &snapshot
}

func TestFetchAllMatchingOverridesForResourceSnapshot(t *testing.T) {
	fakeInformer := informer.FakeManager{
		APIResources: map[schema.GroupVersionKind]bool{
			{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			}: true,
			{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}: true,
			{
				Group:   "",
				Version: "v1",
				Kind:    "Secret",
			}: true,
		},
		IsClusterScopedResource: false,
	}

	tests := []struct {
		name         string
		placementKey string
		master       placementv1beta1.ResourceSnapshotObj
		snapshots    []placementv1beta1.ResourceSnapshotObj
		croList      []placementv1beta1.ClusterResourceOverrideSnapshot
		roList       []placementv1beta1.ResourceOverrideSnapshot
		wantCRO      []*placementv1beta1.ClusterResourceOverrideSnapshot
		wantRO       []*placementv1beta1.ResourceOverrideSnapshot
		wantErr      error
	}{
		{
			name:         "single resource snapshot selecting empty resources",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			name:         "single resource snapshot with no matched overrides",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "test-cluster-role",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "Role",
									Name:    "test-role",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			name: "single resource snapshot with matched cro and ro",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
		},
		{
			name:         "single resource snapshot with matched stale cro and ro snapshot",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			name:         "multiple resource snapshots with matched cro and ro",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.NamespaceResourceContentForTest(t),
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			snapshots: []placementv1beta1.ResourceSnapshotObj{
				&placementv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 0),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel:     "0",
							placementv1beta1.PlacementTrackingLabel: crpName,
						},
						Annotations: map[string]string{
							placementv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: placementv1beta1.ResourceSnapshotSpec{
						SelectedResources: []placementv1beta1.ResourceContent{
							*resource.DeploymentResourceContentForTest(t),
						},
					},
				},
				&placementv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 1),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel:     "0",
							placementv1beta1.PlacementTrackingLabel: crpName,
						},
						Annotations: map[string]string{
							placementv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: placementv1beta1.ResourceSnapshotSpec{
						SelectedResources: []placementv1beta1.ResourceContent{
							*resource.ClusterRoleResourceContentForTest(t),
						},
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "not-exist",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "clusterrole-name",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "not-exist",
								},
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "clusterrole-name",
								},
							},
						},
					},
				},
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "not-exist",
								},
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
		},
		{
			name:         "multiple resource snapshots with matched cro and ro by specifying the placement name",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.NamespaceResourceContentForTest(t),
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			snapshots: []placementv1beta1.ResourceSnapshotObj{
				&placementv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 0),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel:     "0",
							placementv1beta1.PlacementTrackingLabel: crpName,
						},
						Annotations: map[string]string{
							placementv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: placementv1beta1.ResourceSnapshotSpec{
						SelectedResources: []placementv1beta1.ResourceContent{
							*resource.DeploymentResourceContentForTest(t),
						},
					},
				},
				&placementv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 1),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel:     "0",
							placementv1beta1.PlacementTrackingLabel: crpName,
						},
						Annotations: map[string]string{
							placementv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: placementv1beta1.ResourceSnapshotSpec{
						SelectedResources: []placementv1beta1.ResourceContent{
							*resource.ClusterRoleResourceContentForTest(t),
						},
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: crpName,
							},
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "not-exist",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "clusterrole-name",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "not-exist",
								},
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: crpName,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "clusterrole-name",
								},
							},
						},
					},
				},
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: crpName,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "not-exist",
								},
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
		},
		{
			// not supported in the first phase
			name:         "single resource snapshot with multiple matched cro and ro",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
									Name:    "svc-namespace",
								},
							},
						},
					},
				},
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
			},
		},
		{
			name:         "no matched cro and ro which are configured to other placement",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.NamespaceResourceContentForTest(t),
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			snapshots: []placementv1beta1.ResourceSnapshotObj{
				&placementv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 0),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel:     "0",
							placementv1beta1.PlacementTrackingLabel: crpName,
						},
						Annotations: map[string]string{
							placementv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: placementv1beta1.ResourceSnapshotSpec{
						SelectedResources: []placementv1beta1.ResourceContent{
							*resource.DeploymentResourceContentForTest(t),
						},
					},
				},
				&placementv1beta1.ClusterResourceSnapshot{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 1),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel:     "0",
							placementv1beta1.PlacementTrackingLabel: crpName,
						},
						Annotations: map[string]string{
							placementv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: placementv1beta1.ResourceSnapshotSpec{
						SelectedResources: []placementv1beta1.ResourceContent{
							*resource.ClusterRoleResourceContentForTest(t),
						},
					},
				},
			},
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "not-exist",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: "other-placement",
							},
							ClusterResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
								{
									Group:   "rbac.authorization.k8s.io",
									Version: "v1",
									Kind:    "ClusterRole",
									Name:    "clusterrole-name",
								},
							},
						},
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: "other-placement",
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "not-exist",
								},
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: "other-placement",
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			name:         "ro match should take placement scope into account",
			placementKey: crpName,
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.NamespaceResourceContentForTest(t),
						*resource.ServiceResourceContentForTest(t),
						*resource.DeploymentResourceContentForTest(t),
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					// No OverrideSpec.Placement.Scope specified, should match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: crpName,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					// OverrideSpec.Placement.Scope specified as Cluster, should match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  crpName,
								Scope: placementv1beta1.ClusterScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
				{
					// OverrideSpec.Placement.Scope specified as Namespaced, should NOT match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-3",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  crpName,
								Scope: placementv1beta1.NamespaceScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: crpName,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  crpName,
								Scope: placementv1beta1.ClusterScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
		},
		{
			name:         "ro match with resourceSnapshot",
			placementKey: "deployment-namespace/" + rpName,
			master: &placementv1beta1.ResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, rpName, 0),
					Namespace: "deployment-namespace",
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel:     "0",
						placementv1beta1.PlacementTrackingLabel: rpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: placementv1beta1.ResourceSnapshotSpec{
					SelectedResources: []placementv1beta1.ResourceContent{
						*resource.NamespaceResourceContentForTest(t),
						*resource.ServiceResourceContentForTest(t),
						*resource.DeploymentResourceContentForTest(t),
					},
				},
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				{
					// No OverrideSpec.Placement.Scope specified, should NOT match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name: rpName,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Service",
									Name:    "svc-name",
								},
							},
						},
					},
				},
				{
					// OverrideSpec.Placement.Scope specified as Cluster, should NOT match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  rpName,
								Scope: placementv1beta1.ClusterScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
				{
					// OverrideSpec.Placement.Scope specified as Namespaced, should match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-3",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  rpName,
								Scope: placementv1beta1.NamespaceScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
				{
					// OverrideSpec.Placement.Name does not exist, should NOT match.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-4",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  "does-not-exist",
								Scope: placementv1beta1.NamespaceScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-3",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Placement: &placementv1beta1.PlacementRef{
								Name:  rpName,
								Scope: placementv1beta1.NamespaceScoped,
							},
							ResourceSelectors: []placementv1beta1.ResourceSelector{
								{
									Group:   "apps",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "resource envelope inner deployment matches resource override",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
			})),
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				})),
			},
		},
		{
			name: "cluster resource envelope inner cluster role matches cluster resource override",
			master: clusterResourceSnapshotForTest(clusterResourceEnvelopeContentForTest(t, "env", map[string]string{
				"clusterrole.yaml": clusterRoleRawForTest("foo"),
			})),
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				latestCROSnapshotForTest("cro-clusterrole", placementv1beta1.ResourceSelectorTerm{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "foo",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				clusterResourceOverrideSnapshotPtrForTest(latestCROSnapshotForTest("cro-clusterrole", placementv1beta1.ResourceSelectorTerm{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "foo",
				})),
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			name: "explicit envelope wrapper selectors are not matched",
			master: clusterResourceSnapshotForTest(
				clusterResourceEnvelopeContentForTest(t, "env", map[string]string{
					"clusterrole.yaml": clusterRoleRawForTest("foo"),
				}),
				resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
					"deployment.yaml": deploymentRawForTest("ns", "my-app"),
				}),
			),
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				latestCROSnapshotForTest("cro-wrapper", placementv1beta1.ResourceSelectorTerm{
					Group:   placementv1beta1.GroupVersion.Group,
					Version: placementv1beta1.GroupVersion.Version,
					Kind:    string(placementv1beta1.ClusterResourceEnvelopeType),
					Name:    "env",
				}),
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-wrapper", placementv1beta1.ResourceSelector{
					Group:   placementv1beta1.GroupVersion.Group,
					Version: placementv1beta1.GroupVersion.Version,
					Kind:    string(placementv1beta1.ResourceEnvelopeType),
					Name:    "env",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			name: "resource envelope namespace matches namespace cluster resource override",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
			})),
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				latestCROSnapshotForTest("cro-namespace", placementv1beta1.ResourceSelectorTerm{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "ns",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				clusterResourceOverrideSnapshotPtrForTest(latestCROSnapshotForTest("cro-namespace", placementv1beta1.ResourceSelectorTerm{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "ns",
				})),
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			// Collecting override candidates is a best-effort selection concern and must never block the
			// rollout. An inner manifest that cannot be parsed is skipped, and candidates from the remaining valid
			// manifests in the same envelope are still collected.
			name: "resource envelope inner manifest that cannot be parsed is skipped and valid inner manifests still collected",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"bad.yaml":        `"not-an-object"`,
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
			})),
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				})),
			},
		},
		{
			// A cluster-scoped object wrapped in a resource envelope is invalid input, but candidate
			// collection must not hard-fail on it: the offending manifest is skipped while the valid inner
			// manifest in the same envelope is still collected. The authoritative validation lives in the
			// work generator.
			name: "cluster scoped object wrapped in resource envelope is skipped and valid inner manifest still collected",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"clusterrole.yaml": clusterRoleRawForTest("foo"),
				"deployment.yaml":  deploymentRawForTest("ns", "my-app"),
			})),
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				latestCROSnapshotForTest("cro-clusterrole", placementv1beta1.ResourceSelectorTerm{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "foo",
				}),
			},
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				})),
			},
		},
		{
			// A namespaced object wrapped in a cluster resource envelope is invalid input, but candidate
			// collection must not hard-fail on it: the offending manifest is skipped while the valid inner
			// manifest in the same envelope is still collected.
			name: "namespaced object wrapped in cluster resource envelope is skipped and valid inner manifest still collected",
			master: clusterResourceSnapshotForTest(clusterResourceEnvelopeContentForTest(t, "env", map[string]string{
				"deployment.yaml":  deploymentRawForTest("ns", "my-app"),
				"clusterrole.yaml": clusterRoleRawForTest("foo"),
			})),
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				latestCROSnapshotForTest("cro-clusterrole", placementv1beta1.ResourceSelectorTerm{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "foo",
				}),
				latestCROSnapshotForTest("cro-deployment", placementv1beta1.ResourceSelectorTerm{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				clusterResourceOverrideSnapshotPtrForTest(latestCROSnapshotForTest("cro-clusterrole", placementv1beta1.ResourceSelectorTerm{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "ClusterRole",
					Name:    "foo",
				})),
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{},
		},
		{
			// An inner manifest in a different namespace than the resource envelope is invalid input, but
			// candidate collection must not hard-fail on it: the offending manifest is skipped while the
			// valid inner manifest in the same envelope is still collected.
			name: "inner manifest in a different namespace than the resource envelope is skipped and valid inner manifest still collected",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"other.yaml":      deploymentRawForTest("other-ns", "other-app"),
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
			})),
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				}),
				latestROSnapshotForTest("other-ns", "ro-other", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "other-app",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				})),
			},
		},
		{
			name: "resource envelope with multiple inner resources only selects targeted override",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
				"secret.yaml":     secretRawForTest("ns", "secret-name"),
			})),
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				}),
				latestROSnapshotForTest("ns", "ro-service", placementv1beta1.ResourceSelector{
					Group:   "",
					Version: "v1",
					Kind:    "Service",
					Name:    "svc-name",
				}),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-deployment", placementv1beta1.ResourceSelector{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					Name:    "my-app",
				})),
			},
		},
		{
			name: "resource envelope does not duplicate snapshot when multiple selectors match",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
				"secret.yaml":     secretRawForTest("ns", "secret-name"),
			})),
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-multiple-selectors",
					placementv1beta1.ResourceSelector{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
						Name:    "my-app",
					},
					placementv1beta1.ResourceSelector{
						Group:   "",
						Version: "v1",
						Kind:    "Secret",
						Name:    "secret-name",
					},
				),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-multiple-selectors",
					placementv1beta1.ResourceSelector{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
						Name:    "my-app",
					},
					placementv1beta1.ResourceSelector{
						Group:   "",
						Version: "v1",
						Kind:    "Secret",
						Name:    "secret-name",
					},
				)),
			},
		},
		{
			// Only a later selector matches; the snapshot is still selected. This pins the partial-match
			// behaviour so the no-match warning stays scoped to snapshots where no selector matches at all.
			name: "resource override with multiple selectors where only a later selector matches is still selected",
			master: clusterResourceSnapshotForTest(resourceEnvelopeContentForTest(t, "ns", "env", map[string]string{
				"deployment.yaml": deploymentRawForTest("ns", "my-app"),
			})),
			roList: []placementv1beta1.ResourceOverrideSnapshot{
				latestROSnapshotForTest("ns", "ro-partial-match",
					placementv1beta1.ResourceSelector{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
						Name:    "does-not-exist",
					},
					placementv1beta1.ResourceSelector{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
						Name:    "my-app",
					},
				),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{
				resourceOverrideSnapshotPtrForTest(latestROSnapshotForTest("ns", "ro-partial-match",
					placementv1beta1.ResourceSelector{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
						Name:    "does-not-exist",
					},
					placementv1beta1.ResourceSelector{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
						Name:    "my-app",
					},
				)),
			},
		},
		{
			// Only a later selector matches; the snapshot is still selected. This pins the partial-match
			// behaviour so the no-match warning stays scoped to snapshots where no selector matches at all.
			name: "cluster resource override with multiple selectors where only a later selector matches is still selected",
			master: clusterResourceSnapshotForTest(clusterResourceEnvelopeContentForTest(t, "env", map[string]string{
				"clusterrole.yaml": clusterRoleRawForTest("foo"),
			})),
			croList: []placementv1beta1.ClusterResourceOverrideSnapshot{
				latestCROSnapshotForTest("cro-partial-match",
					placementv1beta1.ResourceSelectorTerm{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						Name:    "does-not-exist",
					},
					placementv1beta1.ResourceSelectorTerm{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						Name:    "foo",
					},
				),
			},
			wantCRO: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				clusterResourceOverrideSnapshotPtrForTest(latestCROSnapshotForTest("cro-partial-match",
					placementv1beta1.ResourceSelectorTerm{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						Name:    "does-not-exist",
					},
					placementv1beta1.ResourceSelectorTerm{
						Group:   "rbac.authorization.k8s.io",
						Version: "v1",
						Kind:    "ClusterRole",
						Name:    "foo",
					},
				)),
			},
			wantRO: []*placementv1beta1.ResourceOverrideSnapshot{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{tc.master}
			for i := range tc.snapshots {
				objects = append(objects, tc.snapshots[i])
			}
			for i := range tc.croList {
				objects = append(objects, &tc.croList[i])
			}
			for i := range tc.roList {
				objects = append(objects, &tc.roList[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			gotCRO, gotRO, err := FetchAllMatchingOverridesForResourceSnapshot(context.Background(), fakeClient, &fakeInformer, tc.placementKey, tc.master)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || (err != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("fetchAllMatchingOverridesForResourceSnapshot() got error %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmpopts.SortSlices(func(o1, o2 *placementv1beta1.ClusterResourceOverrideSnapshot) bool {
					return o1.Name < o2.Name
				}),
				cmpopts.SortSlices(func(o1, o2 *placementv1beta1.ResourceOverrideSnapshot) bool {
					if o1.Namespace == o2.Namespace {
						return o1.Name < o2.Name
					}
					return o1.Namespace < o2.Namespace
				}),
				cmpopts.EquateEmpty(),
			}
			if diff := cmp.Diff(tc.wantCRO, gotCRO, options...); diff != "" {
				t.Errorf("fetchAllMatchingOverridesForResourceSnapshot() returned clusterResourceOverrides mismatch (-want, +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantRO, gotRO, options...); diff != "" {
				t.Errorf("fetchAllMatchingOverridesForResourceSnapshot() returned resourceOverrides mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestPickFromResourceMatchedOverridesForTargetCluster(t *testing.T) {
	clusterName := "cluster-1"
	tests := []struct {
		name    string
		cluster *clusterv1beta1.MemberCluster
		croList []*placementv1beta1.ClusterResourceOverrideSnapshot
		roList  []*placementv1beta1.ResourceOverrideSnapshot
		wantCRO []string
		wantRO  []placementv1beta1.NamespacedName
		wantErr error
	}{
		{
			name: "empty overrides",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			},
			wantCRO: nil,
			wantRO:  nil,
		},
		{
			name: "non-latest override snapshots",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			},
			croList: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
			},
			roList: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
			},
			wantCRO: []string{"cro-1", "cro-2"},
			wantRO: []placementv1beta1.NamespacedName{
				{
					Namespace: "deployment-namespace",
					Name:      "ro-2",
				},
				{
					Namespace: "svc-namespace",
					Name:      "ro-1",
				},
			},
		},
		{
			name: "cluster not found",
			croList: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-not-exist",
				},
			},
			wantErr: controller.ErrExpectedBehavior,
		},
		{
			name: "matched overrides with empty cluster label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			},
			croList: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
			},
			roList: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										// empty cluster label selector selects all clusters
										ClusterSelector: &placementv1beta1.ClusterSelector{},
									},
								},
							},
						},
					},
				},
			},
			wantCRO: []string{"cro-1", "cro-2"},
			wantRO: []placementv1beta1.NamespacedName{
				{
					Namespace: "deployment-namespace",
					Name:      "ro-2",
				},
				{
					Namespace: "svc-namespace",
					Name:      "ro-1",
				},
			},
		},
		{
			name: "matched overrides with non-empty cluster label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			croList: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value2",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			roList: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "test",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "test",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key2": "value2",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantCRO: []string{"cro-1"},
			wantRO: []placementv1beta1.NamespacedName{
				{
					Namespace: "test",
					Name:      "ro-1",
				},
				{
					Namespace: "test",
					Name:      "ro-2",
				},
			},
		},
		{
			name: "no matched overrides with non-empty cluster label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			croList: []*placementv1beta1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ClusterResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key1": "value2",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			roList: []*placementv1beta1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "test",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1beta1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1beta1.ResourceOverrideSpec{
							Policy: &placementv1beta1.OverridePolicy{
								OverrideRules: []placementv1beta1.OverrideRule{
									{
										ClusterSelector: &placementv1beta1.ClusterSelector{
											ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
												{
													LabelSelector: &metav1.LabelSelector{
														MatchLabels: map[string]string{
															"key4": "value1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantCRO: []string{},
			wantRO:  []placementv1beta1.NamespacedName{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{tc.cluster}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			gotCRO, gotRO, err := PickFromResourceMatchedOverridesForTargetCluster(context.Background(), fakeClient, clusterName, tc.croList, tc.roList)
			if gotErr, wantErr := err != nil, tc.wantErr != nil; gotErr != wantErr || !errors.Is(err, tc.wantErr) {
				t.Fatalf("pickFromResourceMatchedOverridesForTargetCluster() got error %v, want error %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantCRO, gotCRO); diff != "" {
				t.Errorf("pickFromResourceMatchedOverridesForTargetCluster() returned clusterResourceOverrides mismatch (-want, +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantRO, gotRO); diff != "" {
				t.Errorf("pickFromResourceMatchedOverridesForTargetCluster() returned resourceOverrides mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestIsClusterMatched(t *testing.T) {
	tests := []struct {
		name    string
		cluster clusterv1beta1.MemberCluster
		rule    placementv1beta1.OverrideRule
		want    bool
	}{
		{
			name: "matched overrides with nil cluster selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1beta1.OverrideRule{}, // nil cluster selector selects no clusters
			want: false,
		},
		{
			name: "rule with empty cluster selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{}, // empty cluster label selects all clusters
			},
			want: true,
		},
		{
			name: "rule with empty cluster selector terms",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{}, // empty cluster label terms selects all clusters
				},
			},
			want: true,
		},
		{
			name: "rule with nil cluster label selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: nil,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "rule with empty cluster label selector",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{}, // empty label selector selects all clusters
						},
					},
				},
			},
			want: true,
		},
		{
			name: "matched overrides with non-empty cluster label",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "matched overrides with multiple cluster terms",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value2",
								},
							},
						},
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value1",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "no matched overrides with non-empty cluster label",
			cluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			rule: placementv1beta1.OverrideRule{
				ClusterSelector: &placementv1beta1.ClusterSelector{
					ClusterSelectorTerms: []placementv1beta1.ClusterSelectorTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"key1": "value2",
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsClusterMatched(&tc.cluster, tc.rule)
			if err != nil {
				t.Fatalf("IsClusterMatched() got error %v, want nil", err)
			}

			if got != tc.want {
				t.Errorf("IsClusterMatched() = %v, want %v", got, tc.want)
			}
		})
	}
}
