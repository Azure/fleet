/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package controller

import (
	"context"
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
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/utils/informer"
	"go.goms.io/fleet/test/utils/resource"
)

var (
	crpName = "my-test-crp"
)

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add placement v1beta1 scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add cluster v1beta1 scheme: %v", err)
	}
	if err := placementv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add v1alpha1 scheme: %v", err)
	}
	return scheme
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
				Group:   "",
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
		name      string
		master    *placementv1beta1.ClusterResourceSnapshot
		snapshots []placementv1beta1.ClusterResourceSnapshot
		croList   []placementv1alpha1.ClusterResourceOverrideSnapshot
		roList    []placementv1alpha1.ResourceOverrideSnapshot
		wantCRO   []*placementv1alpha1.ClusterResourceOverrideSnapshot
		wantRO    []*placementv1alpha1.ResourceOverrideSnapshot
	}{
		{
			name: "single resource snapshot selecting empty resources",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel: "0",
						placementv1beta1.CRPTrackingLabel:   crpName,
					},
					Annotations: map[string]string{
						placementv1beta1.ResourceGroupHashAnnotation:         "abc",
						placementv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			croList: []placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{},
				},
			},
			wantCRO: []*placementv1alpha1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1alpha1.ResourceOverrideSnapshot{},
		},
		{
			name: "single resource snapshot with no matched overrides",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel: "0",
						placementv1beta1.CRPTrackingLabel:   crpName,
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
			croList: []placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			roList: []placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			wantCRO: []*placementv1alpha1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1alpha1.ResourceOverrideSnapshot{},
		},
		{
			name: "single resource snapshot with matched cro and ro",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel: "0",
						placementv1beta1.CRPTrackingLabel:   crpName,
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
			croList: []placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			roList: []placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			wantCRO: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantRO: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			name: "single resource snapshot with matched stale cro and ro snapshot",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel: "0",
						placementv1beta1.CRPTrackingLabel:   crpName,
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
			croList: []placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			roList: []placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			wantCRO: []*placementv1alpha1.ClusterResourceOverrideSnapshot{},
			wantRO:  []*placementv1alpha1.ResourceOverrideSnapshot{},
		},
		{
			name: "multiple resource snapshots with matched cro and ro",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel: "0",
						placementv1beta1.CRPTrackingLabel:   crpName,
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
			snapshots: []placementv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 0),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel: "0",
							placementv1beta1.CRPTrackingLabel:   crpName,
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 1),
						Labels: map[string]string{
							placementv1beta1.ResourceIndexLabel: "0",
							placementv1beta1.CRPTrackingLabel:   crpName,
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
			croList: []placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			roList: []placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
								{
									Group:   "",
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
								{
									Group:   "",
									Version: "v1",
									Kind:    "Deployment",
									Name:    "deployment-name",
								},
							},
						},
					},
				},
			},
			wantCRO: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantRO: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
								{
									Group:   "",
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
								{
									Group:   "",
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
			name: "single resource snapshot with multiple matched cro and ro",
			master: &placementv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(placementv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						placementv1beta1.ResourceIndexLabel: "0",
						placementv1beta1.CRPTrackingLabel:   crpName,
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
			croList: []placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			roList: []placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
			wantCRO: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							ClusterResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantRO: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							ResourceSelectors: []placementv1alpha1.ResourceSelector{
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{tc.master}
			for i := range tc.snapshots {
				objects = append(objects, &tc.snapshots[i])
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
			gotCRO, gotRO, err := FetchAllMatchOverridesForResourceSnapshot(context.Background(), fakeClient, &fakeInformer, crpName, tc.master)
			if err != nil {
				t.Fatalf("fetchAllMatchingOverridesForResourceSnapshot() failed, got err %v, want no err", err)
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmpopts.SortSlices(func(o1, o2 placementv1alpha1.ClusterResourceOverride) bool {
					return o1.Name < o2.Name
				}),
				cmpopts.SortSlices(func(o1, o2 placementv1alpha1.ResourceOverride) bool {
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
		croList []*placementv1alpha1.ClusterResourceOverrideSnapshot
		roList  []*placementv1alpha1.ResourceOverrideSnapshot
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
			croList: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			roList: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			croList: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			wantErr: ErrExpectedBehavior,
		},
		{
			name: "matched overrides with empty cluster label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			},
			croList: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			roList: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			croList: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			roList: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "test",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			croList: []*placementv1alpha1.ClusterResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ClusterResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ClusterResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
			roList: []*placementv1alpha1.ResourceOverrideSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "test",
						Labels: map[string]string{
							placementv1beta1.IsLatestSnapshotLabel: "true",
						},
					},
					Spec: placementv1alpha1.ResourceOverrideSnapshotSpec{
						OverrideSpec: placementv1alpha1.ResourceOverrideSpec{
							Policy: &placementv1alpha1.OverridePolicy{
								OverrideRules: []placementv1alpha1.OverrideRule{
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
