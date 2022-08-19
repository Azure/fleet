/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package resourcechange

import (
	"context"
	"reflect"
	"testing"

	utilrand "k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	. "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/keys"
)

var _ controller.Controller = &fakeController{}

// fakeController implements Controller interface
type fakeController struct {
	// the last queued obj
	QueueObj []string
}

func (w *fakeController) Run(ctx context.Context, workerNumber int) error {
	//TODO implement me
	panic("implement me")
}

func (w *fakeController) Enqueue(obj interface{}) {
	w.QueueObj = append(w.QueueObj, obj.(string))
}

func TestTriggerAffectedPlacementsForDeletedClusterRes(t *testing.T) {
	deletedRes := ResourceIdentifier{
		Group:     "abc",
		Name:      "foo",
		Namespace: "bar",
	}
	tests := map[string]struct {
		clusterWideKey keys.ClusterWideKey
		crpList        []*ClusterResourcePlacement
		wantCrp        []string
	}{
		"find a place given the matching": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: ClusterResourcePlacementStatus{
						SelectedResources: []ResourceIdentifier{
							deletedRes,
						},
					},
				},
			},
			wantCrp: []string{"resource-selected"},
		},
		"find many places given the matching": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: ClusterResourcePlacementStatus{
						SelectedResources: []ResourceIdentifier{
							deletedRes,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected-2",
					},
					Status: ClusterResourcePlacementStatus{
						SelectedResources: []ResourceIdentifier{
							deletedRes,
							{
								Group:     "abc",
								Name:      "foo",
								Namespace: "bar",
							},
						},
					},
				},
			},
			wantCrp: []string{"resource-selected", "resource-selected-2"},
		},
		"find no place given the matching": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: ClusterResourcePlacementStatus{
						SelectedResources: []ResourceIdentifier{
							{
								Group:     "abd",
								Name:      "not-deleted",
								Namespace: "bar",
							},
						},
					},
				},
			},
			wantCrp: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			placementController := fakeController{}
			r := &Reconciler{
				PlacementController: &placementController,
			}
			var crpList []runtime.Object
			for _, crp := range tt.crpList {
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crp)
				crpList = append(crpList, &unstructured.Unstructured{Object: uMap})
			}
			got, err := r.findExistingPlacements(tt.clusterWideKey, crpList)
			if !reflect.DeepEqual(placementController.QueueObj, tt.wantCrp) {
				t.Errorf("test case `%s` got crp = %v, wantCrp %v", name, placementController.QueueObj, tt.wantCrp)
				return
			}
			if err != nil {
				t.Errorf("test case `%s` got error = %v, wantErr %v", name, err, nil)
				return
			}
			if !reflect.DeepEqual(got, ctrl.Result{}) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, ctrl.Result{})
			}
		})
	}
}

func TestCollectAllAffectedPlacements(t *testing.T) {
	matchRes := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nameSpace",
			Labels: map[string]string{
				"region":  utilrand.String(10),
				"version": utilrand.String(4),
			},
		},
	}
	tests := map[string]struct {
		res     *corev1.Namespace
		crpList []*ClusterResourcePlacement
		wantCrp map[string]bool
	}{
		"match a place with the matching label": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: matchRes.Labels,
								},
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"match a place with a match Expressions label": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "random",
											Operator: metav1.LabelSelectorOpDoesNotExist,
										},
									},
								},
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"match a place with a single matching label": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"region": matchRes.Labels["region"]},
								},
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"does not match a place with a miss matching label": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"region": matchRes.Labels["region"],
										"random": "doesnotmatter",
									},
								},
							},
						},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
		"match a place with multiple matching resource selectors": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"region": matchRes.Labels["region"]},
								},
							},
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "version",
											Operator: metav1.LabelSelectorOpExists,
										},
									},
								},
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"match a place with only one matching resource selectors": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"region": matchRes.Labels["region"]},
								},
							},
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "version",
											Operator: metav1.LabelSelectorOpDoesNotExist,
										},
									},
								},
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"match a place with a miss matching label but was selected": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"random": "doesnotmatter",
									},
								},
							},
						},
					},
					Status: ClusterResourcePlacementStatus{
						SelectedResources: []ResourceIdentifier{
							{
								Group:     corev1.GroupName,
								Version:   "v1",
								Kind:      matchRes.Kind,
								Name:      matchRes.Name,
								Namespace: "",
							},
							{
								Group:     corev1.GroupName,
								Version:   "v1beta2",
								Kind:      "Pod",
								Name:      matchRes.Name,
								Namespace: "",
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"does not match a place with a miss matching label and was not selected": {
			res: matchRes,
			crpList: []*ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: ClusterResourcePlacementSpec{
						ResourceSelectors: []ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"random": "doesnotmatter",
									},
								},
							},
						},
					},
					Status: ClusterResourcePlacementStatus{
						SelectedResources: []ResourceIdentifier{
							{
								Group:     corev1.GroupName,
								Version:   "v1beta2",
								Kind:      "Pod",
								Name:      matchRes.Name,
								Namespace: "",
							},
						},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var crpList []runtime.Object
			for _, crp := range tt.crpList {
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crp)
				crpList = append(crpList, &unstructured.Unstructured{Object: uMap})
			}
			uRes, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(tt.res)
			got := collectAllAffectedPlacements(&unstructured.Unstructured{Object: uRes}, crpList)
			if !reflect.DeepEqual(got, tt.wantCrp) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCrp)
			}
		})
	}
}
