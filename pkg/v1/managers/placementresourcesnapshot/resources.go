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

package placementresourcesnapshot

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/deployment"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	errors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	hasher "github.com/kubefleet-dev/kubefleet/pkg/utils/resource"
)

const (
	// etcd has a 1.5 MiB limit for objects by default, and Kubernetes clients might
	// reject request entities too large (~2/~3 MiB, depending on the protocol in use).
	//
	// With these factors considered, we set the maximum size of all resource data in a single placement
	// resource snapshot to be ~1.2 MiB, or ~1.26 MB, which should be safe in most cases. Note that the padding
	// space is not just reserved for safety reasons, but also to accommodate the additional fields in
	// the placement resource snapshot object, such as metadata and labels.
	DefaultMaxPerSnapshotResourceDataSizeBytes = 1258291 // 1.2 MiB, or ~1.26 MB.
	DefaultMaxPerSnapshotResourceCnt           = 50

	// The minimum and maximum limits for the size and count of resources in a single placement resource snapshot.
	MinPerSnapshotResourceDataSizeBytes = 2000 // 2 KB.
	MinPerSnapshotResourceCnt           = 2
	MaxPerSnapshotResourceDataSizeBytes = 1468000 // 1.4 MiB, or ~1.47 MB.
	MaxPerSnapshotResourceCnt           = 100
)

const (
	// The format in use to generate a unique identifier for each selected resource.
	//
	// The format is `[API-GROUP]/[KIND]/[NAMESPACE]/[NAME]`, where
	// `[API-GROUP]` is the API group of the resource, `[KIND]` is the kind of the resource, `[NAMESPACE]` is the namespace of the resource,
	// and `[NAME]` is the name of the resource.
	//
	// The API version value is omitted from the unique identifier as for the same resource, only one version is picked as the Kubernetes
	// storage version. It would not make sense to select the same resources twice using different API versions in the same placement policy.
	// When this happens, KubeFleet will pick the resource as selected by the resource selector that appears first.
	//
	// Also note that for cluster-scoped resources, the `[NAMESPACE]` segment will be empty.
	resourceUniqueIdStrFmt = "%s/%s/%s/%s"
)

func (m *Manager) retrieveAndHashSelectedResources(
	ctx context.Context,
	placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor,
) (
	resources []placementv1alpha1.SnapshottedResource,
	hash string,
	err error,
) {
	placementPolicySpec := placementPolicyAccessor.GetSpec()
	resources = make([]placementv1alpha1.SnapshottedResource, 0, len(placementPolicySpec.ResourceSelectors))
	seen := sets.Set[string]{}

	if len(placementPolicySpec.ResourceSelectors) == 0 {
		// KubeFleet does not consider the absence of resource selectors to be an error; however, this special
		// case should be handled by the caller (i.e., the placement resource snapshot manager should not be
		// called at all if there are no resource selectors), hence the unexpected error returned here.
		return nil, "", errors.NewUnexpectedError(nil, "no resource selectors are present")
	}
	if placementPolicyNSName := placementPolicyAccessor.GetNamespace(); placementPolicyNSName != "" {
		// Check if the placement policy is in a namespace eligible for placement.
		if m.resourceEligibilityChecker != nil && !m.resourceEligibilityChecker.AllowsNamespace(placementPolicyNSName) {
			return nil, "", errors.NewUserError(nil, "placement policy is in a namespace that is ineligible for placement",
				"placementPolicyNamespace", placementPolicyNSName)
		}
	}

	for idx := range placementPolicySpec.ResourceSelectors {
		selector := placementPolicySpec.ResourceSelectors[idx]

		// Check if the resource selector selects object(s) from a namespace eligible for placement.
		//
		// Note that if the resource selector selects namespaces, the check will be done later at the retrieval step.
		namespaceInSelector := selector.Namespace
		if placementPolicyNamespace := placementPolicyAccessor.GetNamespace(); placementPolicyNamespace != "" {
			if namespaceInSelector != "" && namespaceInSelector != placementPolicyNamespace {
				return nil, "", errors.NewUserError(nil, "resource selector specifies an invalid namespace (a placement policy can only select resources within its own namespace)",
					"resourceSelectorIndex", idx, "invalidNamespace", namespaceInSelector)
			}
		} else {
			if m.resourceEligibilityChecker != nil && !m.resourceEligibilityChecker.AllowsNamespace(namespaceInSelector) {
				return nil, "", errors.NewUserError(nil, "resource selector specifies a namespace that is ineligible for placement",
					"resourceSelectorIndex", idx, "ineligibleNamespace", namespaceInSelector)
			}
		}

		// Check if the resource selector specifies a GVK eligible for placement.
		gvkInSelector := schema.GroupVersionKind{
			Group:   selector.APIGroup,
			Version: selector.APIVersion,
			Kind:    selector.Kind,
		}
		if m.resourceEligibilityChecker != nil && !m.resourceEligibilityChecker.AllowsGVK(gvkInSelector) {
			return nil, "", errors.NewUserError(nil, "resource selector specifies a GVK that is ineligible for placement",
				"resourceSelectorIndex", idx, "ineligibleGVK", gvkInSelector)
		}

		resourcesAsSelected := make([]*placementv1alpha1.SnapshottedResource, 0, 5)
		switch {
		case len(selector.Name) != 0:
			// Retrieve the resource by name.
			res, err := m.retrieveResourceByName(ctx, placementPolicyAccessor, selector)
			if err != nil {
				return nil, "", errors.Wraps(err, "failed to retrieve a selected resource (name-based selector)",
					"resourceSelectorIndex", idx)
			}
			resourcesAsSelected = append(resourcesAsSelected, &res)
		case selector.LabelSelector != nil:
			// Retrieve the resources by label selector.
			resList, err := m.retrieveResourcesByLabelSelector(ctx, placementPolicyAccessor, selector)
			if err != nil {
				return nil, "", errors.Wraps(err, "failed to retrieve selected resources (label selector-based selector)",
					"resourceSelectorIndex", idx)
			}
			for ridx := range resList {
				resourcesAsSelected = append(resourcesAsSelected, &resList[ridx])
			}
		default:
			return nil, "", errors.NewUserError(nil, "invalid resource selector: neither name nor label selector is specified",
				"resourceSelectorIndex", idx)
		}

		// Make sure that no resources are selected more than once. Duplicates are skipped.
		for ridx := range resourcesAsSelected {
			resource := resourcesAsSelected[ridx]
			resourceId := resourceUniqueId(resource)
			if !seen.Has(resourceId) {
				// The resource has not been seen before; add it to the list of selected resources.
				seen.Insert(resourceId)
				resources = append(resources, *resource)
			} else {
				// The resource has already been seen; skip it and log a message.
				klog.V(2).InfoS("Found duplicate selected resource; skipping it", "resourceId", resourceId, "resourceSelectorIndex", idx)
			}
		}
	}

	// Sort the selected resources to ensure deterministic outcomes.
	sort.Slice(resources, func(i, j int) bool {
		return resourceUniqueId(&resources[i]) < resourceUniqueId(&resources[j])
	})

	hash, err = hasher.HashOf(resources)
	if err != nil {
		return nil, "", errors.Wraps(err, "failed to compute the hash of the selected resources")
	}
	return resources, hash, nil
}

func (m *Manager) retrieveResourceByName(
	ctx context.Context,
	placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor,
	resourceSelector placementv1alpha1.ResourceSelector,
) (placementv1alpha1.SnapshottedResource, error) {
	gvk, gvr, namespace, err := m.lookUpGVKGVRAndNamespace(resourceSelector, placementPolicyAccessor.GetNamespace())
	if err != nil {
		return placementv1alpha1.SnapshottedResource{}, errors.Wraps(err, "failed to look up GVK, GVR and namespace",
			"resourceSelector", resourceSelector)
	}

	var resource *unstructured.Unstructured
	// Before retrieving the resource via cache, verify if the informer has been synced.
	if m.hubDynamicInformerManager.IsInformerSet(gvk) && m.hubDynamicInformerManager.IsInformerSynced(gvr) {
		// An informer for the selected resource has been set up and synced; proceed to retrieve the resource from the cache.
		var obj runtime.Object
		if namespace == "" {
			obj, err = m.hubDynamicInformerManager.Lister(gvr).Get(resourceSelector.Name)
		} else {
			obj, err = m.hubDynamicInformerManager.Lister(gvr).ByNamespace(namespace).Get(resourceSelector.Name)
		}
		if err != nil {
			return placementv1alpha1.SnapshottedResource{}, errors.NewAPIServerError(err, "failed to get selected resource", true,
				"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
		}
		var ok bool
		resource, ok = obj.(*unstructured.Unstructured)
		if !ok {
			return placementv1alpha1.SnapshottedResource{}, errors.NewUnexpectedError(nil, "failed to convert the retrieved resource to unstructured",
				"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
		}
	} else {
		// No informer is set up for the selected resource, or the informer has not been synced yet.
		//
		// As a fallback, retrieve the resource directly from the API server.
		klog.V(2).InfoS("Informer for the selected resource is not set up or not synced; retrieving the resource directly from the API server",
			"gvr", gvr)
		if namespace == "" {
			resource, err = m.hubDynamicClient.Resource(gvr).Get(ctx, resourceSelector.Name, metav1.GetOptions{})
		} else {
			resource, err = m.hubDynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, resourceSelector.Name, metav1.GetOptions{})
		}
		if err != nil {
			return placementv1alpha1.SnapshottedResource{}, errors.NewAPIServerError(err, "failed to get selected resource directly from the API server", false,
				"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
		}
	}

	// Check if the selected resource is eligible for placement.
	//
	// Note that here the resource snapshot manager will throw an error if the resource is not eligible for placement
	// rather than silently ignoring it, as the selection is explicitly defined.
	if m.resourceEligibilityChecker != nil {
		// As a special case, if the selected resource is a namespace itself, check if the namespace is eligible for
		// placement.
		if gvk.Group == "" && gvk.Kind == utils.NamespaceKind {
			if !m.resourceEligibilityChecker.AllowsNamespace(resource.GetName()) {
				return placementv1alpha1.SnapshottedResource{}, errors.NewUserError(nil,
					"selected namespace is not eligible for placement",
					"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
			}
		}

		// Check if the resource itself is eligible for placement.
		isEligible, reason, err := m.resourceEligibilityChecker.AllowsObject(resource)
		if err != nil {
			return placementv1alpha1.SnapshottedResource{}, errors.Wraps(err, "failed to check if the selected resource is eligible for placement",
				"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
		}
		if !isEligible {
			return placementv1alpha1.SnapshottedResource{}, errors.NewUserError(nil,
				fmt.Sprintf("selected resource is not eligible for placement (%s)", reason),
				"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
		}
	}

	snapshottedResource, err := snapshotResource(resource)
	if err != nil {
		return placementv1alpha1.SnapshottedResource{},
			errors.Wraps(err, "failed to snapshot selected resource",
				"gvr", gvr, "namespace", namespace, "name", resourceSelector.Name)
	}
	return snapshottedResource, nil
}

func (m *Manager) retrieveResourcesByLabelSelector(
	ctx context.Context,
	placementPolicyAccessor placementv1alpha1.PlacementPolicyAccessor,
	resourceSelector placementv1alpha1.ResourceSelector,
) ([]placementv1alpha1.SnapshottedResource, error) {
	gvk, gvr, namespace, err := m.lookUpGVKGVRAndNamespace(resourceSelector, placementPolicyAccessor.GetNamespace())
	if err != nil {
		return nil, errors.Wraps(err, "failed to look up GVK, GVR and namespace", "resourceSelector", resourceSelector)
	}

	// Convert the label selector into a selector string.
	selector, err := metav1.LabelSelectorAsSelector(resourceSelector.LabelSelector)
	if err != nil {
		return nil, errors.NewUserError(err, "invalid label selector", "gvk", gvk, "labelSelector", resourceSelector.LabelSelector)
	}

	var resources []*unstructured.Unstructured
	if m.hubDynamicInformerManager.IsInformerSet(gvk) && m.hubDynamicInformerManager.IsInformerSynced(gvr) {
		// An informer for the selected resources has been set up and synced; proceed to retrieve the resources from the cache.
		var objList []runtime.Object
		if namespace == "" {
			objList, err = m.hubDynamicInformerManager.Lister(gvr).List(selector)
		} else {
			objList, err = m.hubDynamicInformerManager.Lister(gvr).ByNamespace(namespace).List(selector)
		}
		if err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list the selected resources", true,
				"gvr", gvr, "namespace", namespace, "labelSelector", selector.String())
		}

		for idx := range objList {
			obj := objList[idx]
			resource, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return nil, errors.NewUnexpectedError(nil, "failed to convert the retrieved resource to unstructured",
					"gvr", gvr, "namespace", namespace)
			}
			resources = append(resources, resource)
		}
	} else {
		// No informer is set up for the selected resources, or the informer has not been synced yet.
		//
		// As a fallback, retrieve the resources directly from the API server.
		klog.V(2).InfoS("Informer for the selected resources is not set up or not synced; retrieving the resources directly from the API server",
			"gvr", gvr)
		var resourceList *unstructured.UnstructuredList
		if namespace == "" {
			resourceList, err = m.hubDynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{
				LabelSelector: selector.String(),
			})
		} else {
			resourceList, err = m.hubDynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: selector.String(),
			})
		}
		if err != nil {
			return nil, errors.NewAPIServerError(err, "failed to list the selected resources", false,
				"gvr", gvr, "namespace", namespace, "labelSelector", selector.String())
		}

		for idx := range resourceList.Items {
			resource := &resourceList.Items[idx]
			resources = append(resources, resource)
		}
	}

	snapshottedResources := make([]placementv1alpha1.SnapshottedResource, 0, len(resources))
	for idx := range resources {
		resource := resources[idx]

		// Check if the resource is eligible for placement.
		//
		// Note that here the resource snapshot manager will skip a resource if it is not eligible for placement
		// rather than returning an error to the caller, as a label selector may match resources that the user
		// did not explicitly pick (e.g., the built-in CA certificate config map in every namespace).
		if m.resourceEligibilityChecker != nil {
			// As a special case, if the selected resource is a namespace itself, check if the namespace is eligible for
			// placement.
			if gvk.Group == "" && gvk.Kind == utils.NamespaceKind {
				if !m.resourceEligibilityChecker.AllowsNamespace(resource.GetName()) {
					klog.V(2).InfoS("Skipping a namespace that is not eligible for placement",
						"gvr", gvr, "resource", klog.KObj(resource))
					continue
				}
			}

			isEligible, reason, err := m.resourceEligibilityChecker.AllowsObject(resource)
			if err != nil {
				return nil, errors.Wraps(err, "failed to check if a selected resource is eligible for placement",
					"gvr", gvr, "namespace", namespace, "name", resource.GetName())
			}
			if !isEligible {
				klog.V(2).InfoS("Skipping a selected resource that is not eligible for placement",
					"gvr", gvr, "resource", klog.KObj(resource), "reason", reason)
				continue
			}
		}

		snapshottedResource, err := snapshotResource(resource)
		if err != nil {
			return nil, errors.Wraps(err, "failed to snapshot selected resource",
				"gvr", gvr, "namespace", namespace, "name", resource.GetName())
		}
		snapshottedResources = append(snapshottedResources, snapshottedResource)
	}
	return snapshottedResources, nil
}

func (m *Manager) lookUpGVKGVRAndNamespace(resourceSelector placementv1alpha1.ResourceSelector, placementPolicyNSName string) (
	schema.GroupVersionKind, schema.GroupVersionResource, string, error) {
	gvk := schema.GroupVersionKind{
		Group:   resourceSelector.APIGroup,
		Version: resourceSelector.APIVersion,
		Kind:    resourceSelector.Kind,
	}

	// Convert the GVK to a GVR using the REST mapper.
	mapping, err := m.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return schema.GroupVersionKind{}, schema.GroupVersionResource{}, "", errors.NewUserError(err, "no match found for GVK; the API might not be available on the cluster", "gvk", gvk)
		}
		return schema.GroupVersionKind{}, schema.GroupVersionResource{}, "", errors.NewUnexpectedError(err, "failed to map GVK to GVR", "gvk", gvk)
	}
	gvr := mapping.Resource
	scope := mapping.Scope.Name()

	// Determine the namespace of the selected resources.
	//
	// If the placement policy is namespace-scoped, the selected resources are assumed to be from the same namespace as the placement policy;
	// if the placement policy is cluster-scoped, the namespace is taken from the resource selector.
	namespace := resourceSelector.Namespace
	if placementPolicyNSName != "" {
		namespace = placementPolicyNSName
	}

	// Check if the resolved scope matches with the placement policy, i.e., PlacementPolicy can only select namespaced resources, while
	// ClusterPlacementPolicy can select any resource.
	if scope == meta.RESTScopeNameRoot && placementPolicyNSName != "" {
		return schema.GroupVersionKind{}, schema.GroupVersionResource{}, "",
			errors.NewUserError(nil, "cluster-scoped resource cannot be selected by a placement policy; use cluster placement policy instead",
				"resourceSelector", resourceSelector)
	}
	// When selecting a namespace-scoped resource, a namespace must be specified.
	if scope == meta.RESTScopeNameNamespace && namespace == "" {
		return schema.GroupVersionKind{}, schema.GroupVersionResource{}, "",
			errors.NewUserError(nil, "a namespace must be specified for namespace-scoped resources", "resourceSelector", resourceSelector)
	}
	// Check if the resolved scope matches with the resource selector, i.e., when selecting a cluster-scoped resource, no namespace can
	// be specified.
	if scope == meta.RESTScopeNameRoot && namespace != "" {
		return schema.GroupVersionKind{}, schema.GroupVersionResource{}, "",
			errors.NewUserError(nil, "namespace must not be specified for cluster-scoped resources", "resourceSelector", resourceSelector)
	}

	return gvk, gvr, namespace, nil
}

// snapshotResource removes fields that are not needed in a snapshot from an unstructured resource and converts it
// into a SnapshottedResource.
func snapshotResource(resource *unstructured.Unstructured) (placementv1alpha1.SnapshottedResource, error) {
	// Create a deep copy of the resource.
	resourceCopy := resource.DeepCopy()

	// Remove certain labels and annotations.
	if annotations := resourceCopy.GetAnnotations(); annotations != nil {
		// Remove the last applied configuration set by kubectl.
		delete(annotations, corev1.LastAppliedConfigAnnotation)

		// Remove the revision annotation set by deployment controller.
		delete(annotations, deployment.RevisionAnnotation)

		if len(annotations) == 0 {
			resourceCopy.SetAnnotations(nil)
		} else {
			resourceCopy.SetAnnotations(annotations)
		}
	}

	// Remove certain system-managed fields.
	resourceCopy.SetOwnerReferences(nil)
	resourceCopy.SetManagedFields(nil)

	// Remove the read-only fields.
	resourceCopy.SetCreationTimestamp(metav1.Time{})
	resourceCopy.SetDeletionTimestamp(nil)
	resourceCopy.SetDeletionGracePeriodSeconds(nil)
	resourceCopy.SetGeneration(0)
	resourceCopy.SetResourceVersion("")
	resourceCopy.SetSelfLink("")
	resourceCopy.SetUID("")
	resourceCopy.SetFinalizers(nil)

	// Remove the status field.
	unstructured.RemoveNestedField(resourceCopy.Object, "status")

	resourceCopyRawData, err := resourceCopy.MarshalJSON()
	if err != nil {
		return placementv1alpha1.SnapshottedResource{}, errors.NewUnexpectedError(err, "failed to marshal the resource copy to JSON",
			"resource", klog.KObj(resourceCopy))
	}

	gvk := resource.GroupVersionKind()

	// Note that for regular Kubernetes resources, the additional information field is always left empty.
	return placementv1alpha1.SnapshottedResource{
		Identifier: placementv1alpha1.ObjectReference{
			Namespace:  resourceCopy.GetNamespace(),
			Name:       resourceCopy.GetName(),
			APIGroup:   gvk.Group,
			APIVersion: gvk.Version,
			Kind:       gvk.Kind,
		},
		Manifest: runtime.RawExtension{Raw: resourceCopyRawData},
	}, nil
}

func resourceUniqueId(resource *placementv1alpha1.SnapshottedResource) string {
	return fmt.Sprintf(resourceUniqueIdStrFmt,
		resource.Identifier.APIGroup,
		resource.Identifier.Kind,
		resource.Identifier.Namespace,
		resource.Identifier.Name)
}

func (m *Manager) splitResourcesIntoSizeControlledGroups(resources []placementv1alpha1.SnapshottedResource) ([][]placementv1alpha1.SnapshottedResource, error) {
	if len(resources) == 0 {
		// Return one single empty group.
		return [][]placementv1alpha1.SnapshottedResource{{}}, nil
	}

	var groups [][]placementv1alpha1.SnapshottedResource
	// Pre-allocate with a reasonably guessed initial capacity.
	currentGroup := make([]placementv1alpha1.SnapshottedResource, 0, 10)
	currentSize := 0

	for i := range resources {
		resource := resources[i]
		resourceSize := len(resource.Manifest.Raw)
		for _, info := range resource.AdditionalInfo {
			resourceSize += len(info)
		}

		if resourceSize > m.maxPerSnapshotResourceDataSizeBytes {
			// A single resource exceeds the per-snapshot size limit; it can never fit into any group.
			return nil, errors.NewUserError(nil, "a single selected resource is too large to fit in a placement resource snapshot",
				"resource", resource.Identifier,
				"resourceSizeBytes", resourceSize, "maxPerSnapshotResourceDataSizeBytes", m.maxPerSnapshotResourceDataSizeBytes)
		}

		// Start a new group if adding this resource would exceed either the size or the count limit.
		if len(currentGroup) > 0 &&
			(currentSize+resourceSize > m.maxPerSnapshotResourceDataSizeBytes || len(currentGroup) >= m.maxPerSnapshotResourceCnt) {
			groups = append(groups, currentGroup)
			currentGroup = nil
			currentSize = 0
		}

		currentGroup = append(currentGroup, resource)
		currentSize += resourceSize
	}

	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups, nil
}
