/*
Copyright 2026 The KubeFleet Authors.

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

package authorization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/resourceeligibility"
)

const (
	crpNamespaceA = "team-a"
	crpNamespaceB = "team-b"
	crpDeletingNS = "deleting"
	crpMissingNS  = "missing"
	crpSource     = "source"
	crpLabel      = "env"
	crpLabelValue = "production"
	crpRoleKind   = "ClusterRole"
	crpConfigMaps = "configmaps"
	crpUnknown    = "Unknown"
	crpGet        = "get"
	crpList       = "list"
	crpSecretKind = "Secret"
	crpSecrets    = "secrets"
)

type crpResourceInventory []schema.GroupVersionResource

func (i crpResourceInventory) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	return i
}

type crpCountingResourceInventory struct {
	NamespacedResourceInventory
	calls int
}

func (i *crpCountingResourceInventory) GetNameSpaceScopedResources() []schema.GroupVersionResource {
	i.calls++
	return i.NamespacedResourceInventory.GetNameSpaceScopedResources()
}

type crpCountingEligibilityChecker struct {
	resourceeligibility.Checker
	calls []schema.GroupVersionResource
}

func (c *crpCountingEligibilityChecker) AllowsGVR(gvr schema.GroupVersionResource) (bool, error) {
	c.calls = append(c.calls, gvr)
	return c.Checker.AllowsGVR(gvr)
}

type crpErrorRESTMapper struct {
	meta.RESTMapper
	err error
}

func (m crpErrorRESTMapper) KindsFor(schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, m.err
}

func newClusterPlacementTestAuthorizer(t *testing.T, sarClient SubjectAccessReviewClient) (*PlacementAuthorizer, client.Client) {
	t.Helper()
	mapper := resourcePlacementTestMapper()
	mapper.Add(rbacv1.SchemeGroupVersion.WithKind(crpRoleKind), meta.RESTScopeRoot)
	mapper.Add(corev1.SchemeGroupVersion.WithKind(crpSecretKind), meta.RESTScopeNamespace)
	now := metav1.Now()
	reader := fake.NewClientBuilder().WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: crpNamespaceA, Labels: map[string]string{crpLabel: crpLabelValue}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: crpNamespaceB}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: crpDeletingNS, Labels: map[string]string{crpLabel: crpLabelValue}, DeletionTimestamp: &now, Finalizers: []string{"cleanup"}}},
	).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			err := fmt.Errorf("unexpected source object read: %T", obj)
			t.Error(err)
			return err
		},
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.NamespaceList); !ok {
				err := fmt.Errorf("unexpected source collection read: %T", list)
				t.Error(err)
				return err
			}
			return c.List(ctx, list, opts...)
		},
	}).Build()
	reviewer, err := NewReviewer(sarClient, mapper, 1)
	if err != nil {
		t.Fatal(err)
	}
	inventory := crpResourceInventory{corev1.SchemeGroupVersion.WithResource(crpConfigMaps), corev1.SchemeGroupVersion.WithResource(crpSecrets)}
	a, err := NewPlacementAuthorizer(mapper, reviewer, resourceeligibility.AllowAll(mapper), reader, inventory)
	if err != nil {
		t.Fatal(err)
	}
	return a, reader
}

func TestClusterResourcePlacementAuthorizeSelections(t *testing.T) {
	role := placementv1beta1.ResourceSelectorTerm{Group: rbacv1.GroupName, Version: "v1", Kind: crpRoleKind, Name: crpSource}
	allRoles := placementv1beta1.ResourceSelectorTerm{Group: rbacv1.GroupName, Version: "v1", Kind: crpRoleKind}
	labeledRoles := placementv1beta1.ResourceSelectorTerm{Group: rbacv1.GroupName, Version: "v1", Kind: crpRoleKind, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}}}
	nsOnly := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceOnly}
	allNSOnly := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceOnly}
	labeledNSOnly := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceOnly, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}}}
	withResources := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResources}
	defaultScope := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA}
	allNamespaces := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources}
	labeledNamespaces := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}}}
	selective := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors}
	configMap := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.ConfigMapKind, Name: crpSource}
	allConfigMaps := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.ConfigMapKind}
	labeledConfigMaps := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.ConfigMapKind, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}}}
	deletingNamespace := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpDeletingNS, SelectionScope: placementv1beta1.NamespaceWithResources}
	missingNamespace := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpMissingNS, SelectionScope: placementv1beta1.NamespaceWithResources}
	missingSelective := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpMissingNS, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors}
	deletingSelective := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpDeletingNS, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors}
	getNS := resourceAttributes(utils.NamespaceGVR, crpGet, "", crpNamespaceA)
	listNS := resourceAttributes(utils.NamespaceGVR, crpList, "", "")
	getRole := resourceAttributes(rbacv1.SchemeGroupVersion.WithResource("clusterroles"), crpGet, "", crpSource)
	listRoles := resourceAttributes(rbacv1.SchemeGroupVersion.WithResource("clusterroles"), crpList, "", "")
	getCM := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpGet, crpNamespaceA, crpSource)
	listCMA := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpList, crpNamespaceA, "")
	listCMB := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpList, crpNamespaceB, "")
	listSecretA := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpSecrets), crpList, crpNamespaceA, "")
	listSecretB := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpSecrets), crpList, crpNamespaceB, "")
	getDeletingNS := resourceAttributes(utils.NamespaceGVR, crpGet, "", crpDeletingNS)
	listCMDeleting := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpList, crpDeletingNS, "")
	listSecretDeleting := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpSecrets), crpList, crpDeletingNS, "")
	getMissingNS := resourceAttributes(utils.NamespaceGVR, crpGet, "", crpMissingNS)
	listCMMissing := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpList, crpMissingNS, "")
	listSecretMissing := resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpSecrets), crpList, crpMissingNS, "")
	tests := []struct {
		name              string
		selectors         []placementv1beta1.ResourceSelectorTerm
		rp                bool
		exclude           []schema.GroupVersionKind
		excludeNamespaces []string
		inventory         NamespacedResourceInventory
		want              []authorizationv1.ResourceAttributes
	}{
		{name: "named cluster resource", selectors: []placementv1beta1.ResourceSelectorTerm{role}, want: []authorizationv1.ResourceAttributes{getRole}},
		{name: "labeled cluster resources", selectors: []placementv1beta1.ResourceSelectorTerm{labeledRoles}, want: []authorizationv1.ResourceAttributes{listRoles}},
		{name: "all cluster resources of kind", selectors: []placementv1beta1.ResourceSelectorTerm{allRoles}, want: []authorizationv1.ResourceAttributes{listRoles}},
		{name: "namespace only by name", selectors: []placementv1beta1.ResourceSelectorTerm{nsOnly}, want: []authorizationv1.ResourceAttributes{getNS}},
		{name: "namespace only does not expand an excluded namespace", selectors: []placementv1beta1.ResourceSelectorTerm{nsOnly}, excludeNamespaces: []string{crpNamespaceA}, want: []authorizationv1.ResourceAttributes{getNS}},
		{name: "namespace only by label", selectors: []placementv1beta1.ResourceSelectorTerm{labeledNSOnly}, want: []authorizationv1.ResourceAttributes{listNS}},
		{name: "namespace only all", selectors: []placementv1beta1.ResourceSelectorTerm{allNSOnly}, want: []authorizationv1.ResourceAttributes{listNS}},
		{name: "namespace expansion", selectors: []placementv1beta1.ResourceSelectorTerm{withResources}, want: []authorizationv1.ResourceAttributes{getNS, listCMA, listSecretA}},
		{name: "default namespace scope", selectors: []placementv1beta1.ResourceSelectorTerm{defaultScope}, want: []authorizationv1.ResourceAttributes{getNS, listCMA, listSecretA}},
		{name: "labeled namespace expansion includes deleting", selectors: []placementv1beta1.ResourceSelectorTerm{labeledNamespaces}, want: []authorizationv1.ResourceAttributes{listNS, listCMA, listSecretA, listCMDeleting, listSecretDeleting}},
		{name: "all namespaces expand separately including deleting", selectors: []placementv1beta1.ResourceSelectorTerm{allNamespaces}, want: []authorizationv1.ResourceAttributes{listNS, listCMA, listSecretA, listCMB, listSecretB, listCMDeleting, listSecretDeleting}},
		{name: "selective named resource", selectors: []placementv1beta1.ResourceSelectorTerm{selective, configMap}, want: []authorizationv1.ResourceAttributes{getNS, getCM}},
		{name: "selective namespace without additional resources", selectors: []placementv1beta1.ResourceSelectorTerm{selective}, want: []authorizationv1.ResourceAttributes{getNS}},
		{name: "selective labeled resources", selectors: []placementv1beta1.ResourceSelectorTerm{selective, labeledConfigMaps}, want: []authorizationv1.ResourceAttributes{getNS, listCMA}},
		{name: "selective all resources of kind", selectors: []placementv1beta1.ResourceSelectorTerm{selective, allConfigMaps}, want: []authorizationv1.ResourceAttributes{getNS, listCMA}},
		{name: "namespace selector after mixed resources", selectors: []placementv1beta1.ResourceSelectorTerm{role, configMap, selective}, want: []authorizationv1.ResourceAttributes{getRole, getCM, getNS}},
		{name: "namespace expansion and cluster resource", selectors: []placementv1beta1.ResourceSelectorTerm{role, withResources}, want: []authorizationv1.ResourceAttributes{getRole, getNS, listCMA, listSecretA}},
		{name: "overlapping namespace selectors", selectors: []placementv1beta1.ResourceSelectorTerm{nsOnly, labeledNSOnly, nsOnly}, want: []authorizationv1.ResourceAttributes{getNS, listNS}},
		{name: "overlapping namespace expansion selectors", selectors: []placementv1beta1.ResourceSelectorTerm{withResources, withResources}, want: []authorizationv1.ResourceAttributes{getNS, listCMA, listSecretA}},
		{name: "overlapping cluster selectors", selectors: []placementv1beta1.ResourceSelectorTerm{role, allRoles, role}, want: []authorizationv1.ResourceAttributes{getRole, listRoles}},
		{name: "deleting namespace expansion", selectors: []placementv1beta1.ResourceSelectorTerm{deletingNamespace}, want: []authorizationv1.ResourceAttributes{getDeletingNS, listCMDeleting, listSecretDeleting}},
		{name: "missing namespace expansion", selectors: []placementv1beta1.ResourceSelectorTerm{missingNamespace}, want: []authorizationv1.ResourceAttributes{getMissingNS, listCMMissing, listSecretMissing}},
		{name: "missing selective namespace", selectors: []placementv1beta1.ResourceSelectorTerm{missingSelective, configMap}, want: []authorizationv1.ResourceAttributes{getMissingNS, resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpGet, crpMissingNS, crpSource)}},
		{name: "deleting selective namespace", selectors: []placementv1beta1.ResourceSelectorTerm{deletingSelective, configMap}, want: []authorizationv1.ResourceAttributes{getDeletingNS, resourceAttributes(corev1.SchemeGroupVersion.WithResource(crpConfigMaps), crpGet, crpDeletingNS, crpSource)}},
		{name: "no namespace matches still requires list", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: "absent"}}}}, want: []authorizationv1.ResourceAttributes{listNS}},
		{name: "ineligible cluster kind skipped", selectors: []placementv1beta1.ResourceSelectorTerm{role}, exclude: []schema.GroupVersionKind{rbacv1.SchemeGroupVersion.WithKind(crpRoleKind)}},
		{name: "ineligible Namespace kind skipped", selectors: []placementv1beta1.ResourceSelectorTerm{nsOnly}, exclude: []schema.GroupVersionKind{utils.NamespaceGVK}},
		{name: "ineligible expanded type skipped", selectors: []placementv1beta1.ResourceSelectorTerm{withResources}, exclude: []schema.GroupVersionKind{corev1.SchemeGroupVersion.WithKind(crpSecretKind)}, want: []authorizationv1.ResourceAttributes{getNS, listCMA}},
		{name: "duplicate inventory entries deduplicate reviews", selectors: []placementv1beta1.ResourceSelectorTerm{withResources}, inventory: crpResourceInventory{corev1.SchemeGroupVersion.WithResource(crpConfigMaps), corev1.SchemeGroupVersion.WithResource(crpConfigMaps)}, want: []authorizationv1.ResourceAttributes{getNS, listCMA}},
		{name: "removed inventory type skipped without dropping valid types", selectors: []placementv1beta1.ResourceSelectorTerm{withResources}, inventory: crpResourceInventory{
			corev1.SchemeGroupVersion.WithResource(crpConfigMaps),
			{Group: "removed.example.io", Version: "v1", Resource: "widgets"},
			corev1.SchemeGroupVersion.WithResource(crpSecrets),
		}, want: []authorizationv1.ResourceAttributes{getNS, listCMA, listSecretA}},
		{name: "RP still supported by configured authorizer", rp: true, selectors: []placementv1beta1.ResourceSelectorTerm{configMap}, want: []authorizationv1.ResourceAttributes{getCM}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []authorizationv1.ResourceAttributes
			a, _ := newClusterPlacementTestAuthorizer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				if review.Spec.User != rpRequester {
					t.Errorf("Authorize() review user = %q, want %q", review.Spec.User, rpRequester)
				}
				got = append(got, *review.Spec.ResourceAttributes)
				review.Status.Allowed = true
				return nil
			}})
			if len(tt.exclude) != 0 {
				if err := a.resourceEligibilityChecker.SetGVKCheckList(resourceeligibility.GVKEligibilityModeDenyList, tt.exclude); err != nil {
					t.Fatal(err)
				}
			}
			if tt.inventory != nil {
				a.namespacedResources = tt.inventory
			}
			if len(tt.excludeNamespaces) != 0 {
				a.resourceEligibilityChecker.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, tt.excludeNamespaces, nil)
			}
			var placement placementv1beta1.PlacementObj = &placementv1beta1.ClusterResourcePlacement{Spec: placementv1beta1.PlacementSpec{ResourceSelectors: tt.selectors}}
			if tt.rp {
				placement = &placementv1beta1.ResourcePlacement{ObjectMeta: metav1.ObjectMeta{Namespace: crpNamespaceA}, Spec: placementv1beta1.PlacementSpec{ResourceSelectors: tt.selectors}}
			}

			result := a.Authorize(context.Background(), authenticationv1.UserInfo{Username: rpRequester}, placement)
			if diff := cmp.Diff(Result{Decision: DecisionAllowed}, result); diff != "" {
				t.Fatalf("Authorize() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.SortSlices(func(a, b authorizationv1.ResourceAttributes) bool { return fmt.Sprint(a) < fmt.Sprint(b) })); diff != "" {
				t.Errorf("Authorize() checks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClusterResourcePlacementResolvesInventoryOncePerRequest(t *testing.T) {
	all := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources}
	named := all
	named.Name = crpNamespaceA
	otherNamed := all
	otherNamed.Name = crpNamespaceB
	labeled := all
	labeled.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}}
	unmatched := all
	unmatched.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: "absent"}}
	namespaceOnly := named
	namespaceOnly.SelectionScope = placementv1beta1.NamespaceOnly
	selective := named
	selective.SelectionScope = placementv1beta1.NamespaceWithResourceSelectors
	tests := []struct {
		name      string
		selectors []placementv1beta1.ResourceSelectorTerm
		expand    bool
	}{
		{name: "all namespaces", selectors: []placementv1beta1.ResourceSelectorTerm{all}, expand: true},
		{name: "labeled namespaces", selectors: []placementv1beta1.ResourceSelectorTerm{labeled}, expand: true},
		{name: "multiple named selectors", selectors: []placementv1beta1.ResourceSelectorTerm{named, otherNamed}, expand: true},
		{name: "overlapping selectors", selectors: []placementv1beta1.ResourceSelectorTerm{all, named}, expand: true},
		{name: "no matches before named selector", selectors: []placementv1beta1.ResourceSelectorTerm{unmatched, named}, expand: true},
		{name: "no matches", selectors: []placementv1beta1.ResourceSelectorTerm{unmatched}},
		{name: "namespace only", selectors: []placementv1beta1.ResourceSelectorTerm{namespaceOnly}},
		{name: "selective resources", selectors: []placementv1beta1.ResourceSelectorTerm{selective, {Version: "v1", Kind: utils.ConfigMapKind, Name: crpSource}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := newClusterPlacementTestAuthorizer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status.Allowed = true
				return nil
			}})
			inventory := &crpCountingResourceInventory{NamespacedResourceInventory: a.namespacedResources}
			checker := &crpCountingEligibilityChecker{Checker: a.resourceEligibilityChecker}
			a.namespacedResources = inventory
			a.resourceEligibilityChecker = checker
			p := &placementv1beta1.ClusterResourcePlacement{Spec: placementv1beta1.PlacementSpec{ResourceSelectors: tt.selectors}}
			for request := 0; request < 2; request++ {
				inventory.calls = 0
				checker.calls = nil
				result := a.Authorize(context.Background(), authenticationv1.UserInfo{Username: rpRequester}, p)
				if diff := cmp.Diff(Result{Decision: DecisionAllowed}, result); diff != "" {
					t.Fatalf("Authorize() request %d mismatch (-want +got):\n%s", request, diff)
				}
				wantInventoryCalls := 0
				var wantEligibilityCalls []schema.GroupVersionResource
				if tt.expand {
					wantInventoryCalls = 1
					wantEligibilityCalls = []schema.GroupVersionResource{
						corev1.SchemeGroupVersion.WithResource(crpConfigMaps),
						corev1.SchemeGroupVersion.WithResource(crpSecrets),
					}
				}
				if inventory.calls != wantInventoryCalls {
					t.Errorf("Authorize() request %d inventory calls = %d, want %d", request, inventory.calls, wantInventoryCalls)
				}
				if diff := cmp.Diff(wantEligibilityCalls, checker.calls); diff != "" {
					t.Errorf("Authorize() request %d eligibility calls mismatch (-want +got):\n%s", request, diff)
				}
			}
		})
	}
}

func TestClusterResourcePlacementNamespaceOnlyDoesNotReadNamespaces(t *testing.T) {
	tests := []struct {
		name     string
		selector placementv1beta1.ResourceSelectorTerm
		want     authorizationv1.ResourceAttributes
	}{
		{
			name:     "named",
			selector: placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceOnly},
			want:     resourceAttributes(utils.NamespaceGVR, crpGet, "", crpNamespaceA),
		},
		{
			name:     "named missing namespace",
			selector: placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpMissingNS, SelectionScope: placementv1beta1.NamespaceOnly},
			want:     resourceAttributes(utils.NamespaceGVR, crpGet, "", crpMissingNS),
		},
		{
			name:     "labeled",
			selector: placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceOnly, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}}},
			want:     resourceAttributes(utils.NamespaceGVR, crpList, "", ""),
		},
		{
			name:     "all",
			selector: placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceOnly},
			want:     resourceAttributes(utils.NamespaceGVR, crpList, "", ""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []authorizationv1.ResourceAttributes
			a, _ := newClusterPlacementTestAuthorizer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				got = append(got, *review.Spec.ResourceAttributes)
				review.Status.Allowed = true
				return nil
			}})
			readerCalls := 0
			readErr := errors.New("NamespaceOnly must not read namespaces")
			a.namespaceReader = fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					readerCalls++
					return readErr
				},
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					readerCalls++
					return readErr
				},
			}).Build()
			p := &placementv1beta1.ClusterResourcePlacement{Spec: placementv1beta1.PlacementSpec{ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{tt.selector}}}
			result := a.Authorize(context.Background(), authenticationv1.UserInfo{Username: rpRequester}, p)
			if diff := cmp.Diff(Result{Decision: DecisionAllowed}, result); diff != "" {
				t.Errorf("Authorize() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]authorizationv1.ResourceAttributes{tt.want}, got); diff != "" {
				t.Errorf("Authorize() checks mismatch (-want +got):\n%s", diff)
			}
			if readerCalls != 0 {
				t.Errorf("Authorize() namespace reader calls = %d, want 0", readerCalls)
			}
		})
	}
}

func TestClusterResourcePlacementNamespacePreflightStopsResolution(t *testing.T) {
	const denialReason = "namespace access denied"
	namedExpansion := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResources}
	missingExpansion := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpMissingNS, SelectionScope: placementv1beta1.NamespaceWithResources}
	labeledExpansion := placementv1beta1.ResourceSelectorTerm{
		Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources,
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{crpLabel: crpLabelValue}},
	}
	allExpansion := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources}
	selectiveNamespace := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors}
	apiErr := errors.New("namespace authorization unavailable")
	tests := []struct {
		name         string
		selector     placementv1beta1.ResourceSelectorTerm
		reviewStatus authorizationv1.SubjectAccessReviewStatus
		reviewErr    error
		wantDecision Decision
	}{
		{name: "named expansion denied", selector: namedExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: denialReason}, wantDecision: DecisionDenied},
		{name: "named expansion no opinion", selector: namedExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{}, wantDecision: DecisionDenied},
		{name: "named expansion API error", selector: namedExpansion, reviewErr: apiErr, wantDecision: DecisionIndeterminate},
		{name: "named expansion evaluation error", selector: namedExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{EvaluationError: apiErr.Error()}, wantDecision: DecisionIndeterminate},
		{name: "missing named expansion denied", selector: missingExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: denialReason}, wantDecision: DecisionDenied},
		{name: "missing named expansion no opinion", selector: missingExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{}, wantDecision: DecisionDenied},
		{name: "missing named expansion API error", selector: missingExpansion, reviewErr: apiErr, wantDecision: DecisionIndeterminate},
		{name: "missing named expansion evaluation error", selector: missingExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{EvaluationError: apiErr.Error()}, wantDecision: DecisionIndeterminate},
		{name: "labeled expansion denied", selector: labeledExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: denialReason}, wantDecision: DecisionDenied},
		{name: "labeled expansion no opinion", selector: labeledExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{}, wantDecision: DecisionDenied},
		{name: "labeled expansion API error", selector: labeledExpansion, reviewErr: apiErr, wantDecision: DecisionIndeterminate},
		{name: "labeled expansion evaluation error", selector: labeledExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{EvaluationError: apiErr.Error()}, wantDecision: DecisionIndeterminate},
		{name: "all namespaces expansion denied", selector: allExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: denialReason}, wantDecision: DecisionDenied},
		{name: "all namespaces expansion no opinion", selector: allExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{}, wantDecision: DecisionDenied},
		{name: "all namespaces expansion API error", selector: allExpansion, reviewErr: apiErr, wantDecision: DecisionIndeterminate},
		{name: "all namespaces expansion evaluation error", selector: allExpansion, reviewStatus: authorizationv1.SubjectAccessReviewStatus{EvaluationError: apiErr.Error()}, wantDecision: DecisionIndeterminate},
		{name: "selective namespace denied", selector: selectiveNamespace, reviewStatus: authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: denialReason}, wantDecision: DecisionDenied},
		{name: "selective namespace no opinion", selector: selectiveNamespace, reviewStatus: authorizationv1.SubjectAccessReviewStatus{}, wantDecision: DecisionDenied},
		{name: "selective namespace API error", selector: selectiveNamespace, reviewErr: apiErr, wantDecision: DecisionIndeterminate},
		{name: "selective namespace evaluation error", selector: selectiveNamespace, reviewStatus: authorizationv1.SubjectAccessReviewStatus{EvaluationError: apiErr.Error()}, wantDecision: DecisionIndeterminate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reviews []authorizationv1.ResourceAttributes
			a, _ := newClusterPlacementTestAuthorizer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				if review.Spec.User != rpRequester {
					t.Errorf("Authorize() review user = %q, want %q", review.Spec.User, rpRequester)
				}
				reviews = append(reviews, *review.Spec.ResourceAttributes)
				review.Status = tt.reviewStatus
				return tt.reviewErr
			}})
			readerCalls := 0
			readErr := errors.New("unexpected namespace read before authorization")
			a.namespaceReader = fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					readerCalls++
					return readErr
				},
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					readerCalls++
					return readErr
				},
			}).Build()
			p := &placementv1beta1.ClusterResourcePlacement{Spec: placementv1beta1.PlacementSpec{ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{tt.selector}}}
			result := a.Authorize(context.Background(), authenticationv1.UserInfo{Username: rpRequester}, p)
			if result.Decision != tt.wantDecision {
				t.Errorf("Authorize() decision = %s, want %s", result.Decision, tt.wantDecision)
			}
			if tt.reviewErr != nil && !errors.Is(result.Err, tt.reviewErr) {
				t.Errorf("Authorize() error = %v, want error containing %v", result.Err, tt.reviewErr)
			}
			verb := crpList
			if tt.selector.Name != "" {
				verb = crpGet
			}
			wantReviews := []authorizationv1.ResourceAttributes{resourceAttributes(utils.NamespaceGVR, verb, "", tt.selector.Name)}
			if diff := cmp.Diff(wantReviews, reviews); diff != "" {
				t.Errorf("Authorize() reviews mismatch (-want +got):\n%s", diff)
			}
			if readerCalls != 0 {
				t.Errorf("Authorize() namespace reader calls = %d, want 0", readerCalls)
			}
			if tt.selector.Name == "" {
				message := result.DenialReason
				if result.Err != nil {
					message += result.Err.Error()
				}
				for _, namespace := range []string{crpNamespaceA, crpNamespaceB, crpDeletingNS} {
					if strings.Contains(message, namespace) {
						t.Errorf("Authorize() response %q disclosed namespace %q", message, namespace)
					}
				}
			}
		})
	}
}

func TestClusterResourcePlacementAuthorizeReadsNamespacesFresh(t *testing.T) {
	var got []string
	user := authenticationv1.UserInfo{Username: "creator"}
	a, reader := newClusterPlacementTestAuthorizer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
		if review.Spec.User != user.Username {
			t.Errorf("Authorize() review user = %q, want %q", review.Spec.User, user.Username)
		}
		if review.Spec.ResourceAttributes.Resource == crpConfigMaps {
			got = append(got, review.Spec.ResourceAttributes.Namespace)
		}
		review.Status.Allowed = true
		return nil
	}})
	p := &placementv1beta1.ClusterResourcePlacement{Spec: placementv1beta1.PlacementSpec{ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{{
		Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources,
	}}}}
	if result := a.Authorize(context.Background(), user, p); result.Decision != DecisionAllowed {
		t.Fatal(result)
	}
	const newNamespace = "team-c"
	if err := reader.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: newNamespace}}); err != nil {
		t.Fatal(err)
	}
	got = nil
	user.Username = "updater"
	if result := a.Authorize(context.Background(), user, p); result.Decision != DecisionAllowed {
		t.Fatal(result)
	}
	if diff := cmp.Diff([]string{crpNamespaceA, crpNamespaceB, crpDeletingNS, newNamespace}, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("Authorize() expanded namespaces mismatch (-want +got):\n%s", diff)
	}
}

func TestClusterResourcePlacementAuthorizeFailures(t *testing.T) {
	ns := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResources}
	selective := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors}
	configMap := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.ConfigMapKind, Name: crpSource}
	role := placementv1beta1.ResourceSelectorTerm{Group: rbacv1.GroupName, Version: "v1", Kind: crpRoleKind, Name: crpSource}
	tests := []struct {
		name      string
		selectors []placementv1beta1.ResourceSelectorTerm
		modify    func(*PlacementAuthorizer, *placementv1beta1.ClusterResourcePlacement)
		denyVerb  string
		sarError  bool
		wantErr   string
		invalid   bool
	}{
		{name: "CRP has namespace", selectors: []placementv1beta1.ResourceSelectorTerm{role}, modify: func(_ *PlacementAuthorizer, p *placementv1beta1.ClusterResourcePlacement) {
			p.Namespace = crpNamespaceA
		}, wantErr: "must not specify a namespace", invalid: true},
		{name: "missing reader", selectors: []placementv1beta1.ResourceSelectorTerm{ns}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) { a.namespaceReader = nil }, wantErr: "dependencies are incomplete"},
		{name: "missing inventory", selectors: []placementv1beta1.ResourceSelectorTerm{ns}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.namespacedResources = nil
		}, wantErr: "dependencies are incomplete"},
		{name: "Namespace mapping failure", selectors: []placementv1beta1.ResourceSelectorTerm{ns}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.restMapper = meta.NewDefaultRESTMapper(nil)
		}, wantErr: "failed to resolve Namespace mapping"},
		{name: "unknown resource kind", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: crpUnknown}}, wantErr: "failed to resolve selector"},
		{name: "namespaced resource without selective namespace", selectors: []placementv1beta1.ResourceSelectorTerm{configMap}, wantErr: "requires NamespaceWithResourceSelectors", invalid: true},
		{name: "namespace expansion cannot scope explicit resources", selectors: []placementv1beta1.ResourceSelectorTerm{ns, configMap}, wantErr: "requires NamespaceWithResourceSelectors", invalid: true},
		{name: "selective namespace without name", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors}}, wantErr: "selected by name", invalid: true},
		{name: "selective namespace with label", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: placementv1beta1.NamespaceWithResourceSelectors, LabelSelector: &metav1.LabelSelector{}}}, wantErr: "selected by name", invalid: true},
		{name: "multiple selective namespace selectors", selectors: []placementv1beta1.ResourceSelectorTerm{selective, selective}, wantErr: "exactly one namespace selector", invalid: true},
		{name: "mixing namespace modes with selective namespace", selectors: []placementv1beta1.ResourceSelectorTerm{ns, selective}, wantErr: "exactly one namespace selector", invalid: true},
		{name: "unsupported namespace scope", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpNamespaceA, SelectionScope: crpUnknown}}, wantErr: "unsupported namespace selection scope", invalid: true},
		{name: "invalid namespace label", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceOnly, LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: crpLabel, Operator: "invalid"}}}}}, wantErr: "invalid resource label selector", invalid: true},
		{name: "namespace eligibility denied", selectors: []placementv1beta1.ResourceSelectorTerm{ns}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.resourceEligibilityChecker.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, []string{crpNamespaceA}, nil)
		}, wantErr: "not eligible for placement", invalid: true},
		{name: "selective namespace eligibility denied", selectors: []placementv1beta1.ResourceSelectorTerm{selective, configMap}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.resourceEligibilityChecker.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, []string{crpNamespaceA}, nil)
		}, wantErr: "not eligible for placement", invalid: true},
		{name: "selective namespace eligibility denied without additional resources", selectors: []placementv1beta1.ResourceSelectorTerm{selective}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.resourceEligibilityChecker.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, []string{crpNamespaceA}, nil)
		}, wantErr: "not eligible for placement", invalid: true},
		{name: "namespace list API failure", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources}}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.namespaceReader = fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list unavailable")
			}}).Build()
		}, wantErr: "list unavailable"},
		{name: "namespace list bad request remains operational", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, SelectionScope: placementv1beta1.NamespaceWithResources}}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			a.namespaceReader = fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return apierrors.NewBadRequest("namespace list rejected")
			}}).Build()
		}, wantErr: "namespace list rejected"},
		{name: "inventory mapper failure", selectors: []placementv1beta1.ResourceSelectorTerm{ns}, modify: func(a *PlacementAuthorizer, _ *placementv1beta1.ClusterResourcePlacement) {
			mapper := crpErrorRESTMapper{RESTMapper: a.restMapper, err: errors.New("inventory mapping unavailable")}
			a.resourceEligibilityChecker = resourceeligibility.AllowAll(mapper)
		}, wantErr: "inventory mapping unavailable"},
		{name: "get denied", selectors: []placementv1beta1.ResourceSelectorTerm{role}, denyVerb: crpGet},
		{name: "expanded list denied", selectors: []placementv1beta1.ResourceSelectorTerm{ns}, denyVerb: crpList},
		{name: "missing namespace expanded list denied", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpMissingNS, SelectionScope: placementv1beta1.NamespaceWithResources}}, denyVerb: crpList},
		{name: "deleting namespace expanded list denied", selectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.NamespaceGVK.Kind, Name: crpDeletingNS, SelectionScope: placementv1beta1.NamespaceWithResources}}, denyVerb: crpList},
		{name: "SAR API failure", selectors: []placementv1beta1.ResourceSelectorTerm{role}, sarError: true, wantErr: "SAR unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := newClusterPlacementTestAuthorizer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				if tt.sarError {
					return errors.New("SAR unavailable")
				}
				review.Status.Denied = review.Spec.ResourceAttributes.Verb == tt.denyVerb
				review.Status.Allowed = !review.Status.Denied
				return nil
			}})
			p := &placementv1beta1.ClusterResourcePlacement{Spec: placementv1beta1.PlacementSpec{ResourceSelectors: tt.selectors}}
			if tt.modify != nil {
				tt.modify(a, p)
			}
			result := a.Authorize(context.Background(), authenticationv1.UserInfo{Username: rpRequester}, p)
			wantDecision := DecisionIndeterminate
			if tt.invalid {
				wantDecision = DecisionInvalid
			}
			if tt.denyVerb != "" {
				if result.Decision != DecisionDenied || result.DenialReason == "" || result.Err != nil {
					t.Errorf("Authorize() = %+v, want denied", result)
				}
			} else if result.Decision != wantDecision || result.Err == nil || !strings.Contains(result.Err.Error(), tt.wantErr) {
				t.Errorf("Authorize() = %+v, want %s with error containing %q", result, wantDecision, tt.wantErr)
			}
		})
	}
}
