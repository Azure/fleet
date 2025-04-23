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

package resourcechange

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
	"github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

var _ controller.Controller = &fakeController{}

// fakeController implements Controller interface
type fakeController struct {
	// the last queued obj
	QueueObj []string
}

func (w *fakeController) Run(_ context.Context, _ int) error {
	//TODO implement me
	panic("implement me")
}

func (w *fakeController) Enqueue(obj interface{}) {
	w.QueueObj = append(w.QueueObj, obj.(string))
}

func TestFindPlacementsSelectedDeletedResV1Alpha1(t *testing.T) {
	deletedRes := fleetv1alpha1.ResourceIdentifier{
		Group:     "abc",
		Name:      "foo",
		Namespace: "bar",
	}
	tests := map[string]struct {
		clusterWideKey keys.ClusterWideKey
		crpList        []*fleetv1alpha1.ClusterResourcePlacement
		wantCrp        []string
	}{
		"match a placement that selected the deleted resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{
							deletedRes,
						},
					},
				},
			},
			wantCrp: []string{"resource-selected"},
		},
		"match all the placements that selected the deleted resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{
							deletedRes,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected-2",
					},
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{
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
		"does not match placement that has selected some other resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{
							{
								Group:     "xyz",
								Name:      "not-deleted",
								Namespace: "bar",
							},
						},
					},
				},
			},
			wantCrp: nil,
		},
		"does not match placement that has not selected any resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedRes},
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{},
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
				PlacementControllerV1Alpha1: &placementController,
			}
			var crpList []runtime.Object
			for _, crp := range tt.crpList {
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crp)
				crpList = append(crpList, &unstructured.Unstructured{Object: uMap})
			}
			r.findPlacementsSelectedDeletedResV1Alpha1(tt.clusterWideKey, crpList)
			if !reflect.DeepEqual(placementController.QueueObj, tt.wantCrp) {
				t.Errorf("test case `%s` got crp = %v, wantCrp %v", name, placementController.QueueObj, tt.wantCrp)
				return
			}
		})
	}
}

func TestFindPlacementsSelectedDeletedResV1Beta11(t *testing.T) {
	// Perform some expedient duplication to accommodate the version differences.
	deletedResV1Alpha1 := fleetv1alpha1.ResourceIdentifier{
		Group:     "abc",
		Name:      "foo",
		Namespace: "bar",
	}
	deletedResV1Beta1 := placementv1beta1.ResourceIdentifier{
		Group:     "abc",
		Name:      "foo",
		Namespace: "bar",
	}

	tests := map[string]struct {
		clusterWideKey keys.ClusterWideKey
		crpList        []*placementv1beta1.ClusterResourcePlacement
		wantCrp        []string
	}{
		"match a placement that selected the deleted resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							deletedResV1Beta1,
						},
					},
				},
			},
			wantCrp: []string{"resource-selected"},
		},
		"match all the placements that selected the deleted resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							deletedResV1Beta1,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected-2",
					},
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							deletedResV1Beta1,
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
		"does not match placement that has selected some other resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "xyz",
								Name:      "not-deleted",
								Namespace: "bar",
							},
						},
					},
				},
			},
			wantCrp: nil,
		},
		"does not match placement that has not selected any resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{},
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
				PlacementControllerV1Alpha1: &placementController,
			}
			var crpList []runtime.Object
			for _, crp := range tt.crpList {
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crp)
				crpList = append(crpList, &unstructured.Unstructured{Object: uMap})
			}
			r.findPlacementsSelectedDeletedResV1Alpha1(tt.clusterWideKey, crpList)
			if !reflect.DeepEqual(placementController.QueueObj, tt.wantCrp) {
				t.Errorf("test case `%s` got crp = %v, wantCrp %v", name, placementController.QueueObj, tt.wantCrp)
				return
			}
		})
	}
}

func TestCollectAllAffectedPlacementsV1Alpha1(t *testing.T) {
	// the resource we use for all the tests
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
		crpList []*fleetv1alpha1.ClusterResourcePlacement
		wantCrp map[string]bool
	}{
		"match a place with the matching label": {
			res: matchRes,
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
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
		"does not match a place with no selector": {
			res: matchRes,
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
		"match a place with the name selector": {
			res: matchRes,
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								Name:    matchRes.Name,
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"match a place with a match Expressions label": {
			res: matchRes,
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
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
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
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
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"region": matchRes.Labels["region"],
										// the mis-matching label
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
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
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
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"region": matchRes.Labels["region"]},
								},
							},
							{
								// the mis-matching label selector
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
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						// the mis-matching resource selector
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
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
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{
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
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
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
					Status: fleetv1alpha1.ClusterResourcePlacementStatus{
						SelectedResources: []fleetv1alpha1.ResourceIdentifier{
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
		"don't select placement with name, nil label selector for namespace with different name": {
			res: matchRes,
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    "Namespace",
								Name:    "test-namespace-1",
							},
						},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
		"select placement with empty name, nil label selector for namespace": {
			res: matchRes,
			crpList: []*fleetv1alpha1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
						ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    "Namespace",
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
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
			validator.ResourceInformer = &informer.FakeManager{}
			got := collectAllAffectedPlacementsV1Alpha1(&unstructured.Unstructured{Object: uRes}, crpList)
			if !reflect.DeepEqual(got, tt.wantCrp) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCrp)
			}
		})
	}
}

func TestCollectAllAffectedPlacementsV1Beta1(t *testing.T) {
	// the resource we use for all the tests
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
		crpList []*placementv1beta1.ClusterResourcePlacement
		wantCrp map[string]bool
	}{
		"match a place with the matching label": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
		"does not match a place with no selector": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
		"match a place with the name selector": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								Name:    matchRes.Name,
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"match a place with a match Expressions label": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"region": matchRes.Labels["region"],
										// the mis-matching label
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
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"region": matchRes.Labels["region"]},
								},
							},
							{
								// the mis-matching label selector
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
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						// the mis-matching resource selector
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
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
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
					Status: placementv1beta1.ClusterResourcePlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
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
		"don't select placement with name, nil label selector for namespace with different name": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    "Namespace",
								Name:    "test-namespace-1",
							},
						},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
		"select placement with empty name, nil label selector for namespace": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.ClusterResourcePlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    "Namespace",
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
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
			validator.ResourceInformer = &informer.FakeManager{}
			got := collectAllAffectedPlacementsV1Beta1(&unstructured.Unstructured{Object: uRes}, crpList)
			if !reflect.DeepEqual(got, tt.wantCrp) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCrp)
			}
		})
	}
}
