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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/informer"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/keys"
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

// fakeLister is a simple fake lister for testing
type fakeLister struct {
	objects []runtime.Object
}

func (f *fakeLister) List(_ labels.Selector) ([]runtime.Object, error) {
	return f.objects, nil
}

func (f *fakeLister) Get(name string) (runtime.Object, error) {
	for _, obj := range f.objects {
		if obj.(*unstructured.Unstructured).GetName() == name {
			return obj, nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "test"}, name)
}

func (f *fakeLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return &fakeNamespaceLister{objects: f.objects, namespace: namespace}
}

// fakeNamespaceLister implements cache.GenericNamespaceLister
type fakeNamespaceLister struct {
	objects   []runtime.Object
	namespace string
}

func (f *fakeNamespaceLister) List(_ labels.Selector) ([]runtime.Object, error) {
	return f.objects, nil
}

func (f *fakeNamespaceLister) Get(name string) (runtime.Object, error) {
	for _, obj := range f.objects {
		if obj.(*unstructured.Unstructured).GetName() == name {
			return obj, nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "test"}, name)
}

// fakeInformerManager is a test-specific informer manager
type fakeInformerManager struct {
	listers map[schema.GroupVersionResource]*fakeLister
}

func (f *fakeInformerManager) AddDynamicResources(_ []informer.APIResourceMeta, _ cache.ResourceEventHandler, _ bool) {
}

func (f *fakeInformerManager) AddStaticResource(_ informer.APIResourceMeta, _ cache.ResourceEventHandler) {
}

func (f *fakeInformerManager) IsInformerSynced(_ schema.GroupVersionResource) bool {
	return true
}

func (f *fakeInformerManager) Start() {
}

func (f *fakeInformerManager) Stop() {
}

func (f *fakeInformerManager) Lister(gvr schema.GroupVersionResource) cache.GenericLister {
	if lister, exists := f.listers[gvr]; exists {
		return lister
	}
	return &fakeLister{objects: []runtime.Object{}}
}

func (f *fakeInformerManager) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	return nil
}

func (f *fakeInformerManager) IsClusterScopedResources(_ schema.GroupVersionKind) bool {
	return true
}

func (f *fakeInformerManager) WaitForCacheSync() {
}

func (f *fakeInformerManager) GetClient() dynamic.Interface {
	return nil
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
					Status: placementv1beta1.PlacementStatus{
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
			wantCrp: []string{"resource-selected", "resource-selected-2"},
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
			wantCrp: nil,
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
				"region":  rand.String(10),
				"version": rand.String(4),
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
			got := collectAllAffectedPlacementsV1Alpha1(&unstructured.Unstructured{Object: uRes}, crpList)
			if !reflect.DeepEqual(got, tt.wantCrp) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCrp)
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
			Name: "test-nameSpace",
			Labels: map[string]string{
				"region":  rand.String(10),
				"version": rand.String(4),
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"does not match a place with a miss matching label and was not selected": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
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
			wantCrp: make(map[string]bool),
		},
		"don't select placement with name, nil label selector for namespace with different name": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
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
					Spec: placementv1beta1.PlacementSpec{
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
		"does not match placement with different GVK selector": {
			res: matchRes,
			crpList: []*placementv1beta1.ClusterResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "resource-not-selected",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			clusterPlacements := convertToClusterResourcePlacements(crpList)
			got := collectAllAffectedPlacementsV1Beta1(&unstructured.Unstructured{Object: uRes}, clusterPlacements)
			if !reflect.DeepEqual(got, tt.wantCrp) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCrp)
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

	tests := map[string]struct {
		res     *unstructured.Unstructured
		rpList  []*placementv1beta1.ResourcePlacement
		wantCrp map[string]bool
	}{
		"match ResourcePlacement with the matching label": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"does not match ResourcePlacement with no selector": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{},
					},
				},
			},
			wantCrp: make(map[string]bool),
		},
		"match ResourcePlacement with the name selector": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"does not match ResourcePlacement with different name": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantCrp: make(map[string]bool),
		},
		"match ResourcePlacement with previously selected resource": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"select ResourcePlacement with empty name, nil label selector for deployment": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
							{
								Group:   "apps",
								Version: "v1",
								Kind:    "Deployment",
							},
						},
					},
				},
			},
			wantCrp: map[string]bool{"resource-selected": true},
		},
		"does not match ResourcePlacement with different GVK selector": {
			res: matchRes,
			rpList: []*placementv1beta1.ResourcePlacement{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resource-not-selected",
						Namespace: "test-namespace",
					},
					Spec: placementv1beta1.PlacementSpec{
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			wantCrp: make(map[string]bool),
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
			got := collectAllAffectedPlacementsV1Beta1(tt.res, resourcePlacements)
			if !reflect.DeepEqual(got, tt.wantCrp) {
				t.Errorf("test case `%s` got = %v, wantResult %v", name, got, tt.wantCrp)
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
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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

	defaultFakeInformerManager := &fakeInformerManager{
		listers: map[schema.GroupVersionResource]*fakeLister{
			{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
				objects: func() []runtime.Object {
					uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP)
					return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
				}(),
			},
			{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
				objects: func() []runtime.Object {
					uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testRP)
					return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
				}(),
			},
			{Group: "", Version: "v1", Resource: "namespaces"}: {
				objects: func() []runtime.Object {
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
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
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
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
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
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
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
			wantResourcePlacementControllerEnqueued: []string{"test-rp"},
			wantResult:                              ctrl.Result{},
			wantError:                               false,
		},
		"namespace-scoped resource with missing parent namespace": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "missing-namespace",
				},
			},
			clusterObj:      testDeployment,
			isClusterScoped: false,
			informerManager: &fakeInformerManager{
				listers: map[schema.GroupVersionResource]*fakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						objects: []runtime.Object{},
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						objects: []runtime.Object{},
					},
					{Group: "", Version: "v1", Resource: "namespaces"}: {
						objects: []runtime.Object{},
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
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			clusterObj:      testDeployment,
			isClusterScoped: false,
			informerManager: &fakeInformerManager{
				listers: map[schema.GroupVersionResource]*fakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						objects: func() []runtime.Object {
							uMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(testCRP)
							return []runtime.Object{&unstructured.Unstructured{Object: uMap}}
						}(),
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						objects: []runtime.Object{},
					},
					{Group: "", Version: "v1", Resource: "namespaces"}: {
						objects: func() []runtime.Object {
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

func TestTriggerAffectedPlacementsForUpdatedClusterRes(t *testing.T) {
	// Test resource - a namespace
	testNamespace := createNamespaceUnstructured(createTestNamespace())

	// Test ResourcePlacement
	testResourcePlacement := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rp",
			Namespace: "test-namespace",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
			ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
		key                                     keys.ClusterWideKey
		resource                                *unstructured.Unstructured
		informerManager                         informer.Manager
		wantPlacementControllerEnqueued         []string
		wantResourcePlacementControllerEnqueued []string
	}{
		"cluster-scoped resource triggers ClusterResourcePlacement v1beta1 with CRP data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "test-namespace",
				},
			},
			resource: testNamespace,
			informerManager: &fakeInformerManager{
				listers: map[schema.GroupVersionResource]*fakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						objects: crpObjects,
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						objects: []runtime.Object{},
					},
				},
			},
			wantPlacementControllerEnqueued:         []string{"test-crp"},
			wantResourcePlacementControllerEnqueued: []string{},
		},
		"namespace-scoped resource triggers ResourcePlacement with RP data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
					Group:     "apps",
					Version:   "v1",
					Kind:      "Deployment",
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
			},
			resource: createDeploymentUnstructured(createTestDeployment()),
			informerManager: &fakeInformerManager{
				listers: map[schema.GroupVersionResource]*fakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						objects: []runtime.Object{},
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						objects: rpObjects,
					},
				},
			},
			wantPlacementControllerEnqueued:         []string{},
			wantResourcePlacementControllerEnqueued: []string{"test-rp"},
		},
		"namespace-scoped resource with no matching ResourcePlacements": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
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
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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

				return &fakeInformerManager{
					listers: map[schema.GroupVersionResource]*fakeLister{
						{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
							objects: []runtime.Object{},
						},
						{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
							objects: rpObjects,
						},
					},
				}
			}(),
			wantPlacementControllerEnqueued:         []string{},
			wantResourcePlacementControllerEnqueued: []string{},
		},
		"cluster-scoped resource with empty informer data": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
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
			informerManager: &fakeInformerManager{
				listers: map[schema.GroupVersionResource]*fakeLister{
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
						objects: []runtime.Object{},
					},
					{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
						objects: []runtime.Object{},
					},
				},
			},
			wantPlacementControllerEnqueued:         []string{},
			wantResourcePlacementControllerEnqueued: []string{},
		},
		"cluster-scoped resource with multiple CRPs": {
			key: keys.ClusterWideKey{
				ResourceIdentifier: fleetv1alpha1.ResourceIdentifier{
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
						ResourceSelectors: []placementv1beta1.ClusterResourceSelector{
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
				return &fakeInformerManager{
					listers: map[schema.GroupVersionResource]*fakeLister{
						{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "clusterresourceplacements"}: {
							objects: []runtime.Object{&unstructured.Unstructured{Object: uMap1}, &unstructured.Unstructured{Object: uMap2}},
						},
						{Group: "placement.kubernetes-fleet.io", Version: "v1beta1", Resource: "resourceplacements"}: {
							objects: []runtime.Object{},
						},
					},
				}
			}(),
			wantPlacementControllerEnqueued:         []string{"test-crp", "test-crp-2"},
			wantResourcePlacementControllerEnqueued: []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reconciler := &Reconciler{
				InformerManager:             tt.informerManager,
				PlacementControllerV1Beta1:  &fakeController{QueueObj: []string{}},
				ResourcePlacementController: &fakeController{QueueObj: []string{}},
			}

			result, err := reconciler.triggerAffectedPlacementsForUpdatedClusterRes(tt.key, tt.resource, tt.key.Namespace == "")
			if err != nil || result.Requeue || result.RequeueAfter > 0 {
				t.Fatalf("triggerAffectedPlacementsForUpdatedClusterRes = %v, %v, want empty result and nil err", result, err)
			}

			// Sort both slices before comparison to handle non-deterministic map iteration order
			gotPlacement := reconciler.PlacementControllerV1Beta1.(*fakeController).QueueObj
			if !cmp.Equal(gotPlacement, tt.wantPlacementControllerEnqueued, sortSlicesOption) {
				t.Errorf("triggerAffectedPlacementsForUpdatedClusterRes enqueues keys to PlacementControllerV1Beta1, got %v, want %v",
					gotPlacement, tt.wantPlacementControllerEnqueued)
			}

			gotResourcePlacement := reconciler.ResourcePlacementController.(*fakeController).QueueObj
			if !cmp.Equal(gotResourcePlacement, tt.wantResourcePlacementControllerEnqueued, sortSlicesOption) {
				t.Errorf("triggerAffectedPlacementsForUpdatedClusterRes enqueues keys to ResourcePlacementController, got %v, want %v",
					gotResourcePlacement, tt.wantResourcePlacementControllerEnqueued)
			}
		})
	}
}
