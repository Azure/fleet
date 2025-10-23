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
	"errors"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
	testinformer "github.com/kubefleet-dev/kubefleet/test/utils/informer"
)

var _ controller.Controller = &fakeController{}

var (
	sortSlicesOption = cmpopts.SortSlices(func(a, b string) bool {
		return a < b
	})
)

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
	if w.QueueObj == nil {
		w.QueueObj = []string{}
	}
	w.QueueObj = append(w.QueueObj, obj.(string))
}

func TestFindPlacementsSelectedDeletedResV1Beta1(t *testing.T) {
	// Perform some expedient duplication to accommodate the version differences.
	deletedResV1Alpha1 := placementv1beta1.ResourceIdentifier{
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
		placementName  []string
	}{
		"match a placement that selected the deleted resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.PlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							deletedResV1Beta1,
						},
					},
				},
			},
			placementName: []string{"resource-selected"},
		},
		"match all the placements that selected the deleted resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.PlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							deletedResV1Beta1,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected-2",
					},
					Status: placementv1beta1.PlacementStatus{
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
			placementName: []string{"resource-selected", "resource-selected-2"},
		},
		"does not match placement that has selected some other resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.PlacementStatus{
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
			placementName: []string{},
		},
		"does not match placement that has not selected any resource": {
			clusterWideKey: keys.ClusterWideKey{ResourceIdentifier: deletedResV1Alpha1},
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Status: placementv1beta1.PlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{},
					},
				},
			},
			placementName: []string{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var placementList []placementv1beta1.PlacementObj
			for _, crp := range tt.crpList {
				placementList = append(placementList, crp)
			}
			got := findPlacementsSelectedDeletedResV1Beta1(tt.clusterWideKey, placementList)
			if !cmp.Equal(got, tt.placementName, sortSlicesOption) {
				t.Errorf("findPlacementsSelectedDeletedResV1Beta1() = %v, want %v", got, tt.placementName)
				return
			}
		})
	}
}

func TestCollectAllAffectedPlacementsV1Beta1_ClusterResourcePlacement(t *testing.T) {
	// the resource we use for all the tests
	matchRes := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
			Labels: map[string]string{
				"region":  rand.String(10),
				"version": rand.String(4),
			},
		},
	}

	// Common ResourceIdentifier for Namespace tests (cluster-scoped)
	namespaceResourceIdentifier := placementv1beta1.ResourceIdentifier{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
		Name:    "test-namespace",
	}

	// Common ResourceIdentifier for namespace-scoped resource tests
	namespaceScopedResourceIdentifier := placementv1beta1.ResourceIdentifier{
		Group:     "apps",
		Version:   "v1",
		Kind:      "Deployment",
		Name:      "test-deployment",
		Namespace: "test-namespace",
	}

	tests := map[string]struct {
		key     keys.ClusterWideKey
		res     *corev1.Namespace
		crpList []*placementv1beta1.ClusterResourcePlacement
		wantCRP map[string]bool
	}{
		"match a place with the matching label": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"Skip a placement with selecting ns only": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceScopedResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: matchRes.Labels,
								},
								SelectionScope: placementv1beta1.NamespaceOnly,
							},
						},
					},
				},
			},
			wantCRP: make(map[string]bool),
		},
		"does not match a place with no selector": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
					},
				},
			},
			wantCRP: make(map[string]bool),
		},
		"match a place with the name selector": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"match a place with a match Expressions label": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"match a place with a single matching label": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"does not match a place with a miss matching label": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: make(map[string]bool),
		},
		"match a place with multiple matching resource selectors": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"match a place with only one matching resource selectors": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"match a place with a miss matching label but was selected": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						// the mis-matching resource selector
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
					Status: placementv1beta1.PlacementStatus{
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
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"does not match a place with a miss matching label and was not selected": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
					Status: placementv1beta1.PlacementStatus{
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
			wantCRP: make(map[string]bool),
		},
		"don't select placement with name, nil label selector for namespace with different name": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
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
			wantCRP: make(map[string]bool),
		},
		"select placement with empty name, nil label selector for namespace": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    "Namespace",
							},
						},
					},
				},
			},
			wantCRP: map[string]bool{"resource-selected": true},
		},
		"match placement through status SelectedResources when selector does not match": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "status-matched-placement",
					},
					Spec: placementv1beta1.PlacementSpec{
						// Selector that does not match the resource
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"nonexistent": "label",
									},
								},
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						// But the resource is in the selected resources status
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     corev1.GroupName,
								Version:   "v1",
								Kind:      matchRes.Kind,
								Name:      matchRes.Name,
								Namespace: "",
							},
						},
					},
				},
			},
			wantCRP: map[string]bool{"status-matched-placement": true},
		},
		"does not match placement with different GVK selector": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-not-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "rbac.authorization.k8s.io",
								Version: "v1",
								Kind:    "ClusterRole",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: matchRes.Labels,
								},
							},
						},
					},
				},
			},
			wantCRP: make(map[string]bool),
		},
		"match ClusterResourcePlacement with previously selected resource": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceScopedResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crp-with-selected-resource",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"nonexistent": "label",
									},
								},
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      "test-deployment",
								Namespace: "test-namespace",
							},
						},
					},
				},
			},
			wantCRP: map[string]bool{"crp-with-selected-resource": true},
		},
		"match ClusterResourcePlacement (even with namespace only) with previously selected resource": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceScopedResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crp-with-selected-resource",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"nonexistent": "label",
									},
								},
								SelectionScope: placementv1beta1.NamespaceOnly,
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      "test-deployment",
								Namespace: "test-namespace",
							},
						},
					},
				},
			},
			wantCRP: map[string]bool{"crp-with-selected-resource": true},
		},
		"does not match ClusterResourcePlacement with previously selected resource when namespace is different": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: namespaceScopedResourceIdentifier,
			},
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crp-with-selected-resource",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   corev1.GroupName,
								Version: "v1",
								Kind:    matchRes.Kind,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"nonexistent": "label",
									},
								},
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      "test-deployment",
								Namespace: "different-namespace",
							},
						},
					},
				},
			},
			wantCRP: make(map[string]bool),
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
			clusterPlacements := convertToClusterResourcePlacements(crpList)
			got := collectAllAffectedPlacementsV1Beta1(tt.key, &unstructured.Unstructured{Object: uRes}, clusterPlacements)
			if !reflect.DeepEqual(got, tt.wantCRP) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCRP)
			}
		})
	}
}

func TestCollectAllAffectedPlacementsV1Beta1_ResourcePlacement(t *testing.T) {
	// the resource we use for all the tests - a deployment in a namespace
	matchRes := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "test-deployment",
				"namespace": "test-namespace",
				"labels": map[string]interface{}{
					"app":     "test-app",
					"version": "v1.0",
				},
			},
		},
	}
	// Ensure GVK is properly set
	matchRes.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	})

	// Common ResourceIdentifier for Deployment tests
	deploymentResourceIdentifier := placementv1beta1.ResourceIdentifier{
		Group:     "apps",
		Version:   "v1",
		Kind:      "Deployment",
		Name:      "test-deployment",
		Namespace: "test-namespace",
	}

	tests := map[string]struct {
		key    keys.ClusterWideKey
		res    *unstructured.Unstructured
		rpList []*placementv1beta1.ResourcePlacement
		wantRP map[string]bool
	}{
		"match ResourcePlacement with the matching label": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "test-app",
									},
								},
							},
						},
					},
				},
			},
			wantRP: map[string]bool{"resource-selected": true},
		},
		"does not match ResourcePlacement with no selector": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{},
					},
				},
			},
			wantRP: make(map[string]bool),
		},
		"match ResourcePlacement with the name selector": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
								Name:    "test-deployment",
								// use the namespace from the resource placement
							},
						},
					},
				},
			},
			wantRP: map[string]bool{"resource-selected": true},
		},
		"does not match ResourcePlacement with different name": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
								Name:    "different-deployment",
							},
						},
					},
				},
			},
			wantRP: make(map[string]bool),
		},
		"match ResourcePlacement with previously selected resource": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						// Selector that does not match the resource
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"different": "label",
									},
								},
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						// But the resource is in the selected resources status
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      "test-deployment",
								Namespace: "test-namespace",
							},
						},
					},
				},
			},
			wantRP: map[string]bool{"resource-selected": true},
		},
		"select ResourcePlacement with empty name, nil label selector for deployment": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
							},
						},
					},
				},
			},
			wantRP: map[string]bool{"resource-selected": true},
		},
		"does not match ResourcePlacement with different GVK selector": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-not-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "",
								Version: "v1",
								Kind:    "ConfigMap",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app":     "test-app",
										"version": "v1.0",
									},
								},
							},
						},
					},
				},
			},
			wantRP: make(map[string]bool),
		},
		"does not match ResourcePlacement through status SelectedResources when name is different": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: deploymentResourceIdentifier,
			},
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "status-matched-rp",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						// Selector that does not match the resource
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"nonexistent": "label",
									},
								},
							},
						},
					},
					Status: placementv1beta1.PlacementStatus{
						// But the resource is in the selected resources status
						SelectedResources: []placementv1beta1.ResourceIdentifier{
							{
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Name:      "different-deployment",
								Namespace: "test-namespace",
							},
						},
					},
				},
			},
			wantRP: make(map[string]bool),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var rpList []runtime.Object
			for _, rp := range tt.rpList {
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(rp)
				rpList = append(rpList, &unstructured.Unstructured{Object: uMap})
			}
			resourcePlacements := convertToResourcePlacements(rpList)
			got := collectAllAffectedPlacementsV1Beta1(tt.key, tt.res, resourcePlacements)
			if !reflect.DeepEqual(got, tt.wantRP) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantRP)
			}
		})
	}
}

func createTestNamespace() *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
			Labels: map[string]string{
				"app": "test",
			},
		},
	}
}

func createNamespaceUnstructured(namespace *corev1.Namespace) *unstructured.Unstructured {
	uObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(namespace)
	result := &unstructured.Unstructured{Object: uObj}
	result.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})
	return result
}

func createTestDeployment() *appv1.Deployment {
	return &appv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"app": "test",
			},
		},
	}
}

func createDeploymentUnstructured(deployment *appv1.Deployment) *unstructured.Unstructured {
	uObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	result := &unstructured.Unstructured{Object: uObj}
	result.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	})
	return result
}

func TestHandleUpdatedResource(t *testing.T) {
	// Test resources
	testNamespace := createNamespaceUnstructured(createTestNamespace())
	testDeployment := createDeploymentUnstructured(createTestDeployment())

	// Test CRP and RP
	testCRP := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			},
		},
	}

	testRP := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rp",
			Namespace: "test-namespace",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			},
		},
	}

	defaultFakeInformerManager := &testinformer.FakeManager{
		Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
			{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
				Objects: func() []runtime.Object {
					uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP)
					return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
				}(),
			},
			{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
				Objects: func() []runtime.Object {
					uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP)
					return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
				}(),
			},
			{Group: "", Version: "v1", Resource: "namespaces"}: {
				Objects: func() []runtime.Object {
					uObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testNamespace)
					return []runtime.Object{&unstructured.Unstructured{Object: uObj}}
				}(),
			},
		},
	}

	tests := map[string]struct {
		key                                     keys.ClusterWideKey
		clusterObj                              runtime.Object
		isClusterScoped                         bool
		informerManager                         informer.Manager
		wantPlacementControllerEnqueued         []string
		wantResourcePlacementControllerEnqueued []string
		wantResult                              ctrl.Result
		wantError                               bool
	}{
		"cluster-scoped resource triggers only ClusterResourcePlacement": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			clusterObj:                              testNamespace,
			isClusterScoped:                         true,
			informerManager:                         defaultFakeInformerManager,
			wantPlacementControllerEnqueued:         []string{"test-crp"},
			wantResourcePlacementControllerEnqueued: []string{},
			wantResult:                              ctrl.Result{},
			wantError:                               false,
		},
		"cluster-scoped resource with no matching CRP": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			clusterObj: func() *unstructured.Unstructured {
				namespace := &corev1.Namespace{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Namespace",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				}
				return createNamespaceUnstructured(namespace)
			}(),
			isClusterScoped:                         true,
			informerManager:                         defaultFakeInformerManager,
			wantPlacementControllerEnqueued:         []string{},
			wantResourcePlacementControllerEnqueued: []string{},
			wantResult:                              ctrl.Result{},
			wantError:                               false,
		},
		"namespace-scoped resource triggers ResourcePlacement and parent namespace CRP": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			clusterObj:                              testDeployment,
			isClusterScoped:                         false,
			informerManager:                         defaultFakeInformerManager,
			wantPlacementControllerEnqueued:         []string{"test-crp"},
			wantResourcePlacementControllerEnqueued: []string{"test-namespace/test-rp"},
			wantResult:                              ctrl.Result{},
			wantError:                               false,
		},
		"namespace-scoped resource with missing parent namespace": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "missing-namespace",
				},
			},
			clusterObj:      testDeployment,
			isClusterScoped: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						Objects: []runtime.Object{},
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						Objects: []runtime.Object{},
					},
					{Group: "", Version: "v1", Resource: "namespaces"}: {
						Objects: []runtime.Object{},
					},
				},
			},
			wantPlacementControllerEnqueued:         []string{},
			wantResourcePlacementControllerEnqueued: []string{},
			wantResult:                              ctrl.Result{},
			wantError:                               false,
		},
		"namespace-scoped resource with no matching ResourcePlacements but matching namespace CRP": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			clusterObj:      testDeployment,
			isClusterScoped: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						Objects: func() []runtime.Object {
							uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP)
							return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
						}(),
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						Objects: []runtime.Object{},
					},
					{Group: "", Version: "v1", Resource: "namespaces"}: {
						Objects: func() []runtime.Object {
							uObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testNamespace)
							return []runtime.Object{&unstructured.Unstructured{Object: uObj}}
						}(),
					},
				},
			},
			wantPlacementControllerEnqueued:         []string{"test-crp"},
			wantResourcePlacementControllerEnqueued: []string{},
			wantResult:                              ctrl.Result{},
			wantError:                               false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reconciler := &Reconciler{
				InformerManager:             tt.informerManager,
				PlacementControllerV1Beta1:  &fakeController{QueueObj: []string{}},
				ResourcePlacementController: &fakeController{QueueObj: []string{}},
			}

			result, err := reconciler.handleUpdatedResource(tt.key, tt.clusterObj, tt.isClusterScoped)
			if gotErr := err != nil; gotErr != tt.wantError {
				t.Errorf("handleUpdatedResource() error = %v, wantError = %v", err, tt.wantError)
				return
			}

			if !cmp.Equal(result, tt.wantResult) {
				t.Errorf("handleUpdatedResource() = %v, want %v", result, tt.wantResult)
			}

			// Sort both slices before comparison to handle non-deterministic map iteration order
			gotPlacement := reconciler.PlacementControllerV1Beta1.(*fakeController).QueueObj
			if !cmp.Equal(gotPlacement, tt.wantPlacementControllerEnqueued, sortSlicesOption) {
				t.Errorf("handleUpdatedResource enqueues keys to PlacementControllerV1Beta1, got %v, want %v",
					gotPlacement, tt.wantPlacementControllerEnqueued)
			}

			gotResourcePlacement := reconciler.ResourcePlacementController.(*fakeController).QueueObj
			if !cmp.Equal(gotResourcePlacement, tt.wantResourcePlacementControllerEnqueued, sortSlicesOption) {
				t.Errorf("handleUpdatedResource enqueues keys to ResourcePlacementController, got %v, want %v",
					gotResourcePlacement, tt.wantResourcePlacementControllerEnqueued)
			}
		})
	}
}

func TestTriggerAffectedPlacementsForDeletedRes(t *testing.T) {
	// Test data for V1Beta1 ClusterResourcePlacement
	testCRP := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-v1beta1",
		},
		Status: placementv1beta1.PlacementStatus{
			SelectedResources: []placementv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Name:      "test-namespace",
					Namespace: "",
				},
			},
		},
	}

	// Test data for ResourcePlacement
	testRP := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rp",
			Namespace: "test-namespace",
		},
		Status: placementv1beta1.PlacementStatus{
			SelectedResources: []placementv1beta1.ResourceIdentifier{
				{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
		},
	}

	// Create test data for CRPs and RPs
	crpObjects := func() []runtime.Object {
		uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP)
		return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
	}()

	rpObjects := func() []runtime.Object {
		uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP)
		return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
	}()

	tests := map[string]struct {
		key             keys.ClusterWideKey
		isClusterScope  bool
		informerManager informer.Manager
		wantCRPEnqueued []string
		wantRPEnqueued  []string
		wantResult      ctrl.Result
		wantError       bool
	}{
		"cluster-scoped resource triggers ClusterResourcePlacement": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			isClusterScope: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
				},
			},
			wantCRPEnqueued: []string{"test-crp-v1beta1"},
			wantRPEnqueued:  []string{},
		},
		"cluster-scoped resource with no matching placements": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "different-namespace",
				},
			},
			isClusterScope: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
		},
		"namespace-scoped resource triggers ResourcePlacement": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScope: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ResourcePlacementGVR: {
						Objects: rpObjects,
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{"test-namespace/test-rp"},
		},
		"namespace-scoped resource with no matching ResourcePlacements": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "deployment",
					Namespace: "different-test-namespace",
				},
			},
			isClusterScope: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ResourcePlacementGVR: {
						Objects: rpObjects,
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
		},
		"cluster-scoped resource with V1Beta1 lister error": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			isClusterScope: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
						Err:     errors.New("test lister error"),
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
			wantError:       true,
		},
		"namespace-scoped resource with ResourcePlacement lister error": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScope: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ResourcePlacementGVR: {
						Objects: []runtime.Object{},
						Err:     errors.New("test lister error"),
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
			wantError:       true,
		},
		"cluster-scoped resource with empty informer data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			isClusterScope: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
		},
		"namespace-scoped resource with empty informer data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScope: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ResourcePlacementGVR: {
						Objects: []runtime.Object{},
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
		},
		"cluster-scope resource triggers multiple CRPs": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			isClusterScope: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: func() []runtime.Object {
							// Create multiple CRPs that have selected this namespace
							testCRP1 := &placementv1beta1.ClusterResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{
									Name: "test-crp-1",
								},
								Status: placementv1beta1.PlacementStatus{
									SelectedResources: []placementv1beta1.ResourceIdentifier{
										{
											Group:     "",
											Version:   "v1",
											Kind:      "Namespace",
											Name:      "test-namespace",
											Namespace: "",
										},
									},
								},
							}
							testCRP2 := &placementv1beta1.ClusterResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{
									Name: "test-crp-2",
								},
								Status: placementv1beta1.PlacementStatus{
									SelectedResources: []placementv1beta1.ResourceIdentifier{
										{
											Group:     "",
											Version:   "v1",
											Kind:      "Namespace",
											Name:      "test-namespace",
											Namespace: "",
										},
									},
								},
							}
							testCRP3 := &placementv1beta1.ClusterResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{
									Name: "test-crp-3",
								},
								Status: placementv1beta1.PlacementStatus{
									SelectedResources: []placementv1beta1.ResourceIdentifier{
										{
											Group:     "",
											Version:   "v1",
											Kind:      "Namespace",
											Name:      "different-namespace", // This CRP should not be triggered
											Namespace: "",
										},
									},
								},
							}
							uMap1, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP1)
							uMap2, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP2)
							uMap3, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP3)
							return []runtime.Object{
								&unstructured.Unstructured{Object: uMap1},
								&unstructured.Unstructured{Object: uMap2},
								&unstructured.Unstructured{Object: uMap3},
							}
						}(),
					},
				},
			},
			wantCRPEnqueued: []string{"test-crp-1", "test-crp-2"},
			wantRPEnqueued:  []string{},
		},
		"namespace-scoped resource triggers multiple RPs": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScope: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ResourcePlacementGVR: {
						Objects: func() []runtime.Object {
							// Create multiple ResourcePlacements that have selected this deployment
							testRP1 := &placementv1beta1.ResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "test-rp-1",
									Namespace: "test-namespace",
								},
								Status: placementv1beta1.PlacementStatus{
									SelectedResources: []placementv1beta1.ResourceIdentifier{
										{
											Group:     "apps",
											Version:   "v1",
											Kind:      "Deployment",
											Name:      "test-deployment",
											Namespace: "test-namespace",
										},
									},
								},
							}
							testRP2 := &placementv1beta1.ResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "test-rp-2",
									Namespace: "test-namespace",
								},
								Status: placementv1beta1.PlacementStatus{
									SelectedResources: []placementv1beta1.ResourceIdentifier{
										{
											Group:     "apps",
											Version:   "v1",
											Kind:      "Deployment",
											Name:      "test-deployment",
											Namespace: "test-namespace",
										},
									},
								},
							}
							testRP3 := &placementv1beta1.ResourcePlacement{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "test-rp-3",
									Namespace: "test-namespace",
								},
								Status: placementv1beta1.PlacementStatus{
									SelectedResources: []placementv1beta1.ResourceIdentifier{
										{
											Group:     "apps",
											Version:   "v1",
											Kind:      "Deployment",
											Name:      "different-deployment", // This RP should not be triggered
											Namespace: "test-namespace",
										},
									},
								},
							}
							uMap1, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP1)
							uMap2, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP2)
							uMap3, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP3)
							return []runtime.Object{
								&unstructured.Unstructured{Object: uMap1},
								&unstructured.Unstructured{Object: uMap2},
								&unstructured.Unstructured{Object: uMap3},
							}
						}(),
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{"test-namespace/test-rp-1", "test-namespace/test-rp-2"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reconciler := &Reconciler{
				InformerManager:             tt.informerManager,
				PlacementControllerV1Beta1:  &fakeController{QueueObj: []string{}},
				ResourcePlacementController: &fakeController{QueueObj: []string{}},
			}

			result, err := reconciler.triggerAffectedPlacementsForDeletedRes(tt.key, tt.isClusterScope)

			// Check error expectation
			if gotErr := err != nil; gotErr != tt.wantError {
				t.Errorf("triggerAffectedPlacementsForDeletedRes() error = %v, wantError = %v", err, tt.wantError)
				return
			}

			// Check result
			if !cmp.Equal(result, tt.wantResult) {
				t.Errorf("triggerAffectedPlacementsForDeletedRes() result = %v, want %v", result, tt.wantResult)
			}

			// Check placement controller enqueued items
			gotCRP := reconciler.PlacementControllerV1Beta1.(*fakeController).QueueObj
			if !cmp.Equal(gotCRP, tt.wantCRPEnqueued, sortSlicesOption) {
				t.Errorf("triggerAffectedPlacementsForDeletedRes enqueues keys to PlacementControllerV1Beta1, got %v, want %v",
					gotCRP, tt.wantCRPEnqueued)
			}

			// Check ResourcePlacement controller enqueued items
			gotRP := reconciler.ResourcePlacementController.(*fakeController).QueueObj
			if !cmp.Equal(gotRP, tt.wantRPEnqueued, sortSlicesOption) {
				t.Errorf("triggerAffectedPlacementsForDeletedRes enqueues keys to ResourcePlacementController, got %v, want %v",
					gotRP, tt.wantRPEnqueued)
			}
		})
	}
}

func TestTriggerAffectedPlacementsForUpdatedRes(t *testing.T) {
	testNamespace := createNamespaceUnstructured(createTestNamespace())

	// Test ResourcePlacement
	testResourcePlacement := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rp",
			Namespace: "test-namespace",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			},
		},
	}

	// Test ClusterResourcePlacement
	testClusterResourcePlacement := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			},
		},
	}

	// Create test data for CRPs and RPs
	crpObjects := func() []runtime.Object {
		uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testClusterResourcePlacement)
		return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
	}()

	rpObjects := func() []runtime.Object {
		uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testResourcePlacement)
		return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
	}()

	tests := map[string]struct {
		key             keys.ClusterWideKey
		resource        *unstructured.Unstructured
		informerManager informer.Manager
		triggerCRP      bool
		wantCRPEnqueued []string
		wantRPEnqueued  []string
	}{
		"cluster-scoped resource triggers ClusterResourcePlacement v1beta1 with CRP data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			resource: testNamespace,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
					utils.ResourcePlacementGVR: {
						Objects: rpObjects,
					},
				},
			},
			triggerCRP:      true,
			wantCRPEnqueued: []string{"test-crp"},
			wantRPEnqueued:  []string{},
		},
		"namespace-scoped resource triggers ResourcePlacement with RP data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			resource: createDeploymentUnstructured(createTestDeployment()),
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
					},
					utils.ResourcePlacementGVR: {
						Objects: rpObjects,
					},
				},
			},
			triggerCRP:      false,
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{"test-namespace/test-rp"},
		},
		"namespace-scoped resource with no matching ResourcePlacements": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment-no-match",
					Namespace: "test-namespace",
				},
			},
			resource: func() *unstructured.Unstructured {
				testDeployment := &appv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deployment-no-match",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "no-match",
						},
					},
				}
				return createDeploymentUnstructured(testDeployment)
			}(),
			informerManager: func() informer.Manager {
				// Create a ResourcePlacement that won't match (different labels)
				nonMatchingRP := &placementv1beta1.ResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-matching-rp",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "different-app",
									},
								},
							},
						},
					},
				}
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(nonMatchingRP)
				rpObjects := []runtime.Object{&unstructured.Unstructured{Object: uMap}}

				return &testinformer.FakeManager{
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterResourcePlacementGVR: {
							Objects: []runtime.Object{},
						},
						utils.ResourcePlacementGVR: {
							Objects: rpObjects,
						},
					},
				}
			}(),
			triggerCRP:      false,
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
		},
		"cluster-scoped resource with empty informer data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "other-namespace",
				},
			},
			resource: func() *unstructured.Unstructured {
				otherResource := &corev1.Namespace{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Namespace",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-namespace",
						Labels: map[string]string{
							"app": "other",
						},
					},
				}
				return createNamespaceUnstructured(otherResource)
			}(),
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
					},
					utils.ResourcePlacementGVR: {
						Objects: []runtime.Object{},
					},
				},
			},
			triggerCRP:      true,
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
		},
		"cluster-scoped resource with multiple CRPs": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			resource: testNamespace,
			informerManager: func() informer.Manager {
				// Create another CRP with same matching labels
				testCRP2 := &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crp-2",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:   "",
								Version: "v1",
								Kind:    "Namespace",
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "test",
									},
								},
							},
						},
					},
				}
				uMap1, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testClusterResourcePlacement)
				uMap2, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP2)
				return &testinformer.FakeManager{
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterResourcePlacementGVR: {
							Objects: []runtime.Object{&unstructured.Unstructured{Object: uMap1}, &unstructured.Unstructured{Object: uMap2}},
						},
						utils.ResourcePlacementGVR: {
							Objects: []runtime.Object{},
						},
					},
				}
			}(),
			triggerCRP:      true,
			wantCRPEnqueued: []string{"test-crp", "test-crp-2"},
			wantRPEnqueued:  []string{},
		},
		// handling a case where the namespace-scoped resource has been deleted
		"namespaced key but with namespace resource triggers ClusterResourcePlacement": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			resource: testNamespace,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
					utils.ResourcePlacementGVR: {
						Objects: rpObjects,
					},
				},
			},
			triggerCRP:      true,
			wantCRPEnqueued: []string{"test-crp"},
			wantRPEnqueued:  []string{},
		},
		"namespace-scoped resource with CRP having namespace-only selector should be skipped": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			resource: testNamespace,
			informerManager: func() informer.Manager {
				// CRP with namespace-only selector should be skipped for namespace-scoped resources
				crpWithNamespaceOnlySelector := &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crp-namespace-only",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:          "",
								Version:        "v1",
								Kind:           "Namespace",
								SelectionScope: placementv1beta1.NamespaceOnly,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "test",
									},
								},
							},
						},
					},
				}
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crpWithNamespaceOnlySelector)
				crpObjects := []runtime.Object{&unstructured.Unstructured{Object: uMap}}

				return &testinformer.FakeManager{
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterResourcePlacementGVR: {
							Objects: crpObjects,
						},
						utils.ResourcePlacementGVR: {
							Objects: []runtime.Object{},
						},
					},
				}
			}(),
			triggerCRP:      true,
			wantCRPEnqueued: []string{}, // Should be empty because namespace-only selector is skipped for namespace-scoped resources
			wantRPEnqueued:  []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reconciler := &Reconciler{
				InformerManager:             tt.informerManager,
				PlacementControllerV1Beta1:  &fakeController{QueueObj: []string{}},
				ResourcePlacementController: &fakeController{QueueObj: []string{}},
			}

			if err := reconciler.triggerAffectedPlacementsForUpdatedRes(tt.key, tt.resource, tt.triggerCRP); err != nil {
				t.Fatalf("triggerAffectedPlacementsForUpdatedClusterRes() = %v, want nil", err)
			}

			// Sort both slices before comparison to handle non-deterministic map iteration order
			gotCRP := reconciler.PlacementControllerV1Beta1.(*fakeController).QueueObj
			if !cmp.Equal(gotCRP, tt.wantCRPEnqueued, sortSlicesOption) {
				t.Errorf("triggerAffectedPlacementsForUpdatedClusterRes enqueues keys to PlacementControllerV1Beta1, got %v, want %v",
					gotCRP, tt.wantCRPEnqueued)
			}

			gotRP := reconciler.ResourcePlacementController.(*fakeController).QueueObj
			if !cmp.Equal(gotRP, tt.wantRPEnqueued, sortSlicesOption) {
				t.Errorf("triggerAffectedPlacementsForUpdatedClusterRes enqueues keys to ResourcePlacementController, got %v, want %v",
					gotRP, tt.wantRPEnqueued)
			}
		})
	}
}

func TestHandleDeletedResource(t *testing.T) {
	// Test data for ClusterResourcePlacement
	testCRP := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp",
		},
		Status: placementv1beta1.PlacementStatus{
			SelectedResources: []placementv1beta1.ResourceIdentifier{
				{
					Group:     "",
					Version:   "v1",
					Kind:      "Namespace",
					Name:      "test-namespace",
					Namespace: "",
				},
				{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
		},
	}

	// Test data for ResourcePlacement
	testRP := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rp",
			Namespace: "test-namespace",
		},
		Status: placementv1beta1.PlacementStatus{
			SelectedResources: []placementv1beta1.ResourceIdentifier{
				{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
		},
	}

	// Create test data for CRPs and RPs
	crpObjects := func() []runtime.Object {
		uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP)
		return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
	}()

	rpObjects := func() []runtime.Object {
		uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP)
		return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
	}()

	// Test namespace for namespace-scoped resource tests
	testNamespace := createNamespaceUnstructured(createTestNamespace())
	tests := map[string]struct {
		key             keys.ClusterWideKey
		isClusterScoped bool
		informerManager informer.Manager
		wantCRPEnqueued []string
		wantRPEnqueued  []string
		wantResult      ctrl.Result
		wantError       bool
	}{
		"cluster-scoped resource deletion triggers ClusterResourcePlacement": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			isClusterScoped: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
				},
			},
			wantCRPEnqueued: []string{"test-crp"},
			wantRPEnqueued:  []string{},
			wantResult:      ctrl.Result{},
			wantError:       false,
		},
		"cluster-scoped resource deletion with no matching placements": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "different-namespace",
				},
			},
			isClusterScoped: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
			wantResult:      ctrl.Result{},
			wantError:       false,
		},
		"namespace-scoped resource deletion triggers both CRP and RP": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScoped: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: crpObjects,
					},
					utils.ResourcePlacementGVR: {
						Objects: rpObjects,
					},
					utils.NamespaceGVR: {
						Objects: []runtime.Object{testNamespace},
					},
				},
			},
			wantCRPEnqueued: []string{"test-crp"},
			wantRPEnqueued:  []string{"test-namespace/test-rp"},
			wantResult:      ctrl.Result{},
			wantError:       false,
		},
		"namespace-scoped resource deletion with CRP error": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScoped: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
						Err:     errors.New("CRP lister error"),
					},
					utils.NamespaceGVR: {
						Objects: []runtime.Object{testNamespace},
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
			wantResult:      ctrl.Result{},
			wantError:       true,
		},
		"namespace-scoped resource deletion with RP error": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScoped: false,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
					},
					utils.ResourcePlacementGVR: {
						Objects: []runtime.Object{},
						Err:     errors.New("RP lister error"),
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
			wantResult:      ctrl.Result{},
			wantError:       true,
		},
		"cluster-scoped resource deletion with lister error": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			isClusterScoped: true,
			informerManager: &testinformer.FakeManager{
				Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
					utils.ClusterResourcePlacementGVR: {
						Objects: []runtime.Object{},
						Err:     errors.New("CRP lister error"),
					},
				},
			},
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{},
			wantResult:      ctrl.Result{},
			wantError:       true,
		},
		"namespace-scoped resource deletion triggers RP as CRP only selects ns": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: placementv1beta1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			isClusterScoped: false,
			informerManager: func() informer.Manager {
				// CRP with namespace-only selector should be skipped for namespace-scoped resources
				crpWithNamespaceOnlySelector := &placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crp-namespace-only",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
							{
								Group:          "",
								Version:        "v1",
								Kind:           "Namespace",
								SelectionScope: placementv1beta1.NamespaceOnly,
							},
						},
					},
				}
				uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crpWithNamespaceOnlySelector)
				crpObjects := []runtime.Object{&unstructured.Unstructured{Object: uMap}}
				return &testinformer.FakeManager{
					Listers: map[schema.GroupVersionResource]*testinformer.FakeLister{
						utils.ClusterResourcePlacementGVR: {
							Objects: crpObjects,
						},
						utils.ResourcePlacementGVR: {
							Objects: rpObjects,
						},
						utils.NamespaceGVR: {
							Objects: []runtime.Object{testNamespace},
						},
					},
				}
			}(),
			wantCRPEnqueued: []string{},
			wantRPEnqueued:  []string{"test-namespace/test-rp"},
			wantResult:      ctrl.Result{},
			wantError:       false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reconciler := &Reconciler{
				InformerManager:             tt.informerManager,
				PlacementControllerV1Beta1:  &fakeController{QueueObj: []string{}},
				ResourcePlacementController: &fakeController{QueueObj: []string{}},
			}

			result, err := reconciler.handleDeletedResource(tt.key, tt.isClusterScoped)

			// Check error expectation
			if gotErr := err != nil; gotErr != tt.wantError {
				t.Fatalf("handleDeletedResource() error = %v, wantError = %v", err, tt.wantError)
				return
			}

			// Check result
			if !cmp.Equal(result, tt.wantResult) {
				t.Errorf("handleDeletedResource() result = %v, want %v", result, tt.wantResult)
			}

			// Check V1Beta1 placement controller enqueued items
			gotCRP := reconciler.PlacementControllerV1Beta1.(*fakeController).QueueObj
			if !cmp.Equal(gotCRP, tt.wantCRPEnqueued, sortSlicesOption) {
				t.Errorf("handleDeletedResource() enqueued CRP = %v, want %v", gotCRP, tt.wantCRPEnqueued)
			}

			// Check ResourcePlacement controller enqueued items
			gotRP := reconciler.ResourcePlacementController.(*fakeController).QueueObj
			if !cmp.Equal(gotRP, tt.wantRPEnqueued, sortSlicesOption) {
				t.Errorf("handleDeletedResource() enqueued RP = %v, want %v", gotRP, tt.wantRPEnqueued)
			}
		})
	}
}

func TestIsSelectNamespaceOnly(t *testing.T) {
	tests := map[string]struct {
		selector placementv1beta1.ResourceSelectorTerm
		want     bool
	}{
		"namespace with namespace only scope": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:          "",
				Version:        "v1",
				Kind:           "Namespace",
				SelectionScope: placementv1beta1.NamespaceOnly,
			},
			want: true,
		},
		"namespace with namespace with resources scope": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:          "",
				Version:        "v1",
				Kind:           "Namespace",
				SelectionScope: placementv1beta1.NamespaceWithResources,
			},
			want: false,
		},
		"configmap with namespace only scope": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:          "",
				Version:        "v1",
				Kind:           "ConfigMap",
				SelectionScope: placementv1beta1.NamespaceOnly,
			},
			want: false,
		},
		"deployment with namespace only scope": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:          "apps",
				Version:        "v1",
				Kind:           "Deployment",
				SelectionScope: placementv1beta1.NamespaceOnly,
			},
			want: false,
		},
		"namespace with wrong group": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:          "core",
				Version:        "v1",
				Kind:           "Namespace",
				SelectionScope: placementv1beta1.NamespaceOnly,
			},
			want: false,
		},
		"namespace with wrong version": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:          "",
				Version:        "v2",
				Kind:           "Namespace",
				SelectionScope: placementv1beta1.NamespaceOnly,
			},
			want: false,
		},
		"namespace with default selection scope (NamespaceWithResources)": {
			selector: placementv1beta1.ResourceSelectorTerm{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := isSelectNamespaceOnly(tt.selector)
			if got != tt.want {
				t.Errorf("isSelectNamespaceOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}
