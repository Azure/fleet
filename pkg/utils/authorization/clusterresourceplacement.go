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

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

// NamespacedResourceInventory supplies the namespaced resource types tracked by the placement controller.
type NamespacedResourceInventory interface {
	GetNameSpaceScopedResources() []schema.GroupVersionResource
}

type namespaceScope struct {
	hasSelectiveNamespace bool
	selectedNamespaceName string
}

func (a *PlacementAuthorizer) resolveClusterResourcePlacement(ctx context.Context, crp *placementv1beta1.ClusterResourcePlacement) ([]authorizationv1.ResourceAttributes, error) {
	if crp == nil {
		return nil, errors.New("cluster resource placement must not be nil")
	}
	if crp.Namespace != "" {
		return nil, invalidPlacementError("cluster resource placement must not specify a namespace")
	}
	expandedChecks, scope, err := a.resolveNamespaceSelectors(ctx, crp.Spec.ResourceSelectors)
	if err != nil {
		return nil, err
	}
	resourceChecks, err := a.resolveNonNamespaceSelectors(crp.Spec.ResourceSelectors, scope)
	if err != nil {
		return nil, err
	}
	return append(expandedChecks, resourceChecks...), nil
}

func (a *PlacementAuthorizer) resolveNamespaceSelectors(ctx context.Context, selectors []placementv1beta1.ResourceSelectorTerm) ([]authorizationv1.ResourceAttributes, namespaceScope, error) {
	var namespaces []corev1.Namespace
	var scope namespaceScope
	namespaceSelectorCount := 0
	for _, selector := range selectors {
		if selectorGVK(selector) != utils.NamespaceGVK {
			continue
		}
		namespaceSelectorCount++
		if selector.SelectionScope == placementv1beta1.NamespaceWithResourceSelectors {
			if scope.hasSelectiveNamespace {
				return nil, namespaceScope{}, invalidPlacementError("NamespaceWithResourceSelectors requires exactly one namespace selector")
			}
			scope.hasSelectiveNamespace = true
			scope.selectedNamespaceName = selector.Name
		}
		if !a.resourceEligibilityChecker.AllowsGVK(utils.NamespaceGVK) {
			continue
		}
		switch selector.SelectionScope {
		case placementv1beta1.NamespaceOnly:
			continue
		case placementv1beta1.NamespaceWithResourceSelectors:
			if !a.resourceEligibilityChecker.AllowsNamespace(selector.Name) {
				return nil, namespaceScope{}, invalidPlacementError(fmt.Sprintf("namespace %q is not eligible for placement", selector.Name))
			}
			continue
		case "", placementv1beta1.NamespaceWithResources:
			selected, err := a.selectNamespaces(ctx, selector)
			if err != nil {
				return nil, namespaceScope{}, err
			}
			namespaces = append(namespaces, selected...)
		default:
			return nil, namespaceScope{}, invalidPlacementError(fmt.Sprintf("unsupported namespace selection scope %q", selector.SelectionScope))
		}
	}
	if scope.hasSelectiveNamespace && namespaceSelectorCount != 1 {
		return nil, namespaceScope{}, invalidPlacementError("NamespaceWithResourceSelectors requires exactly one namespace selector")
	}
	attributes, err := a.expandNamespaces(namespaces)
	if err != nil {
		return nil, namespaceScope{}, err
	}
	return attributes, scope, nil
}

func (a *PlacementAuthorizer) resolveNonNamespaceSelectors(selectors []placementv1beta1.ResourceSelectorTerm, scope namespaceScope) ([]authorizationv1.ResourceAttributes, error) {
	var attributes []authorizationv1.ResourceAttributes
	for _, selector := range selectors {
		gvk := selectorGVK(selector)
		if gvk == utils.NamespaceGVK {
			continue
		}
		mapping, err := a.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve selector %s: %w", gvk, err)
		}
		namespace := ""
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if !scope.hasSelectiveNamespace {
				return nil, invalidPlacementError(fmt.Sprintf("cluster resource placement requires NamespaceWithResourceSelectors to select namespaced resource %s", gvk))
			}
			namespace = scope.selectedNamespaceName
		}
		if !a.resourceEligibilityChecker.AllowsGVK(gvk) {
			continue
		}
		check, err := a.selectorAttributes(mapping.Resource, namespace, selector)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, check)
	}
	return attributes, nil
}

func (a *PlacementAuthorizer) namespaceChecksForPlacement(placement placementv1beta1.PlacementObj) ([]authorizationv1.ResourceAttributes, error) {
	crp, ok := placement.(*placementv1beta1.ClusterResourcePlacement)
	if !ok {
		return nil, nil
	}
	if crp == nil {
		return nil, errors.New("cluster resource placement must not be nil")
	}
	if crp.Namespace != "" {
		return nil, invalidPlacementError("cluster resource placement must not specify a namespace")
	}
	var attributes []authorizationv1.ResourceAttributes
	for _, selector := range crp.Spec.ResourceSelectors {
		if selectorGVK(selector) != utils.NamespaceGVK {
			continue
		}
		checks, err := a.namespaceSelectorAttributes(selector)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, checks...)
	}
	return attributes, nil
}

func (a *PlacementAuthorizer) namespaceSelectorAttributes(selector placementv1beta1.ResourceSelectorTerm) ([]authorizationv1.ResourceAttributes, error) {
	if selector.SelectionScope == placementv1beta1.NamespaceWithResourceSelectors && (selector.Name == "" || selector.LabelSelector != nil) {
		return nil, invalidPlacementError("NamespaceWithResourceSelectors requires a namespace selected by name")
	}
	mapping, err := a.restMapper.RESTMapping(utils.NamespaceGVK.GroupKind(), utils.NamespaceGVK.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve Namespace mapping: %w", err)
	}
	if !a.resourceEligibilityChecker.AllowsGVK(utils.NamespaceGVK) {
		return nil, nil
	}
	check, err := a.selectorAttributes(mapping.Resource, "", selector)
	if err != nil {
		return nil, err
	}
	return []authorizationv1.ResourceAttributes{check}, nil
}

func (a *PlacementAuthorizer) selectNamespaces(ctx context.Context, selector placementv1beta1.ResourceSelectorTerm) ([]corev1.Namespace, error) {
	if selector.Name != "" {
		return []corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: selector.Name}}}, nil
	}
	labelSelector := labels.Everything()
	if selector.LabelSelector != nil {
		var err error
		labelSelector, err = metav1.LabelSelectorAsSelector(selector.LabelSelector)
		if err != nil {
			return nil, invalidPlacementError(fmt.Sprintf("invalid namespace label selector: %v", err))
		}
	}
	var list corev1.NamespaceList
	if err := a.namespaceReader.List(ctx, &list, client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return nil, fmt.Errorf("failed to list selected namespaces: %w", err)
	}
	return list.Items, nil
}

func (a *PlacementAuthorizer) expandNamespaces(namespaces []corev1.Namespace) ([]authorizationv1.ResourceAttributes, error) {
	if len(namespaces) == 0 {
		return nil, nil
	}
	for i := range namespaces {
		namespace := namespaces[i].Name
		if !a.resourceEligibilityChecker.AllowsNamespace(namespace) {
			return nil, invalidPlacementError(fmt.Sprintf("namespace %q is not eligible for placement", namespace))
		}
	}
	var attributes []authorizationv1.ResourceAttributes
	for _, gvr := range a.namespacedResources.GetNameSpaceScopedResources() {
		eligible, err := a.resourceEligibilityChecker.AllowsGVR(gvr)
		switch {
		case meta.IsNoMatchError(err):
			klog.V(2).InfoS("Skip namespace expansion authorization for a resource with no registered kind", "GVR", gvr.String())
			continue
		case err != nil:
			return nil, fmt.Errorf("failed to determine eligibility for %s: %w", gvr, err)
		case !eligible:
			continue
		}
		for i := range namespaces {
			attributes = append(attributes, resourceAttributes(gvr, "list", namespaces[i].Name, ""))
		}
	}
	return attributes, nil
}
