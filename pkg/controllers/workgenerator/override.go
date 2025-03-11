/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workgenerator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/overrider"
)

func (r *Reconciler) fetchClusterResourceOverrideSnapshots(ctx context.Context, resourceBinding *placementv1beta1.ClusterResourceBinding) (map[placementv1beta1.ResourceIdentifier][]*placementv1alpha1.ClusterResourceOverrideSnapshot, error) {
	croMap := make(map[placementv1beta1.ResourceIdentifier][]*placementv1alpha1.ClusterResourceOverrideSnapshot)

	// For now, we get the snapshots sequentially. We can optimize this by getting them in parallel, but we need to reorder
	// the snapshot lists saved in the map.
	for _, name := range resourceBinding.Spec.ClusterResourceOverrideSnapshots {
		snapshot := &placementv1alpha1.ClusterResourceOverrideSnapshot{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: name}, snapshot); err != nil {
			if errors.IsNotFound(err) {
				klog.ErrorS(err, "The clusterResourceOverrideSnapshot is deleted", "resourceBinding", klog.KObj(resourceBinding), "clusterResourceOverrideSnapshot", name)
				// It could be caused by that the user updates the override too frequently and the snapshot has been replaced
				// by the new one.
				// TODO: support customized revision history limit
				return nil, controller.NewUserError(fmt.Errorf("clusterResourceSnapshot %s is not found", name))
			}
			klog.ErrorS(err, "Failed to get the clusterResourceOverrideSnapshot",
				"resourceBinding", klog.KObj(resourceBinding), "clusterResourceOverrideSnapshot", name)
			return nil, controller.NewAPIServerError(true, err)
		}
		for _, selector := range snapshot.Spec.OverrideSpec.ClusterResourceSelectors {
			// Note, we only support name selector here.
			key := placementv1beta1.ResourceIdentifier{
				Group:   selector.Group,
				Version: selector.Version,
				Kind:    selector.Kind,
				Name:    selector.Name,
			}
			croMap[key] = append(croMap[key], snapshot)
		}
	}
	klog.V(2).InfoS("Fetched clusterResourceOverrideSnapshots", "resourceBinding", klog.KObj(resourceBinding), "numberOfResources", len(croMap))
	return croMap, nil
}

func (r *Reconciler) fetchResourceOverrideSnapshots(ctx context.Context, resourceBinding *placementv1beta1.ClusterResourceBinding) (map[placementv1beta1.ResourceIdentifier][]*placementv1alpha1.ResourceOverrideSnapshot, error) {
	roMap := make(map[placementv1beta1.ResourceIdentifier][]*placementv1alpha1.ResourceOverrideSnapshot)

	// For now, we get the snapshots sequentially. We can optimize this by getting them in parallel, but we need to reorder
	// the snapshot lists saved in the map.
	for _, namespacedName := range resourceBinding.Spec.ResourceOverrideSnapshots {
		snapshot := &placementv1alpha1.ResourceOverrideSnapshot{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: namespacedName.Name, Namespace: namespacedName.Namespace}, snapshot); err != nil {
			if errors.IsNotFound(err) {
				// It could be caused by that the user updates the override too frequently and the snapshot has been replaced
				// by the new one.
				// TODO: support customized revision history limit
				klog.ErrorS(err, "The resourceOverrideSnapshot is deleted", "resourceBinding", klog.KObj(resourceBinding), "resourceOverrideSnapshot", namespacedName)
				return nil, controller.NewUserError(fmt.Errorf("resourceSnapshot %s is not found", namespacedName))
			}
			klog.ErrorS(err, "Failed to get the resourceOverrideSnapshot",
				"resourceBinding", klog.KObj(resourceBinding), "resourceOverrideSnapshot", namespacedName)
			return nil, controller.NewAPIServerError(true, err)
		}
		for _, selector := range snapshot.Spec.OverrideSpec.ResourceSelectors {
			key := placementv1beta1.ResourceIdentifier{
				Group:     selector.Group,
				Version:   selector.Version,
				Kind:      selector.Kind,
				Name:      selector.Name,
				Namespace: snapshot.Namespace,
			}
			roMap[key] = append(roMap[key], snapshot)
		}
	}
	klog.V(2).InfoS("Fetched resourceOverrideSnapshots", "resourceBinding", klog.KObj(resourceBinding), "numberOfResources", len(roMap))
	return roMap, nil
}

// applyOverrides applies the overrides on the selected resources.
// The resource could be selected by both ClusterResourceOverride and ResourceOverride.
// It returns
//   - true if the resource is deleted by the overrides.
//   - an error if the override rules are invalid.
func (r *Reconciler) applyOverrides(resource *placementv1beta1.ResourceContent, cluster *clusterv1beta1.MemberCluster,
	croMap map[placementv1beta1.ResourceIdentifier][]*placementv1alpha1.ClusterResourceOverrideSnapshot, roMap map[placementv1beta1.ResourceIdentifier][]*placementv1alpha1.ResourceOverrideSnapshot) (bool, error) {
	if len(croMap) == 0 && len(roMap) == 0 {
		return false, nil
	}

	var uResource unstructured.Unstructured
	if err := uResource.UnmarshalJSON(resource.Raw); err != nil {
		klog.ErrorS(err, "Work has invalid content", "selectedResource", resource.Raw)
		return false, controller.NewUnexpectedBehaviorError(err)
	}
	gvk := uResource.GetObjectKind().GroupVersionKind()
	key := placementv1beta1.ResourceIdentifier{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
		Name:    uResource.GetName(),
	}
	isClusterScopeResource := r.InformerManager.IsClusterScopedResources(gvk)

	// For the namespace scoped resource, it could be selected by the namespace itself.
	// use the namespace as the key
	if !isClusterScopeResource {
		key = placementv1beta1.ResourceIdentifier{
			Group:   utils.NamespaceMetaGVK.Group,
			Version: utils.NamespaceMetaGVK.Version,
			Kind:    utils.NamespaceMetaGVK.Kind,
			Name:    uResource.GetNamespace(),
		}
	}

	// Apply ClusterResourceOverrideSnapshots.
	for _, snapshot := range croMap[key] {
		if snapshot.Spec.OverrideSpec.Policy == nil {
			err := fmt.Errorf("invalid clusterResourceOverrideSnapshot %s: policy is nil", snapshot.Name)
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Found an invalid clusterResourceOverrideSnapshot", "clusterResourceOverrideSnapshot", klog.KObj(snapshot))
			continue // should not happen
		}
		if err := applyOverrideRules(resource, cluster, snapshot.Spec.OverrideSpec.Policy.OverrideRules); err != nil {
			klog.ErrorS(err, "Failed to apply the override rules", "clusterResourceOverrideSnapshot", klog.KObj(snapshot))
			return false, err
		}
	}
	klog.V(2).InfoS("Applied clusterResourceOverrideSnapshots", "resource", klog.KObj(&uResource), "numberOfOverrides", len(croMap[key]))

	// If the resource is selected by both ClusterResourceOverride and ResourceOverride, ResourceOverride will win when resolving conflicts.
	// Apply ResourceOverrideSnapshots.
	if !isClusterScopeResource {
		key = placementv1beta1.ResourceIdentifier{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			Name:      uResource.GetName(),
			Namespace: uResource.GetNamespace(),
		}
		for _, snapshot := range roMap[key] {
			if snapshot.Spec.OverrideSpec.Policy == nil {
				err := fmt.Errorf("invalid resourceOverrideSnapshot %s: policy is nil", snapshot.Name)
				klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Found an invalid resourceOverrideSnapshot", "resourceOverrideSnapshot", klog.KObj(snapshot))
				continue // should not happen
			}
			if err := applyOverrideRules(resource, cluster, snapshot.Spec.OverrideSpec.Policy.OverrideRules); err != nil {
				klog.ErrorS(err, "Failed to apply the override rules", "resourceOverrideSnapshot", klog.KObj(snapshot))
				return false, err
			}
		}
		klog.V(2).InfoS("Applied resourceOverrideSnapshots", "resource", klog.KObj(&uResource), "numberOfOverrides", len(roMap[key]))
	}
	return resource.Raw == nil, nil
}

func applyOverrideRules(resource *placementv1beta1.ResourceContent, cluster *clusterv1beta1.MemberCluster, rules []placementv1alpha1.OverrideRule) error {
	for _, rule := range rules {
		matched, err := overrider.IsClusterMatched(cluster, rule)
		if err != nil {
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Found an invalid override rule")
			return controller.NewUserError(err) // should not happen though and should be rejected by the webhook
		}
		if !matched {
			continue
		}
		if rule.OverrideType == placementv1alpha1.DeleteOverrideType {
			// Delete the resource
			resource.Raw = nil
			return nil
		}
		// Apply JSONPatchOverrides by default
		if err = applyJSONPatchOverride(resource, cluster, rule.JSONPatchOverrides); err != nil {
			klog.ErrorS(err, "Failed to apply JSON patch override")
			return controller.NewUserError(err)
		}
	}
	return nil
}

// applyJSONPatchOverride applies a JSON patch on the selected resources following [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902).
func applyJSONPatchOverride(resourceContent *placementv1beta1.ResourceContent, cluster *clusterv1beta1.MemberCluster, overrides []placementv1alpha1.JSONPatchOverride) error {
	var err error
	if len(overrides) == 0 { // do nothing
		return nil
	}
	// go through the JSON patch overrides to replace the built-in variables before json Marshal
	// as it may contain the built-in variables that cannot be marshaled directly
	for i := range overrides {
		// Process the JSON string to replace variables
		jsonStr := string(overrides[i].Value.Raw)
		// Replace the built-in ${MEMBER-CLUSTER-NAME} variable with the actual cluster name
		jsonStr = strings.ReplaceAll(jsonStr, placementv1alpha1.OverrideClusterNameVariable, cluster.Name)
		// Replace label key variables with actual label values
		jsonStr, err = replaceClusterLabelKeyVariables(jsonStr, cluster)
		if err != nil {
			klog.ErrorS(err, "Failed to replace cluster label key variables in JSON patch override")
			return err
		}
		overrides[i].Value.Raw = []byte(jsonStr)
	}

	jsonPatchBytes, err := json.Marshal(overrides)
	if err != nil {
		klog.ErrorS(err, "Failed to marshal JSON Patch overrides")
		return err
	}

	patch, err := jsonpatch.DecodePatch(jsonPatchBytes)
	if err != nil {
		klog.ErrorS(err, "Failed to decode the passed JSON document as an RFC 6902 patch")
		return err
	}

	patchedObjectJSONBytes, err := patch.Apply(resourceContent.Raw)
	if err != nil {
		klog.ErrorS(err, "Failed to apply the JSON patch to the resource")
		return err
	}
	resourceContent.Raw = patchedObjectJSONBytes
	return nil
}

// replaceClusterLabelKeyVariables finds all occurrences of the OverrideClusterLabelKeyVariablePrefix pattern
// (e.g. ${MEMBER-CLUSTER-LABEL-KEY-region}) in the input string and replaces them with
// the corresponding label values from the cluster.
// If a label with the specified key doesn't exist, it returns an error.
func replaceClusterLabelKeyVariables(input string, cluster *clusterv1beta1.MemberCluster) (string, error) {
	prefixLen := len(placementv1alpha1.OverrideClusterLabelKeyVariablePrefix)
	result := input

	for {
		startIdx := strings.Index(result, placementv1alpha1.OverrideClusterLabelKeyVariablePrefix)
		if startIdx == -1 {
			break
		}
		// extract the key value user wants to replace
		endIdx := strings.Index(result[startIdx+prefixLen:], "}")
		if endIdx == -1 {
			klog.V(2).InfoS("malformed key ${MEMBER-CLUSTER-LABEL-KEY without the closing `}`", "input", input)
			return "", fmt.Errorf("input %s is missing the closing bracket `}`", input)
		}
		endIdx += startIdx + prefixLen
		// extract the key name
		keyName := result[startIdx+prefixLen : endIdx]
		// check if the key exists in the cluster labels
		labelValue, exists := cluster.ObjectMeta.Labels[keyName]
		if !exists {
			klog.V(2).InfoS("Label key not found on cluster", "key", keyName, "cluster", cluster.Name)
			return "", fmt.Errorf("label key %s not found on cluster %s", keyName, cluster.Name)
		}
		// replace this instance of the variable with the actual label value
		fullVariable := result[startIdx : endIdx+1]
		result = strings.Replace(result, fullVariable, labelValue, 1)
	}
	return result, nil
}
