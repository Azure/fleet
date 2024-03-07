/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

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
	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/test/utils/informer"
	"go.goms.io/fleet/test/utils/resource"
)

var (
	crpName = "my-test-crp"
)

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add placement v1beta1 scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add cluster v1beta1 scheme: %v", err)
	}
	if err := fleetv1alpha1.AddToScheme(scheme); err != nil {
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
		master    *fleetv1beta1.ClusterResourceSnapshot
		snapshots []fleetv1beta1.ClusterResourceSnapshot
		croList   []fleetv1alpha1.ClusterResourceOverride
		roList    []fleetv1alpha1.ResourceOverride
		wantCRO   []*fleetv1alpha1.ClusterResourceOverride
		wantRO    []*fleetv1alpha1.ResourceOverride
	}{
		{
			name: "single resource snapshot selecting empty resources",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
			},
			croList: []fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{},
				},
			},
			wantCRO: []*fleetv1alpha1.ClusterResourceOverride{},
			wantRO:  []*fleetv1alpha1.ResourceOverride{},
		},
		{
			name: "single resource snapshot with no matched overrides",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			roList: []fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ro-1",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			wantCRO: []*fleetv1alpha1.ClusterResourceOverride{},
			wantRO:  []*fleetv1alpha1.ResourceOverride{},
		},
		{
			name: "single resource snapshot with matched cro and ro",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			roList: []fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			wantCRO: []*fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			wantRO: []*fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
		{
			name: "multiple resource snapshots with matched cro and ro",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "3",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						*resource.NamespaceResourceContentForTest(t),
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			snapshots: []fleetv1beta1.ClusterResourceSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 0),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crpName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							*resource.DeploymentResourceContentForTest(t),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameWithSubindexFmt, crpName, 0, 1),
						Labels: map[string]string{
							fleetv1beta1.ResourceIndexLabel: "0",
							fleetv1beta1.CRPTrackingLabel:   crpName,
						},
						Annotations: map[string]string{
							fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "1",
						},
					},
					Spec: fleetv1beta1.ResourceSnapshotSpec{
						SelectedResources: []fleetv1beta1.ResourceContent{
							*resource.ClusterRoleResourceContentForTest(t),
						},
					},
				},
			},
			croList: []fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
							{
								Group:   "rbac.authorization.k8s.io",
								Version: "v1",
								Kind:    "ClusterRole",
								Name:    "not-exist",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			roList: []fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			wantCRO: []*fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			wantRO: []*fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Deployment",
								Name:    "deployment-name",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
		{
			// not supported in the first phase
			name: "single resource snapshot with multiple matched cro and ro",
			master: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf(fleetv1beta1.ResourceSnapshotNameFmt, crpName, 0),
					Labels: map[string]string{
						fleetv1beta1.ResourceIndexLabel: "0",
						fleetv1beta1.CRPTrackingLabel:   crpName,
					},
					Annotations: map[string]string{
						fleetv1beta1.ResourceGroupHashAnnotation:         "abc",
						fleetv1beta1.NumberOfResourceSnapshotsAnnotation: "1",
					},
				},
				Spec: fleetv1beta1.ResourceSnapshotSpec{
					SelectedResources: []fleetv1beta1.ResourceContent{
						*resource.ServiceResourceContentForTest(t),
					},
				},
			},
			croList: []fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Namespace",
								Name:    "svc-namespace",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			roList: []fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Service",
								Name:    "svc-name",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			wantCRO: []*fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Namespace",
								Name:    "svc-namespace",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						ClusterResourceSelectors: []fleetv1beta1.ClusterResourceSelector{
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
			wantRO: []*fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Service",
								Name:    "svc-name",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						ResourceSelectors: []fleetv1alpha1.ResourceSelector{
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
			r := Reconciler{
				Client:          fakeClient,
				InformerManager: &fakeInformer,
			}
			gotCRO, gotRO, err := r.fetchAllMatchingOverridesForResourceSnapshot(context.Background(), crpName, tc.master)
			if err != nil {
				t.Fatalf("fetchAllMatchingOverridesForResourceSnapshot() failed, got err %v, want no err", err)
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmpopts.SortSlices(func(o1, o2 fleetv1alpha1.ClusterResourceOverride) bool {
					return o1.Name < o2.Name
				}),
				cmpopts.SortSlices(func(o1, o2 fleetv1alpha1.ResourceOverride) bool {
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
	tests := []struct {
		name    string
		cluster *clusterv1beta1.MemberCluster
		croList []*fleetv1alpha1.ClusterResourceOverride
		roList  []*fleetv1alpha1.ResourceOverride
		wantCRO []string
		wantRO  []fleetv1beta1.NamespacedName
		wantErr error
	}{
		{
			name: "cluster not found",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-not-exist",
				},
			},
			wantErr: controller.ErrExpectedBehavior,
		},
		{
			name: "empty overrides",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			wantCRO: []string{},
			wantRO:  []fleetv1beta1.NamespacedName{},
		},
		{
			name: "matched overrides with empty cluster label",
			cluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-1",
				},
			},
			croList: []*fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{}, // empty cluster label selects all clusters
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{}, // empty cluster label selects all clusters
							},
						},
					},
				},
			},
			roList: []*fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "svc-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{}, // empty cluster label selects all clusters
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "deployment-namespace",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{}, // empty cluster label selects all clusters
							},
						},
					},
				},
			},
			wantCRO: []string{"cro-1", "cro-2"},
			wantRO: []fleetv1beta1.NamespacedName{
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
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			croList: []*fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-2",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			roList: []*fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "test",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-2",
						Namespace: "test",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			wantCRO: []string{"cro-1"},
			wantRO: []fleetv1beta1.NamespacedName{
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
					Name: "cluster-1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			croList: []*fleetv1alpha1.ClusterResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cro-1",
					},
					Spec: fleetv1alpha1.ClusterResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			roList: []*fleetv1alpha1.ResourceOverride{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ro-1",
						Namespace: "test",
					},
					Spec: fleetv1alpha1.ResourceOverrideSpec{
						Policy: &fleetv1alpha1.OverridePolicy{
							OverrideRules: []fleetv1alpha1.OverrideRule{
								{
									ClusterSelector: &fleetv1beta1.ClusterSelector{
										ClusterSelectorTerms: []fleetv1beta1.ClusterSelectorTerm{
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
			wantCRO: []string{},
			wantRO:  []fleetv1beta1.NamespacedName{},
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
			r := Reconciler{
				Client: fakeClient,
			}
			binding := &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "binding-1",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					TargetCluster: "cluster-1",
				},
			}
			gotCRO, gotRO, err := r.pickFromResourceMatchedOverridesForTargetCluster(context.Background(), binding, tc.croList, tc.roList)
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
