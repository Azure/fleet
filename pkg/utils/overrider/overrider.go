/*
Copyright 2025 The KubeFleet Authors.

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

// Package overrider defines common utils for working with override.
package overrider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
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
	placementKey string,
	masterResourceSnapshot placementv1beta1.ResourceSnapshotObj,
) ([]*placementv1beta1.ClusterResourceOverrideSnapshot, []*placementv1beta1.ResourceOverrideSnapshot, error) {
	// fetch the cro and ro snapshot list first before finding the matched ones.
	latestSnapshotLabelMatcher := client.MatchingLabels{
		placementv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
	}
	croList := &placementv1beta1.ClusterResourceOverrideSnapshotList{}
	if err := c.List(ctx, croList, latestSnapshotLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the clusterResourceOverrideSnapshots")
		return nil, nil, err
	}
	roList := &placementv1beta1.ResourceOverrideSnapshotList{}
	if err := c.List(ctx, roList, latestSnapshotLabelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list all the resourceOverrideSnapshots")
		return nil, nil, err
	}

	if len(croList.Items) == 0 && len(roList.Items) == 0 {
		return nil, nil, nil // no overrides and nothing to do
	}

	resourceSnapshots, err := controller.FetchAllResourceSnapshotsAlongWithMaster(ctx, c, placementKey, masterResourceSnapshot)
	if err != nil {
		return nil, nil, err
	}

	possibleCROs := make(map[placementv1beta1.ResourceIdentifier]bool)
	possibleROs := make(map[placementv1beta1.ResourceIdentifier]bool)
	// List all the possible CROs and ROs based on the selected resources.
	for _, snapshot := range resourceSnapshots {
		for _, res := range snapshot.GetResourceSnapshotSpec().SelectedResources {
			croCandidates, roCandidates, err := collectOverrideCandidatesFromSelectedResource(manager, res)
			if err != nil {
				klog.ErrorS(err, "Failed to collect override candidates from selected resource", "snapshot", klog.KObj(snapshot), "selectedResource", res.Raw)
				return nil, nil, err
			}
			for cro := range croCandidates {
				possibleCROs[cro] = true
			}
			for ro := range roCandidates {
				possibleROs[ro] = true
			}
		}
	}

	filteredCRO := make([]*placementv1beta1.ClusterResourceOverrideSnapshot, 0, len(croList.Items))
	filteredRO := make([]*placementv1beta1.ResourceOverrideSnapshot, 0, len(roList.Items))
	for i := range croList.Items {
		placementInOverride := croList.Items[i].Spec.OverrideSpec.Placement
		if placementInOverride != nil && placementInOverride.Name != placementKey {
			klog.V(2).InfoS("Skipping this override which was created for another placement", "clusterResourceOverride", klog.KObj(&croList.Items[i]), "placementInOverride", placementInOverride.Name, "placement", placementKey)
			continue
		}

		matched := false
		for _, selector := range croList.Items[i].Spec.OverrideSpec.ClusterResourceSelectors {
			croKey := placementv1beta1.ResourceIdentifier{
				Group:   selector.Group,
				Version: selector.Version,
				Kind:    selector.Kind,
				Name:    selector.Name,
			}
			if possibleCROs[croKey] {
				filteredCRO = append(filteredCRO, &croList.Items[i])
				matched = true
				break
			}
		}
		if !matched {
			klog.V(2).InfoS("ClusterResourceOverrideSnapshot has no selector that matches a selected resource or inner envelope resource in this placement", "clusterResourceOverride", klog.KObj(&croList.Items[i]), "placement", placementKey, "selectors", croList.Items[i].Spec.OverrideSpec.ClusterResourceSelectors)
		}
	}
	for i := range roList.Items {
		placementInOverride := roList.Items[i].Spec.OverrideSpec.Placement
		if placementInOverride != nil {
			placementKeyInOverride := placementInOverride.Name
			if placementInOverride.Scope == placementv1beta1.NamespaceScoped {
				placementKeyInOverride = controller.GetObjectKeyFromNamespaceName(roList.Items[i].Namespace, placementInOverride.Name)
			}
			if placementKeyInOverride != placementKey {
				klog.V(2).InfoS("Skipping this override which was created for another placement", "resourceOverride", klog.KObj(&roList.Items[i]), "placementInOverride", placementKeyInOverride, "placement", placementKey)
				continue
			}
		}

		matched := false
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
				matched = true
				break
			}
		}
		if !matched {
			klog.V(2).InfoS("ResourceOverrideSnapshot has no selector that matches a selected resource or inner envelope resource in this placement", "resourceOverride", klog.KObj(&roList.Items[i]), "placement", placementKey, "selectors", roList.Items[i].Spec.OverrideSpec.ResourceSelectors)
		}
	}
	return filteredCRO, filteredRO, nil
}

func collectOverrideCandidatesFromSelectedResource(
	manager informer.Manager,
	resourceContent placementv1beta1.ResourceContent,
) (map[placementv1beta1.ResourceIdentifier]bool, map[placementv1beta1.ResourceIdentifier]bool, error) {
	uResource, err := unmarshalSelectedResource(resourceContent)
	if err != nil {
		return nil, nil, err
	}

	croCandidates := make(map[placementv1beta1.ResourceIdentifier]bool)
	roCandidates := make(map[placementv1beta1.ResourceIdentifier]bool)
	switch uResource.GroupVersionKind() {
	case placementv1beta1.GroupVersion.WithKind(string(placementv1beta1.ClusterResourceEnvelopeType)):
		var envelope placementv1beta1.ClusterResourceEnvelope
		if err := json.Unmarshal(resourceContent.Raw, &envelope); err != nil {
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		}
		collectCandidatesFromClusterResourceEnvelope(manager, &envelope, croCandidates)
	case placementv1beta1.GroupVersion.WithKind(string(placementv1beta1.ResourceEnvelopeType)):
		var envelope placementv1beta1.ResourceEnvelope
		if err := json.Unmarshal(resourceContent.Raw, &envelope); err != nil {
			return nil, nil, controller.NewUnexpectedBehaviorError(err)
		}
		croCandidates[namespaceCROCandidate(envelope.GetNamespace())] = true
		collectCandidatesFromResourceEnvelope(manager, &envelope, roCandidates)
	default:
		addCandidatesFromResource(manager, uResource, croCandidates, roCandidates)
	}
	return croCandidates, roCandidates, nil
}

func unmarshalSelectedResource(resourceContent placementv1beta1.ResourceContent) (*unstructured.Unstructured, error) {
	var uResource unstructured.Unstructured
	if err := uResource.UnmarshalJSON(resourceContent.Raw); err != nil {
		klog.ErrorS(err, "Resource has invalid content", "selectedResource", resourceContent.Raw)
		return nil, controller.NewUnexpectedBehaviorError(err)
	}
	return &uResource, nil
}

// collectCandidatesFromClusterResourceEnvelope collects override candidates from the inner manifests of a
// cluster resource envelope. Collecting candidates is a best-effort selection concern and must never block
// the rollout, so an invalid inner manifest or one that cannot be parsed is logged and skipped rather than returning an
// error. The authoritative validation of envelope contents lives in the work generator
// (pkg/controllers/workgenerator/envelope.go), which surfaces the user-facing failure.
func collectCandidatesFromClusterResourceEnvelope(
	manager informer.Manager,
	envelope *placementv1beta1.ClusterResourceEnvelope,
	croCandidates map[placementv1beta1.ResourceIdentifier]bool,
) {
	keys := sortedEnvelopeDataKeys(envelope.Data)
	for _, key := range keys {
		uObj, err := unmarshalEnvelopeData(envelope, key)
		if err != nil {
			klog.V(2).InfoS("Skipped an inner manifest that could not be parsed while collecting override candidates", "manifestKey", key, "envelope", klog.KObj(envelope), "err", err)
			continue
		}
		if !manager.IsClusterScopedResources(uObj.GroupVersionKind()) {
			klog.V(2).InfoS("Skipped an inner manifest while collecting override candidates: a namespaced object has been wrapped in a cluster resource envelope", "manifestKey", key, "wrappedObject", klog.KRef(uObj.GetNamespace(), uObj.GetName()), "envelope", klog.KObj(envelope))
			continue
		}
		croCandidates[clusterScopedCROCandidate(uObj)] = true
	}
}

// collectCandidatesFromResourceEnvelope collects override candidates from the inner manifests of a resource
// envelope. Collecting candidates is a best-effort selection concern and must never block the rollout, so an
// invalid inner manifest or one that cannot be parsed is logged and skipped rather than returning an error. The
// authoritative validation of envelope contents lives in the work generator
// (pkg/controllers/workgenerator/envelope.go), which surfaces the user-facing failure.
func collectCandidatesFromResourceEnvelope(
	manager informer.Manager,
	envelope *placementv1beta1.ResourceEnvelope,
	roCandidates map[placementv1beta1.ResourceIdentifier]bool,
) {
	keys := sortedEnvelopeDataKeys(envelope.Data)
	for _, key := range keys {
		uObj, err := unmarshalEnvelopeData(envelope, key)
		if err != nil {
			klog.V(2).InfoS("Skipped an inner manifest that could not be parsed while collecting override candidates", "manifestKey", key, "envelope", klog.KObj(envelope), "err", err)
			continue
		}
		if manager.IsClusterScopedResources(uObj.GroupVersionKind()) {
			klog.V(2).InfoS("Skipped an inner manifest while collecting override candidates: a cluster scoped object has been wrapped in a resource envelope", "manifestKey", key, "wrappedObject", klog.KRef(uObj.GetNamespace(), uObj.GetName()), "envelope", klog.KObj(envelope))
			continue
		}
		if envelope.GetNamespace() != uObj.GetNamespace() {
			klog.V(2).InfoS("Skipped an inner manifest while collecting override candidates: a namespaced object has been wrapped in a resource envelope from another namespace", "manifestKey", key, "wrappedObject", klog.KRef(uObj.GetNamespace(), uObj.GetName()), "envelope", klog.KObj(envelope))
			continue
		}
		roCandidates[namespacedROCandidate(uObj, envelope.GetNamespace())] = true
	}
}

func unmarshalEnvelopeData(envelope placementv1beta1.EnvelopeReader, key string) (*unstructured.Unstructured, error) {
	var uObj unstructured.Unstructured
	if err := uObj.UnmarshalJSON(envelope.GetData()[key].Raw); err != nil {
		return nil, fmt.Errorf("failed to parse the wrapped manifest data to a Kubernetes runtime object (manifestKey=%s,envelopeObjRef=%v): %w", key, envelope.GetEnvelopeObjRef(), err)
	}
	return &uObj, nil
}

func sortedEnvelopeDataKeys(data map[string]runtime.RawExtension) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func addCandidatesFromResource(
	manager informer.Manager,
	uResource *unstructured.Unstructured,
	croCandidates map[placementv1beta1.ResourceIdentifier]bool,
	roCandidates map[placementv1beta1.ResourceIdentifier]bool,
) {
	if !manager.IsClusterScopedResources(uResource.GroupVersionKind()) {
		croCandidates[namespaceCROCandidate(uResource.GetNamespace())] = true           // selected by the namespace
		roCandidates[namespacedROCandidate(uResource, uResource.GetNamespace())] = true // selected by the object itself
		return
	}
	croCandidates[clusterScopedCROCandidate(uResource)] = true // selected by the object itself
}

func namespaceCROCandidate(namespace string) placementv1beta1.ResourceIdentifier {
	return placementv1beta1.ResourceIdentifier{
		Group:   utils.NamespaceMetaGVK.Group,
		Version: utils.NamespaceMetaGVK.Version,
		Kind:    utils.NamespaceMetaGVK.Kind,
		Name:    namespace,
	}
}

func clusterScopedCROCandidate(uObj *unstructured.Unstructured) placementv1beta1.ResourceIdentifier {
	gvk := uObj.GroupVersionKind()
	return placementv1beta1.ResourceIdentifier{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
		Name:    uObj.GetName(),
	}
}

func namespacedROCandidate(uObj *unstructured.Unstructured, namespace string) placementv1beta1.ResourceIdentifier {
	gvk := uObj.GroupVersionKind()
	return placementv1beta1.ResourceIdentifier{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: namespace,
		Name:      uObj.GetName(),
	}
}

// PickFromResourceMatchedOverridesForTargetCluster filter the overrides that are matched with resources to the target cluster.
func PickFromResourceMatchedOverridesForTargetCluster(
	ctx context.Context,
	c client.Reader,
	targetCluster string,
	croList []*placementv1beta1.ClusterResourceOverrideSnapshot,
	roList []*placementv1beta1.ResourceOverrideSnapshot,
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

	croFiltered := make([]*placementv1beta1.ClusterResourceOverrideSnapshot, 0, len(croList))
	for i, cro := range croList {
		matched, err := isClusterMatched(&cluster, cro.Spec.OverrideSpec.Policy)
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

	roFiltered := make([]*placementv1beta1.ResourceOverrideSnapshot, 0, len(roList))
	for i, ro := range roList {
		matched, err := isClusterMatched(&cluster, ro.Spec.OverrideSpec.Policy)
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

func isClusterMatched(cluster *clusterv1beta1.MemberCluster, policy *placementv1beta1.OverridePolicy) (bool, error) {
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
func IsClusterMatched(cluster *clusterv1beta1.MemberCluster, rule placementv1beta1.OverrideRule) (bool, error) {
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
