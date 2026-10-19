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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeSubjectAccessReviewClient struct {
	create func(context.Context, *authorizationv1.SubjectAccessReview) error
}

func (f *fakeSubjectAccessReviewClient) Create(ctx context.Context, obj ctrlclient.Object, _ ...ctrlclient.CreateOption) error {
	review, ok := obj.(*authorizationv1.SubjectAccessReview)
	if !ok {
		return fmt.Errorf("object type = %T, want *authorizationv1.SubjectAccessReview", obj)
	}
	return f.create(ctx, review)
}

func TestResourceAttributesFor(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	mapper.AddSpecific(
		gvk,
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployment"},
		meta.RESTScopeNamespace,
	)
	reviewer, err := NewReviewer(&fakeSubjectAccessReviewClient{}, mapper, DefaultMaxConcurrentReviews)
	if err != nil {
		t.Fatalf("NewReviewer() returned error %v, want nil", err)
	}

	got, err := reviewer.ResourceAttributesFor(gvk, "get", "workloads", "frontend")
	if err != nil {
		t.Fatalf("ResourceAttributesFor() returned error %v, want nil", err)
	}
	want := authorizationv1.ResourceAttributes{
		Namespace: "workloads",
		Verb:      "get",
		Group:     "apps",
		Version:   "v1",
		Resource:  "deployments",
		Name:      "frontend",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ResourceAttributesFor() mismatch (-want +got):\n%s", diff)
	}
}

func TestResourceAttributesForUnknownKind(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper(nil)
	reviewer, err := NewReviewer(&fakeSubjectAccessReviewClient{}, mapper, DefaultMaxConcurrentReviews)
	if err != nil {
		t.Fatalf("NewReviewer() returned error %v, want nil", err)
	}

	_, err = reviewer.ResourceAttributesFor(
		schema.GroupVersionKind{Group: "unknown.example.io", Version: "v1", Kind: "Unknown"},
		"get",
		"",
		"example",
	)
	if err == nil {
		t.Fatal("ResourceAttributesFor() returned nil error, want mapping error")
	}
}

func TestAuthorizePreservesUserInfo(t *testing.T) {
	attributes := authorizationv1.ResourceAttributes{
		Namespace: "workloads",
		Verb:      "get",
		Group:     "",
		Version:   "v1",
		Resource:  "secrets",
		Name:      "database",
	}
	userInfo := authenticationv1.UserInfo{
		Username: "user@example.com",
		UID:      "requester-uid",
		Groups:   []string{"system:authenticated", "developers"},
		Extra: map[string]authenticationv1.ExtraValue{
			"oid":        {"object-id"},
			"tenant-id":  {"tenant"},
			"multiValue": {"first", "second"},
		},
	}
	var gotSpec authorizationv1.SubjectAccessReviewSpec
	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			gotSpec = *review.Spec.DeepCopy()
			review.Status = authorizationv1.SubjectAccessReviewStatus{Allowed: true}
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)

	result := reviewer.Authorize(context.Background(), userInfo, attributes)
	if !result.Allowed || result.Err != nil {
		t.Fatalf("Authorize() = %+v, want allowed result", result)
	}

	wantSpec := authorizationv1.SubjectAccessReviewSpec{
		User:   userInfo.Username,
		UID:    userInfo.UID,
		Groups: userInfo.Groups,
		Extra: map[string]authorizationv1.ExtraValue{
			"oid":        {"object-id"},
			"tenant-id":  {"tenant"},
			"multiValue": {"first", "second"},
		},
		ResourceAttributes: attributes.DeepCopy(),
	}
	if diff := cmp.Diff(wantSpec, gotSpec); diff != "" {
		t.Errorf("Authorize() SubjectAccessReview mismatch (-want +got):\n%s", diff)
	}
}

func TestAuthorizeReturnsDenialsAndErrors(t *testing.T) {
	apiErr := errors.New("API server unavailable")
	testCases := map[string]struct {
		create  func(context.Context, *authorizationv1.SubjectAccessReview) error
		wantErr string
		reason  string
	}{
		"denied": {
			create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status = authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: "RBAC denied the request"}
				return nil
			},
			reason: "denied access",
		},
		"no opinion": {
			create: func(context.Context, *authorizationv1.SubjectAccessReview) error {
				return nil
			},
			reason: "no opinion",
		},
		"evaluation error": {
			create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status = authorizationv1.SubjectAccessReviewStatus{EvaluationError: "authorizer failed"}
				return nil
			},
			wantErr: "evaluation error",
		},
		"conflicting decision": {
			create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
				review.Status = authorizationv1.SubjectAccessReviewStatus{Allowed: true, Denied: true}
				return nil
			},
			wantErr: "conflicting",
		},
		"API error": {
			create: func(context.Context, *authorizationv1.SubjectAccessReview) error {
				return apiErr
			},
			wantErr: apiErr.Error(),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			reviewer := newTestReviewer(t, &fakeSubjectAccessReviewClient{create: tc.create}, DefaultMaxConcurrentReviews)
			result := reviewer.Authorize(context.Background(), authenticationv1.UserInfo{Username: "requester"}, authorizationv1.ResourceAttributes{
				Verb:     "get",
				Resource: "namespaces",
				Name:     "workloads",
			})
			if result.Allowed {
				t.Errorf("Authorize() = %+v, want denied or error result", result)
			}
			if tc.wantErr != "" && (result.Err == nil || !strings.Contains(result.Err.Error(), tc.wantErr)) {
				t.Errorf("Authorize() error = %v, want error containing %q", result.Err, tc.wantErr)
			}
			if tc.reason != "" && (result.Err != nil || !strings.Contains(result.Reason, tc.reason)) {
				t.Errorf("Authorize() = %+v, want denial reason containing %q", result, tc.reason)
			}
		})
	}
}

func TestAuthorizeAllowsConclusiveDecisionWithEvaluationError(t *testing.T) {
	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			review.Status = authorizationv1.SubjectAccessReviewStatus{
				Allowed:         true,
				EvaluationError: "another authorizer failed",
			}
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)

	result := reviewer.Authorize(context.Background(), authenticationv1.UserInfo{Username: "requester"}, authorizationv1.ResourceAttributes{
		Verb:     "get",
		Resource: "namespaces",
		Name:     "workloads",
	})
	if !result.Allowed || result.Err != nil {
		t.Errorf("Authorize() = %+v, want allowed result", result)
	}
}

func TestAuthorizeHonorsContextTimeout(t *testing.T) {
	client := &fakeSubjectAccessReviewClient{
		create: func(ctx context.Context, _ *authorizationv1.SubjectAccessReview) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result := reviewer.Authorize(ctx, authenticationv1.UserInfo{Username: "requester"}, authorizationv1.ResourceAttributes{Verb: "get", Resource: "namespaces", Name: "workloads"})
	if !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Errorf("Authorize() error = %v, want %v", result.Err, context.DeadlineExceeded)
	}
}

func TestAuthorizeRecordsUnsubmittedReviews(t *testing.T) {
	var calls atomic.Int32
	client := &fakeSubjectAccessReviewClient{
		create: func(context.Context, *authorizationv1.SubjectAccessReview) error {
			calls.Add(1)
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := reviewer.Authorize(
		ctx,
		authenticationv1.UserInfo{Username: "requester"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "first"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "second"},
	)

	if result.Allowed {
		t.Errorf("Authorize() = %+v, want error result", result)
	}
	if !errors.Is(result.Err, context.Canceled) {
		t.Errorf("Authorize() error = %v, want joined error containing %v", result.Err, context.Canceled)
	}
	for _, name := range []string{"first", "second"} {
		want := fmt.Sprintf(`authorization review for get configmaps %q was not submitted`, name)
		if !strings.Contains(result.Err.Error(), want) {
			t.Errorf("Authorize() error = %q, want error containing %q", result.Err, want)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("SubjectAccessReview calls = %d, want 0", got)
	}
}

func TestAuthorizeReturnsDenialWhenConcurrencyIsLimited(t *testing.T) {
	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			review.Status = authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: "RBAC denied the request"}
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, 1)

	result := reviewer.Authorize(
		context.Background(),
		authenticationv1.UserInfo{Username: "requester"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "first"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "second"},
	)
	if result.Allowed || result.Err != nil || !strings.Contains(result.Reason, "RBAC denied the request") {
		t.Errorf("Authorize() = %+v, want original authorization denial", result)
	}
}

func TestAuthorizeCollectsReviewResults(t *testing.T) {
	apiErr := errors.New("API server unavailable")
	var calls atomic.Int32
	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			calls.Add(1)
			switch review.Spec.ResourceAttributes.Name {
			case "first":
				review.Status = authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: "first denied"}
				return nil
			case "second":
				return apiErr
			case "third":
				review.Status = authorizationv1.SubjectAccessReviewStatus{Denied: true, Reason: "third denied"}
				return nil
			default:
				return fmt.Errorf("unexpected resource %q", review.Spec.ResourceAttributes.Name)
			}
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)

	result := reviewer.Authorize(
		context.Background(),
		authenticationv1.UserInfo{Username: "requester"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "first"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "second"},
		authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "third"},
	)

	if result.Allowed {
		t.Errorf("Authorize() = %+v, want denied result", result)
	}
	wantReason := `SubjectAccessReview denied access to get configmaps "first": first denied; SubjectAccessReview denied access to get configmaps "third": third denied`
	if result.Reason != wantReason {
		t.Errorf("Authorize() reason = %q, want %q", result.Reason, wantReason)
	}
	if !errors.Is(result.Err, apiErr) {
		t.Errorf("Authorize() error = %v, want joined error containing %v", result.Err, apiErr)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("SubjectAccessReview calls = %d, want 3", got)
	}
}

func TestAuthorizeDeduplicatesReviews(t *testing.T) {
	var calls atomic.Int32
	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			calls.Add(1)
			review.Status = authorizationv1.SubjectAccessReviewStatus{Allowed: true}
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)
	attributes := authorizationv1.ResourceAttributes{Verb: "get", Resource: "namespaces", Name: "workloads"}

	result := reviewer.Authorize(context.Background(), authenticationv1.UserInfo{Username: "requester"}, attributes, attributes)
	if !result.Allowed || result.Err != nil {
		t.Fatalf("Authorize() = %+v, want allowed result", result)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("SubjectAccessReview calls = %d, want 1", got)
	}
}

func TestAuthorizeDoesNotCacheAcrossCalls(t *testing.T) {
	var calls atomic.Int32
	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			calls.Add(1)
			review.Status = authorizationv1.SubjectAccessReviewStatus{Allowed: true}
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, DefaultMaxConcurrentReviews)
	userInfo := authenticationv1.UserInfo{Username: "requester"}
	attributes := authorizationv1.ResourceAttributes{Verb: "get", Resource: "namespaces", Name: "workloads"}

	result := reviewer.Authorize(context.Background(), userInfo, attributes)
	if !result.Allowed || result.Err != nil {
		t.Fatalf("Authorize() = %+v, want allowed result", result)
	}
	result = reviewer.Authorize(context.Background(), userInfo, attributes)
	if !result.Allowed || result.Err != nil {
		t.Fatalf("Authorize() = %+v, want allowed result", result)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("SubjectAccessReview calls = %d, want 2", got)
	}
}

func TestAuthorizeLimitsConcurrency(t *testing.T) {
	const (
		reviewCount         = 5
		maxConcurrentReview = 2
	)
	started := make(chan struct{}, reviewCount)
	release := make(chan struct{})
	var closeRelease sync.Once
	defer closeRelease.Do(func() { close(release) })

	client := &fakeSubjectAccessReviewClient{
		create: func(_ context.Context, review *authorizationv1.SubjectAccessReview) error {
			started <- struct{}{}
			<-release
			review.Status = authorizationv1.SubjectAccessReviewStatus{Allowed: true}
			return nil
		},
	}
	reviewer := newTestReviewer(t, client, maxConcurrentReview)
	attributes := make([]authorizationv1.ResourceAttributes, reviewCount)
	for i := range attributes {
		attributes[i] = authorizationv1.ResourceAttributes{
			Verb:     "get",
			Resource: "configmaps",
			Name:     string(rune('a' + i)),
		}
	}

	result := make(chan Result, 1)
	go func() {
		result <- reviewer.Authorize(context.Background(), authenticationv1.UserInfo{Username: "requester"}, attributes...)
	}()

	for i := 0; i < maxConcurrentReview; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("Authorize() started %d reviews, want %d", i, maxConcurrentReview)
		}
	}
	select {
	case <-started:
		t.Fatalf("Authorize() exceeded concurrency limit %d", maxConcurrentReview)
	case <-time.After(50 * time.Millisecond):
	}

	closeRelease.Do(func() { close(release) })
	select {
	case got := <-result:
		if !got.Allowed || got.Err != nil {
			t.Errorf("Authorize() = %+v, want allowed result", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Authorize() did not finish after reviews were released")
	}
}

func TestDescribeAttributes(t *testing.T) {
	testCases := map[string]struct {
		attributes authorizationv1.ResourceAttributes
		want       string
	}{
		"core cluster-scoped named resource": {
			attributes: authorizationv1.ResourceAttributes{
				Verb:     "get",
				Resource: "namespaces",
				Name:     "workloads",
			},
			want: `get namespaces "workloads"`,
		},
		"grouped namespaced named resource": {
			attributes: authorizationv1.ResourceAttributes{
				Namespace: "workloads",
				Verb:      "get",
				Group:     "apps",
				Resource:  "deployments",
				Name:      "frontend",
			},
			want: `get deployments.apps "frontend" in namespace "workloads"`,
		},
		"subresource": {
			attributes: authorizationv1.ResourceAttributes{
				Namespace:   "workloads",
				Verb:        "update",
				Group:       "apps",
				Resource:    "deployments",
				Subresource: "status",
				Name:        "frontend",
			},
			want: `update deployments.apps/status "frontend" in namespace "workloads"`,
		},
		"namespaced list": {
			attributes: authorizationv1.ResourceAttributes{
				Namespace: "workloads",
				Verb:      "list",
				Resource:  "secrets",
			},
			want: `list secrets in namespace "workloads"`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := describeAttributes(tc.attributes)
			if got != tc.want {
				t.Errorf("describeAttributes(%v) = %q, want %q", tc.attributes, got, tc.want)
			}
		})
	}
}

func newTestReviewer(t *testing.T, client SubjectAccessReviewClient, maxConcurrentReviews int) *Reviewer {
	t.Helper()
	reviewer, err := NewReviewer(client, meta.NewDefaultRESTMapper(nil), maxConcurrentReviews)
	if err != nil {
		t.Fatalf("NewReviewer() returned error %v, want nil", err)
	}
	return reviewer
}
