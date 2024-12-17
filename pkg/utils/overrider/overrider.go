/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package overrider defines common utils for working with override.
package overrider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

// FetchAllMatchingOverridesForResourceSnapshot fetches all the matching overrides which are attached to the selected resources.
// TODO: to improve the performance, we can add the index on the placement field of the override snapshots.
func FetchAllMatchingOverridesForResourceSnapshot(
	ctx context.Context,
	c client.Client,
	manager informer.Manager,
	crp string,
	masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot,
) ([]*placementv1alpha1.ClusterResourceOverrideSnapshot, []*placementv1alpha1.ResourceOverrideSnapshot, error) {
	// fetch the cro and ro snapshot list first before finding the matched ones.
	latestSnapshotLabelMatcher := client.MatchingLabels{
		placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
	}
	croList := &placementv1alpha1.ClusterResourceOverrideSnapshotList{}
	if err := c.List(ctx, croList, latestSnapshotLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the clusterResourceOverrideSnapshots")
		return nil, nil, err
	}
	roList := &placementv1alpha1.ResourceOverrideSnapshotList{}
	if err := c.List(ctx, roList, latestSnapshotLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the resourceOverrideSnapshots")
		return nil, nil, err
	}

	if len(croList.Items) == 0 && len(roList.Items) == 0 {
		return nil, nil, nil // no overrides and nothing to do
	}

	resourceSnapshots, err := controller.FetchAllClusterResourceSnapshots(ctx, c, crp, masterResourceSnapshot)
	if err != nil {
		return nil, nil, err
	}

	possibleCROs := make(map[placementv1beta1.ResourceIdentifier]bool)
	possibleROs := make(map[placementv1beta1.ResourceIdentifier]bool)
	// List all the possible CROs and ROs based on the selected resources.
	for _, snapshot := range resourceSnapshots {
		for _, res := range snapshot.Spec.SelectedResources {
			var uResource unstructured.Unstructured
			if err := uResource.UnmarshalJSON(res.Raw); err != nil {
				klog.ErrorS(err, "Resource has invalid content", "snapshot", klog.KObj(snapshot), "selectedResource", res.Raw)
				return nil, nil, controller.NewUnexpectedBehaviorError(err)
			}
			// If the resource is namespaced scope resource, the resource could be selected by the namespace or selected
			// by the object itself.
			if !manager.IsClusterScopedResources(uResource.GroupVersionKind()) {
				croKey := placementv1beta1.ResourceIdentifier{
					Group:   utils.NamespaceMetaGVK.Group,
					Version: utils.NamespaceMetaGVK.Version,
					Kind:    utils.NamespaceMetaGVK.Kind,
					Name:    uResource.GetNamespace(),
				}
				possibleCROs[croKey] = true // selected by the namespace
				roKey := placementv1beta1.ResourceIdentifier{
					Group:     uResource.GetObjectKind().GroupVersionKind().Group,
					Version:   uResource.GetObjectKind().GroupVersionKind().Version,
					Kind:      uResource.GetObjectKind().GroupVersionKind().Kind,
					Namespace: uResource.GetNamespace(),
					Name:      uResource.GetName(),
				}
				possibleROs[roKey] = true // selected by the object itself
			} else {
				croKey := placementv1beta1.ResourceIdentifier{
					Group:   uResource.GetObjectKind().GroupVersionKind().Group,
					Version: uResource.GetObjectKind().GroupVersionKind().Version,
					Kind:    uResource.GetObjectKind().GroupVersionKind().Kind,
					Name:    uResource.GetName(),
				}
				possibleCROs[croKey] = true // selected by the object itself
			}
		}
	}

	filteredCRO := make([]*placementv1alpha1.ClusterResourceOverrideSnapshot, 0, len(croList.Items))
	filteredRO := make([]*placementv1alpha1.ResourceOverrideSnapshot, 0, len(roList.Items))
	for i := range croList.Items {
		placementInOverride := croList.Items[i].Spec.OverrideSpec.Placement
		if placementInOverride != nil && placementInOverride.Name != crp {
			klog.V(2).InfoS("Skipping this override which was created for another placement", "clusterResourceOverride", klog.KObj(&croList.Items[i]), "placementInOverride", placementInOverride.Name, "clusterResourcePlacement", crp)
			continue
		}

		for _, selector := range croList.Items[i].Spec.OverrideSpec.ClusterResourceSelectors {
			croKey := placementv1beta1.ResourceIdentifier{
				Group:   selector.Group,
				Version: selector.Version,
				Kind:    selector.Kind,
				Name:    selector.Name,
			}
			if possibleCROs[croKey] {
				filteredCRO = append(filteredCRO, &croList.Items[i])
				break
			}
		}
	}
	for i := range roList.Items {
		placementInOverride := roList.Items[i].Spec.OverrideSpec.Placement
		if placementInOverride != nil && placementInOverride.Name != crp {
			klog.V(2).InfoS("Skipping this override which was created for another placement", "resourceOverride", klog.KObj(&roList.Items[i]), "placementInOverride", placementInOverride.Name, "clusterResourcePlacement", crp)
			continue
		}

		for _, selector := range roList.Items[i].Spec.OverrideSpec.ResourceSelectors {
			roKey := placementv1beta1.ResourceIdentifier{
				Group:     selector.Group,
				Version:   selector.Version,
				Kind:      selector.Kind,
				Namespace: roList.Items[i].Namespace,
				Name:      selector.Name,
			}
			if possibleROs[roKey] {
				filteredRO = append(filteredRO, &roList.Items[i])
				break
			}
		}
	}
	return filteredCRO, filteredRO, nil
}

// PickFromResourceMatchedOverridesForTargetCluster filter the overrides that are matched with resources to the target cluster.
func PickFromResourceMatchedOverridesForTargetCluster(
	ctx context.Context,
	c client.Client,
	targetCluster string,
	croList []*placementv1alpha1.ClusterResourceOverrideSnapshot,
	roList []*placementv1alpha1.ResourceOverrideSnapshot,
) ([]string, []placementv1beta1.NamespacedName, error) {
	if len(croList) == 0 && len(roList) == 0 {
		return nil, nil, nil
	}

	cluster := clusterv1beta1.MemberCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: targetCluster}, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("MemberCluster has been deleted and we expect that scheduler will update the spec of binding to unscheduled", "memberCluster", targetCluster)
			return nil, nil, controller.NewExpectedBehaviorError(err)
		}
		klog.ErrorS(err, "Failed to get the memberCluster", "memberCluster", targetCluster)
		return nil, nil, controller.NewAPIServerError(true, err)
	}

	croFiltered := make([]*placementv1alpha1.ClusterResourceOverrideSnapshot, 0, len(croList))
	for i, cro := range croList {
		matched, err := isClusterMatched(cluster, cro.Spec.OverrideSpec.Policy)
		if err != nil {
			klog.ErrorS(err, "Invalid clusterResourceOverride", "clusterResourceOverride", klog.KObj(cro))
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		}
		if matched {
			croFiltered = append(croFiltered, croList[i])
		}
	}
	// There are no priority for now and sort the cro list by its name.
	sort.SliceStable(croFiltered, func(i, j int) bool {
		return croFiltered[i].Name < croFiltered[j].Name
	})

	roFiltered := make([]*placementv1alpha1.ResourceOverrideSnapshot, 0, len(roList))
	for i, ro := range roList {
		matched, err := isClusterMatched(cluster, ro.Spec.OverrideSpec.Policy)
		if err != nil {
			klog.ErrorS(err, "Invalid resourceOverride", "resourceOverride", klog.KObj(ro))
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		}
		if matched {
			roFiltered = append(roFiltered, roList[i])
		}
	}
	// There are no priority for now and sort the ro list by its namespace and then name.
	sort.SliceStable(roFiltered, func(i, j int) bool {
		if roFiltered[i].Namespace == roFiltered[j].Namespace {
			return roFiltered[i].Name < roFiltered[j].Name
		}
		return roFiltered[i].Namespace < roFiltered[j].Namespace
	})
	croNames := make([]string, len(croFiltered))
	for i, o := range croFiltered {
		croNames[i] = o.Name
	}
	roNames := make([]placementv1beta1.NamespacedName, len(roFiltered))
	for i, o := range roFiltered {
		roNames[i] = placementv1beta1.NamespacedName{Name: o.Name, Namespace: o.Namespace}
	}
	klog.V(2).InfoS("Found matched overrides for the target cluster", "memberCluster", targetCluster, "matchedCROCount", len(croNames), "matchedROCount", len(roNames))
	return croNames, roNames, nil
}

func isClusterMatched(cluster clusterv1beta1.MemberCluster, policy *placementv1alpha1.OverridePolicy) (bool, error) {
	if policy == nil {
		return false, errors.New("policy is nil")
	}
	for _, rule := range policy.OverrideRules {
		matched, err := IsClusterMatched(cluster, rule)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// IsClusterMatched checks if the cluster is matched with the override rules.
func IsClusterMatched(cluster clusterv1beta1.MemberCluster, rule placementv1alpha1.OverrideRule) (bool, error) {
	if rule.ClusterSelector == nil { // it means matching no member clusters
		return false, nil
	}

	if len(rule.ClusterSelector.ClusterSelectorTerms) == 0 {
		return true, nil // it means matching all member clusters
	}

	for _, term := range rule.ClusterSelector.ClusterSelectorTerms {
		selector, err := metav1.LabelSelectorAsSelector(term.LabelSelector)
		if err != nil {
			return false, fmt.Errorf("invalid cluster label selector %v: %w", term.LabelSelector, err)
		}
		if selector.Matches(labels.Set(cluster.Labels)) {
			return true, nil
		}
	}
	return false, nil
}
