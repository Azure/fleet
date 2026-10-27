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

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/resourceeligibility"
)

// ResourcePlacementAuthorizer reviews source-resource access for ResourcePlacement selectors.
type ResourcePlacementAuthorizer struct {
	restMapper                 meta.RESTMapper
	reviewer                   *Reviewer
	resourceEligibilityChecker resourceeligibility.Checker
}

// NewResourcePlacementAuthorizer creates an authorizer for namespaced ResourcePlacements.
func NewResourcePlacementAuthorizer(restMapper meta.RESTMapper, reviewer *Reviewer, checker resourceeligibility.Checker) (*ResourcePlacementAuthorizer, error) {
	if restMapper == nil {
		return nil, errors.New("REST mapper must not be nil")
	}
	if reviewer == nil {
		return nil, errors.New("authorization reviewer must not be nil")
	}
	if checker == nil {
		return nil, errors.New("resource eligibility checker must not be nil")
	}
	return &ResourcePlacementAuthorizer{
		restMapper:                 restMapper,
		reviewer:                   reviewer,
		resourceEligibilityChecker: checker,
	}, nil
}

// Authorize requires get access for named selections and list access for enumerated scopes.
func (a *ResourcePlacementAuthorizer) Authorize(ctx context.Context, user authenticationv1.UserInfo, placement placementv1beta1.PlacementObj) Result {
	if a == nil || a.restMapper == nil || a.reviewer == nil || a.resourceEligibilityChecker == nil {
		return Result{Decision: DecisionIndeterminate, Err: fmt.Errorf("placement authorization dependencies are incomplete")}
	}
	attributes, err := a.resolve(placement)
	if err != nil {
		return Result{Decision: DecisionIndeterminate, Err: err}
	}
	return a.reviewer.Authorize(ctx, user, attributes...)
}

func (a *ResourcePlacementAuthorizer) resolve(placement placementv1beta1.PlacementObj) ([]authorizationv1.ResourceAttributes, error) {
	switch placement := placement.(type) {
	case *placementv1beta1.ResourcePlacement:
		return a.resolveResourcePlacement(placement)
	case *placementv1beta1.ClusterResourcePlacement:
		return nil, errors.New("cluster resource placement authorization is not implemented")
	default:
		return nil, fmt.Errorf("unsupported placement type %T", placement)
	}
}

func (a *ResourcePlacementAuthorizer) resolveResourcePlacement(rp *placementv1beta1.ResourcePlacement) ([]authorizationv1.ResourceAttributes, error) {
	if rp == nil {
		return nil, fmt.Errorf("resource placement must not be nil")
	}
	if rp.Namespace == "" {
		return nil, fmt.Errorf("resource placement must specify a namespace")
	}
	var attributes []authorizationv1.ResourceAttributes
	for _, selector := range rp.Spec.ResourceSelectors {
		gvk := schema.GroupVersionKind{Group: selector.Group, Version: selector.Version, Kind: selector.Kind}
		mapping, err := a.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve selector %s: %w", gvk, err)
		}
		if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
			return nil, fmt.Errorf("resource placement cannot select cluster-scoped resource %s", gvk)
		}
		if !a.resourceEligibilityChecker.AllowsGVK(gvk) {
			continue
		}
		check, err := a.selectorAttributes(mapping.Resource, rp.Namespace, selector)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, check)
	}
	return attributes, nil
}

func (a *ResourcePlacementAuthorizer) selectorAttributes(gvr schema.GroupVersionResource, namespace string, selector placementv1beta1.ResourceSelectorTerm) (authorizationv1.ResourceAttributes, error) {
	if namespace != "" && !a.resourceEligibilityChecker.AllowsNamespace(namespace) {
		return authorizationv1.ResourceAttributes{}, fmt.Errorf("namespace %q is not eligible for placement", namespace)
	}
	if selector.Name != "" {
		if selector.LabelSelector != nil {
			return authorizationv1.ResourceAttributes{}, fmt.Errorf("name and labelSelector are mutually exclusive")
		}
		return resourceAttributes(gvr, "get", namespace, selector.Name), nil
	}
	if selector.LabelSelector != nil {
		if _, err := metav1.LabelSelectorAsSelector(selector.LabelSelector); err != nil {
			return authorizationv1.ResourceAttributes{}, fmt.Errorf("invalid resource label selector: %w", err)
		}
	}
	return resourceAttributes(gvr, "list", namespace, ""), nil
}

func resourceAttributes(gvr schema.GroupVersionResource, verb, namespace, name string) authorizationv1.ResourceAttributes {
	return authorizationv1.ResourceAttributes{
		Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
		Verb: verb, Namespace: namespace, Name: name,
	}
}
