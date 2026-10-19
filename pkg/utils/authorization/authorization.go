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

// Package authorization provides authorization checks for resources selected by Fleet APIs.
package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultMaxConcurrentReviews limits the number of SubjectAccessReviews an admission request issues concurrently.
	DefaultMaxConcurrentReviews = 10
)

// SubjectAccessReviewClient creates SubjectAccessReviews.
type SubjectAccessReviewClient interface {
	Create(context.Context, ctrlclient.Object, ...ctrlclient.CreateOption) error
}

// Result reports the aggregate outcome of an authorization batch.
// When Reason and Err are both set, the denial is conclusive and Err reports additional review failures.
type Result struct {
	Allowed bool
	Reason  string
	Err     error
}

// Reviewer resolves resource kinds and reviews selected-resource access.
type Reviewer struct {
	client               SubjectAccessReviewClient
	restMapper           meta.RESTMapper
	maxConcurrentReviews int
}

// NewReviewer creates a selected-resource authorization reviewer.
func NewReviewer(client SubjectAccessReviewClient, restMapper meta.RESTMapper, maxConcurrentReviews int) (*Reviewer, error) {
	if client == nil {
		return nil, errors.New("subject access review client must not be nil")
	}
	if restMapper == nil {
		return nil, errors.New("REST mapper must not be nil")
	}
	if maxConcurrentReviews < 1 {
		return nil, errors.New("maximum concurrent reviews must be greater than zero")
	}
	return &Reviewer{
		client:               client,
		restMapper:           restMapper,
		maxConcurrentReviews: maxConcurrentReviews,
	}, nil
}

// ResourceAttributesFor resolves a resource kind to the plural resource used by Kubernetes authorization.
func (r *Reviewer) ResourceAttributesFor(gvk schema.GroupVersionKind, verb, namespace, name string) (authorizationv1.ResourceAttributes, error) {
	mapping, err := r.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return authorizationv1.ResourceAttributes{}, fmt.Errorf("failed to resolve resource mapping for %s: %w", gvk.String(), err)
	}
	return authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Verb:      verb,
		Group:     mapping.Resource.Group,
		Version:   mapping.Resource.Version,
		Resource:  mapping.Resource.Resource,
		Name:      name,
	}, nil
}

// Authorize verifies that the request user is allowed to perform all the supplied resource actions.
func (r *Reviewer) Authorize(ctx context.Context, userInfo authenticationv1.UserInfo, attributes ...authorizationv1.ResourceAttributes) Result {
	userInfo = *userInfo.DeepCopy()
	uniqueAttributes := make([]authorizationv1.ResourceAttributes, 0, len(attributes))
	seen := make(map[string]struct{}, len(attributes))
	for i := range attributes {
		key, err := reviewKey(attributes[i])
		if err != nil {
			return Result{Err: fmt.Errorf("failed to build authorization review key: %w", err)}
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniqueAttributes = append(uniqueAttributes, attributes[i])
	}

	var reviews errgroup.Group
	reviews.SetLimit(r.maxConcurrentReviews)
	results := make([]Result, len(uniqueAttributes))
	for i := range uniqueAttributes {
		attributes := uniqueAttributes[i]
		if err := ctx.Err(); err != nil {
			for j := i; j < len(uniqueAttributes); j++ {
				results[j] = Result{
					Err: fmt.Errorf("authorization review for %s was not submitted: %w", describeAttributes(uniqueAttributes[j]), err),
				}
			}
			break
		}
		reviews.Go(func() error {
			results[i] = r.review(ctx, userInfo, attributes)
			return nil
		})
	}
	// Review outcomes are stored in results; group functions do not return errors.
	_ = reviews.Wait()

	reasons := make([]string, 0, len(results))
	errs := make([]error, 0, len(results))
	for i := range results {
		if results[i].Allowed {
			continue
		}
		if results[i].Reason != "" {
			reasons = append(reasons, results[i].Reason)
		}
		if results[i].Err != nil {
			errs = append(errs, results[i].Err)
		}
		if results[i].Reason == "" && results[i].Err == nil {
			errs = append(errs, fmt.Errorf("authorization review for %s returned no decision", describeAttributes(uniqueAttributes[i])))
		}
	}
	if len(reasons) != 0 || len(errs) != 0 {
		return Result{
			Reason: strings.Join(reasons, "; "),
			Err:    errors.Join(errs...),
		}
	}
	return Result{Allowed: true}
}

func (r *Reviewer) review(ctx context.Context, userInfo authenticationv1.UserInfo, attributes authorizationv1.ResourceAttributes) Result {
	review := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:               userInfo.Username,
			UID:                userInfo.UID,
			Groups:             append([]string(nil), userInfo.Groups...),
			Extra:              authorizationExtra(userInfo.Extra),
			ResourceAttributes: attributes.DeepCopy(),
		},
	}
	if err := r.client.Create(ctx, review); err != nil {
		return Result{Err: fmt.Errorf("failed to create SubjectAccessReview for %s: %w", describeAttributes(attributes), err)}
	}
	if review.Status.Allowed && review.Status.Denied {
		return Result{Err: fmt.Errorf("SubjectAccessReview for %s returned conflicting allowed and denied decisions", describeAttributes(attributes))}
	}
	if review.Status.Allowed {
		return Result{Allowed: true}
	}
	if review.Status.EvaluationError != "" {
		return Result{Err: fmt.Errorf("SubjectAccessReview for %s reported an evaluation error: %s", describeAttributes(attributes), review.Status.EvaluationError)}
	}
	decision := "no opinion"
	if review.Status.Denied {
		decision = "denied"
	}
	if review.Status.Reason == "" {
		return Result{Reason: fmt.Sprintf("SubjectAccessReview %s access to %s", decision, describeAttributes(attributes))}
	}
	return Result{Reason: fmt.Sprintf("SubjectAccessReview %s access to %s: %s", decision, describeAttributes(attributes), review.Status.Reason)}
}

func reviewKey(attributes authorizationv1.ResourceAttributes) (string, error) {
	key, err := json.Marshal(attributes)
	if err != nil {
		return "", err
	}
	return string(key), nil
}

func authorizationExtra(extra map[string]authenticationv1.ExtraValue) map[string]authorizationv1.ExtraValue {
	if extra == nil {
		return nil
	}
	converted := make(map[string]authorizationv1.ExtraValue, len(extra))
	for key, values := range extra {
		converted[key] = append(authorizationv1.ExtraValue(nil), values...)
	}
	return converted
}

func describeAttributes(attributes authorizationv1.ResourceAttributes) string {
	resource := attributes.Resource
	if attributes.Group != "" {
		resource = resource + "." + attributes.Group
	}
	if attributes.Subresource != "" {
		resource = resource + "/" + attributes.Subresource
	}
	target := resource
	if attributes.Name != "" {
		target = fmt.Sprintf("%s %q", resource, attributes.Name)
	}
	if attributes.Namespace != "" {
		target = fmt.Sprintf("%s in namespace %q", target, attributes.Namespace)
	}
	return fmt.Sprintf("%s %s", attributes.Verb, target)
}
