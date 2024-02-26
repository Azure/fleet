/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package rollout

import (
	"context"
	"errors"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
)

func (r *Reconciler) getAllOverrides(ctx context.Context, crp string, masterResourceSnapshot *fleetv1beta1.ClusterResourceSnapshot) ([]*fleetv1alpha1.ClusterResourceOverride, []*fleetv1alpha1.ResourceOverride, error) {
	resourceSnapshots, err := controller.FetchAllClusterResourceSnapshots(ctx, r.Client, crp, masterResourceSnapshot)
	if err != nil {
		return nil, nil, err
	}

	possibleCROs := make(map[fleetv1beta1.ResourceIdentifier]bool)
	possibleROs := make(map[fleetv1beta1.ResourceIdentifier]bool)
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
			if !r.InformerManager.IsClusterScopedResources(uResource.GroupVersionKind()) {
				croKey := fleetv1beta1.ResourceIdentifier{
					Group:   utils.NamespaceMetaGVK.Group,
					Version: utils.NamespaceMetaGVK.Version,
					Kind:    utils.NamespaceMetaGVK.Kind,
					Name:    uResource.GetNamespace(),
				}
				possibleCROs[croKey] = true // selected by the namespace
				roKey := fleetv1beta1.ResourceIdentifier{
					Group:     uResource.GetObjectKind().GroupVersionKind().Group,
					Version:   uResource.GetObjectKind().GroupVersionKind().Version,
					Kind:      uResource.GetObjectKind().GroupVersionKind().Kind,
					Namespace: uResource.GetNamespace(),
					Name:      uResource.GetName(),
				}
				possibleROs[roKey] = true // selected by the object itself
			} else {
				croKey := fleetv1beta1.ResourceIdentifier{
					Group:   uResource.GetObjectKind().GroupVersionKind().Group,
					Version: uResource.GetObjectKind().GroupVersionKind().Version,
					Kind:    uResource.GetObjectKind().GroupVersionKind().Kind,
					Name:    uResource.GetName(),
				}
				possibleCROs[croKey] = true // selected by the object itself
			}
		}
	}

	croList := &fleetv1alpha1.ClusterResourceOverrideList{}
	if err := r.Client.List(ctx, croList); err != nil {
		klog.ErrorS(err, "Failed to list all the clusterResourceOverrides")
		return nil, nil, err
	}
	roList := &fleetv1alpha1.ResourceOverrideList{}
	if err := r.Client.List(ctx, roList); err != nil {
		klog.ErrorS(err, "Failed to list all the resourceOverrides")
		return nil, nil, err
	}

	filteredCRO := make([]*fleetv1alpha1.ClusterResourceOverride, 0, len(croList.Items))
	filteredRO := make([]*fleetv1alpha1.ResourceOverride, 0, len(roList.Items))
	for i := range croList.Items {
		for _, selector := range croList.Items[i].Spec.ClusterResourceSelectors {
			croKey := fleetv1beta1.ResourceIdentifier{
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
		for _, selector := range roList.Items[i].Spec.ResourceSelectors {
			roKey := fleetv1beta1.ResourceIdentifier{
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

// getLatestOverridesPerBinding will look for any overrides associated with the "Bound" binding.
func (r *Reconciler) getLatestOverridesPerBinding(ctx context.Context, binding *fleetv1beta1.ClusterResourceBinding, croList []*fleetv1alpha1.ClusterResourceOverride, roList []*fleetv1alpha1.ResourceOverride) ([]string, []fleetv1beta1.NamespacedName, error) {
	cluster := clusterv1beta1.MemberCluster{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: binding.Spec.TargetCluster}, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("MemberCluster has been deleted and we expect that scheduler will update the spec of binding to unscheduled", "memberCluster", binding.Spec.TargetCluster, "clusterResourceBinding", klog.KObj(binding))
			return nil, nil, controller.NewExpectedBehaviorError(err)
		}
		klog.ErrorS(err, "Failed to get the memberCluster", "memberCluster", binding.Spec.TargetCluster, "clusterResourceBinding", klog.KObj(binding))
		return nil, nil, controller.NewAPIServerError(true, err)
	}

	croFiltered := make([]*fleetv1alpha1.ClusterResourceOverride, 0, len(croList))
	for i, cro := range croList {
		matched, err := isClusterMatched(cluster, cro.Spec.Policy)
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

	roFiltered := make([]*fleetv1alpha1.ResourceOverride, 0, len(roList))
	for i, ro := range roList {
		matched, err := isClusterMatched(cluster, ro.Spec.Policy)
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
	roNames := make([]fleetv1beta1.NamespacedName, len(roFiltered))
	for i, o := range roFiltered {
		roNames[i] = fleetv1beta1.NamespacedName{Name: o.Name, Namespace: o.Namespace}
	}
	return croNames, roNames, nil
}

func isClusterMatched(cluster clusterv1beta1.MemberCluster, policy *fleetv1alpha1.OverridePolicy) (bool, error) {
	if policy == nil {
		return false, errors.New("policy is nil")
	}
	for _, rule := range policy.OverrideRules {
		if rule.ClusterSelector == nil { // it means matching all the member clusters
			return true, nil
		}

		for _, term := range rule.ClusterSelector.ClusterSelectorTerms {
			selector, err := metav1.LabelSelectorAsSelector(&term.LabelSelector)
			if err != nil {
				return false, err
			}
			if selector.Matches(labels.Set(cluster.Labels)) {
				return true, nil
			}
		}
	}
	return false, nil
}
