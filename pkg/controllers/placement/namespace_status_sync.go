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

package placement

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

// extractNamespaceFromResourceSelectors extracts the namespace name from ResourceSelectors
// when StatusReportingScope is NamespaceAccessible. Returns empty string if not applicable.
func extractNamespaceFromResourceSelectors(placementObj placementv1beta1.PlacementObj) string {
	spec := placementObj.GetPlacementSpec()

	// Only process if StatusReportingScope is NamespaceAccessible.
	if spec.StatusReportingScope != placementv1beta1.NamespaceAccessible {
		klog.V(2).InfoS("StatusReportingScope is not NamespaceAccessible", "placement", klog.KObj(placementObj))
		return ""
	}

	// CEL validation ensures exactly one Namespace selector exists when NamespaceAccessible.
	for _, selector := range spec.ResourceSelectors {
		selectorGVK := schema.GroupVersionKind{
			Group:   selector.Group,
			Version: selector.Version,
			Kind:    selector.Kind,
		}
		// Check if this is a namespace selector by comparing with the standard namespace GVK
		if selectorGVK == utils.NamespaceGVK {
			return selector.Name
		}
	}

	// This should never happen due to CEL validation, but defensive programming.
	klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("no namespace selector found despite NamespaceAccessible scope")),
		"Failed to find valid Namespace selector for NamespaceAccessible scope",
		"placement", klog.KObj(placementObj))
	return ""
}

// TODO: Need to handle the case where namespace selector changes on the CRP.
// syncClusterResourcePlacementStatus creates or updates ClusterResourcePlacementStatus
// object in the target namespace when StatusReportingScope is NamespaceAccessible.
func (r *Reconciler) syncClusterResourcePlacementStatus(ctx context.Context, placementObj placementv1beta1.PlacementObj) error {
	// Only sync for ClusterResourcePlacement objects (not ResourcePlacement).
	crp, ok := placementObj.(*placementv1beta1.ClusterResourcePlacement)
	if !ok {
		// This is a ResourcePlacement, not a ClusterResourcePlacement - skip sync.
		klog.V(2).InfoS("Skipped processing RP to create/update ClusterResourcePlacementStatus")
		return nil
	}

	// Extract target namespace.
	targetNamespace := extractNamespaceFromResourceSelectors(placementObj)
	if targetNamespace == "" {
		// Not NamespaceAccessible or no namespace found - skip sync.
		klog.V(2).InfoS("Skipped processing CRP to create/update ClusterResourcePlacementStatus", "crp", klog.KObj(crp))
		return nil
	}

	crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crp.Name, // Same name as CRP.
			Namespace: targetNamespace,
		},
	}
	// Use CreateOrUpdate to handle both creation and update cases.
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, crpStatus, func() error {
		// Set the placement status and update time.
		crpStatus.PlacementStatus = *crp.Status.DeepCopy()
		crpStatus.LastUpdatedTime = metav1.Now()

		// Set CRP as owner - this ensures automatic cleanup when CRP is deleted.
		return controllerutil.SetControllerReference(crp, crpStatus, r.Scheme)
	})

	if err != nil {
		klog.ErrorS(err, "Failed to create or update ClusterResourcePlacementStatus", "crp", klog.KObj(crp), "namespace", targetNamespace)
		return controller.NewAPIServerError(false, fmt.Errorf("failed to create or update ClusterResourcePlacementStatus: %w", err))
	}

	klog.V(2).InfoS("Successfully handled ClusterResourcePlacementStatus", "crp", klog.KObj(crp), "namespace", targetNamespace, "operation", op)
	return nil
}
