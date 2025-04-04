/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package drain

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

func TestFetchClusterResourcePlacementToEvict(t *testing.T) {
	tests := []struct {
		name          string
		targetCluster string
		bindings      []placementv1beta1.ClusterResourceBinding
		works         []placementv1beta1.Work
		wantErr       error
		wantMap       map[string][]placementv1beta1.ResourceIdentifier
	}{
		{
			name:          "successfully collected resources for CRP",
			targetCluster: "test-cluster1",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster1",
						State:         placementv1beta1.BindingStateBound,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb2",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster2",
						State:         placementv1beta1.BindingStateBound,
					},
				},
			},
			works: []placementv1beta1.Work{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-work1",
						Namespace: fmt.Sprintf(utils.NamespaceNameFormat, "test-cluster1"),
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Status: placementv1beta1.WorkStatus{
						ManifestConditions: []placementv1beta1.ManifestCondition{
							{
								Identifier: placementv1beta1.WorkResourceIdentifier{
									Group:   "test-group1",
									Version: "test-version1",
									Kind:    "test-kind1",
									Name:    "test-name1",
								},
								Conditions: []metav1.Condition{
									{
										Type:   placementv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-work2",
						Namespace: fmt.Sprintf(utils.NamespaceNameFormat, "test-cluster1"),
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Status: placementv1beta1.WorkStatus{
						ManifestConditions: []placementv1beta1.ManifestCondition{
							{
								Identifier: placementv1beta1.WorkResourceIdentifier{
									Group:   "test-group2",
									Version: "test-version2",
									Kind:    "test-kind2",
									Name:    "test-name2",
								},
								Conditions: []metav1.Condition{
									{
										Type:   placementv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-work3",
						Namespace: fmt.Sprintf(utils.NamespaceNameFormat, "test-cluster2"),
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Status: placementv1beta1.WorkStatus{
						ManifestConditions: []placementv1beta1.ManifestCondition{
							{
								Identifier: placementv1beta1.WorkResourceIdentifier{
									Group:   "test-group3",
									Version: "test-version3",
									Kind:    "test-kind3",
									Name:    "test-name3",
								},
								Conditions: []metav1.Condition{
									{
										Type:   placementv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantErr: nil,
			wantMap: map[string][]placementv1beta1.ResourceIdentifier{
				"test-crp1": {
					{
						Group:   "test-group1",
						Version: "test-version1",
						Kind:    "test-kind1",
						Name:    "test-name1",
					},
					{
						Group:   "test-group2",
						Version: "test-version2",
						Kind:    "test-kind2",
						Name:    "test-name2",
					},
				},
			},
		},
		{
			name:          "failed to wait for placement to be present",
			targetCluster: "test-cluster1",
			bindings: []placementv1beta1.ClusterResourceBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crb1",
						Labels: map[string]string{
							placementv1beta1.CRPTrackingLabel: "test-crp1",
						},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						TargetCluster: "test-cluster1",
						State:         placementv1beta1.BindingStateScheduled,
					},
				},
			},
			wantMap: map[string][]placementv1beta1.ResourceIdentifier{},
			wantErr: fmt.Errorf("failed to wait for placement to be present on member cluster: timed out waiting for the condition"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var objects []client.Object
			scheme := serviceScheme(t)
			for i := range tc.bindings {
				objects = append(objects, &tc.bindings[i])
			}
			for i := range tc.works {
				objects = append(objects, &tc.works[i])
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			d := &Helper{
				HubClient:                            fakeClient,
				ClusterName:                          tc.targetCluster,
				ClusterResourcePlacementResourcesMap: make(map[string][]placementv1beta1.ResourceIdentifier),
			}
			gotErr := d.fetchClusterResourcePlacementToEvict(context.Background())
			if tc.wantErr == nil {
				if gotErr != nil {
					t.Errorf("fetchClusterResourcePlacementToEvict() test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
				}
			} else if gotErr == nil || gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("fetchClusterResourcePlacementToEvict() test %s failed, got error %v, want error %v", tc.name, gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(d.ClusterResourcePlacementResourcesMap, tc.wantMap); diff != "" {
				t.Errorf("fetchClusterResourcePlacementToEvict() test %s failed, got %v, want %v", tc.name, d.ClusterResourcePlacementResourcesMap, tc.wantMap)
			}
		})
	}
}

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add placement v1beta1 scheme: %v", err)
	}
	return scheme
}
