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
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

const (
	noNamespaceResourceSelectorMsg = "NamespaceAccessible ClusterResourcePlacement doesn't specify a resource selector which selects a namespace"

	failedCRPSMessageFmt           = "Failed to create or update ClusterResourcePlacementStatus: %v"
	successfulCRPSMessageFmt       = "Successfully created or updated ClusterResourcePlacementStatus in namespace '%s'"
	namespaceConsistencyMessageFmt = "namespace resource selector is choosing a different namespace '%s' from what was originally picked '%s'. This is not allowed for NamespaceAccessible ClusterResourcePlacement"
)

// extractNamespaceFromResourceSelectors extracts the target namespace name from the
// ClusterResourcePlacement's ResourceSelectors. This function looks for a Namespace
// resource selector and returns its name. Returns empty string if no Namespace
// selector is found.
func extractNamespaceFromResourceSelectors(crp *placementv1beta1.ClusterResourcePlacement) string {
	// CEL validation ensures exactly one Namespace selector exists when NamespaceAccessible.
	for _, selector := range crp.Spec.ResourceSelectors {
		selectorGVK := schema.GroupVersionKind{
			Group:   selector.Group,
			Version: selector.Version,
			Kind:    selector.Kind,
		}
		// Check if this is a namespace selector by comparing with the standard namespace GVK.
		if selectorGVK == utils.NamespaceGVK {
			return selector.Name
		}
	}

	return ""
}

// isNamespaceAccessibleCRP checks if the given placement object is a ClusterResourcePlacement
// with StatusReportingScope set to NamespaceAccessible. Returns true if the placement is
// a CRP with NamespaceAccessible scope, false otherwise.
func isNamespaceAccessibleCRP(placementObj placementv1beta1.PlacementObj) bool {
	crp, ok := placementObj.(*placementv1beta1.ClusterResourcePlacement)
	if !ok {
		klog.V(2).InfoS("Skipped processing RP to create/update ClusterResourcePlacementStatus", "placement", klog.KObj(placementObj))
		return false
	}

	// Only process if StatusReportingScope is NamespaceAccessible.
	if crp.Spec.StatusReportingScope != placementv1beta1.NamespaceAccessible {
		klog.V(2).InfoS("StatusReportingScope is not NamespaceAccessible", "crp", klog.KObj(placementObj))
		return false
	}

	return true
}

// filterStatusSyncedCondition creates a copy of the placement status with the
// ClusterResourcePlacementStatusSynced condition removed. This is used when
// creating ClusterResourcePlacementStatus objects because the StatusSynced
// condition is only relevant for the main CRP object, not the CRPS object.
// Returns a filtered copy of the status with all conditions except StatusSynced.
func filterStatusSyncedCondition(status *placementv1beta1.PlacementStatus) *placementv1beta1.PlacementStatus {
	filteredStatus := status.DeepCopy()

	// Filter out the ClusterResourcePlacementStatusSynced condition.
	filteredConditions := make([]metav1.Condition, 0, len(filteredStatus.Conditions))
	for _, condition := range filteredStatus.Conditions {
		if condition.Type != string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType) {
			filteredConditions = append(filteredConditions, condition)
		}
	}
	filteredStatus.Conditions = filteredConditions

	return filteredStatus
}

// buildStatusSyncedCondition creates a StatusSynced condition based on the sync result.
func buildNamepsaceAccessibleStatusSyncedCondition(syncErr error, targetNamespace string, generation int64) metav1.Condition {
	if syncErr != nil {
		return metav1.Condition{
			Type:               string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType),
			Status:             metav1.ConditionFalse,
			Reason:             condition.StatusSyncFailedReason,
			Message:            fmt.Sprintf(failedCRPSMessageFmt, syncErr),
			ObservedGeneration: generation,
		}
	}

	return metav1.Condition{
		Type:               string(placementv1beta1.ClusterResourcePlacementStatusSyncedConditionType),
		Status:             metav1.ConditionTrue,
		Reason:             condition.StatusSyncSucceededReason,
		Message:            fmt.Sprintf(successfulCRPSMessageFmt, targetNamespace),
		ObservedGeneration: generation,
	}
}

// buildNamespaceAccessibleScheduledCondition creates a Scheduled condition with invalid resource selector reason.
func buildNamespaceAccessibleScheduledCondition(generation int64, msg string) metav1.Condition {
	return metav1.Condition{
		Type:               string(placementv1beta1.ClusterResourcePlacementScheduledConditionType),
		Status:             metav1.ConditionFalse,
		Reason:             condition.InvalidResourceSelectorsReason,
		Message:            msg,
		ObservedGeneration: generation,
	}
}

// setConditionAndUpdateStatus is a helper function that sets the argued condition on the placement
// object and updates its status.
func (r *Reconciler) setConditionAndUpdateStatus(ctx context.Context, placementObj placementv1beta1.PlacementObj, condition metav1.Condition) error {
	placementKObj := klog.KObj(placementObj)

	// Set the condition on the placement object.
	placementObj.SetConditions(condition)

	// Update the placement status.
	if err := r.Client.Status().Update(ctx, placementObj); err != nil {
		klog.ErrorS(err, "Failed to update the placement status with StatusSynced condition", "crp", placementKObj)
		return controller.NewUpdateIgnoreConflictError(err)
	}

	klog.V(2).InfoS("Updated the placement status with StatusSynced condition", "crp", placementKObj)

	return nil
}

// syncClusterResourcePlacementStatus creates or updates a ClusterResourcePlacementStatus
// object in the target namespace for ClusterResourcePlacements with NamespaceAccessible scope.
// It extracts the target namespace from the CRP's resource selectors, creates/updates a CRPS object
// with filtered status (excluding StatusSynced condition), and sets the CRP as owner for
// automatic cleanup. Returns an error if the operation fails.
func (r *Reconciler) syncClusterResourcePlacementStatus(ctx context.Context, crp *placementv1beta1.ClusterResourcePlacement, targetNamespace string) error {
	if targetNamespace == "" {
		// No valid target namespace - skip sync.
		return nil
	}
	crpKObj := klog.KObj(crp)
	crpStatus := &placementv1beta1.ClusterResourcePlacementStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crp.Name,
			Namespace: targetNamespace,
		},
	}
	// Use CreateOrUpdate to handle both creation and update cases.
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, crpStatus, func() error {
		// Set the placement status (excluding StatusSynced condition) and update time.
		crpStatus.PlacementStatus = *filterStatusSyncedCondition(&crp.Status)
		crpStatus.LastUpdatedTime = metav1.Now()

		// Set CRP as owner - this ensures automatic cleanup when CRP is deleted.
		return controllerutil.SetControllerReference(crp, crpStatus, r.Scheme)
	})

	if err != nil {
		klog.ErrorS(err, "Failed to create or update ClusterResourcePlacementStatus", "crp", crpKObj, "namespace", targetNamespace)
		return controller.NewAPIServerError(false, fmt.Errorf("failed to create or update ClusterResourcePlacementStatus: %w", err))
	}

	klog.V(2).InfoS("Successfully handled ClusterResourcePlacementStatus", "crp", crpKObj, "namespace", targetNamespace, "operation", op)
	return nil
}

// handleNamespaceAccessibleCRP handles the complete workflow for ClusterResourcePlacements
// with NamespaceAccessible scope. It syncs the ClusterResourcePlacementStatus object in
// the target namespace, builds a StatusSynced/Scheduled condition based on the sync result,
// target namespace adds the condition to the CRP, and updates the CRP status.
func (r *Reconciler) handleNamespaceAccessibleCRP(ctx context.Context, placementObj placementv1beta1.PlacementObj) error {
	placementKObj := klog.KObj(placementObj)
	crp, _ := placementObj.(*placementv1beta1.ClusterResourcePlacement)
	// Extract target namespace from resource selectors.
	targetNamespace := extractNamespaceFromResourceSelectors(crp)
	if targetNamespace == "" {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("no namespace selector found despite NamespaceAccessible scope")),
			"Failed to find valid Namespace selector for NamespaceAccessible scope",
			"crp", placementKObj)
	}
	syncErr := r.syncClusterResourcePlacementStatus(ctx, crp, targetNamespace)

	var namespaceAccessibleCondition metav1.Condition
	if syncErr != nil {
		// status synced condition with error.
		namespaceAccessibleCondition = buildNamepsaceAccessibleStatusSyncedCondition(syncErr, targetNamespace, placementObj.GetGeneration())
	} else if targetNamespace == "" {
		// scheduled condition with invalid resource selector reason.
		namespaceAccessibleCondition = buildNamespaceAccessibleScheduledCondition(placementObj.GetGeneration(), noNamespaceResourceSelectorMsg)
	} else {
		// successful status synced condition.
		namespaceAccessibleCondition = buildNamepsaceAccessibleStatusSyncedCondition(syncErr, targetNamespace, placementObj.GetGeneration())
	}

	if err := r.setConditionAndUpdateStatus(ctx, placementObj, namespaceAccessibleCondition); err != nil {
		return err
	}

	if syncErr != nil {
		klog.ErrorS(syncErr, "Failed to sync ClusterResourcePlacementStatus", "placement", klog.KObj(placementObj))
		return syncErr
	}

	return nil
}

// validateNamespaceSelectorConsistency validates that ClusterResourcePlacement with
// NamespaceAccessible scope have consistent namespace selectors. This function assumes
// the placement is already confirmed to be NamespaceAccessible. It checks if the
// namespace selector has changed from what was originally selected and sets the
// Scheduled condition accordingly. Returns an error if validation fails or if
// there are issues updating the placement status.
func (r *Reconciler) validateNamespaceSelectorConsistency(ctx context.Context, placementObj placementv1beta1.PlacementObj) (bool, error) {
	placementKObj := klog.KObj(placementObj)
	crp, _ := placementObj.(*placementv1beta1.ClusterResourcePlacement)

	placementStatus := placementObj.GetPlacementStatus()
	currentTargetNamespace := ""

	// Extract namespace from selected resources in status.
	for _, resource := range placementStatus.SelectedResources {
		resourceGVK := schema.GroupVersionKind{
			Group:   resource.Group,
			Version: resource.Version,
			Kind:    resource.Kind,
		}
		// Check if this is a namespace resource by comparing with the standard namespace GVK.
		if resourceGVK == utils.NamespaceGVK {
			currentTargetNamespace = resource.Name
			break
		}
	}

	// CRP status has not been populated or no namespace selected by CRP.
	if currentTargetNamespace == "" {
		klog.V(2).InfoS("No namespace selected yet in status", "crp", placementKObj)
		// No namespace selected yet - skip validation for now.
		return true, nil
	}

	// Extract target namespace from resource selectors in spec.
	targetNamespaceFromSelector := extractNamespaceFromResourceSelectors(crp)
	if targetNamespaceFromSelector == "" {
		klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("no namespace selector found despite NamespaceAccessible scope")),
			"Failed to find valid Namespace selector for NamespaceAccessible scope",
			"crp", placementKObj)
		scheduledCondition := buildNamespaceAccessibleScheduledCondition(placementObj.GetGeneration(), noNamespaceResourceSelectorMsg)
		if err := r.setConditionAndUpdateStatus(ctx, placementObj, scheduledCondition); err != nil {
			return false, err
		}
		return false, nil
	}

	// If both namespaces exist and they don't match, it means the namespace selector changed.
	if currentTargetNamespace != targetNamespaceFromSelector {
		klog.V(2).InfoS("Namespace selector has changed for NamespaceAccessible CRP",
			"crp", placementKObj,
			"currentTargetNamespace", currentTargetNamespace,
			"newTargetNamespace", targetNamespaceFromSelector)

		scheduledCondition := buildNamespaceAccessibleScheduledCondition(placementObj.GetGeneration(), fmt.Sprintf(namespaceConsistencyMessageFmt, targetNamespaceFromSelector, currentTargetNamespace))

		// Set condition and update placement status.
		if err := r.setConditionAndUpdateStatus(ctx, placementObj, scheduledCondition); err != nil {
			return false, err
		}
		klog.ErrorS(controller.NewUserError(fmt.Errorf(namespaceConsistencyMessageFmt, currentTargetNamespace, targetNamespaceFromSelector)),
			"Namespace selector inconsistency detected",
			"crp", placementKObj)
		return false, nil
	}

	// Validation passed.
	return true, nil
}
