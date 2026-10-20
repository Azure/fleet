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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/resourceeligibility"
)

const (
	rpNamespace = "team"
	rpSource    = "config"
	rpRequester = "requester"
)

func resourcePlacementTestMapper() *meta.DefaultRESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind(utils.ConfigMapKind), meta.RESTScopeNamespace)
	mapper.Add(utils.NamespaceGVK, meta.RESTScopeRoot)
	return mapper
}

func TestNewResourcePlacementAuthorizer(t *testing.T) {
	mapper := resourcePlacementTestMapper()
	reviewer := newTestReviewer(t, &fakeSubjectAccessReviewClient{}, 1)
	checker := resourceeligibility.AllowAll(mapper)
	tests := []struct {
		name     string
		mapper   meta.RESTMapper
		reviewer *Reviewer
		checker  resourceeligibility.Checker
		wantErr  string
	}{
		{name: "valid", mapper: mapper, reviewer: reviewer, checker: checker},
		{name: "missing mapper", reviewer: reviewer, checker: checker, wantErr: "REST mapper must not be nil"},
		{name: "missing reviewer", mapper: mapper, checker: checker, wantErr: "authorization reviewer must not be nil"},
		{name: "missing checker", mapper: mapper, reviewer: reviewer, wantErr: "resource eligibility checker must not be nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewResourcePlacementAuthorizer(tt.mapper, tt.reviewer, tt.checker)
			if tt.wantErr != "" {
				if got != nil || err == nil || err.Error() != tt.wantErr {
					t.Fatalf("NewResourcePlacementAuthorizer() = (%v, %v), want (nil, %q)", got, err, tt.wantErr)
				}
			} else if got == nil || err != nil {
				t.Fatalf("NewResourcePlacementAuthorizer() = (%v, %v), want initialized authorizer and nil error", got, err)
			}
		})
	}
}

func TestResourcePlacementAuthorizeUninitialized(t *testing.T) {
	for name, authorizer := range map[string]*ResourcePlacementAuthorizer{"nil": nil, "zero value": {}} {
		t.Run(name, func(t *testing.T) {
			result := authorizer.Authorize(context.Background(), authenticationv1.UserInfo{}, &placementv1beta1.ResourcePlacement{})
			if result.Decision != DecisionIndeterminate || result.Err == nil {
				t.Errorf("Authorize() = %+v, want initialization error", result)
			}
		})
	}
}

func TestResourcePlacementAuthorizeSelections(t *testing.T) {
	named := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.ConfigMapKind, Name: rpSource}
	all := placementv1beta1.ResourceSelectorTerm{Version: "v1", Kind: utils.ConfigMapKind}
	labeled := placementv1beta1.ResourceSelectorTerm{
		Version: "v1", Kind: utils.ConfigMapKind,
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "selected"}},
	}
	emptyLabels := placementv1beta1.ResourceSelectorTerm{
		Version: "v1", Kind: utils.ConfigMapKind,
		LabelSelector: &metav1.LabelSelector{},
	}
	get := resourceAttributes(corev1.SchemeGroupVersion.WithResource("configmaps"), "get", rpNamespace, rpSource)
	list := resourceAttributes(corev1.SchemeGroupVersion.WithResource("configmaps"), "list", rpNamespace, "")
	tests := []struct {
		name      string
		selectors []placementv1beta1.ResourceSelectorTerm
		want      []authorizationv1.ResourceAttributes
	}{
		{name: "named object", selectors: []placementv1beta1.ResourceSelectorTerm{named}, want: []authorizationv1.ResourceAttributes{get}},
		{name: "label selection needs only list", selectors: []placementv1beta1.ResourceSelectorTerm{labeled}, want: []authorizationv1.ResourceAttributes{list}},
		{name: "all of kind needs only list", selectors: []placementv1beta1.ResourceSelectorTerm{all}, want: []authorizationv1.ResourceAttributes{list}},
		{name: "empty label selector", selectors: []placementv1beta1.ResourceSelectorTerm{emptyLabels}, want: []authorizationv1.ResourceAttributes{list}},
		{name: "overlapping selectors deduplicate reviews", selectors: []placementv1beta1.ResourceSelectorTerm{all, labeled, named, all, named}, want: []authorizationv1.ResourceAttributes{list, get}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := resourcePlacementTestMapper()
			var got []authorizationv1.ResourceAttributes
			reviewer := newTestReviewer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				got = append(got, *review.Spec.ResourceAttributes)
				review.Status.Allowed = true
				return nil
			}}, 1)
			a, err := NewResourcePlacementAuthorizer(mapper, reviewer, resourceeligibility.AllowAll(mapper))
			if err != nil {
				t.Fatal(err)
			}
			p := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{Namespace: rpNamespace},
				Spec:       placementv1beta1.PlacementSpec{ResourceSelectors: tt.selectors},
			}
			result := a.Authorize(context.Background(), authenticationv1.UserInfo{Username: rpRequester}, p)
			if diff := cmp.Diff(Result{Decision: DecisionAllowed}, result); diff != "" {
				t.Fatalf("Authorize() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.SortSlices(func(a, b authorizationv1.ResourceAttributes) bool {
				return a.Verb < b.Verb
			})); diff != "" {
				t.Errorf("Authorize() checks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResourcePlacementAuthorizeRejectsUnsupportedTypes(t *testing.T) {
	const deferredCRPErr = "cluster resource placement authorization is not implemented"
	for name, tt := range map[string]struct {
		placement placementv1beta1.PlacementObj
		wantErr   string
	}{
		"nil":           {wantErr: "unsupported placement type <nil>"},
		"typed nil RP":  {placement: (*placementv1beta1.ResourcePlacement)(nil), wantErr: "resource placement must not be nil"},
		"CRP":           {placement: &placementv1beta1.ClusterResourcePlacement{}, wantErr: deferredCRPErr},
		"typed nil CRP": {placement: (*placementv1beta1.ClusterResourcePlacement)(nil), wantErr: deferredCRPErr},
	} {
		t.Run(name, func(t *testing.T) {
			mapper := resourcePlacementTestMapper()
			a, err := NewResourcePlacementAuthorizer(mapper, newTestReviewer(t, &fakeSubjectAccessReviewClient{}, 1), resourceeligibility.AllowAll(mapper))
			if err != nil {
				t.Fatal(err)
			}
			result := a.Authorize(context.Background(), authenticationv1.UserInfo{}, tt.placement)
			if result.Decision != DecisionIndeterminate || result.Err == nil || result.Err.Error() != tt.wantErr {
				t.Errorf("Authorize() = %+v, want indeterminate decision with error %q", result, tt.wantErr)
			}
		})
	}
}

func TestResourcePlacementAuthorizeFailures(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*ResourcePlacementAuthorizer, *placementv1beta1.ResourcePlacement)
		denied bool
	}{
		{name: "unknown kind", modify: func(_ *ResourcePlacementAuthorizer, p *placementv1beta1.ResourcePlacement) {
			p.Spec.ResourceSelectors[0].Kind = "Unknown"
		}},
		{name: "missing namespace", modify: func(_ *ResourcePlacementAuthorizer, p *placementv1beta1.ResourcePlacement) { p.Namespace = "" }},
		{name: "cluster-scoped selector", modify: func(_ *ResourcePlacementAuthorizer, p *placementv1beta1.ResourcePlacement) {
			p.Spec.ResourceSelectors[0].Kind = utils.NamespaceGVK.Kind
		}},
		{name: "name and label selector", modify: func(_ *ResourcePlacementAuthorizer, p *placementv1beta1.ResourcePlacement) {
			p.Spec.ResourceSelectors[0].LabelSelector = &metav1.LabelSelector{}
		}},
		{name: "invalid label selector", modify: func(_ *ResourcePlacementAuthorizer, p *placementv1beta1.ResourcePlacement) {
			p.Spec.ResourceSelectors[0].Name = ""
			p.Spec.ResourceSelectors[0].LabelSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}}}
		}},
		{name: "namespace excluded", modify: func(a *ResourcePlacementAuthorizer, _ *placementv1beta1.ResourcePlacement) {
			a.resourceEligibilityChecker.SetNamespaceCheckList(resourceeligibility.NamespaceEligibilityModeDenyList, []string{rpNamespace}, nil)
		}},
		{name: "SAR error", modify: func(a *ResourcePlacementAuthorizer, _ *placementv1beta1.ResourcePlacement) {
			a.reviewer.client = &fakeSubjectAccessReviewClient{create: func(context.Context, *authorizationv1.SubjectAccessReview) error {
				return errors.New("SAR unavailable")
			}}
		}},
		{name: "get denied", denied: true, modify: func(a *ResourcePlacementAuthorizer, _ *placementv1beta1.ResourcePlacement) {
			a.reviewer.client = &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status.Denied = true
				return nil
			}}
		}},
		{name: "list denied", denied: true, modify: func(a *ResourcePlacementAuthorizer, p *placementv1beta1.ResourcePlacement) {
			p.Spec.ResourceSelectors[0].Name = ""
			a.reviewer.client = &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status.Denied = true
				return nil
			}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := resourcePlacementTestMapper()
			reviewer := newTestReviewer(t, &fakeSubjectAccessReviewClient{create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status.Allowed = true
				return nil
			}}, 1)
			a, err := NewResourcePlacementAuthorizer(mapper, reviewer, resourceeligibility.AllowAll(mapper))
			if err != nil {
				t.Fatal(err)
			}
			p := &placementv1beta1.ResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{Namespace: rpNamespace},
				Spec:       placementv1beta1.PlacementSpec{ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.ConfigMapKind, Name: rpSource}}},
			}
			tt.modify(a, p)
			result := a.Authorize(context.Background(), authenticationv1.UserInfo{}, p)
			wantDecision := DecisionIndeterminate
			if tt.denied {
				wantDecision = DecisionDenied
			}
			if result.Decision != wantDecision || (tt.denied && result.DenialReason == "") || (!tt.denied && result.Err == nil) {
				t.Errorf("Authorize() = %+v, want %s with details", result, wantDecision)
			}
		})
	}
}

func TestResourcePlacementAuthorizeSkipsIneligibleKinds(t *testing.T) {
	mapper := resourcePlacementTestMapper()
	checker := resourceeligibility.AllowAll(mapper)
	if err := checker.SetGVKCheckList(resourceeligibility.GVKEligibilityModeDenyList, []schema.GroupVersionKind{corev1.SchemeGroupVersion.WithKind(utils.ConfigMapKind)}); err != nil {
		t.Fatal(err)
	}
	reviewer := newTestReviewer(t, &fakeSubjectAccessReviewClient{create: func(context.Context, *authorizationv1.SubjectAccessReview) error {
		t.Error("Authorize() submitted a SubjectAccessReview for an ineligible kind")
		return errors.New("unexpected SubjectAccessReview")
	}}, 1)
	a, err := NewResourcePlacementAuthorizer(mapper, reviewer, checker)
	if err != nil {
		t.Fatal(err)
	}
	p := &placementv1beta1.ResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{Namespace: rpNamespace},
		Spec:       placementv1beta1.PlacementSpec{ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{{Version: "v1", Kind: utils.ConfigMapKind}}},
	}
	if result := a.Authorize(context.Background(), authenticationv1.UserInfo{}, p); result.Decision != DecisionAllowed || result.Err != nil {
		t.Errorf("Authorize() = %+v, want allowed with no reviews", result)
	}
}
