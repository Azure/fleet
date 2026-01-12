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

package statusbackreporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"
	parallelizerutil "go.goms.io/fleet/pkg/utils/parallelizer"
)

// Reconciler reconciles a Work object (specifically its status) to back-report
// statuses to their corresponding original resources in the hub cluster.
type Reconciler struct {
	hubClient        client.Client
	hubDynamicClient dynamic.Interface

	parallelizer parallelizerutil.Parallelizer
}

// NewReconciler creates a new Reconciler.
func NewReconciler(hubClient client.Client, hubDynamicClient dynamic.Interface, parallelizer parallelizerutil.Parallelizer) *Reconciler {
	if parallelizer == nil {
		klog.V(2).InfoS("parallelizer is not set; using the default parallelizer with a worker count of 1")
		parallelizer = parallelizerutil.NewParallelizer(1)
	}

	return &Reconciler{
		hubClient:        hubClient,
		hubDynamicClient: hubDynamicClient,
		parallelizer:     parallelizer,
	}
}

// Reconcile reconciles the Work object to back-report statuses to their corresponding
// original resources in the hub cluster.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	workRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation loop starts", "controller", "statusBackReporter", "work", workRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation loop ends", "controller", "statusBackReporter", "work", workRef, "latency", latency)
	}()

	work := &placementv1beta1.Work{}
	if err := r.hubClient.Get(ctx, req.NamespacedName, work); err != nil {
		klog.ErrorS(err, "Failed to retrieve Work object", "work", workRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Perform a sanity check; make sure that mirroring back to original resources can be done, i.e.,
	// the scheduling policy is set to the PickFixed type with exactly one target cluster, or the PickN
	// type with the number of clusters set to 1. The logic also checks if the report back strategy still
	// allows status back-reporting.
	placementObj, shouldSkip, err := r.validatePlacementObjectForOriginalResourceStatusBackReporting(ctx, work)
	if err != nil {
		klog.ErrorS(err, "Failed to validate the placement object associated with the Work object for back-reporting statuses to original resources", "work", workRef)
		return ctrl.Result{}, err
	}
	if shouldSkip {
		klog.V(2).InfoS("Skip status back-reporting to original resources as the report-back strategy on the placement object forbids so", "work", workRef, "placement", klog.KObj(placementObj))
		return ctrl.Result{}, nil
	}

	// Prepare a map for quick lookup of whether a resource is enveloped.
	isResEnvelopedByIdStr := prepareIsResEnvelopedMap(placementObj)

	// Back-report statuses to original resources.

	// Prepare a child context.
	// Cancel the child context anyway to avoid leaks.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(work.Status.ManifestConditions))
	doWork := func(pieces int) {
		manifestCond := &work.Status.ManifestConditions[pieces]
		resIdentifier := manifestCond.Identifier

		applyCond := meta.FindStatusCondition(work.Status.Conditions, placementv1beta1.WorkConditionTypeApplied)
		if applyCond == nil || applyCond.ObservedGeneration != work.Generation || applyCond.Status != metav1.ConditionTrue {
			// The resource has not been successfully applied yet. Skip back-reporting.
			klog.V(2).InfoS("Skip status back-reporting for the resource; the resource has not been successfully applied yet", "work", workRef, "resourceIdentifier", resIdentifier)
			return
		}

		// Skip the resource if there is no back-reported status.
		if manifestCond.BackReportedStatus == nil || len(manifestCond.BackReportedStatus.ObservedStatus.Raw) == 0 {
			klog.V(2).InfoS("Skip status back-reporting for the resource; there is no back-reported status", "work", workRef, "resourceIdentifier", resIdentifier)
			return
		}

		// Skip the resource if it is enveloped.
		idStr := formatWorkResourceIdentifier(&resIdentifier)
		isEnveloped, ok := isResEnvelopedByIdStr[idStr]
		if !ok {
			// The resource is not found in the list of selected resources as reported by the status of the placement object.
			//
			// This is not considered as an error as the resource might be absent due to consistency reasons (i.e., it has
			// just been de-selected); the status back-reporter will skip the resource for now.
			klog.V(2).InfoS("Skip status back-reporting for the resource; the resource is not found in the list of selected resources in the placement object", "work", workRef, "resourceIdentifier", resIdentifier)
			return
		}
		if isEnveloped {
			// The resource is enveloped; skip back-reporting.
			klog.V(2).InfoS("Skip status back-reporting for the resource; the resource is enveloped", "work", workRef, "resourceIdentifier", resIdentifier)
			return
		}

		// Note that applied resources should always have a valid identifier set; for simplicity reasons
		// here the back-reporter will no longer perform any validation.
		gvr := schema.GroupVersionResource{
			Group:    resIdentifier.Group,
			Version:  resIdentifier.Version,
			Resource: resIdentifier.Resource,
		}
		nsName := resIdentifier.Namespace
		resName := resIdentifier.Name
		unstructured, err := r.hubDynamicClient.Resource(gvr).Namespace(nsName).Get(ctx, resName, metav1.GetOptions{})
		if err != nil {
			wrappedErr := fmt.Errorf("failed to retrieve the target resource for status back-reporting: %w", err)
			klog.ErrorS(err, "Failed to retrieve the target resource for status back-reporting", "work", workRef, "resourceIdentifier", resIdentifier)
			errs[pieces] = wrappedErr
			return
		}

		// Set the back-reported status to the target resource.
		statusWrapper := make(map[string]interface{})
		if err := json.Unmarshal(manifestCond.BackReportedStatus.ObservedStatus.Raw, &statusWrapper); err != nil {
			wrappedErr := fmt.Errorf("failed to unmarshal back-reported status: %w", err)
			klog.ErrorS(err, "Failed to unmarshal back-reported status", "work", workRef, "resourceIdentifier", resIdentifier)
			errs[pieces] = wrappedErr
			return
		}

		// Note that if the applied resource has a status sub-resource, it is usually safe for us to assume that
		// the original resource should also have a status sub-resource of the same format.
		unstructured.Object["status"] = statusWrapper["status"]
		_, err = r.hubDynamicClient.Resource(gvr).Namespace(nsName).UpdateStatus(ctx, unstructured, metav1.UpdateOptions{})
		if err != nil {
			// TO-DO (chenyu1): check for cases where the API definition is inconsistent between the member cluster
			// side and the hub cluster side, and single out the errors as user errors instead.
			wrappedErr := fmt.Errorf("failed to update status to the target resource: %w", err)
			klog.ErrorS(err, "Failed to update status to the target resource", "work", workRef, "resourceIdentifier", resIdentifier)
			errs[pieces] = wrappedErr
			return
		}
	}
	r.parallelizer.ParallelizeUntil(childCtx, len(work.Status.ManifestConditions), doWork, "backReportStatusToOriginalResources")
	return ctrl.Result{}, errorsutil.NewAggregate(errs)
}

// validatePlacementObjectForOriginalResourceStatusBackReporting validates whether
// the placement object associated with the given Work object is eligible for back-reporting
// statuses to original resources.
func (r *Reconciler) validatePlacementObjectForOriginalResourceStatusBackReporting(
	ctx context.Context, work *placementv1beta1.Work) (placementv1beta1.PlacementObj, bool, error) {
	// Read the `kubernetes-fleet.io/parent-CRP` label to retrieve the CRP/RP name.
	parentPlacementName, ok := work.Labels[placementv1beta1.PlacementTrackingLabel]
	if !ok || len(parentPlacementName) == 0 {
		// Normally this should never occur.
		wrappedErr := fmt.Errorf("the placement tracking label is absent or invalid (label value: %s)", parentPlacementName)
		return nil, false, controller.NewUnexpectedBehaviorError(wrappedErr)
	}

	// Read the `kubernetes-fleet.io/parent-namespace` label to retrieve the RP namespace (if any).
	parentPlacementNSName := work.Labels[placementv1beta1.ParentNamespaceLabel]

	var placementObj placementv1beta1.PlacementObj
	if len(parentPlacementNSName) == 0 {
		// Retrieve the CRP object.
		placementObj = &placementv1beta1.ClusterResourcePlacement{}
		if err := r.hubClient.Get(ctx, client.ObjectKey{Name: parentPlacementName}, placementObj); err != nil {
			wrappedErr := fmt.Errorf("failed to retrieve CRP object: %w", err)
			return nil, false, controller.NewAPIServerError(true, wrappedErr)
		}
	} else {
		// Retrieve the RP object.
		placementObj = &placementv1beta1.ResourcePlacement{}
		if err := r.hubClient.Get(ctx, client.ObjectKey{Namespace: parentPlacementNSName, Name: parentPlacementName}, placementObj); err != nil {
			wrappedErr := fmt.Errorf("failed to retrieve RP object: %w", err)
			return nil, false, controller.NewAPIServerError(true, wrappedErr)
		}
	}

	// Validate the scheduling policy of the placement object.
	schedulingPolicy := placementObj.GetPlacementSpec().Policy
	switch {
	case schedulingPolicy == nil:
		// The system uses a default scheduling policy of the PickAll placement type. Reject status back-reporting.
		wrappedErr := fmt.Errorf("no scheduling policy specified (the PickAll type is in use); cannot back-report status to original resources")
		return nil, false, controller.NewUserError(wrappedErr)
	case schedulingPolicy.PlacementType == placementv1beta1.PickAllPlacementType:
		wrappedErr := fmt.Errorf("the scheduling policy in use is of the PickAll type; cannot back-report status to original resources")
		return nil, false, controller.NewUserError(wrappedErr)
	case schedulingPolicy.PlacementType == placementv1beta1.PickFixedPlacementType && len(schedulingPolicy.ClusterNames) != 1:
		wrappedErr := fmt.Errorf("the scheduling policy in use is of the PickFixed type, but it has more than one target cluster (%d clusters); cannot back-report status to original resources", len(schedulingPolicy.ClusterNames))
		return nil, false, controller.NewUserError(wrappedErr)
	case schedulingPolicy.PlacementType == placementv1beta1.PickNPlacementType && schedulingPolicy.NumberOfClusters == nil:
		// Normally this should never occur.
		wrappedErr := fmt.Errorf("the scheduling policy in use is of the PickN type, but no number of target clusters is specified; cannot back-report status to original resources")
		return nil, false, controller.NewUserError(wrappedErr)
	case schedulingPolicy.PlacementType == placementv1beta1.PickNPlacementType && *schedulingPolicy.NumberOfClusters != 1:
		wrappedErr := fmt.Errorf("the scheduling policy in use is of the PickN type, but the number of target clusters is not set to 1; cannot back-report status to original resources")
		return nil, false, controller.NewUserError(wrappedErr)
	}

	// Check if the report back strategy on the placement object still allows status back-reporting to the original resources.
	reportBackStrategy := placementObj.GetPlacementSpec().Strategy.ReportBackStrategy
	switch {
	case reportBackStrategy == nil:
		klog.V(2).InfoS("Skip status back-reporting; the strategy has not been set", "placement", klog.KObj(placementObj))
		return placementObj, true, nil
	case reportBackStrategy.Type != placementv1beta1.ReportBackStrategyTypeMirror:
		klog.V(2).InfoS("Skip status back-reporting; it has been disabled in the strategy", "placement", klog.KObj(placementObj))
		return placementObj, true, nil
	case reportBackStrategy.Destination == nil:
		// This in theory should never occur; CEL based validation should have rejected such strategies.
		klog.V(2).InfoS("Skip status back-reporting; destination has not been set in the strategy", "placement", klog.KObj(placementObj))
		return placementObj, true, nil
	case *reportBackStrategy.Destination != placementv1beta1.ReportBackDestinationOriginalResource:
		klog.V(2).InfoS("Skip status back-reporting; destination has been set to the Work API", "placement", klog.KObj(placementObj))
		return placementObj, true, nil
	}

	// The scheduling policy is valid for back-reporting statuses to original resources.
	return placementObj, false, nil
}

// formatResourceIdentifier formats a ResourceIdentifier object to a string for keying purposes.
//
// The format in use is `[API-GROUP]/[API-VERSION]/[API-KIND]/[NAMESPACE]/[NAME]`, e.g., `/v1/Namespace//work`.
func formatResourceIdentifier(resourceIdentifier *placementv1beta1.ResourceIdentifier) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", resourceIdentifier.Group, resourceIdentifier.Version, resourceIdentifier.Kind, resourceIdentifier.Namespace, resourceIdentifier.Name)
}

// formatWorkResourceIdentifier formats a WorkResourceIdentifier object to a string for keying purposes.
//
// The format in use is `[API-GROUP]/[API-VERSION]/[API-KIND]/[NAMESPACE]/[NAME]`, e.g., `/v1/Namespace//work`.
func formatWorkResourceIdentifier(workResourceIdentifier *placementv1beta1.WorkResourceIdentifier) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", workResourceIdentifier.Group, workResourceIdentifier.Version, workResourceIdentifier.Kind, workResourceIdentifier.Namespace, workResourceIdentifier.Name)
}

// prepareIsResEnvelopedMap prepares a map for quick lookup of whether a resource is enveloped.
func prepareIsResEnvelopedMap(placementObj placementv1beta1.PlacementObj) map[string]bool {
	isResEnvelopedByIdStr := make(map[string]bool)

	selectedResources := placementObj.GetPlacementStatus().SelectedResources
	for idx := range selectedResources {
		selectedRes := selectedResources[idx]
		idStr := formatResourceIdentifier(&selectedRes)
		isResEnvelopedByIdStr[idStr] = selectedRes.Envelope != nil
	}

	return isResEnvelopedByIdStr
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("status-back-reporter").
		Watches(&placementv1beta1.Work{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
